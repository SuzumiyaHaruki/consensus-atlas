package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const deepSeekFailureBaseline = "AGENT_PREFERENCE_BASELINE_REJECTED"

type etcdraftAgentB4ArmResult struct {
	ArmID    string
	Intent   *controlexperiment.GuardedTestIntent
	Plan     *controlexperiment.CompiledIntentPlanV2
	Instance *controlexperiment.IntentExecutionInstance
	Report   *controlexperiment.Report
	Bundle   *controlexperiment.ExecutionBundle
	Outcome  *controlexperiment.IntentOutcome
	Audit    controlexperiment.AgentInvocationAudit
}

func consumeEtcdraftAgentB4Arm(
	ctx context.Context,
	key string,
	client deepSeekIntentClient,
	freeze etcdraftAgentB4RequestFreeze,
	armID string,
) (etcdraftAgentB4ArmResult, error) {
	if err := validateEtcdraftAgentB4RequestFreezeBindings(freeze); err != nil {
		return etcdraftAgentB4ArmResult{}, err
	}
	prepared, commitment, err := etcdraftAgentB4ArmMaterial(freeze, armID)
	if err != nil {
		return etcdraftAgentB4ArmResult{}, err
	}
	if client.Endpoint != freeze.Freeze.Transport.Endpoint || client.Model != freeze.Freeze.Transport.Model ||
		client.MaxOutputTokens != freeze.Freeze.Transport.MaxOutputTokens || client.HTTP == nil ||
		freeze.Freeze.Transport.Provider != deepSeekProvider {
		return etcdraftAgentB4ArmResult{}, errors.New("ETCDRAFT_B4_CONSUMER_TRANSPORT_DRIFT")
	}
	if prepared.PromptDigest != commitment.PromptDigest || len(prepared.PromptBytes) != commitment.PromptBytes ||
		prepared.RequestDigest != commitment.RequestDigest || len(prepared.RequestBytes) != commitment.RequestBytes {
		return etcdraftAgentB4ArmResult{}, errors.New("ETCDRAFT_B4_CONSUMER_REQUEST_DRIFT")
	}

	call, err := client.invokePrepared(ctx, key, prepared)
	if err != nil {
		return etcdraftAgentB4ArmResult{}, err
	}
	result := etcdraftAgentB4ArmResult{ArmID: armID}
	audit := controlexperiment.AgentInvocationAudit{
		ID:         "etcdraft-preference-ablation-" + armID + "-m5-18b4",
		ViewDigest: freeze.Inputs.View.Digest,
		Provider:   deepSeekProvider, Endpoint: client.Endpoint, Model: client.Model,
		Thinking: "disabled", Temperature: 0, MaxOutputTokens: client.MaxOutputTokens,
		PromptDigest: call.PromptDigest, RequestDigest: call.RequestDigest,
		ResponseDigest: call.ResponseDigest, Response: call.Response,
		DurationMillis: call.DurationMillis,
		Work:           controlexperiment.AgentInvocationWork(controlexperiment.WorkLedger{}, call.Work),
	}
	fail := func(status string, code string, cause error) (etcdraftAgentB4ArmResult, error) {
		if err := sealEtcdraftAgentB4Arm(freeze, &result, audit, status, code); err != nil {
			return result, err
		}
		if cause == nil {
			cause = errors.New(code)
		}
		return result, fmt.Errorf("%s: %w", code, cause)
	}
	if call.FailureCode != "" {
		status := controlexperiment.AgentInvocationResponseRejected
		if call.FailureCode == deepSeekFailureTransport {
			status = controlexperiment.AgentInvocationTransportFailed
		}
		return fail(status, call.FailureCode, nil)
	}

	intent, err := controlexperiment.ParseGuardedTestIntentProposal(call.Content)
	if err != nil {
		return fail(controlexperiment.AgentInvocationIntentRejected, deepSeekFailureIntent, err)
	}
	result.Intent = &intent
	audit.IntentDigest = intent.Digest
	if err := controlexperiment.ValidatePreferenceOnlyProposal(
		freeze.Inputs.View, freeze.Baseline, intent,
	); err != nil {
		return fail(controlexperiment.AgentInvocationCompileRejected, deepSeekFailureBaseline, err)
	}
	plan, err := controlexperiment.CompileGuardedTestIntentV2(
		freeze.Inputs.View, freeze.Inputs.Knowledge, freeze.Inputs.Catalog,
		freeze.Inputs.Qualification.Manifest, freeze.Inputs.Qualification.Qualification, intent,
	)
	if err != nil {
		return fail(controlexperiment.AgentInvocationCompileRejected, deepSeekFailureCompile, err)
	}
	result.Plan = &plan
	audit.PlanDigest = plan.Digest
	audit.CompilerWork = &plan.CompilerWork
	instance, err := controlexperiment.NewIntentExecutionInstance(
		"etcdraft-preference-ablation-"+armID+"-seed-4-m5-18b4",
		plan, freeze.Freeze.FollowUpSeed, freeze.Freeze.ExecutionBudget,
	)
	if err != nil {
		return fail(controlexperiment.AgentInvocationExecutionFailed, deepSeekFailureExecution, err)
	}
	result.Instance = &instance
	report, bundle, err := etcdraftExecution(ctx, plan.Strategy, plan.Decisions, instance.PolicySeed, true)
	if err != nil {
		return fail(controlexperiment.AgentInvocationExecutionFailed, deepSeekFailureExecution, err)
	}
	if err := validateEtcdraftB4ExecutionInstance(instance, report); err != nil {
		return fail(controlexperiment.AgentInvocationExecutionFailed, deepSeekFailureExecution, err)
	}
	if err := bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
		return fail(controlexperiment.AgentInvocationExecutionFailed, deepSeekFailureExecution, err)
	}
	outcome, err := controlexperiment.NewIntentOutcome(
		"etcdraft-preference-ablation-"+armID+"-seed-4-m5-18b4",
		plan, instance, report, bundle,
	)
	if err != nil {
		return fail(controlexperiment.AgentInvocationExecutionFailed, deepSeekFailureExecution, err)
	}
	result.Report, result.Bundle, result.Outcome = &report, &bundle, &outcome
	audit.ReportDigest, audit.BundleDigest = report.Digest, bundle.Digest
	audit.Work = controlexperiment.AgentInvocationWork(report.Work, call.Work)
	if err := sealEtcdraftAgentB4Arm(
		freeze, &result, audit, controlexperiment.AgentInvocationCompleted, "",
	); err != nil {
		return result, err
	}
	return result, nil
}

func sealEtcdraftAgentB4Arm(
	freeze etcdraftAgentB4RequestFreeze,
	result *etcdraftAgentB4ArmResult,
	audit controlexperiment.AgentInvocationAudit,
	status string,
	failureCode string,
) error {
	audit.Status, audit.FailureCode = status, failureCode
	sealed, err := controlexperiment.NewAgentInvocationAudit(audit)
	if err != nil {
		return err
	}
	result.Audit = sealed
	return validateEtcdraftAgentB4ArmResult(freeze, *result)
}

func etcdraftAgentB4ArmMaterial(
	freeze etcdraftAgentB4RequestFreeze,
	armID string,
) (deepSeekPreparedRequest, controlexperiment.AgentRequestCommitment, error) {
	var prepared deepSeekPreparedRequest
	switch armID {
	case controlexperiment.AgentAblationArmNoFeedback:
		prepared = freeze.NoFeedback
	case controlexperiment.AgentAblationArmWithFeedback:
		prepared = freeze.WithFeedback
	default:
		return deepSeekPreparedRequest{}, controlexperiment.AgentRequestCommitment{},
			errors.New("ETCDRAFT_B4_CONSUMER_ARM_UNKNOWN")
	}
	for _, commitment := range freeze.Freeze.Arms {
		if commitment.ArmID == armID {
			return prepared, commitment, nil
		}
	}
	return deepSeekPreparedRequest{}, controlexperiment.AgentRequestCommitment{},
		errors.New("ETCDRAFT_B4_CONSUMER_ARM_UNBOUND")
}

func validateEtcdraftAgentB4ArmResult(
	freeze etcdraftAgentB4RequestFreeze,
	result etcdraftAgentB4ArmResult,
) error {
	if err := freeze.Freeze.Validate(); err != nil {
		return err
	}
	_, commitment, err := etcdraftAgentB4ArmMaterial(freeze, result.ArmID)
	if err != nil {
		return err
	}
	if err := result.Audit.Validate(); err != nil {
		return err
	}
	if result.Audit.ViewDigest != freeze.Inputs.View.Digest ||
		result.Audit.PromptDigest != commitment.PromptDigest ||
		result.Audit.RequestDigest != commitment.RequestDigest ||
		result.Audit.Provider != freeze.Freeze.Transport.Provider ||
		result.Audit.Endpoint != freeze.Freeze.Transport.Endpoint ||
		result.Audit.Model != freeze.Freeze.Transport.Model ||
		result.Audit.MaxOutputTokens != freeze.Freeze.Transport.MaxOutputTokens {
		return errors.New("ETCDRAFT_B4_CONSUMER_AUDIT_DRIFT")
	}
	switch result.Audit.Status {
	case controlexperiment.AgentInvocationTransportFailed:
		if result.Audit.FailureCode != deepSeekFailureTransport {
			return errors.New("ETCDRAFT_B4_CONSUMER_FAILURE_CLASS_INVALID")
		}
	case controlexperiment.AgentInvocationResponseRejected:
		if result.Audit.FailureCode != deepSeekFailureHTTP &&
			result.Audit.FailureCode != deepSeekFailureResponse {
			return errors.New("ETCDRAFT_B4_CONSUMER_FAILURE_CLASS_INVALID")
		}
	case controlexperiment.AgentInvocationIntentRejected:
		if result.Audit.FailureCode != deepSeekFailureIntent {
			return errors.New("ETCDRAFT_B4_CONSUMER_FAILURE_CLASS_INVALID")
		}
	case controlexperiment.AgentInvocationCompileRejected:
		if result.Audit.FailureCode != deepSeekFailureBaseline &&
			result.Audit.FailureCode != deepSeekFailureCompile {
			return errors.New("ETCDRAFT_B4_CONSUMER_FAILURE_CLASS_INVALID")
		}
	case controlexperiment.AgentInvocationExecutionFailed:
		if result.Audit.FailureCode != deepSeekFailureExecution {
			return errors.New("ETCDRAFT_B4_CONSUMER_FAILURE_CLASS_INVALID")
		}
	case controlexperiment.AgentInvocationCompleted:
		if result.Audit.FailureCode != "" {
			return errors.New("ETCDRAFT_B4_CONSUMER_FAILURE_CLASS_INVALID")
		}
	}
	artifactsAfterIntent := result.Plan != nil || result.Instance != nil || result.Report != nil ||
		result.Bundle != nil || result.Outcome != nil
	artifactsAfterPlan := result.Instance != nil || result.Report != nil || result.Bundle != nil || result.Outcome != nil
	switch result.Audit.Status {
	case controlexperiment.AgentInvocationTransportFailed,
		controlexperiment.AgentInvocationResponseRejected,
		controlexperiment.AgentInvocationIntentRejected:
		if result.Intent != nil || artifactsAfterIntent {
			return errors.New("ETCDRAFT_B4_CONSUMER_FAILURE_ARTIFACT_INVALID")
		}
	case controlexperiment.AgentInvocationCompileRejected:
		if result.Intent == nil || artifactsAfterIntent || result.Audit.IntentDigest != result.Intent.Digest {
			return errors.New("ETCDRAFT_B4_CONSUMER_COMPILE_ARTIFACT_INVALID")
		}
	case controlexperiment.AgentInvocationExecutionFailed:
		if result.Intent == nil || result.Plan == nil ||
			result.Report != nil || result.Bundle != nil || result.Outcome != nil {
			return errors.New("ETCDRAFT_B4_CONSUMER_EXECUTION_ARTIFACT_INVALID")
		}
	case controlexperiment.AgentInvocationCompleted:
		if result.Intent == nil || result.Plan == nil || result.Instance == nil ||
			result.Report == nil || result.Bundle == nil || result.Outcome == nil {
			return errors.New("ETCDRAFT_B4_CONSUMER_COMPLETED_ARTIFACT_MISSING")
		}
	default:
		return errors.New("ETCDRAFT_B4_CONSUMER_STATUS_INVALID")
	}
	if result.Intent != nil {
		preferenceErr := controlexperiment.ValidatePreferenceOnlyProposal(
			freeze.Inputs.View, freeze.Baseline, *result.Intent,
		)
		if result.Audit.FailureCode == deepSeekFailureBaseline {
			if preferenceErr == nil {
				return errors.New("ETCDRAFT_B4_CONSUMER_BASELINE_FAILURE_MISMATCH")
			}
		} else if preferenceErr != nil {
			return preferenceErr
		}
		if result.Audit.FailureCode == deepSeekFailureCompile {
			if _, compileErr := controlexperiment.CompileGuardedTestIntentV2(
				freeze.Inputs.View, freeze.Inputs.Knowledge, freeze.Inputs.Catalog,
				freeze.Inputs.Qualification.Manifest, freeze.Inputs.Qualification.Qualification,
				*result.Intent,
			); compileErr == nil {
				return errors.New("ETCDRAFT_B4_CONSUMER_COMPILE_FAILURE_MISMATCH")
			}
		}
	}
	if result.Plan != nil {
		if result.Intent == nil || result.Audit.IntentDigest != result.Intent.Digest ||
			result.Audit.PlanDigest != result.Plan.Digest {
			return errors.New("ETCDRAFT_B4_CONSUMER_PLAN_AUDIT_MISMATCH")
		}
		if err := result.Plan.ValidateInputs(
			freeze.Inputs.View, freeze.Inputs.Knowledge, freeze.Inputs.Catalog,
			freeze.Inputs.Qualification.Manifest, freeze.Inputs.Qualification.Qualification,
			*result.Intent,
		); err != nil {
			return err
		}
	}
	if result.Instance != nil {
		if result.Plan == nil || result.Instance.PolicySeed != freeze.Freeze.FollowUpSeed ||
			result.Instance.Budget != freeze.Freeze.ExecutionBudget {
			return errors.New("ETCDRAFT_B4_CONSUMER_INSTANCE_DRIFT")
		}
		if err := result.Instance.ValidatePlan(*result.Plan); err != nil {
			return err
		}
	}
	if result.Report != nil {
		if artifactsAfterPlan && (result.Bundle == nil || result.Outcome == nil) {
			return errors.New("ETCDRAFT_B4_CONSUMER_OUTCOME_MISSING")
		}
		if err := validateEtcdraftB4ExecutionInstance(*result.Instance, *result.Report); err != nil {
			return err
		}
		if result.Audit.ReportDigest != result.Report.Digest || result.Audit.BundleDigest != result.Bundle.Digest {
			return errors.New("ETCDRAFT_B4_CONSUMER_EXECUTION_AUDIT_MISMATCH")
		}
		if result.Audit.Work.Primary != result.Report.Work.Primary ||
			result.Audit.Work.Replay != result.Report.Work.Replay {
			return errors.New("ETCDRAFT_B4_CONSUMER_EXECUTION_WORK_MISMATCH")
		}
		if freeze.Spec.SourceObservedWork.ExecutionAttempts+1 >
			freeze.Freeze.PerArmBudget.MaxExecutionAttempts ||
			freeze.Spec.SourceObservedWork.PrimaryWorkUnits+result.Report.Work.Primary.WorkUnits >
				freeze.Freeze.PerArmBudget.MaxPrimaryWorkUnits ||
			freeze.Spec.SourceObservedWork.ReplayWorkUnits+result.Report.Work.Replay.WorkUnits >
				freeze.Freeze.PerArmBudget.MaxReplayWorkUnits {
			return errors.New("ETCDRAFT_B4_CONSUMER_BUDGET_EXCEEDED")
		}
		if err := result.Bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
			return err
		}
		if err := result.Outcome.ValidateInputs(
			*result.Plan, *result.Instance, *result.Report, *result.Bundle,
		); err != nil {
			return err
		}
	}
	if result.Audit.Work.Model.Calls != 1 {
		return errors.New("ETCDRAFT_B4_CONSUMER_MODEL_WORK_INVALID")
	}
	return nil
}

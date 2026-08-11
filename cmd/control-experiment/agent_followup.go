package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const etcdraftAgentFollowUpSummaryVersion = "consensus-atlas/etcdraft-agent-follow-up-summary/v1"

type etcdraftAgentFollowUpBaseline struct {
	Batch   etcdraftAgentFeedbackBatch
	Spec    controlexperiment.AgentFollowUpSpec
	Intent  controlexperiment.GuardedTestIntent
	Plan    controlexperiment.CompiledIntentPlan
	Report  controlexperiment.Report
	Bundle  controlexperiment.ExecutionBundle
	Summary etcdraftAgentFollowUpSummary
}

type etcdraftAgentFollowUpSummary struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Status        string `json:"status"`
	FailureCode   string `json:"failure_code,omitempty"`

	SpecDigest     string `json:"spec_digest"`
	FeedbackDigest string `json:"feedback_digest"`
	IntentDigest   string `json:"intent_digest"`
	PlanDigest     string `json:"plan_digest"`
	BackendID      string `json:"backend_id"`
	Strategy       string `json:"strategy"`
	FollowUpSeed   uint64 `json:"follow_up_seed"`
	Decisions      int    `json:"decisions"`

	SourceExecutionAttempts  int `json:"source_execution_attempts"`
	SourcePrimaryWorkUnits   int `json:"source_primary_work_units"`
	SourceReplayWorkUnits    int `json:"source_replay_work_units"`
	FollowUpPrimaryWorkUnits int `json:"follow_up_primary_work_units"`
	FollowUpReplayWorkUnits  int `json:"follow_up_replay_work_units"`
	ChargedExecutionAttempts int `json:"charged_execution_attempts"`
	ChargedPrimaryWorkUnits  int `json:"charged_primary_work_units"`
	ChargedReplayWorkUnits   int `json:"charged_replay_work_units"`
	ModelCalls               int `json:"model_calls"`

	Termination       string `json:"termination"`
	WorkloadCompleted int    `json:"workload_completed"`
	PSSSamples        int    `json:"pss_samples"`
	UniquePSSStates   int    `json:"unique_pss_states"`
	ReportDigest      string `json:"report_digest"`
	BundleDigest      string `json:"bundle_digest"`
	Digest            string `json:"digest"`
}

func newEtcdraftAgentFollowUpBaseline(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
) (etcdraftAgentFollowUpBaseline, error) {
	batch, err := newEtcdraftComparableAgentFeedbackBatch(ctx, decisions, baseSeed)
	if err != nil {
		return etcdraftAgentFollowUpBaseline{}, err
	}
	return newEtcdraftAgentFollowUpBaselineFromBatch(ctx, decisions, baseSeed, batch)
}

func newEtcdraftAgentFollowUpBaselineFromBatch(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	batch etcdraftAgentFeedbackBatch,
) (etcdraftAgentFollowUpBaseline, error) {
	if baseSeed > ^uint64(0)-3 {
		return etcdraftAgentFollowUpBaseline{}, errors.New("ETCDRAFT_FOLLOW_UP_SEED_OVERFLOW")
	}
	sourceSeeds := []uint64{baseSeed, baseSeed + 1, baseSeed + 2}
	followUpSeed := baseSeed + 3
	spec, err := controlexperiment.NewAgentFollowUpSpec(
		"etcdraft-agent-feedback-ablation-m5-18b3",
		batch.IntentInputs, batch.Feedback, batch.Sources,
		sourceSeeds, followUpSeed, decisions,
		controlexperiment.MethodBudget{
			MaxExecutionAttempts: 1,
			MaxPrimaryWorkUnits:  decisions + 2,
			MaxReplayWorkUnits:   decisions + 2,
		},
		1, deepSeekDefaultTokens,
	)
	if err != nil {
		return etcdraftAgentFollowUpBaseline{}, err
	}
	intent, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID:         "etcdraft-deterministic-feedback-baseline-m5-18b3",
		ViewDigest: batch.IntentInputs.Digest, RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{
			Decisions: decisions,
			FaultEnvelope: controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
			},
		},
		Prefer: controlexperiment.IntentPrefer{BackendIDs: []string{spec.DeterministicBackendID}},
	})
	if err != nil {
		return etcdraftAgentFollowUpBaseline{}, err
	}
	inputs, err := newEtcdraftFeedbackIntentInputs(ctx)
	if err != nil {
		return etcdraftAgentFollowUpBaseline{}, err
	}
	if inputs.View.Digest != batch.IntentInputs.Digest {
		return etcdraftAgentFollowUpBaseline{}, errors.New("ETCDRAFT_FOLLOW_UP_VIEW_MISMATCH")
	}
	plan, err := controlexperiment.CompileGuardedTestIntent(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	)
	if err != nil {
		return etcdraftAgentFollowUpBaseline{}, err
	}
	if plan.BackendID != spec.DeterministicBackendID {
		return etcdraftAgentFollowUpBaseline{}, errors.New("ETCDRAFT_FOLLOW_UP_BASELINE_SELECTION_MISMATCH")
	}
	report, bundle, executionErr := executeEtcdraftAgentFollowUp(
		ctx, inputs, batch.Feedback, batch.Sources, spec, intent, plan,
	)
	failureCode := ""
	if executionErr != nil {
		if report.Digest == "" || bundle.Digest == "" ||
			executionErr.Error() != "EXPERIMENT_COMPILED_INTENT_HARD_ACTION_MISSING" {
			return etcdraftAgentFollowUpBaseline{}, executionErr
		}
		failureCode = executionErr.Error()
	}
	summary, err := newEtcdraftAgentFollowUpSummary(
		spec, intent, plan, report, bundle, failureCode,
	)
	if err != nil {
		return etcdraftAgentFollowUpBaseline{}, err
	}
	return etcdraftAgentFollowUpBaseline{
		Batch: batch, Spec: spec, Intent: intent, Plan: plan,
		Report: report, Bundle: bundle, Summary: summary,
	}, nil
}

func executeEtcdraftAgentFollowUp(
	ctx context.Context,
	inputs etcdraftIntentInputs,
	feedback controlexperiment.AgentBatchFeedbackView,
	sources []controlexperiment.AgentBatchFeedbackInput,
	spec controlexperiment.AgentFollowUpSpec,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlan,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	if err := spec.ValidateInputs(inputs.View, feedback, sources); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	if err := plan.ValidateInputs(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	if err := spec.ValidatePlan(intent, plan); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	report, bundle, err := etcdraftExecution(
		ctx, plan.Strategy, plan.Decisions, spec.FollowUpSeed, true,
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	if len(report.Config.Runs) != 1 || report.Config.Runs[0].Policy.SeedHex !=
		randomPolicySeed(spec.FollowUpSeed, 1) {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			errors.New("ETCDRAFT_FOLLOW_UP_SEED_MISMATCH")
	}
	if err := bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	if err := plan.ValidateExecution(report, bundle); err != nil {
		return report, bundle, err
	}
	return report, bundle, nil
}

func newEtcdraftAgentFollowUpSummary(
	spec controlexperiment.AgentFollowUpSpec,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlan,
	report controlexperiment.Report,
	bundle controlexperiment.ExecutionBundle,
	failureCode string,
) (etcdraftAgentFollowUpSummary, error) {
	if err := spec.ValidatePlan(intent, plan); err != nil {
		return etcdraftAgentFollowUpSummary{}, err
	}
	executionErr := plan.ValidateExecution(report, bundle)
	status := "completed"
	if failureCode == "" {
		if executionErr != nil {
			return etcdraftAgentFollowUpSummary{}, executionErr
		}
	} else {
		if executionErr == nil || executionErr.Error() != failureCode ||
			failureCode != "EXPERIMENT_COMPILED_INTENT_HARD_ACTION_MISSING" {
			return etcdraftAgentFollowUpSummary{}, errors.New("ETCDRAFT_FOLLOW_UP_FAILURE_MISMATCH")
		}
		status = "execution-failed"
	}
	if len(report.Runs) != 1 || report.Runs[0].Workload == nil {
		return etcdraftAgentFollowUpSummary{}, errors.New("ETCDRAFT_FOLLOW_UP_REPORT_INVALID")
	}
	summary := etcdraftAgentFollowUpSummary{
		SchemaVersion: etcdraftAgentFollowUpSummaryVersion,
		ID:            "etcdraft-deterministic-feedback-follow-up-m5-18b3",
		Status:        status,
		FailureCode:   failureCode,
		SpecDigest:    spec.Digest, FeedbackDigest: spec.FeedbackDigest,
		IntentDigest: intent.Digest, PlanDigest: plan.Digest,
		BackendID: plan.BackendID, Strategy: plan.Strategy,
		FollowUpSeed: spec.FollowUpSeed, Decisions: plan.Decisions,
		SourceExecutionAttempts:  spec.SourceObservedWork.ExecutionAttempts,
		SourcePrimaryWorkUnits:   spec.SourceObservedWork.PrimaryWorkUnits,
		SourceReplayWorkUnits:    spec.SourceObservedWork.ReplayWorkUnits,
		FollowUpPrimaryWorkUnits: report.Work.Primary.WorkUnits,
		FollowUpReplayWorkUnits:  report.Work.Replay.WorkUnits,
		ChargedExecutionAttempts: spec.SourceObservedWork.ExecutionAttempts + 1,
		ChargedPrimaryWorkUnits:  spec.SourceObservedWork.PrimaryWorkUnits + report.Work.Primary.WorkUnits,
		ChargedReplayWorkUnits:   spec.SourceObservedWork.ReplayWorkUnits + report.Work.Replay.WorkUnits,
		ModelCalls:               0, Termination: report.Runs[0].Termination,
		WorkloadCompleted: report.Runs[0].Workload.Completed,
		PSSSamples:        report.Runs[0].CorePSSSamples,
		UniquePSSStates:   report.Runs[0].UniqueCoreStates,
		ReportDigest:      report.Digest, BundleDigest: bundle.Digest,
	}
	digest, err := control.CanonicalDigest(summary)
	if err != nil {
		return etcdraftAgentFollowUpSummary{}, err
	}
	summary.Digest = digest
	return summary, nil
}

func persistEtcdraftAgentFollowUpBaseline(
	out string,
	artifactDir string,
	result etcdraftAgentFollowUpBaseline,
	stdout io.Writer,
) error {
	want, err := newEtcdraftAgentFollowUpSummary(
		result.Spec, result.Intent, result.Plan, result.Report, result.Bundle,
		result.Summary.FailureCode,
	)
	if err != nil || want.Digest != result.Summary.Digest {
		return errors.New("ETCDRAFT_FOLLOW_UP_SUMMARY_MISMATCH")
	}
	artifacts := []struct {
		name  string
		value any
	}{
		{name: "feedback.json", value: result.Batch.Feedback},
		{name: "spec.json", value: result.Spec},
		{name: "intent.json", value: result.Intent},
		{name: "plan.json", value: result.Plan},
		{name: "report.json", value: result.Report},
		{name: "bundle.json", value: result.Bundle},
	}
	for _, artifact := range artifacts {
		encoded, err := json.MarshalIndent(artifact.value, "", "  ")
		if err != nil {
			return err
		}
		if err := writeReport(filepath.Join(artifactDir, artifact.name), encoded); err != nil {
			return err
		}
	}
	encoded, err := json.MarshalIndent(result.Summary, "", "  ")
	if err != nil {
		return err
	}
	if err := writeReport(out, encoded); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\nbackend=%s seed=%d states=%d charged=%d/%d digest=%s\n",
		out, result.Summary.BackendID, result.Summary.FollowUpSeed,
		result.Summary.UniquePSSStates, result.Summary.ChargedPrimaryWorkUnits,
		result.Summary.ChargedReplayWorkUnits, result.Summary.Digest)
	return nil
}

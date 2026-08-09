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

const etcdraftAgentB4PreflightSummaryVersion = "consensus-atlas/etcdraft-agent-b4-preflight-summary/v1"

type etcdraftAgentB4Preflight struct {
	Inputs   etcdraftIntentInputs
	Batch    etcdraftAgentFeedbackBatch
	Intent   controlexperiment.GuardedTestIntent
	Plan     controlexperiment.CompiledIntentPlanV2
	Instance controlexperiment.IntentExecutionInstance
	Report   controlexperiment.Report
	Bundle   controlexperiment.ExecutionBundle
	Outcome  controlexperiment.IntentOutcome
	Summary  etcdraftAgentB4PreflightSummary
}

type etcdraftAgentB4PreflightSummary struct {
	SchemaVersion  string `json:"schema_version"`
	ID             string `json:"id"`
	Classification string `json:"classification"`

	ViewDigest     string `json:"view_digest"`
	CatalogDigest  string `json:"catalog_digest"`
	FeedbackDigest string `json:"feedback_digest"`
	IntentDigest   string `json:"intent_digest"`
	PlanDigest     string `json:"plan_digest"`
	InstanceDigest string `json:"execution_instance_digest"`
	OutcomeDigest  string `json:"intent_outcome_digest"`

	BackendID       string               `json:"backend_id"`
	Strategy        string               `json:"strategy"`
	PolicySeed      uint64               `json:"policy_seed"`
	Decisions       int                  `json:"decisions"`
	ExecutionStatus string               `json:"execution_status"`
	IntentStatus    string               `json:"intent_status"`
	ReasonCode      string               `json:"reason_code,omitempty"`
	OracleStatus    string               `json:"oracle_status"`
	MissingActions  []control.ActionKind `json:"missing_actions,omitempty"`

	PrimaryWorkUnits int    `json:"primary_work_units"`
	ReplayWorkUnits  int    `json:"replay_work_units"`
	ModelCalls       int    `json:"model_calls"`
	ReportDigest     string `json:"report_digest"`
	BundleDigest     string `json:"bundle_digest"`
	Digest           string `json:"digest"`
}

func newEtcdraftAgentB4Preflight(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
) (etcdraftAgentB4Preflight, error) {
	if baseSeed > ^uint64(0)-3 {
		return etcdraftAgentB4Preflight{}, errors.New("ETCDRAFT_B4_PREFLIGHT_SEED_OVERFLOW")
	}
	actionClass, err := etcdraftActionClassMethodV2(ctx, decisions, baseSeed)
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	uniform, err := etcdraftAdmissibleUniformMethod(ctx, decisions, baseSeed)
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	return newEtcdraftAgentB4PreflightFromMethods(ctx, decisions, baseSeed, actionClass, uniform)
}

func newEtcdraftAgentB4PreflightFromMethods(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	actionClass etcdraftMethodExecution,
	uniform etcdraftMethodExecution,
) (etcdraftAgentB4Preflight, error) {
	if baseSeed > ^uint64(0)-3 {
		return etcdraftAgentB4Preflight{}, errors.New("ETCDRAFT_B4_PREFLIGHT_SEED_OVERFLOW")
	}
	inputs, err := newEtcdraftB4IntentInputs(ctx)
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	return newEtcdraftAgentB4PreflightFromInputsAndMethods(
		ctx, decisions, baseSeed, inputs, actionClass, uniform,
	)
}

func newEtcdraftAgentB4PreflightFromInputsAndMethods(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	inputs etcdraftIntentInputs,
	actionClass etcdraftMethodExecution,
	uniform etcdraftMethodExecution,
) (etcdraftAgentB4Preflight, error) {
	if baseSeed > ^uint64(0)-3 {
		return etcdraftAgentB4Preflight{}, errors.New("ETCDRAFT_B4_PREFLIGHT_SEED_OVERFLOW")
	}
	batch, err := newEtcdraftAgentFeedbackBatchFromInputsWithID(
		inputs, actionClass, uniform, true, "etcdraft-agent-feedback-m5-18b4-pre",
	)
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	intent, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID:         "etcdraft-feedback-ablation-baseline-m5-18b4-pre",
		ViewDigest: inputs.View.Digest, RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{
			Decisions: decisions,
			FaultEnvelope: controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1,
			},
		},
		Prefer: controlexperiment.IntentPrefer{BackendIDs: []string{etcdraftBackendActionClass}},
	})
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	plan, err := controlexperiment.CompileGuardedTestIntentV2(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	)
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	instance, err := controlexperiment.NewIntentExecutionInstance(
		"etcdraft-feedback-ablation-seed-4-m5-18b4-pre", plan, baseSeed+3,
		controlexperiment.MethodBudget{
			MaxExecutionAttempts: 1,
			MaxPrimaryWorkUnits:  decisions + 2,
			MaxReplayWorkUnits:   decisions + 2,
		},
	)
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	report, bundle, err := etcdraftExecution(
		ctx, plan.Strategy, plan.Decisions, instance.PolicySeed, true,
	)
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	if err := validateEtcdraftB4ExecutionInstance(instance, report); err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	if err := bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	outcome, err := controlexperiment.NewIntentOutcome(
		"etcdraft-feedback-ablation-seed-4-m5-18b4-pre", plan, instance, report, bundle,
	)
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	summary, err := newEtcdraftAgentB4PreflightSummary(
		inputs, batch, intent, plan, instance, report, bundle, outcome,
	)
	if err != nil {
		return etcdraftAgentB4Preflight{}, err
	}
	return etcdraftAgentB4Preflight{
		Inputs: inputs, Batch: batch, Intent: intent, Plan: plan, Instance: instance,
		Report: report, Bundle: bundle, Outcome: outcome, Summary: summary,
	}, nil
}

func newEtcdraftAgentB4PreflightSummary(
	inputs etcdraftIntentInputs,
	batch etcdraftAgentFeedbackBatch,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlanV2,
	instance controlexperiment.IntentExecutionInstance,
	report controlexperiment.Report,
	bundle controlexperiment.ExecutionBundle,
	outcome controlexperiment.IntentOutcome,
) (etcdraftAgentB4PreflightSummary, error) {
	if err := plan.ValidateInputs(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	); err != nil {
		return etcdraftAgentB4PreflightSummary{}, err
	}
	if err := outcome.ValidateInputs(plan, instance, report, bundle); err != nil {
		return etcdraftAgentB4PreflightSummary{}, err
	}
	if err := validateEtcdraftB4ExecutionInstance(instance, report); err != nil {
		return etcdraftAgentB4PreflightSummary{}, err
	}
	if err := batch.Feedback.ValidateInputs(inputs.View, batch.Sources); err != nil {
		return etcdraftAgentB4PreflightSummary{}, err
	}
	summary := etcdraftAgentB4PreflightSummary{
		SchemaVersion:  etcdraftAgentB4PreflightSummaryVersion,
		ID:             "etcdraft-agent-b4-preflight-m5-18b4-pre",
		Classification: "public-trust-boundary-calibration-only",
		ViewDigest:     inputs.View.Digest, CatalogDigest: inputs.Catalog.Digest,
		FeedbackDigest: batch.Feedback.Digest, IntentDigest: intent.Digest,
		PlanDigest: plan.Digest, InstanceDigest: instance.Digest, OutcomeDigest: outcome.Digest,
		BackendID: plan.BackendID, Strategy: plan.Strategy, PolicySeed: instance.PolicySeed,
		Decisions: plan.Decisions, ExecutionStatus: outcome.ExecutionStatus,
		IntentStatus: outcome.IntentStatus, ReasonCode: outcome.ReasonCode,
		OracleStatus:     outcome.OracleStatus,
		MissingActions:   append([]control.ActionKind(nil), outcome.MissingActions...),
		PrimaryWorkUnits: report.Work.Primary.WorkUnits,
		ReplayWorkUnits:  report.Work.Replay.WorkUnits, ModelCalls: 0,
		ReportDigest: report.Digest, BundleDigest: bundle.Digest,
	}
	digest, err := control.CanonicalDigest(summary)
	if err != nil {
		return etcdraftAgentB4PreflightSummary{}, err
	}
	summary.Digest = digest
	return summary, nil
}

func validateEtcdraftB4ExecutionInstance(
	instance controlexperiment.IntentExecutionInstance,
	report controlexperiment.Report,
) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	if err := report.Validate(); err != nil {
		return err
	}
	if len(report.Config.Runs) != 1 ||
		report.Config.Runs[0].Policy.SeedHex != randomPolicySeed(instance.PolicySeed, 1) {
		return errors.New("ETCDRAFT_B4_PREFLIGHT_SEED_MISMATCH")
	}
	return nil
}

func persistEtcdraftAgentB4Preflight(
	out string,
	artifactDir string,
	result etcdraftAgentB4Preflight,
	stdout io.Writer,
) error {
	want, err := newEtcdraftAgentB4PreflightSummary(
		result.Inputs, result.Batch, result.Intent, result.Plan, result.Instance,
		result.Report, result.Bundle, result.Outcome,
	)
	if err != nil || want.Digest != result.Summary.Digest {
		return errors.New("ETCDRAFT_B4_PREFLIGHT_SUMMARY_MISMATCH")
	}
	artifacts := []struct {
		name  string
		value any
	}{
		{name: "view.json", value: result.Inputs.View},
		{name: "feedback.json", value: result.Batch.Feedback},
		{name: "intent.json", value: result.Intent},
		{name: "plan.json", value: result.Plan},
		{name: "execution-instance.json", value: result.Instance},
		{name: "intent-outcome.json", value: result.Outcome},
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
	fmt.Fprintf(stdout, "wrote %s\nexecution=%s intent=%s seed=%d states=%d digest=%s\n",
		out, result.Summary.ExecutionStatus, result.Summary.IntentStatus,
		result.Summary.PolicySeed, result.Report.StateDiscovery.UniqueStates, result.Summary.Digest)
	return nil
}

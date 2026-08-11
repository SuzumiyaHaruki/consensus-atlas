package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

const m521qExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-risk-frontier-authority-m5.21q"

func TestEtcdraftM521qRiskFrontierChoicesChangeEffectiveExecution(t *testing.T) {
	ctx := context.Background()
	_, source, err := etcdraftExecution(ctx, "workload-risk-witness-calibration", 64, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	envelope := &controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	view28, work28 := etcdraftRiskFrontierAt(
		t, ctx, "after-invoke", spec, source, 28, envelope, factory,
	)
	view53, work53 := etcdraftRiskFrontierAt(
		t, ctx, "after-coordinator-change", spec, source, 53, envelope, factory,
	)
	crash, err := controlexperiment.ChooseFirstFrontierAction(
		"choose-crash-old-coordinator", view28, spec,
		[]controlexperiment.FrontierActionSelector{{Kind: control.ActionCrash, Node: "n1"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := controlexperiment.ChooseFirstFrontierAction(
		"choose-progress-without-fault", view28, spec,
		[]controlexperiment.FrontierActionSelector{
			{Kind: control.ActionCompleteEffect},
			{Kind: control.ActionDeliverMessage},
			{Kind: control.ActionFireTemporal},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	restart, err := controlexperiment.ChooseFirstFrontierAction(
		"choose-restart-old-coordinator", view53, spec,
		[]controlexperiment.FrontierActionSelector{{Kind: control.ActionRestart, Node: "n1"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	fallback := []control.ActionKind{
		control.ActionInvoke, control.ActionCompleteEffect,
		control.ActionDeliverMessage, control.ActionFireTemporal,
	}
	riskPolicy, err := controlexperiment.CompileFrontierChoices(
		"risk-frontier-exact-actions-v1", spec,
		[]controlexperiment.RiskFrontierView{view28, view53},
		[]controlexperiment.FrontierChoice{crash, restart}, fallback, 64,
	)
	if err != nil {
		t.Fatal(err)
	}
	controlPolicy, err := controlexperiment.CompileFrontierChoices(
		"progress-frontier-exact-action-v1", spec,
		[]controlexperiment.RiskFrontierView{view28},
		[]controlexperiment.FrontierChoice{progress}, fallback, 64,
	)
	if err != nil {
		t.Fatal(err)
	}
	riskReport, riskBundle := executeEtcdraftFrontierPolicy(t, ctx, "risk", riskPolicy, envelope)
	controlReport, controlBundle := executeEtcdraftFrontierPolicy(t, ctx, "control", controlPolicy, envelope)
	riskWitness, err := newEtcdraftLeaderChangeRiskWitness("m521q-risk", riskBundle)
	if err != nil {
		t.Fatal(err)
	}
	controlWitness, err := newEtcdraftLeaderChangeRiskWitness("m521q-control", controlBundle)
	if err != nil {
		t.Fatal(err)
	}
	if riskBundle.Trace.Digest != source.Trace.Digest ||
		riskWitness.Status != semantic.RiskWitnessReached ||
		controlWitness.Status != semantic.RiskWitnessNotReached ||
		riskBundle.Trace.Digest == controlBundle.Trace.Digest ||
		riskReport.ConfigDigest == controlReport.ConfigDigest ||
		riskReport.Runs[0].PolicyDigest == controlReport.Runs[0].PolicyDigest {
		t.Fatalf("frontier choices did not produce the required behavior delta: risk=%#v control=%#v", riskWitness, controlWitness)
	}
	gate := m521qAuthorityGate{
		SchemaVersion:      "consensus-atlas/risk-frontier-authority-gate/v1",
		SourceBundleDigest: source.Digest,
		SourceTraceDigest:  source.Trace.Digest,
		Views: []m521qViewSummary{
			m521qSummarizeView(view28, work28),
			m521qSummarizeView(view53, work53),
		},
		Choices: []m521qChoiceSummary{
			m521qSummarizeChoice(crash),
			m521qSummarizeChoice(progress),
			m521qSummarizeChoice(restart),
		},
		Executions: []m521qExecutionSummary{
			m521qSummarizeExecution("risk-directed", riskReport, riskBundle, riskWitness),
			m521qSummarizeExecution("progress-control", controlReport, controlBundle, controlWitness),
		},
		PolicyIdentityDifferent: true,
		TraceBehaviorDifferent:  true,
		RiskOutcomeDifferent:    true,
		NewModelCalls:           0,
		NewSUTExecutions:        2,
		Classification: "no-model-exact-frontier-authority-changes-real-execution-" +
			"not-agent-effect-or-method-ranking",
	}
	checked := readM521hJSONFile[m521qAuthorityGate](
		t, m521qExperimentDirectory+"/summary.json", 96<<10,
	)
	if !reflect.DeepEqual(checked, gate) {
		t.Fatalf("checked-in M5.21q authority gate drifted:\n got: %#v\nwant: %#v", checked, gate)
	}
}

func etcdraftRiskFrontierAt(
	t *testing.T,
	ctx context.Context,
	id string,
	spec semantic.RiskWitnessSpec,
	bundle controlexperiment.ExecutionBundle,
	completed int,
	envelope *controlexperiment.FaultEnvelope,
	factory controlexperiment.AdapterFactory,
) (controlexperiment.RiskFrontierView, controlexperiment.PhaseWork) {
	t.Helper()
	prefix, err := controlexperiment.ExecutionTracePrefix(bundle.Trace, completed)
	if err != nil {
		t.Fatal(err)
	}
	milestones, err := projectEtcdraftLeaderChangeRiskMilestones(prefix, nil, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := semantic.NewRiskWitnessResult(
		id+"-result", spec, bundle.Identity.ManifestDigest, prefix.Digest,
		etcdraftLeaderChangeRiskWitnessProjectorID, milestones,
	)
	if err != nil {
		t.Fatal(err)
	}
	view, work, err := controlexperiment.ReconstructRiskFrontierView(
		ctx, id, spec, result, bundle.Trace, completed, etcdraftCampaignRuntimeConfig(), envelope, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	return view, work
}

func executeEtcdraftFrontierPolicy(
	t *testing.T,
	ctx context.Context,
	id string,
	policy controlexperiment.Policy,
	envelope *controlexperiment.FaultEnvelope,
) (controlexperiment.Report, controlexperiment.ExecutionBundle) {
	t.Helper()
	qualification, admission, workload, err := etcdraftQualifiedWorkload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config := controlexperiment.Config{
		SchemaVersion:    controlexperiment.SchemaVersionV2,
		ID:               "public-etcdraft-v2-risk-frontier-authority-m5.21q-" + id,
		PSSID:            etcdraftv2.CorePSSMappingID,
		Runtime:          etcdraftCampaignRuntimeConfig(),
		Admission:        &admission,
		FaultEnvelope:    envelope,
		WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		DecisionsPerRun:  64,
		RequireReplay:    true,
		Runs:             []controlexperiment.RunPlan{{Run: 1, Policy: policy, Workload: &workload}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	report, bundle, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return report, bundle
}

type m521qViewSummary struct {
	ID                    string                       `json:"id"`
	Digest                string                       `json:"digest"`
	PrefixDecisions       int                          `json:"prefix_decisions"`
	NextDecision          int                          `json:"next_decision"`
	Progress              semantic.RiskWitnessProgress `json:"progress"`
	RuntimeActionCount    int                          `json:"runtime_action_count"`
	AdmissibleActionCount int                          `json:"admissible_action_count"`
	ReconstructionWork    controlexperiment.PhaseWork  `json:"reconstruction_work"`
}

type m521qRiskSummary struct {
	Status              string   `json:"status"`
	SatisfiedMilestones []string `json:"satisfied_milestones"`
	FirstMissing        string   `json:"first_missing,omitempty"`
	Digest              string   `json:"digest"`
}

type m521qExecutionSummary struct {
	ID              string                              `json:"id"`
	PolicyDigest    string                              `json:"policy_digest"`
	ConfigDigest    string                              `json:"config_digest"`
	ReportDigest    string                              `json:"report_digest"`
	BundleDigest    string                              `json:"bundle_digest"`
	TraceDigest     string                              `json:"trace_digest"`
	Work            controlexperiment.WorkLedger        `json:"work"`
	Workload        controlexperiment.WorkloadRunReport `json:"workload"`
	UniquePSSStates int                                 `json:"unique_pss_states"`
	Risk            m521qRiskSummary                    `json:"risk"`
}

type m521qChoiceSummary struct {
	ID           string             `json:"id"`
	ViewDigest   string             `json:"view_digest"`
	Decision     int                `json:"decision"`
	ActionID     control.ActionID   `json:"action_id"`
	ActionDigest string             `json:"action_digest"`
	Kind         control.ActionKind `json:"kind"`
	Node         control.NodeRef    `json:"node_ref,omitempty"`
	Digest       string             `json:"choice_digest"`
}

type m521qAuthorityGate struct {
	SchemaVersion           string                  `json:"schema_version"`
	SourceBundleDigest      string                  `json:"source_bundle_digest"`
	SourceTraceDigest       string                  `json:"source_trace_digest"`
	Views                   []m521qViewSummary      `json:"views"`
	Choices                 []m521qChoiceSummary    `json:"choices"`
	Executions              []m521qExecutionSummary `json:"executions"`
	PolicyIdentityDifferent bool                    `json:"policy_identity_different"`
	TraceBehaviorDifferent  bool                    `json:"trace_behavior_different"`
	RiskOutcomeDifferent    bool                    `json:"risk_outcome_different"`
	NewModelCalls           int                     `json:"new_model_calls"`
	NewSUTExecutions        int                     `json:"new_sut_executions"`
	Classification          string                  `json:"classification"`
}

func m521qSummarizeChoice(choice controlexperiment.FrontierChoice) m521qChoiceSummary {
	return m521qChoiceSummary{
		ID: choice.ID, ViewDigest: choice.ViewDigest, Decision: choice.Decision,
		ActionID: choice.Action.ActionID, ActionDigest: choice.Action.ActionDigest,
		Kind: choice.Action.Kind, Node: choice.Action.Node, Digest: choice.Digest,
	}
}

func m521qSummarizeView(
	view controlexperiment.RiskFrontierView,
	work controlexperiment.PhaseWork,
) m521qViewSummary {
	return m521qViewSummary{
		ID: view.ID, Digest: view.Digest,
		PrefixDecisions: view.PrefixDecisions, NextDecision: view.NextDecision,
		Progress: view.Progress, RuntimeActionCount: view.RuntimeActionCount,
		AdmissibleActionCount: len(view.Actions), ReconstructionWork: work,
	}
}

func m521qSummarizeExecution(
	id string,
	report controlexperiment.Report,
	bundle controlexperiment.ExecutionBundle,
	witness semantic.RiskWitnessResult,
) m521qExecutionSummary {
	firstMissing := ""
	if len(witness.MissingMilestones) > 0 {
		firstMissing = witness.MissingMilestones[0]
	}
	return m521qExecutionSummary{
		ID: id, PolicyDigest: report.Runs[0].PolicyDigest,
		ConfigDigest: report.ConfigDigest, ReportDigest: report.Digest,
		BundleDigest: bundle.Digest, TraceDigest: bundle.Trace.Digest,
		Work: report.Work, Workload: *report.Runs[0].Workload,
		UniquePSSStates: report.StateDiscovery.UniqueStates,
		Risk: m521qRiskSummary{
			Status: witness.Status, SatisfiedMilestones: append([]string(nil), witness.SatisfiedMilestones...),
			FirstMissing: firstMissing, Digest: witness.Digest,
		},
	}
}

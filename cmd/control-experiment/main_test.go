package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

type traceMutationSummaryWork struct {
	Primary controlexperiment.PhaseWork `json:"primary"`
	Replay  controlexperiment.PhaseWork `json:"replay"`
}

type traceMutationExecutionSummary struct {
	PlanDigest       string                   `json:"plan_digest,omitempty"`
	FirstDecision    int                      `json:"first_decision,omitempty"`
	ReportDigest     string                   `json:"report_digest"`
	BundleDigest     string                   `json:"bundle_digest"`
	ConfigDigest     string                   `json:"config_digest"`
	TraceDigest      string                   `json:"trace_digest"`
	UniqueCoreStates int                      `json:"unique_core_states,omitempty"`
	PrefixArea       int64                    `json:"prefix_area,omitempty"`
	Work             traceMutationSummaryWork `json:"work"`
}

type traceMutationCheckedSummary struct {
	SchemaVersion  string                        `json:"schema_version"`
	ID             string                        `json:"id"`
	Classification string                        `json:"classification"`
	SelectionRule  string                        `json:"selection_rule"`
	Source         traceMutationExecutionSummary `json:"source"`
	Completed      traceMutationExecutionSummary `json:"completed_mutation"`
	Rejected       struct {
		Purpose        string                   `json:"purpose"`
		PlanDigest     string                   `json:"plan_digest"`
		FirstDecision  int                      `json:"first_decision"`
		Phase          string                   `json:"phase"`
		ReasonCode     string                   `json:"reason_code"`
		FailedDecision int                      `json:"failed_decision"`
		Work           traceMutationSummaryWork `json:"work"`
	} `json:"rejected_calibration"`
	Accounting struct {
		Baseline traceMutationSummaryWork `json:"baseline_method"`
		Rejected traceMutationSummaryWork `json:"rejected_calibration"`
		Total    traceMutationSummaryWork `json:"all_observed"`
	} `json:"accounting"`
}

func TestEtcdraftExecutionBundleClosesTrustedV2Boundary(t *testing.T) {
	report, bundle := sharedEtcdraftWorkloadBundleFixture(t)
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
		t.Fatal(err)
	}
	if bundle.Identity.ReportDigest != report.Digest || len(bundle.Preparations) != 1 ||
		len(bundle.Decisions.Observations) == 0 {
		t.Fatalf("incomplete execution bundle: %#v", bundle.Identity)
	}
	checked := oracle.CheckBundle(bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{})
	if len(checked.Violations) != 0 {
		t.Fatalf("official bundle violations: %#v", checked.Violations)
	}

	manifest, err := (defectbench.BundleBenchmark{
		ID: "bundle-smoke", Classification: "public-calibration-only",
		ProjectorID: etcdraftv2.DecisionProjectionID,
		Budget:      defectbench.BundleBudget{MaxDecisions: 96, MaxPrimaryWorkUnits: 98},
		Variants: []defectbench.BundleVariant{
			{TrialID: "control", VariantID: "official", Kind: defectbench.BundleKindControl, ExpectedBuildID: bundle.Qualification.Manifest.BuildID},
			{TrialID: "candidate", VariantID: "same-source-smoke", Kind: defectbench.BundleKindCalibration, RootCauseID: "none-smoke", ExpectedBuildID: bundle.Qualification.Manifest.BuildID},
		},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := defectbench.EvaluateBundles(manifest, map[string]controlexperiment.ExecutionBundle{
		"control": bundle, "candidate": bundle,
	}, etcdraftv2.DecisionProjector{})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Results[0].Status != defectbench.BundleStatusSurvived &&
		evaluation.Results[0].Status != defectbench.BundleStatusControlPass {
		t.Fatalf("unexpected first status: %s", evaluation.Results[0].Status)
	}
	if evaluation.Summary.Controls != 1 || evaluation.Summary.Candidates != 1 ||
		evaluation.Summary.KilledCandidates != 0 || evaluation.Summary.FalsePositives != 0 {
		t.Fatalf("unexpected smoke evaluation: %#v", evaluation.Summary)
	}

	tampered := bundle
	tampered.Preparations = append([]controlexperiment.PreparationRecord(nil), bundle.Preparations...)
	tampered.Preparations[0].AfterStateDigest = strings.Repeat("0", 64)
	violations := oracle.CheckBundle(tampered, oracle.BundleTraceIntegrity{}).Violations
	if len(violations) != 1 || violations[0].Monitor != "trace-integrity" {
		t.Fatalf("tampered preparation was not rejected: %#v", violations)
	}

	tamperedWork := bundle
	tamperedWork.Work.Primary.WorkUnits--
	if err := tamperedWork.Validate(); err == nil || !strings.Contains(err.Error(), "EXECUTION_BUNDLE_WORK_MISMATCH") {
		t.Fatalf("tampered work ledger error = %v", err)
	}
	tamperedRun := bundle
	tamperedRun.Run.FinalStateDigest = strings.Repeat("0", 64)
	if err := tamperedRun.Validate(); err == nil || !strings.Contains(err.Error(), "EXECUTION_BUNDLE_RUN_TRACE_MISMATCH") {
		t.Fatalf("tampered run binding error = %v", err)
	}
}

func TestEtcdraftActionClassRandomRunsBoundedFaultWorkloadAndReplays(t *testing.T) {
	report, bundle := sharedEtcdraftActionClassBundleFixture(t)
	if report.Digest != "fc0cb500876b1d3d75dc7d8f83dd672513d1c010c523069f12e3be2f6472260a" ||
		bundle.Digest != "b766e3f13e8be015b950032df449762b2ee90e3c80d87e87383c8ff80ac6c9b1" ||
		report.StateDiscovery.UniqueStates != 83 || report.StateDiscovery.PrefixArea != 3681 {
		t.Fatalf("action-class baseline identity changed: report=%s bundle=%s discovery=%#v",
			report.Digest, bundle.Digest, report.StateDiscovery)
	}
	run := report.Runs[0]
	wantFaults := controlexperiment.FaultUsage{
		Crashes: 1, MessageDrops: 2, MessageDuplicates: 1,
	}
	if run.PolicyID != "action-class-random-v1/run-1" || run.Workload == nil ||
		run.Workload.Completed != 1 || run.Faults == nil || *run.Faults != wantFaults ||
		!run.Replay.Stable || report.Work.Primary.WorkUnits != 98 || report.Work.Replay.WorkUnits != 98 {
		t.Fatalf("unexpected action-class baseline: %#v work=%#v", run, report.Work)
	}
	counts := make(map[control.ActionKind]int)
	for _, record := range bundle.Trace.Records {
		counts[record.Action.Kind]++
	}
	if counts[control.ActionCrash] != 1 || counts[control.ActionRestart] != 1 ||
		counts[control.ActionDropMessage] != 2 || counts[control.ActionDuplicateMessage] != 1 ||
		counts[control.ActionInvoke] != 1 {
		t.Fatalf("unexpected action-class trace counts: %#v", counts)
	}
	checked := oracle.CheckBundle(bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{})
	if len(checked.Violations) != 0 {
		t.Fatalf("official action-class bundle violations: %#v", checked.Violations)
	}
}

func TestEtcdraftAdjacentTraceMutationCompletesOrFailsExplicitly(t *testing.T) {
	seedReport, seedBundle := sharedEtcdraftWorkloadBundleFixture(t)
	run := func(first int, planID string) (controlexperiment.Report, controlexperiment.ExecutionBundle, controlexperiment.TraceMutationPlan, error) {
		plan, err := controlexperiment.NewAdjacentTraceMutation(
			planID, seedBundle.Trace, seedReport.Config.Runs[0].Policy, first,
		)
		if err != nil {
			return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, controlexperiment.TraceMutationPlan{}, err
		}
		config := seedReport.Config
		config.ID = "public-etcdraft-v2-trace-mutation-m5.17b"
		config.Runs = append([]controlexperiment.RunPlan(nil), seedReport.Config.Runs...)
		config.Runs[0].Policy = controlexperiment.Policy{
			Version: controlexperiment.TraceMutationPolicyVersion,
			ID:      "adjacent-message-swap-v1/run-1", TraceMutation: &plan,
		}
		factory := func() (control.Adapter, error) {
			return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
		}
		report, bundle, err := controlexperiment.ExecuteQualifiedBundle(
			context.Background(), config, seedBundle.Qualification, factory,
			etcdraftv2.CorePSSMapper{}, etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
		)
		return report, bundle, plan, err
	}
	report, bundle, _, err := run(46, "first-adjacent-message-deliveries-v1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Runs[0].Replay.Stable || bundle.Trace.Records[45].Action.ID != seedBundle.Trace.Records[46].Action.ID ||
		bundle.Trace.Records[46].Action.ID != seedBundle.Trace.Records[45].Action.ID {
		t.Fatalf("adjacent mutation did not preserve its exact splice: %#v", bundle.Trace.Records[45:47])
	}
	_, _, invalidPlan, err := run(34, "dependent-effect-calibration-v1")
	var failure *controlexperiment.ExecutionFailure
	if !errors.As(err, &failure) || failure.Code != "EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED" ||
		failure.Decision != 34 || failure.Work.Primary.SchedulerDecisions != 33 {
		t.Fatalf("dependent mutation failure = %#v/%v", failure, err)
	}
	t.Logf("successful report=%s bundle=%s plan=%s trace=%s; invalid plan=%s work=%#v",
		report.Digest, bundle.Digest, report.Config.Runs[0].Policy.TraceMutation.Digest,
		bundle.Trace.Digest, invalidPlan.Digest, failure.Work)

	encoded, err := os.ReadFile("../../benchmarks/experiments/etcdraft-v2-trace-mutation-m5.17b/summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var summary traceMutationCheckedSummary
	if err := json.Unmarshal(encoded, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.SchemaVersion != "consensus-atlas/trace-mutation-summary/v1" ||
		summary.ID != "public-etcdraft-v2-trace-mutation-m5.17b" ||
		summary.Classification != "public-baseline-calibration-only" ||
		summary.SelectionRule != "first-adjacent-deliver-message-pair-in-source-order" {
		t.Fatalf("unexpected trace-mutation summary identity: %#v", summary)
	}
	if summary.Source.ReportDigest != seedReport.Digest || summary.Source.BundleDigest != seedBundle.Digest ||
		summary.Source.ConfigDigest != seedReport.ConfigDigest || summary.Source.TraceDigest != seedBundle.Trace.Digest ||
		summary.Source.Work.Primary != seedReport.Work.Primary || summary.Source.Work.Replay != seedReport.Work.Replay {
		t.Fatalf("trace-mutation source summary drifted: %#v", summary.Source)
	}
	if summary.Completed.PlanDigest != report.Config.Runs[0].Policy.TraceMutation.Digest ||
		summary.Completed.FirstDecision != 46 || summary.Completed.ReportDigest != report.Digest ||
		summary.Completed.BundleDigest != bundle.Digest || summary.Completed.ConfigDigest != report.ConfigDigest ||
		summary.Completed.TraceDigest != bundle.Trace.Digest ||
		summary.Completed.UniqueCoreStates != report.StateDiscovery.UniqueStates ||
		summary.Completed.PrefixArea != report.StateDiscovery.PrefixArea ||
		summary.Completed.Work.Primary != report.Work.Primary || summary.Completed.Work.Replay != report.Work.Replay {
		t.Fatalf("completed trace-mutation summary drifted: %#v", summary.Completed)
	}
	if summary.Rejected.Purpose != "prove-explicit-unexecutable-accounting-not-a-method-trial" ||
		summary.Rejected.PlanDigest != invalidPlan.Digest || summary.Rejected.FirstDecision != 34 ||
		summary.Rejected.Phase != failure.Phase || summary.Rejected.ReasonCode != failure.Code ||
		summary.Rejected.FailedDecision != failure.Decision ||
		summary.Rejected.Work.Primary != failure.Work.Primary || summary.Rejected.Work.Replay != failure.Work.Replay {
		t.Fatalf("rejected trace-mutation summary drifted: %#v", summary.Rejected)
	}
	wantBaseline := traceMutationSummaryWork{
		Primary: controlexperiment.PhaseWork{SetupAttempts: 2, RuntimeInitializations: 2, PrepareActions: 2, SchedulerDecisions: 192, WorkUnits: 196},
		Replay:  controlexperiment.PhaseWork{SetupAttempts: 2, RuntimeInitializations: 2, PrepareActions: 2, SchedulerDecisions: 192, WorkUnits: 196},
	}
	wantRejected := traceMutationSummaryWork{Primary: failure.Work.Primary, Replay: failure.Work.Replay}
	wantTotal := traceMutationSummaryWork{
		Primary: controlexperiment.PhaseWork{SetupAttempts: 3, RuntimeInitializations: 3, PrepareActions: 3, SchedulerDecisions: 225, WorkUnits: 231},
		Replay:  controlexperiment.PhaseWork{SetupAttempts: 2, RuntimeInitializations: 2, PrepareActions: 2, SchedulerDecisions: 192, WorkUnits: 196},
	}
	if summary.Accounting.Baseline != wantBaseline || summary.Accounting.Rejected != wantRejected ||
		summary.Accounting.Total != wantTotal {
		t.Fatalf("trace-mutation accounting drifted: %#v", summary.Accounting)
	}
}

func TestEtcdraftSemanticWorkloadIsQualifiedCommittedAndReplayStable(t *testing.T) {
	report, executionBundle := sharedEtcdraftWorkloadBundleFixture(t)
	qualificationReport := executionBundle.Qualification.Qualification
	if err := report.ValidateWithQualification(qualificationReport); err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 1 || report.Runs[0].Workload == nil ||
		report.Runs[0].Workload.Completed != 1 ||
		report.Runs[0].Workload.Results[0].Status != "committed" ||
		report.Work.Primary.PrepareActions != 1 || report.Work.Replay.PrepareActions != 1 ||
		report.Work.Primary.SchedulerDecisions != 96 || report.Work.Replay.SchedulerDecisions != 96 ||
		!allReplayStable(report) {
		t.Fatalf("unexpected workload report: %#v", report)
	}
	encoded, err := os.ReadFile("../../benchmarks/experiments/etcdraft-v2-workload-m5.15/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var checked controlexperiment.Report
	if err := json.Unmarshal(encoded, &checked); err != nil {
		t.Fatal(err)
	}
	if err := checked.ValidateWithQualification(qualificationReport); err != nil {
		t.Fatal(err)
	}
	if checked.Digest != report.Digest || checked.Digest !=
		"e680aabd1c98181e2c87fa470610ed300689ed30deff8b2d543c9211a18948d7" {
		t.Fatalf("checked/fresh workload digest = %s/%s", checked.Digest, report.Digest)
	}
	sum := sha256.Sum256(encoded)
	if got := hex.EncodeToString(sum[:]); got != "e7a00826bf4881288696ad5d4c6685d7b82bcf684d7276af1bbf5cc7cad247b1" {
		t.Fatalf("workload artifact SHA-256 = %s", got)
	}
}

func TestEtcdraftExperimentSemanticsV2PreservesTerminalAndPendingRuns(t *testing.T) {
	completedReport, completedBundle, err := etcdraftBundle(
		context.Background(), "workload-semantics-v2", 96, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	completed := completedReport.Runs[0]
	if completedReport.SchemaVersion != controlexperiment.SchemaVersionV2 ||
		completedReport.Config.WorkloadRouterID != etcdraftv2.WorkloadRouterID ||
		completed.Termination != controlexperiment.RunTerminationConfigured ||
		completed.BudgetReached || completed.ChargedDecisions != 42 ||
		len(completed.Selections) != completed.ChargedDecisions ||
		completed.Workload == nil || completed.Workload.Completed != 1 ||
		completed.Workload.Results[0].Status != "committed" ||
		completedReport.Digest != "9411bb311c00bf03e03194e8f91e7a4f796414b044fae2bfa6a3d35f51245b0d" ||
		completedBundle.Digest != "ed44b5be66483be61d15692d1630d91c70c0e485378d9f0849f2753b4155bf69" {
		t.Fatalf("unexpected configured-stop run: %#v report=%s bundle=%s",
			completed, completedReport.Digest, completedBundle.Digest)
	}
	if err := completedBundle.Validate(); err != nil {
		t.Fatal(err)
	}
	completedFeedback, err := controlexperiment.NewPSSFeedback(
		"etcdraft-v2-completed-feedback", completedBundle, etcdraftv2.CorePSSMapper{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if completedFeedback.Samples != 43 || len(completedFeedback.States) != 31 {
		t.Fatalf("unexpected completed feedback: %#v", completedFeedback)
	}
	if err := completedFeedback.ValidateBundle(completedBundle, etcdraftv2.CorePSSMapper{}); err != nil {
		t.Fatal(err)
	}

	pendingReport, pendingBundle, err := etcdraftBundle(
		context.Background(), "workload-semantics-v2", 1, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	pending := pendingReport.Runs[0]
	if pending.Termination != controlexperiment.RunTerminationBudget || !pending.BudgetReached ||
		pending.ChargedDecisions != 1 || pending.Workload == nil || pending.Workload.Offered != 0 ||
		pending.Workload.Completed != 0 || pending.Workload.Pending != 1 ||
		pending.Workload.FinalRoute == nil ||
		pending.Workload.FinalRoute.Status != controlexperiment.WorkloadRouteNoCandidate ||
		pendingReport.Digest != "1570598d392367de14327cd81f08928a87ea491b9da8b215409e65e322e0bff6" ||
		pendingBundle.Digest != "28d271c1e1dcd8358da607dc2bee4f6545ca7ccbca17fedbb42faa65e30fc071" {
		t.Fatalf("unexpected pending run: %#v report=%s bundle=%s",
			pending, pendingReport.Digest, pendingBundle.Digest)
	}
	if err := pendingBundle.Validate(); err != nil {
		t.Fatal(err)
	}
	pendingFeedback, err := controlexperiment.NewPSSFeedback(
		"etcdraft-v2-pending-feedback", pendingBundle, etcdraftv2.CorePSSMapper{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if pendingFeedback.Samples != 2 || len(pendingFeedback.States) != 2 {
		t.Fatalf("unexpected pending feedback: %#v", pendingFeedback)
	}
}

func TestEtcdraftCorpusMutationProducesReprojectedFeedbackAndCompleteMethodCost(t *testing.T) {
	report, bundle, sourceBundle, ledger, err := etcdraftCorpusMutationMethod(context.Background(), 96, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SourceCorpus.Entries[0].ValidateBundle(sourceBundle); err != nil {
		t.Fatal(err)
	}
	if ledger.Feedback == nil {
		t.Fatal("method ledger omitted feedback")
	}
	if err := ledger.Feedback.ValidateBundle(bundle, etcdraftv2.CorePSSMapper{}); err != nil {
		t.Fatal(err)
	}
	plan := report.Config.Runs[0].Policy.TraceMutation
	if plan == nil || plan.SchemaVersion != controlexperiment.TraceMutationPlanVersionV2 ||
		!strings.Contains(report.Config.Runs[0].Policy.Version, "mutation-policy/v2") ||
		len(ledger.SourceCorpus.Entries) != 1 || len(ledger.Records) != 3 ||
		ledger.Records[0].Kind != controlexperiment.MethodRecordSource ||
		ledger.Records[1].Kind != controlexperiment.MethodRecordProposal ||
		ledger.Records[2].Kind != controlexperiment.MethodRecordExecution {
		t.Fatalf("unexpected method structure: plan=%#v ledger=%#v", plan, ledger)
	}
	wantPhase := controlexperiment.PhaseWork{
		SetupAttempts: 2, RuntimeInitializations: 2, PrepareActions: 2,
		SchedulerDecisions: 192, WorkUnits: 196,
	}
	if ledger.Totals.Primary != wantPhase || ledger.Totals.Replay != wantPhase ||
		ledger.Feedback.Samples != 97 || len(ledger.Feedback.States) != 56 ||
		ledger.Feedback.SourceBundleDigest != bundle.Digest ||
		report.Digest != "471d30662a53a9d18011a0eaa3a2b42eb0a3dea3d2a9f5b34d1ecd2808c8a6ab" ||
		bundle.Digest != "0019ec7d3b4f9ad5f24878b497ac0140cf34a73b07a41bd85340ee6ac0a9f303" ||
		plan.Digest != "b3c716e51eff51c5620faa99182f549b565d2e16bcf04ec8ec9483e60c8f2b63" ||
		ledger.SourceCorpus.Digest != "1f93dd84588d979c371bafa58195f566b3abe228f6f6fd77185d0ab78d69f600" ||
		ledger.Feedback.Digest != "9b47d8a9add57b3113a926a353c51ecff772f9a0082e01cb36fa5009bfc8d52a" ||
		ledger.Digest != "7a993f97fbed00246c1c595784be6d91958da7a38f7d484ae62d700fa7b95a08" {
		t.Fatalf("unexpected method totals/feedback: totals=%#v feedback=%#v",
			ledger.Totals, ledger.Feedback)
	}
	t.Logf("report=%s bundle=%s plan=%s corpus=%s feedback=%s ledger=%s",
		report.Digest, bundle.Digest, plan.Digest, ledger.SourceCorpus.Digest,
		ledger.Feedback.Digest, ledger.Digest)
}

func TestEtcdraftM517c2MethodsUseCommonQualifiedSourcesAndCompleteAccounting(t *testing.T) {
	if testing.Short() {
		t.Skip("high-cost real three-run method regression")
	}
	uniform := sharedEtcdraftUniformMethodFixture(t)
	if err := uniform.Observation.ValidateBundles(uniform.Bundles, etcdraftv2.CorePSSMapper{}); err != nil {
		t.Fatal(err)
	}
	guided, err := etcdraftPSSGuidedCorpusMethodFromUniformSources(
		context.Background(), 96, 1, uniform.Reports[:2], uniform.Bundles[:2],
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := guided.Observation.ValidateBundles(guided.Bundles, etcdraftv2.CorePSSMapper{}); err != nil {
		t.Fatal(err)
	}
	if len(uniform.Bundles) != 3 || len(guided.Bundles) < 2 ||
		uniform.Observation.Guidance != nil || guided.Observation.Guidance == nil {
		t.Fatalf("unexpected method shapes: uniform=%#v guided=%#v", uniform.Observation, guided.Observation)
	}
	for index := 0; index < 2; index++ {
		if uniform.Bundles[index].Digest != guided.Bundles[index].Digest ||
			uniform.Bundles[index].Qualification.Qualification.Digest !=
				guided.Bundles[index].Qualification.Qualification.Digest ||
			uniform.Reports[index].Config.FaultEnvelope == nil ||
			*uniform.Reports[index].Config.FaultEnvelope != *guided.Reports[index].Config.FaultEnvelope ||
			uniform.Reports[index].Config.Runs[0].Workload.ID !=
				guided.Reports[index].Config.Runs[0].Workload.ID {
			t.Fatalf("common source %d drifted", index+1)
		}
	}
	wantUniform := controlexperiment.PhaseWork{
		SetupAttempts: 3, RuntimeInitializations: 3, PrepareActions: 3,
		SchedulerDecisions: 288, WorkUnits: 294,
	}
	wantGuidedPrimary := controlexperiment.PhaseWork{
		SetupAttempts: 3, RuntimeInitializations: 3, PrepareActions: 3,
		SchedulerDecisions: 257, WorkUnits: 263,
	}
	wantGuidedReplay := controlexperiment.PhaseWork{
		SetupAttempts: 2, RuntimeInitializations: 2, PrepareActions: 2,
		SchedulerDecisions: 192, WorkUnits: 196,
	}
	choice := guided.Observation.Guidance
	lastRecord := guided.Observation.Ledger.Records[len(guided.Observation.Ledger.Records)-1]
	if uniform.Observation.Ledger.Totals.Primary != wantUniform ||
		uniform.Observation.Ledger.Totals.Replay != wantUniform ||
		uniform.Observation.Measurement.TotalSamples != 291 ||
		uniform.Observation.Measurement.UniqueStates != 238 ||
		uniform.Observation.Digest != "90fb30c3a789de5f7c52c66e82b410e4558cc21a8a701160f584eca2fbdeff75" ||
		guided.Failure == nil || guided.Failure.Code != "EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED" ||
		guided.Failure.Decision != 66 || lastRecord.Outcome != controlexperiment.MethodOutcomeExecutionFailed ||
		guided.Observation.Ledger.Totals.Primary != wantGuidedPrimary ||
		guided.Observation.Ledger.Totals.Replay != wantGuidedReplay ||
		guided.Observation.Measurement.TotalSamples != 194 ||
		guided.Observation.Measurement.UniqueStates != 157 ||
		choice.CandidateCount != 4 || choice.SelectedSourceOrdinal != 2 ||
		choice.SelectedSourceUniqueStates != 75 || choice.SelectedStateGlobalVisits != 1 ||
		choice.FirstDecision != 65 ||
		choice.Mutation.Digest != "313d66bac594616c237b7b2d85e6afbf91835193d92658e6a7d8a50425a5a5b2" ||
		guided.Observation.Digest != "8fd25881a9f1d9315cfbc6650213485be2689c7755cc3bc5e80c935bc386ecf1" {
		t.Fatalf("M5.17c2 method result drifted: uniform=%#v guided=%#v failure=%#v",
			uniform.Observation, guided.Observation, guided.Failure)
	}
	tampered := *choice
	tampered.FirstDecision++
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered PSS guidance was accepted")
	}
	tamperedObservation := uniform.Observation
	tamperedObservation.Budget.MaxPrimaryWorkUnits--
	if err := tamperedObservation.Validate(); err == nil ||
		!strings.Contains(err.Error(), "METHOD_BUDGET_EXCEEDED") {
		t.Fatalf("undersized method budget error = %v", err)
	}
	t.Logf("uniform=%s states=%d work=%d/%d; guided=%s states=%d work=%d/%d failure=%s@%d",
		uniform.Observation.Digest, uniform.Observation.Measurement.UniqueStates,
		uniform.Observation.Ledger.Totals.Primary.WorkUnits,
		uniform.Observation.Ledger.Totals.Replay.WorkUnits,
		guided.Observation.Digest, guided.Observation.Measurement.UniqueStates,
		guided.Observation.Ledger.Totals.Primary.WorkUnits,
		guided.Observation.Ledger.Totals.Replay.WorkUnits,
		guided.Failure.Code, guided.Failure.Decision)
}

func TestEtcdraftM518aBundleV3BindsOperationHistoryAndMethodSpec(t *testing.T) {
	baseline, err := etcdraftReport(context.Background(), "workload-evaluation-v3", 96, 1)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := controlexperiment.MethodConfigProjectionDigest(baseline.Config)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := controlexperiment.NewMethodSpec(controlexperiment.MethodSpec{
		ID:       "public-etcdraft-v2-fixed-calibration-m5.18a",
		Strategy: "workload-evaluation-v3", Decisions: 96, PolicySeed: 1,
		Budget: controlexperiment.MethodBudget{
			MaxExecutionAttempts: 1, MaxPrimaryWorkUnits: 98, MaxReplayWorkUnits: 98,
		},
		PSSID: etcdraftv2.CorePSSMappingID, ProjectorID: etcdraftv2.DecisionProjectionID,
		ConfigProjectionDigest: projection, TimeoutMillis: 120_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	frozenBytes, err := os.ReadFile("../../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/method-spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var frozenSpec controlexperiment.MethodSpec
	if err := json.Unmarshal(frozenBytes, &frozenSpec); err != nil {
		t.Fatal(err)
	}
	if err := frozenSpec.Validate(); err != nil {
		t.Fatal(err)
	}
	if frozenSpec != spec {
		t.Fatalf("frozen/fresh MethodSpec drifted: frozen=%#v fresh=%#v", frozenSpec, spec)
	}
	report, bundle, err := etcdraftBundleV3(
		context.Background(), spec.Strategy, spec.Decisions, spec.PolicySeed, spec.Digest,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.ValidateExecution(report, bundle); err != nil {
		t.Fatal(err)
	}
	if err := bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
		t.Fatal(err)
	}
	persistedBytes, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var persistedBundle controlexperiment.ExecutionBundle
	if err := json.Unmarshal(persistedBytes, &persistedBundle); err != nil {
		t.Fatal(err)
	}
	if err := spec.ValidateExecution(report, persistedBundle); err != nil {
		t.Fatalf("persisted v3 bundle failed validation: %v", err)
	}
	if bundle.SchemaVersion != controlexperiment.ExecutionBundleSchemaVersionV3 ||
		bundle.OperationHistory == nil || len(bundle.OperationHistory.Operations) != 1 ||
		bundle.OperationHistory.Operations[0].RequestID != "m5.15-write-1" ||
		bundle.OperationHistory.Operations[0].Response == nil ||
		bundle.OperationHistory.Operations[0].Response.Status != "committed" ||
		bundle.OperationHistory.Operations[0].ReturnStep <
			bundle.OperationHistory.Operations[0].InvokeStep ||
		bundle.Identity.MethodSpecDigest != spec.Digest {
		t.Fatalf("incomplete v3 operation evidence: %#v", bundle.OperationHistory)
	}

	tampered := bundle
	tamperedHistory := *bundle.OperationHistory
	tamperedHistory.Operations = append(
		[]controlexperiment.OperationRecord(nil), bundle.OperationHistory.Operations...,
	)
	tamperedHistory.Operations[0].ReturnStep++
	tampered.OperationHistory = &tamperedHistory
	if err := tampered.Validate(); err == nil || !strings.Contains(err.Error(), "OPERATION_HISTORY_RETURN_MISMATCH") {
		t.Fatalf("tampered operation history error = %v", err)
	}

	invalidSpec := spec
	invalidSpec.Strategy = "workload;unexpected"
	if _, err := controlexperiment.NewMethodSpec(invalidSpec); err == nil {
		t.Fatal("method strategy containing non-token syntax was accepted")
	}
	t.Logf("method=%s report=%s bundle=%s operations=%s projection=%s",
		spec.Digest, report.Digest, bundle.Digest, bundle.OperationHistory.Digest, projection)
}

func TestRetiredUnqualifiedStrategiesStayUnavailable(t *testing.T) {
	for _, strategy := range []string{"fixed", "random", "stub-planner", "deepseek-planner"} {
		_, err := etcdraftReport(context.Background(), strategy, 1, 1)
		if err == nil || !strings.Contains(err.Error(), "unsupported -strategy") {
			t.Fatalf("strategy %q error = %v, want unsupported", strategy, err)
		}
	}
}

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
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
	report, bundle, err := etcdraftBundle(context.Background(), "workload", 96, 1)
	if err != nil {
		t.Fatal(err)
	}
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
	report, bundle, err := etcdraftBundle(context.Background(), "workload-action-class-random", 96, 1)
	if err != nil {
		t.Fatal(err)
	}
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
	seedReport, seedBundle, err := etcdraftBundle(context.Background(), "workload", 96, 1)
	if err != nil {
		t.Fatal(err)
	}
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
			etcdraftv2.CorePSSMapper{}, etcdraftv2.DecisionProjector{},
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

func TestEtcdraftReportIsEqualBudgetReplayStableAndSelfValidating(t *testing.T) {
	report, err := etcdraftReport(context.Background(), "fixed", 32, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 2 || report.StateDiscovery.TotalDecisions != 64 ||
		report.StateDiscovery.ProtocolSamples != 64 {
		t.Fatalf("unexpected budget: %#v", report.Budget)
	}
	if report.Runs[0].UniqueCoreStates != 23 || report.Runs[1].UniqueCoreStates != 23 ||
		report.StateDiscovery.UniqueStates != 45 || report.StateDiscovery.PrefixArea != 1454 {
		t.Fatalf("unexpected discovery: %#v", report.StateDiscovery)
	}
	if !allReplayStable(report) || report.Work.Primary.WorkUnits != 66 || report.Work.Replay.WorkUnits != 66 {
		t.Fatalf("unexpected replay/work: %#v", report.Work)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var persisted controlexperiment.Report
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if err := persisted.Validate(); err != nil {
		t.Fatalf("persisted Validate() error = %v", err)
	}
	t.Logf("runs=2 decisions=64 states=23/23 union=45 prefix_area=1454 digest=%s", report.Digest)
}

func TestEtcdraftSemanticWorkloadIsQualifiedCommittedAndReplayStable(t *testing.T) {
	report, err := etcdraftReport(context.Background(), "workload", 96, 1)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := qualification.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := report.ValidateWithQualification(bundle.Qualification); err != nil {
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
	if err := checked.ValidateWithQualification(bundle.Qualification); err != nil {
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

func TestEtcdraftRandomReportIsDeterministicAndDoesNotReseedRuntime(t *testing.T) {
	ctx := context.Background()
	first, err := etcdraftReport(ctx, "random", 32, 1)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := etcdraftReport(ctx, "random", 32, 1)
	if err != nil {
		t.Fatal(err)
	}
	other, err := etcdraftReport(ctx, "random", 32, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != repeated.Digest {
		t.Fatalf("same policy seed changed report digest: %s != %s", first.Digest, repeated.Digest)
	}
	if first.Digest == other.Digest {
		t.Fatal("different policy seeds produced the same report digest")
	}
	if first.Config.Runtime != other.Config.Runtime {
		t.Fatal("policy seed changed Runtime configuration")
	}
	for index := range first.Runs {
		if first.Runs[index].TraceDigest != repeated.Runs[index].TraceDigest {
			t.Fatalf("run %d trace changed under repeated policy seed", index+1)
		}
		if first.Runs[index].SeedDigest != other.Runs[index].SeedDigest ||
			first.Runs[index].SeedDigest != first.Runs[0].SeedDigest {
			t.Fatalf("run %d policy seed changed Runtime seed digest", index+1)
		}
	}
	if !allReplayStable(first) || first.StateDiscovery.TotalDecisions != 64 ||
		first.Work.Primary.WorkUnits != 66 || first.Work.Replay.WorkUnits != 66 {
		t.Fatalf("unexpected random budget/replay: %#v", first.Work)
	}
	if first.Runs[0].UniqueCoreStates != 29 || first.Runs[1].UniqueCoreStates != 26 ||
		first.StateDiscovery.UniqueStates != 53 || first.StateDiscovery.PrefixArea != 1843 {
		t.Fatalf("unexpected seed-1 discovery: %#v", first.StateDiscovery)
	}
	t.Logf("seed=1 states=29/26 union=%d prefix_area=%d digest=%s", first.StateDiscovery.UniqueStates,
		first.StateDiscovery.PrefixArea, first.Digest)
}

func TestStubPlannerAttemptUsesTheSoleExecutor(t *testing.T) {
	attempt, err := etcdraftPlannerAttempt(context.Background(), 32, stubPlannerProposal())
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != controlexperiment.PlannerStatusComplete || attempt.Experiment == nil ||
		attempt.Work.ProposalAttempts != 1 || attempt.Work.Model.Calls != 0 {
		t.Fatalf("unexpected attempt envelope: %#v", attempt)
	}
	report := attempt.Experiment
	if report.StateDiscovery.UniqueStates != 45 || report.StateDiscovery.PrefixArea != 1454 ||
		report.Work.Primary.WorkUnits != 66 || report.Work.Replay.WorkUnits != 66 ||
		attempt.Work.Execution != report.Work {
		t.Fatalf("unexpected compiled execution: %#v", report)
	}
	encoded, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	var persisted controlexperiment.PlannerAttempt
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if err := persisted.Validate(); err != nil {
		t.Fatalf("persisted Validate() error = %v", err)
	}
}

func TestStubPlannerRejectedAndUnreachableProposalsAreExplicitAndCharged(t *testing.T) {
	rejectedProposal := stubPlannerProposal()
	rejectedProposal.Policies = rejectedProposal.Policies[:1]
	rejected, err := etcdraftPlannerAttempt(context.Background(), 32, rejectedProposal)
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != controlexperiment.PlannerStatusRejected || rejected.Failure == nil ||
		rejected.Failure.ReasonCode != controlexperiment.ProposalRunSetInvalid ||
		rejected.Work.ProposalAttempts != 1 || rejected.Work.Execution.Primary.SetupAttempts != 0 {
		t.Fatalf("unexpected rejected attempt: %#v", rejected)
	}

	unreachableProposal := stubPlannerProposal()
	unreachableProposal.Policies[0].Rules = []controlexperiment.DecisionRule{{
		Decision: 1, Kind: control.ActionRestart, Node: "n1",
	}}
	unreachable, err := etcdraftPlannerAttempt(context.Background(), 32, unreachableProposal)
	if err != nil {
		t.Fatal(err)
	}
	if unreachable.Status != controlexperiment.PlannerStatusExecutionFailed || unreachable.Failure == nil ||
		unreachable.Failure.ReasonCode != "EXPERIMENT_POLICY_RULE_NOT_ENABLED" ||
		unreachable.Failure.Phase != "primary-policy" || unreachable.Failure.Run != 1 ||
		unreachable.Failure.Decision != 1 || unreachable.Work.ProposalAttempts != 1 ||
		unreachable.Work.Execution.Primary.SetupAttempts != 1 ||
		unreachable.Work.Execution.Primary.RuntimeInitializations != 1 ||
		unreachable.Work.Execution.Primary.SchedulerDecisions != 0 ||
		unreachable.Work.Execution.Primary.WorkUnits != 1 ||
		unreachable.Work.Execution.Replay.SetupAttempts != 0 {
		t.Fatalf("unexpected unreachable attempt: %#v", unreachable)
	}
}

func TestStubPlannerCLIWritesRejectedAttemptWithoutPanicking(t *testing.T) {
	output := filepath.Join(t.TempDir(), "attempt.json")
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{
		"-strategy", "stub-planner", "-decisions", "1", "-out", output,
	}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "status=proposal-rejected") ||
		!strings.Contains(stdout.String(), controlexperiment.ProposalPolicyInvalid) {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestCheckedInDeepSeekAttemptIsExplicitAndSelfValidating(t *testing.T) {
	encoded, err := os.ReadFile("../../benchmarks/experiments/etcdraft-v2-deepseek-planner-m5.13/attempt.json")
	if err != nil {
		t.Fatal(err)
	}
	var attempt controlexperiment.PlannerAttempt
	if err := json.Unmarshal(encoded, &attempt); err != nil {
		t.Fatal(err)
	}
	if err := attempt.Validate(); err != nil {
		t.Fatal(err)
	}
	if attempt.Status != controlexperiment.PlannerStatusExecutionFailed || attempt.Failure == nil ||
		attempt.Failure.ReasonCode != "EXPERIMENT_POLICY_RULE_NOT_ENABLED" ||
		attempt.Failure.Run != 2 || attempt.Failure.Decision != 3 ||
		attempt.Work.Model != (controlexperiment.ModelWork{
			Calls: 1, InputTokens: 555, OutputTokens: 189, TotalTokens: 744,
		}) || attempt.Work.Execution.Primary.SchedulerDecisions != 34 ||
		attempt.Work.Execution.Replay.SchedulerDecisions != 32 {
		t.Fatalf("unexpected checked-in attempt: %#v", attempt)
	}
	sum := sha256.Sum256(encoded)
	if got := hex.EncodeToString(sum[:]); got != "7d6658693ebaf3e05b6e2a130cfd65f79350a2b649276a26c01b8c2fa2689e71" {
		t.Fatalf("artifact SHA-256 = %s", got)
	}
}

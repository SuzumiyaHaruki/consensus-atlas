package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

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
	checked := oracle.CheckBundle(
		bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{}, etcdraftLogProgressMonitor{},
		etcdraftClientApplicationBindingMonitor{},
	)
	if len(checked.Violations) != 0 || len(checked.Checked) != 4 ||
		checked.Checked[2] != etcdraftLogProgressMonitorID ||
		checked.Checked[3] != etcdraftClientApplicationBindingMonitorID {
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
	checkM521nFormalFreshEvaluation(t, spec, report, bundle)

	invalidSpec := spec
	invalidSpec.Strategy = "workload;unexpected"
	if _, err := controlexperiment.NewMethodSpec(invalidSpec); err == nil {
		t.Fatal("method strategy containing non-token syntax was accepted")
	}
	t.Logf("method=%s report=%s bundle=%s operations=%s projection=%s",
		spec.Digest, report.Digest, bundle.Digest, bundle.OperationHistory.Digest, projection)
}

func TestRetiredUnqualifiedStrategiesStayUnavailable(t *testing.T) {
	for _, strategy := range []string{"fixed", "random", "stub-planner"} {
		_, err := etcdraftReport(context.Background(), strategy, 1, 1)
		if err == nil || !strings.Contains(err.Error(), "unsupported -strategy") {
			t.Fatalf("strategy %q error = %v, want unsupported", strategy, err)
		}
	}
}

func TestA5aAgentStrategiesRequireExplicitSemanticInput(t *testing.T) {
	for _, strategy := range []string{
		etcdraftSemanticCalibrationStrategy,
		etcdraftScenarioCalibrationStrategy,
		etcdraftScenarioSessionStrategy,
	} {
		output := &strings.Builder{}
		err := run(context.Background(), []string{
			"-strategy", strategy,
			"-campaign-dir", filepath.Join(t.TempDir(), "agent-run"),
			"-stateless-corpus", etcdraftTestRootCorpusPath,
			"-agent-key-file", "fixture-key-source",
			"-agent-model", openRouterFixtureModel,
		}, output)
		if err == nil || !strings.Contains(err.Error(), "-semantic-input") {
			t.Fatalf("strategy %q accepted implicit semantic knowledge: %v", strategy, err)
		}
	}
}

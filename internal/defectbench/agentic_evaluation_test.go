package defectbench

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

func TestAgenticHoldoutRecomputesVerdictsFromCompletedEpisodeBundles(t *testing.T) {
	contract, exposure, evidence := agenticHoldoutFixture(t)
	report, err := EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, etcdraftv2.DecisionProjector{}, oracle.BundleAgreement{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	want := FormalEvaluationSummary{Controls: 3, Candidates: 3, RootCauses: 3}
	if report.Summary != want || len(report.Results) != 6 || len(report.Pairs) != 3 {
		t.Fatalf("agentic holdout summary = %#v", report)
	}
	for _, result := range report.Results {
		wantStatus := BundleStatusSurvived
		if result.Result.Kind == BundleKindControl {
			wantStatus = BundleStatusControlPass
		}
		if result.Result.Status != wantStatus || result.Result.Finding != nil ||
			result.Result.Oracle.Violations != nil && len(result.Result.Oracle.Violations) != 0 {
			t.Fatalf("method-reported finding affected trusted result: %#v", result)
		}
	}
}

func TestAgenticHoldoutClassifiesIncompleteEpisodeAsInvalidTrial(t *testing.T) {
	contract, exposure, evidence := agenticHoldoutFixture(t)
	trialID := contract.Pairs[0].Candidate.TrialID
	incomplete := evidence[trialID]
	incomplete.EpisodeStatus, incomplete.EvidenceStatus = AgenticEpisodeRiskStopped, "planning-failed"
	incomplete.Bundle, incomplete.CandidateBundles = nil, nil
	evidence[trialID] = incomplete
	report, err := EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, etcdraftv2.DecisionProjector{}, oracle.BundleAgreement{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.InvalidTrials != 1 || report.Summary.KilledCandidates != 0 {
		t.Fatalf("incomplete Episode summary = %#v", report.Summary)
	}
	for _, result := range report.Results {
		if result.TrialID == trialID &&
			(result.Result.Status != BundleStatusInvalid ||
				result.Result.InvalidReason != "AGENTIC_HOLDOUT_EPISODE_INCOMPLETE") {
			t.Fatalf("incomplete Episode result = %#v", result)
		}
	}
}

func TestAgenticHoldoutChargesSearchModelAndMethodIdentity(t *testing.T) {
	contract, exposure, evidence := agenticHoldoutFixture(t)
	trialID := contract.Pairs[0].Candidate.TrialID
	original := evidence[trialID]

	highSearch := original
	highSearch.ScenarioFrontier = controlexperiment.PhaseWork{
		SetupAttempts: contract.Budget.MaxPrimaryWorkUnits,
		WorkUnits:     contract.Budget.MaxPrimaryWorkUnits,
	}
	evidence[trialID] = highSearch
	report, err := EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, etcdraftv2.DecisionProjector{}, oracle.BundleAgreement{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertAgenticInvalidReason(
		t, report, trialID, "AGENTIC_HOLDOUT_AGGREGATE_BUDGET_EXCEEDED",
	)

	highSearchReplay := original
	highSearchReplay.ScenarioSearch.ChildVerification = controlexperiment.PhaseWork{
		SetupAttempts: contract.AgenticBudget.MaxReplayWorkUnits,
		WorkUnits:     contract.AgenticBudget.MaxReplayWorkUnits,
	}
	highSearchReplay.ScenarioSearch.TotalWorkUnits =
		highSearchReplay.ScenarioSearch.ChildVerification.WorkUnits
	evidence[trialID] = highSearchReplay
	report, err = EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, etcdraftv2.DecisionProjector{}, oracle.BundleAgreement{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertAgenticInvalidReason(t, report, trialID, "AGENTIC_HOLDOUT_REPLAY_BUDGET_EXCEEDED")
	for _, result := range report.Results {
		if result.TrialID == trialID && result.Result.ReplayWork <=
			original.Bundle.Work.Replay.WorkUnits {
			t.Fatalf("search verification was not charged as replay: %#v", result.Result)
		}
	}

	overModel := original
	overModel.RiskAttempts = contract.AgenticBudget.MaxModelCalls + 1
	overModel.ModelWork = controlexperiment.ModelWork{
		Calls: overModel.RiskAttempts, InputTokens: overModel.RiskAttempts,
		TotalTokens: overModel.RiskAttempts,
	}
	evidence[trialID] = overModel
	report, err = EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, etcdraftv2.DecisionProjector{}, oracle.BundleAgreement{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertAgenticInvalidReason(t, report, trialID, "AGENTIC_HOLDOUT_MODEL_BUDGET_EXCEEDED")

	otherMethod := original
	otherBundle := agenticHoldoutTestBundle(
		t, testFormalDigest("other-agentic-method"),
		"626173652d6167656e7469632d686f6c646f75742d736565642d7633",
	)
	otherMethod.Bundle = &otherBundle
	evidence[trialID] = otherMethod
	report, err = EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, etcdraftv2.DecisionProjector{}, oracle.BundleAgreement{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertAgenticInvalidReason(t, report, trialID, "AGENTIC_HOLDOUT_BUNDLE_CONTRACT_MISMATCH")
}

func assertAgenticInvalidReason(
	t *testing.T,
	report AgenticHoldoutEvaluation,
	trialID string,
	reason string,
) {
	t.Helper()
	for _, result := range report.Results {
		if result.TrialID == trialID {
			if result.Result.Status != BundleStatusInvalid || result.Result.InvalidReason != reason {
				t.Fatalf("trial %s invalid result = %#v", trialID, result.Result)
			}
			return
		}
	}
	t.Fatalf("trial %s result missing", trialID)
}

func TestAgenticHoldoutEvaluatesBranchOnlyAndUnselectedCandidateBundles(t *testing.T) {
	contract, exposure, evidence := agenticHoldoutFixture(t)
	branchBundle := agenticHoldoutTestBundle(
		t, contract.MethodSpecDigest,
		"6272616e63682d6167656e7469632d686f6c646f75742d736565642d7633",
	)
	for index := range contract.Pairs {
		contract.Pairs[index].Control.ExpectedConfigDigest = ""
		contract.Pairs[index].Candidate.ExpectedConfigDigest = ""
	}
	contract.Composition.MonitorIDs = []string{targetoracles.LogProgressMonitorID}
	contract, err := contract.Seal()
	if err != nil {
		t.Fatal(err)
	}
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	exposure, err = AuditFormalExposure(
		contract, view, []FormalPublicArtifact{{Bytes: []byte(`{"trial_id":"opaque-01"}`)}},
	)
	if err != nil {
		t.Fatal(err)
	}

	branchOnlyID := contract.Pairs[0].Control.TrialID
	branchOnly := evidence[branchOnlyID]
	branchOnly.Bundle = nil
	branchOnly.CandidateBundles = []controlexperiment.ExecutionBundle{branchBundle}
	evidence[branchOnlyID] = branchOnly

	findingID := contract.Pairs[0].Candidate.TrialID
	withBranch := evidence[findingID]
	withBranch.CandidateBundles = []controlexperiment.ExecutionBundle{branchBundle}
	evidence[findingID] = withBranch

	report, err := EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, etcdraftv2.DecisionProjector{},
		digestFindingMonitor{digest: branchBundle.Digest},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range report.Results {
		switch result.TrialID {
		case branchOnlyID:
			if result.Result.Status != BundleStatusFalsePositive ||
				result.Result.BundleDigest != branchBundle.Digest || result.Result.Finding == nil ||
				result.Result.Finding.Monitor != targetoracles.LogProgressMonitorID {
				t.Fatalf("branch-only control result = %#v", result.Result)
			}
		case findingID:
			if result.Result.Status != BundleStatusKilled ||
				result.Result.BundleDigest != branchBundle.Digest || result.Result.Finding == nil ||
				result.Result.Finding.Monitor != targetoracles.LogProgressMonitorID {
				t.Fatalf("unselected finding result = %#v", result.Result)
			}
		}
	}
	primaryBundle := *withBranch.Bundle
	singlePrimary := max(
		primaryBundle.Work.Primary.WorkUnits, branchBundle.Work.Primary.WorkUnits,
	)
	searchPrimary := contract.Budget.MaxPrimaryWorkUnits - singlePrimary
	withBranch.ScenarioFrontier = controlexperiment.PhaseWork{
		SetupAttempts: searchPrimary, WorkUnits: searchPrimary,
	}
	evidence[findingID] = withBranch
	report, err = EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, etcdraftv2.DecisionProjector{},
		digestFindingMonitor{digest: branchBundle.Digest},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range report.Results {
		if result.TrialID == findingID &&
			(result.Result.Status != BundleStatusInvalid ||
				result.Result.InvalidReason != "AGENTIC_HOLDOUT_AGGREGATE_BUDGET_EXCEEDED" ||
				result.Result.Decisions != len(primaryBundle.Trace.Records)+len(branchBundle.Trace.Records) ||
				result.Result.PrimaryWork != searchPrimary+primaryBundle.Work.Primary.WorkUnits+
					branchBundle.Work.Primary.WorkUnits) {
			t.Fatalf("multi-candidate budget was not aggregated before finding: %#v", result.Result)
		}
	}
}

type digestFindingMonitor struct {
	digest string
}

func (digestFindingMonitor) Name() string { return targetoracles.LogProgressMonitorID }

func (monitor digestFindingMonitor) CheckBundle(
	bundle controlexperiment.ExecutionBundle,
) []oracle.Violation {
	if bundle.Digest != monitor.digest {
		return nil
	}
	return []oracle.Violation{{
		Monitor: monitor.Name(), Step: len(bundle.Trace.Records), Message: "test branch finding",
	}}
}

func agenticHoldoutFixture(
	t *testing.T,
) (FormalBenchmarkContract, FormalExposureAudit, map[string]AgenticTrialEvidence) {
	t.Helper()
	budget := controlexperiment.AgenticLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: 512, MaxPrimaryWorkUnits: 1024,
		MaxReplayWorkUnits: 1024, MaxModelCalls: 6, MaxModelTokens: 50_000,
	}
	methodSpec := agenticHoldoutTestMethodSpec(t, 1, budget)
	methodSpecDigest := methodSpec.Digest
	bundle := agenticHoldoutTestBundle(
		t, methodSpecDigest,
		"626173652d6167656e7469632d686f6c646f75742d736565642d7633",
	)
	pair := func(index int, root string) FormalPair {
		controlTrial := "opaque-0" + string(rune('1'+(index-1)*2))
		candidateTrial := "opaque-0" + string(rune('2'+(index-1)*2))
		variant := func(trial, id string) FormalVariant {
			return FormalVariant{
				TrialID: trial, VariantID: id,
				ExpectedBuildID:          bundle.Qualification.Manifest.BuildID,
				ExpectedConfigDigest:     bundle.Identity.ConfigDigest,
				ExpectedBuildAuditDigest: testFormalDigest("audit-" + id),
				ExpectedBinaryDigest:     testFormalDigest("binary-" + id),
			}
		}
		return FormalPair{
			PairID: "private-pair-" + string(rune('a'+index-1)), RootCauseID: root,
			Control:   variant(controlTrial, "private-control-"+string(rune('a'+index-1))),
			Candidate: variant(candidateTrial, "private-candidate-"+string(rune('a'+index-1))),
		}
	}
	contract, err := (FormalBenchmarkContract{
		ID: "agentic-holdout-fixture", FamilyID: "leader-cft",
		ProfileDigest:        bundle.Qualification.Profile.Digest,
		BlindingNonce:        testFormalDigest("agentic-holdout-nonce"),
		MethodSpecDigest:     methodSpecDigest,
		RequiredBundleSchema: bundle.SchemaVersion,
		Budget: BundleBudget{
			MaxDecisions:        budget.MaxPrimarySchedulerDecisions,
			MaxPrimaryWorkUnits: budget.MaxPrimaryWorkUnits,
		},
		AgenticBudget: &budget,
		Composition: FormalCompositionSpec{
			ProjectorID: etcdraftv2.DecisionProjectionID, MonitorIDs: []string{"agreement"},
		},
		Pairs: []FormalPair{
			pair(1, "private-root-alpha"), pair(2, "private-root-beta"),
			pair(3, "private-root-gamma"),
		},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	exposure, err := AuditFormalExposure(
		contract, view, []FormalPublicArtifact{{Bytes: []byte(`{"trial_id":"opaque-01"}`)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence := make(map[string]AgenticTrialEvidence, 6)
	for _, pair := range contract.Pairs {
		for _, trialID := range []string{pair.Control.TrialID, pair.Candidate.TrialID} {
			copyBundle := bundle
			evidence[trialID] = AgenticTrialEvidence{
				TargetID: "etcdraft-v2", MethodSpecDigest: methodSpecDigest,
				MethodSpec: methodSpec, EpisodeCount: 1,
				EpisodeStatus: AgenticEpisodeCompleted, EvidenceStatus: "oracle-finding",
				Budget: budget, ModelWork: controlexperiment.ModelWork{}, Bundle: &copyBundle,
			}
		}
	}
	return contract, exposure, evidence
}

func agenticHoldoutTestMethodSpec(
	t *testing.T,
	episodes int,
	episodeBudget controlexperiment.AgenticLogicalBudget,
) controlexperiment.AgenticMethodSpec {
	t.Helper()
	total, err := controlexperiment.ScaleAgenticLogicalBudget(episodeBudget, episodes)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := controlexperiment.NewAgenticMethodSpec(controlexperiment.AgenticMethodSpec{
		TargetID: "etcdraft-v2",
		Transport: controlexperiment.AgentTransportFreeze{
			Provider: "openrouter", Endpoint: "https://openrouter.ai/api/v1/chat/completions",
			Model: "fixture/agentic-model", Thinking: "low", ExcludeReasoning: true,
			StructuredOutputMode: "json-schema", RequestTimeoutMS: 900_000,
			RoutingPolicy: "openrouter-default", AllowProviderFallback: true,
			MaxOutputTokens: 32000, MaxCallsPerArm: 1,
		},
		RiskPromptVersion:        "risk-agent-navigation-v2",
		ScenarioPromptVersion:    "scenario-agent-investigation-v9",
		SemanticInputSchema:      "etcdraft-agentic-input-v1",
		SemanticInputDigest:      testFormalDigest("agentic-semantic-input"),
		ScenarioSemanticExposure: controlexperiment.ScenarioSemanticExposureFull,
		SourceExposure: controlexperiment.AgenticSourceExposureSpec{
			Mode: controlexperiment.AgenticSourceExposureNone,
		},
		EpisodeLimits: controlexperiment.AgenticEpisodeLimits{
			MaxRiskCalls: 3, MaxScenarioCalls: 3, MaxTotalCalls: episodeBudget.MaxModelCalls,
			MaxObservedTokens: episodeBudget.MaxModelTokens, MaxScenarioPlanSteps: 4,
			MaxRuntimeDecisions: episodeBudget.MaxPrimarySchedulerDecisions,
			SessionWallClockMS:  600_000,
		},
		InvestigationEpisodes: episodes, EpisodeBudget: episodeBudget, InvestigationBudget: total,
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func agenticHoldoutTestBundle(
	t *testing.T,
	methodSpecDigest string,
	seedHex string,
) controlexperiment.ExecutionBundle {
	t.Helper()
	qualified, err := qualification.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	admission, err := controlexperiment.BindExecutionAdmission(
		qualified.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: qualified.Profile.RequiredCapabilityIDs()},
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: "agentic-holdout-write", Value: []byte("alpha"),
	})
	if err != nil {
		t.Fatal(err)
	}
	workload := controlexperiment.WorkloadPlan{
		SchemaVersion: controlexperiment.WorkloadPlanVersion, ID: "agentic-holdout-workload",
		TargetSelector: controlexperiment.TargetSingleCoordinatingMember,
		Invocations: []controlexperiment.WorkloadInvocation{{
			ID: "agentic-holdout-write", Input: payload, ExpectedStatus: "committed",
		}},
	}
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersionV2, ID: "agentic-holdout-v3",
		PSSID:     etcdraftv2.CorePSSMappingID,
		Runtime:   controlexperiment.RuntimeConfig{SeedHex: seedHex, MaxClones: 1},
		Admission: &admission, WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		DecisionsPerRun: 32, RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, Workload: &workload,
			Policy: controlexperiment.Policy{
				Version: controlexperiment.PolicyVersion, ID: "agentic-holdout-policy",
				Priority: []control.ActionKind{
					control.ActionInvoke, control.ActionCompleteEffect,
					control.ActionDeliverMessage, control.ActionFireTemporal,
				},
			},
		}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	_, bundle, err := controlexperiment.ExecuteQualifiedBundleV3(
		context.Background(), config, qualified, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{}, methodSpecDigest,
	)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

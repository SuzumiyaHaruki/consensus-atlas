package defectbench

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

func TestAgenticHoldoutRecomputesVerdictsFromCompletedEpisodeBundles(t *testing.T) {
	contract, exposure, evidence := agenticHoldoutFixture(t)
	report, err := EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, omnipaxosv2.DecisionProjector{}, oracle.BundleAgreement{},
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
	evidence[trialID] = AgenticTrialEvidence{
		TargetID: "omnipaxos-v2", EpisodeStatus: AgenticEpisodeRiskStopped,
		EvidenceStatus: "planning-failed", ModelWork: controlexperiment.ModelWork{},
	}
	report, err := EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, omnipaxosv2.DecisionProjector{}, oracle.BundleAgreement{},
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

func agenticHoldoutFixture(
	t *testing.T,
) (FormalBenchmarkContract, FormalExposureAudit, map[string]AgenticTrialEvidence) {
	t.Helper()
	data, err := os.ReadFile(
		"../../benchmarks/experiments/agentic-investigation-a9e4c4-openrouter-omnipaxos-r5/episode-0001/bundle.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	var bundle controlexperiment.ExecutionBundle
	if err := json.Unmarshal(data, &bundle); err != nil || bundle.Validate() != nil {
		t.Fatalf("archived Agentic bundle invalid: %v", err)
	}
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
		MethodSpecDigest:     testFormalDigest("agentic-holdout-method"),
		RequiredBundleSchema: bundle.SchemaVersion,
		Budget:               BundleBudget{MaxDecisions: 256, MaxPrimaryWorkUnits: 512},
		Composition: FormalCompositionSpec{
			ProjectorID: omnipaxosv2.DecisionProjectionID, MonitorIDs: []string{"agreement"},
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
				TargetID: "omnipaxos-v2", EpisodeStatus: AgenticEpisodeCompleted,
				EvidenceStatus: "oracle-finding", ModelWork: controlexperiment.ModelWork{},
				Bundle: &copyBundle,
			}
		}
	}
	return contract, exposure, evidence
}

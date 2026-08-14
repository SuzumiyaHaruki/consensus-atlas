package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestPairedScenarioEvidenceKeepsCampaignCostSeparateFromBundle(t *testing.T) {
	_, _, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	target := bundle.Trace.ManifestDigest
	writePairedScenarioArm(t, root, pairedScenarioPlannerDeterministic, target, bundle)
	writePairedScenarioArm(t, root, pairedScenarioPlannerAgent, target, bundle)

	evidence, err := loadPairedScenarioTrialEvidence(root)
	if err != nil || evidence.TargetIdentity != target ||
		evidence.Deterministic.Bundle.Digest != bundle.Digest || evidence.Agent.Bundle.Digest != bundle.Digest ||
		evidence.Deterministic.CampaignWork.Primary.WorkUnits <= bundle.Work.Primary.WorkUnits ||
		evidence.Agent.CampaignWork.Primary.WorkUnits <= bundle.Work.Primary.WorkUnits ||
		evidence.Deterministic.CampaignWork.Model.Calls != 0 ||
		evidence.Agent.CampaignWork.Model.Calls != 1 {
		t.Fatalf("paired evidence lost method or full-cost identity: %#v err=%v", evidence, err)
	}
}

func TestPairedScenarioEvidenceRejectsDifferentSUTIdentity(t *testing.T) {
	_, _, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	writePairedScenarioArm(t, root, pairedScenarioPlannerDeterministic, bundle.Trace.ManifestDigest, bundle)
	writePairedScenarioArm(t, root, pairedScenarioPlannerAgent, strings.Repeat("a", 64), bundle)
	if _, err := loadPairedScenarioTrialEvidence(root); err == nil ||
		!strings.Contains(err.Error(), "ARTIFACT_INVALID") {
		t.Fatalf("mismatched target identity error = %v", err)
	}
}

func writePairedScenarioArm(
	t *testing.T,
	root string,
	planner string,
	targetIdentity string,
	bundle controlexperiment.ExecutionBundle,
) {
	t.Helper()
	budget := controlexperiment.CampaignLogicalBudget{
		MaxAttempts: 1, MaxPrimarySchedulerDecisions: 10_000,
		MaxPrimaryWorkUnits: 20_000, MaxReplayWorkUnits: 20_000,
	}
	if planner == pairedScenarioPlannerAgent {
		budget.MaxModelCalls, budget.MaxModelTokens = 4, 40_000
	}
	config, err := controlexperiment.NewCampaignConfig(
		"a8-"+planner, "fixture-target", targetIdentity,
		digestBytes([]byte("experiment-"+planner)), budget, 60_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, planner)
	recovered, err := controlexperiment.CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := json.Marshal(map[string]any{
		"episode":           map[string]any{"planner": planner},
		"semantic_exposure": "full",
		"testing_evidence":  map[string]any{"execution_bundle": bundle},
	})
	if err != nil {
		t.Fatal(err)
	}
	work := bundle.Work
	work.Primary.PrepareActions += 100
	work.Primary.WorkUnits += 100
	work.Replay.PrepareActions += 100
	work.Replay.WorkUnits += 100
	if planner == pairedScenarioPlannerAgent {
		work.Model = controlexperiment.ModelWork{Calls: 1, InputTokens: 100, OutputTokens: 20, TotalTokens: 120}
	}
	provider := controlexperiment.CampaignAttemptProviderFunc(func(
		context.Context,
		controlexperiment.CampaignAttemptRequest,
	) (controlexperiment.CampaignAttemptResult, error) {
		return controlexperiment.CampaignAttemptResult{
			Outcome: controlexperiment.CampaignAttemptCompleted, Work: work, Artifact: artifact,
		}, nil
	})
	coordinator, err := controlexperiment.NewCampaignCoordinator(&recovered, provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

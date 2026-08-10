package controlexperiment

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestCampaignPlannedAttemptBindsPlannerChainAndWork(t *testing.T) {
	planned := campaignPlannedAttemptFixture(t)
	if err := planned.ValidateRequest(planned.View.Request); err != nil {
		t.Fatal(err)
	}
	execution := campaignTestWork(1, 1, 0, 0)
	combined, err := planned.AttachPlanningWork(execution)
	if err != nil || combined != execution {
		t.Fatalf("zero-model work changed execution ledger: %#v/%v", combined, err)
	}

	renamed := planned
	renamed.Proposal.ID = "other-id"
	renamed.Proposal, err = NewGuardedTestIntent(renamed.Proposal)
	if err != nil {
		t.Fatal(err)
	}
	renamed, err = renamed.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := renamed.Validate(); err == nil {
		t.Fatal("planned attempt accepted proposal authority drift")
	}

	charged := planned
	charged.PlanningWork = ModelWork{Calls: 1, InputTokens: 1, TotalTokens: 1}
	charged, err = charged.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := charged.Validate(); err == nil || !strings.Contains(err.Error(), "ALLOWANCE_EXCEEDED") {
		t.Fatalf("planned attempt exceeded zero model allowance: %v", err)
	}
}

func TestCampaignPlannedAttemptSurvivesInterruptionAndBindsCommit(t *testing.T) {
	config := campaignTestConfig(t, "planned-store", strings.Repeat("a", 64), CampaignLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: 4, MaxPrimaryWorkUnits: 8, MaxReplayWorkUnits: 8,
	}, 1_000)
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := NewCampaignSummary(&recovered)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := NewCampaignObservation(summary, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic := campaignPlannerSemantic(t)
	baseline := campaignPlannerBaseline(t, semantic)
	request, err := NewCampaignAttemptRequest(config, recovered.Head)
	if err != nil {
		t.Fatal(err)
	}
	view, err := NewCampaignPlannerView("planned-store-view", semantic, observation, request, baseline)
	if err != nil {
		t.Fatal(err)
	}
	planned := campaignPlannedAttemptForView(t, view)
	if _, err := recovered.PreparePlannedAttempt(planned); err != nil {
		t.Fatal(err)
	}

	interrupted, err := RecoverCampaignDirectory(directory, config)
	if err != nil || len(interrupted.PlannedAttempts) != 1 || interrupted.Head.Sequence != 0 {
		t.Fatalf("durable plan did not survive interruption: %#v/%v", interrupted, err)
	}
	if _, err := interrupted.PreparePlannedAttempt(planned); err != nil {
		t.Fatalf("same plan was not idempotent: %v", err)
	}
	artifact := []byte("planned artifact")
	record, err := NewCampaignAttemptRecord(CampaignAttemptRecord{
		Ordinal: 1, ID: "planned-store-attempt", InputDigest: planned.Digest,
		ArtifactDigest: CampaignArtifactDigest(artifact), Outcome: CampaignAttemptCompleted,
		Work: campaignTestWork(1, 1, 0, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	wrong := record
	wrong.InputDigest = strings.Repeat("f", 64)
	wrong, err = NewCampaignAttemptRecord(wrong)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := interrupted.CommitAttempt(wrong, artifact, 1); err == nil ||
		!strings.Contains(err.Error(), "PLAN_RECORD_MISMATCH") {
		t.Fatalf("commit escaped durable plan binding: %v", err)
	}
	if _, err := interrupted.CommitAttempt(record, artifact, 1); err != nil {
		t.Fatal(err)
	}
	checked, err := RecoverCampaignDirectory(directory, config)
	if err != nil || checked.Head.Sequence != 1 || checked.Head.Record.InputDigest != planned.Digest {
		t.Fatalf("planned commit binding did not recover: %#v/%v", checked, err)
	}
}

func campaignPlannedAttemptFixture(t *testing.T) CampaignPlannedAttempt {
	t.Helper()
	semantic := campaignPlannerSemantic(t)
	observation, request := campaignPlannerPrefix(t)
	baseline := campaignPlannerBaseline(t, semantic)
	view, err := NewCampaignPlannerView("planned-view", semantic, observation, request, baseline)
	if err != nil {
		t.Fatal(err)
	}
	return campaignPlannedAttemptForView(t, view)
}

func campaignPlannerBaseline(t *testing.T, semantic AgentSemanticView) GuardedTestIntent {
	t.Helper()
	baseline, err := NewGuardedTestIntent(GuardedTestIntent{
		ID: "planner-baseline", ViewDigest: semantic.Digest, RiskID: "risk",
		Must: IntentMust{Decisions: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return baseline
}

func campaignPlannedAttemptForView(t *testing.T, view CampaignPlannerView) CampaignPlannedAttempt {
	t.Helper()
	proposal, err := PlanDeterministicCampaignFixture(view)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (CompiledIntentPlanV2{
		SchemaVersion: CompiledIntentPlanVersionV2, ID: "planned-plan",
		ViewDigest: proposal.ViewDigest, IntentDigest: proposal.Digest,
		CatalogDigest: strings.Repeat("b", 64), RiskID: proposal.RiskID,
		BackendID: proposal.Prefer.BackendIDs[0], Strategy: "fixture-strategy", Decisions: 1,
		RequiredCapabilities: []string{"capability"}, RequiredActions: []control.ActionKind{control.ActionInvoke},
		CompilerWork: IntentCompilerWork{CandidatesEvaluated: 1, WorkUnits: 1},
	}).seal()
	if err != nil {
		t.Fatal(err)
	}
	instance, err := NewIntentExecutionInstance("planned-instance", plan, 9, MethodBudget{
		MaxExecutionAttempts: 1, MaxPrimaryWorkUnits: 2, MaxReplayWorkUnits: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	choice, err := NewCampaignExecutionChoice(proposal, plan, instance)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := NewCampaignPlannedAttempt(
		"planned-attempt", view, proposal, plan, instance, choice, ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return planned
}

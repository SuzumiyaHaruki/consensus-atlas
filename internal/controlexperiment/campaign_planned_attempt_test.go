package controlexperiment

import (
	"os"
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
	config, err := RequirePlannedCampaignAttempts(config)
	if err != nil {
		t.Fatal(err)
	}
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
	conflict, err := NewCampaignPlannedAttempt(
		"conflicting-plan", planned.View, planned.Proposal, planned.Plan,
		planned.Instance, planned.Choice, planned.PlanningWork,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := interrupted.PreparePlannedAttempt(conflict); err == nil ||
		!strings.Contains(err.Error(), "PLAN_CONFLICT") {
		t.Fatalf("durable plan was replaceable: %v", err)
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
	planPath := filepath.Join(directory, campaignPlansDir, campaignCheckpointFile(1))
	original, err := os.ReadFile(planPath)
	campaignRequireNoError(t, err)
	tampered := planned
	tampered.ID = "tampered-plan"
	encoded, err := campaignJSONBytes(tampered)
	campaignRequireNoError(t, err)
	campaignRequireNoError(t, os.WriteFile(planPath, encoded, 0o600))
	if _, err := RecoverCampaignDirectory(directory, config); err == nil {
		t.Fatal("tampered plan recovered")
	}
	campaignRequireNoError(t, os.WriteFile(planPath, original, 0o600))
	futurePath := filepath.Join(directory, campaignPlansDir, campaignCheckpointFile(2))
	campaignRequireNoError(t, os.WriteFile(futurePath, original, 0o600))
	if _, err := RecoverCampaignDirectory(directory, config); err == nil {
		t.Fatal("future plan recovered")
	}
	campaignRequireNoError(t, os.Remove(futurePath))
	campaignRequireNoError(t, os.Remove(planPath))
	if _, err := RecoverCampaignDirectory(directory, config); err == nil {
		t.Fatal("planned campaign recovered without its committed plan")
	}
}

func TestCampaignModelCallDurabilityAndAmbiguousRecovery(t *testing.T) {
	config, directory, recovered, intent := campaignModelCallFixture(t, "completed")
	_, err := recovered.PrepareModelCall(intent)
	campaignRequireNoError(t, err)
	if repeated, err := recovered.PrepareModelCall(intent); err != nil || repeated.Digest != intent.Digest {
		t.Fatalf("prepared call was not idempotent: %#v/%v", repeated, err)
	}
	dispatch, err := recovered.DispatchModelCall()
	campaignRequireNoError(t, err)
	result, err := NewCampaignModelCallResult(intent, dispatch, CampaignModelCallResult{
		Status: CampaignModelCallCompleted, Content: []byte(`{"prefer":{}}`),
		ResponseDigest: strings.Repeat("a", 64),
		Response:       &AgentResponseIdentity{ID: "response", Model: "model", FinishReason: "stop"},
		DurationMillis: 1, Work: ModelWork{Calls: 1, InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
	})
	campaignRequireNoError(t, err)
	_, err = recovered.CommitModelCallResult(result)
	campaignRequireNoError(t, err)
	checked, err := RecoverCampaignDirectory(directory, config)
	if err != nil || len(checked.ModelCalls) != 1 || checked.ModelCalls[0].Status != CampaignModelCallCompleted {
		t.Fatalf("completed call did not recover: %#v/%v", checked.ModelCalls, err)
	}
	resultPath := filepath.Join(directory, campaignModelCallsDir, campaignModelCallFile(1, "result"))
	original, err := os.ReadFile(resultPath)
	campaignRequireNoError(t, err)
	tampered := result
	tampered.DurationMillis++
	encoded, err := campaignJSONBytes(tampered)
	campaignRequireNoError(t, err)
	campaignRequireNoError(t, os.WriteFile(resultPath, encoded, 0o600))
	if _, err := RecoverCampaignDirectory(directory, config); err == nil {
		t.Fatal("tampered call result recovered")
	}
	campaignRequireNoError(t, os.WriteFile(resultPath, original, 0o600))
	future := filepath.Join(directory, campaignModelCallsDir, campaignModelCallFile(2, "intent"))
	intentBytes, err := campaignJSONBytes(intent)
	campaignRequireNoError(t, err)
	campaignRequireNoError(t, os.WriteFile(future, intentBytes, 0o600))
	if _, err := RecoverCampaignDirectory(directory, config); err == nil {
		t.Fatal("future call recovered")
	}
	campaignRequireNoError(t, os.Remove(future))
	dispatchPath := filepath.Join(directory, campaignModelCallsDir, campaignModelCallFile(1, "dispatch"))
	dispatchBytes, err := os.ReadFile(dispatchPath)
	campaignRequireNoError(t, err)
	campaignRequireNoError(t, os.Remove(dispatchPath))
	if _, err := RecoverCampaignDirectory(directory, config); err == nil {
		t.Fatal("result recovered without dispatch")
	}
	campaignRequireNoError(t, os.WriteFile(dispatchPath, dispatchBytes, 0o600))
	missingRoot := t.TempDir()
	campaignRequireNoError(t, os.Mkdir(filepath.Join(missingRoot, campaignModelCallsDir), 0o700))
	if _, err := readCampaignModelCalls(
		missingRoot, config, []CampaignCheckpoint{{}, {}}, nil, new([]string),
	); err == nil {
		t.Fatal("committed call chain recovered without call files")
	}

	ambiguousConfig, ambiguousDirectory, active, ambiguousIntent := campaignModelCallFixture(t, "ambiguous")
	_, err = active.PrepareModelCall(ambiguousIntent)
	campaignRequireNoError(t, err)
	_, err = active.DispatchModelCall()
	campaignRequireNoError(t, err)
	ambiguous, err := RecoverCampaignDirectory(ambiguousDirectory, ambiguousConfig)
	if err != nil || ambiguous.ModelCalls[0].Status != CampaignModelCallAmbiguous {
		t.Fatalf("dispatch-only call was not ambiguous: %#v/%v", ambiguous.ModelCalls, err)
	}
	if _, err := ambiguous.DispatchModelCall(); err == nil ||
		!strings.Contains(err.Error(), "AMBIGUOUS") {
		t.Fatalf("ambiguous call was retried: %v", err)
	}
	if _, err := ambiguous.CommitModelCallResult(result); err == nil ||
		!strings.Contains(err.Error(), "AMBIGUOUS") {
		t.Fatalf("ambiguous call accepted a fabricated result: %v", err)
	}
}

func TestCampaignFailureAccountsMatchingDurableModelResult(t *testing.T) {
	config, directory, recovered, intent := campaignModelCallFixture(t, "failure-work")
	_, err := recovered.PrepareModelCall(intent)
	campaignRequireNoError(t, err)
	dispatch, err := recovered.DispatchModelCall()
	campaignRequireNoError(t, err)
	result, err := NewCampaignModelCallResult(intent, dispatch, CampaignModelCallResult{
		Status: CampaignModelCallCompleted, Content: []byte(`{"prefer":{}}`),
		ResponseDigest: strings.Repeat("a", 64),
		Response:       &AgentResponseIdentity{ID: "response", Model: "model", FinishReason: "stop"},
		DurationMillis: 1, Work: ModelWork{Calls: 1, InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
	})
	campaignRequireNoError(t, err)
	_, err = recovered.CommitModelCallResult(result)
	campaignRequireNoError(t, err)
	request, err := NewCampaignAttemptRequest(config, recovered.Head)
	campaignRequireNoError(t, err)
	fabricated := result
	fabricated.DurationMillis++
	fabricated, err = fabricated.seal()
	campaignRequireNoError(t, err)
	if _, err := recovered.FailAttemptWithModelCall(request, CampaignFailureProvider, fabricated); err == nil ||
		!strings.Contains(err.Error(), "MODEL_RESULT_MISMATCH") {
		t.Fatalf("fabricated accounting evidence was accepted: %v", err)
	}
	marker, err := recovered.FailAttemptWithModelCall(request, CampaignFailureProvider, result)
	campaignRequireNoError(t, err)
	want := AgentInvocationWork(emptyWork(), result.Work)
	if marker.Work != want || marker.EvidenceKind != CampaignFailureModelResult ||
		marker.EvidenceDigest != result.Digest || marker.BudgetExceeded {
		t.Fatalf("failure work was not evidence-bound: %#v", marker)
	}
	checked, err := RecoverCampaignDirectory(directory, config)
	campaignRequireNoError(t, err)
	summary, err := NewCampaignSummary(&checked)
	campaignRequireNoError(t, err)
	if summary.Sequence != 0 || len(summary.Attempts) != 0 || summary.Totals != want {
		t.Fatalf("terminal work invented an attempt or disappeared: %#v", summary)
	}
	failurePath := filepath.Join(directory, campaignFailureFile)
	tampered := marker
	tampered.EvidenceDigest = strings.Repeat("f", 64)
	tampered, err = tampered.seal()
	campaignRequireNoError(t, err)
	encoded, err := campaignJSONBytes(tampered)
	campaignRequireNoError(t, err)
	campaignRequireNoError(t, os.WriteFile(failurePath, encoded, 0o600))
	if _, err := RecoverCampaignDirectory(directory, config); err == nil ||
		!strings.Contains(err.Error(), "EVIDENCE_MISMATCH") {
		t.Fatalf("tampered accounting evidence recovered: %v", err)
	}
}

func campaignRequireNoError(t *testing.T, err error) {
	if err != nil {
		t.Fatal(err)
	}
}

func campaignModelCallFixture(
	t *testing.T,
	id string,
) (CampaignConfig, string, CampaignRecovery, CampaignModelCallIntent) {
	config := campaignTestConfig(t, "model-call-"+id, strings.Repeat("a", 64), CampaignLogicalBudget{
		MaxAttempts: 1, MaxPrimarySchedulerDecisions: 2, MaxPrimaryWorkUnits: 4, MaxReplayWorkUnits: 4,
		MaxModelCalls: 1, MaxModelTokens: 100,
	}, 1_000)
	config, err := RequireDurableCampaignPlanner(config)
	campaignRequireNoError(t, err)
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := CreateCampaignDirectory(directory, config)
	campaignRequireNoError(t, err)
	request, err := NewCampaignAttemptRequest(config, recovered.Head)
	campaignRequireNoError(t, err)
	intent, err := NewCampaignModelCallIntent(
		"model-call-"+id, request, strings.Repeat("b", 64), AgentTransportFreeze{
			Provider: "provider", Endpoint: "https://example.invalid", Model: "model",
			Thinking: "disabled", MaxOutputTokens: 32, MaxCallsPerArm: 1,
		}, []byte(`[{"role":"user"}]`), []byte(`{"model":"model"}`),
	)
	campaignRequireNoError(t, err)
	return config, directory, recovered, intent
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

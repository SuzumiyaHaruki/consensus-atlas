package controlexperiment

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestCampaignPlannerViewIsBoundMinimalAndPreferenceOnly(t *testing.T) {
	semantic := campaignPlannerSemantic(t)
	observation, request := campaignPlannerPrefix(t)
	baseline, err := NewGuardedTestIntent(GuardedTestIntent{
		ID: "planner-baseline", ViewDigest: semantic.Digest, RiskID: "risk",
		Must: IntentMust{Decisions: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := NewCampaignPlannerView("planner-view", semantic, observation, request, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := view.ValidateInputs(semantic, observation, request, baseline); err != nil {
		t.Fatal(err)
	}
	if len(view.Feedback.Attempts) != 2 || view.Feedback.Attempts[1].NewPSSStates != 0 ||
		view.Feedback.PSS == nil || view.Feedback.PSS.UniqueStates != 2 ||
		view.Request.Allowance.RemainingAttempts != 1 {
		t.Fatalf("planner projection drifted: %#v", view.Feedback)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"artifact_digest", "bundle_digest", "state_set_digest", "first_global_decision",
		"execution_instance_digest", "policy_seed", "monitor\":\"agreement\",\"step",
		"candidate", "root_cause",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("planner view exposed %q", forbidden)
		}
	}

	proposal, err := PlanDeterministicCampaignFixture(view)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ID != baseline.ID || len(proposal.Prefer.BackendIDs) != 2 ||
		proposal.Prefer.BackendIDs[0] != "backend-b" {
		t.Fatalf("fixture did not rotate after a zero-discovery attempt: %#v", proposal)
	}
	override := proposal
	override.Must.Decisions++
	override, err = NewGuardedTestIntent(override)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePreferenceOnlyProposal(semantic, baseline, override); err == nil {
		t.Fatal("hard-field override was accepted")
	}
	renamed := proposal
	renamed.ID = "renamed"
	renamed, err = NewGuardedTestIntent(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCampaignPlannerProposal(view, renamed); err == nil {
		t.Fatal("proposal identity change was accepted")
	}

	for name, mutate := range map[string]func(CampaignAttemptRequest) CampaignAttemptRequest{
		"identity": func(value CampaignAttemptRequest) CampaignAttemptRequest {
			value.TargetID = "other-target"
			return value
		},
		"head": func(value CampaignAttemptRequest) CampaignAttemptRequest {
			value.PreviousDigest = strings.Repeat("f", 64)
			return value
		},
		"ordinal": func(value CampaignAttemptRequest) CampaignAttemptRequest {
			value.Ordinal++
			return value
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed, err := mutate(request).seal()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewCampaignPlannerView("planner-view", semantic, observation, changed, baseline); err == nil ||
				!strings.Contains(err.Error(), "PREFIX_MISMATCH") {
				t.Fatalf("unbound request accepted: %v", err)
			}
		})
	}
}

func campaignPlannerSemantic(t *testing.T) AgentSemanticView {
	t.Helper()
	pack, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "planner-pack", Family: "fixture", Protocol: "fixture",
		Knowledge: []KnowledgeStatement{{ID: "knowledge", Text: "fixture knowledge"}},
		Risks: []ProtocolRisk{{
			ID: "risk", Summary: "fixture risk", RequiredCapabilities: []string{"capability"},
			RequiredActions:   []control.ActionKind{control.ActionInvoke},
			AllowedBackendIDs: []string{"backend-a", "backend-b"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := (AgentSemanticView{
		SchemaVersion: AgentSemanticViewVersion, ID: "planner-semantic", KnowledgePack: pack,
		CatalogDigest: strings.Repeat("a", 64), ProfileID: "profile",
		ProfileDigest: strings.Repeat("b", 64), AdapterID: "adapter",
		ValidatedCapabilities: []string{"capability"}, AvailableActions: []control.ActionKind{control.ActionInvoke},
		EligibleBackends: []IntentBackendView{
			{ID: "backend-a", SupportedActions: []control.ActionKind{control.ActionInvoke}, MinDecisions: 1, MaxDecisions: 4},
			{ID: "backend-b", SupportedActions: []control.ActionKind{control.ActionInvoke}, MinDecisions: 1, MaxDecisions: 4},
		},
	}).seal()
	if err != nil || semantic.Validate() != nil {
		t.Fatalf("semantic fixture invalid: %v", err)
	}
	return semantic
}

func campaignPlannerPrefix(t *testing.T) (CampaignObservation, CampaignAttemptRequest) {
	t.Helper()
	config := campaignTestConfig(t, "planner-campaign", strings.Repeat("a", 64), CampaignLogicalBudget{
		MaxAttempts: 3, MaxPrimarySchedulerDecisions: 3, MaxPrimaryWorkUnits: 6, MaxReplayWorkUnits: 6,
	}, 1_000)
	recovered, err := CreateCampaignDirectory(t.TempDir()+"/campaign", config)
	if err != nil {
		t.Fatal(err)
	}
	for ordinal := 1; ordinal <= 2; ordinal++ {
		artifact := []byte("planner-artifact-" + string(rune('0'+ordinal)))
		record, err := NewCampaignAttemptRecord(CampaignAttemptRecord{
			Ordinal: ordinal, ID: "planner-attempt-" + string(rune('0'+ordinal)),
			InputDigest: strings.Repeat("d", 64), ArtifactDigest: CampaignArtifactDigest(artifact),
			Outcome: CampaignAttemptCompleted, Work: campaignTestWork(1, 1, 0, 0),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := recovered.CommitAttempt(record, artifact, int64(ordinal)); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := NewCampaignSummary(&recovered)
	if err != nil {
		t.Fatal(err)
	}
	initial := campaignObservationState(t, "passive")
	changed := campaignObservationState(t, "coordinating")
	projections := []CampaignAttemptProjection{
		campaignObservationProjection(summary.Attempts[0], initial, changed),
		campaignObservationProjection(summary.Attempts[1], initial, initial),
	}
	for index := range projections {
		choice, err := (CampaignExecutionChoice{
			SchemaVersion: CampaignExecutionChoiceVersion,
			IntentDigest:  strings.Repeat("1", 64), PlanDigest: strings.Repeat("2", 64),
			ExecutionInstanceDigest: strings.Repeat(string(rune('3'+index)), 64),
			BackendID:               "backend-a", Strategy: "fixture-strategy", PolicySeed: uint64(index + 1),
		}).seal()
		if err != nil {
			t.Fatal(err)
		}
		projections[index].Choice = &choice
	}
	observation, err := NewCampaignObservation(summary, projections)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewCampaignAttemptRequest(config, recovered.Head)
	if err != nil {
		t.Fatal(err)
	}
	return observation, request
}

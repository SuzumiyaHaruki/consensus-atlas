package controlexperiment

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestRiskAgentRepairsUnsupportedCandidateFromMechanicalFeedback(t *testing.T) {
	knowledge := riskAgentFixtureKnowledge(t)
	capabilities := []semantic.ObservationCapability{
		{Kind: semantic.ObservationWorkloadInvoked, Fields: []semantic.ObservationField{
			semantic.ObservationFieldParticipantRole,
		}},
		{Kind: semantic.ObservationMessageDropped, Fields: []semantic.ObservationField{
			semantic.ObservationFieldOperationStage,
		}, Values: map[semantic.ObservationField][]string{
			semantic.ObservationFieldOperationStage: {"inflight"},
		}},
		{Kind: semantic.ObservationDecisionAdvanced},
	}
	actions := []control.ActionKind{control.ActionInvoke, control.ActionDropMessage}
	calls := 0
	planner := func(_ context.Context, view RiskAgentView) ([]byte, ModelWork, error) {
		calls++
		if err := view.Validate(); err != nil {
			t.Fatal(err)
		}
		candidate := RiskCandidate{
			ID: "recovery-after-restart", Summary: "Observe a decision after a participant restart.",
			Predicates: []semantic.ObservationPredicate{
				{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
				{MilestoneID: "restart", Kind: semantic.ObservationNodeRestarted},
			},
		}
		if calls == 1 {
			if view.Prior != nil {
				t.Fatal("first call received invented feedback")
			}
		} else {
			if view.Prior == nil || view.Prior.ReasonCode != RiskAgentReasonUnqualified ||
				len(view.Prior.Issues) != 2 ||
				view.Prior.Issues[0].Code != semantic.RiskIssueMissingAction ||
				view.Prior.Issues[1].Code != semantic.RiskIssueMissingObservationKind {
				t.Fatalf("repair call lost mechanical feedback: %#v", view.Prior)
			}
			candidate = RiskCandidate{
				ID:      "decision-after-inflight-message-loss",
				Summary: "Observe a later decision after one in-flight message is dropped.",
				Predicates: []semantic.ObservationPredicate{
					{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
					{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped,
						Constraints: []semantic.ObservationConstraint{{
							Field: semantic.ObservationFieldOperationStage, Equals: "inflight",
						}}},
					{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced},
				},
			}
		}
		encoded, err := json.Marshal(candidate)
		return encoded, ModelWork{Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5}, err
	}
	result, err := DiscoverRiskWithPlanner(
		context.Background(), RiskAgentBudget{MaxCalls: 2, MaxTokens: 20},
		knowledge, capabilities, actions, planner,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RiskAgentAccepted || result.Accepted == nil || len(result.Attempts) != 2 ||
		result.Attempts[0].Assessment == nil || result.Attempts[0].Assessment.Qualification.Qualified ||
		result.Attempts[0].Feedback.ReasonCode != RiskAgentReasonUnqualified ||
		!result.Accepted.Qualification.Qualified || result.Accepted.Spec.FamilyID != "paxos" ||
		len(result.Accepted.Spec.RequiredOrder) != 2 ||
		result.ModelWork != (ModelWork{Calls: 2, InputTokens: 6, OutputTokens: 4, TotalTokens: 10}) {
		t.Fatalf("unexpected repaired discovery: %#v", result)
	}
}

func TestRiskCandidateRejectsExistingRiskAndMeaninglessBinding(t *testing.T) {
	knowledge := riskAgentFixtureKnowledge(t)
	capabilities := []semantic.ObservationCapability{{
		Kind:   semantic.ObservationWorkloadInvoked,
		Fields: []semantic.ObservationField{semantic.ObservationFieldParticipantNode},
	}}
	actions := []control.ActionKind{control.ActionInvoke}
	existing := RiskCandidate{
		ID: "existing-risk", Summary: "Duplicate the curated risk.",
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "first", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "second", Kind: semantic.ObservationWorkloadInvoked},
		},
	}
	if _, err := AssessRiskCandidate(knowledge, existing, capabilities, actions); err == nil {
		t.Fatal("existing risk was accepted as a discovery")
	}
	meaningless := existing
	meaningless.ID = "single-use-binding"
	meaningless.Predicates[0].Constraints = []semantic.ObservationConstraint{{
		Field: semantic.ObservationFieldParticipantNode, BindAs: "node",
	}}
	if err := meaningless.Validate(); err == nil {
		t.Fatal("single-use entity binding was accepted")
	}
	encoded, err := json.Marshal(meaningless)
	if err != nil {
		t.Fatal(err)
	}
	result, err := DiscoverRiskWithPlanner(
		context.Background(), RiskAgentBudget{MaxCalls: 1, MaxTokens: 10},
		knowledge, capabilities, actions,
		func(context.Context, RiskAgentView) ([]byte, ModelWork, error) {
			return encoded, ModelWork{Calls: 1, InputTokens: 2, OutputTokens: 1, TotalTokens: 3}, nil
		},
	)
	if err != nil || len(result.Attempts) != 1 ||
		result.Attempts[0].Feedback.ReasonCode != RiskAgentReasonBinding {
		t.Fatalf("single-use binding feedback was not precise: %#v err=%v", result, err)
	}
}

func riskAgentFixtureKnowledge(t *testing.T) ProtocolKnowledgePack {
	t.Helper()
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "risk-agent-fixture-knowledge", Family: "paxos", Protocol: "fixture-paxos",
		Knowledge: []KnowledgeStatement{{ID: "rounds", Text: "Decisions follow ordered message and round activity."}},
		Risks: []ProtocolRisk{{
			ID: "existing-risk", Summary: "A curated existing risk.",
			RequiredCapabilities: []string{"runtime-message-control"},
			RequiredActions:      []control.ActionKind{control.ActionDropMessage, control.ActionInvoke},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return knowledge
}

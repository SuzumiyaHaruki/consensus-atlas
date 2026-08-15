package controlexperiment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
			ID: "recovery-after-restart", PropertyRef: "bounded-progress",
			InspirationRef: "message-loss-progress",
			Summary:        "Observe a decision after a participant restart.",
			SuspectedMechanism: "A restarted participant may fail to rejoin enough protocol activity " +
				"to permit a later decision.",
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
				ID: "decision-after-inflight-message-loss", PropertyRef: "bounded-progress",
				InspirationRef: "message-loss-progress",
				Summary:        "Observe a later decision after one in-flight message is dropped.",
				SuspectedMechanism: "Loss of one in-flight protocol message may prevent the remaining " +
					"participants from reaching a later decision.",
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
	legacyKnowledge := riskAgentLegacyFixtureKnowledge(t)
	capabilities := []semantic.ObservationCapability{{
		Kind:   semantic.ObservationWorkloadInvoked,
		Fields: []semantic.ObservationField{semantic.ObservationFieldParticipantNode},
	}}
	actions := []control.ActionKind{control.ActionInvoke}
	existing := RiskCandidate{
		ID: "existing-risk", PropertyRef: "bounded-progress", InspirationRef: "message-loss-progress",
		Summary:            "Duplicate the curated risk.",
		SuspectedMechanism: "A known message-loss mechanism may prevent a later decision.",
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "first", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "second", Kind: semantic.ObservationWorkloadInvoked},
		},
	}
	if _, err := AssessRiskCandidate(legacyKnowledge, existing, capabilities, actions); err == nil {
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

func TestAcceptedRiskCandidateClosesScenarioBridgeBounds(t *testing.T) {
	knowledge := riskAgentFixtureKnowledge(t)
	capabilities := []semantic.ObservationCapability{{Kind: semantic.ObservationWorkloadInvoked}}
	actions := []control.ActionKind{control.ActionInvoke}
	candidate := RiskCandidate{
		ID: "scenario-bridge-boundary", PropertyRef: "bounded-progress", InspirationRef: "original",
		Summary: "Exercise the exact Scenario bridge statement boundary.", SuspectedMechanism: "m",
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "first", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "second", Kind: semantic.ObservationWorkloadInvoked},
		},
	}
	statements, err := scenarioRiskCandidateKnowledge(candidate)
	if err != nil {
		t.Fatal(err)
	}
	candidate.SuspectedMechanism += strings.Repeat(
		"m", protocolKnowledgeTextMaxBytes-len(statements[0].Text),
	)
	if err := candidate.ValidateAgentDraft(); err != nil {
		t.Fatalf("exact bridge boundary was rejected: %v", err)
	}
	assessment, err := AssessRiskCandidate(knowledge, candidate, capabilities, actions)
	if err != nil || !assessment.Qualification.Qualified {
		t.Fatalf("boundary candidate was not accepted: %#v/%v", assessment, err)
	}
	bridge, err := BuildScenarioRiskHypothesis(knowledge, assessment, capabilities, actions)
	if err != nil || bridge.Knowledge.Validate() != nil {
		t.Fatalf("accepted boundary candidate did not close the Scenario bridge: %#v/%v", bridge, err)
	}

	tooLongMechanism := candidate
	tooLongMechanism.SuspectedMechanism += "m"
	if err := tooLongMechanism.ValidateAgentDraft(); !errors.Is(err, errRiskCandidateBridge) {
		t.Fatalf("oversized derived mechanism statement was accepted: %v", err)
	}

	tooLongPredicates := candidate
	tooLongPredicates.SuspectedMechanism = "m"
	tooLongPredicates.Predicates = cloneObservationPredicates(candidate.Predicates)
	tooLongPredicates.Predicates[0].Constraints = []semantic.ObservationConstraint{{
		Field: semantic.ObservationFieldRequestID, Equals: strings.Repeat("x", protocolKnowledgeTextMaxBytes),
	}}
	if err := tooLongPredicates.ValidateAgentDraft(); !errors.Is(err, errRiskCandidateBridge) {
		t.Fatalf("oversized derived predicate statement was accepted: %v", err)
	}
}

func TestAgentMaterialsBoundReferencesAndReserveOriginal(t *testing.T) {
	knowledge := riskAgentFixtureKnowledge(t)
	overlongProperty := knowledge
	overlongProperty.Properties = append([]ProtocolProperty(nil), knowledge.Properties...)
	overlongProperty.Properties[0].ID = strings.Repeat("p", agentMaterialReferenceMaxBytes+1)
	if _, err := NewProtocolKnowledgePack(overlongProperty); err == nil {
		t.Fatal("overlong Property reference was accepted")
	}

	reservedPattern := knowledge
	reservedPattern.IssuePatterns = append([]HistoricalIssuePattern(nil), knowledge.IssuePatterns...)
	reservedPattern.IssuePatterns[0].ID = "original"
	if _, err := NewProtocolKnowledgePack(reservedPattern); err == nil {
		t.Fatal("reserved original IssuePattern reference was accepted")
	}
}

func riskAgentFixtureKnowledge(t *testing.T) ProtocolKnowledgePack {
	t.Helper()
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "risk-agent-fixture-knowledge", Family: "paxos", Protocol: "fixture-paxos",
		Knowledge: []KnowledgeStatement{{ID: "rounds", Text: "Decisions follow ordered message and round activity."}},
		Properties: []ProtocolProperty{{
			ID: "bounded-progress", Summary: "An invoked operation eventually reaches a decision under the configured bound.",
		}},
		IssuePatterns: []HistoricalIssuePattern{{
			ID: "message-loss-progress", Summary: "Progress stalls after a protocol message is lost.",
			Mechanism: "An implementation may omit a retry or recovery transition after message loss.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := knowledge.ValidateAgentMaterials(); err != nil {
		t.Fatal(err)
	}
	return knowledge
}

func riskAgentLegacyFixtureKnowledge(t *testing.T) ProtocolKnowledgePack {
	t.Helper()
	active := riskAgentFixtureKnowledge(t)
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: active.ID, Family: active.Family, Protocol: active.Protocol,
		Knowledge:     append([]KnowledgeStatement(nil), active.Knowledge...),
		Properties:    append([]ProtocolProperty(nil), active.Properties...),
		IssuePatterns: append([]HistoricalIssuePattern(nil), active.IssuePatterns...),
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
	if knowledge.Validate() != nil || knowledge.ValidateAgentMaterials() == nil {
		t.Fatal("legacy curated Risk must remain valid outside the active Agent-material boundary")
	}
	return knowledge
}

package controlexperiment

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
			MechanismSteps: []RiskMechanismStep{
				{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Start the operation."},
				{MilestoneID: "restart", Kind: semantic.ObservationNodeRestarted, Rationale: "Observe participant restart."},
			},
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
				MechanismSteps: []RiskMechanismStep{
					{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Start the operation."},
					{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped, Rationale: "Drop one in-flight message."},
					{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced, Rationale: "Observe later decision progress."},
				},
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
		candidate = riskCandidateWithSupport(candidate, "primer/rounds")
		encoded, err := json.Marshal(candidate)
		return encoded, ModelWork{Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5}, err
	}
	result, err := DiscoverRiskWithPlanner(
		context.Background(), RiskAgentBudget{MaxCalls: 2, MaxTokens: 20},
		knowledge, capabilities, actions, nil, nil, nil, planner,
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

func TestRiskAgentReadsDeclaredKnowledgeBeforeSubmittingPortfolio(t *testing.T) {
	knowledge := riskAgentFixtureKnowledge(t)
	capabilities := []semantic.ObservationCapability{
		{Kind: semantic.ObservationWorkloadInvoked},
		{Kind: semantic.ObservationMessageDropped},
		{Kind: semantic.ObservationDecisionAdvanced},
	}
	actions := []control.ActionKind{control.ActionInvoke, control.ActionDropMessage}
	catalog, err := KnowledgeSourceCatalog(knowledge)
	if err != nil || len(catalog) != 1 {
		t.Fatalf("fixture source catalog invalid: %#v/%v", catalog, err)
	}
	readerCalls := 0
	reader := func(request KnowledgeReadRequest) (KnowledgeReadResult, error) {
		readerCalls++
		if request.Reference != catalog[0].Reference || request.MaxLines != 20 {
			t.Fatalf("reader received unrelated request: %#v", request)
		}
		return KnowledgeReadResult{
			Status: KnowledgeDiscoveryCompleted, Source: catalog[0], StartLine: 4, EndLine: 6,
			TotalLines: 12, Text: "func round() {\n  retryLostMessage()\n}", Truncated: true,
		}, nil
	}
	calls := 0
	planner := func(_ context.Context, view RiskAgentView) ([]byte, ModelWork, error) {
		calls++
		if err := view.Validate(); err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			if len(view.KnowledgeSources) != 1 || len(view.KnowledgeResults) != 0 ||
				view.MaxKnowledgeRequests != RiskKnowledgeRequestsPerCall ||
				slices.Contains(view.AvailableSupportRefs, "source/"+catalog[0].Reference) {
				t.Fatalf("first view lacked declared discovery surface: %#v", view)
			}
			encoded, marshalErr := json.Marshal(riskAgentResponseEnvelope{
				ResponseKind: riskAgentResponseKnowledgeQuery, Candidates: []RiskCandidate{},
				KnowledgeRequests: []KnowledgeReadRequest{{Reference: catalog[0].Reference, MaxLines: 20}},
			})
			return encoded, ModelWork{Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5}, marshalErr
		}
		if len(view.KnowledgeResults) != 1 || !strings.Contains(view.KnowledgeResults[0].Text, "retryLostMessage") ||
			view.Prior == nil || view.Prior.ReasonCode != RiskAgentReasonKnowledgeRead ||
			!slices.Contains(view.AvailableSupportRefs, "source/"+catalog[0].Reference) {
			t.Fatalf("read result did not return to the repair view: %#v", view)
		}
		candidate := RiskCandidate{
			ID: "decision-after-declared-retry", PropertyRef: "bounded-progress",
			InspirationRef: "message-loss-progress", Summary: "Observe a decision after message loss exercises retry.",
			MechanismSteps: []RiskMechanismStep{
				{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Start an operation."},
				{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped, Rationale: "Exercise the documented retry path."},
				{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced, Rationale: "Observe later decision progress."},
			},
			Predicates: []semantic.ObservationPredicate{
				{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
				{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped},
				{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced},
			},
		}
		candidate = riskCandidateWithSupport(candidate, "source/"+catalog[0].Reference)
		alternative := candidate
		alternative.ID = "decision-after-alternate-retry"
		encoded, marshalErr := json.Marshal(RiskCandidatePortfolio{Candidates: []RiskCandidate{candidate, alternative}})
		return encoded, ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7}, marshalErr
	}
	result, err := DiscoverRiskWithPlanner(
		context.Background(), RiskAgentBudget{MaxCalls: 2, MaxTokens: 20},
		knowledge, capabilities, actions, nil, nil, reader, planner,
	)
	if err != nil || result.Status != RiskAgentAccepted || result.Accepted == nil ||
		len(result.Attempts) != 2 || len(result.Attempts[0].KnowledgeResults) != 1 ||
		result.Attempts[0].Feedback.ReasonCode != RiskAgentReasonKnowledgeRead || readerCalls != 1 || calls != 2 ||
		result.ModelWork != (ModelWork{Calls: 2, InputTokens: 7, OutputTokens: 5, TotalTokens: 12}) {
		t.Fatalf("knowledge-assisted Risk loop failed: %#v calls=%d reads=%d err=%v", result, calls, readerCalls, err)
	}
}

func TestRiskAgentCanChooseAnotherDeclaredSourceAfterStoppedRead(t *testing.T) {
	knowledge := riskAgentFixtureKnowledge(t)
	knowledge.TargetDossier = cloneTargetDossier(knowledge.TargetDossier)
	knowledge.TargetDossier.Components[0].EvidenceRefs = append(
		knowledge.TargetDossier.Components[0].EvidenceRefs, "fixture.go:retry",
	)
	var err error
	knowledge, err = NewProtocolKnowledgePack(knowledge)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := KnowledgeSourceCatalog(knowledge)
	if err != nil || len(catalog) != 2 {
		t.Fatalf("two-source fixture invalid: %#v/%v", catalog, err)
	}
	byLocator := make(map[string]KnowledgeSource, len(catalog))
	for _, source := range catalog {
		byLocator[source.Locator] = source
	}
	readerCalls := 0
	reader := func(request KnowledgeReadRequest) (KnowledgeReadResult, error) {
		readerCalls++
		source := byLocator["round"]
		if readerCalls == 1 {
			if request.Reference != source.Reference {
				t.Fatalf("unexpected first source: %#v", request)
			}
			return stoppedKnowledgeRead(source, KnowledgeDiscoveryLocatorNotFound), nil
		}
		source = byLocator["retry"]
		if request.Reference != source.Reference {
			t.Fatalf("stopped read was not followed by another source: %#v", request)
		}
		return KnowledgeReadResult{
			Status: KnowledgeDiscoveryCompleted, Source: source, StartLine: 2, EndLine: 3,
			TotalLines: 3, Text: "func retry() {}\n", Truncated: true,
		}, nil
	}
	calls := 0
	planner := func(_ context.Context, view RiskAgentView) ([]byte, ModelWork, error) {
		calls++
		if err := view.Validate(); err != nil {
			t.Fatal(err)
		}
		if calls <= 2 {
			locator := "round"
			if calls == 2 {
				locator = "retry"
				if len(view.KnowledgeResults) != 1 ||
					view.KnowledgeResults[0].ReasonCode != KnowledgeDiscoveryLocatorNotFound ||
					view.MaxKnowledgeRequests != 1 || view.Prior == nil ||
					view.Prior.ReasonCode != RiskAgentReasonKnowledgeReadStopped {
					t.Fatalf("stopped result did not preserve one remaining choice: %#v", view)
				}
			}
			encoded, marshalErr := json.Marshal(riskAgentResponseEnvelope{
				ResponseKind: riskAgentResponseKnowledgeQuery, Candidates: []RiskCandidate{},
				KnowledgeRequests: []KnowledgeReadRequest{{
					Reference: byLocator[locator].Reference, MaxLines: 20,
				}},
			})
			return encoded, ModelWork{Calls: 1, InputTokens: 1, OutputTokens: 1, TotalTokens: 2}, marshalErr
		}
		if view.MaxKnowledgeRequests != 0 || len(view.KnowledgeResults) != 2 ||
			view.KnowledgeResults[1].Status != KnowledgeDiscoveryCompleted {
			t.Fatalf("completed result did not close source discovery: %#v", view)
		}
		candidate := RiskCandidate{
			ID: "decision-after-alternate-source", PropertyRef: "bounded-progress",
			InspirationRef: "message-loss-progress", Summary: "Observe a decision after message loss.",
			MechanismSteps: []RiskMechanismStep{
				{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Start work."},
				{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped, Rationale: "Exercise loss."},
				{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced, Rationale: "Observe progress."},
			},
			Predicates: []semantic.ObservationPredicate{
				{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
				{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped},
				{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced},
			},
		}
		candidate = riskCandidateWithSupport(candidate, "source/"+byLocator["retry"].Reference)
		encoded, marshalErr := json.Marshal(RiskCandidatePortfolio{Candidates: []RiskCandidate{candidate}})
		return encoded, ModelWork{Calls: 1, InputTokens: 1, OutputTokens: 1, TotalTokens: 2}, marshalErr
	}
	result, err := DiscoverRiskWithPlanner(
		context.Background(), RiskAgentBudget{MaxCalls: 3, MaxTokens: 30}, knowledge,
		[]semantic.ObservationCapability{
			{Kind: semantic.ObservationWorkloadInvoked},
			{Kind: semantic.ObservationMessageDropped},
			{Kind: semantic.ObservationDecisionAdvanced},
		},
		[]control.ActionKind{control.ActionInvoke, control.ActionDropMessage}, nil, nil, reader, planner,
	)
	if err != nil || result.Status != RiskAgentAccepted || result.Accepted == nil || calls != 3 || readerCalls != 2 {
		t.Fatalf("alternate-source discovery failed: %#v calls=%d reads=%d err=%v", result, calls, readerCalls, err)
	}
}

func TestRiskAgentResponseEnvelopeRequiresOneExclusiveBranch(t *testing.T) {
	portfolio, requests, query, err := parseRiskAgentResponse([]byte(
		`{"response_kind":"portfolio","candidates":[{"id":"candidate"}],"knowledge_requests":[]}`,
	))
	if err != nil || query || len(requests) != 0 || len(portfolio.Candidates) != 1 ||
		portfolio.Candidates[0].ID != "candidate" {
		t.Fatalf("direct envelope portfolio was not parsed: %#v/%#v/%v/%v", portfolio, requests, query, err)
	}
	_, _, _, err = parseRiskAgentResponse([]byte(
		`{"response_kind":"knowledge-query","candidates":[{"id":"candidate"}],"knowledge_requests":[{"reference":"fixture.go:round","max_lines":20}]}`,
	))
	if !errors.Is(err, errRiskCandidateJSON) {
		t.Fatalf("mixed envelope branches were accepted: %v", err)
	}
}

func TestRiskAgentSelectsFirstMechanicallyQualifiedPortfolioCandidate(t *testing.T) {
	knowledge := riskAgentFixtureKnowledge(t)
	capabilities := []semantic.ObservationCapability{
		{Kind: semantic.ObservationWorkloadInvoked},
		{Kind: semantic.ObservationMessageDropped},
		{Kind: semantic.ObservationDecisionAdvanced},
	}
	actions := []control.ActionKind{control.ActionInvoke, control.ActionDropMessage}
	qualified := RiskCandidate{
		ID: "decision-after-message-loss", PropertyRef: "bounded-progress",
		InspirationRef: "message-loss-progress",
		Summary:        "Observe a later decision after one protocol message is dropped.",
		MechanismSteps: []RiskMechanismStep{
			{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Start the operation."},
			{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped, Rationale: "Remove one protocol message."},
			{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced, Rationale: "Observe later decision progress."},
		},
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped},
			{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced},
		},
	}
	qualified = riskCandidateWithSupport(qualified, "primer/rounds")
	unknownProperty := qualified
	unknownProperty.ID = "unknown-property-hypothesis"
	unknownProperty.PropertyRef = "not-supplied"
	encoded, err := json.Marshal(RiskCandidatePortfolio{
		Candidates: []RiskCandidate{unknownProperty, qualified},
	})
	if err != nil {
		t.Fatal(err)
	}
	memory := []RiskExplorationMemoryEntry{{
		Episode: 1, CandidateID: "earlier-message-loss", Summary: "Earlier investigation.",
		SuspectedMechanism: "A message-loss ordering was investigated previously.",
		EpisodeOutcome:     RiskMemoryOutcomeRiskNearMiss, RiskStatus: semantic.RiskWitnessNotReached,
		SatisfiedMilestones: []string{"invoke"}, FirstMissingMilestone: "decision",
		ProtocolPSSStates: 3, NewProtocolPSSStates: 2,
		MechanicalReasonCodes: []string{RiskAgentReasonUnqualified},
		ModelCalls:            2, ModelTokens: 12, SearchWorkUnits: 7, ExecutionWorkUnits: 5,
	}}
	result, err := DiscoverRiskWithPlanner(
		context.Background(), RiskAgentBudget{MaxCalls: 1, MaxTokens: 10},
		knowledge, capabilities, actions, memory, nil, nil,
		func(_ context.Context, view RiskAgentView) ([]byte, ModelWork, error) {
			if len(view.ExplorationMemory) != 1 ||
				view.ExplorationMemory[0].FirstMissingMilestone != "decision" ||
				view.ExplorationMemory[0].NewProtocolPSSStates != 2 {
				t.Fatalf("trusted exploration memory was not supplied: %#v", view.ExplorationMemory)
			}
			view.ExplorationMemory[0].SatisfiedMilestones[0] = "mutated"
			return encoded, ModelWork{Calls: 1, InputTokens: 2, OutputTokens: 1, TotalTokens: 3}, nil
		},
	)
	if err != nil || result.Status != RiskAgentAccepted || result.Accepted == nil ||
		result.Accepted.Candidate.ID != qualified.ID || len(result.Attempts) != 1 ||
		len(result.Attempts[0].Feedback.Reviews) != 2 ||
		result.Attempts[0].Feedback.Reviews[0].ReasonCode != RiskAgentReasonProperty ||
		result.Attempts[0].Feedback.Reviews[1].Outcome != RiskCandidateQualified {
		t.Fatalf("portfolio was not mechanically reviewed in order: %#v/%v", result, err)
	}
	if memory[0].SatisfiedMilestones[0] != "invoke" {
		t.Fatal("Risk Agent mutated caller-owned exploration memory")
	}
	invisible := riskCandidateWithSupport(qualified, "source/fixture.go:round")
	invisibleBytes, err := json.Marshal(RiskCandidatePortfolio{Candidates: []RiskCandidate{invisible}})
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := DiscoverRiskWithPlanner(
		context.Background(), RiskAgentBudget{MaxCalls: 1, MaxTokens: 10},
		knowledge, capabilities, actions, nil, nil, nil,
		func(context.Context, RiskAgentView) ([]byte, ModelWork, error) {
			return invisibleBytes, ModelWork{Calls: 1, InputTokens: 1, OutputTokens: 1, TotalTokens: 2}, nil
		},
	)
	if err != nil || len(rejected.Attempts) != 1 ||
		rejected.Attempts[0].Feedback.ReasonCode != RiskAgentReasonSupport {
		t.Fatalf("unread source support was accepted: %#v/%v", rejected, err)
	}
}

func TestRiskAgentRepairsUnboundMechanismFromMechanicalFeedback(t *testing.T) {
	knowledge := riskAgentFixtureKnowledge(t)
	capabilities := []semantic.ObservationCapability{
		{Kind: semantic.ObservationWorkloadInvoked},
		{Kind: semantic.ObservationMessageDropped},
		{Kind: semantic.ObservationCoordinatorChange},
		{Kind: semantic.ObservationDecisionAdvanced},
	}
	actions := []control.ActionKind{control.ActionInvoke, control.ActionDropMessage}
	legacy := RiskCandidate{
		ID: "message-loss-coordinator-change", PropertyRef: "bounded-progress",
		InspirationRef: "message-loss-progress",
		Summary:        "A dropped message may affect progress after a coordinator change.",
		SuspectedMechanism: "A dropped accept message is lost before the coordinator changes, " +
			"but the executable predicates omit the message loss.",
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "change", Kind: semantic.ObservationCoordinatorChange},
			{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced},
		},
	}
	corrected := legacy
	corrected.SuspectedMechanism = ""
	corrected.Predicates = []semantic.ObservationPredicate{
		{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
		{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped},
		{MilestoneID: "change", Kind: semantic.ObservationCoordinatorChange},
		{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced},
	}
	corrected.MechanismSteps = []RiskMechanismStep{
		{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Start the operation."},
		{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped, Rationale: "Drop the relevant protocol message."},
		{MilestoneID: "change", Kind: semantic.ObservationCoordinatorChange, Rationale: "Observe the coordinator transition."},
		{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced, Rationale: "Observe subsequent decision progress."},
	}
	corrected = riskCandidateWithSupport(corrected, "primer/rounds")
	calls := 0
	result, err := DiscoverRiskWithPlanner(
		context.Background(), RiskAgentBudget{MaxCalls: 2, MaxTokens: 20},
		knowledge, capabilities, actions, nil, nil, nil,
		func(_ context.Context, view RiskAgentView) ([]byte, ModelWork, error) {
			calls++
			candidate := legacy
			if calls == 2 {
				if view.Prior == nil || view.Prior.ReasonCode != RiskAgentReasonAlignment {
					t.Fatalf("alignment feedback was not returned to the Agent: %#v", view.Prior)
				}
				candidate = corrected
			}
			encoded, marshalErr := json.Marshal(RiskCandidatePortfolio{Candidates: []RiskCandidate{candidate}})
			return encoded, ModelWork{Calls: 1, InputTokens: 2, OutputTokens: 2, TotalTokens: 4}, marshalErr
		},
	)
	if err != nil || result.Status != RiskAgentAccepted || result.Accepted == nil || calls != 2 ||
		len(result.Attempts) != 2 || result.Attempts[0].Feedback.ReasonCode != RiskAgentReasonAlignment ||
		result.Accepted.Candidate.ID != corrected.ID || len(result.Accepted.Candidate.MechanismSteps) != 4 ||
		!strings.Contains(result.Accepted.Candidate.SuspectedMechanism, "drop [message-dropped]") {
		t.Fatalf("unbound mechanism was not repaired: %#v/%v", result, err)
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
	meaningless.SuspectedMechanism = ""
	meaningless.MechanismSteps = []RiskMechanismStep{
		{MilestoneID: "first", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Observe the first invocation milestone."},
		{MilestoneID: "second", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Observe the second invocation milestone."},
	}
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
		knowledge, capabilities, actions, nil, nil, nil,
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
	if err != nil || bridge.Knowledge.Validate() != nil || bridge.Knowledge.TargetDossier == nil ||
		bridge.Knowledge.TargetDossier.Scope != knowledge.TargetDossier.Scope ||
		bridge.AcceptedHypothesis.Validate(bridge.Knowledge, bridge.Hypothesis, bridge.Spec) != nil {
		t.Fatalf("accepted boundary candidate did not close the Scenario bridge: %#v/%v", bridge, err)
	}
	tamperedContext := bridge.AcceptedHypothesis
	tamperedContext.Candidate.Predicates = cloneObservationPredicates(tamperedContext.Candidate.Predicates)
	tamperedContext.Candidate.Predicates[0].MilestoneID = "changed-milestone"
	if tamperedContext.Validate(bridge.Knowledge, bridge.Hypothesis, bridge.Spec) == nil {
		t.Fatal("accepted context drifted away from its executable witness")
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

	invalidEvidence := knowledge
	invalidEvidence.Properties = append([]ProtocolProperty(nil), knowledge.Properties...)
	invalidEvidence.Properties[0].EvidenceLevel = "model-confirmed"
	if _, err := NewProtocolKnowledgePack(invalidEvidence); err == nil {
		t.Fatal("unknown Property evidence level was accepted")
	}

	emptyEvidence := knowledge
	emptyEvidence.Properties = append([]ProtocolProperty(nil), knowledge.Properties...)
	emptyEvidence.Properties[0].EvidenceLevel = ""
	emptyEvidence, err := NewProtocolKnowledgePack(emptyEvidence)
	if err != nil || emptyEvidence.ValidateAgentMaterials() == nil {
		t.Fatalf("active Agent material accepted an unspecified evidence level: %v", err)
	}

	noPatterns := knowledge
	noPatterns.IssuePatterns = nil
	noPatterns, err = NewProtocolKnowledgePack(noPatterns)
	if err != nil || noPatterns.ValidateAgentMaterials() != nil {
		t.Fatalf("no-pattern Agent material was rejected: %v", err)
	}

	invalidDossier := knowledge
	invalidDossier.TargetDossier = cloneTargetDossier(knowledge.TargetDossier)
	invalidDossier.TargetDossier.Components[0].EvidenceRefs = []string{"", "source.go:1"}
	if _, err := NewProtocolKnowledgePack(invalidDossier); err == nil {
		t.Fatal("empty Target Dossier evidence reference was accepted")
	}
}

func riskCandidateWithSupport(candidate RiskCandidate, reference string) RiskCandidate {
	candidate.MechanismSteps = append([]RiskMechanismStep(nil), candidate.MechanismSteps...)
	for index := range candidate.MechanismSteps {
		candidate.MechanismSteps[index].SupportRefs = []string{reference}
	}
	return candidate
}

func riskAgentFixtureKnowledge(t *testing.T) ProtocolKnowledgePack {
	t.Helper()
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "risk-agent-fixture-knowledge", Family: "paxos", Protocol: "fixture-paxos",
		Knowledge: []KnowledgeStatement{{ID: "rounds", Text: "Decisions follow ordered message and round activity."}},
		Properties: []ProtocolProperty{{
			ID: "bounded-progress", Summary: "An invoked operation eventually reaches a decision under the configured bound.",
			EvidenceLevel: PropertyEvidenceHypothesis,
		}},
		IssuePatterns: []HistoricalIssuePattern{{
			ID: "message-loss-progress", Summary: "Progress stalls after a protocol message is lost.",
			Mechanism:     "An implementation may omit a retry or recovery transition after message loss.",
			Applicability: "A produced protocol message can be selected for loss.",
			Boundary:      "Delay alone is not a bounded liveness failure.",
		}},
		TargetDossier: &TargetDossier{
			Scope: "Fixture Paxos core under an in-memory controlled transport.",
			Components: []TargetMaterial{{
				ID: "fixture-core", Summary: "The fixture exposes ordered round and decision observations.",
				EvidenceRefs: []string{"fixture.go:round"},
			}},
			BlindSpots: []TargetMaterial{{
				ID: "no-storage", Summary: "The fixture has no durable storage model.",
			}},
		},
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
		TargetDossier: cloneTargetDossier(active.TargetDossier),
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

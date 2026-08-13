package controlexperiment

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestSemanticExplorerSealsAllRejectedAndPlannerFailureArtifacts(t *testing.T) {
	ctx, runtimeConfig, root, frontier := semanticBestFirstFixtureRoot(t)
	riskSpec := semanticBestFirstRiskSpec(t)
	knowledge, hypothesis := semanticExplorerKnowledgeAndHypothesis(t, riskSpec)
	searchSpec, err := NewStatelessDFSSpec(
		"fixture-a2b-terminal-search", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		2, len(frontier.Actions)+1, 12000,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	projector := fixtureSemanticPrefixProjector{preferred: frontier.Actions[len(frontier.Actions)-1].ActionID}

	t.Run("all-proposals-rejected", func(t *testing.T) {
		planner := func(context.Context, SemanticExplorerAgentView) ([]byte, ModelWork, error) {
			return []byte(`{"unexpected_authority":true}`), semanticExplorerFixtureWork(), nil
		}
		_, exploreErr := ExploreBoundedSemanticBestFirstWithExplorer(
			ctx, "fixture-a2b-all-rejected", SemanticExplorerBudget{MaxCalls: 2, MaxTokens: 100},
			knowledge, hypothesis, searchSpec, root, riskSpec, factory, projector, planner,
		)
		var failure *SemanticExplorerExecutionError
		if !errors.As(exploreErr, &failure) || failure.Failure.Validate() != nil ||
			failure.Failure.ReasonCode != SemanticExplorerFailureAllRejected ||
			len(failure.Failure.Calls) != 2 ||
			failure.Failure.ModelWork != (ModelWork{Calls: 2, InputTokens: 10, OutputTokens: 4, TotalTokens: 14}) {
			t.Fatalf("all-rejected terminal evidence was lost: %#v/%v", failure, exploreErr)
		}
		tampered := failure.Failure
		tampered.ReasonCode = SemanticExplorerFailureCallBudget
		if tampered.Validate() == nil {
			t.Fatal("all-rejected artifact could be relabeled as generic budget exhaustion")
		}
	})

	t.Run("planner-failed-after-dispatch", func(t *testing.T) {
		planner := func(context.Context, SemanticExplorerAgentView) ([]byte, ModelWork, error) {
			return nil, ModelWork{Calls: 1}, errors.New("fixture transport terminal")
		}
		_, exploreErr := ExploreBoundedSemanticBestFirstWithExplorer(
			ctx, "fixture-a2b-planner-failed", SemanticExplorerBudget{MaxCalls: 2, MaxTokens: 100},
			knowledge, hypothesis, searchSpec, root, riskSpec, factory, projector, planner,
		)
		var failure *SemanticExplorerExecutionError
		if !errors.As(exploreErr, &failure) || failure.Failure.Validate() != nil ||
			failure.Failure.ReasonCode != SemanticExplorerFailurePlanner ||
			failure.Failure.TerminalModelWork != (ModelWork{Calls: 1}) ||
			failure.Failure.ModelWork != (ModelWork{Calls: 1}) || len(failure.Failure.Calls) != 0 {
			t.Fatalf("planner terminal evidence was lost: %#v/%v", failure, exploreErr)
		}
	})
}

func TestSemanticExplorerRejectsRepairsExpandsAndReplaysWithoutPlanner(t *testing.T) {
	ctx, runtimeConfig, root, frontier := semanticBestFirstFixtureRoot(t)
	riskSpec := semanticBestFirstRiskSpec(t)
	knowledge, hypothesis := semanticExplorerKnowledgeAndHypothesis(t, riskSpec)
	searchSpec, err := NewStatelessDFSSpec(
		"fixture-a2b-explorer-search", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		2, 64, 30000,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	projector := fixtureSemanticPrefixProjector{preferred: frontier.Actions[len(frontier.Actions)-1].ActionID}
	invocations := 0
	planner := func(_ context.Context, view SemanticExplorerAgentView) ([]byte, ModelWork, error) {
		invocations++
		if view.Knowledge.Digest != knowledge.Digest || view.Hypothesis.Digest != hypothesis.Digest ||
			view.Request.Ordinal != invocations || view.Request.RemainingCalls != 11-invocations ||
			view.Request.RemainingTokens != 100-(invocations-1)*7 || view.Request.Validate() != nil {
			t.Fatalf("planner received an unbound semantic request: %#v", view)
		}
		assertSemanticQueueViewAllowlist(t, view.Request.Queue)
		assertSemanticExplorerViewDoesNotLeakAuthority(t, view)
		switch invocations {
		case 1:
			if view.Request.PriorFeedback != nil {
				t.Fatal("initial Explorer request unexpectedly contained feedback")
			}
			return []byte(`{"unexpected_authority":true}`), semanticExplorerFixtureWork(), nil
		case 2:
			if view.Request.PriorFeedback == nil ||
				view.Request.PriorFeedback.Outcome != SemanticExplorerFeedbackRejected ||
				view.Request.PriorFeedback.ReasonCode != SemanticExplorerReasonJSON ||
				view.Request.PriorFeedback.CurrentQueueDigest != view.Request.Queue.Digest {
				t.Fatalf("rejected proposal did not become exact same-queue feedback: %#v", view.Request)
			}
		case 3:
			if view.Request.PriorFeedback == nil ||
				view.Request.PriorFeedback.Outcome != SemanticExplorerFeedbackExpanded ||
				view.Request.PriorFeedback.SelectedCandidateID != semanticCandidateID(1) ||
				view.Request.PriorFeedback.CurrentQueueDigest != view.Request.Queue.Digest ||
				view.Request.PriorFeedback.PriorQueueDigest == view.Request.Queue.Digest {
				t.Fatalf("executed selection did not become next-queue feedback: %#v", view.Request)
			}
		}
		return semanticExplorerProposalWire(t, view.Request, semanticExplorerCandidateIDs(view.Request.Queue)),
			semanticExplorerFixtureWork(), nil
	}
	result, err := ExploreBoundedSemanticBestFirstWithExplorer(
		ctx, "fixture-single-explorer-v1", SemanticExplorerBudget{MaxCalls: 10, MaxTokens: 100},
		knowledge, hypothesis, searchSpec, root, riskSpec, factory, projector, planner,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Calls) != len(frontier.Actions)+1 || invocations != len(result.Calls) ||
		result.Calls[0].Status != SemanticExplorerCallRejected ||
		result.Calls[0].ReasonCode != SemanticExplorerReasonJSON ||
		result.Calls[1].Status != SemanticExplorerCallAccepted ||
		result.Calls[0].Request.Queue.Digest != result.Calls[1].Request.Queue.Digest ||
		len(result.Search.ExpansionOrder) != len(frontier.Actions) ||
		result.Search.ExpansionOrder[0] != semanticCandidateID(1) ||
		result.ModelWork != (ModelWork{Calls: 5, InputTokens: 25, OutputTokens: 10, TotalTokens: 35}) {
		t.Fatalf("Explorer rejection/repair/expansion chain is incomplete: %#v", result)
	}
	deterministic, err := ExploreBoundedSemanticBestFirst(
		ctx, searchSpec, root, riskSpec, factory, projector,
		NewDeterministicSemanticBestFirstGuidance(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if deterministic.ExpansionOrder[0] == result.Search.ExpansionOrder[0] ||
		deterministic.Search.Items[len(frontier.Actions)].ChildPrefixDigest ==
			result.Search.Search.Items[len(frontier.Actions)].ChildPrefixDigest {
		t.Fatalf("Explorer did not cause a real first-expanded Trace delta: deterministic=%#v explorer=%#v",
			deterministic.ExpansionOrder, result.Search.ExpansionOrder)
	}
	beforeReplayCalls := invocations
	if err := result.ValidateSources(ctx, root, riskSpec, knowledge, hypothesis, factory, projector); err != nil {
		t.Fatalf("Explorer result cannot be replayed from recorded calls: %v", err)
	}
	if invocations != beforeReplayCalls {
		t.Fatal("source validation called the planner instead of replaying exact accepted/rejected records")
	}
	var persisted SemanticExplorerResult
	roundTripJSON(t, result, &persisted)
	if err := persisted.ValidateSources(ctx, root, riskSpec, knowledge, hypothesis, factory, projector); err != nil {
		t.Fatalf("persisted Explorer result lost source replay: %v", err)
	}
}

func TestSemanticExplorerStrictProposalReasonsAndBindings(t *testing.T) {
	ctx, runtimeConfig, root, frontier := semanticBestFirstFixtureRoot(t)
	riskSpec := semanticBestFirstRiskSpec(t)
	knowledge, hypothesis := semanticExplorerKnowledgeAndHypothesis(t, riskSpec)
	searchSpec, err := NewStatelessDFSSpec(
		"fixture-a2b-boundary-search", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}, 2, 6, 5000,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	projector := fixtureSemanticPrefixProjector{preferred: frontier.Actions[len(frontier.Actions)-1].ActionID}
	var request SemanticExplorerRequest
	planner := func(_ context.Context, view SemanticExplorerAgentView) ([]byte, ModelWork, error) {
		request = view.Request
		return semanticExplorerProposalWire(t, request, semanticExplorerCandidateIDs(request.Queue)),
			semanticExplorerFixtureWork(), nil
	}
	result, err := ExploreBoundedSemanticBestFirstWithExplorer(
		ctx, "fixture-explorer-boundary-v1", SemanticExplorerBudget{MaxCalls: 4, MaxTokens: 50},
		knowledge, hypothesis, searchSpec, root, riskSpec, factory, projector, planner,
	)
	if err != nil {
		t.Fatal(err)
	}
	request = result.Calls[0].Request
	validIDs := semanticExplorerCandidateIDs(request.Queue)
	stale := SemanticExplorerProposal{
		SchemaVersion: SemanticExplorerProposalVersion, ID: request.ID,
		RequestDigest: result.Digest, QueueDigest: request.Queue.Digest,
		OrderedCandidateIDs: validIDs,
	}
	duplicate := append([]string(nil), validIDs...)
	duplicate[len(duplicate)-1] = duplicate[0]
	cases := []struct {
		name     string
		response []byte
		reason   string
	}{
		{name: "json-authority", response: []byte(`{"action_ids":[]}`), reason: SemanticExplorerReasonJSON},
		{name: "stale-binding", response: semanticExplorerRawProposal(t, stale), reason: SemanticExplorerReasonBinding},
		{name: "duplicate-candidate", response: semanticExplorerProposalWire(t, request, duplicate), reason: SemanticExplorerReasonCandidate},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			call, callErr := newSemanticExplorerCall(request, test.response, semanticExplorerFixtureWork())
			if callErr != nil || call.Status != SemanticExplorerCallRejected || call.ReasonCode != test.reason ||
				call.ValidateSource() != nil {
				t.Fatalf("strict proposal reason mismatch: call=%#v err=%v", call, callErr)
			}
		})
	}
	tampered := result
	tampered.Calls = cloneSemanticExplorerCalls(result.Calls)
	tampered.Calls[0].ModelWork.TotalTokens++
	tampered, err = tampered.seal()
	if err != nil {
		t.Fatal(err)
	}
	if tampered.Validate(root, riskSpec) == nil {
		t.Fatal("resealed Explorer result with detached model accounting was accepted")
	}
	if reflect.DeepEqual(result.Calls[0].ResponseBytes, []byte{}) {
		t.Fatal("Explorer call failed to preserve exact response bytes")
	}
}

func semanticExplorerKnowledgeAndHypothesis(
	t *testing.T,
	spec semantic.RiskWitnessSpec,
) (ProtocolKnowledgePack, TestHypothesis) {
	t.Helper()
	if spec.Validate() != nil {
		t.Fatal("invalid risk spec")
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-a2b-knowledge", Family: spec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{ID: "risk-order", Text: "Explore prefixes with different risk progress."}},
		Risks: []ProtocolRisk{{
			ID: spec.RiskID, Summary: "Fixture semantic prefix risk.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionCrash, control.ActionFireTemporal},
			AllowedBackendIDs:    []string{StatelessSearchBoundedDepthFirst, SemanticBestFirstAlgorithmID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"fixture-a2b-hypothesis", knowledge, spec,
		"Compare semantic progress across the bounded global queue.", SemanticBestFirstAlgorithmID,
	)
	if err != nil {
		t.Fatal(err)
	}
	return knowledge, hypothesis
}

func semanticExplorerCandidateIDs(view SemanticQueueView) []string {
	result := make([]string, len(view.Candidates))
	for index, candidate := range view.Candidates {
		result[index] = candidate.CandidateID
	}
	return result
}

func semanticExplorerProposalWire(
	t *testing.T,
	request SemanticExplorerRequest,
	ordered []string,
) []byte {
	t.Helper()
	return semanticExplorerRawProposal(t, SemanticExplorerProposal{
		SchemaVersion: SemanticExplorerProposalVersion, ID: request.ID,
		RequestDigest: request.Digest, QueueDigest: request.Queue.Digest,
		OrderedCandidateIDs: append([]string(nil), ordered...),
	})
}

func semanticExplorerRawProposal(t *testing.T, proposal SemanticExplorerProposal) []byte {
	t.Helper()
	encoded, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func semanticExplorerFixtureWork() ModelWork {
	return ModelWork{Calls: 1, InputTokens: 5, OutputTokens: 2, TotalTokens: 7}
}

func assertSemanticExplorerViewDoesNotLeakAuthority(t *testing.T, view SemanticExplorerAgentView) {
	t.Helper()
	assertJSONObjectKeys(t, view, map[string]bool{
		"knowledge": true, "hypothesis": true, "request": true,
	})
	requestKeys := map[string]bool{
		"schema_version": true, "id": true, "ordinal": true, "algorithm_id": true,
		"guidance_id": true, "knowledge_digest": true, "hypothesis_digest": true,
		"remaining_calls": true, "remaining_tokens": true, "queue": true,
		"mutable_fields": true, "digest": true,
	}
	if view.Request.PriorFeedback != nil {
		requestKeys["prior_feedback"] = true
	}
	assertJSONObjectKeys(t, view.Request, requestKeys)
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{
		[]byte("action_id"), []byte("work_item_digest"), []byte("evidence_digest"),
		[]byte("target_identity"), []byte("oracle"), []byte("verdict"), []byte("root_cause"),
	} {
		if jsonContains(encoded, forbidden) {
			t.Fatalf("semantic Explorer view leaked forbidden field %q", forbidden)
		}
	}
}

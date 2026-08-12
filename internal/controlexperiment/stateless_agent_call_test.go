package controlexperiment

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestStatelessAgentCallEvidenceBindsRequestDispatchAndTerminalResult(t *testing.T) {
	request := fixtureStatelessAgentCallRequest(t)
	transport := AgentTransportFreeze{
		Provider: "fixture", Endpoint: "https://example.invalid/v1", Model: "fixture-model",
		Thinking: "disabled", MaxOutputTokens: 200, MaxCallsPerArm: 1, MaxRetries: 0,
	}
	intent, err := NewStatelessAgentCallIntent(
		"fixture-call-1", 1, "fixture-root", request, transport,
		[]byte(`[{"role":"user","content":"fixture"}]`), []byte(`{"model":"fixture-model"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := NewStatelessAgentCallDispatch(intent)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := NewStatelessFrontierOrderProposal(
		request, fixtureFrontierActionIDs(request.Frontier),
	)
	if err != nil {
		t.Fatal(err)
	}
	wire := StatelessFrontierOrderProposal{
		SchemaVersion: StatelessFrontierOrderProposalVersion, ID: request.ID,
		RequestDigest: request.Digest, ViewDigest: request.Frontier.Digest,
		ActionIDs: proposal.ActionIDs,
	}
	content, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := NewStatelessAgentCallResult(intent, dispatch, StatelessAgentCallResult{
		Status: StatelessAgentCallCompleted, Content: content, ProposalDigest: proposal.Digest,
		ResponseDigest: strings.Repeat("4", 64),
		Response:       &AgentResponseIdentity{ID: "fixture-response", Model: "fixture-model", FinishReason: "stop"},
		DurationMillis: 1, Work: ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 2, TotalTokens: 6},
	})
	if err != nil || completed.ValidateInputs(intent, dispatch) != nil {
		t.Fatalf("completed result was not bound: %#v/%v", completed, err)
	}
	rejected, err := NewStatelessAgentCallResult(intent, dispatch, StatelessAgentCallResult{
		Status: StatelessAgentCallRejected, FailureCode: "frontier-proposal-rejected",
		Content: []byte(`{"wrong":true}`), ResponseDigest: strings.Repeat("5", 64),
		Response:       &AgentResponseIdentity{ID: "fixture-rejected", Model: "fixture-model", FinishReason: "stop"},
		DurationMillis: 1, Work: ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 1, TotalTokens: 5},
	})
	if err != nil || rejected.ValidateInputs(intent, dispatch) != nil {
		t.Fatalf("rejected result was not durable: %#v/%v", rejected, err)
	}
	tampered := completed
	tampered.ProposalDigest = strings.Repeat("6", 64)
	if tampered.ValidateInputs(intent, dispatch) == nil {
		t.Fatal("tampered terminal result was accepted")
	}
}

func fixtureFrontierActionIDs(frontier ActionFrontierView) []control.ActionID {
	ids := make([]control.ActionID, 0, len(frontier.Actions))
	for _, action := range frontier.Actions {
		ids = append(ids, action.ActionID)
	}
	return ids
}

func fixtureStatelessAgentCallRequest(t *testing.T) StatelessFrontierOrderRequest {
	t.Helper()
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "73746174656c6573732d63616c6c", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	frontier, _, err := ReconstructActionFrontierView(
		ctx, "fixture-call-frontier", root, 0, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		func() (control.Adapter, error) { return fixture.New(), nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge := fixtureStatelessAgentKnowledge(t)
	method, err := NewStatelessAgentTraversalMethod("fixture-call-method", "fixture-planner", knowledge)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewStatelessFrontierOrderRequest(
		"fixture-call-request", 1, method, knowledge, frontier, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

package controlexperiment

import (
	"strings"
	"testing"
)

func TestPlanningAgentCallIntentAndContentReadyRemainSchemaNeutral(t *testing.T) {
	requestDigest := strings.Repeat("3", 64)
	transport := AgentTransportFreeze{
		Provider: "fixture", Endpoint: "https://example.invalid/v1", Model: "fixture-model",
		Thinking: "disabled", MaxOutputTokens: 200, MaxCallsPerArm: 1, MaxRetries: 0,
	}
	intent, err := NewPlanningAgentCallIntent(
		"semantic-call-1", 1, "semantic-root", requestDigest, transport,
		[]byte(`[{"role":"user","content":"semantic"}]`), []byte(`{"model":"fixture-model"}`),
	)
	if err != nil || intent.ValidatePlanningRequestDigest(requestDigest) != nil {
		t.Fatalf("generic planning intent was not bound: %#v/%v", intent, err)
	}
	dispatch, err := NewStatelessAgentCallDispatch(intent)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"candidate_order":["candidate-1"]}`)
	ready, err := NewStatelessAgentCallResult(intent, dispatch, StatelessAgentCallResult{
		Status: StatelessAgentCallContentReady, Content: content,
		ResponseDigest: strings.Repeat("7", 64),
		Response:       &AgentResponseIdentity{ID: "fixture-semantic", Model: "fixture-model", FinishReason: "stop"},
		Work:           ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 2, TotalTokens: 6},
	})
	if err != nil || ready.ValidateInputs(intent, dispatch) != nil {
		t.Fatalf("content-ready result was not durable: %#v/%v", ready, err)
	}
	audit, err := NewStatelessAgentCallAudit(intent, &dispatch, &ready)
	if err != nil || audit.Status != StatelessAgentCallContentReady || audit.Work != ready.Work {
		t.Fatalf("content-ready audit was not sealed: %#v/%v", audit, err)
	}
	tampered := ready
	tampered.ProposalDigest = strings.Repeat("8", 64)
	if tampered.ValidateInputs(intent, dispatch) == nil {
		t.Fatal("content-ready transport result acquired proposal authority")
	}
}

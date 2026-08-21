package controlexperiment

import (
	"strings"
	"testing"
)

func TestPlanningAgentCallIntentAndContentReadyRemainSchemaNeutral(t *testing.T) {
	requestDigest := strings.Repeat("3", 64)
	transport := AgentTransportFreeze{
		Provider: "fixture", Endpoint: "https://example.invalid/v1", Model: "fixture-model",
		Thinking: "disabled", StructuredOutputMode: "json-object", RequestTimeoutMS: 1_000,
		RoutingPolicy: "fixture-fixed", MaxOutputTokens: 200, MaxCallsPerArm: 1, MaxRetries: 0,
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

func TestFailedPlanningCallCanPreserveObservedProviderResponse(t *testing.T) {
	requestDigest := strings.Repeat("3", 64)
	transport := AgentTransportFreeze{
		Provider: "fixture", Endpoint: "https://example.invalid/v1", Model: "fixture-model",
		Thinking: "low", StructuredOutputMode: "json-object", RequestTimeoutMS: 1_000,
		RoutingPolicy: "fixture-fixed", MaxOutputTokens: 200, MaxCallsPerArm: 1, MaxRetries: 0,
	}
	intent, err := NewPlanningAgentCallIntent(
		"scenario-call-1", 1, "scenario-root", requestDigest, transport,
		[]byte(`[{"role":"user","content":"repair"}]`), []byte(`{"model":"fixture-model"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := NewStatelessAgentCallDispatch(intent)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := NewStatelessAgentCallResult(intent, dispatch, StatelessAgentCallResult{
		Status: StatelessAgentCallFailed, FailureCode: "response-finish-length",
		ResponseDigest: strings.Repeat("7", 64),
		Response: &AgentResponseIdentity{
			ID: "fixture-truncated", Model: "fixture-model", FinishReason: "length",
		},
		Work:              ModelWork{Calls: 1, InputTokens: 40, OutputTokens: 16, TotalTokens: 56},
		TransportAttempts: 1, ProviderUsageStatus: "observed",
	})
	if err != nil || failed.ValidateInputs(intent, dispatch) != nil || failed.Response == nil ||
		failed.Response.FinishReason != "length" || failed.FailureCode != "response-finish-length" {
		t.Fatalf("known failed response was not durable: %#v/%v", failed, err)
	}
	tampered := failed
	tampered.Response = nil
	if tampered.ValidateInputs(intent, dispatch) == nil {
		t.Fatal("response-known failure was accepted after dropping only its response identity")
	}
	for name, result := range map[string]StatelessAgentCallResult{
		"empty identity": {
			Status: StatelessAgentCallContentReady, Content: []byte(`{}`),
			ResponseDigest: strings.Repeat("7", 64), Response: &AgentResponseIdentity{},
			Work: ModelWork{Calls: 1, InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		},
		"length mismatch": {
			Status: StatelessAgentCallFailed, FailureCode: "response-finish-length",
			ResponseDigest: strings.Repeat("7", 64), Response: &AgentResponseIdentity{
				ID: "fixture-truncated", Model: "fixture-model", FinishReason: "stop",
			},
			Work:                ModelWork{Calls: 1, InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
			ProviderUsageStatus: "observed",
		},
	} {
		if _, err := NewStatelessAgentCallResult(intent, dispatch, result); err == nil {
			t.Fatalf("%s response metadata was accepted", name)
		}
	}
}

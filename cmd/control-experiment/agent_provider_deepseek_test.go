package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestDeepSeekProviderUsesFixedJSONObjectTransport(t *testing.T) {
	const secret = "deepseek-fixture-secret"
	response := []byte(`{
  "id":"deepseek-response","model":"deepseek-v4-flash-0731",
  "choices":[{"index":0,"message":{"role":"assistant","content":"{\"action_ids\":[\"a\"]}"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}
}`)
	client := newDeepSeekIntentClient("")
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != deepSeekChatEndpoint || request.Header.Get("Authorization") != "Bearer "+secret {
			t.Fatalf("unexpected DeepSeek request metadata: %s", request.URL)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte(secret)) || !bytes.Contains(body, []byte("local validation is authoritative")) {
			t.Fatal("DeepSeek request leaked a credential or omitted the local-schema boundary")
		}
		var payload deepSeekChatRequest
		if json.Unmarshal(body, &payload) != nil || payload.Model != deepSeekDefaultModel ||
			payload.ResponseFormat.Type != "json_object" || payload.Thinking.Type != "enabled" ||
			payload.ReasoningEffort != "high" || payload.Stream {
			t.Fatalf("unexpected DeepSeek payload: %#v", payload)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	prepared, err := client.prepare("return JSON", "public-user", fixtureOpenRouterStructuredOutput())
	if err != nil {
		t.Fatal(err)
	}
	call, err := client.invokePrepared(context.Background(), secret, prepared)
	if err != nil || call.FailureCode != "" || call.UsageStatus != agentProviderUsageObserved ||
		call.Work != (controlexperiment.ModelWork{Calls: 1, InputTokens: 20, OutputTokens: 8, TotalTokens: 28}) ||
		string(call.Content) != `{"action_ids":["a"]}` {
		t.Fatalf("unexpected DeepSeek call: %#v/%v", call, err)
	}
	freeze := client.freeze()
	if freeze.Provider != deepSeekProvider || freeze.StructuredOutputMode != "json-object" ||
		freeze.RoutingPolicy != "provider-fixed" || freeze.AllowProviderFallback {
		t.Fatalf("DeepSeek method identity drifted: %#v", freeze)
	}
}

func TestDeepSeekProviderKeepsAmbiguousUsageExplicit(t *testing.T) {
	client := newDeepSeekIntentClient(deepSeekDefaultModel)
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	prepared, err := client.prepare("return JSON", "public-user", fixtureOpenRouterStructuredOutput())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	call, err := client.invokePrepared(ctx, "fixture-key", prepared)
	if err != nil || call.FailureCode != agentFailureTransport || call.TransportAttempts != 1 ||
		call.UsageStatus != agentProviderUsageUnknown || call.Work != (controlexperiment.ModelWork{Calls: 1}) {
		t.Fatalf("ambiguous DeepSeek call was misclassified: %#v/%v", call, err)
	}
}

func TestDeepSeekProviderClassifiesLengthAndKeepsResponseIdentity(t *testing.T) {
	response := []byte(`{
  "id":"deepseek-truncated","model":"deepseek-v4-flash-0731",
  "choices":[{"index":0,"message":{"role":"assistant","content":"{\"intent\":\"revise\""},"finish_reason":"length"}],
  "usage":{"prompt_tokens":40,"completion_tokens":16,"total_tokens":56}
}`)
	client := newDeepSeekIntentClient(deepSeekDefaultModel)
	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	prepared, err := client.prepare("return JSON", "public-user", fixtureOpenRouterStructuredOutput())
	if err != nil {
		t.Fatal(err)
	}
	call, err := client.invokePrepared(context.Background(), "fixture-key", prepared)
	if err != nil || call.FailureCode != agentFailureResponseFinishLength || len(call.Content) != 0 ||
		call.Response == nil || call.Response.ID != "deepseek-truncated" ||
		call.Response.FinishReason != "length" || call.UsageStatus != agentProviderUsageObserved ||
		call.Work != (controlexperiment.ModelWork{Calls: 1, InputTokens: 40, OutputTokens: 16, TotalTokens: 56}) ||
		call.ResponseDigest == "" {
		t.Fatalf("truncated response identity or accounting was lost: %#v/%v", call, err)
	}
}

func TestDurableJournalPreservesKnownLengthFailure(t *testing.T) {
	response := []byte(`{
  "id":"deepseek-journal-truncated","model":"deepseek-v4-flash-0731",
  "choices":[{"index":0,"message":{"role":"assistant","content":"{\"intent\":\"continue\""},"finish_reason":"length"}],
  "usage":{"prompt_tokens":30,"completion_tokens":12,"total_tokens":42}
}`)
	client := newDeepSeekIntentClient(deepSeekDefaultModel)
	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "journal")
	journal, err := newStatelessAgentCallJournal(directory, client, "fixture-key")
	if err != nil || journal.SetRoot("fixture-root") != nil {
		t.Fatalf("journal setup failed: %v", err)
	}
	prepared, err := client.prepare("return JSON", "public-user", fixtureOpenRouterStructuredOutput())
	if err != nil {
		t.Fatal(err)
	}
	_, work, callErr := journal.planningCall(context.Background(), planningAgentCallPlan{
		intentID: "scenario-call-1", requestDigest: strings.Repeat("3", 64),
		prepared: prepared, contentReady: true,
	})
	if statelessAgentFailureCode(callErr) != statelessAgentFailureFinishLength ||
		work != (controlexperiment.ModelWork{Calls: 1, InputTokens: 30, OutputTokens: 12, TotalTokens: 42}) {
		t.Fatalf("journal lost the typed length failure: work=%#v err=%v", work, callErr)
	}
	var persisted controlexperiment.StatelessAgentCallResult
	if err := readStrictJSONFile(
		filepath.Join(directory, "model-calls", "001-fixture-root", "result.json"), 2<<20, &persisted,
	); err != nil || persisted.Status != controlexperiment.StatelessAgentCallFailed ||
		persisted.FailureCode != statelessAgentFailureFinishLength || persisted.Response == nil ||
		persisted.Response.ID != "deepseek-journal-truncated" || persisted.Response.FinishReason != "length" ||
		persisted.ProviderUsageStatus != agentProviderUsageObserved {
		t.Fatalf("durable length failure lost provider evidence: %#v/%v", persisted, err)
	}
}

func TestDurableJournalRecoversExactScenarioLengthRepairSequence(t *testing.T) {
	binding := strings.Repeat("a", 64)
	lengthResponse := []byte(`{
  "id":"length-before-repair","model":"deepseek-v4-flash-0731",
  "choices":[{"index":0,"message":{"role":"assistant","content":"{\"intent\":\"revise\""},"finish_reason":"length"}],
  "usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}
}`)
	successResponse := []byte(`{
  "id":"repaired-response","model":"deepseek-v4-flash-0731",
  "choices":[{"index":0,"message":{"role":"assistant","content":"{\"intent\":\"abandon\"}"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}
}`)
	responses := [][]byte{lengthResponse, successResponse}
	providerCalls := 0
	client := newDeepSeekIntentClient(deepSeekDefaultModel)
	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		response := responses[providerCalls]
		providerCalls++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	prepared, err := client.prepare("return JSON", "public-user", fixtureOpenRouterStructuredOutput())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "scenario-journal")
	journal, err := newStatelessAgentCallJournal(directory, client, "fixture-key")
	if err != nil || journal.SetRoot("scenario-root") != nil {
		t.Fatal(err)
	}
	firstPlan := planningAgentCallPlan{
		intentID:      "scenario-agent-call-1-binding-" + binding,
		requestDigest: binding, prepared: prepared, contentReady: true,
	}
	if _, _, err := journal.planningCall(context.Background(), firstPlan); statelessAgentFailureCode(err) != statelessAgentFailureFinishLength {
		t.Fatalf("initial length failure missing: %v", err)
	}

	// Interruption immediately after the failed result is recoverable.
	recovered, err := recoverStatelessAgentCallJournal(directory, client)
	if err != nil || recovered.SetRoot("scenario-root") != nil {
		t.Fatalf("length failure was not recoverable: %#v/%v", recovered, err)
	}
	if _, _, err := recovered.planningCall(context.Background(), firstPlan); statelessAgentFailureCode(err) != statelessAgentFailureFinishLength {
		t.Fatalf("recovered length result drifted: %v", err)
	}
	repairPlan := planningAgentCallPlan{
		intentID:      "scenario-agent-repair-call-2-from-1-binding-" + binding,
		requestDigest: binding, prepared: prepared, contentReady: true,
	}
	if _, _, err := recovered.planningCall(context.Background(), repairPlan); !errors.Is(err, errStatelessAgentCallKeyRequired) {
		t.Fatalf("repair intent was not durably prepared: %v", err)
	}

	// A crash after dispatch but before a durable provider result remains an
	// explicit ambiguous call: recovery accepts the exact repair sequence but
	// never redispatches a request whose remote outcome is unknown.
	ambiguousDirectory := filepath.Join(t.TempDir(), "scenario-journal")
	if err := os.MkdirAll(filepath.Join(ambiguousDirectory, "model-calls", "001-scenario-root"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ambiguousDirectory, "model-calls", "002-scenario-root"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"intent.json", "dispatch.json", "result.json"} {
		data, err := os.ReadFile(filepath.Join(directory, "model-calls", "001-scenario-root", name))
		if err != nil {
			t.Fatalf("copy first call evidence %s: %v", name, err)
		}
		if err := os.WriteFile(
			filepath.Join(ambiguousDirectory, "model-calls", "001-scenario-root", name), data, 0o600,
		); err != nil {
			t.Fatalf("copy first call evidence %s: %v", name, err)
		}
	}
	repairCallDirectory := filepath.Join(directory, "model-calls", "002-scenario-root")
	var repairIntent controlexperiment.StatelessAgentCallIntent
	if err := readStrictJSONFile(filepath.Join(repairCallDirectory, "intent.json"), 128<<10, &repairIntent); err != nil {
		t.Fatal(err)
	}
	repairDispatch, err := controlexperiment.NewStatelessAgentCallDispatch(repairIntent)
	if err != nil || writeStatelessAgentJSON(
		filepath.Join(ambiguousDirectory, "model-calls", "002-scenario-root"), "intent.json", repairIntent,
	) != nil || writeStatelessAgentJSON(
		filepath.Join(ambiguousDirectory, "model-calls", "002-scenario-root"), "dispatch.json", repairDispatch,
	) != nil {
		t.Fatalf("prepare ambiguous repair evidence: %v", err)
	}
	ambiguous, err := recoverStatelessAgentCallJournal(ambiguousDirectory, client)
	if err != nil || ambiguous.SetRoot("scenario-root") != nil {
		t.Fatalf("dispatched repair was not structurally recoverable: %#v/%v", ambiguous, err)
	}
	if _, _, err := ambiguous.planningCall(context.Background(), firstPlan); statelessAgentFailureCode(err) != statelessAgentFailureFinishLength {
		t.Fatalf("ambiguous sequence lost first failure: %v", err)
	}
	if _, _, err := ambiguous.planningCall(context.Background(), repairPlan); err == nil ||
		!strings.Contains(err.Error(), "RECOVERED_AMBIGUOUS") {
		t.Fatalf("ambiguous repair was silently redispatched: %v", err)
	}

	// Interruption after the repair intent is also recoverable, and replaying
	// both calls performs only the previously undispatched repair request.
	recovered, err = recoverStatelessAgentCallJournal(directory, client)
	if err != nil || recovered.SetRoot("scenario-root") != nil {
		t.Fatalf("repair intent was not recoverable: %#v/%v", recovered, err)
	}
	if _, _, err := recovered.planningCall(context.Background(), firstPlan); statelessAgentFailureCode(err) != statelessAgentFailureFinishLength {
		t.Fatalf("first call replay drifted: %v", err)
	}
	if err := recovered.ActivateKey("fixture-key"); err != nil {
		t.Fatal(err)
	}
	content, work, err := recovered.planningCall(context.Background(), repairPlan)
	if err != nil || string(content) != `{"intent":"abandon"}` || work.TotalTokens != 14 || providerCalls != 2 {
		t.Fatalf("repair dispatch did not complete exactly once: %q/%#v calls=%d err=%v",
			content, work, providerCalls, err)
	}
	if terminal, err := recoverStatelessAgentCallJournal(directory, client); err != nil || len(terminal.recovered) != 2 {
		t.Fatalf("completed repair sequence was not recoverable: %#v/%v", terminal, err)
	}

	// A repair intent not immediately bound to a length failure is rejected.
	invalidDirectory := filepath.Join(t.TempDir(), "invalid-repair")
	invalid, err := newStatelessAgentCallJournal(invalidDirectory, client, "")
	if err != nil || invalid.SetRoot("scenario-root") != nil {
		t.Fatal(err)
	}
	invalid.recovered = nil
	if _, _, err := invalid.planningCall(context.Background(), repairPlan); !errors.Is(err, errStatelessAgentCallKeyRequired) {
		t.Fatalf("invalid repair intent setup failed: %v", err)
	}
	if _, err := recoverStatelessAgentCallJournal(invalidDirectory, client); err == nil {
		t.Fatal("unbound repair intent was accepted")
	}
}

func TestAgentProviderSelectionDoesNotFallbackAcrossProviders(t *testing.T) {
	deepseek, err := newAgentIntentTransport(deepSeekProvider, "")
	if err != nil || deepseek.freeze().Provider != deepSeekProvider || deepseek.freeze().AllowProviderFallback {
		t.Fatalf("DeepSeek provider selection drifted: %#v/%v", deepseek, err)
	}
	openrouter, err := newAgentIntentTransport(openRouterProvider, openRouterFixtureModel)
	if err != nil || openrouter.freeze().Provider != openRouterProvider || !openrouter.freeze().AllowProviderFallback {
		t.Fatalf("OpenRouter provider selection drifted: %#v/%v", openrouter, err)
	}
	if _, err := newAgentIntentTransport("automatic", deepSeekDefaultModel); err == nil {
		t.Fatal("cross-provider automatic routing was accepted")
	}
}

func TestScenarioDeepSeekTransportUsesLowThinkingAndBoundsOutput(t *testing.T) {
	transport, err := newScenarioAgentIntentTransport(deepSeekProvider, deepSeekDefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	freeze := transport.freeze()
	if freeze.Provider != deepSeekProvider || freeze.Thinking != "low" ||
		freeze.MaxOutputTokens != scenarioAgentMaxOutputTokens || freeze.AllowProviderFallback {
		t.Fatalf("Scenario transport was not independently bounded: %#v", freeze)
	}
	prepared, err := transport.prepare(
		"return JSON", "public-user", fixtureOpenRouterStructuredOutput(),
	)
	if err != nil {
		t.Fatal(err)
	}
	var request deepSeekChatRequest
	if json.Unmarshal(prepared.RequestBytes, &request) != nil ||
		request.Thinking.Type != "enabled" || request.ReasoningEffort != "low" ||
		request.MaxTokens != scenarioAgentMaxOutputTokens {
		t.Fatalf("Scenario request did not use its frozen reasoning settings: %#v", request)
	}
}

func TestScenarioPromptFeedbackOmitsRepeatedTracePayload(t *testing.T) {
	encoded, err := json.Marshal(compactScenarioPromptSteps([]controlexperiment.ScenarioStepFeedback{{
		StepID: "drop-response", Outcome: controlexperiment.ScenarioStepRejected,
		ReasonCode: "selector-no-match", Decision: 41, ViewDigest: strings.Repeat("1", 64),
		MatchCount: 0,
		Choice: &controlexperiment.FrontierChoice{
			ID: "old-choice", ViewDigest: strings.Repeat("2", 64), Decision: 40,
		},
		Available: []controlexperiment.FrontierActionRef{{ActionID: "enabled-action"}},
		SelectorTrace: []controlexperiment.ScenarioSelectorFilter{{
			Field: "message_target", Requested: "n1", CandidateCount: 0,
		}},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "choice") || strings.Contains(text, "view_digest") ||
		strings.Contains(text, "risk_progress") || !strings.Contains(text, "selector_trace") ||
		!strings.Contains(text, "available_actions") {
		t.Fatalf("compact feedback kept repeated trace payload or lost the rejection cause: %s", text)
	}
}

func TestAgenticSessionWallClockCreatesRealDeadline(t *testing.T) {
	ctx, cancel, err := agenticSessionContext(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	select {
	case <-ctx.Done():
		if ctx.Err() != context.DeadlineExceeded {
			t.Fatalf("unexpected session termination: %v", ctx.Err())
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("configured session wall clock did not cancel work")
	}
}

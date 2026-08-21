package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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

func TestScenarioDeepSeekTransportUsesHighThinkingAndBoundsOutput(t *testing.T) {
	transport, err := newScenarioAgentIntentTransport(deepSeekProvider, deepSeekDefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	freeze := transport.freeze()
	if freeze.Provider != deepSeekProvider || freeze.Thinking != "high" ||
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
		request.Thinking.Type != "enabled" || request.ReasoningEffort != "high" ||
		request.MaxTokens != scenarioAgentMaxOutputTokens {
		t.Fatalf("Scenario request did not use its frozen reasoning settings: %#v", request)
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

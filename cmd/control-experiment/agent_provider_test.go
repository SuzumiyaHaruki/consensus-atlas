package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type agentHTTPDoerFunc func(*http.Request) (*http.Response, error)

const openRouterFixtureModel = "fixture/model"

func fixtureOpenRouterIntentClient() openRouterIntentClient {
	return newOpenRouterIntentClient(openRouterFixtureModel)
}

func fixtureOpenRouterStructuredOutput() openRouterStructuredOutput {
	return openRouterStructuredOutput{
		Name:   "fixture_output_v1",
		Schema: json.RawMessage(`{"type":"object","additionalProperties":true}`),
	}
}

func fixtureOpenRouterResponse(t *testing.T, ordinal int, content []byte) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"id": "fixture-response-" + string(rune('0'+ordinal)), "model": openRouterFixtureModel,
		"system_fingerprint": "fixture-provider", "choices": []any{map[string]any{
			"index": 0, "message": map[string]any{"role": "assistant", "content": string(content)},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{"prompt_tokens": 4, "completion_tokens": 3, "total_tokens": 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func buildOmnipaxosScenarioWorker(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required for the OmniPaxos Scenario test")
	}
	manifest := filepath.Join("..", "..", "adapters", "omnipaxosv2", "worker", "Cargo.toml")
	command := exec.Command("cargo", "build", "--locked", "--quiet", "--manifest-path", manifest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build OmniPaxos worker: %v\n%s", err, output)
	}
	path, err := filepath.Abs(filepath.Join(
		"..", "..", "adapters", "omnipaxosv2", "worker", "target", "debug",
		"consensus-atlas-omnipaxos-worker",
	))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func (function agentHTTPDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestOpenRouterProviderFreezesExactRequestAndChargesAcceptedResponse(t *testing.T) {
	const secret = "test-secret-never-persist"
	responseJSON := []byte(`{
  "id":"mock-response",
  "model":"fixture/model-20260813",
  "system_fingerprint":"mock-fingerprint",
  "choices":[{"index":0,"message":{"role":"assistant","content":"{\"action_ids\":[\"a\"]}"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}
}`)
	client := openRouterIntentClient{
		Endpoint: openRouterChatEndpoint, Model: openRouterFixtureModel,
		ReasoningEffort: openRouterDefaultReasoningEffort, ExcludeReasoning: true,
		MaxOutputTokens: openRouterDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || request.URL.String() != openRouterChatEndpoint ||
				request.Header.Get("Authorization") != "Bearer "+secret ||
				request.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected request metadata: %s %s", request.Method, request.URL)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(body, []byte(secret)) || !bytes.Contains(body, []byte("public-user")) {
				t.Fatal("request leaked a credential or lost frozen public content")
			}
			var payload openRouterChatRequest
			if err := json.Unmarshal(body, &payload); err != nil ||
				payload.Model != openRouterFixtureModel ||
				payload.MaxTokens != openRouterDefaultTokens ||
				payload.Reasoning.Effort != openRouterDefaultReasoningEffort ||
				!payload.Reasoning.Exclude || payload.ResponseFormat.Type != "json_schema" ||
				!payload.ResponseFormat.JSONSchema.Strict ||
				payload.ResponseFormat.JSONSchema.Name != "fixture_output_v1" ||
				!payload.Provider.RequireParameters {
				t.Fatalf("unexpected OpenRouter payload: %#v/%v", payload, err)
			}
			return &http.Response{
				StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(responseJSON)),
			}, nil
		}),
	}
	clock := time.Unix(100, 0)
	client.Now = func() time.Time {
		current := clock
		clock = clock.Add(7 * time.Millisecond)
		return current
	}
	prepared, err := client.prepare("public-system", "public-user", fixtureOpenRouterStructuredOutput())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(prepared.RequestBytes, []byte(secret)) || prepared.PromptDigest == "" ||
		prepared.RequestDigest == "" {
		t.Fatal("prepared request did not preserve the pre-credential boundary")
	}
	call, err := client.invokePrepared(context.Background(), secret, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if call.FailureCode != "" || call.Response == nil || call.Response.ID != "mock-response" ||
		call.UsageStatus != agentProviderUsageObserved ||
		call.Response.Model != "fixture/model-20260813" ||
		call.Response.FinishReason != "stop" || call.Work != (controlexperiment.ModelWork{
		Calls: 1, InputTokens: 100, OutputTokens: 50, TotalTokens: 150,
	}) || call.DurationMillis != 7 || string(call.Content) != `{"action_ids":["a"]}` {
		t.Fatalf("unexpected model call: %#v", call)
	}
}

func TestOpenRouterProviderRecordsSingleFailureWithoutDiagnostics(t *testing.T) {
	client := openRouterIntentClient{
		Endpoint: openRouterChatEndpoint, Model: openRouterFixtureModel,
		ReasoningEffort: openRouterDefaultReasoningEffort, ExcludeReasoning: true,
		MaxOutputTokens: openRouterDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("provider diagnostic that must not be persisted")
		}),
	}
	prepared, err := client.prepare("public-system", "public-user", fixtureOpenRouterStructuredOutput())
	if err != nil {
		t.Fatal(err)
	}
	call, err := client.invokePrepared(context.Background(), "test-secret", prepared)
	if err != nil {
		t.Fatal(err)
	}
	if call.FailureCode != agentFailureTransport || call.Work.Calls != 1 ||
		call.RequestDigest == "" || call.ResponseDigest != "" || call.Response != nil {
		t.Fatalf("unexpected transport failure: %#v", call)
	}

	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(errorReader{})}, nil
	})
	call, err = client.invokePrepared(context.Background(), "test-secret", prepared)
	if err != nil {
		t.Fatal(err)
	}
	if call.FailureCode != agentFailureTransport || call.ResponseDigest != "" || call.Response != nil ||
		call.UsageStatus != agentProviderUsageUnknown {
		t.Fatalf("unexpected unreadable response: %#v", call)
	}
}

func TestOpenRouterProviderNeverRetriesAmbiguousPOST(t *testing.T) {
	attempts := 0
	client := newOpenRouterIntentClient(openRouterFixtureModel)
	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("completion and provider billing are ambiguous")
	})
	prepared, err := client.prepare("public-system", "public-user", fixtureOpenRouterStructuredOutput())
	if err != nil {
		t.Fatal(err)
	}
	call, err := client.invokePrepared(context.Background(), "test-secret", prepared)
	if err != nil || call.FailureCode != agentFailureTransport || call.TransportAttempts != 1 ||
		attempts != 1 || call.Work != (controlexperiment.ModelWork{Calls: 1}) ||
		call.UsageStatus != agentProviderUsageUnknown ||
		openRouterTransportFreeze(client).MaxRetries != 0 {
		t.Fatalf("ambiguous POST was retried or mis-accounted: %#v attempts=%d err=%v", call, attempts, err)
	}

	attempts = 0
	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":"unavailable"}`))),
		}, nil
	})
	call, err = client.invokePrepared(context.Background(), "test-secret", prepared)
	if err != nil || call.FailureCode != agentFailureHTTP || call.TransportAttempts != 1 || attempts != 1 {
		t.Fatalf("HTTP failure was retried: %#v attempts=%d err=%v", call, attempts, err)
	}
}

func TestOpenRouterModelSelectionUsesOneTransport(t *testing.T) {
	for _, model := range []string{
		"vendor-a/model-a",
		"vendor-b/model-b.1",
		"router/model-c:fast",
	} {
		client := newOpenRouterIntentClient(model)
		prepared, err := client.prepare("public-system", "public-user", fixtureOpenRouterStructuredOutput())
		if err != nil {
			t.Fatalf("model %q was not accepted: %v", model, err)
		}
		var payload openRouterChatRequest
		if err := json.Unmarshal(prepared.RequestBytes, &payload); err != nil ||
			payload.Model != model || openRouterTransportFreeze(client).Validate() != nil {
			t.Fatalf("model %q changed transport shape: %#v/%v", model, payload, err)
		}
	}

	client := newOpenRouterIntentClient(openRouterFixtureModel)
	client.Endpoint = "https://example.invalid/chat/completions"
	if _, err := client.prepare("public-system", "public-user", fixtureOpenRouterStructuredOutput()); err == nil {
		t.Fatal("non-OpenRouter endpoint was accepted")
	}
	client = newOpenRouterIntentClient("invalid model")
	if _, err := client.prepare("public-system", "public-user", fixtureOpenRouterStructuredOutput()); err == nil {
		t.Fatal("invalid OpenRouter model ID was accepted")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("unreadable") }
func (errorReader) Close() error             { return nil }

func TestReadAgentKeyRejectsLoosePermissionsSymlinkAndMultipleLines(t *testing.T) {
	directory := t.TempDir()
	keyPath := filepath.Join(directory, "key.txt")
	if err := os.WriteFile(keyPath, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := readAgentKey(keyPath)
	if err != nil || key != "secret" {
		t.Fatalf("secure key read = %q/%v", key, err)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAgentKey(keyPath); err == nil || !strings.Contains(err.Error(), "KEY_FILE_INVALID") {
		t.Fatalf("loose permissions accepted: %v", err)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(directory, "key-link.txt")
	if err := os.Symlink(keyPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readAgentKey(linkPath); err == nil || !strings.Contains(err.Error(), "KEY_FILE_INVALID") {
		t.Fatalf("symlink accepted: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("first\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAgentKey(keyPath); err == nil || !strings.Contains(err.Error(), "KEY_FILE_CONTENT_INVALID") {
		t.Fatalf("multiple lines accepted: %v", err)
	}
}

package main

import (
	"bytes"
	"context"
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

type agentHTTPDoerFunc func(*http.Request) (*http.Response, error)

func (function agentHTTPDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestDeepSeekProviderFreezesExactRequestAndChargesAcceptedResponse(t *testing.T) {
	const secret = "test-secret-never-persist"
	responseJSON := []byte(`{
  "id":"mock-response",
  "model":"deepseek-v4-flash",
  "system_fingerprint":"mock-fingerprint",
  "choices":[{"index":0,"message":{"role":"assistant","content":"{\"action_ids\":[\"a\"]}"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}
}`)
	client := deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || request.URL.String() != deepSeekChatEndpoint ||
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
	prepared, err := client.prepare("public-system", "public-user")
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
		call.Response.FinishReason != "stop" || call.Work != (controlexperiment.ModelWork{
		Calls: 1, InputTokens: 100, OutputTokens: 50, TotalTokens: 150,
	}) || call.DurationMillis != 7 || string(call.Content) != `{"action_ids":["a"]}` {
		t.Fatalf("unexpected model call: %#v", call)
	}
}

func TestDeepSeekProviderRecordsBoundedFailuresWithoutDiagnostics(t *testing.T) {
	client := deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("provider diagnostic that must not be persisted")
		}),
	}
	prepared, err := client.prepare("public-system", "public-user")
	if err != nil {
		t.Fatal(err)
	}
	call, err := client.invokePrepared(context.Background(), "test-secret", prepared)
	if err != nil {
		t.Fatal(err)
	}
	if call.FailureCode != deepSeekFailureTransport || call.Work.Calls != 1 ||
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
	if call.FailureCode != deepSeekFailureResponse || call.ResponseDigest != "" || call.Response != nil {
		t.Fatalf("unexpected unreadable response: %#v", call)
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

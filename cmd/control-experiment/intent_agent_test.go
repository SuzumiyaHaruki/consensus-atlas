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

type agentHTTPDoerFunc func(*http.Request) (*http.Response, error)

func (function agentHTTPDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestDeepSeekIntentClientUsesBlindViewAndBoundedJSONRequest(t *testing.T) {
	inputs, err := newEtcdraftIntentInputs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	proposal := controlexperiment.GuardedTestIntent{
		SchemaVersion: controlexperiment.GuardedTestIntentVersion,
		ID:            "mock-one-shot", ViewDigest: inputs.View.Digest,
		RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{Decisions: 96, FaultEnvelope: controlexperiment.FaultEnvelope{
			MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
			MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
		}},
		Prefer: controlexperiment.IntentPrefer{BackendIDs: []string{etcdraftBackendActionClass}},
	}
	proposalJSON, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	responseJSON, err := json.Marshal(map[string]any{
		"id": "mock-response", "model": deepSeekV4Flash, "object": "chat.completion",
		"system_fingerprint": "mock-fingerprint",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop", "logprobs": nil,
			"message": map[string]any{"role": "assistant", "content": string(proposalJSON)},
		}},
		"usage": map[string]any{
			"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150,
			"prompt_cache_hit_tokens": 0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	const secret = "test-secret-never-persist"
	client := deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || request.URL.String() != deepSeekChatEndpoint ||
				request.Header.Get("Authorization") != "Bearer "+secret ||
				request.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected request metadata: %s %s", request.Method, request.URL)
			}
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if bytes.Contains(body, []byte(secret)) || bytes.Contains(bytes.ToLower(body), []byte("build_id")) ||
				bytes.Contains(bytes.ToLower(body), []byte("root_cause")) {
				t.Fatal("request leaked a secret or defect identity")
			}
			var sent deepSeekChatRequest
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Fatal(err)
			}
			if sent.Model != deepSeekV4Flash || sent.ResponseFormat.Type != "json_object" ||
				sent.Thinking.Type != "disabled" || sent.Temperature != 0 || sent.Stream ||
				sent.MaxTokens != deepSeekDefaultTokens || len(sent.Messages) != 2 ||
				!strings.Contains(sent.Messages[1].Content, "AgentSemanticView JSON") {
				t.Fatalf("unexpected bounded request: %#v", sent)
			}
			parts := strings.SplitN(sent.Messages[1].Content, "\n\nAgentSemanticView JSON:\n", 2)
			if len(parts) != 2 || !strings.Contains(parts[0], "fictional-structure-only") ||
				strings.Contains(parts[0], inputs.View.Digest) ||
				strings.Contains(parts[0], etcdraftBackendActionClass) {
				t.Fatal("structural example anchors the model to a real view choice")
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
	call, err := client.invoke(context.Background(), secret, inputs.View)
	if err != nil {
		t.Fatal(err)
	}
	if call.FailureCode != "" || call.Response == nil || call.Response.ID != "mock-response" ||
		call.Response.FinishReason != "stop" || call.Work != (controlexperiment.ModelWork{
		Calls: 1, InputTokens: 100, OutputTokens: 50, TotalTokens: 150,
	}) || call.DurationMillis != 7 || len(call.Content) == 0 {
		t.Fatalf("unexpected model call: %#v", call)
	}
	intent, err := controlexperiment.ParseGuardedTestIntentProposal(call.Content)
	if err != nil {
		t.Fatal(err)
	}
	if intent.ViewDigest != inputs.View.Digest || intent.Digest == "" {
		t.Fatalf("unexpected parsed intent: %#v", intent)
	}
}

func TestDeepSeekIntentClientRecordsFailuresWithoutLeakingTransportErrors(t *testing.T) {
	inputs, err := newEtcdraftIntentInputs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client := deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("provider diagnostic that must not be persisted")
		}),
	}
	call, err := client.invoke(context.Background(), "test-secret", inputs.View)
	if err != nil {
		t.Fatal(err)
	}
	if call.FailureCode != deepSeekFailureTransport || call.Work.Calls != 1 ||
		call.RequestDigest == "" || call.ResponseDigest != "" || call.Response != nil {
		t.Fatalf("unexpected transport failure: %#v", call)
	}
}

func TestEtcdraftAgentOneShotUnreadableResponseIsNotTransportFailure(t *testing.T) {
	client := deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(errorReader{})}, nil
		}),
	}
	result, err := executeEtcdraftAgentOneShot(context.Background(), "test-secret", client)
	if err == nil || !strings.Contains(err.Error(), deepSeekFailureResponse) {
		t.Fatalf("unreadable response error = %v", err)
	}
	if result.Audit.Status != controlexperiment.AgentInvocationResponseRejected ||
		result.Audit.ResponseDigest != "" || result.Audit.FailureCode != deepSeekFailureResponse {
		t.Fatalf("unreadable response audit = %#v", result.Audit)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("unreadable") }

func (errorReader) Close() error { return nil }

func TestEtcdraftAgentOneShotMockCompilesExecutesAndPersists(t *testing.T) {
	inputs, err := newEtcdraftIntentInputs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	proposal := controlexperiment.GuardedTestIntent{
		SchemaVersion: controlexperiment.GuardedTestIntentVersion,
		ID:            "mock-executed-one-shot", ViewDigest: inputs.View.Digest,
		RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{Decisions: 96, FaultEnvelope: controlexperiment.FaultEnvelope{
			MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
			MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
		}},
		Prefer: controlexperiment.IntentPrefer{BackendIDs: []string{etcdraftBackendActionClass}},
	}
	proposalJSON, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	responseJSON, err := json.Marshal(map[string]any{
		"id": "mock-executed-response", "model": deepSeekV4Flash,
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop",
			"message": map[string]any{"role": "assistant", "content": string(proposalJSON)},
		}},
		"usage": map[string]any{"prompt_tokens": 90, "completion_tokens": 30, "total_tokens": 120},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(responseJSON))}, nil
		}),
	}
	result, err := executeEtcdraftAgentOneShot(context.Background(), "test-secret", client)
	if err != nil {
		t.Fatal(err)
	}
	if result.Audit.Status != controlexperiment.AgentInvocationCompleted || result.Intent == nil ||
		result.Plan == nil || result.Report == nil || result.Bundle == nil ||
		result.Audit.Work.Model.TotalTokens != 120 || result.Audit.CompilerWork.WorkUnits != 3 ||
		result.Audit.ReportDigest != result.Report.Digest || result.Audit.BundleDigest != result.Bundle.Digest {
		t.Fatalf("unexpected completed one-shot: %#v", result.Audit)
	}
	directory := filepath.Join(t.TempDir(), "one-shot")
	if err := persistEtcdraftAgentOneShot(directory, result); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"audit.json", "view.json", "intent.json", "plan.json", "report.json", "bundle.json"} {
		encoded, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte("test-secret")) {
			t.Fatalf("artifact %s contains key", name)
		}
	}
	if err := persistEtcdraftAgentOneShot(directory, result); err == nil ||
		!strings.Contains(err.Error(), "DIRECTORY_NOT_NEW") {
		t.Fatalf("artifact overwrite was allowed: %v", err)
	}
}

func TestEtcdraftAgentOneShotInvalidIntentProducesDurableAudit(t *testing.T) {
	responseJSON := []byte(`{
  "id":"mock-invalid-response",
  "model":"deepseek-v4-flash",
  "choices":[{"index":0,"message":{"role":"assistant","content":"{\"oracle\":\"pass\"}"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10}
}`)
	client := deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(responseJSON))}, nil
		}),
	}
	result, err := executeEtcdraftAgentOneShot(context.Background(), "test-secret", client)
	if err == nil || !strings.Contains(err.Error(), deepSeekFailureIntent) {
		t.Fatalf("invalid intent error = %v", err)
	}
	if result.Audit.Status != controlexperiment.AgentInvocationIntentRejected ||
		result.Audit.FailureCode != deepSeekFailureIntent || result.Intent != nil ||
		result.Audit.Work.Model.TotalTokens != 10 {
		t.Fatalf("unexpected rejected audit: %#v", result.Audit)
	}
	directory := filepath.Join(t.TempDir(), "rejected")
	if err := persistEtcdraftAgentOneShot(directory, result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "intent.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected intent artifact unexpectedly exists: %v", err)
	}
}

func TestCheckedAgentOneShotSummaryRecompilesAndReexecutes(t *testing.T) {
	encoded, err := os.ReadFile("../../benchmarks/experiments/etcdraft-v2-agent-one-shot-m5.18b1/summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Classification string                              `json:"classification"`
		ViewDigest     string                              `json:"view_digest"`
		AcceptedIntent controlexperiment.GuardedTestIntent `json:"accepted_intent"`
		Compiled       struct {
			PlanDigest string `json:"plan_digest"`
		} `json:"compiled"`
		Execution struct {
			ReportDigest     string `json:"report_digest"`
			BundleDigest     string `json:"bundle_digest"`
			Decisions        int    `json:"decisions"`
			PrimaryWorkUnits int    `json:"primary_work_units"`
			ReplayWorkUnits  int    `json:"replay_work_units"`
			CorePSSStates    int    `json:"core_pss_states"`
			PrefixArea       int64  `json:"prefix_area"`
		} `json:"execution"`
	}
	if err := json.Unmarshal(encoded, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Classification != "public-transport-calibration-only" {
		t.Fatalf("unexpected classification: %s", summary.Classification)
	}
	inputs, err := newEtcdraftIntentInputs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.ViewDigest != inputs.View.Digest || summary.AcceptedIntent.ViewDigest != inputs.View.Digest {
		t.Fatal("checked summary is bound to a different Agent view")
	}
	if err := summary.AcceptedIntent.Validate(); err != nil {
		t.Fatal(err)
	}
	plan, err := controlexperiment.CompileGuardedTestIntent(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, summary.AcceptedIntent,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Digest != summary.Compiled.PlanDigest {
		t.Fatalf("compiled plan = %s, want %s", plan.Digest, summary.Compiled.PlanDigest)
	}
	report, bundle, err := executeEtcdraftCompiledIntent(
		context.Background(), inputs, summary.AcceptedIntent, plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Digest != summary.Execution.ReportDigest || bundle.Digest != summary.Execution.BundleDigest ||
		report.StateDiscovery.TotalDecisions != summary.Execution.Decisions ||
		report.Work.Primary.WorkUnits != summary.Execution.PrimaryWorkUnits ||
		report.Work.Replay.WorkUnits != summary.Execution.ReplayWorkUnits ||
		report.StateDiscovery.UniqueStates != summary.Execution.CorePSSStates ||
		report.StateDiscovery.PrefixArea != summary.Execution.PrefixArea {
		t.Fatalf("checked summary drifted: report=%s bundle=%s discovery=%#v work=%#v",
			report.Digest, bundle.Digest, report.StateDiscovery, report.Work)
	}
}

func TestReadAgentKeyRejectsLoosePermissionsAndSymlink(t *testing.T) {
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
}

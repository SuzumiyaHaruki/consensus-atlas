package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestM523R4StatelessAgentJournalResumesPreparedAndCompletedCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	view := statelessAgentRecoveryView(t, ctx)
	response := statelessAgentRecoveryResponse(t, view)
	calls := 0
	client := defaultDeepSeekIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "agent-attempt")
	journal, err := newStatelessAgentCallJournal(directory, client, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.SetRoot("fixture-root"); err != nil {
		t.Fatal(err)
	}
	if _, work, err := journal.Planner(ctx, view); err == nil ||
		!strings.Contains(err.Error(), "KEY_REQUIRED") || work != (controlexperiment.ModelWork{}) || calls != 0 {
		t.Fatalf("prepared call crossed key boundary: work=%#v calls=%d err=%v", work, calls, err)
	}
	recovered, err := recoverStatelessAgentCallJournal(directory, client)
	if err != nil || recovered.SetRoot("fixture-root") != nil || recovered.ActivateKey("resume-key") != nil {
		t.Fatalf("prepared call did not recover: %#v/%v", recovered, err)
	}
	content, work, err := recovered.Planner(ctx, view)
	if err != nil || calls != 1 || len(content) == 0 || work.Calls != 1 || work.TotalTokens != 7 {
		t.Fatalf("prepared call did not dispatch exactly once: work=%#v calls=%d err=%v", work, calls, err)
	}
	completed, err := recoverStatelessAgentCallJournal(directory, client)
	if err != nil || completed.SetRoot("fixture-root") != nil {
		t.Fatalf("completed call did not recover: %#v/%v", completed, err)
	}
	replayed, replayWork, err := completed.Planner(ctx, view)
	if err != nil || calls != 1 || !bytes.Equal(replayed, content) || replayWork != work {
		t.Fatalf("completed call invoked transport again: work=%#v calls=%d err=%v", replayWork, calls, err)
	}
}

func TestM523R4StatelessAgentJournalRejectsAmbiguousDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	view := statelessAgentRecoveryView(t, ctx)
	calls := 0
	client := defaultDeepSeekIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, nil
	})
	directory := filepath.Join(t.TempDir(), "agent-attempt")
	journal, err := newStatelessAgentCallJournal(directory, client, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.SetRoot("fixture-root"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Planner(ctx, view); err == nil {
		t.Fatal("fixture did not freeze a prepared call")
	}
	callDirectory := filepath.Join(directory, "model-calls", "001-fixture-root")
	var intent controlexperiment.StatelessAgentCallIntent
	if err := readM523gJSON(filepath.Join(callDirectory, "intent.json"), 128<<10, &intent, true); err != nil {
		t.Fatal(err)
	}
	dispatch, err := controlexperiment.NewStatelessAgentCallDispatch(intent)
	if err != nil || writeStatelessAgentJSON(callDirectory, "dispatch.json", dispatch) != nil {
		t.Fatalf("fixture dispatch failed: %v", err)
	}
	recovered, err := recoverStatelessAgentCallJournal(directory, client)
	if err != nil || recovered.SetRoot("fixture-root") != nil || recovered.ActivateKey("forbidden-key") != nil {
		t.Fatalf("ambiguous call did not recover: %#v/%v", recovered, err)
	}
	if _, work, err := recovered.Planner(ctx, view); err == nil ||
		!strings.Contains(err.Error(), "AMBIGUOUS") || work.Calls != 1 || calls != 0 {
		t.Fatalf("ambiguous call was retried: work=%#v calls=%d err=%v", work, calls, err)
	}
}

func statelessAgentRecoveryView(
	t *testing.T,
	ctx context.Context,
) controlexperiment.StatelessSearchAgentView {
	t.Helper()
	_, source, err := etcdraftExecution(ctx, "workload-risk-witness-calibration", 64, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	root, err := controlexperiment.ExecutionTracePrefix(source.Trace, 0)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, err := controlexperiment.ReconstructActionFrontierView(
		ctx, "etcdraft-stateless-agent-recovery-frontier", root, 0,
		etcdraftCampaignRuntimeConfig(),
		&controlexperiment.FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		func() (control.Adapter, error) { return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig()) },
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := etcdraftStatelessAgentKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	method, err := controlexperiment.NewStatelessAgentTraversalMethod(
		"etcdraft-stateless-agent-recovery", "fixture-frontier-order", knowledge,
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := controlexperiment.NewStatelessFrontierOrderRequest(
		"etcdraft-stateless-agent-recovery-request-1", 1, method, knowledge, frontier, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return controlexperiment.StatelessSearchAgentView{Knowledge: knowledge, Request: request}
}

func statelessAgentRecoveryResponse(
	t *testing.T,
	view controlexperiment.StatelessSearchAgentView,
) []byte {
	t.Helper()
	ids := make([]control.ActionID, 0, len(view.Request.Frontier.Actions))
	for _, action := range view.Request.Frontier.Actions {
		ids = append(ids, action.ActionID)
	}
	wire := controlexperiment.StatelessFrontierOrderProposal{
		SchemaVersion: controlexperiment.StatelessFrontierOrderProposalVersion,
		ID:            view.Request.ID, RequestDigest: view.Request.Digest,
		ViewDigest: view.Request.Frontier.Digest, ActionIDs: ids,
	}
	content, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	response, err := json.Marshal(deepSeekChatResponse{
		ID: "fixture-response", Model: deepSeekV4Flash,
		Choices: []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{{Index: 0, Message: struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "assistant", Content: string(content)}, FinishReason: "stop"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["usage"] = map[string]int{"prompt_tokens": 4, "completion_tokens": 3, "total_tokens": 7}
	response, err = json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

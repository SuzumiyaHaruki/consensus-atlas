package raftrsv2

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestNaturalElectionAndFreshWorkerReplay(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	adapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{Seed: []byte("raft-rs-m5.21s")})
	if err != nil {
		t.Fatal(err)
	}
	firstPID := adapter.worker.cmd.Process.Pid

	for decisions := 0; decisions < 128 && leader(adapter) == ""; decisions++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no progress action at decision %d: %+v", decisions, actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	if got := leader(adapter); got != "n1" {
		t.Fatalf("leader=%q, want n1", got)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []control.ActionKind{
		control.ActionFireTemporal, control.ActionCompleteEffect, control.ActionDeliverMessage,
	} {
		if !traceHasAction(trace, kind) {
			t.Fatalf("trace does not contain %s", kind)
		}
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}

	replayAdapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(ctx, replayAdapter, controlruntime.Config{
		Seed: []byte("raft-rs-m5.21s"),
	}, trace); err != nil {
		t.Fatal(err)
	}
	secondPID := replayAdapter.worker.cmd.Process.Pid
	if secondPID == firstPID {
		t.Fatalf("replay reused worker pid %d", firstPID)
	}
	if got := leader(replayAdapter); got != "n1" {
		t.Fatalf("replay leader=%q, want n1", got)
	}
	if err := replayAdapter.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("trace=%s decisions=%d worker_pids=%d,%d", trace.Digest, len(trace.Records), firstPID, secondPID)
}

func TestOpaqueProposalCommitResultAndFreshWorkerReplay(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	adapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{Seed: []byte("raft-rs-m5.21t")})
	if err != nil {
		t.Fatal(err)
	}
	firstPID := adapter.worker.cmd.Process.Pid
	payload, err := InputPayload(Input{
		RequestID: "request-1", Value: []byte("opaque-m5.21t"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var invoke control.ActionID
	for decision := 0; decision < 128; decision++ {
		invoke, err = runtime.OfferInvoke(ctx, "n1", payload)
		if err == nil {
			break
		}
		if !errors.Is(err, controlruntime.ErrInvokeNotEligible) {
			t.Fatal(err)
		}
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			t.Fatal(enabledErr)
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no action can reach an invokable leader: %+v", actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	if invoke == "" {
		t.Fatal("leader never became eligible for invoke")
	}
	if _, err := runtime.Select(ctx, invoke); err != nil {
		t.Fatal(err)
	}
	for decision := 0; decision < 128 && clientResponse(runtime.Snapshot(), "request-1") == nil; decision++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no action can progress proposal: %+v", actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	response := clientResponse(runtime.Snapshot(), "request-1")
	assertClientResult(t, response)
	if count := clientResultCount(runtime.Snapshot(), "request-1"); count != 1 {
		t.Fatalf("client result count=%d, want 1", count)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if !traceHasAction(trace, control.ActionInvoke) {
		t.Fatal("trace does not contain invoke")
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}

	replayAdapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	replayRuntime, err := controlruntime.Replay(ctx, replayAdapter, controlruntime.Config{
		Seed: []byte("raft-rs-m5.21t"),
	}, trace)
	if err != nil {
		t.Fatal(err)
	}
	secondPID := replayAdapter.worker.cmd.Process.Pid
	if secondPID == firstPID {
		t.Fatalf("replay reused worker pid %d", firstPID)
	}
	assertClientResult(t, clientResponse(replayRuntime.Snapshot(), "request-1"))
	if count := clientResultCount(replayRuntime.Snapshot(), "request-1"); count != 1 {
		t.Fatalf("replay client result count=%d, want 1", count)
	}
	if err := replayAdapter.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("trace=%s decisions=%d worker_pids=%d,%d", trace.Digest, len(trace.Records), firstPID, secondPID)
}

func TestWorkerRejectsUnsupportedOperation(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := startWorker(ctx, workerPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.call(workerRequest{Op: "reset"}); err != nil {
		t.Fatal(err)
	}
	_, err = client.call(workerRequest{Op: "not-a-worker-operation"})
	if err == nil || !strings.Contains(err.Error(), "RAFT_RS_WORKER_OPERATION_UNSUPPORTED") {
		t.Fatalf("unexpected rejection: %v", err)
	}
	if err := client.close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerAbnormalExitIsReported(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := startWorker(ctx, workerPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.call(workerRequest{Op: "reset"}); err != nil {
		t.Fatal(err)
	}
	if err := client.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := client.close(); err == nil || !strings.Contains(err.Error(), "RAFT_RS_WORKER_EXIT") {
		t.Fatalf("unexpected exit result: %v", err)
	}
}

func buildWorker(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required for the raft-rs binding integration test")
	}
	manifest := filepath.Join("worker", "Cargo.toml")
	command := exec.Command("cargo", "build", "--locked", "--quiet", "--manifest-path", manifest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build raft-rs worker: %v\n%s", err, output)
	}
	path, err := filepath.Abs(filepath.Join("worker", "target", "debug", "consensus-atlas-raftrs-worker"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func progressAction(actions []control.Action) (control.Action, bool) {
	for _, kind := range []control.ActionKind{
		control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionFireTemporal,
	} {
		for _, action := range actions {
			if action.Kind == kind {
				return action, true
			}
		}
	}
	return control.Action{}, false
}

func leader(adapter *Adapter) control.NodeID {
	for _, node := range adapter.last.Nodes {
		if node.Role == "Leader" {
			return nodeNames[node.ID]
		}
	}
	return ""
}

func traceHasAction(trace controlruntime.Trace, kind control.ActionKind) bool {
	for _, record := range trace.Records {
		if record.Action.Kind == kind {
			return true
		}
	}
	return false
}

func clientResponse(snapshot controlruntime.Snapshot, requestID string) *control.ClientResponse {
	for _, item := range snapshot.Items {
		if item.Kind == control.ItemClientResult && item.Value.Response != nil &&
			item.Value.Response.RequestID == requestID {
			return item.Value.Response
		}
	}
	return nil
}

func clientResultCount(snapshot controlruntime.Snapshot, requestID string) int {
	count := 0
	for _, item := range snapshot.Items {
		if item.Kind == control.ItemClientResult && item.Value.Response != nil &&
			item.Value.Response.RequestID == requestID {
			count++
		}
	}
	return count
}

func assertClientResult(t *testing.T, response *control.ClientResponse) {
	t.Helper()
	if response == nil || response.Status != "committed" || response.Owner.Node != "n1" ||
		response.Payload.SchemaVersion != resultSchema {
		t.Fatalf("invalid client response: %+v", response)
	}
	var result clientResult
	if err := json.Unmarshal(response.Payload.Bytes, &result); err != nil {
		t.Fatal(err)
	}
	if result.Index == 0 || result.Term == 0 || string(result.Value) != "opaque-m5.21t" {
		t.Fatalf("invalid result payload: %+v", result)
	}
}

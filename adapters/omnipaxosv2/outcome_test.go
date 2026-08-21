package omnipaxosv2

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestA9e3aWorkerDeadlineProducesTerminalOutcomeOutsideTrace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	workerPath := buildWorker(t)
	adapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{
		Seed: []byte("omnipaxos-a9e2d-worker-exit"),
	})
	if err != nil {
		t.Fatal(err)
	}

	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var selected control.Action
	for _, action := range actions {
		if action.Kind == control.ActionFireTemporal {
			selected = action
			break
		}
	}
	if selected.ID == "" {
		t.Fatalf("initial worker frontier has no temporal action: %+v", actions)
	}
	prefix, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.worker.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	selectCtx, selectCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	t.Cleanup(selectCancel)
	started := time.Now()
	_, selectErr := runtime.Select(selectCtx, selected.ID)
	if selectErr == nil || time.Since(started) > time.Second {
		t.Fatalf("worker call did not stop at its action deadline: elapsed=%s err=%v", time.Since(started), selectErr)
	}
	var terminalError *controlruntime.TerminalExecutionError
	if !errors.As(selectErr, &terminalError) {
		t.Fatalf("deadline error has no terminal outcome: %v", selectErr)
	}
	outcome, ok := runtime.TerminalOutcome()
	if !ok || outcome.Validate() != nil || outcome.Class != control.ExecutionFailureDeadline ||
		outcome.Code != "ACTION_DEADLINE_EXCEEDED" || outcome.Decision != 1 ||
		outcome.AttemptedAction.ID != selected.ID || outcome.PrefixTraceDigest != prefix.Digest ||
		outcome.Digest != terminalError.Terminal.Digest {
		t.Fatalf("deadline terminal outcome is invalid: %#v / %v", outcome, terminalError)
	}
	after, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Records) != 0 || after.Digest != prefix.Digest || after.Validate() != nil {
		t.Fatalf("failed action unexpectedly entered the sealed trace: before=%s after=%#v", prefix.Digest, after)
	}
	failedWorker := adapter.worker
	if err := runtime.Close(); err == nil {
		t.Fatal("worker process exit produced no close error")
	}
	if failedWorker.cmd.ProcessState == nil || failedWorker.cmd.ProcessState.Success() {
		t.Fatalf("worker was not confirmed as an unsuccessful process exit: %#v", failedWorker.cmd.ProcessState)
	}

	replayAdapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := controlruntime.Replay(ctx, replayAdapter, controlruntime.Config{
		Seed: []byte("omnipaxos-a9e2d-worker-exit"),
	}, after)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayed.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("class=%s code=%s decision=%d prefix=%s outcome=%s trace_records=0",
		outcome.Class, outcome.Code, outcome.Decision, outcome.PrefixTraceDigest, outcome.Digest)
}

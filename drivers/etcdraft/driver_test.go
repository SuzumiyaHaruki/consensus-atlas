package etcdraft_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/drivers/etcdraft"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/families/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/explore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/host"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

func TestExplicitElectionAndProposal(t *testing.T) {
	e, adapter := newEngine(t)
	startAll(t, e)
	executeAndDrain(t, e, core.Event{Kind: core.EventCampaign, Target: "n1"}, 500)
	payload, _ := json.Marshal(map[string]string{"value": "alpha"})
	executeAndDrain(t, e, core.Event{Kind: core.EventPropose, Target: "n1", Payload: payload}, 500)

	trace := e.Trace()
	if !hasObservation(trace, "transition:candidate->leader") {
		t.Fatal("trace has no candidate-to-leader transition")
	}
	commits := 0
	for _, record := range trace {
		for _, observation := range record.Observations {
			if observation.Kind == "commit" && observation.Value == "alpha" {
				commits++
			}
		}
	}
	if commits != 3 {
		t.Fatalf("alpha commits = %d, want 3", commits)
	}
	if result := oracle.Check(trace, oracle.TraceIntegrity{}, oracle.Agreement{}); len(result.Violations) != 0 {
		t.Fatalf("oracle violations = %#v", result.Violations)
	}
	if pending := e.Pending(); len(pending) != 0 {
		t.Fatalf("pending at end = %#v", pending)
	}
	if err := adapter.CheckConformance(); err != nil {
		t.Fatal(err)
	}
	discovery, err := raftfamily.Discover(trace)
	if err != nil {
		t.Fatalf("project real etcd/raft evidence through Raft PSS: %v", err)
	}
	if discovery.Samples == 0 || discovery.UniqueStates == 0 || discovery.UniqueStates > discovery.Samples {
		t.Fatalf("invalid protocol-state discovery summary: %#v", discovery)
	}
}

func TestFollowerDoesNotExposeProposalThatNativeRaftWouldDrop(t *testing.T) {
	protocolDriver, err := etcdraft.New([]string{"n1", "n2", "n3"})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"value": "alpha"})
	proposal := core.Event{Kind: core.EventPropose, Target: "n1", Payload: payload}
	if enabled, _ := protocolDriver.EnabledInput(proposal); enabled {
		t.Fatal("follower exposed a proposal that native Raft would drop")
	}
}

func TestCrashBeforeSyncCancelsVolatileBatch(t *testing.T) {
	e, adapter := newEngine(t)
	startAll(t, e)

	campaignID := e.Schedule(core.Event{Kind: core.EventCampaign, Target: "n1"})
	if _, err := e.Execute(context.Background(), campaignID); err != nil {
		t.Fatal(err)
	}
	persist := findEvent(t, e.Enabled(), core.EventPersist, "n1")
	if _, err := e.Execute(context.Background(), persist.ID); err != nil {
		t.Fatal(err)
	}
	crashID := e.Schedule(core.Event{Kind: core.EventCrash, Target: "n1"})
	crashRecord, err := e.Execute(context.Background(), crashID)
	if err != nil {
		t.Fatal(err)
	}
	if len(crashRecord.Cancelled) == 0 {
		t.Fatal("crash did not cancel the outstanding Ready operations")
	}
	for _, event := range e.Pending() {
		if event.Source == "n1" && event.Kind == core.EventMessage {
			t.Fatalf("message %s was network-visible before sync/release", event.ID)
		}
		if event.Group != "" && event.Target == "n1" {
			t.Fatalf("stale volatile host operation remains pending: %#v", event)
		}
	}

	restartID := e.Schedule(core.Event{Kind: core.EventRestart, Target: "n1"})
	if _, err := e.Execute(context.Background(), restartID); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(context.Background(), 200); err != nil {
		t.Fatal(err)
	}
	executeAndDrain(t, e, core.Event{Kind: core.EventCampaign, Target: "n1"}, 500)
	if !hasObservation(e.Trace(), "transition:candidate->leader") {
		t.Fatal("node did not elect after durable restart")
	}
	if err := adapter.CheckConformance(); err != nil {
		t.Fatal(err)
	}
}

func TestReleasedMessageSurvivesSenderCrash(t *testing.T) {
	e, _ := newEngine(t)
	startAll(t, e)

	campaignID := e.Schedule(core.Event{Kind: core.EventCampaign, Target: "n1"})
	if _, err := e.Execute(context.Background(), campaignID); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []core.EventKind{core.EventPersist, core.EventSync, core.EventEmit} {
		event := findEvent(t, e.Enabled(), kind, "n1")
		if _, err := e.Execute(context.Background(), event.ID); err != nil {
			t.Fatal(err)
		}
	}
	message := findMessage(t, e.Pending(), "n1")
	crashID := e.Schedule(core.Event{Kind: core.EventCrash, Target: "n1"})
	if _, err := e.Execute(context.Background(), crashID); err != nil {
		t.Fatal(err)
	}
	stillPending := false
	for _, event := range e.Pending() {
		if event.ID == message.ID {
			stillPending = true
		}
	}
	if !stillPending {
		t.Fatal("released message was incorrectly deleted with the sender's volatile batch")
	}
	if _, err := e.Execute(context.Background(), message.ID); err != nil {
		t.Fatalf("deliver released message after sender crash: %v", err)
	}
}

func TestRandomExplorerUsesRaftPSSFromSharedMeasurementRoot(t *testing.T) {
	factory := func(ctx context.Context) (*engine.Engine, error) {
		protocolDriver, err := etcdraft.New([]string{"n1", "n2", "n3"})
		if err != nil {
			return nil, err
		}
		adapter, err := host.New(protocolDriver)
		if err != nil {
			return nil, err
		}
		e := engine.New(adapter)
		for _, node := range []string{"n1", "n2", "n3"} {
			e.Schedule(core.Event{Kind: core.EventStart, Target: node})
		}
		if err := e.Run(ctx, 300); err != nil {
			return nil, err
		}
		e.Schedule(core.Event{Kind: core.EventCampaign, Target: "n1"})
		e.Schedule(core.Event{Kind: core.EventCrash, Target: "n1"})
		e.Schedule(core.Event{Kind: core.EventRestart, Target: "n1"})
		return e, nil
	}
	config := explore.Config{
		Runs: 16, BudgetPerRun: 32, DecisionBudget: 32, Seed: 11,
		Actions: explore.ActionPolicy{DropMessages: true},
	}
	first, err := (explore.Random{}).Explore(context.Background(), factory, config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (explore.Random{}).Explore(context.Background(), factory, config)
	if err != nil {
		t.Fatal(err)
	}
	if !first.BudgetReached || first.ChargedDecisions != 32 || len(first.Runs) != len(second.Runs) {
		t.Fatalf("unexpected exploration result: %#v", first)
	}
	measured := make([]protocolstate.MeasuredRun, 0, len(first.Runs))
	for index, run := range first.Runs {
		if run.ExecutionError != "" || !run.Conform ||
			run.ExecutionFingerprint != second.Runs[index].ExecutionFingerprint {
			t.Fatalf("invalid or unstable run %d: %#v", index+1, run)
		}
		measured = append(measured, protocolstate.MeasuredRun{
			Run: run.Run, DecisionCount: len(run.Decisions),
			InitialSnapshot: run.InitialSnapshot, Trace: run.Trace,
		})
	}
	discovery, err := protocolstate.Aggregate(measured, raftfamily.Projector{})
	if err != nil {
		t.Fatal(err)
	}
	if discovery.TotalDecisions != 32 || discovery.UniqueStates <= 1 {
		t.Fatalf("unexpected protocol-state discovery: %#v", discovery)
	}
}

func newEngine(t *testing.T) (*engine.Engine, *host.Adapter) {
	t.Helper()
	protocolDriver, err := etcdraft.New([]string{"n1", "n2", "n3"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := host.New(protocolDriver)
	if err != nil {
		t.Fatal(err)
	}
	return engine.New(adapter), adapter
}

func startAll(t *testing.T, e *engine.Engine) {
	t.Helper()
	for _, node := range []string{"n1", "n2", "n3"} {
		e.Schedule(core.Event{Kind: core.EventStart, Target: node})
	}
	if err := e.Run(context.Background(), 300); err != nil {
		t.Fatal(err)
	}
	if pending := e.Pending(); len(pending) != 0 {
		t.Fatalf("startup pending = %#v", pending)
	}
}

func executeAndDrain(t *testing.T, e *engine.Engine, event core.Event, limit int) {
	t.Helper()
	id := e.Schedule(event)
	if _, err := e.Execute(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(context.Background(), limit); err != nil {
		t.Fatal(err)
	}
}

func findEvent(t *testing.T, events []core.Event, kind core.EventKind, target string) core.Event {
	t.Helper()
	for _, event := range events {
		if event.Kind == kind && event.Target == target {
			return event
		}
	}
	t.Fatalf("no enabled %s event for %s; events=%#v", kind, target, events)
	return core.Event{}
}

func findMessage(t *testing.T, events []core.Event, source string) core.Event {
	t.Helper()
	for _, event := range events {
		if event.Kind == core.EventMessage && event.Source == source {
			return event
		}
	}
	t.Fatalf("no pending message from %s; events=%#v", source, events)
	return core.Event{}
}

func hasObservation(trace []core.TraceRecord, label string) bool {
	for _, record := range trace {
		for _, observation := range record.Observations {
			if observation.Label == label {
				return true
			}
		}
	}
	return false
}

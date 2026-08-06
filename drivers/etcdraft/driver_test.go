package etcdraft_test

import (
	"context"
	"encoding/json"
	"strings"
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

// The official v3.6 package chooses native election deadlines through an
// opaque randomness source. Advancing ConsensusAtlas virtual time must not
// pretend that this produces a replayable Raft Tick: it only records a clock
// boundary and leaves the native Driver's unsupported capability unchanged.
func TestVirtualTimeDoesNotCreateUncontrolledEtcdTimeout(t *testing.T) {
	e, adapter := newEngine(t)
	if err := e.Advance(100); err != nil {
		t.Fatal(err)
	}
	for _, event := range e.Pending() {
		if event.Kind == core.EventTimeout {
			t.Fatalf("virtual time created uncontrolled etcd timeout: %#v", event)
		}
	}
	trace := e.Trace()
	if len(trace) != 1 || trace[0].Event.Kind != core.EventClockAdvance {
		t.Fatalf("virtual-time trace = %#v", trace)
	}
	var timeoutCapability *bool
	for _, capability := range adapter.Capabilities().Capabilities {
		if capability.ID == "natural-election-timeout-replay" {
			supported := capability.Supported
			timeoutCapability = &supported
			break
		}
	}
	if timeoutCapability == nil || *timeoutCapability {
		t.Fatalf("native timeout capability changed: %#v", adapter.Capabilities())
	}
}

func TestReadIndexInputEmitsTypedReadState(t *testing.T) {
	e, adapter := newEngine(t)
	startAll(t, e)
	executeAndDrain(t, e, core.Event{Kind: core.EventCampaign, Target: "n1"}, 500)
	payload, _ := json.Marshal(map[string]string{
		"operation": "linearizable-read", "request_id": "read-A",
	})
	executeAndDrain(t, e, core.Event{Kind: core.EventQuery, Target: "n1", Payload: payload}, 500)

	var found bool
	for _, record := range e.Trace() {
		for _, observation := range record.Observations {
			if observation.Kind == "read-state" && observation.Value == "read-A" &&
				observation.Evidence["index"] != "" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("ReadIndex request produced no typed read-state observation")
	}
	checked := oracle.Check(e.Trace(), oracle.TraceIntegrity{}, oracle.Agreement{}, raftfamily.LinearizableRead{})
	if len(checked.Violations) != 0 {
		t.Fatalf("oracle violations = %#v", checked.Violations)
	}
	if err := adapter.CheckConformance(); err != nil {
		t.Fatal(err)
	}
}

// TestReadIndexDelayedResponseSequence keeps the response messages generated
// by the first ReadIndex request in the Runtime mailbox, changes the cluster's
// committed index through a new leader, then releases the old response before
// newer traffic. The upstream regression used one leader tick to complete A;
// this Driver has no certified natural-timer control, so a second legal retry
// of A produces the same confirming heartbeat without inventing a timeout.
func TestReadIndexDelayedResponseSequence(t *testing.T) {
	e, adapter := newEngine(t)
	startAll(t, e)
	executeAndDrain(t, e, core.Event{Kind: core.EventCampaign, Target: "n1"}, 500)

	queryA := readQueryPayload(t, "A")
	executeInputAndFinishBatch(t, e, core.Event{Kind: core.EventQuery, Target: "n1", Payload: queryA})
	deliverAndFinishBatch(t, e, "n1", "n2", "MsgHeartbeat")
	deliverAndFinishBatch(t, e, "n1", "n3", "MsgHeartbeat")
	delayed := pendingMessageIDs(t, e, "", "n1", "MsgHeartbeatResp")
	if len(delayed) != 2 {
		t.Fatalf("delayed heartbeat responses = %v, want exactly two", delayed)
	}

	// Confirm the first A without consuming either delayed response. A legal
	// retry causes a fresh heartbeat and response carrying the same context.
	executeInputAndFinishBatch(t, e, core.Event{Kind: core.EventQuery, Target: "n1", Payload: queryA})
	deliverAndFinishBatch(t, e, "n1", "n2", "MsgHeartbeat")
	response := findPendingMessageExcept(t, e, "n2", "n1", "MsgHeartbeatResp", delayed)
	if _, err := e.Execute(context.Background(), response.ID); err != nil {
		t.Fatalf("deliver confirming heartbeat response: %v", err)
	}
	finishCurrentBatch(t, e, "n1")
	if !hasReadState(e.Trace(), "A") {
		t.Fatal("first read request was not confirmed before the leader separation")
	}

	if err := e.Partition([][]string{{"n1"}, {"n2", "n3"}}); err != nil {
		t.Fatal(err)
	}
	executeAndDrain(t, e, core.Event{Kind: core.EventCampaign, Target: "n2"}, 500)
	proposal, _ := json.Marshal(map[string]string{"value": "new-leader-entry"})
	executeAndDrain(t, e, core.Event{Kind: core.EventPropose, Target: "n2", Payload: proposal}, 500)

	queryB := readQueryPayload(t, "B")
	executeInputAndFinishBatch(t, e, core.Event{Kind: core.EventQuery, Target: "n1", Payload: queryB})

	// A is retried after B. Its new heartbeat cannot cross the partition, but
	// the already-created old responses have retained Runtime ownership.
	executeInputAndFinishBatch(t, e, core.Event{Kind: core.EventQuery, Target: "n1", Payload: queryA})
	e.Heal()
	if _, err := e.Execute(context.Background(), delayed[0]); err != nil {
		t.Fatalf("release delayed heartbeat response: %v", err)
	}
	finishCurrentBatch(t, e, "n1")

	checked := oracle.Check(e.Trace(), oracle.TraceIntegrity{}, oracle.Agreement{}, raftfamily.LinearizableRead{})
	if !hasReadState(e.Trace(), "B") {
		t.Fatal("sequence did not return a read-state for B; trace cannot exercise the regression boundary")
	}
	if !hasReadViolation(checked, "B") {
		t.Fatalf("oracle result = %#v, want a linearizable-read violation for B", checked)
	}
	for _, violation := range checked.Violations {
		if violation.Monitor != "linearizable-read" {
			t.Fatalf("oracle result contains an unexpected violation: %#v", checked)
		}
	}
	if err := adapter.CheckConformance(); err != nil {
		t.Fatal(err)
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

// The official RawNode implementation computes MustSync from its current
// HardState, even when a restarted node has committed work left to apply. The
// opt-in host policy must preserve that exact decision: no synthetic Sync is
// scheduled for an empty Ready, and the captured decision is observable only
// after the batch is acknowledged. This is intentionally separate from the
// default conservative policy used by frozen campaigns.
func TestMustSyncPolicyObservesEmptyReadyAfterRestart(t *testing.T) {
	protocolDriver, err := etcdraft.NewWithConfig(etcdraft.Config{
		Nodes:           []string{"n1", "n2", "n3"},
		ElectionTick:    10,
		HeartbeatTick:   1,
		ReadySyncPolicy: etcdraft.ReadySyncMustSync,
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := host.New(protocolDriver)
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(adapter)
	if err != nil {
		t.Fatal(err)
	}

	start := e.Schedule(core.Event{Kind: core.EventStart, Target: "n1"})
	if _, err := e.Execute(context.Background(), start); err != nil {
		t.Fatal(err)
	}
	// Save Raft's bootstrap state but deliberately leave it unapplied. A crash
	// here models a process that has made the Ready durable but has not yet
	// handed committed entries to the application.
	for _, kind := range []core.EventKind{core.EventPersist, core.EventSync} {
		event := findEvent(t, e.Enabled(), kind, "n1")
		if _, err := e.Execute(context.Background(), event.ID); err != nil {
			t.Fatalf("execute %s: %v", kind, err)
		}
	}
	crash := e.Schedule(core.Event{Kind: core.EventCrash, Target: "n1"})
	if _, err := e.Execute(context.Background(), crash); err != nil {
		t.Fatal(err)
	}
	restart := e.Schedule(core.Event{Kind: core.EventRestart, Target: "n1"})
	if _, err := e.Execute(context.Background(), restart); err != nil {
		t.Fatal(err)
	}

	for _, event := range e.Pending() {
		if event.Target == "n1" && event.Kind == core.EventSync {
			t.Fatalf("empty restart Ready unexpectedly scheduled Sync: %#v", event)
		}
	}
	finishCurrentBatch(t, e, "n1")
	var found bool
	for _, record := range e.Trace() {
		for _, observation := range record.Observations {
			if observation.Label != "ready:must-sync" || observation.Node != "n1" {
				continue
			}
			found = true
			if observation.Value != "false" || observation.Evidence["entries"] != "0" ||
				observation.Evidence["hard_state_empty"] != "true" {
				t.Fatalf("restart Ready observation = %#v", observation)
			}
		}
	}
	if !found {
		t.Fatal("must-sync policy emitted no Ready.MustSync observation")
	}
	if violations := (raftfamily.ReadyMustSync{}).Check(e.Trace()); len(violations) != 0 {
		t.Fatalf("official Ready evidence violates monitor: %#v", violations)
	}
	if err := adapter.CheckConformance(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultDriverDoesNotExposeConditionalReadySync(t *testing.T) {
	protocolDriver, err := etcdraft.New([]string{"n1", "n2", "n3"})
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range protocolDriver.Capabilities().Capabilities {
		if capability.ID == "conditional-ready-sync" || capability.ID == "ready-must-sync-observation" {
			t.Fatalf("default manifest exposed opt-in Ready policy: %#v", protocolDriver.Capabilities())
		}
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
		e, err := engine.New(adapter)
		if err != nil {
			return nil, err
		}
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
	e, err := engine.New(adapter)
	if err != nil {
		t.Fatal(err)
	}
	return e, adapter
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

func executeInputAndFinishBatch(t *testing.T, e *engine.Engine, event core.Event) {
	t.Helper()
	id := e.Schedule(event)
	if _, err := e.Execute(context.Background(), id); err != nil {
		t.Fatalf("execute %s for %s: %v", event.Kind, event.Target, err)
	}
	finishCurrentBatch(t, e, event.Target)
}

func deliverAndFinishBatch(t *testing.T, e *engine.Engine, source, target, typeHint string) {
	t.Helper()
	event := findPendingMessage(t, e, source, target, typeHint)
	if _, err := e.Execute(context.Background(), event.ID); err != nil {
		t.Fatalf("deliver %s from %s to %s: %v", typeHint, source, target, err)
	}
	finishCurrentBatch(t, e, target)
}

func finishCurrentBatch(t *testing.T, e *engine.Engine, node string) {
	t.Helper()
	group := ""
	for steps := 0; steps < 32; steps++ {
		event, found := findHostOperation(e.Enabled(), node, group)
		if !found {
			if group == "" {
				t.Fatalf("node %s has no enabled Ready operation", node)
			}
			return
		}
		if group == "" {
			group = event.Group
		}
		if _, err := e.Execute(context.Background(), event.ID); err != nil {
			t.Fatalf("execute host operation %s for %s: %v", event.Kind, node, err)
		}
		if event.Kind == core.EventAcknowledge {
			return
		}
	}
	t.Fatalf("Ready batch for %s did not finish", node)
}

func findHostOperation(events []core.Event, node, group string) (core.Event, bool) {
	for _, kind := range []core.EventKind{
		core.EventPersist, core.EventSync, core.EventEmit, core.EventApply, core.EventAcknowledge,
	} {
		for _, event := range events {
			if event.Kind == kind && event.Target == node && (group == "" || event.Group == group) {
				return event, true
			}
		}
	}
	return core.Event{}, false
}

func readQueryPayload(t *testing.T, requestID string) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"operation": "linearizable-read", "request_id": requestID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func pendingMessageIDs(t *testing.T, e *engine.Engine, source, target, typeHint string) []string {
	t.Helper()
	var ids []string
	for _, event := range e.Pending() {
		if event.Kind != core.EventMessage || event.Source != source && source != "" ||
			event.Target != target && target != "" || event.Message == nil || event.Message.TypeHint != typeHint {
			continue
		}
		ids = append(ids, event.ID)
	}
	return ids
}

func findPendingMessage(t *testing.T, e *engine.Engine, source, target, typeHint string) core.Event {
	t.Helper()
	return findPendingMessageExcept(t, e, source, target, typeHint, nil)
}

func findPendingMessageExcept(t *testing.T, e *engine.Engine, source, target, typeHint string, excluded []string) core.Event {
	t.Helper()
	excludedSet := make(map[string]bool, len(excluded))
	for _, id := range excluded {
		excludedSet[id] = true
	}
	for _, event := range e.Pending() {
		if excludedSet[event.ID] || event.Kind != core.EventMessage || event.Source != source || event.Target != target ||
			event.Message == nil || event.Message.TypeHint != typeHint {
			continue
		}
		return event
	}
	t.Fatalf("no pending %s from %s to %s; events=%#v", typeHint, source, target, e.Pending())
	return core.Event{}
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

func hasReadState(trace []core.TraceRecord, requestID string) bool {
	for _, record := range trace {
		for _, observation := range record.Observations {
			if observation.Kind == "read-state" && observation.Value == requestID {
				return true
			}
		}
	}
	return false
}

func hasReadViolation(result oracle.Result, requestID string) bool {
	needle := "request \"" + requestID + "\""
	for _, violation := range result.Violations {
		if violation.Monitor == "linearizable-read" && strings.Contains(violation.Message, needle) {
			return true
		}
	}
	return false
}

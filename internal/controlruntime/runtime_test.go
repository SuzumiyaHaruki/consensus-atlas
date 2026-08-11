package controlruntime_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

type falseEntropyDigestAdapter struct {
	control.Adapter
}

type invokeGateAdapter struct {
	control.Adapter
	allow bool
}

func (adapter *invokeGateAdapter) Check(
	ctx context.Context,
	command control.AdapterCommand,
) (control.CommandEligibility, error) {
	if command.Kind == control.ActionInvoke && !adapter.allow {
		return control.CommandEligibility{ReasonCode: "TEST_INVOKE_GATE_CLOSED"}, nil
	}
	return adapter.Adapter.Check(ctx, command)
}

type cyclicEmissionAdapter struct {
	control.Adapter
}

type nonzeroClockAdapter struct {
	control.Adapter
}

type staleRestartEmissionAdapter struct {
	control.Adapter
	restarting bool
}

type rewrittenEntropyAdapter struct {
	control.Adapter
	snapshots int
	armed     bool
}

type failingRuntimeActionAdapter struct {
	control.Adapter
}

func (adapter *failingRuntimeActionAdapter) ApplyRuntimeAction(ctx context.Context, action control.Action) error {
	if action.Kind == control.ActionPartition {
		return errors.New("TEST_PARTITION_ACTUATION_FAILED")
	}
	return adapter.Adapter.ApplyRuntimeAction(ctx, action)
}

func (adapter *falseEntropyDigestAdapter) SnapshotEntropy(ctx context.Context) (control.EntropyAuditEnvelope, error) {
	audit, err := adapter.Adapter.SnapshotEntropy(ctx)
	if err != nil {
		return control.EntropyAuditEnvelope{}, err
	}
	audit.TapeDigest = strings.Repeat("0", 64)
	return audit, nil
}

func (adapter *cyclicEmissionAdapter) Collect(ctx context.Context, yield control.YieldID) (control.Emission, error) {
	emission, err := adapter.Adapter.Collect(ctx, yield)
	if err != nil {
		return control.Emission{}, err
	}
	if len(emission.Items) >= 2 {
		emission.Items[0].Dependencies = []control.ItemID{emission.Items[1].ID}
		emission.Items[1].Dependencies = []control.ItemID{emission.Items[0].ID}
	}
	return emission.Seal()
}

func (adapter *nonzeroClockAdapter) Manifest(ctx context.Context) (control.AdapterManifest, error) {
	manifest, err := adapter.Adapter.Manifest(ctx)
	if err != nil {
		return control.AdapterManifest{}, err
	}
	manifest.Capabilities.Temporal.ClockError = 1
	return manifest, nil
}

func (adapter *staleRestartEmissionAdapter) Submit(ctx context.Context, command control.AdapterCommand) error {
	adapter.restarting = command.Kind == control.ActionRestart
	return adapter.Adapter.Submit(ctx, command)
}

func (adapter *staleRestartEmissionAdapter) Collect(ctx context.Context, yield control.YieldID) (control.Emission, error) {
	emission, err := adapter.Adapter.Collect(ctx, yield)
	if err != nil {
		return control.Emission{}, err
	}
	if adapter.restarting {
		for index := range emission.Items {
			if emission.Items[index].Owner.Node == "n1" {
				emission.Items[index].Owner.Incarnation = 1
				if emission.Items[index].Temporal != nil {
					emission.Items[index].Temporal.Owner.Incarnation = 1
				}
			}
		}
	}
	return emission.Seal()
}

func (adapter *rewrittenEntropyAdapter) SnapshotEntropy(ctx context.Context) (control.EntropyAuditEnvelope, error) {
	audit, err := adapter.Adapter.SnapshotEntropy(ctx)
	if err != nil {
		return control.EntropyAuditEnvelope{}, err
	}
	adapter.snapshots++
	if !adapter.armed {
		return audit, nil
	}
	var tape controlentropy.Tape
	if err := json.Unmarshal(audit.Tape.Bytes, &tape); err != nil {
		return control.EntropyAuditEnvelope{}, err
	}
	if len(tape.Draws) == 0 || tape.Draws[0].IntResult == nil {
		return control.EntropyAuditEnvelope{}, nil
	}
	rewritten := (*tape.Draws[0].IntResult + 1) % tape.Draws[0].Bound
	tape.Draws[0].IntResult = &rewritten
	tape, err = tape.Seal()
	if err != nil {
		return control.EntropyAuditEnvelope{}, err
	}
	encoded, err := json.Marshal(tape)
	if err != nil {
		return control.EntropyAuditEnvelope{}, err
	}
	audit.Tape, err = control.NewPayload(controlentropy.TapeSchemaVersion, "json", encoded)
	if err != nil {
		return control.EntropyAuditEnvelope{}, err
	}
	audit.TapeDigest = tape.Digest
	return audit, nil
}

func (adapter *rewrittenEntropyAdapter) Submit(ctx context.Context, command control.AdapterCommand) error {
	adapter.armed = true
	return adapter.Adapter.Submit(ctx, command)
}

func TestRuntimeRejectsSelfReportedEntropyDigest(t *testing.T) {
	adapter := &falseEntropyDigestAdapter{Adapter: fixture.New()}
	_, err := controlruntime.New(context.Background(), adapter, runtimeConfig())
	if err == nil || !strings.Contains(err.Error(), "ENTROPY_TAPE_ENVELOPE_MISMATCH") {
		t.Fatalf("New() error = %v, want entropy envelope mismatch", err)
	}
}

func TestRuntimeRejectsRewrittenEntropyHistory(t *testing.T) {
	ctx := context.Background()
	adapter := &rewrittenEntropyAdapter{Adapter: fixture.New()}
	runtime, err := controlruntime.New(ctx, adapter, runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	action := actionForNode(t, actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionFireTemporal), "n1")
	_, err = runtime.Select(ctx, action.ID)
	if err == nil || !strings.Contains(err.Error(), "ENTROPY_TAPE_PREFIX_REWRITTEN") {
		t.Fatalf("Select() error = %v, want entropy prefix rewrite", err)
	}
}

func TestRuntimeRejectsCyclicEmissionDependencies(t *testing.T) {
	adapter := &cyclicEmissionAdapter{Adapter: fixture.New()}
	_, err := controlruntime.New(context.Background(), adapter, runtimeConfig())
	if err == nil || !strings.Contains(err.Error(), "ITEM_DEPENDENCY_CYCLE") {
		t.Fatalf("New() error = %v, want dependency cycle", err)
	}
}

func TestV2Alpha1RejectsNonzeroClockError(t *testing.T) {
	adapter := &nonzeroClockAdapter{Adapter: fixture.New()}
	config := runtimeConfig()
	config.ClockError = 1
	_, err := controlruntime.New(context.Background(), adapter, config)
	if err == nil || !strings.Contains(err.Error(), "CLOCK_ERROR_V2ALPHA1_REQUIRES_ZERO") {
		t.Fatalf("New() error = %v, want zero clock-error requirement", err)
	}
}

func TestRestartRejectsOutputFromPreviousIncarnation(t *testing.T) {
	ctx := context.Background()
	adapter := &staleRestartEmissionAdapter{Adapter: fixture.New()}
	runtime, err := controlruntime.New(ctx, adapter, runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	crash := actionForNode(t, actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionCrash), "n1")
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		t.Fatal(err)
	}
	restart := actionForNode(t, actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionRestart), "n1")
	_, err = runtime.Select(ctx, restart.ID)
	if err == nil || !strings.Contains(err.Error(), "ITEM_OWNER_INCARNATION_MISMATCH") {
		t.Fatalf("Select(restart) error = %v, want stale-incarnation rejection", err)
	}
}

func TestSnapshotDigestIncludesCompleteItemValue(t *testing.T) {
	runtime := newRuntime(t)
	original := runtime.Snapshot()
	originalDigest, err := original.Digest()
	if err != nil {
		t.Fatal(err)
	}
	mutated := runtime.Snapshot()
	for index := range mutated.Items {
		if mutated.Items[index].Value.Temporal != nil {
			mutated.Items[index].Value.Temporal.Deadline++
			mutatedDigest, err := mutated.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if mutatedDigest == originalDigest {
				t.Fatal("item value mutation did not change state digest")
			}
			freshDigest, err := runtime.Snapshot().Digest()
			if err != nil {
				t.Fatal(err)
			}
			if freshDigest != originalDigest {
				t.Fatal("snapshot mutation changed Runtime-owned state")
			}
			return
		}
	}
	t.Fatal("fixture did not produce a temporal item")
}

func TestRuntimeRejectsActionOutsideEnabledSetWithoutMutation(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	before, err := runtime.Snapshot().Digest()
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Select(ctx, control.ActionID("not-enabled"))
	if err == nil || !strings.Contains(err.Error(), "ACTION_NOT_ENABLED") {
		t.Fatalf("Select() error = %v, want action-not-enabled", err)
	}
	after, err := runtime.Snapshot().Digest()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("rejected action mutated Runtime state")
	}
}

func TestTemporalActionsOnlyExposeEarliestDeadline(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	actions := mustEnabled(t, ctx, runtime)
	initial := actionsOfKind(actions, control.ActionFireTemporal)
	if len(initial) != 2 {
		t.Fatalf("initial temporal actions = %d, want 2", len(initial))
	}
	first := actionForNode(t, initial, "n1")
	record, err := runtime.Select(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ClockAdvance == nil || record.ClockAdvance.From != 0 || record.ClockAdvance.To != 1 {
		t.Fatalf("clock advance = %+v, want 0 -> 1", record.ClockAdvance)
	}

	actions = mustEnabled(t, ctx, runtime)
	remaining := actionsOfKind(actions, control.ActionFireTemporal)
	if len(remaining) != 1 || remaining[0].Node.Node != "n2" {
		t.Fatalf("temporal actions after n1 pulse = %+v, want only n2@1", remaining)
	}
	secondRecord, err := runtime.Select(ctx, remaining[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if secondRecord.ClockAdvance == nil || secondRecord.ClockAdvance.From != 1 || secondRecord.ClockAdvance.To != 1 {
		t.Fatalf("second clock advance = %+v, want 1 -> 1", secondRecord.ClockAdvance)
	}
	if got := len(actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionFireTemporal)); got != 2 {
		t.Fatalf("next-period temporal actions = %d, want 2", got)
	}

	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(ctx, fixture.New(), runtimeConfig(), trace); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestReleasedMessageSurvivesSourceAndTargetCrash(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	messageID := invokeMessage(t, ctx, runtime, fixture.OpEmitMessage)

	crashSource := actionForNode(t, actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionCrash), "n1")
	if _, err := runtime.Select(ctx, crashSource.ID); err != nil {
		t.Fatal(err)
	}
	assertItemState(t, runtime.Snapshot(), messageID, control.ItemEnabled)
	assertActionForItem(t, mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, messageID, true)

	crashTarget := actionForNode(t, actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionCrash), "n2")
	if _, err := runtime.Select(ctx, crashTarget.ID); err != nil {
		t.Fatal(err)
	}
	assertItemState(t, runtime.Snapshot(), messageID, control.ItemEnabled)
	assertActionForItem(t, mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, messageID, false)
	assertActionForItem(t, mustEnabled(t, ctx, runtime), control.ActionDropMessage, messageID, true)

	restart := actionForNode(t, actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionRestart), "n2")
	if restart.Node.Incarnation != 2 {
		t.Fatalf("restart incarnation = %d, want 2", restart.Node.Incarnation)
	}
	if _, err := runtime.Select(ctx, restart.ID); err != nil {
		t.Fatal(err)
	}
	deliver := actionForItem(t, mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, messageID)
	if deliver.Node.Incarnation != 2 {
		t.Fatalf("delivery target incarnation = %d, want 2", deliver.Node.Incarnation)
	}
	if _, err := runtime.Select(ctx, deliver.ID); err != nil {
		t.Fatal(err)
	}
	assertItemState(t, runtime.Snapshot(), messageID, control.ItemCompleted)

	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(ctx, fixture.New(), runtimeConfig(), trace); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestReplayWithProgressChargesCompletedDecisionBeforeDivergence(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	action := mustEnabled(t, ctx, runtime)[0]
	if _, err := runtime.Select(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	trace.Records[0].Outcome = "tampered-expected-outcome"
	trace, err = trace.Seal()
	if err != nil {
		t.Fatal(err)
	}
	_, progress, err := controlruntime.ReplayWithProgress(ctx, fixture.New(), runtimeConfig(), trace)
	var divergence *controlruntime.ReplayDivergenceError
	if !errors.As(err, &divergence) {
		t.Fatalf("ReplayWithProgress() error = %v, want divergence", err)
	}
	if !progress.RuntimeInitialized || progress.Decisions != 1 {
		t.Fatalf("ReplayWithProgress() progress = %#v, want initialized + one decision", progress)
	}
}

func TestRestartKeepsOldTemporalItemsCanceled(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	var old control.ItemID
	for _, item := range runtime.Snapshot().Items {
		if item.Owner.Node == "n1" && item.Kind == control.ItemTemporal {
			old = item.ID
			break
		}
	}
	if old == "" {
		t.Fatal("initial n1 temporal item missing")
	}
	crash := actionForNode(t, actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionCrash), "n1")
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		t.Fatal(err)
	}
	assertItemState(t, runtime.Snapshot(), old, control.ItemCanceled)
	restart := actionForNode(t, actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionRestart), "n1")
	if _, err := runtime.Select(ctx, restart.ID); err != nil {
		t.Fatal(err)
	}
	assertItemState(t, runtime.Snapshot(), old, control.ItemCanceled)
	assertActionForItem(t, mustEnabled(t, ctx, runtime), control.ActionFireTemporal, old, false)
}

func TestNodeCanOwnMultipleOutstandingItems(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	invokeMessage(t, ctx, runtime, fixture.OpEmitDurableMessage)
	invokeMessage(t, ctx, runtime, fixture.OpEmitDurableMessage)
	counts := make(map[control.ItemKind]int)
	for _, item := range runtime.Snapshot().Items {
		if item.Owner.Node == "n1" && !isTerminalState(item.State) {
			counts[item.Kind]++
		}
	}
	if counts[control.ItemMessage] < 2 || counts[control.ItemEffect] < 2 || counts[control.ItemTemporal] < 1 {
		t.Fatalf("outstanding n1 items = %+v, want multiple messages/effects and a timer", counts)
	}
}

func TestDependentMessageReleasesOnlyAfterDurableEffect(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	messageID := invokeMessage(t, ctx, runtime, fixture.OpEmitDurableMessage)
	snapshot := runtime.Snapshot()
	effectID := firstItemOfKind(t, snapshot, control.ItemEffect)
	assertItemState(t, snapshot, effectID, control.ItemEnabled)
	assertItemState(t, snapshot, messageID, control.ItemBlocked)
	assertActionForItem(t, mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, messageID, false)

	complete := actionForItem(t, mustEnabled(t, ctx, runtime), control.ActionCompleteEffect, effectID)
	if _, err := runtime.Select(ctx, complete.ID); err != nil {
		t.Fatal(err)
	}
	snapshot = runtime.Snapshot()
	assertItemState(t, snapshot, effectID, control.ItemCompleted)
	assertItemState(t, snapshot, messageID, control.ItemEnabled)
	if durableEffects(t, snapshot, "n1") != 1 {
		t.Fatalf("durable effects = %d, want 1", durableEffects(t, snapshot, "n1"))
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(ctx, fixture.New(), runtimeConfig(), trace); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestCrashBeforeEffectCancelsCapturedMessage(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	messageID := invokeMessage(t, ctx, runtime, fixture.OpEmitDurableMessage)
	effectID := firstItemOfKind(t, runtime.Snapshot(), control.ItemEffect)
	crash := actionForNode(t, actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionCrash), "n1")
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		t.Fatal(err)
	}
	assertItemState(t, runtime.Snapshot(), effectID, control.ItemCanceled)
	assertItemState(t, runtime.Snapshot(), messageID, control.ItemCanceled)
}

func TestPartitionBlocksDeliveryWithoutDroppingMessage(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	messageID := invokeMessage(t, ctx, runtime, fixture.OpEmitMessage)
	partitionID, err := runtime.OfferPartition([]control.NodeID{"n1"}, []control.NodeID{"n2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, partitionID); err != nil {
		t.Fatal(err)
	}
	assertItemState(t, runtime.Snapshot(), messageID, control.ItemEnabled)
	assertActionForItem(t, mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, messageID, false)
	heal := actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionHeal)
	if len(heal) != 1 {
		t.Fatalf("heal actions = %d, want 1", len(heal))
	}
	if _, err := runtime.Select(ctx, heal[0].ID); err != nil {
		t.Fatal(err)
	}
	assertActionForItem(t, mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, messageID, true)
}

func TestFailedPartitionActuationDoesNotCommitRuntimeState(t *testing.T) {
	ctx := context.Background()
	adapter := &failingRuntimeActionAdapter{Adapter: fixture.New()}
	runtime, err := controlruntime.New(ctx, adapter, runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	partitionID, err := runtime.OfferPartition([]control.NodeID{"n1"}, []control.NodeID{"n2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, partitionID); err == nil || !strings.Contains(err.Error(), "ADAPTER_RUNTIME_ACTION_FAILED") {
		t.Fatalf("Select(partition) error = %v, want actuation failure", err)
	}
	if got := len(runtime.Snapshot().Partitions); got != 0 {
		t.Fatalf("partitions after failed actuation = %d, want 0", got)
	}
}

func TestSleepCannotSkipEarlierPeriodicPulse(t *testing.T) {
	ctx := context.Background()
	runtime := newRuntime(t)
	payload, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpSleep, Delay: 5})
	if err != nil {
		t.Fatal(err)
	}
	id, err := runtime.OfferInvoke(ctx, "n1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, id); err != nil {
		t.Fatal(err)
	}
	sleepID := temporalItemOfKind(t, runtime, control.TemporalSleepWakeup)
	assertActionForItem(t, mustEnabled(t, ctx, runtime), control.ActionFireTemporal, sleepID, false)
	for runtime.Snapshot().LogicalTime < 4 {
		actions := actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionFireTemporal)
		if len(actions) == 0 {
			t.Fatal("no periodic pulse available")
		}
		if _, err := runtime.Select(ctx, actions[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	assertActionForItem(t, mustEnabled(t, ctx, runtime), control.ActionFireTemporal, sleepID, false)
	for {
		actions := actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionFireTemporal)
		if hasActionForItem(actions, control.ActionFireTemporal, sleepID) {
			break
		}
		if _, err := runtime.Select(ctx, actions[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.Snapshot().LogicalTime != 4 {
		t.Fatalf("logical time before deadline-5 actions = %d, want 4", runtime.Snapshot().LogicalTime)
	}
}

func TestOfferInvokeRejectsIneligibleInputWithoutQueueing(t *testing.T) {
	ctx := context.Background()
	adapter := &invokeGateAdapter{Adapter: fixture.New()}
	runtime, err := controlruntime.New(ctx, adapter, runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpSleep, Delay: 5})
	if err != nil {
		t.Fatal(err)
	}
	before, err := runtime.Snapshot().Digest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.OfferInvoke(ctx, "n1", payload); !errors.Is(err, controlruntime.ErrInvokeNotEligible) {
		t.Fatalf("OfferInvoke() error = %v, want ErrInvokeNotEligible", err)
	}
	after, err := runtime.Snapshot().Digest()
	if err != nil {
		t.Fatal(err)
	}
	if before != after || len(runtime.Snapshot().Offered) != 0 {
		t.Fatal("ineligible invoke changed Runtime state")
	}

	adapter.allow = true
	id, err := runtime.OfferInvoke(ctx, "n1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, id); err != nil {
		t.Fatal(err)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(
		ctx, &invokeGateAdapter{Adapter: fixture.New(), allow: true}, runtimeConfig(), trace,
	); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func newRuntime(t *testing.T) *controlruntime.Runtime {
	t.Helper()
	runtime, err := controlruntime.New(context.Background(), fixture.New(), runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func runtimeConfig() controlruntime.Config {
	return controlruntime.Config{Seed: []byte("runtime-fixture-seed"), ClockError: 0, MaxClones: 2}
}

func invokeMessage(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime, operation string) control.ItemID {
	t.Helper()
	payload, err := fixture.InputPayload(fixture.Input{Operation: operation, Target: "n2", Value: "value"})
	if err != nil {
		t.Fatal(err)
	}
	actionID, err := runtime.OfferInvoke(ctx, "n1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, actionID); err != nil {
		t.Fatal(err)
	}
	return firstItemOfKind(t, runtime.Snapshot(), control.ItemMessage)
}

func mustEnabled(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime) []control.Action {
	t.Helper()
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return actions
}

func actionsOfKind(actions []control.Action, kind control.ActionKind) []control.Action {
	var result []control.Action
	for _, action := range actions {
		if action.Kind == kind {
			result = append(result, action)
		}
	}
	return result
}

func actionForNode(t *testing.T, actions []control.Action, node control.NodeID) control.Action {
	t.Helper()
	for _, action := range actions {
		if action.Node.Node == node {
			return action
		}
	}
	t.Fatalf("no action for node %s in %+v", node, actions)
	return control.Action{}
}

func actionForItem(t *testing.T, actions []control.Action, kind control.ActionKind, item control.ItemID) control.Action {
	t.Helper()
	for _, action := range actions {
		if action.Kind == kind && action.Item == item {
			return action
		}
	}
	t.Fatalf("no %s action for item %s", kind, item)
	return control.Action{}
}

func hasActionForItem(actions []control.Action, kind control.ActionKind, item control.ItemID) bool {
	for _, action := range actions {
		if action.Kind == kind && action.Item == item {
			return true
		}
	}
	return false
}

func assertActionForItem(t *testing.T, actions []control.Action, kind control.ActionKind, item control.ItemID, want bool) {
	t.Helper()
	if got := hasActionForItem(actions, kind, item); got != want {
		t.Fatalf("has %s for %s = %v, want %v", kind, item, got, want)
	}
}

func firstItemOfKind(t *testing.T, snapshot controlruntime.Snapshot, kind control.ItemKind) control.ItemID {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Kind == kind {
			return item.ID
		}
	}
	t.Fatalf("no item of kind %s", kind)
	return ""
}

func assertItemState(t *testing.T, snapshot controlruntime.Snapshot, id control.ItemID, want control.ItemState) {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.ID == id {
			if item.State != want {
				t.Fatalf("item %s state = %s, want %s", id, item.State, want)
			}
			return
		}
	}
	t.Fatalf("item %s not found", id)
}

func durableEffects(t *testing.T, snapshot controlruntime.Snapshot, node control.NodeID) uint64 {
	t.Helper()
	for _, state := range snapshot.Nodes {
		if state.Ref.Node == node {
			return state.DurableEffects
		}
	}
	t.Fatalf("node %s not found", node)
	return 0
}

func temporalItemOfKind(t *testing.T, runtime *controlruntime.Runtime, kind control.TemporalKind) control.ItemID {
	t.Helper()
	for _, item := range runtime.Snapshot().Items {
		if item.Kind == control.ItemTemporal && item.State == control.ItemBlocked &&
			item.Value.Temporal != nil && item.Value.Temporal.Kind == kind {
			return item.ID
		}
	}
	t.Fatalf("no blocked temporal item of kind %s", kind)
	return ""
}

func isTerminalState(state control.ItemState) bool {
	switch state {
	case control.ItemCompleted, control.ItemCanceled, control.ItemFailed, control.ItemDropped:
		return true
	default:
		return false
	}
}

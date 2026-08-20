package hashicorpraftv2

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestConfiguredFiveNodeMembershipUsesTheDefaultRuntimePath(t *testing.T) {
	adapter, err := NewWithConfig(Config{NodeCount: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	manifest, err := adapter.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []control.NodeID{"n1", "n2", "n3", "n4", "n5"}
	if !reflect.DeepEqual(manifest.Nodes, want) {
		t.Fatalf("manifest nodes=%v, want %v", manifest.Nodes, want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{Seed: []byte("hashicorp-five-node")})
	if err != nil {
		t.Fatal(err)
	}
	if initial := enabledMessages(t, ctx, runtime, "request-vote"); len(initial) != 4 {
		t.Fatalf("initial vote requests=%d, want 4", len(initial))
	}
}

func TestConfiguredMembershipDefaultsAndBounds(t *testing.T) {
	implicit := NewAdapter()
	explicit, err := NewWithConfig(Config{NodeCount: defaultNodeCount})
	if err != nil {
		t.Fatal(err)
	}
	left, err := implicit.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	right, err := explicit.Manifest(context.Background())
	if err != nil || left.ConfigurationDigest != right.ConfigurationDigest {
		t.Fatalf("omitted and explicit default membership diverged: left=%s right=%s err=%v",
			left.ConfigurationDigest, right.ConfigurationDigest, err)
	}
	if _, err := NewWithConfig(Config{NodeCount: maxStaticNodes + 1}); err == nil {
		t.Fatal("oversized HashiCorp membership was accepted")
	}
}

func TestThreeNodeRuntimeDeliverDropAndFSMApply(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	adapter := NewAdapter()
	defer adapter.Close()
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{Seed: []byte("hashicorp-m5.4b")})
	if err != nil {
		t.Fatal(err)
	}

	initial := enabledMessages(t, ctx, runtime, "request-vote")
	if len(initial) != 2 {
		t.Fatalf("got %d initial votes, want 2", len(initial))
	}
	selectMessage(t, ctx, runtime, initial, "n3", control.ActionDropMessage)
	for _, item := range initial {
		if item.Value.Message.Target == "n3" {
			if _, retained := adapter.pendingRPC[item.ID]; retained {
				t.Fatal("dropped synchronous RPC is still retained by Adapter")
			}
		}
	}
	selectMessage(t, ctx, runtime, initial, "n2", control.ActionDeliverMessage)

	appendEntries := enabledMessages(t, ctx, runtime, "append-entries")
	if len(appendEntries) != 2 {
		t.Fatalf("got %d initial appends, want 2", len(appendEntries))
	}
	selectMessage(t, ctx, runtime, appendEntries, "n3", control.ActionDropMessage)
	selectMessage(t, ctx, runtime, appendEntries, "n2", control.ActionDeliverMessage)

	payload, err := InputPayload([]byte("opaque-command-m5.4b"))
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := runtime.OfferInvoke(ctx, "n1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, invoke); err != nil {
		t.Fatal(err)
	}

	for decision := 0; decision < 16 && !hasApplyObservation(runtime.Snapshot()); decision++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var selected *control.Action
		for index := range actions {
			if actions[index].Kind == control.ActionDeliverMessage {
				selected = &actions[index]
				break
			}
		}
		if selected == nil {
			t.Fatalf("no delivery can progress apply: %+v", actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	if !hasApplyObservation(runtime.Snapshot()) {
		t.Fatal("opaque command did not reach FSM.Apply")
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("runtime slice trace=%s decisions=%d", trace.Digest, len(trace.Records))
	for _, kind := range []control.ActionKind{control.ActionDropMessage, control.ActionDeliverMessage, control.ActionInvoke} {
		if !traceHasAction(trace, kind) {
			t.Fatalf("trace does not contain %s", kind)
		}
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReleasedMessageSurvivesSourceAndTargetRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	adapter := NewAdapter()
	defer adapter.Close()
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{Seed: []byte("hashicorp-m5.4c")})
	if err != nil {
		t.Fatal(err)
	}

	initial := enabledMessages(t, ctx, runtime, "request-vote")
	message := messageForTarget(t, initial, "n2")
	stores := adapter.nodes["n1"].stores
	termBefore, err := stores.stable.GetUint64([]byte("CurrentTerm"))
	if err != nil || termBefore == 0 {
		t.Fatalf("read durable term before crash: term=%d err=%v", termBefore, err)
	}

	selectNodeAction(t, ctx, runtime, control.ActionCrash, "n1")
	if state, ok := snapshotItem(runtime.Snapshot(), message.ID); !ok || state.State != control.ItemEnabled {
		t.Fatalf("released message changed after source crash: %+v", state)
	}
	selectNodeAction(t, ctx, runtime, control.ActionRestart, "n1")
	if adapter.nodes["n1"].stores != stores || adapter.nodes["n1"].incarnation != 2 {
		t.Fatal("restart replaced durable stores or did not advance incarnation")
	}
	termAfter, err := stores.stable.GetUint64([]byte("CurrentTerm"))
	if err != nil || termAfter != termBefore {
		t.Fatalf("durable term changed across restart: before=%d after=%d err=%v", termBefore, termAfter, err)
	}

	selectNodeAction(t, ctx, runtime, control.ActionCrash, "n2")
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hasItemAction(actions, control.ActionDeliverMessage, message.ID) || !hasItemAction(actions, control.ActionDropMessage, message.ID) {
		t.Fatal("stopped target exposed invalid message actions")
	}
	selectNodeAction(t, ctx, runtime, control.ActionRestart, "n2")
	actions, err = runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deliver, ok := itemAction(actions, control.ActionDeliverMessage, message.ID)
	if !ok || deliver.Node.Incarnation != 2 {
		t.Fatalf("message not deliverable to restarted target: %+v", deliver)
	}
	if _, err := runtime.Select(ctx, deliver.ID); err != nil {
		t.Fatal(err)
	}
	if state, ok := snapshotItem(runtime.Snapshot(), message.ID); !ok || state.State != control.ItemCompleted {
		t.Fatalf("old message was not completed after delivery: %+v", state)
	}
}

func enabledMessages(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime, kind string) []controlruntime.ItemSnapshot {
	t.Helper()
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[control.ItemID]struct{})
	var result []controlruntime.ItemSnapshot
	for _, action := range actions {
		if action.Kind != control.ActionDeliverMessage {
			continue
		}
		item, ok := snapshotItem(runtime.Snapshot(), action.Item)
		if ok && item.Value.Message.TypeHint == kind {
			if _, duplicate := seen[item.ID]; !duplicate {
				seen[item.ID] = struct{}{}
				result = append(result, item)
			}
		}
	}
	return result
}

func selectMessage(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime, items []controlruntime.ItemSnapshot, target control.NodeID, kind control.ActionKind) {
	t.Helper()
	var id control.ItemID
	for _, item := range items {
		if item.Value.Message.Target == target {
			id = item.ID
		}
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		if action.Kind == kind && action.Item == id {
			if _, err := runtime.Select(ctx, action.ID); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no %s for %s message %s", kind, target, id)
}

func messageForTarget(t *testing.T, items []controlruntime.ItemSnapshot, target control.NodeID) controlruntime.ItemSnapshot {
	t.Helper()
	for _, item := range items {
		if item.Value.Message.Target == target {
			return item
		}
	}
	t.Fatalf("no message for target %s", target)
	return controlruntime.ItemSnapshot{}
}

func selectNodeAction(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime, kind control.ActionKind, node control.NodeID) {
	t.Helper()
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		if action.Kind == kind && action.Node.Node == node {
			if _, err := runtime.Select(ctx, action.ID); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no %s action for node %s", kind, node)
}

func itemAction(actions []control.Action, kind control.ActionKind, item control.ItemID) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind && action.Item == item {
			return action, true
		}
	}
	return control.Action{}, false
}

func hasItemAction(actions []control.Action, kind control.ActionKind, item control.ItemID) bool {
	_, ok := itemAction(actions, kind, item)
	return ok
}

func snapshotItem(snapshot controlruntime.Snapshot, id control.ItemID) (controlruntime.ItemSnapshot, bool) {
	for _, item := range snapshot.Items {
		if item.ID == id {
			return item, true
		}
	}
	return controlruntime.ItemSnapshot{}, false
}

func hasApplyObservation(snapshot controlruntime.Snapshot) bool {
	for _, item := range snapshot.Items {
		if item.Value.Observation != nil && item.Value.Observation.Kind == "fsm-apply" {
			return true
		}
	}
	return false
}

func traceHasAction(trace controlruntime.Trace, kind control.ActionKind) bool {
	for _, record := range trace.Records {
		if record.Action.Kind == kind {
			return true
		}
	}
	return false
}

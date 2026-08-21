package omnipaxosv2

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestNonLeaderOpaqueAppendDecisionResultAndFreshWorkerReplay(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	adapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{Seed: []byte("omnipaxos-m5.21w")})
	if err != nil {
		t.Fatal(err)
	}
	firstPID := adapter.worker.cmd.Process.Pid
	driveToLeader(t, ctx, runtime, adapter)
	payload, err := InputPayload(Input{RequestID: "request-1", Value: []byte("opaque-m5.21w")})
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := runtime.OfferInvoke(ctx, "n2", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, invoke); err != nil {
		t.Fatal(err)
	}
	for decisions := 0; decisions < 256 && clientResponse(runtime.Snapshot(), "request-1") == nil; decisions++ {
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			t.Fatal(enabledErr)
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no action can progress append: %+v", actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	assertClientResult(t, clientResponse(runtime.Snapshot(), "request-1"))
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
	history, err := (ObservationProjector{}).Project(trace)
	if err != nil {
		t.Fatal(err)
	}
	observed := make(map[semantic.ObservationKind]int)
	requestObserved := false
	decisionRequestObserved := false
	promiseObserved := false
	for _, event := range history.Events {
		observed[event.Kind]++
		requestObserved = requestObserved || event.RequestID == "request-1"
		decisionRequestObserved = decisionRequestObserved ||
			event.Kind == semantic.ObservationDecisionAdvanced && event.RequestID == "request-1"
		promiseObserved = promiseObserved || event.Kind == ObservationPromiseRaised &&
			len(event.Attributes) >= 2
	}
	if observed[semantic.ObservationWorkloadInvoked] == 0 ||
		observed[semantic.ObservationDecisionAdvanced] == 0 || !requestObserved ||
		!decisionRequestObserved || !promiseObserved {
		t.Fatalf("common observation projection incomplete: observed=%v request=%v", observed, requestObserved)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}

	replayAdapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	replayRuntime, err := controlruntime.Replay(ctx, replayAdapter, controlruntime.Config{
		Seed: []byte("omnipaxos-m5.21w"),
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

func TestNaturalElectionMessageControlsAndFreshWorkerReplay(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	adapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{
		Seed: []byte("omnipaxos-m5.21v"), MaxClones: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstPID := adapter.worker.cmd.Process.Pid
	if err := exerciseMessageControls(t, ctx, runtime); err != nil {
		t.Fatal(err)
	}
	for decisions := 0; decisions < 256 && consensusLeader(adapter) == ""; decisions++ {
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			t.Fatal(enabledErr)
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no progress action at decision %d: %+v", decisions, actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	if got := consensusLeader(adapter); got != "n1" {
		t.Fatalf("leader=%q, want n1", got)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []control.ActionKind{
		control.ActionFireTemporal, control.ActionDuplicateMessage,
		control.ActionDropMessage, control.ActionDeliverMessage,
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
	replayRuntime, err := controlruntime.Replay(ctx, replayAdapter, controlruntime.Config{
		Seed: []byte("omnipaxos-m5.21v"), MaxClones: 1,
	}, trace)
	if err != nil {
		t.Fatal(err)
	}
	secondPID := replayAdapter.worker.cmd.Process.Pid
	if secondPID == firstPID {
		t.Fatalf("replay reused worker pid %d", firstPID)
	}
	if got := consensusLeader(replayAdapter); got != "n1" {
		t.Fatalf("replay leader=%q, want n1", got)
	}
	replayedTrace, err := replayRuntime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if replayedTrace.Digest != trace.Digest {
		t.Fatalf("trace digest mismatch: %s != %s", replayedTrace.Digest, trace.Digest)
	}
	if err := replayAdapter.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("trace=%s decisions=%d worker_pids=%d,%d", trace.Digest, len(trace.Records), firstPID, secondPID)
}

func TestManifestKeepsM521wBoundary(t *testing.T) {
	adapter, err := New(Config{WorkerPath: buildWorker(t)})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := adapter.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []control.ActionKind{
		control.ActionInvoke,
		control.ActionDropMessage, control.ActionDuplicateMessage,
		control.ActionDeliverMessage, control.ActionFireTemporal,
	} {
		if !hasAction(manifest.Capabilities.Actions, kind) {
			t.Fatalf("missing action capability %s", kind)
		}
	}
	for _, forbidden := range []control.ActionKind{
		control.ActionCrash, control.ActionRestart,
		control.ActionCompleteEffect,
	} {
		if hasAction(manifest.Capabilities.Actions, forbidden) {
			t.Fatalf("unexpected action capability %s", forbidden)
		}
	}
	if len(manifest.Capabilities.Items) != 3 ||
		manifest.Capabilities.Items[0] != control.ItemMessage ||
		manifest.Capabilities.Items[1] != control.ItemTemporal ||
		manifest.Capabilities.Items[2] != control.ItemClientResult ||
		len(manifest.Capabilities.EffectKinds) != 0 {
		t.Fatalf("unexpected item/effect capabilities: %+v", manifest.Capabilities)
	}
	if manifest.Capabilities.Message == nil ||
		len(manifest.Capabilities.Message.TypeHints) != len(messageTypeHints) ||
		len(manifest.Capabilities.Message.MetadataKeys) != len(messageMetadataKeys) {
		t.Fatalf("unexpected message capability: %+v", manifest.Capabilities.Message)
	}
	risk, err := semantic.QualifyRisk([]semantic.ObservationPredicate{{
		MilestoneID: "restart", Kind: semantic.ObservationNodeRestarted,
	}}, (ObservationProjector{}).Capabilities(), manifest.Capabilities.Actions)
	if err != nil {
		t.Fatal(err)
	}
	if risk.Qualified || len(risk.Issues) != 2 ||
		risk.Issues[0].Code != semantic.RiskIssueMissingAction ||
		risk.Issues[0].Action != control.ActionRestart ||
		risk.Issues[1].Code != semantic.RiskIssueMissingObservationKind ||
		risk.Issues[1].Kind != semantic.ObservationNodeRestarted {
		t.Fatalf("unsupported restart Risk was not rejected mechanically: %#v", risk)
	}
}

func TestConfiguredFiveNodeMembershipElectsAndReplays(t *testing.T) {
	workerPath := buildWorker(t)
	config := Config{WorkerPath: workerPath, NodeCount: 5}
	adapter, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := adapter.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantNodes := []control.NodeID{"n1", "n2", "n3", "n4", "n5"}
	if !reflect.DeepEqual(manifest.Nodes, wantNodes) {
		t.Fatalf("manifest nodes=%v, want %v", manifest.Nodes, wantNodes)
	}
	defaultAdapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	defaultManifest, err := defaultAdapter.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if defaultManifest.ConfigurationDigest == manifest.ConfigurationDigest ||
		len(defaultManifest.Nodes) != DefaultNodeCount {
		t.Fatalf("configured membership did not change manifest identity: default=%+v configured=%+v",
			defaultManifest, manifest)
	}
	explicitDefault, err := New(Config{WorkerPath: workerPath, NodeCount: DefaultNodeCount})
	if err != nil {
		t.Fatal(err)
	}
	explicitManifest, err := explicitDefault.Manifest(context.Background())
	if err != nil || explicitManifest.ConfigurationDigest != defaultManifest.ConfigurationDigest {
		t.Fatalf("omitted and explicit default membership diverged: omitted=%s explicit=%s err=%v",
			defaultManifest.ConfigurationDigest, explicitManifest.ConfigurationDigest, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{Seed: []byte("omnipaxos-five-node")})
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.last.Nodes) != 5 {
		t.Fatalf("worker nodes=%d, want 5", len(adapter.last.Nodes))
	}
	driveToLeader(t, ctx, runtime, adapter)
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	replayAdapter, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := controlruntime.Replay(
		ctx, replayAdapter, controlruntime.Config{Seed: []byte("omnipaxos-five-node")}, trace,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayedTrace, err := replayed.Trace()
	if err != nil || replayedTrace.Digest != trace.Digest {
		t.Fatalf("five-node replay drift: got=%s want=%s err=%v", replayedTrace.Digest, trace.Digest, err)
	}
	if err := replayAdapter.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNodeCountIsRejectedBeforeWorkerAccess(t *testing.T) {
	_, err := New(Config{WorkerPath: "/not/read/for/invalid/node/count", NodeCount: MaxStaticNodes + 1})
	if err == nil || !strings.Contains(err.Error(), "OMNIPAXOS_NODE_COUNT_INVALID") {
		t.Fatalf("oversized membership was not rejected before worker access: %v", err)
	}
}

func TestLeafMessageSemanticsDisambiguateSameRouteWithoutChangingPayloadOrReplay(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	seed := []byte("omnipaxos-leaf-message-semantics-v1")
	config := controlruntime.Config{Seed: seed, MaxClones: 1}
	adapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, adapter, config)
	if err != nil {
		t.Fatal(err)
	}

	var preserved controlruntime.ItemSnapshot
	for decisions := 0; decisions < 256 && preserved.ID == ""; decisions++ {
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			t.Fatal(enabledErr)
		}
		for _, action := range actions {
			item, ok := snapshotItem(runtime.Snapshot(), action.Item)
			if action.Kind != control.ActionDuplicateMessage || !ok || item.Value.Message == nil ||
				item.Value.Message.TypeHint != "sequence-paxos/prepare" {
				continue
			}
			preserved = item
			originalBytes := append([]byte(nil), item.Value.Message.Payload.Bytes...)
			if _, err := runtime.Select(ctx, action.ID); err != nil {
				t.Fatal(err)
			}
			clone, ok := cloneOf(runtime.Snapshot(), item.Value.Message.ID)
			if !ok || clone.Value.Message == nil ||
				!bytes.Equal(clone.Value.Message.Payload.Bytes, originalBytes) {
				t.Fatalf("duplicated Prepare changed payload: %#v", clone)
			}
			actions, enabledErr = runtime.EnabledActions(ctx)
			if enabledErr != nil {
				t.Fatal(enabledErr)
			}
			deliver, ok := actionForItem(actions, control.ActionDeliverMessage, clone.ID)
			if !ok {
				t.Fatalf("deliver action missing for Prepare clone %s", clone.ID)
			}
			if _, err := runtime.Select(ctx, deliver.ID); err != nil {
				t.Fatal(err)
			}
			break
		}
		if preserved.ID != "" {
			break
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no action can reach Prepare: %+v", actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	if preserved.ID == "" || preserved.Value.Message == nil {
		t.Fatal("no Sequence Paxos Prepare was preserved")
	}
	for decisions := 0; decisions < 256 && consensusLeader(adapter) != "n1"; decisions++ {
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			t.Fatal(enabledErr)
		}
		selected, ok := progressActionExcept(actions, preserved.ID)
		if !ok {
			t.Fatalf("no action can complete election while preserving %s", preserved.ID)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	if consensusLeader(adapter) != "n1" {
		t.Fatal("n1 did not become the common leader")
	}
	payload, err := InputPayload(Input{RequestID: "leaf-semantics-request", Value: []byte("value")})
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

	var selectedMessage controlruntime.ItemSnapshot
	coarseMatches, exactMatches := 0, 0
	for decisions := 0; decisions < 256; decisions++ {
		snapshot := runtime.Snapshot()
		coarseMatches, exactMatches = 0, 0
		for _, item := range snapshot.Items {
			if item.State != control.ItemEnabled || item.Value.Message == nil ||
				item.Value.Message.Source != preserved.Value.Message.Source ||
				item.Value.Message.Target != preserved.Value.Message.Target ||
				!strings.HasPrefix(item.Value.Message.TypeHint, "sequence-paxos/") {
				continue
			}
			coarseMatches++
			if item.Value.Message.TypeHint == "sequence-paxos/accept-decide" {
				exactMatches++
				selectedMessage = item
			}
		}
		if coarseMatches > 1 && exactMatches == 1 {
			break
		}
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			t.Fatal(enabledErr)
		}
		selected, ok := progressActionExcept(actions, preserved.ID)
		if !ok {
			t.Fatalf("no action can reach same-route AcceptDecide: %+v", actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	if coarseMatches <= 1 || exactMatches != 1 || selectedMessage.Value.Message == nil {
		t.Fatalf("selector contrast not established: coarse=%d exact=%d", coarseMatches, exactMatches)
	}
	metadata := selectedMessage.Value.Message.Metadata
	if metadata["request_id"] != "leaf-semantics-request" || metadata["entry_count"] != "1" ||
		metadata["ballot_number"] == "" || metadata["sequence_counter"] == "" {
		t.Fatalf("AcceptDecide metadata incomplete: %#v", metadata)
	}
	preservedNow, ok := snapshotItem(runtime.Snapshot(), preserved.ID)
	if !ok || preservedNow.State != control.ItemEnabled || preservedNow.Value.Message == nil ||
		!bytes.Equal(preservedNow.Value.Message.Payload.Bytes, preserved.Value.Message.Payload.Bytes) {
		t.Fatal("descriptive semantics changed the preserved Prepare")
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deliver, ok := actionForItem(actions, control.ActionDeliverMessage, selectedMessage.ID)
	if !ok {
		t.Fatalf("exact AcceptDecide is not deliverable: %s", selectedMessage.ID)
	}
	if _, err := runtime.Select(ctx, deliver.ID); err != nil {
		t.Fatal(err)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	replayAdapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := controlruntime.Replay(ctx, replayAdapter, config, trace)
	if err != nil {
		t.Fatal(err)
	}
	replayedTrace, err := replayed.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if replayedTrace.Digest != trace.Digest {
		t.Fatalf("replay digest mismatch: %s != %s", replayedTrace.Digest, trace.Digest)
	}
	if err := replayed.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("coarse_matches=%d exact_matches=%d decisions=%d trace=%s", coarseMatches, exactMatches, len(trace.Records), trace.Digest)
}

func TestWorkerRejectsUnsupportedOperation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := startWorker(ctx, buildWorker(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.call(ctx, workerRequest{Op: "reset"}); err != nil {
		t.Fatal(err)
	}
	_, err = client.call(ctx, workerRequest{Op: "not-a-worker-operation"})
	if err == nil || !strings.Contains(err.Error(), "OMNIPAXOS_WORKER_OPERATION_UNSUPPORTED") {
		t.Fatalf("unexpected rejection: %v", err)
	}
	if err := client.close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerAbnormalExitIsReported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := startWorker(ctx, buildWorker(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.call(ctx, workerRequest{Op: "reset"}); err != nil {
		t.Fatal(err)
	}
	if err := client.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := client.close(); err == nil || !strings.Contains(err.Error(), "OMNIPAXOS_WORKER_EXIT") {
		t.Fatalf("unexpected exit result: %v", err)
	}
}

func exerciseMessageControls(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
) error {
	t.Helper()
	for decision := 0; decision < 64; decision++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return err
		}
		duplicate, ok := firstAction(actions, control.ActionDuplicateMessage)
		if !ok {
			selected, progress := progressAction(actions)
			if !progress {
				t.Fatalf("no action can produce a message: %+v", actions)
			}
			if _, err := runtime.Select(ctx, selected.ID); err != nil {
				return err
			}
			continue
		}
		original, ok := snapshotItem(runtime.Snapshot(), duplicate.Item)
		if !ok || original.Value.Message == nil {
			t.Fatalf("duplicate source missing: %s", duplicate.Item)
		}
		if _, err := runtime.Select(ctx, duplicate.ID); err != nil {
			return err
		}
		clone, ok := cloneOf(runtime.Snapshot(), original.Value.Message.ID)
		if !ok {
			t.Fatalf("clone missing for %s", original.Value.Message.ID)
		}
		actions, err = runtime.EnabledActions(ctx)
		if err != nil {
			return err
		}
		drop, ok := actionForItem(actions, control.ActionDropMessage, original.ID)
		if !ok {
			t.Fatalf("drop action missing for %s", original.ID)
		}
		if _, err := runtime.Select(ctx, drop.ID); err != nil {
			return err
		}
		actions, err = runtime.EnabledActions(ctx)
		if err != nil {
			return err
		}
		deliver, ok := actionForItem(actions, control.ActionDeliverMessage, clone.ID)
		if !ok {
			t.Fatalf("deliver action missing for clone %s", clone.ID)
		}
		_, err = runtime.Select(ctx, deliver.ID)
		return err
	}
	t.Fatal("no message appeared within the control limit")
	return nil
}

func driveToLeader(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	adapter *Adapter,
) {
	t.Helper()
	for decisions := 0; decisions < 256 && consensusLeader(adapter) == ""; decisions++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no action can elect leader: %+v", actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	if got := consensusLeader(adapter); got != "n1" {
		t.Fatalf("leader=%q, want n1", got)
	}
}

func buildWorker(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required for the OmniPaxos binding integration test")
	}
	manifest := filepath.Join("worker", "Cargo.toml")
	command := exec.Command("cargo", "build", "--locked", "--quiet", "--manifest-path", manifest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build OmniPaxos worker: %v\n%s", err, output)
	}
	targetDir := filepath.Join("worker", "target")
	if configured := os.Getenv("CARGO_TARGET_DIR"); configured != "" {
		targetDir = configured
	}
	path, err := filepath.Abs(filepath.Join(targetDir, "debug", "consensus-atlas-omnipaxos-worker"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func progressAction(actions []control.Action) (control.Action, bool) {
	for _, kind := range []control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal} {
		if action, ok := firstAction(actions, kind); ok {
			return action, true
		}
	}
	return control.Action{}, false
}

func progressActionExcept(actions []control.Action, excluded control.ItemID) (control.Action, bool) {
	for _, kind := range []control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal} {
		for _, action := range actions {
			if action.Kind == kind && action.Item != excluded {
				return action, true
			}
		}
	}
	return control.Action{}, false
}

func firstAction(actions []control.Action, kind control.ActionKind) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind {
			return action, true
		}
	}
	return control.Action{}, false
}

func actionForItem(
	actions []control.Action,
	kind control.ActionKind,
	item control.ItemID,
) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind && action.Item == item {
			return action, true
		}
	}
	return control.Action{}, false
}

func snapshotItem(snapshot controlruntime.Snapshot, id control.ItemID) (controlruntime.ItemSnapshot, bool) {
	for _, item := range snapshot.Items {
		if item.ID == id {
			return item, true
		}
	}
	return controlruntime.ItemSnapshot{}, false
}

func cloneOf(snapshot controlruntime.Snapshot, message control.MessageID) (controlruntime.ItemSnapshot, bool) {
	for _, item := range snapshot.Items {
		if item.Value.Message != nil && item.Value.Message.CloneOf == message {
			return item, true
		}
	}
	return controlruntime.ItemSnapshot{}, false
}

func consensusLeader(adapter *Adapter) control.NodeID {
	if len(adapter.last.Nodes) != adapter.config.ResolvedNodeCount() || adapter.last.Nodes[0].Leader == 0 {
		return ""
	}
	leader := adapter.last.Nodes[0].Leader
	for _, node := range adapter.last.Nodes[1:] {
		if node.Leader != leader {
			return ""
		}
	}
	return nodeName(leader)
}

func traceHasAction(trace controlruntime.Trace, kind control.ActionKind) bool {
	for _, record := range trace.Records {
		if record.Action.Kind == kind {
			return true
		}
	}
	return false
}

func hasAction(actions []control.ActionKind, want control.ActionKind) bool {
	for _, action := range actions {
		if action == want {
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
	if response == nil || response.Status != "decided" || response.Owner.Node != "n2" ||
		response.Payload.SchemaVersion != resultSchema {
		t.Fatalf("invalid client response: %+v", response)
	}
	var result workerDecision
	if err := json.Unmarshal(response.Payload.Bytes, &result); err != nil {
		t.Fatal(err)
	}
	if result.Index != 1 || string(result.Value) != "opaque-m5.21w" {
		t.Fatalf("invalid result payload: %+v", result)
	}
}

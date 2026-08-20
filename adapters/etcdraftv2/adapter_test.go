package etcdraftv2_test

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type evidenceState struct {
	Node                control.NodeID `json:"node"`
	Incarnation         uint64         `json:"incarnation"`
	Running             bool           `json:"running"`
	Role                string         `json:"role"`
	Term                uint64         `json:"term"`
	StorageTerm         uint64         `json:"storage_term"`
	StorageCommit       uint64         `json:"storage_commit"`
	StorageLastIndex    uint64         `json:"storage_last_index"`
	AdvanceCount        uint64         `json:"advance_count"`
	TickCount           uint64         `json:"tick_count"`
	ConfState           confStateView  `json:"conf_state"`
	DurableDigest       string         `json:"durable_image_digest"`
	ApplicationDigest   string         `json:"application_digest"`
	ApplicationCommands int            `json:"application_commands"`
}

type clusterEvidence struct {
	Nodes []evidenceState `json:"nodes"`
}

type confStateView struct {
	Voters []uint64 `json:"voters"`
}

func TestSingleNodeBootstrapReadyPulseAndReplay(t *testing.T) {
	ctx := context.Background()
	originalReader := cryptorand.Reader
	runtime, err := controlruntime.New(ctx, etcdraftv2.New(), runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if cryptorand.Reader != originalReader {
		t.Fatal("Adapter construction leaked the scoped crypto/rand.Reader")
	}

	initial := runtime.Snapshot()
	if countItems(initial, control.ItemEffect, control.ItemEnabled) != 1 {
		t.Fatalf("initial snapshot = %+v, want one enabled Ready effect", initial.Items)
	}
	if countItems(initial, control.ItemTemporal, control.ItemEnabled) != 0 {
		t.Fatal("periodic pulse became visible before bootstrap Ready was advanced")
	}

	var leaderSeen bool
	var final evidenceState
	for decision := 0; decision < 64; decision++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		action, ok := preferredAction(actions)
		if !ok {
			t.Fatalf("no Ready effect or periodic pulse at decision %d: %+v", decision, actions)
		}
		record, err := runtime.Select(ctx, action.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cryptorand.Reader != originalReader {
			t.Fatal("selected action leaked the scoped crypto/rand.Reader")
		}
		if record.Evidence == nil {
			t.Fatal("adapter-directed action has no evidence")
		}
		final = evidenceForNode(t, decodeEvidence(t, *record.Evidence), "n1")
		if final.Role == "StateLeader" {
			leaderSeen = true
		}
		if leaderSeen && action.Kind == control.ActionCompleteEffect &&
			hasKind(mustEnabled(t, ctx, runtime), control.ActionFireTemporal) {
			break
		}
	}
	if !leaderSeen {
		t.Fatal("natural periodic pulses did not elect the single node within the decision bound")
	}
	if final.AdvanceCount < 2 || final.TickCount < 1 || final.StorageLastIndex < 2 ||
		final.StorageTerm == 0 || len(final.ConfState.Voters) != 1 || final.ConfState.Voters[0] != 1 {
		t.Fatalf("final evidence does not show persisted bootstrap/election Ready state: %+v", final)
	}

	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Records) == 0 {
		t.Fatal("trace has no decisions")
	}
	if _, err := controlruntime.Replay(ctx, etcdraftv2.New(), runtimeConfig(), trace); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	assertEntropyDomain(t, trace)
}

func TestManifestKeepsSingleNodeMessageLimitExplicit(t *testing.T) {
	manifest, err := etcdraftv2.New().Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ImplementationID != "go.etcd.io/raft/v3@v3.6.0" || len(manifest.Nodes) != 1 ||
		!manifest.Capabilities.StrictReplay || !manifest.Capabilities.Entropy.StrictReplay {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, kind := range manifest.Capabilities.Items {
		if kind == control.ItemMessage {
			t.Fatal("single-node slice falsely declared message support")
		}
	}
	if !hasActionKind(manifest.Capabilities.Actions, control.ActionCrash) ||
		!hasActionKind(manifest.Capabilities.Actions, control.ActionRestart) ||
		len(manifest.Capabilities.CrashModes) != 1 || manifest.Capabilities.CrashModes[0] != "power-loss" {
		t.Fatal("single-node slice did not declare its lifecycle surface")
	}
}

func TestCollectAndEnabledInspectionArePure(t *testing.T) {
	ctx := context.Background()
	adapter := etcdraftv2.New()
	if err := adapter.Reset(ctx, []byte("etcdraft-v2-purity-seed")); err != nil {
		t.Fatal(err)
	}
	yield, err := adapter.RunUntilYield(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := adapter.Collect(ctx, yield.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Collect(ctx, yield.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("Collect digest differs: %s != %s", first.Digest, second.Digest)
	}
	first.Items[0].Effect.Request.Bytes[0] ^= 1
	third, err := adapter.Collect(ctx, yield.ID)
	if err != nil {
		t.Fatal(err)
	}
	if third.Digest != second.Digest || third.Items[0].Effect.Request.Digest != second.Items[0].Effect.Request.Digest {
		t.Fatal("caller mutation changed frozen Ready emission")
	}

	runtime, err := controlruntime.New(ctx, etcdraftv2.New(), runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	before, err := runtime.Snapshot().Digest()
	if err != nil {
		t.Fatal(err)
	}
	left := mustEnabled(t, ctx, runtime)
	right := mustEnabled(t, ctx, runtime)
	after, err := runtime.Snapshot().Digest()
	if err != nil {
		t.Fatal(err)
	}
	leftDigest, err := control.CanonicalDigest(left)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := control.CanonicalDigest(right)
	if err != nil {
		t.Fatal(err)
	}
	if before != after || leftDigest != rightDigest {
		t.Fatal("enabled inspection mutated Adapter or Runtime state")
	}
}

func TestConfigurationDigestBindsTopologyAndTickParameters(t *testing.T) {
	ctx := context.Background()
	leftConfig := etcdraftv2.ThreeNodeConfig()
	rightConfig := etcdraftv2.ThreeNodeConfig()
	rightConfig.Nodes[0], rightConfig.Nodes[2] = rightConfig.Nodes[2], rightConfig.Nodes[0]
	left := mustAdapter(t, leftConfig)
	right := mustAdapter(t, rightConfig)
	leftManifest, err := left.Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := right.Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if leftManifest.ConfigurationDigest != rightManifest.ConfigurationDigest {
		t.Fatal("configuration digest depends on input node order")
	}
	rightConfig.ElectionTick++
	changedManifest, err := mustAdapter(t, rightConfig).Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changedManifest.ConfigurationDigest == leftManifest.ConfigurationDigest {
		t.Fatal("configuration digest ignored ElectionTick")
	}
	if len(leftManifest.Nodes) != 3 || !hasActionKind(leftManifest.Capabilities.Actions, control.ActionDeliverMessage) ||
		!hasItemKind(leftManifest.Capabilities.Items, control.ItemMessage) {
		t.Fatalf("three-node manifest does not declare its message surface: %+v", leftManifest)
	}
}

func TestThreeNodeBootstrapDrainsBeforePulses(t *testing.T) {
	ctx := context.Background()
	runtime, err := controlruntime.New(ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got := countItems(runtime.Snapshot(), control.ItemEffect, control.ItemEnabled); got != 3 {
		t.Fatalf("initial Ready effects = %d, want 3", got)
	}
	if hasKind(mustEnabled(t, ctx, runtime), control.ActionFireTemporal) {
		t.Fatal("pulse exposed before all bootstrap Ready effects completed")
	}
	drainClusterEffects(t, ctx, runtime, 24)
	pulses := actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionFireTemporal)
	if len(pulses) != 3 {
		t.Fatalf("initial cluster pulses = %d, want 3", len(pulses))
	}
	for _, pulse := range pulses {
		if pulse.Node.Incarnation != 1 {
			t.Fatalf("pulse has wrong incarnation: %+v", pulse)
		}
	}
}

func TestCustomStaticNodeCountNeedsNoAdapterChange(t *testing.T) {
	ctx := context.Background()
	config := etcdraftv2.Config{
		Nodes: []etcdraftv2.NodeConfig{
			{Node: "n4", RaftID: 44}, {Node: "n2", RaftID: 22},
			{Node: "n1", RaftID: 11}, {Node: "n3", RaftID: 33},
		},
		ElectionTick: 7, HeartbeatTick: 2,
	}
	runtime, err := controlruntime.New(ctx, mustAdapter(t, config), clusterRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got := countItems(runtime.Snapshot(), control.ItemEffect, control.ItemEnabled); got != 4 {
		t.Fatalf("custom cluster bootstrap Ready effects = %d, want 4", got)
	}
	drainClusterEffects(t, ctx, runtime, 32)
	if got := len(actionsOfKind(mustEnabled(t, ctx, runtime), control.ActionFireTemporal)); got != 4 {
		t.Fatalf("custom cluster pulses = %d, want 4", got)
	}
}

func TestCompactNodeCountBuildsConventionalStaticMembership(t *testing.T) {
	ctx := context.Background()
	adapter := mustAdapter(t, etcdraftv2.Config{
		NodeCount: 5, ElectionTick: 7, HeartbeatTick: 2,
	})
	manifest, err := adapter.Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []control.NodeID{"n1", "n2", "n3", "n4", "n5"}
	if !reflect.DeepEqual(manifest.Nodes, want) {
		t.Fatalf("compact membership = %v, want %v", manifest.Nodes, want)
	}
	if _, err := etcdraftv2.NewWithConfig(etcdraftv2.Config{
		NodeCount:    3,
		Nodes:        []etcdraftv2.NodeConfig{{Node: "custom", RaftID: 11}},
		ElectionTick: 5, HeartbeatTick: 1,
	}); err == nil || !strings.Contains(err.Error(), "NODE_CONFIG_AMBIGUOUS") {
		t.Fatalf("ambiguous compact and explicit membership accepted: %v", err)
	}
}

func TestNodeCountIsBoundedBeforeMembershipExpansion(t *testing.T) {
	for _, config := range []etcdraftv2.Config{
		{NodeCount: etcdraftv2.MaxStaticNodes + 1, ElectionTick: 5, HeartbeatTick: 1},
		{NodeCount: int(^uint(0) >> 1), ElectionTick: 5, HeartbeatTick: 1},
		{
			Nodes:         make([]etcdraftv2.NodeConfig, etcdraftv2.MaxStaticNodes+1),
			ElectionTick:  5,
			HeartbeatTick: 1,
		},
	} {
		if _, err := etcdraftv2.NewWithConfig(config); err == nil ||
			!strings.Contains(err.Error(), "NODE_COUNT_INVALID") {
			t.Fatalf("oversized membership accepted: nodes=%d count=%d err=%v",
				len(config.Nodes), config.NodeCount, err)
		}
	}
}

func TestCrashBeforeBootstrapPersistenceRestartsFromEmptyDurableImage(t *testing.T) {
	ctx := context.Background()
	runtime, err := controlruntime.New(ctx, etcdraftv2.New(), runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	initialEffect := firstItemOfKindForNode(t, runtime.Snapshot(), control.ItemEffect, "n1")
	crash := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionCrash, "n1")
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		t.Fatal(err)
	}
	assertSnapshotItemState(t, runtime.Snapshot(), initialEffect, control.ItemCanceled)
	restart := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionRestart, "n1")
	if restart.Node.Incarnation != 2 {
		t.Fatalf("restart incarnation = %d, want 2", restart.Node.Incarnation)
	}
	record, err := runtime.Select(ctx, restart.ID)
	if err != nil {
		t.Fatal(err)
	}
	restarted := evidenceForNode(t, decodeEvidence(t, *record.Evidence), "n1")
	if !restarted.Running || restarted.Incarnation != 2 || restarted.StorageLastIndex != 0 {
		t.Fatalf("empty-image restart evidence = %+v", restarted)
	}
	effect := firstEnabledItemOfKindForNode(t, runtime.Snapshot(), control.ItemEffect, "n1")
	complete, ok := actionForItem(mustEnabled(t, ctx, runtime), control.ActionCompleteEffect, effect)
	if !ok {
		t.Fatal("restarted bootstrap Ready has no completion action")
	}
	record, err = runtime.Select(ctx, complete.ID)
	if err != nil {
		t.Fatal(err)
	}
	drainNodeEffects(t, ctx, runtime, "n1", 8)
	after := latestEvidenceForNode(t, runtime, "n1")
	if after.StorageLastIndex < 1 || len(after.ConfState.Voters) != 1 || after.ConfState.Voters[0] != 1 {
		t.Fatalf("restarted bootstrap was not durably reconstructed: %+v", after)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(ctx, etcdraftv2.New(), runtimeConfig(), trace); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestRestartReconstructsPersistedStateWithNewIncarnation(t *testing.T) {
	ctx := context.Background()
	runtime, err := controlruntime.New(ctx, etcdraftv2.New(), runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	effect := firstEnabledItemOfKindForNode(t, runtime.Snapshot(), control.ItemEffect, "n1")
	complete, ok := actionForItem(mustEnabled(t, ctx, runtime), control.ActionCompleteEffect, effect)
	if !ok {
		t.Fatal("bootstrap Ready has no completion action")
	}
	_, err = runtime.Select(ctx, complete.ID)
	if err != nil {
		t.Fatal(err)
	}
	drainNodeEffects(t, ctx, runtime, "n1", 8)
	before := latestEvidenceForNode(t, runtime, "n1")
	oldPulse := firstEnabledItemOfKindForNode(t, runtime.Snapshot(), control.ItemTemporal, "n1")
	crash := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionCrash, "n1")
	record, err := runtime.Select(ctx, crash.ID)
	if err != nil {
		t.Fatal(err)
	}
	stopped := evidenceForNode(t, decodeEvidence(t, *record.Evidence), "n1")
	if stopped.Running || stopped.Role != "StateStopped" || stopped.DurableDigest != before.DurableDigest ||
		stopped.StorageLastIndex != before.StorageLastIndex {
		t.Fatalf("stopped evidence lost durable state: before=%+v stopped=%+v", before, stopped)
	}
	assertSnapshotItemState(t, runtime.Snapshot(), oldPulse, control.ItemCanceled)
	restart := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionRestart, "n1")
	record, err = runtime.Select(ctx, restart.ID)
	if err != nil {
		t.Fatal(err)
	}
	after := evidenceForNode(t, decodeEvidence(t, *record.Evidence), "n1")
	if !after.Running || after.Incarnation != 2 || after.DurableDigest != before.DurableDigest ||
		after.StorageLastIndex != before.StorageLastIndex || after.StorageCommit != before.StorageCommit ||
		len(after.ConfState.Voters) != 1 {
		t.Fatalf("restart did not reconstruct durable state: before=%+v after=%+v", before, after)
	}
	for _, item := range runtime.Snapshot().Items {
		if item.Owner.Node == "n1" && !terminalItemState(item.State) && item.Owner.Incarnation != 2 {
			t.Fatalf("live item retained old incarnation after restart: %+v", item)
		}
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(ctx, etcdraftv2.New(), runtimeConfig(), trace); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestSingleNodeOpaqueProposalCommitsAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	runtime, err := controlruntime.New(ctx, etcdraftv2.New(), runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	leader := driveUntilStableLeader(t, ctx, runtime, 128)
	if leader != "n1" {
		t.Fatalf("single-node leader = %s, want n1", leader)
	}
	payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: "single-request-1", Value: []byte("alpha"),
	})
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := runtime.OfferInvoke(ctx, leader, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, invoke); err != nil {
		t.Fatal(err)
	}
	driveUntilApplicationCommands(t, ctx, runtime, map[control.NodeID]int{"n1": 1}, 96)
	result := findClientResponse(t, runtime.Snapshot(), "single-request-1")
	if result.State != control.ItemCompleted || result.Value.Response.Status != "committed" {
		t.Fatalf("client result = %+v", result)
	}
	before := latestEvidenceForNode(t, runtime, "n1")
	if before.ApplicationCommands != 1 || before.ApplicationDigest == "" {
		t.Fatalf("application evidence before restart = %+v", before)
	}
	crash := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionCrash, "n1")
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		t.Fatal(err)
	}
	restart := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionRestart, "n1")
	if _, err := runtime.Select(ctx, restart.ID); err != nil {
		t.Fatal(err)
	}
	after := latestEvidenceForNode(t, runtime, "n1")
	if after.ApplicationCommands != 1 || after.ApplicationDigest != before.ApplicationDigest ||
		after.Incarnation != 2 {
		t.Fatalf("application evidence after restart = %+v, before = %+v", after, before)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if !traceHasKind(trace, control.ActionInvoke) || !traceHasKind(trace, control.ActionCrash) ||
		!traceHasKind(trace, control.ActionRestart) {
		t.Fatal("proposal lifecycle trace is incomplete")
	}
	if _, err := controlruntime.Replay(ctx, etcdraftv2.New(), runtimeConfig(), trace); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestProposalBeforeLeaderReturnsRejectedResult(t *testing.T) {
	ctx := context.Background()
	runtime, err := controlruntime.New(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	drainClusterEffects(t, ctx, runtime, 32)
	payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: "early-request-1", Value: []byte("early"),
	})
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
	result := findClientResponse(t, runtime.Snapshot(), "early-request-1")
	if result.State != control.ItemCompleted || result.Value.Response.Status != "rejected" {
		t.Fatalf("early client result = %+v", result)
	}
	for _, node := range latestClusterEvidence(t, runtime).Nodes {
		if node.ApplicationCommands != 0 {
			t.Fatalf("rejected proposal changed application on %s: %+v", node.Node, node)
		}
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(), trace,
	); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestThreeNodeProposalsSurviveLeaderChangeAndRecovery(t *testing.T) {
	ctx := context.Background()
	runtime, err := controlruntime.New(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	firstLeader := driveUntilStableLeader(t, ctx, runtime, 512)
	firstPayload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: "cluster-request-1", Value: []byte("alpha"),
	})
	if err != nil {
		t.Fatal(err)
	}
	firstInvoke, err := runtime.OfferInvoke(ctx, firstLeader, firstPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, firstInvoke); err != nil {
		t.Fatal(err)
	}
	driveUntilApplicationCommands(t, ctx, runtime, map[control.NodeID]int{
		"n1": 1, "n2": 1, "n3": 1,
	}, 768)
	if result := findClientResponse(t, runtime.Snapshot(), "cluster-request-1"); result.State != control.ItemCompleted || result.Value.Response.Status != "committed" {
		t.Fatalf("first client result = %+v", result)
	}

	crash := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionCrash, firstLeader)
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		t.Fatal(err)
	}
	stopped := latestEvidenceForNode(t, runtime, firstLeader)
	if stopped.Running || stopped.ApplicationCommands != 1 || stopped.ApplicationDigest == "" {
		t.Fatalf("stopped leader lost its first committed command: %+v", stopped)
	}
	secondLeader := driveUntilStableLeader(t, ctx, runtime, 1024)
	if secondLeader == firstLeader {
		t.Fatalf("stopped node %s remained leader", firstLeader)
	}
	secondPayload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: "cluster-request-2", Value: []byte("beta"),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondInvoke, err := runtime.OfferInvoke(ctx, secondLeader, secondPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, secondInvoke); err != nil {
		t.Fatal(err)
	}
	survivors := map[control.NodeID]int{"n1": 2, "n2": 2, "n3": 2}
	delete(survivors, firstLeader)
	driveUntilApplicationCommands(t, ctx, runtime, survivors, 1024)
	if result := findClientResponse(t, runtime.Snapshot(), "cluster-request-2"); result.State != control.ItemCompleted || result.Value.Response.Status != "committed" {
		t.Fatalf("second client result = %+v", result)
	}

	restart := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionRestart, firstLeader)
	if _, err := runtime.Select(ctx, restart.ID); err != nil {
		t.Fatal(err)
	}
	driveUntilApplicationCommands(t, ctx, runtime, map[control.NodeID]int{
		"n1": 2, "n2": 2, "n3": 2,
	}, 1536)
	final := latestClusterEvidence(t, runtime)
	var applicationDigest string
	for _, node := range final.Nodes {
		if !node.Running || node.ApplicationCommands != 2 || node.ApplicationDigest == "" {
			t.Fatalf("node did not converge after recovery: %+v", node)
		}
		if applicationDigest == "" {
			applicationDigest = node.ApplicationDigest
		} else if node.ApplicationDigest != applicationDigest {
			t.Fatalf("application digests diverged: %s != %s", node.ApplicationDigest, applicationDigest)
		}
	}
	if recovered := evidenceForNode(t, final, firstLeader); recovered.Incarnation != 2 {
		t.Fatalf("recovered leader incarnation = %d, want 2", recovered.Incarnation)
	}

	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if countTraceKind(trace, control.ActionInvoke) != 2 || !traceHasKind(trace, control.ActionCrash) ||
		!traceHasKind(trace, control.ActionRestart) {
		t.Fatal("leader-change proposal trace is incomplete")
	}
	history, err := (etcdraftv2.ObservationProjector{}).Project(trace)
	if err != nil {
		t.Fatal(err)
	}
	observed := make(map[semantic.ObservationKind]int)
	requests := make(map[string]bool)
	inflightChanges := 0
	raftTermObserved := false
	for _, event := range history.Events {
		observed[event.Kind]++
		requests[event.RequestID] = event.RequestID != ""
		if event.Kind == semantic.ObservationCoordinatorChange && event.OperationStage == "inflight" {
			inflightChanges++
		}
		if event.Kind == etcdraftv2.ObservationRaftTermAdvanced && len(event.Attributes) == 1 &&
			event.Attributes[0].Field == etcdraftv2.ObservationFieldRaftTerm &&
			event.Attributes[0].Type == semantic.ObservationValueUint {
			raftTermObserved = true
		}
	}
	if observed[semantic.ObservationCoordinatorChange] == 0 ||
		observed[semantic.ObservationNodeRestarted] == 0 ||
		observed[semantic.ObservationDecisionAdvanced] == 0 ||
		!requests["cluster-request-1"] || !requests["cluster-request-2"] || inflightChanges != 0 ||
		!raftTermObserved {
		t.Fatalf("common observation projection incomplete: observed=%v requests=%v", observed, requests)
	}
	if _, err := controlruntime.Replay(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(), trace,
	); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestThreeNodeMessageControlAndReplay(t *testing.T) {
	ctx := context.Background()
	adapter := mustAdapter(t, etcdraftv2.ThreeNodeConfig())
	runtime, err := controlruntime.New(ctx, adapter, clusterRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}

	original := driveUntilDeliverableMessage(t, ctx, runtime, 160)
	duplicate, ok := actionForItem(mustEnabled(t, ctx, runtime), control.ActionDuplicateMessage, original.ID)
	if !ok {
		t.Fatal("deliverable Raft message has no duplicate action")
	}
	if _, err := runtime.Select(ctx, duplicate.ID); err != nil {
		t.Fatal(err)
	}
	clone := findClone(t, runtime.Snapshot(), original.Value.Message.ID)

	partitionID, err := runtime.OfferPartition(
		[]control.NodeID{original.Value.Message.Source.Node},
		[]control.NodeID{original.Value.Message.Target},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, partitionID); err != nil {
		t.Fatal(err)
	}
	actions := mustEnabled(t, ctx, runtime)
	if _, ok := actionForItem(actions, control.ActionDeliverMessage, original.ID); ok {
		t.Fatal("partition left original message deliverable")
	}
	if _, ok := actionForItem(actions, control.ActionDeliverMessage, clone.ID); ok {
		t.Fatal("partition left cloned message deliverable")
	}
	if _, ok := actionForItem(actions, control.ActionDropMessage, original.ID); !ok {
		t.Fatal("partition removed explicit drop control")
	}

	heal, ok := firstActionOfKind(actions, control.ActionHeal)
	if !ok {
		t.Fatal("partition has no heal action")
	}
	if _, err := runtime.Select(ctx, heal.ID); err != nil {
		t.Fatal(err)
	}
	cloneDeliver, ok := actionForItem(mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, clone.ID)
	if !ok {
		t.Fatal("clone not deliverable after heal")
	}
	if _, err := runtime.Select(ctx, cloneDeliver.ID); err != nil {
		t.Fatal(err)
	}
	dropOriginal, ok := actionForItem(mustEnabled(t, ctx, runtime), control.ActionDropMessage, original.ID)
	if !ok {
		t.Fatal("original message has no drop action after clone delivery")
	}
	if _, err := runtime.Select(ctx, dropOriginal.ID); err != nil {
		t.Fatal(err)
	}
	leaderSeen := false
	for decision := 0; decision < 320; decision++ {
		actions = mustEnabled(t, ctx, runtime)
		action, ok := clusterProgressAction(actions)
		if !ok {
			t.Fatalf("cluster made no progress at decision %d: %+v", decision, actions)
		}
		record, err := runtime.Select(ctx, action.ID)
		if err != nil {
			t.Fatal(err)
		}
		if record.Evidence != nil && hasLeader(decodeEvidence(t, *record.Evidence)) {
			leaderSeen = true
		}
		if leaderSeen {
			next := mustEnabled(t, ctx, runtime)
			if !hasKind(next, control.ActionCompleteEffect) && !hasKind(next, control.ActionDeliverMessage) {
				break
			}
		}
	}
	if !leaderSeen {
		t.Fatal("three-node controlled schedule did not elect a leader")
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if !traceHasKind(trace, control.ActionDuplicateMessage) || !traceHasKind(trace, control.ActionPartition) ||
		!traceHasKind(trace, control.ActionHeal) || !traceHasKind(trace, control.ActionDropMessage) ||
		!traceHasKind(trace, control.ActionDeliverMessage) {
		t.Fatal("trace is missing a required message-control action")
	}
	history, err := (etcdraftv2.ObservationProjector{}).Project(trace)
	if err != nil {
		t.Fatal(err)
	}
	dropRoleBound := false
	for _, event := range history.Events {
		if event.Kind == semantic.ObservationMessageDropped &&
			event.MessageRole == original.Value.Message.TypeHint {
			dropRoleBound = true
			break
		}
	}
	if !dropRoleBound {
		t.Fatalf("dropped message observation lost leaf type %q", original.Value.Message.TypeHint)
	}
	if _, err := controlruntime.Replay(ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(), trace); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	assertEntropyNodes(t, trace, map[control.NodeID]bool{"n1": true, "n2": true, "n3": true})
}

func TestCrashCancelsUnreleasedRaftMessageAndReadyEffect(t *testing.T) {
	ctx := context.Background()
	runtime, err := controlruntime.New(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	message := driveUntilBlockedMessage(t, ctx, runtime, 160)
	if message.Value.Message == nil || len(message.Value.Dependencies) != 1 {
		t.Fatalf("captured message has no Ready dependency: %+v", message)
	}
	effect := message.Value.Dependencies[0]
	source := message.Value.Message.Source.Node
	crash := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionCrash, source)
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		t.Fatal(err)
	}
	assertSnapshotItemState(t, runtime.Snapshot(), message.ID, control.ItemCanceled)
	assertSnapshotItemState(t, runtime.Snapshot(), effect, control.ItemCanceled)
	restart := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionRestart, source)
	if restart.Node.Incarnation != 2 {
		t.Fatalf("restart incarnation = %d, want 2", restart.Node.Incarnation)
	}
	if _, err := runtime.Select(ctx, restart.ID); err != nil {
		t.Fatal(err)
	}
	assertSnapshotItemState(t, runtime.Snapshot(), message.ID, control.ItemCanceled)
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(), trace,
	); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestReleasedRaftMessageSurvivesSourceAndTargetRestart(t *testing.T) {
	ctx := context.Background()
	runtime, err := controlruntime.New(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	message := driveUntilDeliverableMessage(t, ctx, runtime, 160)
	source := message.Value.Message.Source.Node
	target := message.Value.Message.Target
	advance := firstEnabledItemOfKindForNode(t, runtime.Snapshot(), control.ItemEffect, source)
	crashSource := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionCrash, source)
	if _, err := runtime.Select(ctx, crashSource.ID); err != nil {
		t.Fatal(err)
	}
	assertSnapshotItemState(t, runtime.Snapshot(), message.ID, control.ItemEnabled)
	assertSnapshotItemState(t, runtime.Snapshot(), advance, control.ItemCanceled)
	if _, ok := actionForItem(mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, message.ID); !ok {
		t.Fatal("source crash removed a Runtime-owned released message")
	}
	restartSource := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionRestart, source)
	if _, err := runtime.Select(ctx, restartSource.ID); err != nil {
		t.Fatal(err)
	}
	drainNodeEffects(t, ctx, runtime, source, 16)
	restartedSource := latestEvidenceForNode(t, runtime, source)
	if !restartedSource.Running || restartedSource.Incarnation != 2 ||
		len(restartedSource.ConfState.Voters) != 3 {
		t.Fatalf("source did not recover persisted-before-Advance Ready: %+v", restartedSource)
	}
	assertSnapshotItemState(t, runtime.Snapshot(), message.ID, control.ItemEnabled)
	crashTarget := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionCrash, target)
	if _, err := runtime.Select(ctx, crashTarget.ID); err != nil {
		t.Fatal(err)
	}
	assertSnapshotItemState(t, runtime.Snapshot(), message.ID, control.ItemEnabled)
	if _, ok := actionForItem(mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, message.ID); ok {
		t.Fatal("message remained deliverable to a stopped target")
	}
	if _, ok := actionForItem(mustEnabled(t, ctx, runtime), control.ActionDropMessage, message.ID); !ok {
		t.Fatal("stopped target removed explicit message drop control")
	}
	restartTarget := actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionRestart, target)
	if _, err := runtime.Select(ctx, restartTarget.ID); err != nil {
		t.Fatal(err)
	}
	drainNodeEffects(t, ctx, runtime, target, 16)
	deliver, ok := actionForItem(mustEnabled(t, ctx, runtime), control.ActionDeliverMessage, message.ID)
	if !ok {
		t.Fatal("released message was not deliverable after target restart")
	}
	if deliver.Node.Incarnation != 2 {
		t.Fatalf("delivery target incarnation = %d, want 2", deliver.Node.Incarnation)
	}
	if _, err := runtime.Select(ctx, deliver.ID); err != nil {
		t.Fatal(err)
	}
	assertSnapshotItemState(t, runtime.Snapshot(), message.ID, control.ItemCompleted)
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if !traceHasKind(trace, control.ActionCrash) || !traceHasKind(trace, control.ActionRestart) ||
		!traceHasKind(trace, control.ActionDeliverMessage) {
		t.Fatal("lifecycle trace is missing crash/restart/deliver")
	}
	if _, err := controlruntime.Replay(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(), trace,
	); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func runtimeConfig() controlruntime.Config {
	return controlruntime.Config{Seed: []byte("official-etcdraft-v2-seed"), ClockError: 0, MaxClones: 1}
}

func clusterRuntimeConfig() controlruntime.Config {
	return controlruntime.Config{
		Seed: []byte("official-etcdraft-v2-cluster-seed"), ClockError: 0, MaxClones: 1,
	}
}

func mustAdapter(t *testing.T, config etcdraftv2.Config) *etcdraftv2.Adapter {
	t.Helper()
	adapter, err := etcdraftv2.NewWithConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func driveUntilDeliverableMessage(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	bound int,
) controlruntime.ItemSnapshot {
	t.Helper()
	for decision := 0; decision < bound; decision++ {
		actions := mustEnabled(t, ctx, runtime)
		if deliver, ok := firstActionOfKind(actions, control.ActionDeliverMessage); ok {
			item, ok := itemByID(runtime.Snapshot(), deliver.Item)
			if !ok || item.Value.Message == nil {
				t.Fatalf("deliver action references no message item: %+v", deliver)
			}
			return item
		}
		action, ok := clusterProgressAction(actions)
		if !ok {
			t.Fatalf("no progress action before first message at decision %d: %+v", decision, actions)
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("no deliverable message within %d decisions", bound)
	return controlruntime.ItemSnapshot{}
}

func driveUntilBlockedMessage(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	bound int,
) controlruntime.ItemSnapshot {
	t.Helper()
	for decision := 0; decision < bound; decision++ {
		for _, item := range runtime.Snapshot().Items {
			if item.Kind == control.ItemMessage && item.State == control.ItemBlocked {
				return item
			}
		}
		actions := mustEnabled(t, ctx, runtime)
		action, ok := clusterProgressAction(actions)
		if !ok {
			t.Fatalf("no progress action before blocked message at decision %d: %+v", decision, actions)
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("no blocked message within %d decisions", bound)
	return controlruntime.ItemSnapshot{}
}

func drainNodeEffects(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	node control.NodeID,
	bound int,
) {
	t.Helper()
	for decision := 0; decision < bound; decision++ {
		var selected control.Action
		found := false
		for _, action := range mustEnabled(t, ctx, runtime) {
			if action.Kind == control.ActionCompleteEffect && action.Node.Node == node {
				selected = action
				found = true
				break
			}
		}
		if !found {
			return
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("node %s still has Ready effects after %d decisions", node, bound)
}

func drainClusterEffects(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	bound int,
) {
	t.Helper()
	for decision := 0; decision < bound; decision++ {
		action, ok := firstActionOfKind(mustEnabled(t, ctx, runtime), control.ActionCompleteEffect)
		if !ok {
			return
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("cluster still has Ready effects after %d decisions", bound)
}

func latestEvidenceForNode(
	t *testing.T,
	runtime *controlruntime.Runtime,
	node control.NodeID,
) evidenceState {
	t.Helper()
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	for index := len(trace.Records) - 1; index >= 0; index-- {
		if trace.Records[index].Evidence != nil {
			return evidenceForNode(t, decodeEvidence(t, *trace.Records[index].Evidence), node)
		}
	}
	return evidenceForNode(t, decodeEvidence(t, trace.InitialEvidence), node)
}

func latestClusterEvidence(t *testing.T, runtime *controlruntime.Runtime) clusterEvidence {
	t.Helper()
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	for index := len(trace.Records) - 1; index >= 0; index-- {
		if trace.Records[index].Evidence != nil {
			return decodeEvidence(t, *trace.Records[index].Evidence)
		}
	}
	return decodeEvidence(t, trace.InitialEvidence)
}

func driveUntilStableLeader(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	bound int,
) control.NodeID {
	t.Helper()
	for decision := 0; decision < bound; decision++ {
		actions := mustEnabled(t, ctx, runtime)
		for _, state := range latestClusterEvidence(t, runtime).Nodes {
			if !state.Running || state.Role != "StateLeader" {
				continue
			}
			leaderReady := false
			for _, action := range actions {
				if action.Kind == control.ActionCompleteEffect && action.Node.Node == state.Node {
					leaderReady = true
					break
				}
			}
			if !leaderReady {
				return state.Node
			}
		}
		action, ok := clusterProgressAction(actions)
		if !ok {
			t.Fatalf("no progress action before stable leader at decision %d: %+v", decision, actions)
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("no stable leader within %d decisions", bound)
	return ""
}

func driveUntilApplicationCommands(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	wanted map[control.NodeID]int,
	bound int,
) {
	t.Helper()
	for decision := 0; decision < bound; decision++ {
		evidence := latestClusterEvidence(t, runtime)
		satisfied := true
		for node, count := range wanted {
			if evidenceForNode(t, evidence, node).ApplicationCommands < count {
				satisfied = false
				break
			}
		}
		if satisfied {
			return
		}
		actions := mustEnabled(t, ctx, runtime)
		action, ok := clusterProgressAction(actions)
		if !ok {
			t.Fatalf("no progress action before application target at decision %d: %+v", decision, actions)
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("application target %+v not reached within %d decisions", wanted, bound)
}

func findClientResponse(
	t *testing.T,
	snapshot controlruntime.Snapshot,
	requestID string,
) controlruntime.ItemSnapshot {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Kind == control.ItemClientResult && item.Value.Response != nil &&
			item.Value.Response.RequestID == requestID {
			return item
		}
	}
	t.Fatalf("no client result for request %s", requestID)
	return controlruntime.ItemSnapshot{}
}

func clusterProgressAction(actions []control.Action) (control.Action, bool) {
	for _, kind := range []control.ActionKind{
		control.ActionCompleteEffect,
		control.ActionDeliverMessage,
		control.ActionFireTemporal,
	} {
		if action, ok := firstActionOfKind(actions, kind); ok {
			return action, true
		}
	}
	return control.Action{}, false
}

func firstActionOfKind(actions []control.Action, kind control.ActionKind) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind {
			return action, true
		}
	}
	return control.Action{}, false
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

func actionForItem(actions []control.Action, kind control.ActionKind, item control.ItemID) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind && action.Item == item {
			return action, true
		}
	}
	return control.Action{}, false
}

func actionForNode(
	t *testing.T,
	actions []control.Action,
	kind control.ActionKind,
	node control.NodeID,
) control.Action {
	t.Helper()
	for _, action := range actions {
		if action.Kind == kind && action.Node.Node == node {
			return action
		}
	}
	t.Fatalf("no %s action for %s: %+v", kind, node, actions)
	return control.Action{}
}

func firstItemOfKindForNode(
	t *testing.T,
	snapshot controlruntime.Snapshot,
	kind control.ItemKind,
	node control.NodeID,
) control.ItemID {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Kind == kind && item.Owner.Node == node {
			return item.ID
		}
	}
	t.Fatalf("no %s item for %s", kind, node)
	return ""
}

func firstEnabledItemOfKindForNode(
	t *testing.T,
	snapshot controlruntime.Snapshot,
	kind control.ItemKind,
	node control.NodeID,
) control.ItemID {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Kind == kind && item.Owner.Node == node && item.State == control.ItemEnabled {
			return item.ID
		}
	}
	t.Fatalf("no enabled %s item for %s", kind, node)
	return ""
}

func assertSnapshotItemState(
	t *testing.T,
	snapshot controlruntime.Snapshot,
	id control.ItemID,
	wanted control.ItemState,
) {
	t.Helper()
	item, ok := itemByID(snapshot, id)
	if !ok || item.State != wanted {
		t.Fatalf("item %s state = %+v, want %s", id, item, wanted)
	}
}

func terminalItemState(state control.ItemState) bool {
	switch state {
	case control.ItemCompleted, control.ItemCanceled, control.ItemFailed, control.ItemDropped:
		return true
	default:
		return false
	}
}

func itemByID(snapshot controlruntime.Snapshot, id control.ItemID) (controlruntime.ItemSnapshot, bool) {
	for _, item := range snapshot.Items {
		if item.ID == id {
			return item, true
		}
	}
	return controlruntime.ItemSnapshot{}, false
}

func findClone(t *testing.T, snapshot controlruntime.Snapshot, original control.MessageID) controlruntime.ItemSnapshot {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Value.Message != nil && item.Value.Message.CloneOf == original {
			return item
		}
	}
	t.Fatalf("no clone of message %s", original)
	return controlruntime.ItemSnapshot{}
}

func hasActionKind(kinds []control.ActionKind, wanted control.ActionKind) bool {
	for _, kind := range kinds {
		if kind == wanted {
			return true
		}
	}
	return false
}

func hasItemKind(kinds []control.ItemKind, wanted control.ItemKind) bool {
	for _, kind := range kinds {
		if kind == wanted {
			return true
		}
	}
	return false
}

func preferredAction(actions []control.Action) (control.Action, bool) {
	for _, kind := range []control.ActionKind{control.ActionCompleteEffect, control.ActionFireTemporal} {
		for _, action := range actions {
			if action.Kind == kind {
				return action, true
			}
		}
	}
	return control.Action{}, false
}

func mustEnabled(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime) []control.Action {
	t.Helper()
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return actions
}

func hasKind(actions []control.Action, kind control.ActionKind) bool {
	for _, action := range actions {
		if action.Kind == kind {
			return true
		}
	}
	return false
}

func countItems(snapshot controlruntime.Snapshot, kind control.ItemKind, state control.ItemState) int {
	count := 0
	for _, item := range snapshot.Items {
		if item.Kind == kind && item.State == state {
			count++
		}
	}
	return count
}

func decodeEvidence(t *testing.T, evidence control.EvidenceEnvelope) clusterEvidence {
	t.Helper()
	var state clusterEvidence
	if err := json.Unmarshal(evidence.Payload.Bytes, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func evidenceForNode(t *testing.T, evidence clusterEvidence, node control.NodeID) evidenceState {
	t.Helper()
	for _, state := range evidence.Nodes {
		if state.Node == node {
			return state
		}
	}
	t.Fatalf("evidence has no node %s: %+v", node, evidence.Nodes)
	return evidenceState{}
}

func assertEntropyDomain(t *testing.T, trace controlruntime.Trace) {
	t.Helper()
	audit := trace.InitialEntropy
	if len(trace.Records) > 0 && trace.Records[len(trace.Records)-1].Entropy != nil {
		audit = *trace.Records[len(trace.Records)-1].Entropy
	}
	var tape controlentropy.Tape
	if err := json.Unmarshal(audit.Tape.Bytes, &tape); err != nil {
		t.Fatal(err)
	}
	if err := tape.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(tape.Draws) == 0 {
		t.Fatal("official RawNode produced no audited native entropy draw")
	}
	for _, draw := range tape.Draws {
		if draw.Domain.Namespace != "etcdraft-v3.6.0" || draw.Domain.Node != "n1" ||
			draw.Domain.Incarnation != 1 || draw.Domain.ID != "native-election-timeout" {
			t.Fatalf("unexpected entropy domain: %+v", draw.Domain)
		}
	}
}

func assertEntropyNodes(t *testing.T, trace controlruntime.Trace, expected map[control.NodeID]bool) {
	t.Helper()
	audit := trace.InitialEntropy
	if len(trace.Records) > 0 && trace.Records[len(trace.Records)-1].Entropy != nil {
		audit = *trace.Records[len(trace.Records)-1].Entropy
	}
	var tape controlentropy.Tape
	if err := json.Unmarshal(audit.Tape.Bytes, &tape); err != nil {
		t.Fatal(err)
	}
	if err := tape.Validate(); err != nil {
		t.Fatal(err)
	}
	seen := make(map[control.NodeID]bool, len(expected))
	for _, draw := range tape.Draws {
		if draw.Domain.Namespace != "etcdraft-v3.6.0" || draw.Domain.Incarnation != 1 ||
			draw.Domain.ID != "native-election-timeout" || !expected[draw.Domain.Node] {
			t.Fatalf("unexpected entropy domain: %+v", draw.Domain)
		}
		seen[draw.Domain.Node] = true
	}
	for node := range expected {
		if !seen[node] {
			t.Fatalf("entropy tape has no native draw for %s", node)
		}
	}
}

func hasLeader(evidence clusterEvidence) bool {
	for _, node := range evidence.Nodes {
		if node.Role == "StateLeader" {
			return true
		}
	}
	return false
}

func traceHasKind(trace controlruntime.Trace, kind control.ActionKind) bool {
	for _, record := range trace.Records {
		if record.Action.Kind == kind {
			return true
		}
	}
	return false
}

func countTraceKind(trace controlruntime.Trace, kind control.ActionKind) int {
	count := 0
	for _, record := range trace.Records {
		if record.Action.Kind == kind {
			count++
		}
	}
	return count
}

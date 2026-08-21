package etcdraftv2_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

func TestThreeNodeOnlineCorePSSSamplingIsReplayStable(t *testing.T) {
	ctx := context.Background()
	runtime, sampler := newOnlineCorePSSSampler(t, ctx)
	initial := coreSampleState(t, sampler.Samples()[0].State)
	if got := countCoreEntities(initial, psscore.EntityParticipant); got != 3 {
		t.Fatalf("initial participant entities = %d, want 3", got)
	}
	if got := countCoreModes(initial, psscore.ModePassive); got != 3 {
		t.Fatalf("initial passive participants = %d, want 3", got)
	}

	decisions := make([]control.ActionID, 0)
	leader := driveSampledUntilStableLeader(t, ctx, runtime, sampler, &decisions, 256)
	elected := coreSampleState(t, sampler.Samples()[len(sampler.Samples())-1].State)
	if got := countCoreModes(elected, psscore.ModeCoordinating); got != 1 {
		t.Fatalf("coordinating participants = %d, want 1", got)
	}
	if elected.Digest == initial.Digest {
		t.Fatal("leader election did not change Core PSS")
	}
	if err := elected.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	var follower control.NodeID
	for _, node := range []control.NodeID{"n1", "n2", "n3"} {
		if node != leader {
			follower = node
			break
		}
	}
	selectAndSample(t, ctx, runtime, sampler,
		actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionCrash, follower).ID, &decisions)
	selectAndSample(t, ctx, runtime, sampler,
		actionForNode(t, mustEnabled(t, ctx, runtime), control.ActionRestart, follower).ID, &decisions)

	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(), trace,
	); err != nil {
		t.Fatalf("strict Runtime replay failed: %v", err)
	}

	replayRuntime, replaySampler := newOnlineCorePSSSampler(t, ctx)
	for _, id := range decisions {
		selectAndSample(t, ctx, replayRuntime, replaySampler, id, nil)
	}
	if !reflect.DeepEqual(sampler.Samples(), replaySampler.Samples()) {
		t.Fatal("fresh ActionID replay changed the Core PSS sample sequence")
	}
	discovery, err := sampler.Discovery()
	if err != nil {
		t.Fatal(err)
	}
	replayDiscovery, err := replaySampler.Discovery()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(discovery, replayDiscovery) {
		t.Fatal("fresh ActionID replay changed the discovery ledger")
	}
	if discovery.Samples != len(decisions)+1 || discovery.UniqueStates < 3 {
		t.Fatalf("unexpected online discovery: %#v", discovery)
	}
	t.Logf("decisions=%d samples=%d unique_states=%d", len(decisions), discovery.Samples, discovery.UniqueStates)
	if !samplesContainMode(t, sampler.Samples(), psscore.ModeInactive) {
		t.Fatal("crash boundary produced no inactive Core PSS participant")
	}
}

func newOnlineCorePSSSampler(
	t *testing.T,
	ctx context.Context,
) (*controlruntime.Runtime, *psscore.OnlineSampler) {
	t.Helper()
	runtime, err := controlruntime.New(
		ctx, mustAdapter(t, etcdraftv2.ThreeNodeConfig()), clusterRuntimeConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	sampler, err := psscore.NewOnlineSampler(etcdraftv2.CorePSSMapper{}, runtime.Snapshot(), trace.InitialEvidence)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, sampler
}

func driveSampledUntilStableLeader(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	sampler *psscore.OnlineSampler,
	decisions *[]control.ActionID,
	bound int,
) control.NodeID {
	t.Helper()
	for decision := 0; decision < bound; decision++ {
		actions := mustEnabled(t, ctx, runtime)
		for _, state := range latestClusterEvidence(t, runtime).Nodes {
			if !state.Running || state.Role != "StateLeader" {
				continue
			}
			if _, busy := actionForNodeIfPresent(actions, control.ActionCompleteEffect, state.Node); !busy {
				return state.Node
			}
		}
		action, ok := clusterProgressAction(actions)
		if !ok {
			t.Fatalf("no progress action before stable leader at decision %d: %+v", decision, actions)
		}
		selectAndSample(t, ctx, runtime, sampler, action.ID, decisions)
	}
	t.Fatalf("no stable leader within %d decisions", bound)
	return ""
}

func selectAndSample(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	sampler *psscore.OnlineSampler,
	id control.ActionID,
	decisions *[]control.ActionID,
) {
	t.Helper()
	record, err := runtime.Select(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := sampler.Capture(record, runtime.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if decisions != nil {
		*decisions = append(*decisions, id)
	}
}

func actionForNodeIfPresent(
	actions []control.Action,
	kind control.ActionKind,
	node control.NodeID,
) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind && action.Node.Node == node {
			return action, true
		}
	}
	return control.Action{}, false
}

func coreSampleState(t *testing.T, sample any) psscore.State {
	t.Helper()
	state, ok := sample.(psscore.State)
	if !ok {
		t.Fatalf("sample state type = %T, want psscore.State", sample)
	}
	return state
}

func samplesContainMode(t *testing.T, samples []protocolstate.Sample, mode psscore.ParticipantMode) bool {
	t.Helper()
	for _, sample := range samples {
		if countCoreModes(coreSampleState(t, sample.State), mode) > 0 {
			return true
		}
	}
	return false
}

func countCoreEntities(state psscore.State, kind psscore.EntityKind) int {
	count := 0
	for _, entity := range state.Semantic.Entities {
		if entity.Kind == kind {
			count++
		}
	}
	return count
}

func countCoreModes(state psscore.State, mode psscore.ParticipantMode) int {
	count := 0
	for _, entity := range state.Semantic.Entities {
		if entity.Kind == psscore.EntityParticipant && entity.Mode == mode {
			count++
		}
	}
	return count
}

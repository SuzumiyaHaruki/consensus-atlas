package omnipaxosv2

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

func TestCorePSSOnlineSamplingIsFreshReplayStable(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	runtime, sampler, adapter := newSampledRuntime(t, ctx, workerPath)
	firstPID := adapter.worker.cmd.Process.Pid
	initial := coreState(t, sampler.Samples()[0])
	if countMode(initial, psscore.ModePassive) != 3 || countEntity(initial, psscore.EntityParticipant) != 3 {
		t.Fatalf("unexpected initial Core PSS: %+v", initial.Semantic)
	}

	electionIDs := driveSampledToLeader(t, ctx, runtime, sampler, adapter)
	elected := coreState(t, sampler.Samples()[len(sampler.Samples())-1])
	if countMode(elected, psscore.ModeCoordinating) != 1 || elected.Digest == initial.Digest {
		t.Fatalf("leader election not represented conservatively: %+v", elected.Semantic)
	}
	payload, err := InputPayload(Input{RequestID: "pss-request-1", Value: []byte("pss-opaque-m5.21x")})
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := runtime.OfferInvoke(ctx, "n2", payload)
	if err != nil {
		t.Fatal(err)
	}
	selectAndSample(t, ctx, runtime, sampler, invoke)
	postInvokeIDs := make([]control.ActionID, 0)
	for decisions := 0; decisions < 256 && clientResponse(runtime.Snapshot(), "pss-request-1") == nil; decisions++ {
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			t.Fatal(enabledErr)
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no sampled workload progress action: %+v", actions)
		}
		selectAndSample(t, ctx, runtime, sampler, selected.ID)
		postInvokeIDs = append(postInvokeIDs, selected.ID)
	}
	decided := coreState(t, sampler.Samples()[len(sampler.Samples())-1])
	if countEntity(decided, psscore.EntityDecision) != 1 || decided.Digest == elected.Digest {
		t.Fatalf("decision frontier not represented: %+v", decided.Semantic)
	}
	if countEntity(decided, psscore.EntityValue) != 0 || samplesContainMode(sampler.Samples(), psscore.ModeContending) {
		t.Fatal("mapping invented value or contending semantics")
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}

	replayRuntime, replaySampler, replayAdapter := newSampledRuntime(t, ctx, workerPath)
	for _, id := range electionIDs {
		selectAndSample(t, ctx, replayRuntime, replaySampler, id)
	}
	replayInvoke, err := replayRuntime.OfferInvoke(ctx, "n2", payload)
	if err != nil {
		t.Fatal(err)
	}
	if replayInvoke != invoke {
		t.Fatalf("invoke identity changed: %s != %s", replayInvoke, invoke)
	}
	selectAndSample(t, ctx, replayRuntime, replaySampler, replayInvoke)
	for _, id := range postInvokeIDs {
		selectAndSample(t, ctx, replayRuntime, replaySampler, id)
	}
	if !reflect.DeepEqual(sampler.Samples(), replaySampler.Samples()) {
		t.Fatal("fresh worker replay changed Core PSS samples")
	}
	replayTrace, err := replayRuntime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if replayTrace.Digest != trace.Digest {
		t.Fatalf("sampling trace digest changed: %s != %s", replayTrace.Digest, trace.Digest)
	}
	discovery, err := sampler.Discovery()
	if err != nil {
		t.Fatal(err)
	}
	replayDiscovery, err := replaySampler.Discovery()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(discovery, replayDiscovery) || discovery.UniqueStates < 3 {
		t.Fatalf("discovery mismatch or too few states: %+v / %+v", discovery, replayDiscovery)
	}
	semanticGraphs := uniqueSemanticGraphs(t, sampler.Samples())
	if semanticGraphs < 3 {
		t.Fatalf("semantic graph count=%d, want at least 3", semanticGraphs)
	}
	secondPID := replayAdapter.worker.cmd.Process.Pid
	if secondPID == firstPID {
		t.Fatalf("sampling replay reused worker pid %d", firstPID)
	}
	if err := replayAdapter.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("trace=%s decisions=%d samples=%d core_states=%d semantic_graphs=%d worker_pids=%d,%d",
		trace.Digest,
		len(electionIDs)+1+len(postInvokeIDs), discovery.Samples, discovery.UniqueStates,
		semanticGraphs, firstPID, secondPID)
}

func TestCorePSSMappingUsesRelativeBallotAndDecisionRanks(t *testing.T) {
	left := adapterSnapshot{LogicalTime: 7, Nodes: []workerNode{
		{ID: 1, Leader: 1, PromiseNumber: 5, PromisePID: 1, PromisePriority: 3, DecidedIndex: 10},
		{ID: 2, Leader: 1, PromiseNumber: 5, PromisePID: 1, PromisePriority: 2, DecidedIndex: 10},
		{ID: 3, Leader: 1, PromiseNumber: 4, PromisePID: 3, PromisePriority: 1},
	}}
	right := adapterSnapshot{LogicalTime: 7, Nodes: []workerNode{
		{ID: 1, Leader: 1, PromiseNumber: 105, PromisePID: 1, PromisePriority: 30, DecidedIndex: 1010},
		{ID: 2, Leader: 1, PromiseNumber: 105, PromisePID: 1, PromisePriority: 20, DecidedIndex: 1010},
		{ID: 3, Leader: 1, PromiseNumber: 104, PromisePID: 3, PromisePriority: 10},
	}}
	mapSnapshot := func(snapshot adapterSnapshot) psscore.SemanticObservation {
		payload, err := control.NewJSONPayload(evidenceSchema, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := (CorePSSMapper{}).Map(control.EvidenceEnvelope{Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		return observation
	}
	if leftObservation, rightObservation := mapSnapshot(left), mapSnapshot(right); !reflect.DeepEqual(leftObservation, rightObservation) {
		t.Fatalf("absolute ballot/decision values changed Core semantics:\n%+v\n%+v", leftObservation, rightObservation)
	}
}

func newSampledRuntime(
	t *testing.T,
	ctx context.Context,
	workerPath string,
) (*controlruntime.Runtime, *psscore.OnlineSampler, *Adapter) {
	t.Helper()
	adapter, err := New(Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{Seed: []byte("omnipaxos-m5.21x")})
	if err != nil {
		t.Fatal(err)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	sampler, err := psscore.NewOnlineSampler(CorePSSMapper{}, runtime.Snapshot(), trace.InitialEvidence)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, sampler, adapter
}

func driveSampledToLeader(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	sampler *psscore.OnlineSampler,
	adapter *Adapter,
) []control.ActionID {
	t.Helper()
	ids := make([]control.ActionID, 0)
	for decisions := 0; decisions < 256 && consensusLeader(adapter) == ""; decisions++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no sampled election progress action: %+v", actions)
		}
		selectAndSample(t, ctx, runtime, sampler, selected.ID)
		ids = append(ids, selected.ID)
	}
	if consensusLeader(adapter) != "n1" {
		t.Fatal("sampled execution did not elect n1")
	}
	return ids
}

func selectAndSample(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
	sampler *psscore.OnlineSampler,
	id control.ActionID,
) {
	t.Helper()
	record, err := runtime.Select(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := sampler.Capture(record, runtime.Snapshot()); err != nil {
		t.Fatal(err)
	}
}

func coreState(t *testing.T, sample protocolstate.Sample) psscore.State {
	t.Helper()
	state, ok := sample.State.(psscore.State)
	if !ok {
		t.Fatalf("sample state type=%T", sample.State)
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func countEntity(state psscore.State, kind psscore.EntityKind) int {
	count := 0
	for _, entity := range state.Semantic.Entities {
		if entity.Kind == kind {
			count++
		}
	}
	return count
}

func countMode(state psscore.State, mode psscore.ParticipantMode) int {
	count := 0
	for _, entity := range state.Semantic.Entities {
		if entity.Kind == psscore.EntityParticipant && entity.Mode == mode {
			count++
		}
	}
	return count
}

func samplesContainMode(samples []protocolstate.Sample, mode psscore.ParticipantMode) bool {
	for _, sample := range samples {
		state, ok := sample.State.(psscore.State)
		if ok && countMode(state, mode) > 0 {
			return true
		}
	}
	return false
}

func uniqueSemanticGraphs(t *testing.T, samples []protocolstate.Sample) int {
	t.Helper()
	unique := make(map[string]bool)
	for _, sample := range samples {
		state := coreState(t, sample)
		digest, err := control.CanonicalDigest(state.Semantic)
		if err != nil {
			t.Fatal(err)
		}
		unique[digest] = true
	}
	return len(unique)
}

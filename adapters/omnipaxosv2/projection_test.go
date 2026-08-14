package omnipaxosv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestRealDecidedPrefixPassesAgreementAndCalibrationDiverges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	adapter, err := New(Config{WorkerPath: buildWorker(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := adapter.Close(); err != nil {
			t.Error(err)
		}
	})
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{
		Seed: []byte("omnipaxos-m5.21y"),
	})
	if err != nil {
		t.Fatal(err)
	}
	driveToLeader(t, ctx, runtime, adapter)
	payload, err := InputPayload(Input{RequestID: "agreement-request-1", Value: []byte("agreement-control")})
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
	for decisions := 0; decisions < 256 && decidedNodeCountAt(adapter, 1) < 2; decisions++ {
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			t.Fatal(enabledErr)
		}
		selected, ok := progressAction(actions)
		if !ok {
			t.Fatalf("no action can complete the decided prefix: %+v", actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	if decidedNodeCountAt(adapter, 1) < 2 {
		t.Fatalf("fewer than two nodes reached decided index 1: %+v", adapter.last.Nodes)
	}

	evidence, err := adapter.SnapshotEvidence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projector := DecisionProjector{}
	observations, err := projector.Project(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) < 2 {
		t.Fatalf("decision observations=%d, want at least 2", len(observations))
	}
	controlResult := oracle.CheckBundle(bundleWithDecisions(observations), oracle.BundleAgreement{})
	if len(controlResult.Violations) != 0 {
		t.Fatalf("correct control violated Agreement: %+v", controlResult.Violations)
	}

	calibration := adapter.snapshot()
	divergent := sha256.Sum256([]byte("omnipaxos-m5.21y-controlled-divergence"))
	for index := range calibration.Nodes {
		if calibration.Nodes[index].DecidedIndex == 1 {
			calibration.Nodes[index].DecidedPrefixDigest = hex.EncodeToString(divergent[:])
			break
		}
	}
	calibrationPayload, err := control.NewJSONPayload(evidenceSchema, calibration)
	if err != nil {
		t.Fatal(err)
	}
	calibrationObservations, err := projector.Project(control.EvidenceEnvelope{Payload: calibrationPayload})
	if err != nil {
		t.Fatal(err)
	}
	calibrationResult := oracle.CheckBundle(bundleWithDecisions(calibrationObservations), oracle.BundleAgreement{})
	if len(calibrationResult.Violations) != 1 || calibrationResult.Violations[0].Monitor != "agreement" {
		t.Fatalf("controlled divergence was not detected: %+v", calibrationResult)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	replayAdapter, err := New(Config{WorkerPath: buildWorker(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := replayAdapter.Close(); err != nil {
			t.Error(err)
		}
	})
	replayRuntime, err := controlruntime.Replay(ctx, replayAdapter, controlruntime.Config{
		Seed: []byte("omnipaxos-m5.21y"),
	}, trace)
	if err != nil {
		t.Fatal(err)
	}
	replayEvidence, err := replayAdapter.SnapshotEvidence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replayObservations, err := projector.Project(replayEvidence)
	if err != nil {
		t.Fatal(err)
	}
	replayTrace, err := replayRuntime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if replayTrace.Digest != trace.Digest || !reflect.DeepEqual(replayObservations, observations) {
		t.Fatal("fresh worker replay changed trace or decision projection")
	}
	t.Logf("trace=%s decisions=%d projection=%s observations=%d replay_equal=true control_violations=0 calibration_violations=1",
		trace.Digest, len(trace.Records), projector.ID(), len(observations))
}

func TestDecisionProjectorRejectsMalformedPrefixDigest(t *testing.T) {
	snapshot := adapterSnapshot{Nodes: []workerNode{
		{ID: 1, DecidedIndex: 1, DecidedPrefixDigest: "not-a-digest"},
		{ID: 2, DecidedPrefixDigest: zeroDigest()},
		{ID: 3, DecidedPrefixDigest: zeroDigest()},
	}}
	payload, err := control.NewJSONPayload(evidenceSchema, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (DecisionProjector{}).Project(control.EvidenceEnvelope{Payload: payload}); err == nil {
		t.Fatal("malformed prefix digest was accepted")
	}
}

func TestProjectEvidenceExposesOnlySemanticWorkerState(t *testing.T) {
	snapshot := adapterSnapshot{LogicalTime: 7, Nodes: []workerNode{
		{ID: 1, Leader: 2, DecidedIndex: 3, DecidedPrefixDigest: zeroDigest(), PromiseNumber: 4, PromisePriority: 3, PromisePID: 2},
		{ID: 2, Leader: 2, DecidedIndex: 3, DecidedPrefixDigest: zeroDigest(), PromiseNumber: 4, PromisePriority: 2, PromisePID: 2},
		{ID: 3, Leader: 2, DecidedIndex: 2, DecidedPrefixDigest: zeroDigest(), PromiseNumber: 3, PromisePriority: 1, PromisePID: 3},
	}}
	payload, err := control.NewJSONPayload(evidenceSchema, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := ProjectEvidence(control.EvidenceEnvelope{Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.LogicalTime != 7 || len(evidence.Nodes) != 3 ||
		evidence.Nodes[0].Node != "n1" || evidence.Nodes[0].Leader != "n2" ||
		evidence.Nodes[0].PromiseNode != "n2" || evidence.Nodes[2].DecidedIndex != 2 {
		t.Fatalf("unexpected projected evidence: %#v", evidence)
	}
}

func decidedNodeCountAt(adapter *Adapter, index uint64) int {
	count := 0
	for _, node := range adapter.last.Nodes {
		if node.DecidedIndex >= index {
			count++
		}
	}
	return count
}

func bundleWithDecisions(observations []semantic.DecisionObservation) controlexperiment.ExecutionBundle {
	return controlexperiment.ExecutionBundle{Decisions: controlexperiment.DecisionHistory{
		Observations: observations,
	}}
}

func zeroDigest() string {
	value := sha256.Sum256(nil)
	return hex.EncodeToString(value[:])
}

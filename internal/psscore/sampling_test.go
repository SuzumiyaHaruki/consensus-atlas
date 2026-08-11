package psscore_test

import (
	"encoding/json"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

type fixtureMapper struct{}

func (fixtureMapper) ID() string { return "fixture/core-pss-v1" }

func (fixtureMapper) Map(evidence control.EvidenceEnvelope) (psscore.SemanticObservation, error) {
	var value struct {
		LogicalTime uint64 `json:"logical_time"`
	}
	if err := json.Unmarshal(evidence.Payload.Bytes, &value); err != nil {
		return psscore.SemanticObservation{}, err
	}
	return psscore.SemanticObservation{
		LogicalTime: value.LogicalTime,
		Graph: psscore.SemanticGraph{Entities: []psscore.Entity{{
			ID: "node", Kind: psscore.EntityParticipant, Mode: psscore.ModePassive,
		}}},
	}, nil
}

func TestOnlineSamplerCapturesEveryConsecutiveDecision(t *testing.T) {
	snapshot := fixtureSnapshot(0, 0)
	sampler, err := psscore.NewOnlineSampler(fixtureMapper{}, snapshot, fixtureEvidence(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Step = 1
	if err := sampler.Capture(controlruntime.ActionRecord{
		Step: 1, Outcome: "applied",
	}, snapshot); err != nil {
		t.Fatal(err)
	}
	samples := sampler.Samples()
	if len(samples) != 2 || samples[0].Step != 0 || samples[1].Step != 1 {
		t.Fatalf("unexpected samples: %#v", samples)
	}
	discovery, err := sampler.Discovery()
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Samples != 2 || discovery.UniqueStates != 1 {
		t.Fatalf("unexpected discovery: %#v", discovery)
	}
}

func TestOnlineSamplerRejectsStaleEvidenceWithoutLosingPriorObservation(t *testing.T) {
	snapshot := fixtureSnapshot(0, 0)
	sampler, err := psscore.NewOnlineSampler(fixtureMapper{}, snapshot, fixtureEvidence(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Step = 1
	stale := fixtureEvidence(t, 1)
	digest, err := control.CanonicalDigest(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := sampler.Capture(controlruntime.ActionRecord{
		Step: 1, Outcome: "applied", Evidence: &stale, EvidenceDigest: digest,
	}, snapshot); err == nil {
		t.Fatal("stale evidence was accepted")
	}
	if err := sampler.Capture(controlruntime.ActionRecord{Step: 1, Outcome: "applied"}, snapshot); err != nil {
		t.Fatalf("failed capture corrupted the prior observation: %v", err)
	}
	if len(sampler.Samples()) != 2 {
		t.Fatalf("samples after retry = %d, want 2", len(sampler.Samples()))
	}
}

func fixtureSnapshot(step, logicalTime uint64) controlruntime.Snapshot {
	return controlruntime.Snapshot{
		Step: step, LogicalTime: logicalTime,
		Nodes: []controlruntime.NodeSnapshot{{
			Ref: control.NodeRef{Node: "node", Incarnation: 1}, Lifecycle: control.NodeRunning,
		}},
	}
}

func fixtureEvidence(t *testing.T, logicalTime uint64) control.EvidenceEnvelope {
	t.Helper()
	payload, err := control.NewJSONPayload("fixture/evidence-v1", map[string]uint64{"logical_time": logicalTime})
	if err != nil {
		t.Fatal(err)
	}
	return control.EvidenceEnvelope{Yield: "fixture-yield", Payload: payload}
}

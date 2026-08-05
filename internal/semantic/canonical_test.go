package semantic_test

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestStructuralCanonicalizeRenamesNodes(t *testing.T) {
	left := []core.TraceRecord{{
		Step: 1, Event: core.Event{ID: "event-a", Kind: core.EventCampaign, Target: "server-7"}, Outcome: "applied",
	}}
	right := []core.TraceRecord{{
		Step: 1, Event: core.Event{ID: "event-z", Kind: core.EventCampaign, Target: "server-99"}, Outcome: "applied",
	}}
	leftHash, err := semantic.CanonicalFingerprint(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := semantic.CanonicalFingerprint(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash {
		t.Fatalf("hashes differ after node renaming: %s != %s", leftHash, rightHash)
	}
}

func TestExecutionFingerprintIncludesSnapshots(t *testing.T) {
	left := []core.TraceRecord{{
		Step: 1, Event: core.Event{ID: "e1", Kind: core.EventCampaign, Target: "n1"},
		Outcome: "applied", After: map[string]any{"role": "candidate"},
	}}
	right := []core.TraceRecord{{
		Step: 1, Event: core.Event{ID: "e1", Kind: core.EventCampaign, Target: "n1"},
		Outcome: "applied", After: map[string]any{"role": "leader"},
	}}
	leftHash, err := semantic.ExecutionFingerprint(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := semantic.ExecutionFingerprint(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash == rightHash {
		t.Fatal("execution fingerprints match despite different snapshots")
	}
}

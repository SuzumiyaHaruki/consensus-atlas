package raft_test

import (
	"testing"

	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/families/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

func TestReadyMustSyncRejectsEmptyReadySynchronousWrite(t *testing.T) {
	trace := []core.TraceRecord{{
		Step: 7,
		Observations: []core.Observation{{
			Kind: "ready", Label: "ready:must-sync", Value: "true",
			Evidence: map[string]string{"must_sync": "true", "entries": "0", "hard_state_empty": "true"},
		}},
	}}
	violations := (raftfamily.ReadyMustSync{}).Check(trace)
	if len(violations) != 1 || violations[0].Step != 7 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestReadyMustSyncAcceptsJustifiedOrDisabledSync(t *testing.T) {
	trace := []core.TraceRecord{{
		Step: 1,
		Observations: []core.Observation{
			{Kind: "ready", Label: "ready:must-sync", Value: "false", Evidence: map[string]string{"must_sync": "false", "entries": "0", "hard_state_empty": "true"}},
			{Kind: "ready", Label: "ready:must-sync", Value: "true", Evidence: map[string]string{"must_sync": "true", "entries": "1", "hard_state_empty": "true"}},
			{Kind: "ready", Label: "ready:must-sync", Value: "true", Evidence: map[string]string{"must_sync": "true", "entries": "0", "hard_state_empty": "false"}},
		},
	}}
	if violations := (raftfamily.ReadyMustSync{}).Check(trace); len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestReadyMustSyncClassifiesMalformedDriverEvidenceAsInvalid(t *testing.T) {
	trace := []core.TraceRecord{{
		Step: 3,
		Observations: []core.Observation{{
			Kind: "ready", Label: "ready:must-sync", Value: "false",
			Evidence: map[string]string{"must_sync": "true", "entries": "0", "hard_state_empty": "true"},
		}},
	}}
	monitor := raftfamily.ReadyMustSync{}
	if err := monitor.ValidateEvidence(trace); err == nil {
		t.Fatal("malformed Driver evidence was accepted")
	}
	if violations := monitor.Check(trace); len(violations) != 0 {
		t.Fatalf("malformed evidence created protocol violations: %#v", violations)
	}
}

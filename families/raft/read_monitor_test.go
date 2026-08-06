package raft_test

import (
	"encoding/json"
	"strings"
	"testing"

	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/families/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

func TestLinearizableReadUsesTrustedPreQueryCommitLowerBound(t *testing.T) {
	trace := []core.TraceRecord{
		readQueryRecord(1, "same-context", 4),
		readQueryRecord(2, "same-context", 7),
		readStateRecord(3, "same-context", "6"),
	}
	violations := (raftfamily.LinearizableRead{}).Check(trace)
	if len(violations) != 1 || !strings.Contains(violations[0].Message, "below trusted lower bound 7") {
		t.Fatalf("violations = %#v, want one lower-bound violation", violations)
	}
}

func TestLinearizableReadAcceptsSafeOrUnansweredRequest(t *testing.T) {
	trace := []core.TraceRecord{
		readQueryRecord(1, "answered", 5),
		readStateRecord(2, "answered", "5"),
		readQueryRecord(3, "unanswered", 8),
	}
	if violations := (raftfamily.LinearizableRead{}).Check(trace); len(violations) != 0 {
		t.Fatalf("violations = %#v, want none", violations)
	}
}

func TestLinearizableReadSeparatesUnboundStateFromMalformedEvidence(t *testing.T) {
	trace := []core.TraceRecord{
		readStateRecord(1, "unknown", "3"),
		readQueryRecord(2, "known", 2),
		readStateRecord(3, "known", "not-an-index"),
	}
	monitor := raftfamily.LinearizableRead{}
	if err := monitor.ValidateEvidence(trace); err == nil {
		t.Fatal("malformed read-state evidence was accepted")
	}
	if violations := monitor.Check(trace); len(violations) != 1 || !strings.Contains(violations[0].Message, "unknown request") {
		t.Fatalf("violations = %#v, want only the semantic unbound-context violation", violations)
	}
}

func readQueryRecord(step int, requestID string, commit uint64) core.TraceRecord {
	payload, _ := json.Marshal(map[string]string{
		"operation": "linearizable-read", "request_id": requestID,
	})
	return core.TraceRecord{
		Step: step, Outcome: string(core.StatusApplied),
		Event: core.Event{Kind: core.EventQuery, Payload: payload},
		Before: map[string]any{
			"driver": map[string]any{"nodes": map[string]any{
				"n1": map[string]any{"commit": commit - 1},
				"n2": map[string]any{"commit": commit},
			}},
		},
	}
}

func readStateRecord(step int, requestID, index string) core.TraceRecord {
	return core.TraceRecord{Step: step, Observations: []core.Observation{{
		Kind: "read-state", Value: requestID,
		Evidence: map[string]string{"request_id": requestID, "index": index},
	}}}
}

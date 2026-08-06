package core_test

import (
	"encoding/json"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

func TestCanonicalTraceDigestSurvivesPersistence(t *testing.T) {
	trace := []core.TraceRecord{{
		Step: 1, LogicalTime: 2, Event: core.Event{ID: "event-1", Kind: core.EventCampaign},
		Outcome: "applied",
		Before:  map[string]any{"term": uint64(9007199254740993), "nested": []any{1, "x"}},
		After:   map[string]any{"term": uint64(9007199254740994)},
	}}
	want, err := core.CanonicalTraceDigest(trace)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	var persisted []core.TraceRecord
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	got, err := core.CanonicalTraceDigest(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("persisted trace digest = %s, in-memory digest = %s", got, want)
	}
}

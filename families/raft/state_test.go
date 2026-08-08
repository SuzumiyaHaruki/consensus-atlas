package raft_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/families/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

func TestDeclarativePSSMatchesExecutableProjector(t *testing.T) {
	data, err := os.ReadFile("pss/raft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var declaration struct {
		Version  int    `json:"version"`
		ID       string `json:"id"`
		Sampling struct {
			Included []string `json:"included_boundaries"`
			Excluded []string `json:"excluded_microsteps"`
		} `json:"sampling"`
		Limits struct {
			MaxNodes int `json:"max_nodes_for_exact_permutation"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(data, &declaration); err != nil {
		t.Fatal(err)
	}
	if declaration.Version != raftfamily.PSSVersion || declaration.ID != raftfamily.PSSID {
		t.Fatalf("declaration identity = (%d, %s), executable = (%d, %s)",
			declaration.Version, declaration.ID, raftfamily.PSSVersion, raftfamily.PSSID)
	}
	if declaration.Limits.MaxNodes != raftfamily.MaxCanonicalNodes {
		t.Fatalf("declarative max nodes = %d, executable max = %d",
			declaration.Limits.MaxNodes, raftfamily.MaxCanonicalNodes)
	}
	if want := []string{"acknowledge", "crash", "restart"}; !reflect.DeepEqual(declaration.Sampling.Included, want) {
		t.Fatalf("included boundaries = %#v, want %#v", declaration.Sampling.Included, want)
	}
	if want := []string{"persist", "sync", "emit", "apply"}; !reflect.DeepEqual(declaration.Sampling.Excluded, want) {
		t.Fatalf("excluded microsteps = %#v, want %#v", declaration.Sampling.Excluded, want)
	}
}

func TestProjectIsInvariantToNodeTermIndexAndValueRenaming(t *testing.T) {
	left := snapshot(map[string]nodeFixture{
		"a": fixture(true, "leader", 8, "a", "a", 5, 8, "value-alpha", []string{"a", "b", "c"}),
		"b": fixture(true, "follower", 8, "a", "a", 5, 8, "value-alpha", []string{"a", "b", "c"}),
		"c": fixture(true, "follower", 8, "a", "a", 5, 8, "value-alpha", []string{"a", "b", "c"}),
	})
	right := snapshot(map[string]nodeFixture{
		"x": fixture(true, "follower", 80, "y", "y", 50, 80, "renamed-value", []string{"x", "y", "z"}),
		"y": fixture(true, "leader", 80, "y", "y", 50, 80, "renamed-value", []string{"x", "y", "z"}),
		"z": fixture(true, "follower", 80, "y", "y", 50, 80, "renamed-value", []string{"x", "y", "z"}),
	})
	_, leftKey, err := raftfamily.Project(left)
	if err != nil {
		t.Fatal(err)
	}
	_, rightKey, err := raftfamily.Project(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftKey != rightKey {
		t.Fatalf("canonical keys differ after semantic renaming:\n%s\n%s", leftKey, rightKey)
	}
}

func TestProjectDistinguishesConflictingLogShape(t *testing.T) {
	same := snapshot(map[string]nodeFixture{
		"a": fixture(true, "leader", 8, "a", "a", 5, 8, "same", []string{"a", "b", "c"}),
		"b": fixture(true, "follower", 8, "a", "a", 5, 8, "same", []string{"a", "b", "c"}),
		"c": fixture(true, "follower", 8, "a", "a", 5, 8, "same", []string{"a", "b", "c"}),
	})
	conflict := snapshot(map[string]nodeFixture{
		"a": fixture(true, "leader", 8, "a", "a", 5, 8, "same", []string{"a", "b", "c"}),
		"b": fixture(true, "follower", 8, "a", "a", 5, 8, "same", []string{"a", "b", "c"}),
		"c": fixture(true, "follower", 8, "a", "a", 5, 8, "conflict", []string{"a", "b", "c"}),
	})
	_, sameKey, err := raftfamily.Project(same)
	if err != nil {
		t.Fatal(err)
	}
	_, conflictKey, err := raftfamily.Project(conflict)
	if err != nil {
		t.Fatal(err)
	}
	if sameKey == conflictKey {
		t.Fatal("same-log and conflicting-log states have the same canonical key")
	}
}

func TestProjectRejectsUnknownNodeRelations(t *testing.T) {
	invalid := snapshot(map[string]nodeFixture{
		"a": fixture(true, "leader", 8, "outside", "a", 5, 8, "value", []string{"a", "b", "c"}),
		"b": fixture(true, "follower", 8, "a", "a", 5, 8, "value", []string{"a", "b", "c"}),
		"c": fixture(true, "follower", 8, "a", "a", 5, 8, "value", []string{"a", "b", "c"}),
	})
	if _, _, err := raftfamily.Project(invalid); err == nil {
		t.Fatal("unknown vote relation was accepted")
	}
}

func TestDiscoverSamplesStableBoundariesOnly(t *testing.T) {
	running := snapshot(map[string]nodeFixture{
		"a": fixture(true, "leader", 2, "a", "a", 4, 2, "value", []string{"a", "b", "c"}),
		"b": fixture(true, "follower", 2, "a", "a", 4, 2, "value", []string{"a", "b", "c"}),
		"c": fixture(true, "follower", 2, "a", "a", 4, 2, "value", []string{"a", "b", "c"}),
	})
	crashed := snapshot(map[string]nodeFixture{
		"a": fixture(false, "stopped", 2, "a", "a", 4, 2, "value", []string{"a", "b", "c"}),
		"b": fixture(true, "follower", 2, "a", "a", 4, 2, "value", []string{"a", "b", "c"}),
		"c": fixture(true, "follower", 2, "a", "a", 4, 2, "value", []string{"a", "b", "c"}),
	})
	trace := []core.TraceRecord{
		{Step: 1, Event: core.Event{Kind: core.EventAcknowledge}, Outcome: "applied", After: running},
		{Step: 2, Event: core.Event{Kind: core.EventPersist}, Outcome: "applied", After: running},
		{Step: 3, Event: core.Event{Kind: core.EventSync}, Outcome: "applied", After: running},
		{Step: 4, Event: core.Event{Kind: core.EventAcknowledge}, Outcome: "applied", After: running},
		{Step: 5, Event: core.Event{Kind: core.EventCrash}, Outcome: "applied", After: crashed},
	}
	summary, err := raftfamily.Discover(trace)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Samples != 3 {
		t.Fatalf("samples = %d, want 3 acknowledge/crash boundaries", summary.Samples)
	}
	if summary.UniqueStates != 2 {
		t.Fatalf("unique states = %d, want running and crashed", summary.UniqueStates)
	}
	wantCurve := []protocolstate.DiscoveryPoint{
		{Step: 1, Samples: 1, UniqueStates: 1, NewState: true},
		{Step: 4, Samples: 2, UniqueStates: 1, NewState: false},
		{Step: 5, Samples: 3, UniqueStates: 2, NewState: true},
	}
	if !reflect.DeepEqual(summary.Curve, wantCurve) {
		t.Fatalf("unexpected discovery curve: %#v", summary.Curve)
	}
	_, runningKey, err := raftfamily.Project(running)
	if err != nil {
		t.Fatal(err)
	}
	_, crashedKey, err := raftfamily.Project(crashed)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.States) != 2 || summary.States[0].Key != runningKey ||
		summary.States[0].FirstStep != 1 || summary.States[1].Key != crashedKey ||
		summary.States[1].FirstStep != 5 {
		t.Fatalf("unexpected discovery witnesses: %#v", summary.States)
	}
}

type nodeFixture map[string]any

func fixture(
	running bool,
	role string,
	term uint64,
	vote, lead string,
	index, entryTerm uint64,
	value string,
	voters []string,
) nodeFixture {
	return nodeFixture{
		"running": running, "role": role, "term": term, "vote": vote, "lead": lead,
		"commit": index, "applied": index, "durable_term": term, "durable_commit": index,
		"durable_last_index": index, "durable_last_term": entryTerm,
		"durable_snapshot_index": uint64(0), "durable_snapshot_term": uint64(0),
		"durable_log": []map[string]any{{
			"index": index, "term": entryTerm, "type": "EntryNormal", "value_digest": value,
		}},
		"voters": voters,
	}
}

func snapshot(nodes map[string]nodeFixture) map[string]any {
	converted := make(map[string]any, len(nodes))
	for name, node := range nodes {
		converted[name] = node
	}
	return map[string]any{"driver": map[string]any{"nodes": converted}}
}

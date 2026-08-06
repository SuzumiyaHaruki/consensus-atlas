package raft_test

import (
	"encoding/json"
	"testing"

	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/families/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
)

func TestCoverageMatcherDistinguishesRaftLogRelations(t *testing.T) {
	matcher := raftfamily.CoverageMatcher{}
	base := traceSnapshot(map[string][]entry{
		"a": {{1, 1, "x"}},
		"b": {{1, 1, "x"}},
		"c": {{1, 1, "x"}, {2, 1, "y"}},
	})
	if !matcher.Match(semantic(raftfamily.RelationLogAfter, raftfamily.LogSameNonEmpty), core.TraceRecord{After: base}) {
		t.Fatal("equal non-empty logs were not matched")
	}
	if !matcher.Match(semantic(raftfamily.RelationLogAfter, raftfamily.LogStrictPrefix), core.TraceRecord{After: base}) {
		t.Fatal("strict log prefix was not matched")
	}
	conflict := traceSnapshot(map[string][]entry{
		"a": {{1, 1, "x"}},
		"b": {{1, 2, "z"}},
	})
	if !matcher.Match(semantic(raftfamily.RelationLogAfter, raftfamily.LogConflict), core.TraceRecord{After: conflict}) {
		t.Fatal("conflicting logs were not matched")
	}
}

func TestCoverageMatcherUsesRoleAndPartitionEvidence(t *testing.T) {
	matcher := raftfamily.CoverageMatcher{}
	snapshot := traceSnapshot(map[string][]entry{"a": nil, "b": nil, "c": nil})
	driver := snapshot.(map[string]any)["driver"].(map[string]any)
	nodes := driver["nodes"].(map[string]any)
	node := nodes["a"].(map[string]any)
	node["role"] = "leader"
	record := core.TraceRecord{Event: core.Event{Kind: core.EventCrash, Target: "a"}, Before: snapshot}
	if !matcher.Match(semantic(raftfamily.RelationTargetRoleBefore, "leader"), record) {
		t.Fatal("target role before event was not matched")
	}
	payload, _ := json.Marshal(map[string]any{"groups": [][]string{{"a"}, {"b", "c"}}})
	partition := core.TraceRecord{Event: core.Event{Kind: core.EventPartition, Payload: payload}, Before: snapshot}
	if !matcher.Match(semantic(raftfamily.RelationPartitionShape, raftfamily.PartitionMinorityQuorum), partition) {
		t.Fatal("minority/quorum partition was not matched")
	}
	if !matcher.Match(semantic(raftfamily.RelationPartitionShape, raftfamily.PartitionIsolateLeader), partition) {
		t.Fatal("isolated leader partition was not matched")
	}
	partition.Before = map[string]any{"system": snapshot, "network": map[string]any{"partition_groups": map[string]int{}}}
	if !matcher.Match(semantic(raftfamily.RelationPartitionShape, raftfamily.PartitionIsolateLeader), partition) {
		t.Fatal("control snapshot did not expose the frozen system state to the Family matcher")
	}
}

type entry struct {
	index uint64
	term  uint64
	value string
}

func traceSnapshot(logs map[string][]entry) any {
	nodes := make(map[string]any)
	for name, entries := range logs {
		log := make([]map[string]any, 0, len(entries))
		for _, item := range entries {
			log = append(log, map[string]any{
				"index": item.index, "term": item.term, "type": "EntryNormal", "value_digest": item.value,
			})
		}
		nodes[name] = map[string]any{
			"running": true, "role": "follower", "term": uint64(1), "commit": uint64(0),
			"applied": uint64(0), "durable_term": uint64(1), "durable_commit": uint64(0),
			"durable_last_index": uint64(len(entries)), "durable_last_term": uint64(1),
			"durable_snapshot_index": uint64(0), "durable_snapshot_term": uint64(0),
			"durable_log": log, "voters": []string{"a", "b", "c"},
		}
	}
	return map[string]any{"driver": map[string]any{"nodes": nodes}}
}

func semantic(relation, value string) coverage.SemanticPredicate {
	return coverage.SemanticPredicate{Domain: raftfamily.PSSID, Relation: relation, Value: value}
}

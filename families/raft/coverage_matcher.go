package raft

import (
	"encoding/json"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
)

const (
	RelationTargetRoleBefore = "event-target-role-before"
	RelationTargetRoleAfter  = "event-target-role-after"
	RelationLogAfter         = "durable-normal-log-relation-after"
	RelationPartitionShape   = "partition-shape"
	RelationLeaderCountAfter = "leader-count-after"

	LogSameNonEmpty = "same-nonempty"
	LogStrictPrefix = "strict-prefix"
	LogConflict     = "conflict"

	PartitionMinorityQuorum = "minority-vs-quorum"
	PartitionIsolateLeader  = "isolate-leader"
)

// CoverageMatcher interprets only Raft Family PSS relations over immutable
// trace snapshots. The generic coverage package remains unaware of Raft.
type CoverageMatcher struct{}

func (CoverageMatcher) ID() string { return PSSID }

func (CoverageMatcher) Match(predicate coverage.SemanticPredicate, record core.TraceRecord) bool {
	if predicate.Domain != PSSID {
		return false
	}
	switch predicate.Relation {
	case RelationTargetRoleBefore:
		return targetRole(record.Before, record.Event.Target) == predicate.Value
	case RelationTargetRoleAfter:
		return targetRole(record.After, record.Event.Target) == predicate.Value
	case RelationLogAfter:
		return hasLogRelation(record.After, predicate.Value, minimumEntries(predicate.Parameters))
	case RelationPartitionShape:
		return partitionRelation(record, predicate.Value)
	case RelationLeaderCountAfter:
		return leaderCountMatches(record.After, predicate.Parameters)
	default:
		return false
	}
}

func targetRole(snapshot any, target string) string {
	if target == "" {
		return ""
	}
	state, err := decodeSnapshot(snapshot)
	if err != nil {
		return ""
	}
	return state.Driver.Nodes[target].Role
}

func hasLogRelation(snapshot any, relation string, minimum int) bool {
	state, err := decodeSnapshot(snapshot)
	if err != nil {
		return false
	}
	names := make([]string, 0, len(state.Driver.Nodes))
	for name := range state.Driver.Nodes {
		names = append(names, name)
	}
	for left := 0; left < len(names); left++ {
		for right := left + 1; right < len(names); right++ {
			first := applicationEntries(state.Driver.Nodes[names[left]].DurableLog)
			second := applicationEntries(state.Driver.Nodes[names[right]].DurableLog)
			switch relation {
			case LogSameNonEmpty:
				if len(first) >= minimum && len(second) >= minimum && entriesEqual(first, second) {
					return true
				}
			case LogStrictPrefix:
				if isStrictPrefix(first, second, minimum) || isStrictPrefix(second, first, minimum) {
					return true
				}
			case LogConflict:
				if entriesConflict(first, second) {
					return true
				}
			}
		}
	}
	return false
}

func applicationEntries(entries []implementationEntry) []implementationEntry {
	result := make([]implementationEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type == "EntryNormal" && entry.ValueDigest != "" {
			result = append(result, entry)
		}
	}
	return result
}

func entriesEqual(left, right []implementationEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !entryEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

func isStrictPrefix(prefix, complete []implementationEntry, minimum int) bool {
	if len(complete) < minimum || len(prefix) >= len(complete) {
		return false
	}
	for index := range prefix {
		if !entryEqual(prefix[index], complete[index]) {
			return false
		}
	}
	return true
}

func entriesConflict(left, right []implementationEntry) bool {
	for _, first := range left {
		for _, second := range right {
			if first.Index == second.Index && !entryEqual(first, second) {
				return true
			}
		}
	}
	return false
}

func entryEqual(left, right implementationEntry) bool {
	return left.Index == right.Index && left.Term == right.Term && left.Type == right.Type && left.ValueDigest == right.ValueDigest
}

func minimumEntries(parameters map[string]string) int {
	value, err := strconv.Atoi(parameters["min_entries"])
	if err != nil || value < 1 {
		return 1
	}
	return value
}

func partitionRelation(record core.TraceRecord, relation string) bool {
	if record.Event.Kind != core.EventPartition {
		return false
	}
	var payload struct {
		Groups [][]string `json:"groups"`
	}
	if json.Unmarshal(record.Event.Payload, &payload) != nil || len(payload.Groups) < 2 {
		return false
	}
	state, err := decodeSnapshot(record.Before)
	if err != nil {
		return false
	}
	quorum := len(state.Driver.Nodes)/2 + 1
	switch relation {
	case PartitionMinorityQuorum:
		minority, enough := false, false
		for _, group := range payload.Groups {
			minority = minority || len(group) < quorum
			enough = enough || len(group) >= quorum
		}
		return minority && enough
	case PartitionIsolateLeader:
		leader := ""
		for name, node := range state.Driver.Nodes {
			if node.Role == "leader" {
				leader = name
				break
			}
		}
		if leader == "" {
			return false
		}
		for _, group := range payload.Groups {
			for _, node := range group {
				if node == leader {
					return len(group) < quorum
				}
			}
		}
	}
	return false
}

func leaderCountMatches(snapshot any, parameters map[string]string) bool {
	state, err := decodeSnapshot(snapshot)
	if err != nil {
		return false
	}
	count := 0
	for _, node := range state.Driver.Nodes {
		if node.Running && node.Role == "leader" {
			count++
		}
	}
	minimum, _ := strconv.Atoi(parameters["min"])
	maximum, maxErr := strconv.Atoi(parameters["max"])
	return count >= minimum && (maxErr != nil || maximum < 0 || count <= maximum)
}

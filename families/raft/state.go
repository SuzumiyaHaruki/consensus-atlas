package raft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

const (
	PSSID             = "raft-family-pss-v1"
	PSSVersion        = 1
	MaxCanonicalNodes = 8
)

type implementationSnapshot struct {
	Driver struct {
		Nodes map[string]implementationNode `json:"nodes"`
	} `json:"driver"`
}

type implementationNode struct {
	Running              bool                  `json:"running"`
	Epoch                uint64                `json:"epoch"`
	Role                 string                `json:"role"`
	Term                 uint64                `json:"term"`
	Vote                 string                `json:"vote"`
	Lead                 string                `json:"lead"`
	Commit               uint64                `json:"commit"`
	Applied              uint64                `json:"applied"`
	DurableTerm          uint64                `json:"durable_term"`
	DurableCommit        uint64                `json:"durable_commit"`
	DurableLastIndex     uint64                `json:"durable_last_index"`
	DurableLastTerm      uint64                `json:"durable_last_term"`
	DurableSnapshotIndex uint64                `json:"durable_snapshot_index"`
	DurableSnapshotTerm  uint64                `json:"durable_snapshot_term"`
	DurableLog           []implementationEntry `json:"durable_log"`
	Voters               []string              `json:"voters"`
	Outstanding          string                `json:"outstanding"`
}

type implementationEntry struct {
	Index       uint64 `json:"index"`
	Term        uint64 `json:"term"`
	Type        string `json:"type"`
	ValueDigest string `json:"value_digest"`
}

type CanonicalState struct {
	PSSID string          `json:"pss_id"`
	Nodes []CanonicalNode `json:"nodes"`
}

type CanonicalNode struct {
	ID                   string           `json:"id"`
	Running              bool             `json:"running"`
	Role                 string           `json:"role"`
	Term                 int              `json:"term_rank"`
	Vote                 string           `json:"vote,omitempty"`
	Lead                 string           `json:"lead,omitempty"`
	Commit               int              `json:"commit_rank"`
	Applied              int              `json:"applied_rank"`
	DurableTerm          int              `json:"durable_term_rank"`
	DurableCommit        int              `json:"durable_commit_rank"`
	DurableLastIndex     int              `json:"durable_last_index_rank"`
	DurableLastTerm      int              `json:"durable_last_term_rank"`
	DurableSnapshotIndex int              `json:"durable_snapshot_index_rank"`
	DurableSnapshotTerm  int              `json:"durable_snapshot_term_rank"`
	DurableLog           []CanonicalEntry `json:"durable_log,omitempty"`
	Voters               []string         `json:"voters,omitempty"`
}

type CanonicalEntry struct {
	Index int    `json:"index_rank"`
	Term  int    `json:"term_rank"`
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
}

// Projector implements the generic discovery boundary for Raft-family PSS v1.
// It contains no etcd/raft types and consumes only versioned snapshot evidence.
type Projector struct{}

func (Projector) ID() string { return PSSID }

// IsSample deliberately excludes Ready host microsteps. A completed
// acknowledge closes one Ready batch; crash and restart change availability and
// must be visible even before the next batch closes.
func (Projector) IsSample(record core.TraceRecord) bool {
	if record.Outcome != string(core.StatusApplied) {
		return false
	}
	switch record.Event.Kind {
	case core.EventAcknowledge, core.EventCrash, core.EventRestart:
		return true
	default:
		return false
	}
}

func IsProtocolSample(record core.TraceRecord) bool {
	return (Projector{}).IsSample(record)
}

func (Projector) Project(snapshot any) (any, string, error) {
	return Project(snapshot)
}

func Discover(trace []core.TraceRecord) (protocolstate.DiscoverySummary, error) {
	projector := Projector{}
	samples := make([]protocolstate.Sample, 0, len(trace))
	for _, record := range trace {
		if !projector.IsSample(record) {
			continue
		}
		state, key, err := projector.Project(record.After)
		if err != nil {
			return protocolstate.DiscoverySummary{}, fmt.Errorf(
				"project protocol state at step %d: %w", record.Step, err,
			)
		}
		samples = append(samples, protocolstate.Sample{Step: record.Step, Key: key, State: state})
	}
	return protocolstate.Discover(PSSID, samples)
}

func Project(snapshot any) (CanonicalState, string, error) {
	implementation, err := decodeSnapshot(snapshot)
	if err != nil {
		return CanonicalState{}, "", err
	}
	names := make([]string, 0, len(implementation.Driver.Nodes))
	for name := range implementation.Driver.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return CanonicalState{}, "", errors.New("snapshot contains no raft nodes")
	}
	if len(names) > MaxCanonicalNodes {
		return CanonicalState{}, "", fmt.Errorf(
			"exact node permutation supports at most %d nodes, got %d", MaxCanonicalNodes, len(names),
		)
	}
	if err := validateEvidence(implementation.Driver.Nodes, names); err != nil {
		return CanonicalState{}, "", err
	}
	termRanks := rankTerms(implementation.Driver.Nodes)
	indexRanks := rankIndexes(implementation.Driver.Nodes)

	var bestEncoded []byte
	var bestState CanonicalState
	visitPermutations(names, func(order []string) {
		candidate := buildCandidate(implementation.Driver.Nodes, order, termRanks, indexRanks)
		encoded, marshalErr := json.Marshal(candidate)
		if marshalErr != nil {
			return
		}
		if bestEncoded == nil || string(encoded) < string(bestEncoded) {
			bestEncoded = append([]byte(nil), encoded...)
			bestState = candidate
		}
	})
	if bestEncoded == nil {
		return CanonicalState{}, "", errors.New("failed to construct canonical raft state")
	}
	sum := sha256.Sum256(bestEncoded)
	return bestState, hex.EncodeToString(sum[:]), nil
}

func validateEvidence(nodes map[string]implementationNode, names []string) error {
	for _, name := range names {
		node := nodes[name]
		relations := []struct {
			name   string
			target string
		}{{"vote", node.Vote}, {"lead", node.Lead}}
		for _, relation := range relations {
			target := relation.target
			if target == "" {
				continue
			}
			if _, ok := nodes[target]; !ok {
				return fmt.Errorf("node %s has %s relation to unknown node %s", name, relation.name, target)
			}
		}
		seenVoters := make(map[string]bool, len(node.Voters))
		for _, voter := range node.Voters {
			if _, ok := nodes[voter]; !ok {
				return fmt.Errorf("node %s has voter relation to unknown node %s", name, voter)
			}
			if seenVoters[voter] {
				return fmt.Errorf("node %s has duplicate voter relation %s", name, voter)
			}
			seenVoters[voter] = true
		}
		seenIndexes := make(map[uint64]bool, len(node.DurableLog))
		for _, entry := range node.DurableLog {
			if entry.Index == 0 {
				return fmt.Errorf("node %s has durable entry at index zero", name)
			}
			if entry.Type == "" {
				return fmt.Errorf("node %s has durable entry %d without a type", name, entry.Index)
			}
			if seenIndexes[entry.Index] {
				return fmt.Errorf("node %s has duplicate durable entry index %d", name, entry.Index)
			}
			seenIndexes[entry.Index] = true
		}
	}
	return nil
}

func decodeSnapshot(snapshot any) (implementationSnapshot, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return implementationSnapshot{}, err
	}
	var implementation implementationSnapshot
	if err := json.Unmarshal(encoded, &implementation); err != nil {
		return implementationSnapshot{}, err
	}
	if implementation.Driver.Nodes == nil {
		var control struct {
			System json.RawMessage `json:"system"`
		}
		if json.Unmarshal(encoded, &control) == nil && len(control.System) != 0 {
			if err := json.Unmarshal(control.System, &implementation); err != nil {
				return implementationSnapshot{}, err
			}
		}
	}
	if implementation.Driver.Nodes == nil {
		return implementationSnapshot{}, errors.New("snapshot has no driver.nodes evidence")
	}
	return implementation, nil
}

func buildCandidate(
	nodes map[string]implementationNode,
	order []string,
	termRanks, indexRanks map[uint64]int,
) CanonicalState {
	aliases := make(map[string]string, len(order))
	for index, name := range order {
		aliases[name] = fmt.Sprintf("n%d", index+1)
	}
	valueAliases := make(map[string]string)
	state := CanonicalState{PSSID: PSSID, Nodes: make([]CanonicalNode, 0, len(order))}
	for index, name := range order {
		source := nodes[name]
		canonical := CanonicalNode{
			ID: fmt.Sprintf("n%d", index+1), Running: source.Running, Role: source.Role,
			Term: termRanks[source.Term], Vote: relationAlias(source.Vote, aliases),
			Lead: relationAlias(source.Lead, aliases), Commit: indexRanks[source.Commit],
			Applied: indexRanks[source.Applied], DurableTerm: termRanks[source.DurableTerm],
			DurableCommit:        indexRanks[source.DurableCommit],
			DurableLastIndex:     indexRanks[source.DurableLastIndex],
			DurableLastTerm:      termRanks[source.DurableLastTerm],
			DurableSnapshotIndex: indexRanks[source.DurableSnapshotIndex],
			DurableSnapshotTerm:  termRanks[source.DurableSnapshotTerm],
		}
		entries := append([]implementationEntry(nil), source.DurableLog...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Index < entries[j].Index })
		for _, entry := range entries {
			current := CanonicalEntry{
				Index: indexRanks[entry.Index], Term: termRanks[entry.Term], Type: entry.Type,
			}
			if entry.ValueDigest != "" {
				alias, ok := valueAliases[entry.ValueDigest]
				if !ok {
					alias = fmt.Sprintf("v%d", len(valueAliases)+1)
					valueAliases[entry.ValueDigest] = alias
				}
				current.Value = alias
			}
			canonical.DurableLog = append(canonical.DurableLog, current)
		}
		for _, voter := range source.Voters {
			canonical.Voters = append(canonical.Voters, relationAlias(voter, aliases))
		}
		sort.Strings(canonical.Voters)
		state.Nodes = append(state.Nodes, canonical)
	}
	return state
}

func rankTerms(nodes map[string]implementationNode) map[uint64]int {
	var values []uint64
	for _, node := range nodes {
		values = append(values, node.Term, node.DurableTerm, node.DurableLastTerm, node.DurableSnapshotTerm)
		for _, entry := range node.DurableLog {
			values = append(values, entry.Term)
		}
	}
	return rankNonZero(values)
}

func rankIndexes(nodes map[string]implementationNode) map[uint64]int {
	var values []uint64
	for _, node := range nodes {
		values = append(values, node.Commit, node.Applied, node.DurableCommit,
			node.DurableLastIndex, node.DurableSnapshotIndex)
		for _, entry := range node.DurableLog {
			values = append(values, entry.Index)
		}
	}
	return rankNonZero(values)
}

func rankNonZero(values []uint64) map[uint64]int {
	unique := make(map[uint64]bool)
	for _, value := range values {
		if value != 0 {
			unique[value] = true
		}
	}
	ordered := make([]uint64, 0, len(unique))
	for value := range unique {
		ordered = append(ordered, value)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	ranks := map[uint64]int{0: 0}
	for index, value := range ordered {
		ranks[value] = index + 1
	}
	return ranks
}

func relationAlias(value string, aliases map[string]string) string {
	if value == "" {
		return ""
	}
	if alias, ok := aliases[value]; ok {
		return alias
	}
	return "external" // validateEvidence rejects this for the current closed cluster model.
}

func visitPermutations(items []string, visit func([]string)) {
	working := append([]string(nil), items...)
	var generate func(int)
	generate = func(index int) {
		if index == len(working) {
			visit(append([]string(nil), working...))
			return
		}
		for next := index; next < len(working); next++ {
			working[index], working[next] = working[next], working[index]
			generate(index + 1)
			working[index], working[next] = working[next], working[index]
		}
	}
	generate(0)
}

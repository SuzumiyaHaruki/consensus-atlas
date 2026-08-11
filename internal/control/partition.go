package control

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
)

// PartitionParameters is the public, canonical parameter contract shared by
// Runtime-owned Partition/Heal actions and target-specific actuators.
type PartitionParameters struct {
	ID    string   `json:"id"`
	Left  []NodeID `json:"left"`
	Right []NodeID `json:"right"`
}

func NewPartitionParameters(left, right []NodeID) (PartitionParameters, error) {
	left, right = canonicalNodeSet(left), canonicalNodeSet(right)
	if len(left) == 0 || len(right) == 0 {
		return PartitionParameters{}, fmt.Errorf("PARTITION_GROUP_REQUIRED")
	}
	if nodeSetKey(right) < nodeSetKey(left) {
		left, right = right, left
	}
	seen := make(map[NodeID]struct{}, len(left))
	for _, node := range left {
		seen[node] = struct{}{}
	}
	for _, node := range right {
		if _, exists := seen[node]; exists {
			return PartitionParameters{}, fmt.Errorf("PARTITION_GROUP_OVERLAP: %s", node)
		}
	}
	id, err := StableID("partition", nodeSetKey(left), nodeSetKey(right))
	if err != nil {
		return PartitionParameters{}, err
	}
	return PartitionParameters{ID: id, Left: left, Right: right}, nil
}

func DecodePartitionParameters(raw json.RawMessage) (PartitionParameters, error) {
	var parameters PartitionParameters
	if err := json.Unmarshal(raw, &parameters); err != nil {
		return PartitionParameters{}, fmt.Errorf("PARTITION_PARAMETERS_INVALID: %w", err)
	}
	if err := parameters.Validate(); err != nil {
		return PartitionParameters{}, err
	}
	return parameters, nil
}

func (parameters PartitionParameters) Validate() error {
	canonical, err := NewPartitionParameters(parameters.Left, parameters.Right)
	if err != nil {
		return err
	}
	if parameters.ID != canonical.ID || !slices.Equal(parameters.Left, canonical.Left) ||
		!slices.Equal(parameters.Right, canonical.Right) {
		return fmt.Errorf("PARTITION_PARAMETERS_NONCANONICAL")
	}
	return nil
}

func canonicalNodeSet(nodes []NodeID) []NodeID {
	seen := make(map[NodeID]struct{}, len(nodes))
	result := make([]NodeID, 0, len(nodes))
	for _, node := range nodes {
		if node == "" {
			continue
		}
		if _, exists := seen[node]; exists {
			continue
		}
		seen[node] = struct{}{}
		result = append(result, node)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func nodeSetKey(nodes []NodeID) string {
	encoded, _ := json.Marshal(nodes)
	return string(encoded)
}

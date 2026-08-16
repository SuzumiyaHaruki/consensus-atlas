package omnipaxosv2

import (
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

// Evidence is the target-owned read-only projection used by semantic
// composition. It omits worker transport state and raw protocol messages.
type Evidence struct {
	LogicalTime uint64         `json:"logical_time"`
	Nodes       []NodeEvidence `json:"nodes"`
}

type NodeEvidence struct {
	Node                control.NodeID           `json:"node"`
	Leader              control.NodeID           `json:"leader,omitempty"`
	DecidedIndex        uint64                   `json:"decided_index"`
	DecidedPrefixDigest string                   `json:"decided_prefix_digest"`
	DecidedPrefixes     []DecisionPrefixEvidence `json:"decided_prefixes"`
	PromiseNumber       uint32                   `json:"promise_number"`
	PromisePriority     uint32                   `json:"promise_priority"`
	PromiseNode         control.NodeID           `json:"promise_node,omitempty"`
}

type DecisionPrefixEvidence struct {
	Index  uint64 `json:"index"`
	Digest string `json:"digest"`
}

func ProjectEvidence(envelope control.EvidenceEnvelope) (Evidence, error) {
	snapshot, err := decodeEvidence(envelope)
	if err != nil {
		return Evidence{}, err
	}
	result := Evidence{LogicalTime: snapshot.LogicalTime, Nodes: make([]NodeEvidence, 0, len(snapshot.Nodes))}
	for _, node := range snapshot.Nodes {
		prefixes := make([]DecisionPrefixEvidence, 0, len(node.DecidedPrefixes))
		for _, prefix := range node.DecidedPrefixes {
			prefixes = append(prefixes, DecisionPrefixEvidence{Index: prefix.Index, Digest: prefix.Digest})
		}
		result.Nodes = append(result.Nodes, NodeEvidence{
			Node: nodeNames[node.ID], Leader: nodeNames[node.Leader],
			DecidedIndex: node.DecidedIndex, DecidedPrefixDigest: node.DecidedPrefixDigest,
			DecidedPrefixes: prefixes,
			PromiseNumber:   node.PromiseNumber, PromisePriority: node.PromisePriority,
			PromiseNode: nodeNames[node.PromisePID],
		})
	}
	return result, nil
}

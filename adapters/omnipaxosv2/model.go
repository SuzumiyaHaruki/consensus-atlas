package omnipaxosv2

import "github.com/SuzumiyaHaruki/consensus-atlas/internal/control"

const (
	adapterID      = "omnipaxos-v2alpha1"
	implementation = "crates.io/omnipaxos@0.2.2"
	workerSchema   = "consensus-atlas/omnipaxos-worker/v1"
	evidenceSchema = "consensus-atlas/omnipaxos-v2-evidence/v1"
	messageSchema  = "consensus-atlas/omnipaxos-v2-message/v1"
	callbackSchema = "consensus-atlas/omnipaxos-v2-callback/v1"
	inputSchema    = "consensus-atlas/omnipaxos-v2-input/v1"
	resultSchema   = "consensus-atlas/omnipaxos-v2-client-result/v1"
)

var nodeNames = map[uint64]control.NodeID{1: "n1", 2: "n2", 3: "n3"}

type Config struct {
	WorkerPath string
}

type workerRequest struct {
	ID      uint64 `json:"id"`
	Op      string `json:"op"`
	Node    uint64 `json:"node,omitempty"`
	Payload []byte `json:"payload,omitempty"`
}

type workerResponse struct {
	SchemaVersion string           `json:"schema_version"`
	ID            uint64           `json:"id"`
	OK            bool             `json:"ok"`
	Error         string           `json:"error,omitempty"`
	Nodes         []workerNode     `json:"nodes"`
	Messages      []workerMessage  `json:"messages"`
	Decisions     []workerDecision `json:"decisions"`
}

type workerNode struct {
	ID                  uint64 `json:"id"`
	Leader              uint64 `json:"leader"`
	DecidedIndex        uint64 `json:"decided_index"`
	DecidedPrefixDigest string `json:"decided_prefix_digest"`
	PromiseNumber       uint32 `json:"promise_number"`
	PromisePriority     uint32 `json:"promise_priority"`
	PromisePID          uint64 `json:"promise_pid"`
}

type workerMessage struct {
	From     uint64 `json:"from"`
	To       uint64 `json:"to"`
	TypeHint string `json:"type_hint"`
	Bytes    []byte `json:"bytes"`
}

type workerDecision struct {
	Node      uint64 `json:"node"`
	Index     uint64 `json:"index"`
	RequestID string `json:"request_id"`
	Origin    uint64 `json:"origin"`
	Value     []byte `json:"value"`
}

type adapterSnapshot struct {
	LogicalTime uint64                            `json:"logical_time"`
	YieldSeq    uint64                            `json:"yield_sequence"`
	ItemSeq     uint64                            `json:"item_sequence"`
	Nodes       []workerNode                      `json:"nodes"`
	Pulses      map[control.NodeID]control.ItemID `json:"pulse_bindings"`
}

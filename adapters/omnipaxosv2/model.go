package omnipaxosv2

import "github.com/SuzumiyaHaruki/consensus-atlas/internal/control"

const (
	adapterID      = "omnipaxos-v2alpha1"
	implementation = "crates.io/omnipaxos@0.2.2"
	workerSchema   = "consensus-atlas/omnipaxos-worker/v3"
	evidenceSchema = "consensus-atlas/omnipaxos-v2-evidence/v2"
	messageSchema  = "consensus-atlas/omnipaxos-v2-message/v1"
	callbackSchema = "consensus-atlas/omnipaxos-v2-callback/v1"
	inputSchema    = "consensus-atlas/omnipaxos-v2-input/v1"
	resultSchema   = "consensus-atlas/omnipaxos-v2-client-result/v1"
)

var nodeNames = map[uint64]control.NodeID{1: "n1", 2: "n2", 3: "n3"}

var messageTypeHints = []string{
	"ble/heartbeat-reply",
	"ble/heartbeat-request",
	"sequence-paxos/accept-decide",
	"sequence-paxos/accept-stop-sign",
	"sequence-paxos/accept-sync",
	"sequence-paxos/accepted",
	"sequence-paxos/compaction-snapshot",
	"sequence-paxos/compaction-trim",
	"sequence-paxos/decide",
	"sequence-paxos/forward-stop-sign",
	"sequence-paxos/not-accepted",
	"sequence-paxos/prepare",
	"sequence-paxos/prepare-req",
	"sequence-paxos/promise",
	"sequence-paxos/proposal-forward",
}

var messageMetadataKeys = []string{
	"accepted_index",
	"ballot_config_id",
	"ballot_number",
	"ballot_pid",
	"ballot_priority",
	"decided_index",
	"entry_count",
	"heartbeat_round",
	"request_id",
	"sequence_counter",
	"sequence_session",
	"sync_index",
}

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
	ID                  uint64                 `json:"id"`
	Leader              uint64                 `json:"leader"`
	DecidedIndex        uint64                 `json:"decided_index"`
	DecidedPrefixDigest string                 `json:"decided_prefix_digest"`
	DecidedPrefixes     []workerDecisionPrefix `json:"decided_prefixes"`
	PromiseNumber       uint32                 `json:"promise_number"`
	PromisePriority     uint32                 `json:"promise_priority"`
	PromisePID          uint64                 `json:"promise_pid"`
}

type workerDecisionPrefix struct {
	Index  uint64 `json:"index"`
	Digest string `json:"digest"`
}

type workerMessage struct {
	From     uint64            `json:"from"`
	To       uint64            `json:"to"`
	Family   string            `json:"family"`
	TypeHint string            `json:"type_hint"`
	Metadata map[string]string `json:"metadata"`
	Bytes    []byte            `json:"bytes"`
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

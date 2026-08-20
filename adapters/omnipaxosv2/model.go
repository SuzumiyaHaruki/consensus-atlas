package omnipaxosv2

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	adapterID      = "omnipaxos-v2alpha1"
	implementation = "github.com/haraldng/omnipaxos@e3e989b6bb85762264dafb821b3e5c84c7de36a1"
	workerSchema   = "consensus-atlas/omnipaxos-worker/v5"
	evidenceSchema = "consensus-atlas/omnipaxos-v2-evidence/v2"
	messageSchema  = "consensus-atlas/omnipaxos-v2-message/v1"
	callbackSchema = "consensus-atlas/omnipaxos-v2-callback/v1"
	inputSchema    = "consensus-atlas/omnipaxos-v2-input/v1"
	resultSchema   = "consensus-atlas/omnipaxos-v2-client-result/v1"
)

const (
	DefaultNodeCount = 3
	MaxStaticNodes   = 64
)

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
	WorkerPath string `json:"-"`
	// NodeCount is the conventional static membership n1..nN. Zero means the
	// documented three-node default; there is only one resolved execution path.
	NodeCount int `json:"node_count,omitempty"`
}

func (config Config) ValidateNodeConfiguration() error {
	if config.NodeCount < 0 || config.NodeCount > MaxStaticNodes ||
		config.NodeCount > 0 && config.NodeCount < DefaultNodeCount {
		return fmt.Errorf("OMNIPAXOS_NODE_COUNT_INVALID")
	}
	return nil
}

func (config Config) ResolvedNodeCount() int {
	if config.NodeCount == 0 {
		return DefaultNodeCount
	}
	return config.NodeCount
}

func (config Config) NodeIDs() []control.NodeID {
	count := config.ResolvedNodeCount()
	if config.ValidateNodeConfiguration() != nil {
		return nil
	}
	result := make([]control.NodeID, count)
	for index := range result {
		result[index] = nodeName(uint64(index + 1))
	}
	return result
}

func nodeName(id uint64) control.NodeID {
	if id == 0 || id > MaxStaticNodes {
		return ""
	}
	return control.NodeID("n" + strconv.FormatUint(id, 10))
}

func protocolID(node control.NodeID) (uint64, bool) {
	value := string(node)
	if len(value) < 2 || value[0] != 'n' || strings.HasPrefix(value[1:], "0") {
		return 0, false
	}
	id, err := strconv.ParseUint(value[1:], 10, 64)
	return id, err == nil && id > 0 && id <= MaxStaticNodes && nodeName(id) == node
}

type workerRequest struct {
	ID        uint64 `json:"id"`
	Op        string `json:"op"`
	Node      uint64 `json:"node,omitempty"`
	NodeCount uint64 `json:"node_count,omitempty"`
	Payload   []byte `json:"payload,omitempty"`
}

type workerResponse struct {
	SchemaVersion string               `json:"schema_version"`
	ID            uint64               `json:"id"`
	OK            bool                 `json:"ok"`
	Error         string               `json:"error,omitempty"`
	Configuration *workerConfiguration `json:"configuration,omitempty"`
	Nodes         []workerNode         `json:"nodes"`
	Messages      []workerMessage      `json:"messages"`
	Decisions     []workerDecision     `json:"decisions"`
}

type workerConfiguration struct {
	NodeCount                uint64   `json:"node_count"`
	ElectionTickTimeout      uint64   `json:"election_tick_timeout"`
	ResendMessageTickTimeout uint64   `json:"resend_message_tick_timeout"`
	BufferSize               uint64   `json:"buffer_size"`
	BatchSize                uint64   `json:"batch_size"`
	LeaderPriorities         []uint32 `json:"leader_priorities"`
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

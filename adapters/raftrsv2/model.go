package raftrsv2

import "github.com/SuzumiyaHaruki/consensus-atlas/internal/control"

const (
	adapterID      = "raft-rs-v2alpha1"
	implementation = "crates.io/raft@0.7.0"
	workerSchema   = "consensus-atlas/raft-rs-worker/v1"
	evidenceSchema = "consensus-atlas/raft-rs-v2-evidence/v1"
	readySchema    = "consensus-atlas/raft-rs-v2-ready-effect/v1"
	messageSchema  = "consensus-atlas/raft-rs-v2-message/v1"
	callbackSchema = "consensus-atlas/raft-rs-v2-callback/v1"
	inputSchema    = "consensus-atlas/raft-rs-v2-input/v1"
	proposalSchema = "consensus-atlas/raft-rs-v2-proposal/v1"
	resultSchema   = "consensus-atlas/raft-rs-v2-client-result/v1"

	effectReadyComplete = "raft-rs-ready-complete"
)

var nodeNames = map[uint64]control.NodeID{1: "n1", 2: "n2", 3: "n3"}

type Config struct {
	WorkerPath string
}

type readyRequest struct {
	ReadyID uint64 `json:"ready_id"`
	Digest  string `json:"digest"`
}

type workerRequest struct {
	ID      uint64 `json:"id"`
	Op      string `json:"op"`
	Node    uint64 `json:"node,omitempty"`
	ReadyID uint64 `json:"ready_id,omitempty"`
	Message []byte `json:"message,omitempty"`
	Data    []byte `json:"data,omitempty"`
}

type workerResponse struct {
	SchemaVersion string          `json:"schema_version"`
	ID            uint64          `json:"id"`
	OK            bool            `json:"ok"`
	Error         string          `json:"error,omitempty"`
	Nodes         []workerNode    `json:"nodes"`
	Messages      []workerMessage `json:"messages"`
	Commits       []workerCommit  `json:"commits"`
}

type workerNode struct {
	ID      uint64       `json:"id"`
	Role    string       `json:"role"`
	Term    uint64       `json:"term"`
	Vote    uint64       `json:"vote"`
	Lead    uint64       `json:"lead"`
	Commit  uint64       `json:"commit"`
	Applied uint64       `json:"applied"`
	Ready   *workerReady `json:"ready,omitempty"`
}

type workerReady struct {
	ReadyID uint64 `json:"ready_id"`
	Digest  string `json:"digest"`
}

type workerMessage struct {
	From     uint64 `json:"from"`
	To       uint64 `json:"to"`
	TypeHint string `json:"type_hint"`
	Term     uint64 `json:"term"`
	Index    uint64 `json:"index"`
	Commit   uint64 `json:"commit"`
	Bytes    []byte `json:"bytes"`
}

type workerCommit struct {
	Node  uint64 `json:"node"`
	Index uint64 `json:"index"`
	Term  uint64 `json:"term"`
	Data  []byte `json:"data"`
}

type readyBinding struct {
	Item    control.ItemID `json:"item_id"`
	ReadyID uint64         `json:"ready_id"`
	Digest  string         `json:"digest"`
}

type adapterSnapshot struct {
	LogicalTime uint64                            `json:"logical_time"`
	YieldSeq    uint64                            `json:"yield_sequence"`
	ItemSeq     uint64                            `json:"item_sequence"`
	Nodes       []workerNode                      `json:"nodes"`
	Ready       map[control.NodeID]readyBinding   `json:"ready_bindings"`
	Pulses      map[control.NodeID]control.ItemID `json:"pulse_bindings"`
}

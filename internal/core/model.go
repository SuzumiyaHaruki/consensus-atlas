package core

import "encoding/json"

type EventKind string

const (
	EventStart       EventKind = "start"
	EventCampaign    EventKind = "campaign"
	EventPropose     EventKind = "propose"
	EventMessage     EventKind = "message"
	EventTimeout     EventKind = "timeout"
	EventPersist     EventKind = "persist"
	EventSync        EventKind = "sync"
	EventEmit        EventKind = "emit"
	EventApply       EventKind = "apply"
	EventAcknowledge EventKind = "acknowledge"
	EventCrash       EventKind = "crash"
	EventRestart     EventKind = "restart"
	EventDuplicate   EventKind = "duplicate"
	EventPartition   EventKind = "partition"
	EventHeal        EventKind = "heal"
)

// MessageEnvelope is protocol-neutral transport metadata. Payload is retained
// in the trace so an execution can be replayed without consulting an in-memory
// message table. TypeHint and Metadata are selectors, not protocol semantics.
type MessageEnvelope struct {
	From          string            `json:"from"`
	To            string            `json:"to"`
	SenderEpoch   uint64            `json:"sender_epoch"`
	LinkSequence  uint64            `json:"link_sequence"`
	CausationID   string            `json:"causation_id,omitempty"`
	CloneOf       string            `json:"clone_of,omitempty"`
	TypeHint      string            `json:"type_hint,omitempty"`
	PayloadDigest string            `json:"payload_digest"`
	Payload       []byte            `json:"payload"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type Event struct {
	ID           string           `json:"id"`
	Kind         EventKind        `json:"kind"`
	Source       string           `json:"source,omitempty"`
	Target       string           `json:"target,omitempty"`
	At           uint64           `json:"at"`
	Group        string           `json:"group,omitempty"`
	Dependencies []string         `json:"dependencies,omitempty"`
	Payload      json.RawMessage  `json:"payload,omitempty"`
	Message      *MessageEnvelope `json:"message,omitempty"`
}

type Effect struct {
	Key     string           `json:"key,omitempty"`
	Kind    EventKind        `json:"kind"`
	Source  string           `json:"source,omitempty"`
	Target  string           `json:"target,omitempty"`
	Delay   uint64           `json:"delay,omitempty"`
	Group   string           `json:"group,omitempty"`
	After   []string         `json:"after,omitempty"`
	Payload json.RawMessage  `json:"payload,omitempty"`
	Message *MessageEnvelope `json:"message,omitempty"`
}

type Observation struct {
	Kind     string            `json:"kind"`
	Label    string            `json:"label"`
	Node     string            `json:"node,omitempty"`
	Value    string            `json:"value,omitempty"`
	Evidence map[string]string `json:"evidence,omitempty"`
}

type ApplyStatus string

const (
	StatusApplied ApplyStatus = "applied"
	StatusIgnored ApplyStatus = "ignored"
)

type ApplyResult struct {
	Status       ApplyStatus   `json:"status"`
	Effects      []Effect      `json:"effects,omitempty"`
	Observations []Observation `json:"observations,omitempty"`
	// CancelGroups invalidates volatile work created by an interrupted host
	// batch. The engine records every removed event in the parent trace record.
	CancelGroups []string `json:"cancel_groups,omitempty"`
}

type TraceRecord struct {
	Step         int           `json:"step"`
	LogicalTime  uint64        `json:"logical_time"`
	Event        Event         `json:"event"`
	Outcome      string        `json:"outcome"`
	Before       any           `json:"before,omitempty"`
	After        any           `json:"after,omitempty"`
	Observations []Observation `json:"observations,omitempty"`
	Cancelled    []string      `json:"cancelled,omitempty"`
}

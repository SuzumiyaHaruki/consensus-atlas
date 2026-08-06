package core

import (
	"encoding/json"
	"errors"
	"fmt"
)

type EventKind string

const (
	EventStart EventKind = "start"
	// EventProtocolInput is the sole extensible application/protocol input
	// boundary. Operation identifies a frozen Driver-declared operation; its
	// meaning is never inferred by the Runtime. The older campaign/propose/
	// query/timeout kinds remain readable solely for historical trace replay.
	EventProtocolInput EventKind = "protocol-input"
	EventCampaign      EventKind = "campaign"
	EventPropose       EventKind = "propose"
	EventQuery         EventKind = "query"
	EventMessage       EventKind = "message"
	EventTimeout       EventKind = "timeout"
	EventPersist       EventKind = "persist"
	EventSync          EventKind = "sync"
	EventEmit          EventKind = "emit"
	EventApply         EventKind = "apply"
	EventAcknowledge   EventKind = "acknowledge"
	EventCrash         EventKind = "crash"
	EventRestart       EventKind = "restart"
	EventDuplicate     EventKind = "duplicate"
	EventPartition     EventKind = "partition"
	EventHeal          EventKind = "heal"
	// EventClockAdvance is an engine control record. It is never passed to a
	// protocol adapter; it makes virtual-time progress explicit in a replay
	// trace.
	EventClockAdvance EventKind = "clock-advance"
)

// Timer is a declarative, protocol-neutral request for the Engine to make a
// timeout input available at an absolute logical deadline. A TimerSource owns
// the declaration; the Engine owns queueing, cancellation, and release. In
// particular, reaching Deadline does not execute the timeout automatically.
//
// IDs identify one live timer across successive declarations. A source must
// remove or re-arm an ID after the corresponding timeout is delivered.
type Timer struct {
	ID       string          `json:"id"`
	Target   string          `json:"target"`
	Deadline uint64          `json:"deadline"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// Normalize validates a timer and returns a detached canonical payload. The
// canonical form prevents semantically identical JSON formatting from looking
// like an unintended timer re-arm in a deterministic replay.
func (t Timer) Normalize() (Timer, error) {
	if t.ID == "" {
		return Timer{}, errors.New("timer id is required")
	}
	if t.Target == "" {
		return Timer{}, fmt.Errorf("timer %q target is required", t.ID)
	}
	if len(t.Payload) == 0 {
		return t, nil
	}
	var value any
	if err := json.Unmarshal(t.Payload, &value); err != nil {
		return Timer{}, fmt.Errorf("timer %q payload: %w", t.ID, err)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return Timer{}, fmt.Errorf("timer %q payload: %w", t.ID, err)
	}
	t.Payload = payload
	return t, nil
}

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
	ID   string    `json:"id"`
	Kind EventKind `json:"kind"`
	// Operation is required only for EventProtocolInput. It is an opaque,
	// versioned Driver input ID, not a protocol field interpreted by Engine.
	Operation    string           `json:"operation,omitempty"`
	Source       string           `json:"source,omitempty"`
	Target       string           `json:"target,omitempty"`
	At           uint64           `json:"at"`
	Group        string           `json:"group,omitempty"`
	Dependencies []string         `json:"dependencies,omitempty"`
	Payload      json.RawMessage  `json:"payload,omitempty"`
	Message      *MessageEnvelope `json:"message,omitempty"`
	// TimerID is assigned by the Engine for a declared Timer. Test-plan input
	// cannot set it, so an adapter can distinguish a released native timer from
	// an arbitrary externally injected timeout.
	TimerID string `json:"timer_id,omitempty"`
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

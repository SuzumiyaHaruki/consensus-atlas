package core

import "encoding/json"

type EventKind string

const (
	EventCampaign EventKind = "campaign"
	EventPropose  EventKind = "propose"
	EventMessage  EventKind = "message"
	EventTimeout  EventKind = "timeout"
	EventPersist  EventKind = "persist"
	EventCrash    EventKind = "crash"
	EventRestart  EventKind = "restart"
)

type Event struct {
	ID           string          `json:"id"`
	Kind         EventKind       `json:"kind"`
	Source       string          `json:"source,omitempty"`
	Target       string          `json:"target,omitempty"`
	At           uint64          `json:"at"`
	Dependencies []string        `json:"dependencies,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
}

type Effect struct {
	Key     string          `json:"key,omitempty"`
	Kind    EventKind       `json:"kind"`
	Source  string          `json:"source,omitempty"`
	Target  string          `json:"target,omitempty"`
	Delay   uint64          `json:"delay,omitempty"`
	After   []string        `json:"after,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
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
}

type TraceRecord struct {
	Step         int           `json:"step"`
	LogicalTime  uint64        `json:"logical_time"`
	Event        Event         `json:"event"`
	Outcome      string        `json:"outcome"`
	Before       any           `json:"before,omitempty"`
	After        any           `json:"after,omitempty"`
	Observations []Observation `json:"observations,omitempty"`
}

package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

type CanonicalEvent struct {
	Index        int                   `json:"index"`
	Kind         core.EventKind        `json:"kind"`
	Source       string                `json:"source,omitempty"`
	Target       string                `json:"target,omitempty"`
	At           uint64                `json:"at"`
	Group        string                `json:"group,omitempty"`
	Dependencies []int                 `json:"dependencies,omitempty"`
	Payload      json.RawMessage       `json:"payload,omitempty"`
	Message      *core.MessageEnvelope `json:"message,omitempty"`
	Outcome      string                `json:"outcome"`
	Observations []core.Observation    `json:"observations,omitempty"`
}

// StructuralCanonicalize is deliberately conservative: it renames node IDs by
// first appearance and event IDs by execution order, but retains payloads. A
// protocol PSS must provide any stronger term/log/value normalization.
func StructuralCanonicalize(trace []core.TraceRecord) []CanonicalEvent {
	nodeAliases := make(map[string]string)
	eventIndexes := make(map[string]int, len(trace))
	alias := func(node string) string {
		if node == "" {
			return ""
		}
		if current, ok := nodeAliases[node]; ok {
			return current
		}
		current := "n" + itoa(len(nodeAliases)+1)
		nodeAliases[node] = current
		return current
	}

	out := make([]CanonicalEvent, 0, len(trace))
	for index, record := range trace {
		eventIndexes[record.Event.ID] = index + 1
		observations := append([]core.Observation(nil), record.Observations...)
		for i := range observations {
			observations[i].Node = alias(observations[i].Node)
		}
		canonical := CanonicalEvent{
			Index:        index + 1,
			Kind:         record.Event.Kind,
			Source:       alias(record.Event.Source),
			Target:       alias(record.Event.Target),
			At:           record.Event.At,
			Group:        record.Event.Group,
			Payload:      append([]byte(nil), record.Event.Payload...),
			Message:      canonicalMessage(record.Event.Message, alias),
			Outcome:      record.Outcome,
			Observations: observations,
		}
		for _, dependency := range record.Event.Dependencies {
			canonical.Dependencies = append(canonical.Dependencies, eventIndexes[dependency])
		}
		out = append(out, canonical)
	}
	return out
}

func canonicalMessage(message *core.MessageEnvelope, alias func(string) string) *core.MessageEnvelope {
	if message == nil {
		return nil
	}
	cloned := *message
	// Raw event IDs and physical link sequence are replay evidence, not part of
	// the protocol scenario class.
	cloned.CausationID = ""
	cloned.CloneOf = ""
	cloned.LinkSequence = 0
	cloned.From = alias(message.From)
	cloned.To = alias(message.To)
	cloned.Payload = append([]byte(nil), message.Payload...)
	if message.Metadata != nil {
		cloned.Metadata = make(map[string]string, len(message.Metadata))
		for key, value := range message.Metadata {
			cloned.Metadata[key] = value
		}
	}
	return &cloned
}

// CanonicalFingerprint identifies the current conservative structural scenario
// class. It is not used to prove byte-for-byte deterministic replay.
func CanonicalFingerprint(trace []core.TraceRecord) (string, error) {
	encoded, err := json.Marshal(StructuralCanonicalize(trace))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// ExecutionFingerprint includes raw events and before/after snapshots. Two
// replay runs must have this fingerprint in common before Replay evidence is
// granted to coverage atoms.
func ExecutionFingerprint(trace []core.TraceRecord) (string, error) {
	encoded, err := json.Marshal(trace)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func itoa(value int) string {
	if value < 10 {
		return string(rune('0' + value))
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

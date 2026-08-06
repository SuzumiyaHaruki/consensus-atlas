package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

// CanonicalTraceDigest returns the identity used for persisted traces. The
// JSON round trip with UseNumber makes a trace produced in memory and the same
// trace decoded from a Campaign artifact hash identically.
func CanonicalTraceDigest(trace []TraceRecord) (string, error) {
	encoded, err := json.Marshal(trace)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(encoded)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// UnmarshalJSON preserves numbers stored in the open Before/After snapshots.
// The default decoder converts them to float64, which can change both value
// and trace identity after persistence.
func (record *TraceRecord) UnmarshalJSON(data []byte) error {
	var wire struct {
		Step         int             `json:"step"`
		LogicalTime  uint64          `json:"logical_time"`
		Event        Event           `json:"event"`
		Outcome      string          `json:"outcome"`
		Before       json.RawMessage `json:"before"`
		After        json.RawMessage `json:"after"`
		Observations []Observation   `json:"observations"`
		Cancelled    []string        `json:"cancelled"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return err
	}
	before, err := decodeOpenJSON(wire.Before)
	if err != nil {
		return err
	}
	after, err := decodeOpenJSON(wire.After)
	if err != nil {
		return err
	}
	*record = TraceRecord{
		Step: wire.Step, LogicalTime: wire.LogicalTime, Event: wire.Event,
		Outcome: wire.Outcome, Before: before, After: after,
		Observations: wire.Observations, Cancelled: wire.Cancelled,
	}
	return nil
}

func canonicalJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func decodeOpenJSON(data json.RawMessage) (any, error) {
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

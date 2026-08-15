package omnipaxosv2

import (
	"encoding/json"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type Input struct {
	RequestID string `json:"request_id"`
	Value     []byte `json:"value"`
}

func InputPayload(input Input) (control.PayloadEnvelope, error) {
	if input.RequestID == "" {
		return control.PayloadEnvelope{}, errors.New("OMNIPAXOS_INPUT_REQUEST_ID_REQUIRED")
	}
	return control.NewJSONPayload(inputSchema, input)
}

func decodeInput(payload control.PayloadEnvelope) (Input, error) {
	if err := payload.Validate(); err != nil {
		return Input{}, err
	}
	if payload.SchemaVersion != inputSchema || payload.Encoding != "json" {
		return Input{}, errors.New("OMNIPAXOS_INPUT_SCHEMA_MISMATCH")
	}
	var input Input
	if err := json.Unmarshal(payload.Bytes, &input); err != nil {
		return Input{}, err
	}
	if input.RequestID == "" {
		return Input{}, errors.New("OMNIPAXOS_INPUT_REQUEST_ID_REQUIRED")
	}
	return input, nil
}

// ProjectInput exposes only the Adapter-owned workload identity needed by
// semantic projection. Generic packages continue to treat the payload as
// opaque.
func ProjectInput(payload control.PayloadEnvelope) (Input, error) {
	input, err := decodeInput(payload)
	if err != nil {
		return Input{}, err
	}
	input.Value = append([]byte(nil), input.Value...)
	return input, nil
}

func encodeEntry(input Input, origin uint64) ([]byte, error) {
	return json.Marshal(struct {
		RequestID string `json:"request_id"`
		Origin    uint64 `json:"origin"`
		Value     []byte `json:"value"`
	}{input.RequestID, origin, input.Value})
}

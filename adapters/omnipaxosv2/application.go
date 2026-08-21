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

// ClientResult is the target-owned projection of a worker decision carried
// by a generic client response. Node and Origin remain protocol evidence;
// generic execution code continues to treat the response payload as opaque.
type ClientResult struct {
	Node      control.NodeID `json:"node"`
	Index     uint64         `json:"index"`
	RequestID string         `json:"request_id"`
	Origin    control.NodeID `json:"origin"`
	Value     []byte         `json:"value"`
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

// ProjectClientResult validates both the target-local decision payload and
// its generic response envelope. The Adapter only returns a client result at
// the request origin after that node observes the entry as decided.
func ProjectClientResult(response control.ClientResponse) (ClientResult, error) {
	if err := response.Payload.Validate(); err != nil {
		return ClientResult{}, err
	}
	if response.Payload.SchemaVersion != resultSchema || response.Payload.Encoding != "json" {
		return ClientResult{}, errors.New("OMNIPAXOS_CLIENT_RESULT_SCHEMA_MISMATCH")
	}
	var decision workerDecision
	if err := json.Unmarshal(response.Payload.Bytes, &decision); err != nil {
		return ClientResult{}, err
	}
	node, origin := nodeName(decision.Node), nodeName(decision.Origin)
	if node == "" || origin == "" || decision.Index == 0 || decision.RequestID == "" ||
		node != origin || response.Status != "decided" || response.RequestID != decision.RequestID ||
		response.Owner.Validate() != nil || response.Owner.Node != origin {
		return ClientResult{}, errors.New("OMNIPAXOS_CLIENT_RESULT_IDENTITY_MISMATCH")
	}
	result := ClientResult{
		Node: node, Index: decision.Index, RequestID: decision.RequestID,
		Origin: origin, Value: append([]byte(nil), decision.Value...),
	}
	return result, nil
}

func encodeEntry(input Input, origin uint64) ([]byte, error) {
	return json.Marshal(struct {
		RequestID string `json:"request_id"`
		Origin    uint64 `json:"origin"`
		Value     []byte `json:"value"`
	}{input.RequestID, origin, input.Value})
}

package control

import (
	"encoding/json"
)

const AdapterCommandPayloadSchema = "consensus-atlas/adapter-command/v1"

// AdapterCommandEnvelope is the protocol-neutral Runtime-to-Adapter wire body.
// Protocol-specific payloads remain opaque inside Parameters and Item.
type AdapterCommandEnvelope struct {
	LogicalTime uint64          `json:"logical_time"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Item        *ProducedItem   `json:"item,omitempty"`
}

type AdapterInvokeParameters struct {
	Input PayloadEnvelope `json:"input"`
}

type AdapterResultParameters struct {
	Result string `json:"result"`
}

type AdapterModeParameters struct {
	Mode string `json:"mode"`
}

func NewAdapterCommandPayload(
	logicalTime uint64,
	parameters json.RawMessage,
	item *ProducedItem,
) (PayloadEnvelope, error) {
	encoded, err := json.Marshal(AdapterCommandEnvelope{
		LogicalTime: logicalTime, Parameters: parameters, Item: item,
	})
	if err != nil {
		return PayloadEnvelope{}, err
	}
	return NewPayload(AdapterCommandPayloadSchema, "json", encoded)
}

func DecodeAdapterCommand(command AdapterCommand) (AdapterCommandEnvelope, error) {
	if command.Payload.SchemaVersion != AdapterCommandPayloadSchema || command.Payload.Encoding != "json" {
		return AdapterCommandEnvelope{}, invalid(
			"ADAPTER_COMMAND_SCHEMA_MISMATCH", "payload", command.Payload.SchemaVersion+"/"+command.Payload.Encoding,
		)
	}
	if err := command.Payload.Validate(); err != nil {
		return AdapterCommandEnvelope{}, err
	}
	var envelope AdapterCommandEnvelope
	if err := json.Unmarshal(command.Payload.Bytes, &envelope); err != nil {
		return AdapterCommandEnvelope{}, err
	}
	return envelope, nil
}

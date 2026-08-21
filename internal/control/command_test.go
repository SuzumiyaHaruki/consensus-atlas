package control_test

import (
	"encoding/json"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestAdapterCommandPayloadPreservesWireShape(t *testing.T) {
	parameters := json.RawMessage(`{"mode":"power-loss"}`)
	payload, err := control.NewAdapterCommandPayload(7, parameters, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(payload.Bytes), `{"logical_time":7,"parameters":{"mode":"power-loss"}}`; got != want {
		t.Fatalf("command payload bytes = %s, want %s", got, want)
	}
	envelope, err := control.DecodeAdapterCommand(control.AdapterCommand{Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.LogicalTime != 7 || string(envelope.Parameters) != string(parameters) || envelope.Item != nil {
		t.Fatalf("decoded envelope = %#v", envelope)
	}
}

func TestDecodeAdapterCommandRejectsWrongWireContract(t *testing.T) {
	payload, err := control.NewJSONPayload("other-command/v1", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = control.DecodeAdapterCommand(control.AdapterCommand{Payload: payload})
	if code := control.ValidationCode(err); code != "ADAPTER_COMMAND_SCHEMA_MISMATCH" {
		t.Fatalf("DecodeAdapterCommand() code = %q, error = %v", code, err)
	}
}

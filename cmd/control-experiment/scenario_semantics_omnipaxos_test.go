package main

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestOmnipaxosLeafMessageSemanticClasses(t *testing.T) {
	tests := map[string]string{
		"ble/heartbeat-request":           controlexperiment.ConsensusMessageHeartbeat,
		"ble/heartbeat-reply":             controlexperiment.ConsensusMessageHeartbeat,
		"sequence-paxos/proposal-forward": controlexperiment.ConsensusMessageProposal,
		"sequence-paxos/accept-decide":    controlexperiment.ConsensusMessageReplication,
		"sequence-paxos/accepted":         controlexperiment.ConsensusMessageReplication,
		"sequence-paxos/decide":           controlexperiment.ConsensusMessageReplication,
		"sequence-paxos/prepare-req":      controlexperiment.ConsensusMessageRecovery,
		"sequence-paxos/accept-sync":      controlexperiment.ConsensusMessageRecovery,
		"sequence-paxos/prepare":          controlexperiment.ConsensusSemanticUnknown,
		"sequence-paxos/promise":          controlexperiment.ConsensusSemanticUnknown,
	}
	for messageType, want := range tests {
		if got := omnipaxosScenarioMessageClass(messageType); got != want {
			t.Errorf("message class for %q = %q, want %q", messageType, got, want)
		}
	}
}

func TestOmnipaxosScenarioOperationStateOnlyMarksCarryingMessages(t *testing.T) {
	for _, test := range []struct {
		name    string
		message *control.MessageEnvelope
		want    bool
	}{
		{name: "accept-sync", message: &control.MessageEnvelope{
			TypeHint: "sequence-paxos/accept-sync",
			Metadata: map[string]string{"entry_count": "1", "request_id": "request-1"},
		}, want: true},
		{name: "accept-decide", message: &control.MessageEnvelope{
			TypeHint: "sequence-paxos/accept-decide",
			Metadata: map[string]string{"entry_count": "2", "request_id": "request-1"},
		}, want: true},
		{name: "empty-sync", message: &control.MessageEnvelope{
			TypeHint: "sequence-paxos/accept-sync",
			Metadata: map[string]string{"entry_count": "0", "request_id": "request-1"},
		}},
		{name: "heartbeat", message: &control.MessageEnvelope{
			TypeHint: "ble/heartbeat-request", Metadata: map[string]string{"request_id": "request-1"},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := omnipaxosScenarioCarriesOperation(test.message); got != test.want {
				t.Fatalf("operation carrying classification = %t, want %t", got, test.want)
			}
		})
	}
}

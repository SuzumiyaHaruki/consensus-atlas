package main

import (
	"testing"

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

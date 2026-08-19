package omnipaxosv2

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestOperationReplicationRequiresLeafEntriesAndRequestIdentity(t *testing.T) {
	tests := []struct {
		name     string
		message  *control.MessageEnvelope
		expected bool
	}{
		{name: "prepare", message: &control.MessageEnvelope{TypeHint: "sequence-paxos/prepare"}},
		{name: "empty accept sync", message: &control.MessageEnvelope{TypeHint: "sequence-paxos/accept-sync", Metadata: map[string]string{"entry_count": "0", "request_id": "r"}}},
		{name: "missing request", message: &control.MessageEnvelope{TypeHint: "sequence-paxos/accept-decide", Metadata: map[string]string{"entry_count": "1"}}},
		{name: "accept sync", message: &control.MessageEnvelope{TypeHint: "sequence-paxos/accept-sync", Metadata: map[string]string{"entry_count": "1", "request_id": "r"}}, expected: true},
		{name: "accept decide", message: &control.MessageEnvelope{TypeHint: "sequence-paxos/accept-decide", Metadata: map[string]string{"entry_count": "1", "request_id": "r"}}, expected: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := omnipaxosOperationReplication(test.message); got != test.expected {
				t.Fatalf("operation replication=%v, want %v", got, test.expected)
			}
		})
	}
}

func TestObservedRequestFollowsMessageDependenciesAndRejectsConflict(t *testing.T) {
	messages := map[control.ItemID]omnipaxosObservedMessage{
		"accepted":    {Dependencies: []control.ItemID{"replication"}},
		"replication": {OperationRequestID: "request-r"},
	}
	if got, ok := omnipaxosObservedRequest("accepted", messages); !ok || got != "request-r" {
		t.Fatalf("request=%q/%v", got, ok)
	}
	messages["accepted"] = omnipaxosObservedMessage{Dependencies: []control.ItemID{"replication", "other"}}
	messages["other"] = omnipaxosObservedMessage{OperationRequestID: "request-s"}
	if got, ok := omnipaxosObservedRequest("accepted", messages); ok || got != "" {
		t.Fatalf("conflicting request=%q/%v", got, ok)
	}
}

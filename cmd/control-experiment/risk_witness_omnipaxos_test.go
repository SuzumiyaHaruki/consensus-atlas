package main

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestOmnipaxosMessageLossRiskBindsOperationReplicationToOneRequest(t *testing.T) {
	spec, err := omnipaxosMessageLossWitness()
	if err != nil {
		t.Fatal(err)
	}
	predicates := omnipaxosMessageLossObservationPredicates()
	qualification, err := semantic.QualifyRisk(
		predicates, (omnipaxosv2.ObservationProjector{}).Capabilities(),
		[]control.ActionKind{control.ActionInvoke, control.ActionDropMessage},
	)
	if err != nil || !qualification.Qualified {
		t.Fatalf("request-bound Risk not qualified: %#v/%v", qualification, err)
	}

	tests := []struct {
		name      string
		dropRole  string
		dropID    string
		decision  string
		wantCount int
	}{
		{name: "prepare drop does not instantiate hypothesis", decision: "request-r", wantCount: 1},
		{name: "operation replication reaches", dropRole: omnipaxosv2.ObservationMessageRoleOperationReplication, dropID: "request-r", decision: "request-r", wantCount: 3},
		{name: "different decided request stops after drop", dropRole: omnipaxosv2.ObservationMessageRoleOperationReplication, dropID: "request-r", decision: "request-s", wantCount: 2},
		{name: "pending request remains partial", dropRole: omnipaxosv2.ObservationMessageRoleOperationReplication, dropID: "request-r", wantCount: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := []semantic.Observation{{
				Kind: semantic.ObservationWorkloadInvoked, Step: 1,
				SourceDigest: strings.Repeat("1", 64), ParticipantRole: "coordinator",
				RequestID: "request-r",
			}, {
				Kind: semantic.ObservationMessageDropped, Step: 2,
				SourceDigest: strings.Repeat("2", 64), OperationStage: "inflight",
				MessageRole: test.dropRole, RequestID: test.dropID,
			}}
			if test.decision != "" {
				events = append(events, semantic.Observation{
					Kind: semantic.ObservationDecisionAdvanced, Step: 3,
					SourceDigest: strings.Repeat("3", 64), RequestID: test.decision,
				})
			}
			milestones, matchErr := semantic.MatchLinearRiskWitness(
				spec, predicates, semantic.ObservationHistory{
					ProjectorID: "omnipaxos-risk-fidelity-test",
					TraceDigest: strings.Repeat("4", 64), Events: events,
				},
			)
			if matchErr != nil || len(milestones) != test.wantCount {
				t.Fatalf("milestones=%#v, want %d: %v", milestones, test.wantCount, matchErr)
			}
		})
	}
}

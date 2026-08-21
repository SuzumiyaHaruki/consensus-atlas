package etcdraftv2

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestObservationCapabilitiesRejectActionDependentMessageAndCoordinatorBindings(t *testing.T) {
	cases := map[string][]semantic.ObservationPredicate{
		"episode-1-source-target-confusion": {
			{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped, Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldParticipantNode, BindAs: "follower-node"},
			}},
			{MilestoneID: "deliver", Kind: semantic.ObservationMessageDelivered, Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldParticipantNode, BindAs: "follower-node"},
			}},
		},
		"episode-3-reversed-delivery-roles": {
			{MilestoneID: "append", Kind: semantic.ObservationMessageDelivered, Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldParticipantNode, BindAs: "coordinator-node"},
			}},
			{MilestoneID: "ack", Kind: semantic.ObservationMessageDelivered, Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldParticipantNode, BindAs: "follower-node"},
			}},
		},
		"episode-4-old-new-coordinator-confusion": {
			{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked, Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldParticipantNode, BindAs: "coordinator-node"},
			}},
			{MilestoneID: "change", Kind: semantic.ObservationCoordinatorChange, Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldParticipantNode, BindAs: "coordinator-node"},
			}},
		},
	}
	actions := []control.ActionKind{
		control.ActionInvoke, control.ActionDropMessage, control.ActionDeliverMessage,
	}
	for name, predicates := range cases {
		t.Run(name, func(t *testing.T) {
			qualification, err := semantic.QualifyRisk(
				predicates, (ObservationProjector{}).Capabilities(), actions,
			)
			if err != nil {
				t.Fatal(err)
			}
			if qualification.Qualified {
				t.Fatalf("ambiguous predicates were mechanically executable: %#v", qualification)
			}
			found := false
			for _, issue := range qualification.Issues {
				if issue.Code == semantic.RiskIssueMissingObservationField &&
					issue.Field == semantic.ObservationFieldParticipantNode {
					found = true
				}
			}
			if !found {
				t.Fatalf("ambiguous participant field was not rejected explicitly: %#v", qualification)
			}
		})
	}
}

func TestObservationCapabilitiesAcceptStableMessageAndCoordinatorEndpoints(t *testing.T) {
	predicates := []semantic.ObservationPredicate{
		{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped, Constraints: []semantic.ObservationConstraint{
			{Field: semantic.ObservationFieldMessageSourceNode, BindAs: "follower-node"},
			{Field: semantic.ObservationFieldMessageTargetNode, BindAs: "leader-node"},
		}},
		{MilestoneID: "change", Kind: semantic.ObservationCoordinatorChange, Constraints: []semantic.ObservationConstraint{
			{Field: semantic.ObservationFieldPreviousCoordinatorNode, BindAs: "leader-node"},
			{Field: semantic.ObservationFieldNewCoordinatorNode, BindAs: "new-leader-node"},
		}},
	}
	qualification, err := semantic.QualifyRisk(
		predicates, (ObservationProjector{}).Capabilities(),
		[]control.ActionKind{control.ActionDropMessage},
	)
	if err != nil || !qualification.Qualified {
		t.Fatalf("stable endpoint predicates were rejected: %#v/%v", qualification, err)
	}
}

func TestCoordinatorChangeUsesStableNodeIdentity(t *testing.T) {
	previous := control.NodeRef{Node: "n1", Incarnation: 1}
	restarted := control.NodeRef{Node: "n1", Incarnation: 2}
	replacement := control.NodeRef{Node: "n2", Incarnation: 1}
	if coordinatorNodeChanged(previous, restarted) {
		t.Fatal("incarnation-only restart was reported as a coordinator change")
	}
	if !coordinatorNodeChanged(previous, replacement) {
		t.Fatal("node replacement was not reported as a coordinator change")
	}
}

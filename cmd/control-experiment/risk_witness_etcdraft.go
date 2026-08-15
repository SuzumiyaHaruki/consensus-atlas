package main

import (
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

func etcdraftLeaderChangeObservationPredicates() []semantic.ObservationPredicate {
	return []semantic.ObservationPredicate{
		{
			MilestoneID: raftfamily.MilestoneWorkloadInvokedAtCoordinator,
			Kind:        semantic.ObservationWorkloadInvoked,
			Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldParticipantRole, Equals: "coordinator"},
				{Field: semantic.ObservationFieldParticipantNode, BindAs: "old-coordinator"},
			},
		},
		{
			MilestoneID: raftfamily.MilestoneCoordinatorChangedInflight,
			Kind:        semantic.ObservationCoordinatorChange,
			Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldOperationStage, Equals: "inflight"},
				{Field: semantic.ObservationFieldRelatedNode, BindAs: "old-coordinator"},
				{Field: semantic.ObservationFieldParticipantNode, BindAs: "new-coordinator"},
			},
		},
		{
			MilestoneID: raftfamily.MilestoneOldCoordinatorRestarted,
			Kind:        semantic.ObservationNodeRestarted,
			Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldParticipantNode, BindAs: "old-coordinator"},
			},
		},
	}
}

func projectEtcdraftObservationMilestones(
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) ([]semantic.RiskWitnessMilestoneEvidence, error) {
	history, err := (etcdraftv2.ObservationProjector{}).Project(trace)
	if err != nil {
		return nil, err
	}
	return semantic.MatchLinearRiskWitness(
		spec, etcdraftLeaderChangeObservationPredicates(), history,
	)
}

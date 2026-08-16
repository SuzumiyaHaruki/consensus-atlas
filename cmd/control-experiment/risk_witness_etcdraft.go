package main

import (
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

const etcdraftSemanticPrefixProjectorID = "official-etcdraft-v2-leader-change-prefix-v2"

// etcdraftSemanticPrefixProjector is target-owned composition. The common
// Scenario executor remains unaware of terms, roles and etcd/raft evidence.
type etcdraftSemanticPrefixProjector struct{}

func (etcdraftSemanticPrefixProjector) ID() string {
	return etcdraftSemanticPrefixProjectorID
}

func (etcdraftSemanticPrefixProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	want, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil || spec.Validate() != nil || !reflect.DeepEqual(spec, want) || trace.Validate() != nil {
		return semantic.RiskWitnessResult{}, errors.New("ETCDRAFT_SEMANTIC_PREFIX_PROJECTOR_INPUT_INVALID")
	}
	milestones, err := projectEtcdraftObservationMilestones(spec, trace)
	if err != nil {
		return semantic.RiskWitnessResult{}, err
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, etcdraftSemanticPrefixProjectorID, milestones,
	)
}

var _ controlexperiment.SemanticPrefixProjector = etcdraftSemanticPrefixProjector{}

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

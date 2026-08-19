package main

import (
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	omnipaxosMessageLossRiskID          = "message-loss-before-decision"
	omnipaxosMilestoneWorkloadInvoked   = "workload-invoked-at-coordinator"
	omnipaxosMilestoneMessageDropped    = "consensus-message-dropped-while-inflight"
	omnipaxosMilestoneDecisionAfterDrop = "workload-decided-after-drop"
	omnipaxosScenarioProjectorID        = "omnipaxos-v2-message-loss-prefix-v3"
)

func omnipaxosMessageLossWitness() (semantic.RiskWitnessSpec, error) {
	return semantic.NewRiskWitnessSpec(
		"omnipaxos-message-loss-before-decision-v2", "paxos", omnipaxosMessageLossRiskID,
		[]string{
			omnipaxosMilestoneWorkloadInvoked,
			omnipaxosMilestoneMessageDropped,
			omnipaxosMilestoneDecisionAfterDrop,
		},
		[]semantic.RiskWitnessOrder{
			{Before: omnipaxosMilestoneWorkloadInvoked, After: omnipaxosMilestoneMessageDropped},
			{Before: omnipaxosMilestoneMessageDropped, After: omnipaxosMilestoneDecisionAfterDrop},
		},
	)
}

func omnipaxosMessageLossObservationPredicates() []semantic.ObservationPredicate {
	return []semantic.ObservationPredicate{
		{
			MilestoneID: omnipaxosMilestoneWorkloadInvoked,
			Kind:        semantic.ObservationWorkloadInvoked,
			Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldParticipantRole, Equals: "coordinator"},
				{Field: semantic.ObservationFieldRequestID, BindAs: "request"},
			},
		},
		{
			MilestoneID: omnipaxosMilestoneMessageDropped,
			Kind:        semantic.ObservationMessageDropped,
			Constraints: []semantic.ObservationConstraint{
				{Field: semantic.ObservationFieldMessageRole, Equals: omnipaxosv2.ObservationMessageRoleOperationReplication},
				{Field: semantic.ObservationFieldOperationStage, Equals: "inflight"},
				{Field: semantic.ObservationFieldRequestID, BindAs: "request"},
			},
		},
		{
			MilestoneID: omnipaxosMilestoneDecisionAfterDrop,
			Kind:        semantic.ObservationDecisionAdvanced,
			Constraints: []semantic.ObservationConstraint{{
				Field: semantic.ObservationFieldRequestID, BindAs: "request",
			}},
		},
	}
}

type omnipaxosScenarioProjector struct{}

func (omnipaxosScenarioProjector) ID() string { return omnipaxosScenarioProjectorID }

func (omnipaxosScenarioProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	want, err := omnipaxosMessageLossWitness()
	if err != nil || spec.Validate() != nil || !reflect.DeepEqual(spec, want) || trace.Validate() != nil {
		return semantic.RiskWitnessResult{}, errors.New("OMNIPAXOS_SCENARIO_PROJECTOR_INPUT_INVALID")
	}
	history, err := (omnipaxosv2.ObservationProjector{}).Project(trace)
	if err != nil {
		return semantic.RiskWitnessResult{}, err
	}
	milestones, err := semantic.MatchLinearRiskWitness(
		spec, omnipaxosMessageLossObservationPredicates(), history,
	)
	if err != nil {
		return semantic.RiskWitnessResult{}, err
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, omnipaxosScenarioProjectorID, milestones,
	)
}

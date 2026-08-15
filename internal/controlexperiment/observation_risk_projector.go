package controlexperiment

import (
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type ObservationHistoryProjector interface {
	ID() string
	Project(controlruntime.Trace) (semantic.ObservationHistory, error)
}

// LinearObservationRiskProjector is the protocol-neutral bridge from a
// target's Observation projector to the existing Scenario search interface.
type LinearObservationRiskProjector struct {
	id         string
	spec       semantic.RiskWitnessSpec
	predicates []semantic.ObservationPredicate
	target     ObservationHistoryProjector
}

func NewLinearObservationRiskProjector(
	spec semantic.RiskWitnessSpec,
	predicates []semantic.ObservationPredicate,
	target ObservationHistoryProjector,
) (LinearObservationRiskProjector, error) {
	if spec.Validate() != nil || isNilSemanticComponent(target) || target.ID() == "" ||
		len(predicates) != len(spec.Milestones) {
		return LinearObservationRiskProjector{}, errors.New("EXPERIMENT_LINEAR_OBSERVATION_PROJECTOR_INPUT_INVALID")
	}
	if _, err := semantic.CompileRiskRequirements(predicates); err != nil {
		return LinearObservationRiskProjector{}, err
	}
	for index, predicate := range predicates {
		if predicate.MilestoneID != spec.Milestones[index].ID {
			return LinearObservationRiskProjector{}, errors.New("EXPERIMENT_LINEAR_OBSERVATION_PROJECTOR_MILESTONE_INVALID")
		}
	}
	return LinearObservationRiskProjector{
		id: target.ID() + "/linear-risk", spec: spec,
		predicates: cloneObservationPredicates(predicates), target: target,
	}, nil
}

func (projector LinearObservationRiskProjector) ID() string { return projector.id }

func (projector LinearObservationRiskProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	if projector.id == "" || projector.target == nil || trace.Validate() != nil ||
		spec.Validate() != nil || !reflect.DeepEqual(spec, projector.spec) {
		return semantic.RiskWitnessResult{}, errors.New("EXPERIMENT_LINEAR_OBSERVATION_PROJECTOR_STATE_INVALID")
	}
	history, err := projector.target.Project(trace)
	if err != nil || history.ProjectorID != projector.target.ID() {
		return semantic.RiskWitnessResult{}, errors.New("EXPERIMENT_LINEAR_OBSERVATION_HISTORY_INVALID")
	}
	milestones, err := semantic.MatchLinearRiskWitness(spec, projector.predicates, history)
	if err != nil {
		return semantic.RiskWitnessResult{}, err
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, projector.id, milestones,
	)
}

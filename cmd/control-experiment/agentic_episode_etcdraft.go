package main

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

// newEtcdraftAgenticEpisodeTarget is the second composition of the common
// coordinator. It intentionally contains all etcd/raft-specific construction
// so the Agent orchestration does not learn terms, roles or message types.
func newEtcdraftAgenticEpisodeTarget(
	inputs etcdraftSemanticCalibrationInputs,
) (agenticEpisodeTarget, error) {
	if inputs.knowledge.Validate() != nil || inputs.root.Validate() != nil ||
		inputs.campaign.qualification.Validate() != nil || inputs.experiment.validate() != nil {
		return agenticEpisodeTarget{}, errors.New("ETCDRAFT_AGENTIC_EPISODE_INPUT_INVALID")
	}
	observationProjector := etcdraftv2.ObservationProjector{}
	target := agenticEpisodeTarget{
		ID: "etcdraft-v2", Knowledge: inputs.knowledge,
		Actions:              inputs.campaign.qualification.Manifest.Capabilities.Actions,
		ObservationProjector: observationProjector,
		ScenarioInputs: func(
			risk controlexperiment.ScenarioRiskHypothesis,
			projector controlexperiment.SemanticPrefixProjector,
		) (scenarioEpisodeCoreInputs, error) {
			return scenarioEpisodeCoreInputs{
				Knowledge: risk.Knowledge, Hypothesis: risk.Hypothesis, RiskSpec: risk.Spec,
				Root: inputs.root, Runtime: inputs.experiment.Runtime,
				FaultEnvelope:    inputs.experiment.faultEnvelope(),
				SemanticExposure: inputs.experiment.ScenarioSemanticExposure,
				NewAdapter: func() (control.Adapter, error) {
					return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
				},
				RiskProjector: projector,
				SemanticProjector: func(
					trace controlruntime.Trace,
					frontier controlexperiment.RiskFrontierView,
					snapshot controlruntime.Snapshot,
				) (controlexperiment.ScenarioSemanticExposure, error) {
					return projectEtcdraftScenarioSemantics(
						inputs.experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
					)
				},
			}, nil
		},
		Execute: func(
			ctx context.Context,
			risk controlexperiment.ScenarioRiskHypothesis,
			projector controlexperiment.SemanticPrefixProjector,
			execution controlexperiment.ScenarioExecution,
		) (scenarioTestingResult, error) {
			return executeEtcdraftScenarioQualifiedRisk(ctx, inputs, execution, risk.Spec, projector)
		},
	}
	if target.validate() != nil {
		return agenticEpisodeTarget{}, errors.New("ETCDRAFT_AGENTIC_EPISODE_TARGET_INVALID")
	}
	return target, nil
}

func etcdraftAgenticEpisodeRecoveryBinding() agenticEpisodeRecoveryBinding {
	projector := etcdraftv2.ObservationProjector{}
	return agenticEpisodeRecoveryBinding{
		TargetID: "etcdraft-v2", ObservationProjector: projector,
		Testing: func(
			planID string,
			bundle controlexperiment.ExecutionBundle,
			risk semantic.RiskWitnessResult,
			spec semantic.RiskWitnessSpec,
			riskProjector controlexperiment.SemanticPrefixProjector,
		) (scenarioTestingResult, error) {
			result := newEtcdraftScenarioTestingResult(planID, bundle, risk)
			return result, validateEtcdraftScenarioTestingRisk(result, spec, riskProjector)
		},
	}
}

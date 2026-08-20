package main

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type omnipaxosAgenticEpisodeInputs struct {
	WorkerPath    string
	Knowledge     controlexperiment.ProtocolKnowledgePack
	Experiment    omnipaxosScenarioExperimentConfig
	Workload      controlexperiment.WorkloadPlan
	Root          controlruntime.Trace
	Qualification omnipaxosScenarioQualification
	Preparation   controlexperiment.AgenticPreparationWork
}

func prepareOmnipaxosAgenticEpisode(
	ctx context.Context,
	workerPath string,
	semanticInputPath string,
) (omnipaxosAgenticEpisodeInputs, error) {
	knowledge, experiment, workload, err := loadOmnipaxosAgenticAuthoringSource(semanticInputPath)
	if err != nil {
		return omnipaxosAgenticEpisodeInputs{}, err
	}
	root, err := buildOmnipaxosScenarioRoot(ctx, workerPath, experiment, workload)
	if err != nil {
		return omnipaxosAgenticEpisodeInputs{}, err
	}
	qualification, err := qualifyOmnipaxosScenario(ctx, experiment.adapterConfig(workerPath))
	if err != nil {
		return omnipaxosAgenticEpisodeInputs{}, err
	}
	qualificationWork, err := qualificationPreparationWork(qualification.Bundle)
	if err != nil {
		return omnipaxosAgenticEpisodeInputs{}, err
	}
	return omnipaxosAgenticEpisodeInputs{
		WorkerPath: workerPath, Knowledge: knowledge, Experiment: experiment,
		Workload: workload, Root: root, Qualification: qualification,
		Preparation: controlexperiment.AgenticPreparationWork{
			QualificationReports: len(qualification.Bundle.ConformanceReports),
			QualificationCases:   qualificationCaseCount(qualification.Bundle),
			Qualification:        qualificationWork,
			Root: controlexperiment.WorkLedger{Primary: controlexperiment.PhaseWork{
				SetupAttempts: 1, RuntimeInitializations: 1,
				PrepareActions: 1, SchedulerDecisions: len(root.Records),
				WorkUnits: 2 + len(root.Records),
			}},
		},
	}, nil
}

func omnipaxosAgenticEpisodeRecoveryBinding() agenticEpisodeRecoveryBinding {
	projector := omnipaxosv2.ObservationProjector{}
	return agenticEpisodeRecoveryBinding{
		TargetID: "omnipaxos-v2", ObservationProjector: projector,
		Testing: func(
			planID string,
			bundle controlexperiment.ExecutionBundle,
			risk semantic.RiskWitnessResult,
			spec semantic.RiskWitnessSpec,
			riskProjector controlexperiment.SemanticPrefixProjector,
		) (scenarioTestingResult, error) {
			registry := omnipaxosAgenticOracleRegistry()
			result := newScenarioTestingResult(planID, bundle, risk, registry)
			return result, validateScenarioTestingRisk(
				result, spec, riskProjector, omnipaxosv2.DecisionProjector{}, registry,
			)
		},
	}
}

func newOmnipaxosAgenticEpisodeTarget(
	inputs omnipaxosAgenticEpisodeInputs,
) (agenticEpisodeTarget, error) {
	if inputs.Knowledge.ValidateAgentMaterials() != nil || inputs.Root.Validate() != nil ||
		inputs.Qualification.Bundle.Validate() != nil || inputs.WorkerPath == "" {
		return agenticEpisodeTarget{}, errors.New("OMNIPAXOS_AGENTIC_EPISODE_INPUT_INVALID")
	}
	observationProjector := omnipaxosv2.ObservationProjector{}
	oracleRegistry := omnipaxosAgenticOracleRegistry()
	if oracleRegistry.Validate() != nil {
		return agenticEpisodeTarget{}, errors.New("OMNIPAXOS_AGENTIC_ORACLE_REGISTRY_INVALID")
	}
	actions := []control.ActionKind{
		control.ActionDeliverMessage, control.ActionDropMessage, control.ActionFireTemporal,
		control.ActionInvoke,
	}
	surface, err := controlexperiment.NewAgentTargetSurface(
		"omnipaxos-v2", inputs.Qualification.Bundle.Manifest, inputs.Workload,
		inputs.Experiment.Runtime, inputs.Experiment.FaultEnvelope,
		controlexperiment.AgentTargetExtensions{
			ComposableActions:       actions,
			ObservationCapabilities: observationProjector.Capabilities(),
			OracleCapabilities:      oracleRegistry.Capabilities(),
			FidelityBoundaries: []controlexperiment.AgentFidelityBoundary{{
				ID:                  "single-worker-memory-storage",
				Summary:             "All participants use MemoryStorage in one worker process; durable node restart, partial persistence, and process-isolated recovery are not represented.",
				AffectedPropertyIDs: []string{"recovery-suffix-continuity"},
			}},
		},
	)
	if err != nil {
		return agenticEpisodeTarget{}, err
	}
	target := agenticEpisodeTarget{
		ID: "omnipaxos-v2", Knowledge: inputs.Knowledge, Surface: surface,
		OracleRegistry:              oracleRegistry,
		ObservationProjector:        observationProjector,
		ClosureFactory:              newOmnipaxosScenarioClosureFactory(),
		ClosureSupport:              omnipaxosScenarioClosureSupports,
		ClosureMinimumScenarioCalls: closureScenarioCallLowerBound(len(surface.Nodes)),
		ScenarioInputs: func(
			risk controlexperiment.ScenarioRiskHypothesis,
			projector controlexperiment.SemanticPrefixProjector,
		) (scenarioEpisodeCoreInputs, error) {
			factory := func() (control.Adapter, error) {
				return omnipaxosv2.New(inputs.Experiment.adapterConfig(inputs.WorkerPath))
			}
			return scenarioEpisodeCoreInputs{
				Knowledge: risk.Knowledge, Hypothesis: risk.Hypothesis,
				AcceptedHypothesis: &risk.AcceptedHypothesis, RiskSpec: risk.Spec,
				Root: inputs.Root, Runtime: inputs.Experiment.Runtime,
				FaultEnvelope:         inputs.Experiment.faultEnvelope(),
				SemanticExposure:      inputs.Experiment.ScenarioSemanticExposure,
				SingleStrategicAction: true,
				NewAdapter:            factory, RiskProjector: projector,
				SemanticProjector: func(
					trace controlruntime.Trace,
					frontier controlexperiment.RiskFrontierView,
					snapshot controlruntime.Snapshot,
				) (controlexperiment.ScenarioSemanticExposure, error) {
					return projectOmnipaxosScenarioSemantics(
						inputs.Experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
					)
				},
			}, nil
		},
		Execute: func(
			ctx context.Context,
			risk controlexperiment.ScenarioRiskHypothesis,
			projector controlexperiment.SemanticPrefixProjector,
			execution controlexperiment.ScenarioExecution,
			methodSpecDigest string,
		) (scenarioTestingResult, error) {
			return executeOmnipaxosScenarioQualifiedRisk(
				ctx, inputs.WorkerPath, inputs.Experiment, inputs.Workload, inputs.Qualification,
				inputs.Root, execution, risk.Spec, projector, methodSpecDigest,
			)
		},
	}
	if target.validate() != nil {
		return agenticEpisodeTarget{}, errors.New("OMNIPAXOS_AGENTIC_EPISODE_TARGET_INVALID")
	}
	return target, nil
}

package main

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	etcdqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

type etcdraftAgenticExecutionInputs struct {
	qualification etcdqualification.Bundle
	admission     controlexperiment.ExecutionAdmission
	workload      controlexperiment.WorkloadPlan
	preparation   controlexperiment.AgenticPreparationWork
}

type etcdraftAgenticEpisodeInputs struct {
	execution   etcdraftAgenticExecutionInputs
	root        controlruntime.Trace
	knowledge   controlexperiment.ProtocolKnowledgePack
	experiment  etcdraftAgentExperimentConfig
	client      agentIntentTransport
	preparation controlexperiment.AgenticPreparationWork
}

func prepareEtcdraftAgenticExecutionInputs(
	ctx context.Context,
	workload controlexperiment.WorkloadPlan,
	experiment etcdraftAgentExperimentConfig,
) (etcdraftAgenticExecutionInputs, controlruntime.Trace, error) {
	var empty etcdraftAgenticExecutionInputs
	if workload.Validate() != nil || experiment.validateAgentic() != nil {
		return empty, controlruntime.Trace{}, errors.New("ETCDRAFT_AGENTIC_ROOT_INPUT_INVALID")
	}
	qualification, admission, _, err := etcdraftQualifiedWorkloadWithConfig(
		ctx, experiment.AdapterConfig,
	)
	if err != nil {
		return empty, controlruntime.Trace{}, err
	}
	policy := controlexperiment.Policy{
		Version: controlexperiment.PolicyVersion,
		ID:      "etcdraft-agentic-root-source-v1",
		Priority: []control.ActionKind{
			control.ActionInvoke, control.ActionCompleteEffect,
			control.ActionDeliverMessage, control.ActionFireTemporal,
		},
	}
	config := controlexperiment.Config{
		SchemaVersion:    controlexperiment.SchemaVersionV2,
		ID:               "etcdraft-agentic-root-source",
		PSSID:            etcdraftv2.CorePSSMappingID,
		Runtime:          experiment.Runtime,
		Admission:        &admission,
		FaultEnvelope:    experiment.faultEnvelope(),
		WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		DecisionsPerRun:  64,
		RequireReplay:    true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, Policy: policy, Workload: &workload,
		}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(experiment.AdapterConfig)
	}
	_, source, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
	)
	if err != nil {
		return empty, controlruntime.Trace{}, err
	}
	rootDecisions := 0
	for index, record := range source.Trace.Records {
		if record.Action.Kind == control.ActionInvoke {
			rootDecisions = index + 1
			break
		}
	}
	root, err := controlexperiment.ExecutionTracePrefix(source.Trace, rootDecisions)
	if err != nil || rootDecisions == 0 {
		return empty, controlruntime.Trace{}, errors.New("ETCDRAFT_AGENTIC_ROOT_MILESTONE_MISSING")
	}
	qualificationWork, err := qualificationPreparationWork(qualification)
	if err != nil {
		return empty, controlruntime.Trace{}, err
	}
	return etcdraftAgenticExecutionInputs{
		qualification: qualification, admission: admission, workload: workload,
		preparation: controlexperiment.AgenticPreparationWork{
			QualificationReports: len(qualification.ConformanceReports),
			QualificationCases:   qualificationCaseCount(qualification),
			Qualification:        qualificationWork,
			Root:                 source.Work,
		},
	}, root, nil
}

func prepareEtcdraftAgenticEpisode(
	ctx context.Context,
	corpusPath string,
	semanticInputPath string,
	client agentIntentTransport,
) (etcdraftAgenticEpisodeInputs, error) {
	knowledge, experiment, workload, err := loadEtcdraftAgenticAuthoringSource(semanticInputPath)
	if err != nil {
		return etcdraftAgenticEpisodeInputs{}, err
	}
	if corpusPath != "" {
		return etcdraftAgenticEpisodeInputs{}, errors.New("ETCDRAFT_AGENTIC_EXTERNAL_ROOT_UNSUPPORTED")
	}
	execution, root, err := prepareEtcdraftAgenticExecutionInputs(ctx, workload, experiment)
	if err != nil {
		return etcdraftAgenticEpisodeInputs{}, err
	}
	client, err = configureAgentIntentTransport(
		client, experiment.ModelReasoningEffort, experiment.ModelExcludeReasoning,
		experiment.ModelMaxOutputTokens, experiment.ModelMaxRetries,
	)
	if err != nil || !client.ready() {
		return etcdraftAgenticEpisodeInputs{}, errors.New("ETCDRAFT_AGENTIC_TRANSPORT_CONFIG_INVALID")
	}
	return etcdraftAgenticEpisodeInputs{
		execution: execution, root: root, knowledge: knowledge, experiment: experiment, client: client,
		preparation: execution.preparation,
	}, nil
}

func qualificationCaseCount(bundle conformance.QualificationBundle) int {
	total := 0
	for _, report := range bundle.ConformanceReports {
		total += len(report.Cases)
	}
	return total
}

func qualificationPreparationWork(
	bundle conformance.QualificationBundle,
) (controlexperiment.PhaseWork, error) {
	if bundle.Work == nil || bundle.Work.Validate() != nil {
		return controlexperiment.PhaseWork{}, errors.New("AGENTIC_QUALIFICATION_WORK_MISSING")
	}
	return controlexperiment.PhaseWork{
		SetupAttempts:          bundle.Work.SetupAttempts,
		RuntimeInitializations: bundle.Work.RuntimeInitializations,
		SchedulerDecisions:     bundle.Work.SchedulerDecisions,
		WorkUnits:              bundle.Work.WorkUnits,
	}, nil
}

// newEtcdraftAgenticEpisodeTarget is the second composition of the common
// coordinator. It intentionally contains all etcd/raft-specific construction
// so the Agent orchestration does not learn terms, roles or message types.
func newEtcdraftAgenticEpisodeTarget(
	inputs etcdraftAgenticEpisodeInputs,
) (agenticEpisodeTarget, error) {
	if inputs.knowledge.ValidateAgentMaterials() != nil || inputs.root.Validate() != nil ||
		inputs.execution.qualification.Validate() != nil || inputs.experiment.validateAgentic() != nil {
		return agenticEpisodeTarget{}, errors.New("ETCDRAFT_AGENTIC_EPISODE_INPUT_INVALID")
	}
	observationProjector := etcdraftv2.ObservationProjector{}
	oracleRegistry := etcdraftAgenticOracleRegistry()
	if oracleRegistry.Validate() != nil {
		return agenticEpisodeTarget{}, errors.New("ETCDRAFT_AGENTIC_ORACLE_REGISTRY_INVALID")
	}
	actions := []control.ActionKind{
		control.ActionCompleteEffect, control.ActionCrash, control.ActionDeliverMessage,
		control.ActionDropMessage, control.ActionDuplicateMessage, control.ActionFireTemporal,
		control.ActionHeal, control.ActionInvoke, control.ActionPartition, control.ActionRestart,
	}
	surface, err := controlexperiment.NewAgentTargetSurface(
		"etcdraft-v2", inputs.execution.qualification.Manifest, inputs.execution.workload,
		inputs.experiment.Runtime, inputs.experiment.FaultEnvelope,
		controlexperiment.AgentTargetExtensions{
			ComposableActions:       actions,
			ObservationCapabilities: observationProjector.Capabilities(),
			OracleCapabilities:      oracleRegistry.Capabilities(),
			FidelityBoundaries: []controlexperiment.AgentFidelityBoundary{{
				ID:                  "rawnode-host-persistence-atomicity",
				Summary:             "The in-process RawNode host completes Ready persistence and releases dependent effects atomically; WAL latency, partial writes, fsync failure, and acknowledgement-before-persistence windows are not represented.",
				AffectedPropertyIDs: []string{"client-operation-continuity", "recovery-progress-monotonicity"},
			}},
		},
	)
	if err != nil {
		return agenticEpisodeTarget{}, err
	}
	target := agenticEpisodeTarget{
		ID: "etcdraft-v2", Knowledge: inputs.knowledge, Surface: surface,
		OracleRegistry:              oracleRegistry,
		ObservationProjector:        observationProjector,
		ClosureFactory:              newEtcdraftScenarioClosureFactory(),
		ClosureSupport:              etcdraftScenarioClosureSupports,
		ClosureMinimumScenarioCalls: closureScenarioCallLowerBound(len(surface.Nodes)),
		ScenarioInputs: func(
			risk controlexperiment.ScenarioRiskHypothesis,
			projector controlexperiment.SemanticPrefixProjector,
		) (scenarioEpisodeCoreInputs, error) {
			return scenarioEpisodeCoreInputs{
				Knowledge: risk.Knowledge, Hypothesis: risk.Hypothesis,
				AcceptedHypothesis: &risk.AcceptedHypothesis, RiskSpec: risk.Spec,
				Root: inputs.root, Runtime: inputs.experiment.Runtime,
				FaultEnvelope:         inputs.experiment.faultEnvelope(),
				SemanticExposure:      inputs.experiment.ScenarioSemanticExposure,
				SingleStrategicAction: true,
				NewAdapter: func() (control.Adapter, error) {
					return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
				},
				ActionPreparer: prepareEtcdraftScenarioAction,
				RiskProjector:  projector,
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
			methodSpecDigest string,
		) (scenarioTestingResult, error) {
			return executeEtcdraftScenarioQualifiedRisk(
				ctx, inputs.execution, inputs.root, inputs.experiment,
				execution, risk.Spec, projector, methodSpecDigest,
			)
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
			rootDecisions int,
		) (scenarioTestingResult, error) {
			registry := etcdraftAgenticOracleRegistry()
			result := newScenarioTestingResult(planID, bundle, risk, registry, rootDecisions)
			return result, validateScenarioTestingRisk(
				result, spec, riskProjector, etcdraftv2.DecisionProjector{}, registry,
			)
		},
	}
}

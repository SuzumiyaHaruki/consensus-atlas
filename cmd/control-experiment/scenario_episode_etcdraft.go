package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

type etcdraftScenarioEpisodeResult = scenarioAgentEpisodeResult

type etcdraftScenarioTestingResult = scenarioTestingResult

func runEtcdraftScenarioAgentEpisode(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
	journal *scenarioAgentCallJournal,
	maxCalls int,
	maxPlanSteps int,
	maxDecisions int,
	activateKey func() error,
) (etcdraftScenarioEpisodeResult, error) {
	result, err := runScenarioAgentEpisodeCore(ctx, etcdraftScenarioCoreInputs(inputs),
		journal, maxCalls, maxPlanSteps, maxDecisions, activateKey)
	if err != nil {
		return result, err
	}
	return finishEtcdraftScenarioEpisode(ctx, inputs, result)
}

func runEtcdraftDeterministicScenarioEpisode(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
) (etcdraftScenarioEpisodeResult, error) {
	result, err := runScenarioEpisodeCore(
		ctx, etcdraftScenarioCoreInputs(inputs), inputs.experiment.ScenarioMaxCalls,
		inputs.experiment.ScenarioMaxSteps, inputs.experiment.ScenarioMaxDecisions,
		etcdraftDeterministicScenarioPlanner,
	)
	if err != nil {
		return result, err
	}
	return finishEtcdraftScenarioEpisode(ctx, inputs, result)
}

func etcdraftScenarioCoreInputs(inputs etcdraftSemanticCalibrationInputs) scenarioEpisodeCoreInputs {
	projector := etcdraftSemanticPrefixProjector{}
	return scenarioEpisodeCoreInputs{
		RootID: "invoked-scenario", Knowledge: inputs.knowledge, Hypothesis: inputs.hypothesis,
		RiskSpec: inputs.riskSpec, Root: inputs.root, Runtime: inputs.experiment.Runtime,
		FaultEnvelope:    inputs.experiment.faultEnvelope(),
		SemanticExposure: inputs.experiment.ScenarioSemanticExposure,
		NewAdapter: func() (control.Adapter, error) {
			return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
		},
		RiskProjector: projector,
		SemanticProjector: func(trace controlruntime.Trace, frontier controlexperiment.RiskFrontierView,
			snapshot controlruntime.Snapshot) (controlexperiment.ScenarioSemanticExposure, error) {
			return projectEtcdraftScenarioSemantics(
				inputs.experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
			)
		},
	}
}

func finishEtcdraftScenarioEpisode(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
	result etcdraftScenarioEpisodeResult,
) (etcdraftScenarioEpisodeResult, error) {
	if result.Agent.Status == controlexperiment.ScenarioAgentCompleted {
		testing, err := executeEtcdraftScenarioQualified(ctx, inputs, *result.Agent.Execution)
		if err != nil {
			return etcdraftScenarioEpisodeResult{}, err
		}
		result.Testing = &testing
	}
	return result, nil
}

func etcdraftDeterministicScenarioPlanner(
	_ context.Context,
	view controlexperiment.ScenarioAgentView,
) ([]byte, controlexperiment.ModelWork, error) {
	if view.Semantics.Mode != controlexperiment.ScenarioSemanticExposureFull ||
		view.Semantics.Validate(view.Frontier) != nil {
		return nil, controlexperiment.ModelWork{}, errors.New("ETCDRAFT_A8_BASELINE_SEMANTICS_INVALID")
	}
	wanted := control.ActionCrash
	oldCoordinator := control.NodeID("")
	switch view.Frontier.Progress.FirstMissingMilestone {
	case raftfamily.MilestoneCoordinatorChangedInflight:
	case raftfamily.MilestoneOldCoordinatorRestarted:
		wanted = control.ActionRestart
		if view.Prior == nil {
			return nil, controlexperiment.ModelWork{}, errors.New("ETCDRAFT_A8_BASELINE_PRIOR_REQUIRED")
		}
		for _, step := range view.Prior.Steps {
			if step.Choice != nil && step.Choice.Action.Kind == control.ActionCrash {
				oldCoordinator = step.Choice.Action.Node.Node
				break
			}
		}
		if oldCoordinator == "" {
			return nil, controlexperiment.ModelWork{}, errors.New("ETCDRAFT_A8_BASELINE_PRIOR_INVALID")
		}
	default:
		return nil, controlexperiment.ModelWork{}, errors.New("ETCDRAFT_A8_BASELINE_RISK_UNSUPPORTED")
	}
	selected := -1
	for index, action := range view.Frontier.Actions {
		if action.Kind != wanted || wanted == control.ActionRestart && action.Node.Node != oldCoordinator {
			continue
		}
		if wanted == control.ActionCrash {
			hint := view.Semantics.ActionHints[index]
			if hint.ActorRole != controlexperiment.ConsensusActorLeader ||
				hint.OperationState != controlexperiment.ConsensusOperationInflight {
				continue
			}
		}
		if selected >= 0 {
			return nil, controlexperiment.ModelWork{}, errors.New("ETCDRAFT_A8_BASELINE_ACTION_AMBIGUOUS")
		}
		selected = index
	}
	if selected < 0 {
		return nil, controlexperiment.ModelWork{}, errors.New("ETCDRAFT_A8_BASELINE_ACTION_MISSING")
	}
	ordinal := 1
	if view.Prior != nil {
		ordinal = view.Prior.Attempt + 1
	}
	plan := controlexperiment.ScenarioPlan{
		ID: fmt.Sprintf("etcdraft-a8-baseline-plan-%d", ordinal),
		Steps: []controlexperiment.ScenarioStep{{
			ID: fmt.Sprintf("etcdraft-a8-baseline-step-%d", ordinal),
			Selector: controlexperiment.FrontierActionSelector{
				ActionID: view.Frontier.Actions[selected].ActionID,
			},
		}},
	}
	encoded, err := json.Marshal(plan)
	return encoded, controlexperiment.ModelWork{}, err
}

func executeEtcdraftScenarioQualified(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
	execution controlexperiment.ScenarioExecution,
) (etcdraftScenarioTestingResult, error) {
	spec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		return etcdraftScenarioTestingResult{}, err
	}
	return executeEtcdraftScenarioQualifiedRisk(
		ctx, inputs, execution, spec, etcdraftSemanticPrefixProjector{},
	)
}

func executeEtcdraftScenarioQualifiedRisk(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
	execution controlexperiment.ScenarioExecution,
	spec semantic.RiskWitnessSpec,
	projector controlexperiment.SemanticPrefixProjector,
) (etcdraftScenarioTestingResult, error) {
	if spec.Validate() != nil || projector == nil || execution.FinalRisk.Validate(spec) != nil {
		return etcdraftScenarioTestingResult{}, errors.New("ETCDRAFT_SCENARIO_QUALIFIED_INPUT_INVALID")
	}
	policy, err := controlexperiment.CompileScenarioPolicy(
		"etcdraft-scenario-qualified-policy", inputs.root, execution,
		[]control.ActionKind{
			control.ActionInvoke, control.ActionCompleteEffect,
			control.ActionDeliverMessage, control.ActionFireTemporal,
		},
	)
	if err != nil {
		return etcdraftScenarioTestingResult{}, err
	}
	config := controlexperiment.Config{
		SchemaVersion:    controlexperiment.SchemaVersionV2,
		ID:               "etcdraft-scenario-qualified-testing",
		PSSID:            etcdraftv2.CorePSSMappingID,
		Runtime:          inputs.experiment.Runtime,
		Admission:        &inputs.campaign.admission,
		FaultEnvelope:    inputs.experiment.faultEnvelope(),
		WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		DecisionsPerRun:  len(execution.FinalTrace.Records), RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{Run: 1, Policy: policy, Workload: &inputs.campaign.workload}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	_, bundle, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, inputs.campaign.qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
	)
	if err != nil {
		return etcdraftScenarioTestingResult{}, err
	}
	risk, err := projector.Project(execution.FinalRisk.ID, spec, bundle.Trace)
	if err != nil || bundle.Trace.Digest != execution.FinalTrace.Digest ||
		!reflect.DeepEqual(risk, execution.FinalRisk) ||
		!reflect.DeepEqual(bundle.Qualification, inputs.campaign.qualification) {
		return etcdraftScenarioTestingResult{}, errors.New("ETCDRAFT_SCENARIO_QUALIFIED_TRACE_MISMATCH")
	}
	result := newEtcdraftScenarioTestingResult(execution.PlanID, bundle, risk)
	if err := validateEtcdraftScenarioTestingRisk(result, spec, projector); err != nil {
		return etcdraftScenarioTestingResult{}, err
	}
	return result, nil
}

func newEtcdraftScenarioTestingResult(
	planID string,
	bundle controlexperiment.ExecutionBundle,
	risk semantic.RiskWitnessResult,
) scenarioTestingResult {
	verdict := oracle.CheckBundle(
		bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{}, etcdraftLogProgressMonitor{},
		etcdraftClientApplicationBindingMonitor{},
	)
	outcome := scenarioTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingViolation
	}
	return scenarioTestingResult{
		PlanID: planID, Bundle: bundle, Risk: risk,
		CorePSSSamples: bundle.Run.CorePSSSamples, UniqueCorePSSStates: bundle.Run.UniqueCoreStates,
		Replay: bundle.Run.Replay, Oracle: verdict, Outcome: outcome,
	}
}

func validateEtcdraftScenarioTesting(result scenarioTestingResult) error {
	spec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		return err
	}
	return validateEtcdraftScenarioTestingRisk(result, spec, etcdraftSemanticPrefixProjector{})
}

func validateEtcdraftScenarioTestingRisk(
	result scenarioTestingResult,
	spec semantic.RiskWitnessSpec,
	projector controlexperiment.SemanticPrefixProjector,
) error {
	if spec.Validate() != nil || projector == nil || result.validateExecutionStructure() != nil ||
		result.Bundle.ValidateProjection(etcdraftv2.DecisionProjector{}) != nil {
		return errors.New("ETCDRAFT_SCENARIO_TESTING_EXECUTION_INVALID")
	}
	risk, err := projector.Project(result.Risk.ID, spec, result.Bundle.Trace)
	if err != nil || !reflect.DeepEqual(result.Risk, risk) {
		return errors.New("ETCDRAFT_SCENARIO_TESTING_RISK_INVALID")
	}
	verdict := oracle.CheckBundle(
		result.Bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{},
		etcdraftLogProgressMonitor{}, etcdraftClientApplicationBindingMonitor{},
	)
	outcome := scenarioTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingViolation
	}
	if !reflect.DeepEqual(result.Oracle, verdict) || result.Outcome != outcome {
		return errors.New("ETCDRAFT_SCENARIO_TESTING_ORACLE_INVALID")
	}
	return nil
}

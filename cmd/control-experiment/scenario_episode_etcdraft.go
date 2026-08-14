package main

import (
	"context"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
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
	projector := etcdraftSemanticPrefixProjector{}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	result, err := runScenarioAgentEpisodeCore(ctx, scenarioEpisodeCoreInputs{
		RootID: "invoked-scenario", Knowledge: inputs.knowledge, Hypothesis: inputs.hypothesis,
		RiskSpec: inputs.riskSpec, Root: inputs.root, Runtime: inputs.experiment.Runtime,
		FaultEnvelope:    inputs.experiment.faultEnvelope(),
		SemanticExposure: inputs.experiment.ScenarioSemanticExposure,
		NewAdapter:       factory, RiskProjector: projector,
		SemanticProjector: func(trace controlruntime.Trace, frontier controlexperiment.RiskFrontierView,
			snapshot controlruntime.Snapshot) (controlexperiment.ScenarioSemanticExposure, error) {
			return projectEtcdraftScenarioSemantics(
				inputs.experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
			)
		},
	}, journal, maxCalls, maxPlanSteps, maxDecisions, activateKey)
	if err != nil {
		return result, err
	}
	if result.Agent.Status == controlexperiment.ScenarioAgentCompleted {
		testing, err := executeEtcdraftScenarioQualified(ctx, inputs, *result.Agent.Execution)
		if err != nil {
			return etcdraftScenarioEpisodeResult{}, err
		}
		result.Testing = &testing
	}
	return result, nil
}

func executeEtcdraftScenarioQualified(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
	execution controlexperiment.ScenarioExecution,
) (etcdraftScenarioTestingResult, error) {
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
	risk, err := projectEtcdraftSemanticRisk(
		execution.FinalRisk.ID, inputs.riskSpec, bundle.Trace,
		bundle.ClientHistory, bundle.OperationHistory,
	)
	if err != nil || bundle.Trace.Digest != execution.FinalTrace.Digest ||
		!reflect.DeepEqual(risk, execution.FinalRisk) {
		return etcdraftScenarioTestingResult{}, errors.New("ETCDRAFT_SCENARIO_QUALIFIED_TRACE_MISMATCH")
	}
	verdict := oracle.CheckBundle(
		bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{}, etcdraftLogProgressMonitor{},
		etcdraftClientApplicationBindingMonitor{},
	)
	outcome := scenarioTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingViolation
	}
	result := etcdraftScenarioTestingResult{
		PlanID: execution.PlanID, Bundle: bundle, Risk: risk,
		CorePSSSamples: bundle.Run.CorePSSSamples, UniqueCorePSSStates: bundle.Run.UniqueCoreStates,
		Replay: bundle.Run.Replay, Oracle: verdict, Outcome: outcome,
	}
	if err := validateEtcdraftScenarioTesting(result); err != nil {
		return etcdraftScenarioTestingResult{}, err
	}
	return result, nil
}

func validateEtcdraftScenarioTesting(result scenarioTestingResult) error {
	spec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil || result.validateExecutionStructure() != nil ||
		result.Bundle.ValidateProjection(etcdraftv2.DecisionProjector{}) != nil {
		return errors.New("ETCDRAFT_SCENARIO_TESTING_EXECUTION_INVALID")
	}
	risk, err := projectEtcdraftSemanticRisk(
		result.Risk.ID, spec, result.Bundle.Trace,
		result.Bundle.ClientHistory, result.Bundle.OperationHistory,
	)
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

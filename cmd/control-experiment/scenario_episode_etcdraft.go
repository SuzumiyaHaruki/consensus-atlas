package main

import (
	"context"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

// prepareEtcdraftScenarioAction translates a protocol-neutral partition
// intent into the currently observed leader-versus-peers grouping.
func prepareEtcdraftScenarioAction(
	_ context.Context,
	selector controlexperiment.FrontierActionSelector,
	trace controlruntime.Trace,
	runtime *controlruntime.Runtime,
) (control.ActionID, bool, error) {
	if selector.ActionID != "" || selector.Kind != control.ActionPartition ||
		selector.Node != "" || selector.ItemKind != "" || selector.Owner != "" ||
		selector.MessageSource != "" || selector.MessageTarget != "" ||
		selector.TemporalKind != "" || selector.EffectKind != "" || selector.Durability != "" {
		return "", false, nil
	}
	snapshot := runtime.Snapshot()
	if len(snapshot.Partitions) != 0 {
		return "", false, nil
	}
	route, err := (etcdraftv2.WorkloadRouter{}).Route(
		controlexperiment.TargetSingleCoordinatingMember, latestScenarioEvidence(trace),
	)
	if err != nil {
		return "", false, err
	}
	if len(route.Candidates) != 1 {
		return "", false, nil
	}
	leader := route.Candidates[0]
	peers := make([]control.NodeID, 0, len(snapshot.Nodes)-1)
	for _, node := range snapshot.Nodes {
		if node.Ref.Node != leader {
			peers = append(peers, node.Ref.Node)
		}
	}
	if len(peers) == 0 {
		return "", false, nil
	}
	actionID, err := runtime.OfferPartition([]control.NodeID{leader}, peers)
	if err != nil {
		return "", false, err
	}
	return actionID, true, nil
}

func executeEtcdraftScenarioQualifiedRisk(
	ctx context.Context,
	executionInputs etcdraftAgenticExecutionInputs,
	root controlruntime.Trace,
	experiment etcdraftAgentExperimentConfig,
	execution controlexperiment.ScenarioExecution,
	spec semantic.RiskWitnessSpec,
	projector controlexperiment.SemanticPrefixProjector,
	methodSpecDigest string,
) (scenarioTestingResult, error) {
	if spec.Validate() != nil || projector == nil || execution.FinalRisk.Validate(spec) != nil ||
		methodSpecDigest != "" && !validAgenticSHA256(methodSpecDigest) {
		return scenarioTestingResult{}, errors.New("ETCDRAFT_SCENARIO_QUALIFIED_INPUT_INVALID")
	}
	policy, err := controlexperiment.CompileScenarioPolicy(
		"etcdraft-scenario-qualified-policy", root, execution,
		[]control.ActionKind{
			control.ActionInvoke, control.ActionCompleteEffect,
			control.ActionDeliverMessage, control.ActionFireTemporal,
		},
	)
	if err != nil {
		return scenarioTestingResult{}, err
	}
	config := controlexperiment.Config{
		SchemaVersion:    controlexperiment.SchemaVersionV2,
		ID:               "etcdraft-scenario-qualified-testing",
		PSSID:            etcdraftv2.CorePSSMappingID,
		Runtime:          experiment.Runtime,
		Admission:        &executionInputs.admission,
		FaultEnvelope:    experiment.faultEnvelope(),
		WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		DecisionsPerRun:  len(execution.FinalTrace.Records),
		RequireReplay:    true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, Policy: policy, Workload: &executionInputs.workload,
		}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(experiment.AdapterConfig)
	}
	var bundle controlexperiment.ExecutionBundle
	if methodSpecDigest == "" {
		_, bundle, err = controlexperiment.ExecuteQualifiedBundle(
			ctx, config, executionInputs.qualification, factory, etcdraftv2.CorePSSMapper{},
			etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
		)
	} else {
		_, bundle, err = controlexperiment.ExecuteQualifiedBundleV3(
			ctx, config, executionInputs.qualification, factory, etcdraftv2.CorePSSMapper{},
			etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{}, methodSpecDigest,
		)
	}
	if err != nil {
		return scenarioTestingResult{}, err
	}
	risk, err := projector.Project(execution.FinalRisk.ID, spec, bundle.Trace)
	if err != nil || bundle.Trace.Digest != execution.FinalTrace.Digest ||
		!reflect.DeepEqual(risk, execution.FinalRisk) ||
		!reflect.DeepEqual(bundle.Qualification, executionInputs.qualification) {
		return scenarioTestingResult{}, errors.New("ETCDRAFT_SCENARIO_QUALIFIED_TRACE_MISMATCH")
	}
	result := newEtcdraftScenarioTestingResult(execution.PlanID, bundle, risk)
	if err := validateEtcdraftScenarioTestingRisk(result, spec, projector); err != nil {
		return scenarioTestingResult{}, err
	}
	return result, nil
}

func newEtcdraftScenarioTestingResult(
	planID string,
	bundle controlexperiment.ExecutionBundle,
	risk semantic.RiskWitnessResult,
) scenarioTestingResult {
	registry := etcdraftAgenticOracleRegistry()
	verdict := registry.Check(bundle)
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
	registry := etcdraftAgenticOracleRegistry()
	if registryErr := registry.Validate(); registryErr != nil {
		return registryErr
	}
	verdict := registry.Check(result.Bundle)
	outcome := scenarioTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingViolation
	}
	if !reflect.DeepEqual(result.Oracle, verdict) || result.Outcome != outcome {
		return errors.New("ETCDRAFT_SCENARIO_TESTING_ORACLE_INVALID")
	}
	return nil
}

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	omnipaxosqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/omnipaxosv2"
)

type omnipaxosScenarioInputs struct {
	WorkerPath    string
	Knowledge     controlexperiment.ProtocolKnowledgePack
	Hypothesis    controlexperiment.TestHypothesis
	Experiment    omnipaxosScenarioExperimentConfig
	Workload      controlexperiment.WorkloadPlan
	RiskSpec      semantic.RiskWitnessSpec
	Root          controlruntime.Trace
	Qualification omnipaxosScenarioQualification
}

func prepareOmnipaxosScenario(
	ctx context.Context,
	workerPath string,
	semanticInputPath string,
) (omnipaxosScenarioInputs, error) {
	spec, err := omnipaxosMessageLossWitness()
	if err != nil {
		return omnipaxosScenarioInputs{}, err
	}
	knowledge, hypothesis, experiment, workload, err := loadOmnipaxosScenarioAuthoringSource(
		semanticInputPath, spec,
	)
	if err != nil {
		return omnipaxosScenarioInputs{}, err
	}
	root, err := buildOmnipaxosScenarioRoot(ctx, workerPath, experiment, workload)
	if err != nil {
		return omnipaxosScenarioInputs{}, err
	}
	qualification, err := qualifyOmnipaxosScenario(ctx, workerPath)
	if err != nil {
		return omnipaxosScenarioInputs{}, err
	}
	return omnipaxosScenarioInputs{
		WorkerPath: workerPath, Knowledge: knowledge, Hypothesis: hypothesis,
		Experiment: experiment, Workload: workload, RiskSpec: spec,
		Root: root, Qualification: qualification,
	}, nil
}

func runOmnipaxosScenarioAgentEpisode(
	ctx context.Context,
	inputs omnipaxosScenarioInputs,
	journal *scenarioAgentCallJournal,
	maxCalls int,
	activateKey func() error,
) (scenarioAgentEpisodeResult, error) {
	factory := func() (control.Adapter, error) {
		return omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: inputs.WorkerPath})
	}
	projector := omnipaxosScenarioProjector{}
	result, err := runScenarioAgentEpisodeCore(ctx, scenarioEpisodeCoreInputs{
		RootID:    "omnipaxos-invoked-scenario",
		Knowledge: inputs.Knowledge, Hypothesis: inputs.Hypothesis,
		RiskSpec: inputs.RiskSpec, Root: inputs.Root, Runtime: inputs.Experiment.Runtime,
		FaultEnvelope:    inputs.Experiment.faultEnvelope(),
		SemanticExposure: inputs.Experiment.ScenarioSemanticExposure,
		NewAdapter:       factory, RiskProjector: projector,
		SemanticProjector: func(trace controlruntime.Trace, frontier controlexperiment.RiskFrontierView,
			snapshot controlruntime.Snapshot) (controlexperiment.ScenarioSemanticExposure, error) {
			return projectOmnipaxosScenarioSemantics(
				inputs.Experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
			)
		},
	}, journal, maxCalls, inputs.Experiment.ScenarioMaxSteps,
		inputs.Experiment.ScenarioMaxDecisions, activateKey)
	if err != nil {
		return result, err
	}
	if result.Agent.Status == controlexperiment.ScenarioAgentCompleted {
		testing, err := executeOmnipaxosScenarioQualified(
			ctx, inputs.WorkerPath, inputs.Experiment, inputs.Workload,
			inputs.Qualification, inputs.Root, *result.Agent.Execution,
		)
		if err != nil {
			return scenarioAgentEpisodeResult{}, err
		}
		result.Testing = &testing
	}
	return result, nil
}

func buildOmnipaxosScenarioRoot(
	ctx context.Context,
	workerPath string,
	experiment omnipaxosScenarioExperimentConfig,
	workload controlexperiment.WorkloadPlan,
) (controlruntime.Trace, error) {
	if workerPath == "" || experiment.validate() != nil || workload.Validate() != nil ||
		len(workload.Invocations) != 1 {
		return controlruntime.Trace{}, errors.New("OMNIPAXOS_SCENARIO_ROOT_INPUT_INVALID")
	}
	seed, err := hex.DecodeString(experiment.Runtime.SeedHex)
	if err != nil {
		return controlruntime.Trace{}, err
	}
	adapter, err := omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
	if err != nil {
		return controlruntime.Trace{}, err
	}
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{
		Seed: seed, ClockError: experiment.Runtime.ClockError,
		MaxClones: experiment.Runtime.MaxClones,
	})
	if err != nil {
		return controlruntime.Trace{}, err
	}
	defer runtime.Close()
	router := omnipaxosv2.WorkloadRouter{}
	for decisions := 0; decisions < 256; decisions++ {
		trace, err := runtime.Trace()
		if err != nil {
			return controlruntime.Trace{}, err
		}
		route, err := router.Route(workload.TargetSelector, latestScenarioEvidence(trace))
		if err != nil {
			return controlruntime.Trace{}, err
		}
		if len(route.Candidates) == 1 {
			invoke, err := runtime.OfferInvoke(ctx, route.Candidates[0], workload.Invocations[0].Input)
			if err != nil {
				return controlruntime.Trace{}, err
			}
			if _, err := runtime.Select(ctx, invoke); err != nil {
				return controlruntime.Trace{}, err
			}
			return runtime.Trace()
		}
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return controlruntime.Trace{}, err
		}
		selected, ok := firstOmnipaxosScenarioProgress(actions)
		if !ok {
			return controlruntime.Trace{}, errors.New("OMNIPAXOS_SCENARIO_ROOT_QUIESCENT")
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			return controlruntime.Trace{}, err
		}
	}
	return controlruntime.Trace{}, errors.New("OMNIPAXOS_SCENARIO_ROOT_COORDINATOR_MISSING")
}

func firstOmnipaxosScenarioProgress(actions []control.Action) (control.Action, bool) {
	for _, kind := range []control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal} {
		for _, action := range actions {
			if action.Kind == kind {
				return action, true
			}
		}
	}
	return control.Action{}, false
}

type omnipaxosScenarioQualification struct {
	Bundle    omnipaxosqualification.Bundle
	Admission controlexperiment.ExecutionAdmission
}

func qualifyOmnipaxosScenario(
	ctx context.Context,
	workerPath string,
) (omnipaxosScenarioQualification, error) {
	bundle, err := omnipaxosqualification.Run(ctx, workerPath)
	if err != nil {
		return omnipaxosScenarioQualification{}, err
	}
	admission, err := controlexperiment.BindExecutionAdmission(
		bundle.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: []string{
			conformance.CapabilityStrictYieldEvidence,
			conformance.CapabilityPureEnabledCheck,
			conformance.CapabilityNaturalTemporal,
			conformance.CapabilityRuntimeOwnedMessage,
			conformance.CapabilityStrictDecisionReplay,
			conformance.CapabilityOpaqueInvokeBoundary,
		}},
	)
	if err != nil {
		return omnipaxosScenarioQualification{}, err
	}
	return omnipaxosScenarioQualification{Bundle: bundle, Admission: admission}, nil
}

func executeOmnipaxosScenarioQualified(
	ctx context.Context,
	workerPath string,
	experiment omnipaxosScenarioExperimentConfig,
	workload controlexperiment.WorkloadPlan,
	qualification omnipaxosScenarioQualification,
	root controlruntime.Trace,
	execution controlexperiment.ScenarioExecution,
) (scenarioTestingResult, error) {
	spec, err := omnipaxosMessageLossWitness()
	if workerPath == "" || experiment.validate() != nil || workload.Validate() != nil ||
		qualification.Bundle.Validate() != nil || qualification.Admission.Validate() != nil ||
		qualification.Admission.VerifyQualification(qualification.Bundle.Qualification) != nil ||
		err != nil || root.Validate() != nil || execution.FinalTrace.Validate() != nil ||
		execution.FinalRisk.Validate(spec) != nil ||
		len(execution.Steps) == 0 || len(execution.Steps) > len(execution.FinalTrace.Records) {
		return scenarioTestingResult{}, errors.New("OMNIPAXOS_SCENARIO_QUALIFIED_INPUT_INVALID")
	}
	policy, err := controlexperiment.CompileScenarioPolicy(
		"omnipaxos-scenario-qualified-policy", root, execution,
		[]control.ActionKind{
			control.ActionInvoke, control.ActionDeliverMessage, control.ActionFireTemporal,
		},
	)
	if err != nil {
		return scenarioTestingResult{}, err
	}
	config := controlexperiment.Config{
		SchemaVersion:    controlexperiment.SchemaVersionV2,
		ID:               "omnipaxos-scenario-qualified-testing",
		PSSID:            omnipaxosv2.CorePSSMappingID,
		Runtime:          experiment.Runtime,
		Admission:        &qualification.Admission,
		FaultEnvelope:    experiment.faultEnvelope(),
		WorkloadRouterID: omnipaxosv2.WorkloadRouterID,
		DecisionsPerRun:  len(execution.FinalTrace.Records),
		RequireReplay:    true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, Policy: policy, Workload: &workload,
		}},
	}
	factory := func() (control.Adapter, error) {
		return omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
	}
	_, bundle, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, qualification.Bundle, factory, omnipaxosv2.CorePSSMapper{},
		omnipaxosv2.DecisionProjector{}, omnipaxosv2.WorkloadRouter{},
	)
	if err != nil {
		return scenarioTestingResult{}, err
	}
	risk, err := (omnipaxosScenarioProjector{}).Project(
		execution.FinalRisk.ID, spec, bundle.Trace,
	)
	if err != nil || bundle.Trace.Digest != execution.FinalTrace.Digest ||
		!reflect.DeepEqual(risk, execution.FinalRisk) ||
		!reflect.DeepEqual(bundle.Qualification, qualification.Bundle) {
		return scenarioTestingResult{}, errors.New("OMNIPAXOS_SCENARIO_QUALIFIED_TRACE_MISMATCH")
	}
	verdict := oracle.CheckBundle(bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{})
	outcome := scenarioTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingViolation
	}
	result := scenarioTestingResult{
		PlanID: execution.PlanID, Bundle: bundle, Risk: risk,
		CorePSSSamples:      bundle.Run.CorePSSSamples,
		UniqueCorePSSStates: bundle.Run.UniqueCoreStates,
		Replay:              bundle.Run.Replay, Oracle: verdict, Outcome: outcome,
	}
	if err := validateOmnipaxosScenarioTesting(result); err != nil {
		return scenarioTestingResult{}, err
	}
	return result, nil
}

func validateOmnipaxosScenarioTesting(result scenarioTestingResult) error {
	spec, err := omnipaxosMessageLossWitness()
	if err != nil || result.validateExecutionStructure() != nil ||
		result.Bundle.ValidateProjection(omnipaxosv2.DecisionProjector{}) != nil {
		return errors.New("OMNIPAXOS_SCENARIO_TESTING_EXECUTION_INVALID")
	}
	risk, err := (omnipaxosScenarioProjector{}).Project(
		result.Risk.ID, spec, result.Bundle.Trace,
	)
	if err != nil || !reflect.DeepEqual(result.Risk, risk) {
		return errors.New("OMNIPAXOS_SCENARIO_TESTING_RISK_INVALID")
	}
	verdict := oracle.CheckBundle(
		result.Bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{},
	)
	outcome := scenarioTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingViolation
	}
	if !reflect.DeepEqual(result.Oracle, verdict) || result.Outcome != outcome {
		return errors.New("OMNIPAXOS_SCENARIO_TESTING_ORACLE_INVALID")
	}
	return nil
}

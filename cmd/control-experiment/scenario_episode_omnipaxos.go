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
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	omnipaxosqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/omnipaxosv2"
)

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

func executeOmnipaxosScenarioQualifiedRisk(
	ctx context.Context,
	workerPath string,
	experiment omnipaxosScenarioExperimentConfig,
	workload controlexperiment.WorkloadPlan,
	qualification omnipaxosScenarioQualification,
	root controlruntime.Trace,
	execution controlexperiment.ScenarioExecution,
	spec semantic.RiskWitnessSpec,
	projector controlexperiment.SemanticPrefixProjector,
	methodSpecDigest string,
) (scenarioTestingResult, error) {
	return executeOmnipaxosScenarioQualifiedRiskWithWorkload(
		ctx, workerPath, experiment, &workload, qualification, root, execution,
		spec, projector, methodSpecDigest,
	)
}

func executeOmnipaxosScenarioQualifiedRiskWithWorkload(
	ctx context.Context,
	workerPath string,
	experiment omnipaxosScenarioExperimentConfig,
	workload *controlexperiment.WorkloadPlan,
	qualification omnipaxosScenarioQualification,
	root controlruntime.Trace,
	execution controlexperiment.ScenarioExecution,
	spec semantic.RiskWitnessSpec,
	projector controlexperiment.SemanticPrefixProjector,
	methodSpecDigest string,
) (scenarioTestingResult, error) {
	if workerPath == "" || experiment.validate() != nil ||
		workload != nil && workload.Validate() != nil ||
		qualification.Bundle.Validate() != nil || qualification.Admission.Validate() != nil ||
		qualification.Admission.VerifyQualification(qualification.Bundle.Qualification) != nil ||
		spec.Validate() != nil || projector == nil || root.Validate() != nil || execution.FinalTrace.Validate() != nil ||
		execution.FinalRisk.Validate(spec) != nil ||
		methodSpecDigest != "" && !validAgenticSHA256(methodSpecDigest) ||
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
		SchemaVersion:   controlexperiment.SchemaVersionV2,
		ID:              "omnipaxos-scenario-qualified-testing",
		PSSID:           omnipaxosv2.CorePSSMappingID,
		Runtime:         experiment.Runtime,
		Admission:       &qualification.Admission,
		FaultEnvelope:   experiment.faultEnvelope(),
		DecisionsPerRun: len(execution.FinalTrace.Records),
		RequireReplay:   true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, Policy: policy, Workload: workload,
		}},
	}
	if workload != nil {
		config.WorkloadRouterID = omnipaxosv2.WorkloadRouterID
	}
	factory := func() (control.Adapter, error) {
		return omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
	}
	var bundle controlexperiment.ExecutionBundle
	if methodSpecDigest == "" {
		_, bundle, err = controlexperiment.ExecuteQualifiedBundle(
			ctx, config, qualification.Bundle, factory, omnipaxosv2.CorePSSMapper{},
			omnipaxosv2.DecisionProjector{}, omnipaxosv2.WorkloadRouter{},
		)
	} else {
		_, bundle, err = controlexperiment.ExecuteQualifiedBundleV3(
			ctx, config, qualification.Bundle, factory, omnipaxosv2.CorePSSMapper{},
			omnipaxosv2.DecisionProjector{}, omnipaxosv2.WorkloadRouter{}, methodSpecDigest,
		)
	}
	if err != nil {
		return scenarioTestingResult{}, err
	}
	risk, err := projector.Project(
		execution.FinalRisk.ID, spec, bundle.Trace,
	)
	if err != nil || bundle.Trace.Digest != execution.FinalTrace.Digest ||
		!reflect.DeepEqual(risk, execution.FinalRisk) ||
		!reflect.DeepEqual(bundle.Qualification, qualification.Bundle) {
		return scenarioTestingResult{}, errors.New("OMNIPAXOS_SCENARIO_QUALIFIED_TRACE_MISMATCH")
	}
	result := newOmnipaxosScenarioTestingResult(execution.PlanID, bundle, risk)
	if err := validateOmnipaxosScenarioTestingRisk(result, spec, projector); err != nil {
		return scenarioTestingResult{}, err
	}
	return result, nil
}

func newOmnipaxosScenarioTestingResult(
	planID string,
	bundle controlexperiment.ExecutionBundle,
	risk semantic.RiskWitnessResult,
) scenarioTestingResult {
	registry := omnipaxosAgenticOracleRegistry()
	verdict := registry.Check(bundle)
	outcome := scenarioTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingViolation
	}
	return scenarioTestingResult{
		PlanID: planID, Bundle: bundle, Risk: risk,
		CorePSSSamples:      bundle.Run.CorePSSSamples,
		UniqueCorePSSStates: bundle.Run.UniqueCoreStates,
		Replay:              bundle.Run.Replay, Oracle: verdict, Outcome: outcome,
	}
}

func validateOmnipaxosScenarioTestingRisk(
	result scenarioTestingResult,
	spec semantic.RiskWitnessSpec,
	projector controlexperiment.SemanticPrefixProjector,
) error {
	if spec.Validate() != nil || projector == nil || result.validateExecutionStructure() != nil ||
		result.Bundle.ValidateProjection(omnipaxosv2.DecisionProjector{}) != nil {
		return errors.New("OMNIPAXOS_SCENARIO_TESTING_EXECUTION_INVALID")
	}
	risk, err := projector.Project(
		result.Risk.ID, spec, result.Bundle.Trace,
	)
	if err != nil || !reflect.DeepEqual(result.Risk, risk) {
		return errors.New("OMNIPAXOS_SCENARIO_TESTING_RISK_INVALID")
	}
	registry := omnipaxosAgenticOracleRegistry()
	if registryErr := registry.Validate(); registryErr != nil {
		return registryErr
	}
	verdict := registry.Check(result.Bundle)
	outcome := scenarioTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingViolation
	}
	if !reflect.DeepEqual(result.Oracle, verdict) || result.Outcome != outcome {
		return errors.New("OMNIPAXOS_SCENARIO_TESTING_ORACLE_INVALID")
	}
	return nil
}

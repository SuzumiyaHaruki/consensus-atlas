package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
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

func buildOmnipaxosAgenticBootstrapRoot(
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
	adapter, err := omnipaxosv2.New(experiment.adapterConfig(workerPath))
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
	trace, err := runtime.Trace()
	if err != nil {
		return controlruntime.Trace{}, err
	}
	route, err := (omnipaxosv2.WorkloadRouter{}).Route(
		workload.TargetSelector, latestScenarioEvidence(trace),
	)
	if err != nil || len(route.Candidates) != 0 {
		return controlruntime.Trace{}, errors.New("OMNIPAXOS_SCENARIO_BOOTSTRAP_COORDINATOR_PRESENT")
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return controlruntime.Trace{}, err
	}
	if _, ok := firstOmnipaxosScenarioProgress(actions); !ok {
		return controlruntime.Trace{}, errors.New("OMNIPAXOS_SCENARIO_ROOT_QUIESCENT")
	}
	return trace, nil
}

// buildOmnipaxosScenarioRoot retains the workload-ready root used by the
// narrow public-progress calibration regressions. Active blind experiments select the
// bootstrap mode explicitly and therefore keep coordinator election and Invoke
// inside the Agent-owned path.
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
	adapter, err := omnipaxosv2.New(experiment.adapterConfig(workerPath))
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
		trace, traceErr := runtime.Trace()
		if traceErr != nil {
			return controlruntime.Trace{}, traceErr
		}
		route, routeErr := router.Route(workload.TargetSelector, latestScenarioEvidence(trace))
		if routeErr != nil {
			return controlruntime.Trace{}, routeErr
		}
		if len(route.Candidates) == 1 {
			invoke, offerErr := runtime.OfferInvoke(
				ctx, route.Candidates[0], workload.Invocations[0].Input,
			)
			if offerErr != nil {
				return controlruntime.Trace{}, offerErr
			}
			if _, selectErr := runtime.Select(ctx, invoke); selectErr != nil {
				return controlruntime.Trace{}, selectErr
			}
			return runtime.Trace()
		}
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			return controlruntime.Trace{}, enabledErr
		}
		selected, ok := firstOmnipaxosScenarioProgress(actions)
		if !ok {
			return controlruntime.Trace{}, errors.New("OMNIPAXOS_SCENARIO_ROOT_QUIESCENT")
		}
		if _, selectErr := runtime.Select(ctx, selected.ID); selectErr != nil {
			return controlruntime.Trace{}, selectErr
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

func newOmnipaxosScenarioActionPreparer(
	workload controlexperiment.WorkloadPlan,
) controlexperiment.ScenarioActionPreparer {
	return func(
		ctx context.Context,
		selector controlexperiment.FrontierActionSelector,
		trace controlruntime.Trace,
		runtime *controlruntime.Runtime,
	) (control.ActionID, bool, error) {
		if !scenarioPreparedInvokeSelector(selector) || len(workload.Invocations) != 1 {
			return "", false, nil
		}
		route, err := (omnipaxosv2.WorkloadRouter{}).Route(
			workload.TargetSelector, latestScenarioEvidence(trace),
		)
		if err != nil || len(route.Candidates) != 1 {
			return "", false, err
		}
		actionID, err := runtime.OfferInvoke(
			ctx, route.Candidates[0], workload.Invocations[0].Input,
		)
		return actionID, err == nil, err
	}
}

type omnipaxosScenarioQualification struct {
	Bundle    omnipaxosqualification.Bundle
	Admission controlexperiment.ExecutionAdmission
}

func qualifyOmnipaxosScenario(
	ctx context.Context,
	adapterConfig omnipaxosv2.Config,
) (omnipaxosScenarioQualification, error) {
	bundle, err := omnipaxosqualification.RunWithConfig(ctx, adapterConfig)
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
	policy, err := controlexperiment.RecordedScenarioSchedulePolicy(
		"omnipaxos-scenario-recorded-schedule", root, execution,
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
		return omnipaxosv2.New(experiment.adapterConfig(workerPath))
	}
	var bundle controlexperiment.ExecutionBundle
	if methodSpecDigest == "" {
		_, bundle, err = controlexperiment.ExecuteQualifiedRecordedBundle(
			ctx, config, execution.FinalTrace, qualification.Bundle, factory, omnipaxosv2.CorePSSMapper{},
			omnipaxosv2.DecisionProjector{}, omnipaxosv2.WorkloadRouter{},
		)
	} else {
		_, bundle, err = controlexperiment.ExecuteQualifiedRecordedBundleV3(
			ctx, config, execution.FinalTrace, qualification.Bundle, factory, omnipaxosv2.CorePSSMapper{},
			omnipaxosv2.DecisionProjector{}, omnipaxosv2.WorkloadRouter{}, methodSpecDigest,
		)
	}
	if err != nil {
		return scenarioTestingResult{}, err
	}
	if methodSpecDigest != "" {
		targetConfig, marshalErr := json.Marshal(experiment.AdapterConfig)
		if marshalErr != nil {
			return scenarioTestingResult{}, marshalErr
		}
		bundle, err = bundle.WithExecutionRecipe(controlexperiment.ExecutionRecipe{
			TargetID: "omnipaxos-v2", Config: config, TargetConfig: targetConfig,
		})
		if err != nil {
			return scenarioTestingResult{}, err
		}
	}
	risk, err := projector.Project(
		execution.FinalRisk.ID, spec, bundle.Trace,
	)
	if err != nil || bundle.Trace.Digest != execution.FinalTrace.Digest ||
		!reflect.DeepEqual(risk, execution.FinalRisk) ||
		!reflect.DeepEqual(bundle.Qualification, qualification.Bundle) {
		return scenarioTestingResult{}, errors.New("OMNIPAXOS_SCENARIO_QUALIFIED_TRACE_MISMATCH")
	}
	registry := omnipaxosAgenticOracleRegistry()
	result := newScenarioTestingResult(
		execution.PlanID, bundle, risk, registry,
		controlexperiment.ScenarioAgentAttributionRootDecisions(execution),
	)
	if err := validateScenarioTestingRisk(
		result, spec, projector, omnipaxosv2.DecisionProjector{}, registry,
	); err != nil {
		return scenarioTestingResult{}, err
	}
	return result, nil
}

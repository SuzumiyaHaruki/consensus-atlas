package main

import (
	"context"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	omnipaxosqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/omnipaxosv2"
)

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
	var opened []*omnipaxosv2.Adapter
	factory := func() (control.Adapter, error) {
		adapter, err := omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
		if err == nil {
			opened = append(opened, adapter)
		}
		return adapter, err
	}
	defer func() {
		for _, adapter := range opened {
			_ = adapter.Close()
		}
	}()
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
	return scenarioTestingResult{
		PlanID: execution.PlanID, Bundle: bundle, Risk: risk,
		CorePSSSamples:      bundle.Run.CorePSSSamples,
		UniqueCorePSSStates: bundle.Run.UniqueCoreStates,
		Replay:              bundle.Run.Replay, Oracle: verdict, Outcome: outcome,
	}, nil
}

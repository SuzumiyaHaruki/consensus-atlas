package main

import (
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

const (
	scenarioTestingOracleClean   = "oracle-clean"
	scenarioTestingOracleFinding = "oracle-finding"
)

// scenarioTestingResult is a target-neutral view over the existing Bundle.
// Exact execution, PSS samples, Replay and qualification remain in Bundle.
type scenarioTestingResult struct {
	PlanID              string                            `json:"plan_id"`
	Bundle              controlexperiment.ExecutionBundle `json:"execution_bundle"`
	Risk                semantic.RiskWitnessResult        `json:"risk"`
	CorePSSSamples      int                               `json:"core_pss_samples"`
	UniqueCorePSSStates int                               `json:"unique_core_pss_states"`
	Replay              controlexperiment.ReplayResult    `json:"replay"`
	Oracle              oracle.Result                     `json:"oracle"`
	Outcome             string                            `json:"outcome"`
}

func (result scenarioTestingResult) validateExecutionStructure() error {
	if result.PlanID == "" || result.Bundle.Validate() != nil ||
		result.Risk.TargetIdentityDigest != result.Bundle.Trace.ManifestDigest ||
		result.Risk.ExecutionDigest != result.Bundle.Trace.Digest ||
		result.CorePSSSamples != result.Bundle.Run.CorePSSSamples ||
		result.UniqueCorePSSStates != result.Bundle.Run.UniqueCoreStates ||
		result.Replay != result.Bundle.Run.Replay || !result.Replay.Required || !result.Replay.Stable ||
		(result.Outcome != scenarioTestingOracleClean && result.Outcome != scenarioTestingOracleFinding) {
		return errors.New("SCENARIO_TESTING_EXECUTION_INVALID")
	}
	return nil
}

func newScenarioTestingResult(
	planID string,
	bundle controlexperiment.ExecutionBundle,
	risk semantic.RiskWitnessResult,
	registry targetoracles.Registry,
) scenarioTestingResult {
	verdict := registry.Check(bundle)
	outcome := scenarioTestingOracleClean
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingOracleFinding
	}
	return scenarioTestingResult{
		PlanID: planID, Bundle: bundle, Risk: risk,
		CorePSSSamples: bundle.Run.CorePSSSamples, UniqueCorePSSStates: bundle.Run.UniqueCoreStates,
		Replay: bundle.Run.Replay, Oracle: verdict, Outcome: outcome,
	}
}

func validateScenarioTestingRisk(
	result scenarioTestingResult,
	spec semantic.RiskWitnessSpec,
	projector controlexperiment.SemanticPrefixProjector,
	decisionProjector semantic.DecisionProjector,
	registry targetoracles.Registry,
) error {
	if spec.Validate() != nil || projector == nil || decisionProjector == nil ||
		result.validateExecutionStructure() != nil ||
		result.Bundle.ValidateProjection(decisionProjector) != nil || registry.Validate() != nil ||
		registry.ProjectorID() != decisionProjector.ID() {
		return errors.New("SCENARIO_TESTING_EXECUTION_INVALID")
	}
	risk, err := projector.Project(result.Risk.ID, spec, result.Bundle.Trace)
	if err != nil || !reflect.DeepEqual(result.Risk, risk) {
		return errors.New("SCENARIO_TESTING_RISK_INVALID")
	}
	verdict := registry.Check(result.Bundle)
	outcome := scenarioTestingOracleClean
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingOracleFinding
	}
	if !reflect.DeepEqual(result.Oracle, verdict) || result.Outcome != outcome {
		return errors.New("SCENARIO_TESTING_ORACLE_INVALID")
	}
	return nil
}

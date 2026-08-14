package main

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	scenarioTestingPassed    = "passed"
	scenarioTestingViolation = "violation"
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
		(result.Outcome != scenarioTestingPassed && result.Outcome != scenarioTestingViolation) {
		return errors.New("SCENARIO_TESTING_EXECUTION_INVALID")
	}
	return nil
}

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
	PlanID            string                            `json:"plan_id"`
	Bundle            controlexperiment.ExecutionBundle `json:"execution_bundle"`
	Risk              semantic.RiskWitnessResult        `json:"risk"`
	Replay            controlexperiment.ReplayResult    `json:"replay"`
	Oracle            oracle.Result                     `json:"oracle"`
	OracleAttribution *scenarioOracleAttribution        `json:"oracle_attribution,omitempty"`
	Outcome           string                            `json:"outcome"`
}

// scenarioOracleAttribution stores only the Agent-owned execution boundary.
// Root and post-root findings are derived from the complete Oracle report. This
// is evaluation attribution, not a new Oracle or verdict source.
type scenarioOracleAttribution struct {
	RootDecisions int `json:"root_decisions"`
}

func newScenarioOracleAttribution(
	rootDecisions int,
	traceDecisions int,
) (*scenarioOracleAttribution, error) {
	if rootDecisions < 0 || rootDecisions > traceDecisions {
		return nil, errors.New("SCENARIO_TESTING_ORACLE_BOUNDARY_INVALID")
	}
	return &scenarioOracleAttribution{RootDecisions: rootDecisions}, nil
}

func (attribution *scenarioOracleAttribution) validate(
	traceDecisions int,
) error {
	if attribution == nil {
		// Historical artifacts predate root/post-root attribution. They remain
		// readable, but current executions always construct a non-nil value.
		return nil
	}
	want, err := newScenarioOracleAttribution(attribution.RootDecisions, traceDecisions)
	if err != nil || !reflect.DeepEqual(attribution, want) {
		return errors.New("SCENARIO_TESTING_ORACLE_ATTRIBUTION_INVALID")
	}
	return nil
}

func (result scenarioTestingResult) agentPathOracleViolations() []oracle.Violation {
	if result.OracleAttribution == nil {
		return result.Oracle.Violations
	}
	violations := make([]oracle.Violation, 0, len(result.Oracle.Violations))
	for _, violation := range result.Oracle.Violations {
		if violation.Step > result.OracleAttribution.RootDecisions {
			violations = append(violations, violation)
		}
	}
	return violations
}

func (result scenarioTestingResult) rootPrefixOracleViolations() []oracle.Violation {
	if result.OracleAttribution == nil {
		return nil
	}
	violations := make([]oracle.Violation, 0, len(result.Oracle.Violations))
	for _, violation := range result.Oracle.Violations {
		if violation.Step <= result.OracleAttribution.RootDecisions {
			violations = append(violations, violation)
		}
	}
	return violations
}

func (result scenarioTestingResult) validateExecutionStructure() error {
	if result.PlanID == "" || result.Bundle.Validate() != nil ||
		result.Risk.TargetIdentityDigest != result.Bundle.Trace.ManifestDigest ||
		result.Risk.ExecutionDigest != result.Bundle.Trace.Digest ||
		result.Replay != result.Bundle.Run.Replay || !result.Replay.Required || !result.Replay.Stable ||
		result.OracleAttribution.validate(len(result.Bundle.Trace.Records)) != nil ||
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
	rootDecisions int,
) scenarioTestingResult {
	verdict := registry.Check(bundle)
	attribution, attributionErr := newScenarioOracleAttribution(rootDecisions, len(bundle.Trace.Records))
	if attributionErr != nil {
		attribution = &scenarioOracleAttribution{RootDecisions: -1}
	}
	outcome := scenarioTestingOracleClean
	if len(verdict.Violations) > 0 {
		outcome = scenarioTestingOracleFinding
	}
	return scenarioTestingResult{
		PlanID: planID, Bundle: bundle, Risk: risk,
		Replay: bundle.Run.Replay, Oracle: verdict, OracleAttribution: attribution, Outcome: outcome,
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
	if !reflect.DeepEqual(result.Oracle, verdict) || result.Outcome != outcome ||
		result.OracleAttribution.validate(len(result.Bundle.Trace.Records)) != nil {
		return errors.New("SCENARIO_TESTING_ORACLE_INVALID")
	}
	return nil
}

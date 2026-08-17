package main

import (
	"context"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	agenticEpisodeCompleted       = "completed"
	agenticEpisodeRiskStopped     = "risk-agent-stopped"
	agenticEpisodeScenarioStopped = "scenario-agent-stopped"
	agenticEpisodeTokenStopped    = "model-token-threshold-reached"
	agenticEpisodeExecutionFailed = "execution-failed"

	agenticEvidenceCapabilityGap        = "capability-gap"
	agenticEvidencePlanningFailed       = "planning-failed"
	agenticEvidenceBudgetExhausted      = "search-budget-exhausted"
	agenticEvidenceExecutionFailed      = "execution-failed"
	agenticEvidenceInconclusive         = "inconclusive"
	agenticEvidenceRiskReached          = "risk-reached"
	agenticEvidenceRiskUnverified       = "risk-reached-unverified"
	agenticEvidenceOracleFinding        = "oracle-finding"
	agenticEvidenceHypothesisNotReached = "hypothesis-not-reached"
	agenticEvidenceHypothesisAbandoned  = "hypothesis-abandoned"
)

func addAgentModelWork(total *controlexperiment.ModelWork, value controlexperiment.ModelWork) {
	total.Calls += value.Calls
	total.InputTokens += value.InputTokens
	total.OutputTokens += value.OutputTokens
	total.TotalTokens += value.TotalTokens
}

var errAgenticEpisodeTokenThreshold = errors.New("AGENTIC_EPISODE_MODEL_TOKEN_THRESHOLD_REACHED")

func validAgenticSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}

type agenticEpisodeBudget struct {
	MaxRiskCalls         int                                     `json:"max_risk_calls"`
	MaxScenarioCalls     int                                     `json:"max_scenario_calls"`
	MaxTotalCalls        int                                     `json:"max_total_calls"`
	MaxObservedTokens    int                                     `json:"max_observed_tokens"`
	MaxScenarioPlanSteps int                                     `json:"max_scenario_plan_steps"`
	MaxRuntimeDecisions  int                                     `json:"max_runtime_decisions"`
	Logical              *controlexperiment.AgenticLogicalBudget `json:"logical_budget,omitempty"`
}

func (budget agenticEpisodeBudget) validate() error {
	if budget.MaxRiskCalls <= 0 || budget.MaxRiskCalls > controlexperiment.RiskAgentMaxCalls ||
		budget.MaxScenarioCalls <= 0 || budget.MaxScenarioCalls > controlexperiment.ScenarioAgentMaxCalls ||
		budget.MaxTotalCalls <= 0 || budget.MaxRiskCalls+budget.MaxScenarioCalls > budget.MaxTotalCalls ||
		budget.MaxObservedTokens <= 0 || budget.MaxScenarioPlanSteps <= 0 ||
		budget.MaxScenarioPlanSteps > controlexperiment.ScenarioPlanMaxSteps ||
		budget.MaxRuntimeDecisions <= 0 || budget.MaxRuntimeDecisions > controlexperiment.ScenarioAgentMaxDecisions {
		return errors.New("AGENTIC_EPISODE_BUDGET_INVALID")
	}
	if budget.Logical != nil && (budget.Logical.Validate() != nil ||
		budget.MaxTotalCalls != budget.Logical.MaxModelCalls ||
		budget.MaxObservedTokens != budget.Logical.MaxModelTokens ||
		budget.MaxRuntimeDecisions > budget.Logical.MaxPrimarySchedulerDecisions) {
		return errors.New("AGENTIC_EPISODE_LOGICAL_BUDGET_INVALID")
	}
	return nil
}

type agenticEpisodeMetrics struct {
	CandidateAccepted             bool `json:"candidate_accepted"`
	ExecutedCandidates            int  `json:"executed_candidates"`
	BranchCandidates              int  `json:"branch_candidates"`
	RiskReached                   bool `json:"risk_reached"`
	CorePSSSamples                int  `json:"core_pss_samples"`
	UniquePSSStates               int  `json:"unique_pss_states"`
	ProtocolPSSStates             int  `json:"protocol_pss_states,omitempty"`
	ControlPSSStates              int  `json:"control_pss_states,omitempty"`
	OracleFindings                int  `json:"oracle_findings"`
	CapabilityGapAttempts         int  `json:"capability_gap_attempts,omitempty"`
	RepeatedCapabilityGapAttempts int  `json:"repeated_capability_gap_attempts,omitempty"`
	CapabilityRepairAttempts      int  `json:"capability_repair_attempts,omitempty"`
	CapabilityRepairExecutions    int  `json:"capability_repair_executions,omitempty"`
}

type agenticEpisodeWork struct {
	Model                     controlexperiment.ModelWork        `json:"model"`
	ScenarioFrontier          controlexperiment.PhaseWork        `json:"scenario_frontier"`
	ScenarioSearch            controlexperiment.StatelessDFSWork `json:"scenario_search"`
	QualifiedExecution        controlexperiment.WorkLedger       `json:"qualified_execution"`
	BranchQualifiedExecutions []agenticBranchExecutionWork       `json:"branch_qualified_executions,omitempty"`
}

type agenticBranchExecutionWork struct {
	BranchID string                       `json:"branch_id"`
	Work     controlexperiment.WorkLedger `json:"work"`
}

type agenticBranchTestingResult struct {
	BranchID          string                `json:"branch_id"`
	Intent            string                `json:"intent"`
	ReferenceBranchID string                `json:"reference_branch_id,omitempty"`
	Testing           scenarioTestingResult `json:"testing"`
}

// agenticEvidenceAssessment separates orchestration completion from what the
// produced evidence actually establishes. In particular, an executed scenario
// whose witness was not reached is inconclusive, never a rejected hypothesis.
type agenticEvidenceAssessment struct {
	Status                string   `json:"status"`
	ReasonCode            string   `json:"reason_code,omitempty"`
	PropertyID            string   `json:"property_id,omitempty"`
	EvidenceLevel         string   `json:"evidence_level,omitempty"`
	OracleIDs             []string `json:"oracle_ids,omitempty"`
	FidelityAssessment    string   `json:"fidelity_assessment,omitempty"`
	FidelityBoundaryIDs   []string `json:"fidelity_boundary_ids,omitempty"`
	FirstMissingMilestone string   `json:"first_missing_milestone,omitempty"`
}

type agenticEpisodeResult struct {
	MethodSpecDigest      string                                      `json:"method_spec_digest,omitempty"`
	Status                string                                      `json:"status"`
	RiskAgent             controlexperiment.RiskAgentResult           `json:"risk_agent"`
	RiskProviderCalls     []controlexperiment.StatelessAgentCallAudit `json:"risk_provider_calls"`
	Scenario              *scenarioAgentEpisodeResult                 `json:"scenario,omitempty"`
	ScenarioProviderCalls []controlexperiment.StatelessAgentCallAudit `json:"scenario_provider_calls,omitempty"`
	Failure               *controlexperiment.MethodFailure            `json:"failure,omitempty"`
	Testing               *scenarioTestingResult                      `json:"testing,omitempty"`
	BranchTesting         []agenticBranchTestingResult                `json:"branch_testing,omitempty"`
	Metrics               agenticEpisodeMetrics                       `json:"metrics"`
	Work                  agenticEpisodeWork                          `json:"work"`
	Assessment            agenticEvidenceAssessment                   `json:"evidence_assessment"`
}

// agenticEpisodeObservationProjector is the only semantic surface the common
// coordinator asks a target to expose. The target remains responsible for
// translating its own evidence into these observations.
type agenticEpisodeObservationProjector interface {
	controlexperiment.ObservationHistoryProjector
	Capabilities() []semantic.ObservationCapability
}

// agenticEpisodeTarget is a thin composition boundary, not a protocol model.
// The coordinator owns Agent calls, budgets and phase order. A target supplies
// its runtime composition and qualified testing callback.
type agenticEpisodeTarget struct {
	ID                   string
	MethodSpecDigest     string
	Knowledge            controlexperiment.ProtocolKnowledgePack
	Surface              controlexperiment.AgentTargetSurface
	ObservationProjector agenticEpisodeObservationProjector
	OracleRegistry       agenticOracleRegistry
	ScenarioInputs       func(
		controlexperiment.ScenarioRiskHypothesis,
		controlexperiment.SemanticPrefixProjector,
	) (scenarioEpisodeCoreInputs, error)
	Execute func(
		context.Context,
		controlexperiment.ScenarioRiskHypothesis,
		controlexperiment.SemanticPrefixProjector,
		controlexperiment.ScenarioExecution,
		string,
	) (scenarioTestingResult, error)
}

func (target agenticEpisodeTarget) validate() error {
	actions := target.Surface.Capabilities.ComposableActions
	if strings.TrimSpace(target.ID) == "" || strings.ContainsAny(target.ID, " /\\") ||
		target.MethodSpecDigest != "" && !validAgenticSHA256(target.MethodSpecDigest) ||
		target.Knowledge.ValidateAgentMaterials() != nil || target.ObservationProjector == nil ||
		target.Surface.ValidateAgainstKnowledge(target.Knowledge) != nil ||
		target.Surface.TargetID != target.ID ||
		target.OracleRegistry.validate() != nil ||
		!target.OracleRegistry.MatchesCapabilities(target.Surface.Capabilities.OracleCapabilities) ||
		reflect.ValueOf(target.ObservationProjector).Kind() == reflect.Pointer &&
			reflect.ValueOf(target.ObservationProjector).IsNil() ||
		semantic.ValidateObservationCapabilities(target.ObservationProjector.Capabilities()) != nil ||
		!target.Surface.MatchesPlanningInputs(actions, target.ObservationProjector.Capabilities()) ||
		len(actions) == 0 || target.ScenarioInputs == nil || target.Execute == nil {
		return errors.New("AGENTIC_EPISODE_TARGET_INVALID")
	}
	seen := make(map[control.ActionKind]bool, len(actions))
	for _, action := range actions {
		if action.Validate() != nil || seen[action] {
			return errors.New("AGENTIC_EPISODE_TARGET_ACTIONS_INVALID")
		}
		seen[action] = true
	}
	return nil
}

func runAgenticEpisode(
	ctx context.Context,
	target agenticEpisodeTarget,
	riskJournal *statelessAgentCallJournal,
	scenarioJournal *scenarioAgentCallJournal,
	budget agenticEpisodeBudget,
	memory []controlexperiment.RiskExplorationMemoryEntry,
	knowledgeReader controlexperiment.RiskKnowledgeReader,
	activateRiskKey func() error,
	activateScenarioKey func() error,
) (agenticEpisodeResult, error) {
	result := agenticEpisodeResult{
		MethodSpecDigest: target.MethodSpecDigest, Status: agenticEpisodeRiskStopped,
		Assessment: agenticEvidenceAssessment{
			Status: agenticEvidencePlanningFailed, ReasonCode: "risk-candidate-unavailable",
		},
	}
	if target.validate() != nil || riskJournal == nil || scenarioJournal == nil ||
		scenarioJournal.core == nil || riskJournal == scenarioJournal.core || budget.validate() != nil ||
		activateRiskKey == nil || activateScenarioKey == nil ||
		riskJournal.SetRoot(target.ID+"-risk-agent") != nil {
		return result, errors.New("AGENTIC_EPISODE_INPUT_INVALID")
	}
	capabilities := target.ObservationProjector.Capabilities()
	actions := target.Surface.Capabilities.ComposableActions
	risk, runErr := controlexperiment.DiscoverRiskWithPlanner(
		ctx, controlexperiment.RiskAgentBudget{
			MaxCalls: budget.MaxRiskCalls, MaxTokens: budget.MaxObservedTokens,
		}, target.Knowledge, capabilities, actions, memory, &target.Surface, knowledgeReader,
		func(ctx context.Context, view controlexperiment.RiskAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			content, work, err := planRiskCandidate(ctx, riskJournal, view)
			if !errors.Is(err, errStatelessAgentCallKeyRequired) {
				return content, work, err
			}
			if err := activateRiskKey(); err != nil {
				return nil, work, err
			}
			return planRiskCandidate(ctx, riskJournal, view)
		},
	)
	result.RiskAgent = risk
	riskAudits, auditErr := riskJournal.Audits()
	if auditErr != nil {
		return result, auditErr
	}
	result.RiskProviderCalls = riskAudits
	result.Work.Model = modelWorkFromAgentAudits(result.RiskProviderCalls)
	if runErr != nil {
		return result, runErr
	}
	if risk.Accepted == nil {
		result.Assessment = assessRiskAgentStop(risk)
		return result, nil
	}
	result.Assessment = target.acceptedEvidenceAssessment(*risk.Accepted)
	result.Metrics.CandidateAccepted = true
	if result.Work.Model.TotalTokens >= budget.MaxObservedTokens {
		result.Status = agenticEpisodeTokenStopped
		result.Assessment.Status = agenticEvidenceBudgetExhausted
		result.Assessment.ReasonCode = "model-token-threshold-reached"
		return result, nil
	}
	scenarioRisk, err := controlexperiment.BuildScenarioRiskHypothesis(
		target.Knowledge, *risk.Accepted, capabilities, actions, &target.Surface,
	)
	if err != nil {
		return result, err
	}
	projector, err := controlexperiment.NewLinearObservationRiskProjector(
		scenarioRisk.Spec, scenarioRisk.Predicates, target.ObservationProjector,
	)
	if err != nil {
		return result, err
	}
	coreInputs, err := target.ScenarioInputs(scenarioRisk, projector)
	if err != nil || !reflect.DeepEqual(coreInputs.Knowledge, scenarioRisk.Knowledge) ||
		!reflect.DeepEqual(coreInputs.Hypothesis, scenarioRisk.Hypothesis) ||
		coreInputs.AcceptedHypothesis == nil ||
		!reflect.DeepEqual(*coreInputs.AcceptedHypothesis, scenarioRisk.AcceptedHypothesis) ||
		!reflect.DeepEqual(coreInputs.RiskSpec, scenarioRisk.Spec) || coreInputs.RiskProjector == nil ||
		coreInputs.RiskProjector.ID() != projector.ID() {
		return result, errors.New("AGENTIC_EPISODE_TARGET_SCENARIO_INVALID")
	}
	coreInputs.TargetSurface = &target.Surface
	rootID := target.ID + "-agentic-" + risk.Accepted.Candidate.ID
	coreInputs.RootID = rootID
	if scenarioJournal.SetRoot(rootID) != nil {
		return result, errors.New("AGENTIC_EPISODE_SCENARIO_ROOT_INVALID")
	}
	scenarioObservedTokens := 0
	scenario, scenarioErr := runScenarioEpisodeCore(
		ctx, coreInputs, budget.MaxScenarioCalls, budget.MaxScenarioPlanSteps,
		budget.MaxRuntimeDecisions,
		func(ctx context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			content, work, err := scenarioJournal.Planner(ctx, scenarioRisk.Spec, view)
			if errors.Is(err, errStatelessAgentCallKeyRequired) {
				if err := activateScenarioKey(); err != nil {
					return nil, work, err
				}
				content, work, err = scenarioJournal.Planner(ctx, scenarioRisk.Spec, view)
			}
			if err == nil {
				scenarioObservedTokens += work.TotalTokens
				if result.Work.Model.TotalTokens+scenarioObservedTokens > budget.MaxObservedTokens {
					return nil, work, errAgenticEpisodeTokenThreshold
				}
			}
			return content, work, err
		},
	)
	result.Scenario = &scenario
	result.ScenarioProviderCalls, err = scenarioJournal.Audits()
	if err != nil {
		return result, err
	}
	result.Work.Model = modelWorkFromAgentAudits(result.RiskProviderCalls)
	addAgentModelWork(&result.Work.Model, modelWorkFromAgentAudits(result.ScenarioProviderCalls))
	result.Work.ScenarioFrontier = scenario.FrontierWork
	result.Work.ScenarioSearch = scenario.Agent.ExecutionWork
	if errors.Is(scenarioErr, errAgenticEpisodeTokenThreshold) {
		result.Status = agenticEpisodeTokenStopped
		result.Assessment.Status = agenticEvidenceBudgetExhausted
		result.Assessment.ReasonCode = "model-token-threshold-reached"
		return result, nil
	}
	if scenarioErr != nil {
		if scenario.Failure != nil {
			result.Status = agenticEpisodeExecutionFailed
			result.Assessment.Status = agenticEvidenceExecutionFailed
			result.Assessment.ReasonCode = scenario.Failure.Code
			result.Failure = cloneAgenticEpisodeFailure(scenario.Failure)
			return result, nil
		}
		return result, scenarioErr
	}
	result.BranchTesting, err = executeAgenticBranchCandidates(
		ctx, target, scenarioRisk, projector, scenario.Agent,
	)
	if err != nil {
		return result, err
	}
	for _, branch := range result.BranchTesting {
		result.Work.BranchQualifiedExecutions = append(
			result.Work.BranchQualifiedExecutions,
			agenticBranchExecutionWork{BranchID: branch.BranchID, Work: branch.Testing.Bundle.Work},
		)
	}
	if scenario.Agent.Execution == nil {
		if len(result.BranchTesting) > 0 {
			result.Status = agenticEpisodeCompleted
			result.Metrics, err = agenticEpisodeMetricsFromEvidence(nil, result.BranchTesting)
			if err != nil {
				return result, err
			}
			result.Assessment = assessUnselectedBranchEvidence(
				result.Assessment, result.BranchTesting, scenario.Agent,
			)
			return result, nil
		}
		result.Status = agenticEpisodeScenarioStopped
		switch scenario.Agent.StopReason {
		case controlexperiment.ScenarioAgentStopDecisionBudget:
			result.Assessment.Status = agenticEvidenceBudgetExhausted
			result.Assessment.ReasonCode = "runtime-decision-budget-exhausted"
		case controlexperiment.ScenarioAgentStopFinalSelectionRequired:
			result.Assessment.Status = agenticEvidenceInconclusive
			result.Assessment.ReasonCode = "scenario-final-selection-required"
		case controlexperiment.ScenarioAgentStopHypothesisAbandoned:
			result.Assessment.Status = agenticEvidenceInconclusive
			result.Assessment.ReasonCode = agenticEvidenceHypothesisAbandoned
		case controlexperiment.ScenarioAgentStopCapabilityGap:
			result.Assessment.Status = agenticEvidenceCapabilityGap
			result.Assessment.ReasonCode = lastScenarioCapabilityGapCode(scenario.Agent)
		default:
			result.Assessment.Status = agenticEvidencePlanningFailed
			result.Assessment.ReasonCode = "scenario-planning-failed"
		}
		return result, nil
	}
	testing, err := target.Execute(
		ctx, scenarioRisk, projector, *scenario.Agent.Execution, target.MethodSpecDigest,
	)
	if err != nil {
		return result, err
	}
	result.Testing = &testing
	result.Status = agenticEpisodeCompleted
	result.Metrics, err = agenticEpisodeMetricsFromEvidence(&testing, result.BranchTesting)
	if err != nil {
		return result, err
	}
	result.Work.QualifiedExecution = testing.Bundle.Work
	result.Assessment = assessTestingEvidence(
		result.Assessment, testing, scenario.Agent,
	)
	result.Assessment = overrideWithBranchOracleFinding(result.Assessment, result.BranchTesting)
	return result, nil
}

func lastScenarioCapabilityGapCode(result controlexperiment.ScenarioAgentResult) string {
	for index := len(result.Attempts) - 1; index >= 0; index-- {
		if len(result.Attempts[index].Feedback.CapabilityGaps) > 0 {
			return result.Attempts[index].Feedback.CapabilityGaps[0].Code
		}
	}
	return controlexperiment.ScenarioAgentStopCapabilityGap
}

func executeAgenticBranchCandidates(
	ctx context.Context,
	target agenticEpisodeTarget,
	risk controlexperiment.ScenarioRiskHypothesis,
	projector controlexperiment.SemanticPrefixProjector,
	scenario controlexperiment.ScenarioAgentResult,
) ([]agenticBranchTestingResult, error) {
	seen := make(map[string]bool, len(scenario.CandidateExecutions)+1)
	if scenario.Execution != nil {
		seen[scenario.Execution.FinalTrace.Digest] = true
	}
	result := make([]agenticBranchTestingResult, 0, len(scenario.CandidateExecutions))
	for _, candidate := range scenario.CandidateExecutions {
		digest := candidate.Execution.FinalTrace.Digest
		if digest == "" || seen[digest] {
			continue
		}
		seen[digest] = true
		testing, err := target.Execute(
			ctx, risk, projector, candidate.Execution, target.MethodSpecDigest,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, agenticBranchTestingResult{
			BranchID: candidate.BranchID, Intent: candidate.Intent,
			ReferenceBranchID: candidate.ReferenceBranchID, Testing: testing,
		})
	}
	return result, nil
}

func assessUnselectedBranchEvidence(
	assessment agenticEvidenceAssessment,
	branches []agenticBranchTestingResult,
	scenario controlexperiment.ScenarioAgentResult,
) agenticEvidenceAssessment {
	for _, branch := range branches {
		candidate := assessTestingEvidence(assessment, branch.Testing, scenario)
		if candidate.Status == agenticEvidenceOracleFinding {
			return candidate
		}
	}
	for _, branch := range branches {
		if branch.Testing.Risk.Status == semantic.RiskWitnessReached {
			return assessTestingEvidence(assessment, branch.Testing, scenario)
		}
	}
	assessment.Status = agenticEvidenceInconclusive
	assessment.ReasonCode = "scenario-final-selection-required"
	if scenario.StopReason == controlexperiment.ScenarioAgentStopDecisionBudget {
		assessment.Status = agenticEvidenceBudgetExhausted
		assessment.ReasonCode = "runtime-decision-budget-exhausted"
	}
	return assessment
}

func overrideWithBranchOracleFinding(
	assessment agenticEvidenceAssessment,
	branches []agenticBranchTestingResult,
) agenticEvidenceAssessment {
	for _, branch := range branches {
		if len(branch.Testing.Oracle.Violations) > 0 {
			assessment.Status = agenticEvidenceOracleFinding
			assessment.ReasonCode = branch.Testing.Oracle.Violations[0].Monitor
			return assessment
		}
	}
	return assessment
}

func (target agenticEpisodeTarget) acceptedEvidenceAssessment(
	accepted controlexperiment.RiskCandidateAssessment,
) agenticEvidenceAssessment {
	assessment := agenticEvidenceAssessment{
		Status: agenticEvidencePlanningFailed, ReasonCode: "scenario-not-executed",
		PropertyID:         accepted.Candidate.PropertyRef,
		OracleIDs:          target.Surface.OracleIDsForProperty(accepted.Candidate.PropertyRef),
		FidelityAssessment: accepted.FidelityAssessment,
	}
	for _, notice := range accepted.FidelityNotices {
		assessment.FidelityBoundaryIDs = append(assessment.FidelityBoundaryIDs, notice.Reference)
	}
	for _, property := range target.Knowledge.Properties {
		if property.ID == accepted.Candidate.PropertyRef {
			assessment.EvidenceLevel = property.EvidenceLevel
			break
		}
	}
	return assessment
}

func assessRiskAgentStop(result controlexperiment.RiskAgentResult) agenticEvidenceAssessment {
	assessment := agenticEvidenceAssessment{
		Status: agenticEvidencePlanningFailed, ReasonCode: "risk-candidate-unavailable",
	}
	if len(result.Attempts) == 0 {
		return assessment
	}
	feedback := result.Attempts[len(result.Attempts)-1].Feedback
	assessment.ReasonCode = feedback.ReasonCode
	if len(feedback.CapabilityGaps) > 0 {
		assessment.Status = agenticEvidenceCapabilityGap
		assessment.ReasonCode = feedback.CapabilityGaps[0].Code
		return assessment
	}
	for _, review := range feedback.Reviews {
		if len(review.CapabilityGaps) > 0 {
			assessment.Status = agenticEvidenceCapabilityGap
			assessment.ReasonCode = review.CapabilityGaps[0].Code
			return assessment
		}
		if len(review.Issues) > 0 {
			assessment.Status = agenticEvidenceCapabilityGap
			assessment.ReasonCode = review.Issues[0].Code
			return assessment
		}
	}
	if len(feedback.Issues) > 0 {
		assessment.Status = agenticEvidenceCapabilityGap
		assessment.ReasonCode = feedback.Issues[0].Code
	}
	return assessment
}

func assessTestingEvidence(
	assessment agenticEvidenceAssessment,
	testing scenarioTestingResult,
	scenario controlexperiment.ScenarioAgentResult,
) agenticEvidenceAssessment {
	if len(testing.Oracle.Violations) > 0 {
		assessment.Status = agenticEvidenceOracleFinding
		assessment.ReasonCode = testing.Oracle.Violations[0].Monitor
		return assessment
	}
	if testing.Risk.Status == semantic.RiskWitnessReached {
		assessment.Status = agenticEvidenceRiskReached
		assessment.ReasonCode = "property-oracle-clean"
		if len(assessment.OracleIDs) == 0 {
			assessment.Status = agenticEvidenceRiskUnverified
			assessment.ReasonCode = "missing-property-oracle"
		} else if !containsAllStrings(testing.Oracle.Checked, assessment.OracleIDs) {
			assessment.Status = agenticEvidenceRiskUnverified
			assessment.ReasonCode = "property-oracle-not-executed"
		}
		return assessment
	}
	assessment.Status = agenticEvidenceInconclusive
	assessment.ReasonCode = agenticEvidenceHypothesisNotReached
	if len(testing.Risk.MissingMilestones) > 0 {
		assessment.FirstMissingMilestone = testing.Risk.MissingMilestones[0]
	}
	switch scenario.StopReason {
	case controlexperiment.ScenarioAgentStopHypothesisAbandoned:
		assessment.ReasonCode = agenticEvidenceHypothesisAbandoned
	case controlexperiment.ScenarioAgentStopDecisionBudget:
		assessment.Status = agenticEvidenceBudgetExhausted
		assessment.ReasonCode = "runtime-decision-budget-exhausted"
	case controlexperiment.ScenarioAgentStopCallBudget:
		assessment.Status = agenticEvidenceBudgetExhausted
		assessment.ReasonCode = "scenario-call-budget-exhausted"
	}
	return assessment
}

func containsAllStrings(values []string, required []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range required {
		if !seen[value] {
			return false
		}
	}
	return true
}

func cloneAgenticEpisodeFailure(failure *controlexperiment.MethodFailure) *controlexperiment.MethodFailure {
	if failure == nil {
		return nil
	}
	cloned := *failure
	if failure.Terminal != nil {
		terminal := *failure.Terminal
		terminal.AttemptedAction.Parameters = append(
			[]byte(nil), failure.Terminal.AttemptedAction.Parameters...,
		)
		cloned.Terminal = &terminal
	}
	return &cloned
}

func modelWorkFromAgentAudits(audits []controlexperiment.StatelessAgentCallAudit) controlexperiment.ModelWork {
	var result controlexperiment.ModelWork
	for _, audit := range audits {
		addAgentModelWork(&result, audit.Work)
	}
	return result
}

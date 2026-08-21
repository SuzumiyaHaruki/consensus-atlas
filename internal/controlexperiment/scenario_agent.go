package controlexperiment

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

// SemanticPrefixProjector is trusted target composition. It derives a Risk
// witness from an exact trace while common planning remains protocol-neutral.
type SemanticPrefixProjector interface {
	ID() string
	Project(string, semantic.RiskWitnessSpec, controlruntime.Trace) (semantic.RiskWitnessResult, error)
}

func isNilSemanticComponent(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

const (
	ScenarioPlanningBackendID      = "bounded-scenario-plan-v1"
	ScenarioAgentMaxCalls          = 16
	ScenarioAgentMaxDecisions      = 512
	ScenarioNaturalProgressSlice   = 4
	ScenarioBootstrapProgressSlice = 16

	ScenarioAgentCompleted = "completed"
	ScenarioAgentStopped   = "stopped"

	ScenarioAgentProposalInvalid = "proposal-invalid"
	ScenarioAgentExecutionFailed = "execution-failed"

	ScenarioAgentStopWitnessInstantiated    = "witness-instantiated"
	ScenarioAgentStopPathSelected           = "path-selected"
	ScenarioAgentStopDecisionBudget         = "decision-budget-exhausted"
	ScenarioAgentStopCallBudget             = "call-budget-exhausted"
	ScenarioAgentStopFinalSelectionRequired = "final-selection-required"
	ScenarioAgentStopHypothesisAbandoned    = "hypothesis-abandoned"
	ScenarioAgentStopCapabilityGap          = "capability-gap"
	ScenarioAgentStopProviderResponse       = "provider-response-failed"
	ScenarioAgentReasonResponseFinishLength = "response-finish-length"
	ScenarioAgentReasonResponseEmptyContent = "response-empty-content"
	ScenarioAgentReasonResponseMalformed    = "response-malformed"
	ScenarioAgentReasonResponseTooLarge     = "response-too-large"
)

// ScenarioPlannerResponseFailure describes a charged provider response that
// produced no executable proposal. It is not a transport ambiguity and never
// authorizes hidden Runtime work. Repairable is used only for one explicit
// output-length repair call within the existing call/token budget.
type ScenarioPlannerResponseFailure struct {
	Code       string
	Repairable bool
}

func (failure *ScenarioPlannerResponseFailure) Error() string {
	if failure == nil {
		return ScenarioAgentStopProviderResponse
	}
	return failure.Code
}

func validScenarioPlannerResponseFailure(failure *ScenarioPlannerResponseFailure) bool {
	if failure == nil {
		return false
	}
	switch failure.Code {
	case ScenarioAgentReasonResponseFinishLength:
		return true
	case ScenarioAgentReasonResponseEmptyContent,
		ScenarioAgentReasonResponseMalformed,
		ScenarioAgentReasonResponseTooLarge:
		return !failure.Repairable
	default:
		return false
	}
}

type ScenarioAgentFeedback struct {
	Attempt              int                               `json:"attempt"`
	Intent               string                            `json:"intent,omitempty"`
	BranchID             string                            `json:"branch_id,omitempty"`
	ReferenceBranchID    string                            `json:"reference_branch_id,omitempty"`
	Outcome              string                            `json:"outcome"`
	ReasonCode           string                            `json:"reason_code,omitempty"`
	ValidationIssues     []ScenarioProposalValidationIssue `json:"validation_issues,omitempty"`
	CapabilityGaps       []AgentCapabilityGap              `json:"capability_gaps,omitempty"`
	AllowedIntents       []string                          `json:"allowed_intents,omitempty"`
	PreviousProposal     *ScenarioInvestigationProposal    `json:"previous_proposal,omitempty"`
	FailedStep           *ScenarioStep                     `json:"failed_step,omitempty"`
	Steps                []ScenarioStepFeedback            `json:"steps,omitempty"`
	NaturalProgress      []ScenarioStepFeedback            `json:"natural_progress,omitempty"`
	NaturalProgressStop  string                            `json:"natural_progress_stop,omitempty"`
	ClosureCandidates    []FrontierActionRef               `json:"closure_candidates,omitempty"`
	ClosureHandoff       bool                              `json:"closure_handoff,omitempty"`
	ClosureHandoffStepID string                            `json:"closure_handoff_step_id,omitempty"`
	ProgressDelta        *ScenarioProgressDelta            `json:"progress_delta,omitempty"`
}

type ScenarioAgentView struct {
	// Provider prompts use AcceptedHypothesis when present and omit the two
	// overlapping construction contracts below. They remain available to
	// trusted validation and direct composition tests.
	Knowledge               ProtocolKnowledgePack         `json:"knowledge"`
	Hypothesis              TestHypothesis                `json:"hypothesis"`
	AcceptedHypothesis      *AcceptedHypothesisContext    `json:"accepted_hypothesis,omitempty"`
	TargetSurface           *AgentTargetSurface           `json:"target_surface,omitempty"`
	OrderedMilestones       []string                      `json:"ordered_milestones"`
	Frontier                RiskFrontierView              `json:"root_frontier"`
	Semantics               ScenarioSemanticExposure      `json:"action_semantics"`
	MaxSteps                int                           `json:"max_steps"`
	DecisionAllowance       int                           `json:"decision_allowance"`
	RemainingDecisions      int                           `json:"remaining_decisions"`
	AvailableIntents        []string                      `json:"available_intents"`
	PostInterventionClosure bool                          `json:"post_intervention_closure"`
	Branches                []ScenarioInvestigationBranch `json:"branches,omitempty"`
	Prior                   *ScenarioAgentFeedback        `json:"prior_feedback,omitempty"`
}

type ScenarioAgentAttempt struct {
	Ordinal       int                            `json:"ordinal"`
	ResponseBytes []byte                         `json:"response_bytes"`
	Proposal      *ScenarioInvestigationProposal `json:"proposal,omitempty"`
	Execution     *ScenarioExecution             `json:"execution,omitempty"`
	Feedback      ScenarioAgentFeedback          `json:"feedback"`
	ModelWork     ModelWork                      `json:"model_work"`
}

type ScenarioAgentResult struct {
	Status                     string                        `json:"status"`
	StopReason                 string                        `json:"stop_reason"`
	DecisionsUsed              int                           `json:"decisions_used"`
	SelectedPathDecisions      int                           `json:"selected_path_decisions"`
	BranchExplorationDecisions int                           `json:"branch_exploration_decisions"`
	Attempts                   []ScenarioAgentAttempt        `json:"attempts"`
	Branches                   []ScenarioInvestigationBranch `json:"branches,omitempty"`
	CandidateExecutions        []ScenarioCandidateExecution  `json:"candidate_executions,omitempty"`
	Execution                  *ScenarioExecution            `json:"execution,omitempty"`
	ExecutionWork              ScenarioExecutionWork         `json:"execution_work"`
	ModelWork                  ModelWork                     `json:"model_work"`
}

type ScenarioPlanner func(context.Context, ScenarioAgentView) ([]byte, ModelWork, error)

type scenarioInvestigationBranchState struct {
	public         ScenarioInvestigationBranch
	rootTrace      controlruntime.Trace
	rootRisk       semantic.RiskWitnessResult
	rootFrontier   RiskFrontierView
	rootExecution  *ScenarioExecution
	finalTrace     controlruntime.Trace
	finalRisk      semantic.RiskWitnessResult
	finalFrontier  RiskFrontierView
	finalExecution *ScenarioExecution
}

// ScenarioSemanticProjector is target-owned. Generic continuation rebuilds
// the trusted frontier and snapshot; the target may only classify that exact
// state into the closed Scenario semantic vocabulary.
type ScenarioSemanticProjector func(
	controlruntime.Trace,
	RiskFrontierView,
	controlruntime.Snapshot,
) (ScenarioSemanticExposure, error)

func ExploreScenarioWithPlanner(
	ctx context.Context,
	maxCalls int,
	maxPlanSteps int,
	maxDecisions int,
	knowledge ProtocolKnowledgePack,
	hypothesis TestHypothesis,
	spec semantic.RiskWitnessSpec,
	rootFrontier RiskFrontierView,
	rootSemantics ScenarioSemanticExposure,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	targetSurface *AgentTargetSurface,
	acceptedHypothesis *AcceptedHypothesisContext,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	semanticProjector ScenarioSemanticProjector,
	planner ScenarioPlanner,
	preparers ...ScenarioActionPreparer,
) (ScenarioAgentResult, error) {
	return exploreScenarioWithPlanner(
		ctx, maxCalls, maxPlanSteps, maxDecisions, knowledge, hypothesis, spec,
		rootFrontier, rootSemantics, rootRisk, root, runtimeConfig, faultEnvelope,
		targetSurface, acceptedHypothesis, newAdapter, projector, semanticProjector,
		nil, false, false, planner, preparers...,
	)
}

// ExploreScenarioWithPlannerAndClosure is the Target-composed Agent path.
// The factory is consulted only after a strategic plan has executed a real
// intervention; nil retains the exact behavior of ExploreScenarioWithPlanner.
func ExploreScenarioWithPlannerAndClosure(
	ctx context.Context,
	maxCalls int,
	maxPlanSteps int,
	maxDecisions int,
	knowledge ProtocolKnowledgePack,
	hypothesis TestHypothesis,
	spec semantic.RiskWitnessSpec,
	rootFrontier RiskFrontierView,
	rootSemantics ScenarioSemanticExposure,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	targetSurface *AgentTargetSurface,
	acceptedHypothesis *AcceptedHypothesisContext,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	semanticProjector ScenarioSemanticProjector,
	closureFactory ScenarioClosureFactory,
	planner ScenarioPlanner,
	preparers ...ScenarioActionPreparer,
) (ScenarioAgentResult, error) {
	return exploreScenarioWithPlanner(
		ctx, maxCalls, maxPlanSteps, maxDecisions, knowledge, hypothesis, spec,
		rootFrontier, rootSemantics, rootRisk, root, runtimeConfig, faultEnvelope,
		targetSurface, acceptedHypothesis, newAdapter, projector, semanticProjector,
		closureFactory, closureFactory != nil, false, planner, preparers...,
	)
}

// ExploreScenarioWithPlannerAndScopedClosure exposes closure to the planner
// only when trusted Target composition declares support for this exact spec.
// The factory remains authoritative after a real intervention is executed.
func ExploreScenarioWithPlannerAndScopedClosure(
	ctx context.Context,
	maxCalls int,
	maxPlanSteps int,
	maxDecisions int,
	knowledge ProtocolKnowledgePack,
	hypothesis TestHypothesis,
	spec semantic.RiskWitnessSpec,
	rootFrontier RiskFrontierView,
	rootSemantics ScenarioSemanticExposure,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	targetSurface *AgentTargetSurface,
	acceptedHypothesis *AcceptedHypothesisContext,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	semanticProjector ScenarioSemanticProjector,
	closureFactory ScenarioClosureFactory,
	closureSupport ScenarioClosureSupport,
	planner ScenarioPlanner,
	preparers ...ScenarioActionPreparer,
) (ScenarioAgentResult, error) {
	advertiseClosure := closureFactory != nil && closureSupport != nil && closureSupport(spec)
	return exploreScenarioWithPlanner(
		ctx, maxCalls, maxPlanSteps, maxDecisions, knowledge, hypothesis, spec,
		rootFrontier, rootSemantics, rootRisk, root, runtimeConfig, faultEnvelope,
		targetSurface, acceptedHypothesis, newAdapter, projector, semanticProjector,
		closureFactory, advertiseClosure, true, planner, preparers...,
	)
}

func exploreScenarioWithPlanner(
	ctx context.Context,
	maxCalls int,
	maxPlanSteps int,
	maxDecisions int,
	knowledge ProtocolKnowledgePack,
	hypothesis TestHypothesis,
	spec semantic.RiskWitnessSpec,
	rootFrontier RiskFrontierView,
	rootSemantics ScenarioSemanticExposure,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	targetSurface *AgentTargetSurface,
	acceptedHypothesis *AcceptedHypothesisContext,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	semanticProjector ScenarioSemanticProjector,
	closureFactory ScenarioClosureFactory,
	advertiseClosure bool,
	singlePath bool,
	planner ScenarioPlanner,
	preparers ...ScenarioActionPreparer,
) (ScenarioAgentResult, error) {
	if maxCalls <= 0 || maxCalls > ScenarioAgentMaxCalls || maxPlanSteps <= 0 ||
		maxPlanSteps > ScenarioPlanMaxSteps || maxDecisions <= 0 ||
		maxDecisions > ScenarioAgentMaxDecisions || planner == nil || semanticProjector == nil ||
		hypothesis.Validate(knowledge, spec, ScenarioPlanningBackendID) != nil ||
		rootFrontier.Validate(spec) != nil || rootSemantics.Validate(rootFrontier) != nil ||
		rootRisk.Validate(spec) != nil || root.Validate() != nil ||
		targetSurface != nil && targetSurface.Validate() != nil ||
		acceptedHypothesis != nil && acceptedHypothesis.Validate(knowledge, hypothesis, spec) != nil ||
		rootFrontier.PrefixTraceDigest != root.Digest || rootRisk.ExecutionDigest != root.Digest ||
		rootFrontier.Progress.ValidateSource(spec, rootRisk) != nil || len(preparers) > 1 ||
		len(preparers) == 1 && preparers[0] == nil {
		return ScenarioAgentResult{}, errors.New("EXPERIMENT_SCENARIO_AGENT_INPUT_INVALID")
	}
	var preparer []ScenarioActionPreparer
	if len(preparers) == 1 {
		preparer = preparers
	}
	result := ScenarioAgentResult{Status: ScenarioAgentStopped}
	currentFrontier := cloneScenarioFrontier(rootFrontier)
	currentSemantics := cloneScenarioSemantics(rootSemantics)
	currentRisk := rootRisk
	currentTrace := root
	branches := make(map[string]scenarioInvestigationBranchState)
	usedDecisions := 0
	var prior *ScenarioAgentFeedback
	var pendingRepairView *ScenarioAgentView
	repairUsed := false
	for ordinal := 1; ordinal <= maxCalls; ordinal++ {
		remaining := maxDecisions - usedDecisions
		selectionOnly := !singlePath && len(result.Branches) > 0 && (remaining <= 0 || ordinal == maxCalls)
		if remaining <= 0 && !selectionOnly {
			break
		}
		progressQuantum, viewMaxSteps, attemptAllowance := 0, 0, 0
		availableIntents := []string{ScenarioIntentSelect}
		if !selectionOnly {
			remainingCalls := maxCalls - ordinal + 1
			progressQuantum = (remaining + remainingCalls - 1) / remainingCalls
			progressLimit := ScenarioNaturalProgressSlice
			if currentSemantics.Coordination != nil &&
				currentSemantics.Coordination.Status != ConsensusCoordinatorPresent {
				progressLimit = ScenarioBootstrapProgressSlice
			}
			if progressQuantum > progressLimit {
				progressQuantum = progressLimit
			}
			viewMaxSteps = maxPlanSteps
			if singlePath {
				viewMaxSteps = 1
			}
			if remaining < viewMaxSteps {
				viewMaxSteps = remaining
			}
			attemptAllowance = viewMaxSteps + progressQuantum
			if remaining < attemptAllowance {
				attemptAllowance = remaining
			}
			availableIntents = scenarioAvailableIntents(prior, result.Branches)
			if singlePath {
				availableIntents = scenarioSinglePathIntents(prior)
			}
			if ordinal == maxCalls {
				availableIntents = scenarioFinalExplorationIntents(prior)
				if singlePath {
					availableIntents = scenarioSinglePathIntents(prior)
				}
			}
		}
		view := ScenarioAgentView{
			Knowledge:               cloneProtocolKnowledge(knowledge),
			TargetSurface:           cloneAgentTargetSurface(targetSurface),
			Hypothesis:              hypothesis,
			AcceptedHypothesis:      cloneAcceptedHypothesisContext(acceptedHypothesis),
			OrderedMilestones:       scenarioMilestoneIDs(spec),
			Frontier:                cloneScenarioFrontier(currentFrontier),
			Semantics:               cloneScenarioSemantics(currentSemantics),
			MaxSteps:                viewMaxSteps,
			DecisionAllowance:       attemptAllowance,
			RemainingDecisions:      remaining,
			AvailableIntents:        availableIntents,
			PostInterventionClosure: advertiseClosure,
			Branches:                cloneScenarioBranches(result.Branches),
			Prior:                   cloneScenarioFeedback(prior),
		}
		repairAttempt := pendingRepairView != nil
		if repairAttempt {
			if pendingRepairView.Frontier.Digest != view.Frontier.Digest {
				return result, errors.New("EXPERIMENT_SCENARIO_AGENT_REPAIR_FRONTIER_DRIFT")
			}
			view = cloneScenarioAgentView(*pendingRepairView)
			pendingRepairView = nil
		}
		response, work, err := planner(ctx, view)
		if err != nil {
			var responseFailure *ScenarioPlannerResponseFailure
			if !errors.As(err, &responseFailure) || !validScenarioPlannerResponseFailure(responseFailure) {
				return result, err
			}
			if !validStatelessPlannerWork(work) {
				return result, errors.New("EXPERIMENT_SCENARIO_AGENT_MODEL_WORK_INVALID")
			}
			addScenarioModelWork(&result.ModelWork, work)
			attempt := ScenarioAgentAttempt{
				Ordinal: ordinal, ModelWork: work,
				Feedback: ScenarioAgentFeedback{
					Attempt: ordinal, Outcome: ScenarioAgentStopped,
					ReasonCode:     responseFailure.Code,
					AllowedIntents: append([]string(nil), view.AvailableIntents...),
				},
			}
			result.Attempts = append(result.Attempts, attempt)
			if responseFailure.Repairable {
				// A length repair is the same logical proposal against the same
				// frontier. Do not let this transport-level outcome change the
				// Scenario intent phase before the one allowed repair call.
				if repairUsed || ordinal == maxCalls {
					return finishScenarioAgentResult(result, root, ScenarioAgentStopProviderResponse), nil
				}
				repairUsed = true
				frozen := cloneScenarioAgentView(view)
				pendingRepairView = &frozen
				continue
			}
			return finishScenarioAgentResult(result, root, ScenarioAgentStopProviderResponse), nil
		}
		if !validStatelessPlannerWork(work) {
			return result, errors.New("EXPERIMENT_SCENARIO_AGENT_MODEL_WORK_INVALID")
		}
		addScenarioModelWork(&result.ModelWork, work)
		attempt := ScenarioAgentAttempt{
			Ordinal: ordinal, ResponseBytes: append([]byte(nil), response...), ModelWork: work,
		}
		proposal, validationIssue := InspectScenarioInvestigationProposal(response)
		if validationIssue != nil {
			attempt.Feedback = ScenarioAgentFeedback{
				Attempt: ordinal, Outcome: ScenarioAgentStopped, ReasonCode: ScenarioAgentProposalInvalid,
				ValidationIssues: []ScenarioProposalValidationIssue{*validationIssue},
				AllowedIntents:   append([]string(nil), view.AvailableIntents...),
			}
			if validationIssue.Code != ScenarioProposalIssueJSONInvalid {
				attempt.Proposal = cloneScenarioProposal(&proposal)
				attempt.Feedback.Intent = proposal.Intent
				attempt.Feedback.BranchID = proposal.BranchID
				attempt.Feedback.ReferenceBranchID = proposal.ReferenceBranchID
				attempt.Feedback.PreviousProposal = cloneScenarioProposal(&proposal)
			}
			result.Attempts = append(result.Attempts, attempt)
			if repairAttempt {
				return finishScenarioAgentResult(result, root, ScenarioAgentStopProviderResponse), nil
			}
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		if !containsScenarioIntent(view.AvailableIntents, proposal.Intent) {
			attempt.Proposal = cloneScenarioProposal(&proposal)
			attempt.Feedback = ScenarioAgentFeedback{
				Attempt: ordinal, Intent: proposal.Intent, BranchID: proposal.BranchID,
				ReferenceBranchID: proposal.ReferenceBranchID,
				Outcome:           ScenarioAgentStopped, ReasonCode: ScenarioAgentProposalInvalid,
				ValidationIssues: []ScenarioProposalValidationIssue{{
					Code: ScenarioProposalIssueIntentUnavailable, Field: "intent",
				}},
				AllowedIntents:   append([]string(nil), view.AvailableIntents...),
				PreviousProposal: cloneScenarioProposal(&proposal),
			}
			result.Attempts = append(result.Attempts, attempt)
			if repairAttempt {
				return finishScenarioAgentResult(result, root, ScenarioAgentStopProviderResponse), nil
			}
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		attempt.Proposal = cloneScenarioProposal(&proposal)
		if proposal.Intent == ScenarioIntentAbandon {
			attempt.Feedback = ScenarioAgentFeedback{
				Attempt: ordinal, Intent: proposal.Intent, Outcome: ScenarioAgentStopped,
				ReasonCode:       ScenarioAgentStopHypothesisAbandoned,
				PreviousProposal: cloneScenarioProposal(&proposal),
			}
			result.Attempts = append(result.Attempts, attempt)
			return finishScenarioAgentResult(
				result, root, ScenarioAgentStopHypothesisAbandoned,
			), nil
		}
		plan := proposal.Plan
		selected, reason := selectScenarioProposalRoot(
			proposal, currentTrace, currentRisk, currentFrontier, result.Execution, prior, branches,
		)
		if reason != "" {
			attempt.Feedback = rejectedScenarioIntentFeedback(ordinal, proposal, reason)
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		if proposal.Intent == ScenarioIntentSelect {
			result.Execution = cloneScenarioExecution(selected.execution)
			if result.Execution == nil {
				attempt.Feedback = rejectedScenarioIntentFeedback(
					ordinal, proposal, ScenarioReasonBranchUnknown,
				)
				result.Attempts = append(result.Attempts, attempt)
				prior = &result.Attempts[len(result.Attempts)-1].Feedback
				continue
			}
			currentTrace, currentRisk = selected.trace, selected.risk
			currentFrontier = cloneScenarioFrontier(selected.frontier)
			result.SelectedPathDecisions = len(currentTrace.Records) - len(root.Records)
			attempt.Feedback = ScenarioAgentFeedback{
				Attempt: ordinal, Intent: proposal.Intent, Outcome: ScenarioStatusCompleted,
				PreviousProposal: cloneScenarioProposal(&proposal),
			}
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			if currentRisk.Status == semantic.RiskWitnessReached {
				return finishScenarioAgentResult(result, root, ScenarioAgentStopWitnessInstantiated), nil
			}
			return finishScenarioAgentResult(result, root, ScenarioAgentStopPathSelected), nil
		}
		if targetSurface != nil {
			gaps, gapErr := targetSurface.ScenarioCapabilityGaps(plan, selected.frontier.Actions)
			if gapErr != nil {
				return result, gapErr
			}
			if len(gaps) > 0 {
				attempt.Feedback = ScenarioAgentFeedback{
					Attempt: ordinal, Intent: proposal.Intent, BranchID: proposal.BranchID,
					ReferenceBranchID: proposal.ReferenceBranchID,
					Outcome:           ScenarioAgentStopped, ReasonCode: gaps[0].Code,
					CapabilityGaps:   cloneAgentCapabilityGaps(gaps),
					PreviousProposal: cloneScenarioProposal(&proposal),
					FailedStep:       scenarioPlanStepByID(plan, gaps[0].Reference),
				}
				result.Attempts = append(result.Attempts, attempt)
				prior = &result.Attempts[len(result.Attempts)-1].Feedback
				continue
			}
		}
		attemptRootTrace, attemptRootRisk := selected.trace, selected.risk
		naturalProgressAllowance := progressQuantum
		if attemptAllowance < naturalProgressAllowance {
			naturalProgressAllowance = attemptAllowance
		}
		inheritedIntervention := latestScenarioExecutionClosureIntervention(selected.execution)
		inheritedClosureChoices := scenarioExecutionClosureChoices(selected.execution)
		execution, err := executeSemanticBoundedScenarioPlanWithClosureContext(
			ctx, plan.ID, plan, viewMaxSteps, attemptAllowance, remaining,
			spec, selected.risk, selected.trace,
			runtimeConfig, faultEnvelope, newAdapter, projector, semanticProjector,
			closureFactory, inheritedIntervention, inheritedClosureChoices,
			naturalProgressAllowance, preparer...,
		)
		addScenarioExecutionWork(&result.ExecutionWork, execution.Work)
		if err != nil {
			attempt.Execution = &execution
			attempt.Feedback = ScenarioAgentFeedback{
				Attempt: ordinal, Intent: proposal.Intent, BranchID: proposal.BranchID,
				ReferenceBranchID: proposal.ReferenceBranchID, Outcome: ScenarioAgentStopped,
				ReasonCode: ScenarioAgentExecutionFailed, PreviousProposal: cloneScenarioProposal(&proposal),
				Steps: cloneScenarioStepFeedback(execution.Steps),
			}
			result.Attempts = append(result.Attempts, attempt)
			return result, err
		}
		attempt.Execution = &execution
		attempt.Feedback = ScenarioAgentFeedback{
			Attempt: ordinal, Intent: proposal.Intent, BranchID: proposal.BranchID,
			ReferenceBranchID: proposal.ReferenceBranchID, Outcome: execution.Status,
			PreviousProposal:     cloneScenarioProposal(&proposal),
			Steps:                cloneScenarioStepFeedback(execution.Steps),
			NaturalProgress:      cloneScenarioStepFeedback(execution.AutomaticProgress),
			NaturalProgressStop:  execution.NaturalProgressStop,
			ClosureCandidates:    cloneFrontierActionRefs(execution.ClosureCandidates),
			ClosureHandoff:       execution.ClosureHandoff,
			ClosureHandoffStepID: execution.ClosureHandoffStepID,
		}
		if execution.NaturalProgressStop == ScenarioProgressClosureUnderdetermined ||
			execution.NaturalProgressStop == ScenarioProgressClosureQuiescent ||
			execution.NaturalProgressStop == ScenarioProgressClosureBudget {
			attempt.Feedback.Outcome = ScenarioAgentStopped
			attempt.Feedback.ReasonCode = execution.NaturalProgressStop
		}
		if execution.Status == ScenarioStatusStopped && len(execution.Steps) > 0 {
			failed := execution.Steps[len(execution.Steps)-1]
			attempt.Feedback.ReasonCode = failed.ReasonCode
			attempt.Feedback.FailedStep = scenarioPlanStepByID(plan, failed.StepID)
		}
		committed, ok := committedScenarioPrefix(execution)
		if !ok {
			delta, deltaErr := NewScenarioProgressDelta(
				spec, attemptRootRisk, attemptRootTrace, execution.FinalRisk, execution.FinalTrace,
			)
			if deltaErr != nil {
				return result, deltaErr
			}
			enrichScenarioProgressDelta(
				&delta, execution.NaturalProgressStop, faultEnvelope, execution.FinalTrace, nil,
			)
			attempt.Feedback.ProgressDelta = &delta
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		executedDecisions := len(execution.FinalTrace.Records) - len(attemptRootTrace.Records)
		usedDecisions += executedDecisions
		result.DecisionsUsed = usedDecisions
		promote := proposal.Intent == ScenarioIntentContinue || proposal.Intent == ScenarioIntentRevise
		if !promote {
			result.BranchExplorationDecisions += executedDecisions
		}
		path := ScenarioAgentResult{Execution: cloneScenarioExecution(selected.execution)}
		mergeScenarioExecution(&path, committed)
		finalTrace, finalRisk := execution.FinalTrace, execution.FinalRisk
		delta, deltaErr := NewScenarioProgressDelta(
			spec, attemptRootRisk, attemptRootTrace, finalRisk, finalTrace,
		)
		if deltaErr != nil {
			return result, deltaErr
		}
		if execution.continuationFrontier == nil || execution.continuationSnapshot == nil {
			return result, errors.New("EXPERIMENT_SCENARIO_CONTINUATION_FRONTIER_MISSING")
		}
		frontier := cloneScenarioFrontier(*execution.continuationFrontier)
		snapshot := *execution.continuationSnapshot
		semantics, err := semanticProjector(finalTrace, frontier, snapshot)
		if err != nil {
			return result, fmt.Errorf("EXPERIMENT_SCENARIO_CONTINUATION_SEMANTICS_FAILED: %w", err)
		}
		if err := semantics.Validate(frontier); err != nil {
			return result, fmt.Errorf("EXPERIMENT_SCENARIO_CONTINUATION_SEMANTICS_INVALID: %w", err)
		}
		enrichScenarioProgressDelta(
			&delta, execution.NaturalProgressStop, faultEnvelope, finalTrace, frontier.Actions,
		)
		attempt.Feedback.ProgressDelta = &delta
		if promote {
			currentTrace, currentRisk = finalTrace, finalRisk
			currentFrontier, currentSemantics = frontier, semantics
			result.Execution = path.Execution
			result.SelectedPathDecisions = len(currentTrace.Records) - len(root.Records)
		}
		if proposal.BranchID != "" {
			public := ScenarioInvestigationBranch{
				ID: proposal.BranchID, Intent: proposal.Intent,
				ReferenceBranchID: proposal.ReferenceBranchID,
				RootDecision:      selected.frontier.NextDecision,
				FinalDecision:     frontier.NextDecision,
				AvailableActions:  cloneFrontierActionRefs(frontier.Actions), Plan: plan,
				AppliedInterventions: appliedScenarioInterventions(execution),
				Outcome:              attempt.Feedback.Outcome, ReasonCode: attempt.Feedback.ReasonCode,
				ProgressDelta: cloneScenarioProgressDelta(attempt.Feedback.ProgressDelta),
			}
			state := scenarioInvestigationBranchState{
				public: public, rootTrace: selected.trace, rootRisk: selected.risk,
				rootFrontier:  cloneScenarioFrontier(selected.frontier),
				rootExecution: cloneScenarioExecution(selected.execution), finalTrace: finalTrace,
				finalRisk: finalRisk, finalFrontier: cloneScenarioFrontier(frontier),
				finalExecution: cloneScenarioExecution(path.Execution),
			}
			branches[proposal.BranchID] = state
			result.Branches = append(result.Branches, public)
			result.CandidateExecutions = append(result.CandidateExecutions, ScenarioCandidateExecution{
				BranchID: proposal.BranchID, Intent: proposal.Intent,
				ReferenceBranchID: proposal.ReferenceBranchID,
				Execution:         *cloneScenarioExecution(path.Execution),
			})
		}
		result.Attempts = append(result.Attempts, attempt)
		prior = &result.Attempts[len(result.Attempts)-1].Feedback
		if promote && currentRisk.Status == semantic.RiskWitnessReached {
			return finishScenarioAgentResult(result, root, ScenarioAgentStopWitnessInstantiated), nil
		}
		if usedDecisions >= maxDecisions {
			if ordinal < maxCalls && len(result.Branches) > 0 {
				continue
			}
			return finishScenarioAgentResult(result, root, ScenarioAgentStopDecisionBudget), nil
		}
	}
	stop := ScenarioAgentStopCallBudget
	if result.Execution == nil && len(result.Branches) > 0 {
		stop = ScenarioAgentStopFinalSelectionRequired
	} else if prior != nil && len(prior.CapabilityGaps) > 0 {
		stop = ScenarioAgentStopCapabilityGap
	}
	return finishScenarioAgentResult(result, root, stop), nil
}

func finishScenarioAgentResult(
	result ScenarioAgentResult,
	root controlruntime.Trace,
	stopReason string,
) ScenarioAgentResult {
	result.StopReason = stopReason
	if result.Execution != nil {
		result.Status = ScenarioAgentCompleted
		result.SelectedPathDecisions = len(result.Execution.FinalTrace.Records) - len(root.Records)
	}
	return result
}

func cloneScenarioAgentView(view ScenarioAgentView) ScenarioAgentView {
	view.Knowledge = cloneProtocolKnowledge(view.Knowledge)
	view.AcceptedHypothesis = cloneAcceptedHypothesisContext(view.AcceptedHypothesis)
	view.TargetSurface = cloneAgentTargetSurface(view.TargetSurface)
	view.OrderedMilestones = append([]string(nil), view.OrderedMilestones...)
	view.Frontier = cloneScenarioFrontier(view.Frontier)
	view.Semantics = cloneScenarioSemantics(view.Semantics)
	view.AvailableIntents = append([]string(nil), view.AvailableIntents...)
	view.Branches = cloneScenarioBranches(view.Branches)
	view.Prior = cloneScenarioFeedback(view.Prior)
	return view
}

func scenarioMilestoneIDs(spec semantic.RiskWitnessSpec) []string {
	result := make([]string, len(spec.Milestones))
	for index, milestone := range spec.Milestones {
		result[index] = milestone.ID
	}
	return result
}

func scenarioAvailableIntents(
	prior *ScenarioAgentFeedback,
	branches []ScenarioInvestigationBranch,
) []string {
	if prior == nil {
		return []string{ScenarioIntentContinue}
	}
	if prior.Outcome == ScenarioAgentStopped {
		result := []string{ScenarioIntentRevise}
		if prior.ProgressDelta != nil {
			result = append(result, ScenarioIntentAbandon)
		}
		return result
	}
	result := []string{ScenarioIntentContinue, ScenarioIntentBranch}
	if prior.ProgressDelta != nil {
		result = append(result, ScenarioIntentAbandon)
	}
	if len(branches) > 0 {
		result = append(result, ScenarioIntentControl)
		for _, branch := range branches {
			if branch.Outcome == ScenarioStatusCompleted && len(branch.Plan.Steps) > 1 &&
				len(branch.AppliedInterventions) == len(branch.Plan.Steps) {
				result = append(result, ScenarioIntentAblate)
				break
			}
		}
	}
	return result
}

func scenarioFinalExplorationIntents(prior *ScenarioAgentFeedback) []string {
	if prior == nil {
		return []string{ScenarioIntentContinue}
	}
	result := []string{ScenarioIntentContinue}
	if prior.Outcome == ScenarioAgentStopped {
		result = []string{ScenarioIntentRevise}
	}
	if prior.ProgressDelta != nil {
		result = append(result, ScenarioIntentAbandon)
	}
	return result
}

func scenarioSinglePathIntents(prior *ScenarioAgentFeedback) []string {
	if prior == nil || prior.Outcome != ScenarioAgentStopped {
		return []string{ScenarioIntentContinue}
	}
	result := []string{ScenarioIntentRevise}
	if prior.ProgressDelta != nil {
		result = append(result, ScenarioIntentAbandon)
	}
	return result
}

func containsScenarioIntent(values []string, intent string) bool {
	for _, value := range values {
		if value == intent {
			return true
		}
	}
	return false
}

type scenarioProposalRootSelection struct {
	trace     controlruntime.Trace
	risk      semantic.RiskWitnessResult
	frontier  RiskFrontierView
	execution *ScenarioExecution
}

func selectScenarioProposalRoot(
	proposal ScenarioInvestigationProposal,
	currentTrace controlruntime.Trace,
	currentRisk semantic.RiskWitnessResult,
	currentFrontier RiskFrontierView,
	currentExecution *ScenarioExecution,
	prior *ScenarioAgentFeedback,
	branches map[string]scenarioInvestigationBranchState,
) (scenarioProposalRootSelection, string) {
	selected := scenarioProposalRootSelection{
		trace: currentTrace, risk: currentRisk, frontier: currentFrontier,
		execution: currentExecution,
	}
	if proposal.BranchID != "" {
		if _, exists := branches[proposal.BranchID]; exists {
			return scenarioProposalRootSelection{}, ScenarioReasonBranchDuplicate
		}
	}
	if proposal.Intent == ScenarioIntentRevise && prior == nil {
		return scenarioProposalRootSelection{}, ScenarioReasonRevisionUnavailable
	}
	if proposal.FromBranchID != "" {
		branch, exists := branches[proposal.FromBranchID]
		if !exists {
			return scenarioProposalRootSelection{}, ScenarioReasonBranchUnknown
		}
		selected = scenarioProposalRootSelection{
			trace: branch.finalTrace, risk: branch.finalRisk,
			frontier: branch.finalFrontier, execution: branch.finalExecution,
		}
	}
	if proposal.Intent != ScenarioIntentControl && proposal.Intent != ScenarioIntentAblate {
		return selected, ""
	}
	reference, exists := branches[proposal.ReferenceBranchID]
	if !exists {
		return scenarioProposalRootSelection{}, ScenarioReasonBranchUnknown
	}
	if proposal.Intent == ScenarioIntentAblate && !validScenarioAblation(proposal, reference.public) {
		return scenarioProposalRootSelection{}, ScenarioReasonAblationInvalid
	}
	return scenarioProposalRootSelection{
		trace: reference.rootTrace, risk: reference.rootRisk,
		frontier: reference.rootFrontier, execution: reference.rootExecution,
	}, ""
}

func validScenarioAblation(proposal ScenarioInvestigationProposal, reference ScenarioInvestigationBranch) bool {
	if reference.Outcome != ScenarioStatusCompleted ||
		len(reference.AppliedInterventions) != len(reference.Plan.Steps) ||
		len(proposal.Plan.Steps) >= len(reference.Plan.Steps) {
		return false
	}
	appliedIDs := make(map[string]bool, len(reference.AppliedInterventions))
	for _, intervention := range reference.AppliedInterventions {
		appliedIDs[intervention.StepID] = true
	}
	omittedIDs := make(map[string]bool, len(proposal.OmittedStepIDs))
	for _, omitted := range proposal.OmittedStepIDs {
		if !appliedIDs[omitted] {
			return false
		}
		omittedIDs[omitted] = true
	}
	expected := make([]ScenarioStep, 0, len(reference.Plan.Steps)-len(omittedIDs))
	for _, step := range reference.Plan.Steps {
		if !omittedIDs[step.ID] {
			expected = append(expected, step)
		}
	}
	return reflect.DeepEqual(proposal.Plan.Steps, expected)
}

func appliedScenarioInterventions(execution ScenarioExecution) []ScenarioAppliedIntervention {
	var result []ScenarioAppliedIntervention
	for _, step := range execution.Steps {
		if step.Outcome != ScenarioStepApplied || step.Choice == nil {
			continue
		}
		result = append(result, ScenarioAppliedIntervention{
			StepID: step.StepID, Decision: step.Decision, Action: step.Choice.Action,
		})
	}
	return result
}

func rejectedScenarioIntentFeedback(
	ordinal int,
	proposal ScenarioInvestigationProposal,
	reason string,
) ScenarioAgentFeedback {
	return ScenarioAgentFeedback{
		Attempt: ordinal, Intent: proposal.Intent, BranchID: proposal.BranchID,
		ReferenceBranchID: proposal.ReferenceBranchID, Outcome: ScenarioAgentStopped,
		ReasonCode: reason, PreviousProposal: cloneScenarioProposal(&proposal),
	}
}

// committedScenarioPrefix retains only the mechanically applied leading
// steps. A rejected step remains in the attempt feedback, while the verified
// prefix becomes the continuation root and stays eligible for exact policy
// compilation.
func committedScenarioPrefix(execution ScenarioExecution) (ScenarioExecution, bool) {
	applied := 0
	for applied < len(execution.Steps) && execution.Steps[applied].Outcome == ScenarioStepApplied {
		applied++
	}
	if applied == 0 && len(execution.AutomaticProgress) == 0 {
		return ScenarioExecution{}, false
	}
	committed := execution
	committed.Status = ScenarioStatusCompleted
	committed.Steps = cloneScenarioStepFeedback(execution.Steps[:applied])
	committed.AutomaticProgress = cloneScenarioStepFeedback(execution.AutomaticProgress)
	return committed, true
}

func scenarioPlanStepByID(plan ScenarioPlan, id string) *ScenarioStep {
	for _, step := range plan.Steps {
		if step.ID == id {
			value := step
			return &value
		}
	}
	return nil
}

func mergeScenarioExecution(result *ScenarioAgentResult, execution ScenarioExecution) {
	if result.Execution == nil {
		value := execution
		value.Steps = cloneScenarioStepFeedback(execution.Steps)
		result.Execution = &value
		return
	}
	result.Execution.Status = execution.Status
	result.Execution.Steps = append(
		result.Execution.Steps, cloneScenarioStepFeedback(execution.Steps)...,
	)
	result.Execution.AutomaticProgress = append(
		result.Execution.AutomaticProgress,
		cloneScenarioStepFeedback(execution.AutomaticProgress)...,
	)
	if execution.ClosureHandoff {
		result.Execution.ClosureHandoff = true
		result.Execution.ClosureHandoffStepID = execution.ClosureHandoffStepID
	}
	result.Execution.NaturalProgressStop = execution.NaturalProgressStop
	result.Execution.ClosureCandidates = cloneFrontierActionRefs(execution.ClosureCandidates)
	result.Execution.FinalTrace = execution.FinalTrace
	result.Execution.FinalRisk = execution.FinalRisk
	addScenarioExecutionWork(&result.Execution.Work, execution.Work)
}

func cloneScenarioSemantics(exposure ScenarioSemanticExposure) ScenarioSemanticExposure {
	exposure.ActionHints = append([]ConsensusActionHint(nil), exposure.ActionHints...)
	if exposure.Coordination != nil {
		coordination := *exposure.Coordination
		coordination.ElectionProgress.CandidateNodes = append(
			[]control.NodeID(nil), exposure.Coordination.ElectionProgress.CandidateNodes...,
		)
		exposure.Coordination = &coordination
	}
	return exposure
}

func cloneScenarioFrontier(view RiskFrontierView) RiskFrontierView {
	view.Actions = cloneFrontierActionRefs(view.Actions)
	view.Progress = cloneRiskProgress(view.Progress)
	return view
}

func cloneScenarioFeedback(feedback *ScenarioAgentFeedback) *ScenarioAgentFeedback {
	if feedback == nil {
		return nil
	}
	value := *feedback
	value.ValidationIssues = append([]ScenarioProposalValidationIssue(nil), feedback.ValidationIssues...)
	value.CapabilityGaps = cloneAgentCapabilityGaps(feedback.CapabilityGaps)
	value.AllowedIntents = append([]string(nil), feedback.AllowedIntents...)
	value.PreviousProposal = cloneScenarioProposal(feedback.PreviousProposal)
	if feedback.FailedStep != nil {
		step := *feedback.FailedStep
		value.FailedStep = &step
	}
	value.Steps = cloneScenarioStepFeedback(feedback.Steps)
	value.NaturalProgress = cloneScenarioStepFeedback(feedback.NaturalProgress)
	value.ClosureCandidates = cloneFrontierActionRefs(feedback.ClosureCandidates)
	value.ProgressDelta = cloneScenarioProgressDelta(feedback.ProgressDelta)
	return &value
}

func cloneScenarioProposal(proposal *ScenarioInvestigationProposal) *ScenarioInvestigationProposal {
	if proposal == nil {
		return nil
	}
	value := *proposal
	value.OmittedStepIDs = append([]string(nil), proposal.OmittedStepIDs...)
	value.Plan = *cloneScenarioPlan(&proposal.Plan)
	return &value
}

func cloneScenarioBranches(branches []ScenarioInvestigationBranch) []ScenarioInvestigationBranch {
	result := append([]ScenarioInvestigationBranch(nil), branches...)
	for index := range result {
		result[index].AvailableActions = cloneFrontierActionRefs(branches[index].AvailableActions)
		result[index].Plan = *cloneScenarioPlan(&branches[index].Plan)
		result[index].AppliedInterventions = append(
			[]ScenarioAppliedIntervention(nil), branches[index].AppliedInterventions...,
		)
		result[index].ProgressDelta = cloneScenarioProgressDelta(branches[index].ProgressDelta)
	}
	return result
}

func cloneScenarioExecution(execution *ScenarioExecution) *ScenarioExecution {
	if execution == nil {
		return nil
	}
	value := *execution
	value.Steps = cloneScenarioStepFeedback(execution.Steps)
	value.AutomaticProgress = cloneScenarioStepFeedback(execution.AutomaticProgress)
	value.ClosureCandidates = cloneFrontierActionRefs(execution.ClosureCandidates)
	if execution.continuationFrontier != nil {
		frontier := cloneScenarioFrontier(*execution.continuationFrontier)
		value.continuationFrontier = &frontier
	}
	if execution.continuationSnapshot != nil {
		snapshot := *execution.continuationSnapshot
		value.continuationSnapshot = &snapshot
	}
	return &value
}

func cloneScenarioProgressDelta(delta *ScenarioProgressDelta) *ScenarioProgressDelta {
	if delta == nil {
		return nil
	}
	value := *delta
	value.NewMilestones = append([]string(nil), delta.NewMilestones...)
	value.NewMilestoneEvidence = append([]ScenarioMilestoneEvidence(nil), delta.NewMilestoneEvidence...)
	value.ActionCounts = append([]ScenarioActionCount(nil), delta.ActionCounts...)
	value.AvailableInterventions = append([]control.ActionKind(nil), delta.AvailableInterventions...)
	value.RecentActions = append([]ScenarioRecentAction(nil), delta.RecentActions...)
	if delta.FaultAllowance != nil {
		allowance := *delta.FaultAllowance
		value.FaultAllowance = &allowance
	}
	if delta.FaultUsage != nil {
		usage := *delta.FaultUsage
		value.FaultUsage = &usage
	}
	if delta.FaultRemaining != nil {
		remaining := *delta.FaultRemaining
		value.FaultRemaining = &remaining
	}
	return &value
}

func cloneScenarioPlan(plan *ScenarioPlan) *ScenarioPlan {
	if plan == nil {
		return nil
	}
	value := *plan
	value.Steps = append([]ScenarioStep(nil), plan.Steps...)
	return &value
}

func cloneScenarioStepFeedback(feedback []ScenarioStepFeedback) []ScenarioStepFeedback {
	result := append([]ScenarioStepFeedback(nil), feedback...)
	for index := range result {
		result[index].Available = cloneFrontierActionRefs(feedback[index].Available)
		result[index].SelectorTrace = append(
			[]ScenarioSelectorFilter(nil), feedback[index].SelectorTrace...,
		)
		result[index].RiskProgress = cloneRiskProgress(feedback[index].RiskProgress)
		if feedback[index].Choice != nil {
			choice := *feedback[index].Choice
			result[index].Choice = &choice
		}
	}
	return result
}

func cloneRiskProgress(progress semantic.RiskWitnessProgress) semantic.RiskWitnessProgress {
	progress.SatisfiedMilestones = append([]string(nil), progress.SatisfiedMilestones...)
	return progress
}

func addScenarioModelWork(total *ModelWork, value ModelWork) {
	total.Calls += value.Calls
	total.InputTokens += value.InputTokens
	total.OutputTokens += value.OutputTokens
	total.TotalTokens += value.TotalTokens
}

func addScenarioExecutionWork(total *ScenarioExecutionWork, value ScenarioExecutionWork) {
	addScenarioPhase(&total.FrontierReconstruction, value.FrontierReconstruction)
	addScenarioPhase(&total.ChildMaterialization, value.ChildMaterialization)
	addScenarioPhase(&total.ChildVerification, value.ChildVerification)
	total.TotalWorkUnits = total.FrontierReconstruction.WorkUnits +
		total.ChildMaterialization.WorkUnits + total.ChildVerification.WorkUnits
}

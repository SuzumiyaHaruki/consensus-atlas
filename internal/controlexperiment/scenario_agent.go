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
	ScenarioAgentStopDecisionBudget         = "decision-budget-exhausted"
	ScenarioAgentStopCallBudget             = "call-budget-exhausted"
	ScenarioAgentStopHypothesisAbandoned    = "hypothesis-abandoned"
	ScenarioAgentStopCapabilityGap          = "capability-gap"
	ScenarioAgentStopProviderResponse       = "provider-response-failed"
	ScenarioAgentStopSetupQuiescent         = "setup-quiescent"
	ScenarioAgentStopSetupBudget            = "setup-budget-exhausted"
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
	Attempt             int                               `json:"attempt"`
	Intent              string                            `json:"intent,omitempty"`
	Outcome             string                            `json:"outcome"`
	ReasonCode          string                            `json:"reason_code,omitempty"`
	ValidationIssues    []ScenarioProposalValidationIssue `json:"validation_issues,omitempty"`
	CapabilityGaps      []AgentCapabilityGap              `json:"capability_gaps,omitempty"`
	AllowedIntents      []string                          `json:"allowed_intents,omitempty"`
	PreviousProposal    *ScenarioInvestigationProposal    `json:"previous_proposal,omitempty"`
	FailedStep          *ScenarioStep                     `json:"failed_step,omitempty"`
	Steps               []ScenarioStepFeedback            `json:"steps,omitempty"`
	NaturalProgress     []ScenarioStepFeedback            `json:"natural_progress,omitempty"`
	NaturalProgressStop string                            `json:"natural_progress_stop,omitempty"`
	ProgressDelta       *ScenarioProgressDelta            `json:"progress_delta,omitempty"`
}

type ScenarioAgentView struct {
	// Provider prompts use AcceptedHypothesis when present and omit the two
	// overlapping construction contracts below. They remain available to
	// trusted validation and direct composition tests.
	Knowledge          ProtocolKnowledgePack      `json:"knowledge"`
	Hypothesis         TestHypothesis             `json:"hypothesis"`
	AcceptedHypothesis *AcceptedHypothesisContext `json:"accepted_hypothesis,omitempty"`
	TargetSurface      *AgentTargetSurface        `json:"target_surface,omitempty"`
	OrderedMilestones  []string                   `json:"ordered_milestones"`
	Frontier           RiskFrontierView           `json:"root_frontier"`
	Semantics          ScenarioSemanticExposure   `json:"action_semantics"`
	MaxSteps           int                        `json:"max_steps"`
	DecisionAllowance  int                        `json:"decision_allowance"`
	RemainingDecisions int                        `json:"remaining_decisions"`
	AvailableIntents   []string                   `json:"available_intents"`
	Prior              *ScenarioAgentFeedback     `json:"prior_feedback,omitempty"`
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
	Status                string                 `json:"status"`
	StopReason            string                 `json:"stop_reason"`
	DecisionsUsed         int                    `json:"decisions_used"`
	SelectedPathDecisions int                    `json:"selected_path_decisions"`
	Attempts              []ScenarioAgentAttempt `json:"attempts"`
	Execution             *ScenarioExecution     `json:"execution,omitempty"`
	SetupWork             ScenarioExecutionWork  `json:"setup_work"`
	ExecutionWork         ScenarioExecutionWork  `json:"execution_work"`
	ModelWork             ModelWork              `json:"model_work"`
}

type ScenarioPlanner func(context.Context, ScenarioAgentView) ([]byte, ModelWork, error)

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
		planner, preparers...,
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
	usedDecisions := 0
	var prior *ScenarioAgentFeedback
	var pendingRepairView *ScenarioAgentView
	repairUsed := false
	for ordinal := 1; ordinal <= maxCalls; ordinal++ {
		remaining := maxDecisions - usedDecisions
		if remaining <= 0 {
			break
		}
		automaticGoal := scenarioAutomaticGoalFor(acceptedHypothesis, currentRisk)
		if len(preparer) == 1 && automaticGoal.autoInvoke {
			setupAllowance := scenarioAutomaticSetupAllowance(remaining, automaticGoal)
			if setupAllowance <= 0 {
				return finishScenarioAgentResult(result, root, ScenarioAgentStopSetupBudget), nil
			}
			automatic, frontier, semantics, applied, automaticErr :=
				executeScenarioAutomaticGoal(
					ctx, ordinal, setupAllowance, spec, currentRisk, currentTrace, currentSemantics,
					runtimeConfig, faultEnvelope, newAdapter, projector,
					semanticProjector, automaticGoal, preparer[0],
				)
			if automaticErr != nil {
				return result, automaticErr
			}
			if applied {
				executed := len(automatic.FinalTrace.Records) - len(currentTrace.Records)
				usedDecisions += executed
				result.DecisionsUsed = usedDecisions
				addScenarioExecutionWork(&result.SetupWork, automatic.Work)
				addScenarioExecutionWork(&result.ExecutionWork, automatic.Work)
				mergeScenarioExecution(&result, automatic)
				currentTrace, currentRisk = automatic.FinalTrace, automatic.FinalRisk
				currentFrontier, currentSemantics = frontier, semantics
				result.SelectedPathDecisions = len(currentTrace.Records) -
					ScenarioAgentAttributionRootDecisions(*result.Execution)
				if currentRisk.Status == semantic.RiskWitnessReached {
					return finishScenarioAgentResult(
						result, root, ScenarioAgentStopWitnessInstantiated,
					), nil
				}
				remaining = maxDecisions - usedDecisions
				if remaining <= 0 {
					return finishScenarioAgentResult(result, root, ScenarioAgentStopDecisionBudget), nil
				}
			}
			if !scenarioAutomaticGoalReached(
				automaticGoal, currentRisk, currentFrontier, currentSemantics,
			) {
				switch automatic.NaturalProgressStop {
				case ScenarioProgressQuiescent:
					return finishScenarioAgentResult(result, root, ScenarioAgentStopSetupQuiescent), nil
				case ScenarioProgressBudget:
					return finishScenarioAgentResult(result, root, ScenarioAgentStopSetupBudget), nil
				}
				return finishScenarioAgentResult(result, root, ScenarioAgentStopSetupQuiescent), nil
			}
		}
		remainingCalls := maxCalls - ordinal + 1
		progressQuantum := (remaining + remainingCalls - 1) / remainingCalls
		progressLimit := ScenarioNaturalProgressSlice
		if currentSemantics.Coordination != nil &&
			currentSemantics.Coordination.Status != ConsensusCoordinatorPresent {
			progressLimit = ScenarioBootstrapProgressSlice
		}
		if progressQuantum > progressLimit {
			progressQuantum = progressLimit
		}
		viewMaxSteps := maxPlanSteps
		if remaining < viewMaxSteps {
			viewMaxSteps = remaining
		}
		attemptAllowance := viewMaxSteps + progressQuantum
		if remaining < attemptAllowance {
			attemptAllowance = remaining
		}
		availableIntents := scenarioSinglePathIntents(prior)
		view := ScenarioAgentView{
			Knowledge:          cloneProtocolKnowledge(knowledge),
			TargetSurface:      cloneAgentTargetSurface(targetSurface),
			Hypothesis:         hypothesis,
			AcceptedHypothesis: cloneAcceptedHypothesisContext(acceptedHypothesis),
			OrderedMilestones:  scenarioMilestoneIDs(spec),
			Frontier:           cloneScenarioFrontier(currentFrontier),
			Semantics:          cloneScenarioSemantics(currentSemantics),
			MaxSteps:           viewMaxSteps,
			DecisionAllowance:  attemptAllowance,
			RemainingDecisions: remaining,
			AvailableIntents:   availableIntents,
			Prior:              cloneScenarioFeedback(prior),
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
			if !validStatelessPlannerFailureWork(work) {
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
				Attempt: ordinal, Intent: proposal.Intent,
				Outcome: ScenarioAgentStopped, ReasonCode: ScenarioAgentProposalInvalid,
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
		if proposal.Intent == ScenarioIntentRevise && prior == nil {
			attempt.Feedback = ScenarioAgentFeedback{
				Attempt: ordinal, Intent: proposal.Intent, Outcome: ScenarioAgentStopped,
				ReasonCode:       ScenarioReasonRevisionUnavailable,
				PreviousProposal: cloneScenarioProposal(&proposal),
			}
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		if targetSurface != nil {
			gaps, gapErr := targetSurface.ScenarioCapabilityGaps(plan, currentFrontier.Actions)
			if gapErr != nil {
				return result, gapErr
			}
			if len(gaps) > 0 {
				attempt.Feedback = ScenarioAgentFeedback{
					Attempt: ordinal, Intent: proposal.Intent,
					Outcome: ScenarioAgentStopped, ReasonCode: gaps[0].Code,
					CapabilityGaps:   cloneAgentCapabilityGaps(gaps),
					PreviousProposal: cloneScenarioProposal(&proposal),
					FailedStep:       scenarioPlanStepByID(plan, gaps[0].Reference),
				}
				result.Attempts = append(result.Attempts, attempt)
				prior = &result.Attempts[len(result.Attempts)-1].Feedback
				continue
			}
		}
		attemptRootTrace, attemptRootRisk := currentTrace, currentRisk
		naturalProgressAllowance := progressQuantum
		if attemptAllowance < naturalProgressAllowance {
			naturalProgressAllowance = attemptAllowance
		}
		automaticGoal = scenarioAutomaticGoalFor(acceptedHypothesis, currentRisk)
		execution, err := executeSemanticBoundedScenarioPlan(
			ctx, plan.ID, plan, viewMaxSteps, attemptAllowance,
			spec, currentRisk, currentTrace,
			runtimeConfig, faultEnvelope, newAdapter, projector, semanticProjector,
			automaticGoal, naturalProgressAllowance, preparer...,
		)
		executedThisAttempt := len(execution.FinalTrace.Records) - len(attemptRootTrace.Records)
		execution.NaturalProgressStop = scenarioAgentNaturalProgressStop(
			execution.NaturalProgressStop, usedDecisions, executedThisAttempt, maxDecisions,
		)
		addScenarioExecutionWork(&result.ExecutionWork, execution.Work)
		if err != nil {
			attempt.Execution = &execution
			attempt.Feedback = ScenarioAgentFeedback{
				Attempt: ordinal, Intent: proposal.Intent, Outcome: ScenarioAgentStopped,
				ReasonCode: ScenarioAgentExecutionFailed, PreviousProposal: cloneScenarioProposal(&proposal),
				Steps: cloneScenarioStepFeedback(execution.Steps),
			}
			result.Attempts = append(result.Attempts, attempt)
			return result, err
		}
		attempt.Execution = &execution
		attempt.Feedback = ScenarioAgentFeedback{
			Attempt: ordinal, Intent: proposal.Intent, Outcome: execution.Status,
			PreviousProposal:    cloneScenarioProposal(&proposal),
			Steps:               cloneScenarioStepFeedback(execution.Steps),
			NaturalProgress:     cloneScenarioStepFeedback(execution.AutomaticProgress),
			NaturalProgressStop: execution.NaturalProgressStop,
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
		path := ScenarioAgentResult{Execution: cloneScenarioExecution(result.Execution)}
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
		currentTrace, currentRisk = finalTrace, finalRisk
		currentFrontier, currentSemantics = frontier, semantics
		result.Execution = path.Execution
		result.SelectedPathDecisions = len(currentTrace.Records) -
			ScenarioAgentAttributionRootDecisions(*result.Execution)
		result.Attempts = append(result.Attempts, attempt)
		prior = &result.Attempts[len(result.Attempts)-1].Feedback
		if currentRisk.Status == semantic.RiskWitnessReached {
			return finishScenarioAgentResult(result, root, ScenarioAgentStopWitnessInstantiated), nil
		}
		if usedDecisions >= maxDecisions {
			return finishScenarioAgentResult(result, root, ScenarioAgentStopDecisionBudget), nil
		}
	}
	stop := ScenarioAgentStopCallBudget
	if prior != nil && len(prior.CapabilityGaps) > 0 {
		stop = ScenarioAgentStopCapabilityGap
	}
	return finishScenarioAgentResult(result, root, stop), nil
}

func scenarioAgentNaturalProgressStop(stop string, used int, executed int, total int) string {
	if stop == ScenarioProgressBudget && used >= 0 && executed >= 0 && total > 0 &&
		used+executed < total {
		return ScenarioProgressSlice
	}
	return stop
}

func scenarioAutomaticGoalFor(
	accepted *AcceptedHypothesisContext,
	current semantic.RiskWitnessResult,
) scenarioAutomaticProgressGoal {
	if accepted == nil || len(current.MissingMilestones) == 0 {
		return scenarioAutomaticProgressGoal{}
	}
	missing := current.MissingMilestones[0]
	for index, predicate := range accepted.Candidate.Predicates {
		if predicate.MilestoneID == missing {
			strategic := false
			switch predicate.Kind {
			case semantic.ObservationMessageDropped,
				semantic.ObservationNodeCrashed,
				semantic.ObservationNodeRestarted:
				strategic = true
			}
			goal := scenarioAutomaticProgressGoal{
				autoInvoke:        predicate.Kind == semantic.ObservationWorkloadInvoked,
				yieldForStrategic: strategic,
				invokeMilestone:   predicate.MilestoneID,
			}
			if goal.autoInvoke {
				for nextIndex := index + 1; nextIndex < len(accepted.Candidate.Predicates); nextIndex++ {
					next := accepted.Candidate.Predicates[nextIndex]
					if _, ok := scenarioStrategicPredicateSelector(next, current); ok {
						next.Constraints = append(
							[]semantic.ObservationConstraint(nil), next.Constraints...,
						)
						goal.yieldForStrategic = true
						goal.strategicPredicate = &next
						break
					}
					if !scenarioAutomaticPrerequisiteKind(next.Kind) {
						break
					}
				}
			}
			return goal
		}
	}
	return scenarioAutomaticProgressGoal{}
}

func scenarioAutomaticPrerequisiteKind(kind semantic.ObservationKind) bool {
	switch kind {
	case semantic.ObservationMessageDelivered,
		semantic.ObservationTemporalFired,
		semantic.ObservationCoordinatorChange,
		semantic.ObservationEpochAdvanced,
		semantic.ObservationDecisionAdvanced:
		return true
	default:
		return false
	}
}

func scenarioAutomaticSetupAllowance(remaining int, goal scenarioAutomaticProgressGoal) int {
	if remaining <= 0 {
		return 0
	}
	if goal.strategicPredicate != nil {
		return remaining - 1
	}
	return remaining
}

func scenarioAutomaticGoalReached(
	goal scenarioAutomaticProgressGoal,
	risk semantic.RiskWitnessResult,
	frontier RiskFrontierView,
	semantics ScenarioSemanticExposure,
) bool {
	if !scenarioRiskHasMilestone(risk, goal.invokeMilestone) {
		return false
	}
	if goal.strategicPredicate == nil {
		return true
	}
	return scenarioStrategicFrontierAvailable(
		*goal.strategicPredicate, risk, frontier.Actions, &semantics,
	)
}

func executeScenarioAutomaticGoal(
	ctx context.Context,
	ordinal int,
	maxDecisions int,
	spec semantic.RiskWitnessSpec,
	currentRisk semantic.RiskWitnessResult,
	currentTrace controlruntime.Trace,
	currentSemantics ScenarioSemanticExposure,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	semanticProjector ScenarioSemanticProjector,
	goal scenarioAutomaticProgressGoal,
	preparer ScenarioActionPreparer,
) (ScenarioExecution, RiskFrontierView, ScenarioSemanticExposure, bool, error) {
	if !goal.autoInvoke || maxDecisions <= 0 || preparer == nil {
		return ScenarioExecution{}, RiskFrontierView{}, ScenarioSemanticExposure{}, false, nil
	}
	id := fmt.Sprintf("scenario-automatic-goal-%02d", ordinal)
	progress, err := executeScenarioNaturalProgress(
		ctx, id, maxDecisions, spec, currentRisk, currentTrace, runtimeConfig,
		faultEnvelope, newAdapter, projector, goal, preparer,
		semanticProjector, &currentSemantics,
	)
	if err != nil {
		return ScenarioExecution{}, RiskFrontierView{}, ScenarioSemanticExposure{}, false, err
	}
	execution := progress.Execution
	execution.AutomaticProgress = cloneScenarioStepFeedback(execution.Steps)
	execution.Steps = nil
	execution.NaturalProgressStop = progress.StopReason
	if len(execution.AutomaticProgress) == 0 {
		return execution, RiskFrontierView{}, currentSemantics, false, nil
	}
	if execution.FinalRisk.Status == semantic.RiskWitnessReached {
		return execution, RiskFrontierView{}, currentSemantics, true, nil
	}
	frontier, snapshot, reconstruction, err := ReconstructRiskFrontierState(
		ctx, id+"-continuation", spec, execution.FinalRisk, execution.FinalTrace,
		len(execution.FinalTrace.Records), runtimeConfig, faultEnvelope, newAdapter,
	)
	addScenarioPhase(&execution.Work.FrontierReconstruction, reconstruction)
	execution.Work.TotalWorkUnits = execution.Work.FrontierReconstruction.WorkUnits +
		execution.Work.ChildMaterialization.WorkUnits + execution.Work.ChildVerification.WorkUnits
	if err != nil {
		return ScenarioExecution{}, RiskFrontierView{}, ScenarioSemanticExposure{}, false, err
	}
	semantics, err := semanticProjector(execution.FinalTrace, frontier, snapshot)
	if err != nil || semantics.Validate(frontier) != nil {
		return ScenarioExecution{}, RiskFrontierView{}, ScenarioSemanticExposure{}, false,
			errors.Join(errors.New("EXPERIMENT_SCENARIO_AUTOMATIC_SEMANTICS_INVALID"), err)
	}
	return execution, frontier, semantics, true, nil
}

func finishScenarioAgentResult(
	result ScenarioAgentResult,
	root controlruntime.Trace,
	stopReason string,
) ScenarioAgentResult {
	result.StopReason = stopReason
	if result.Execution != nil {
		result.Status = ScenarioAgentCompleted
		result.SelectedPathDecisions = len(result.Execution.FinalTrace.Records) -
			ScenarioAgentAttributionRootDecisions(*result.Execution)
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

func scenarioSinglePathIntents(prior *ScenarioAgentFeedback) []string {
	if prior == nil {
		return []string{ScenarioIntentContinue}
	}
	if prior.Outcome != ScenarioAgentStopped {
		result := []string{ScenarioIntentContinue}
		if prior.ProgressDelta != nil {
			result = append(result, ScenarioIntentAbandon)
		}
		return result
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
	result.Execution.NaturalProgressStop = execution.NaturalProgressStop
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
	value.ProgressDelta = cloneScenarioProgressDelta(feedback.ProgressDelta)
	return &value
}

func cloneScenarioProposal(proposal *ScenarioInvestigationProposal) *ScenarioInvestigationProposal {
	if proposal == nil {
		return nil
	}
	value := *proposal
	value.Plan = *cloneScenarioPlan(&proposal.Plan)
	return &value
}

func cloneScenarioExecution(execution *ScenarioExecution) *ScenarioExecution {
	if execution == nil {
		return nil
	}
	value := *execution
	value.Steps = cloneScenarioStepFeedback(execution.Steps)
	value.AutomaticProgress = cloneScenarioStepFeedback(execution.AutomaticProgress)
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

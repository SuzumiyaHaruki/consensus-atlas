package controlexperiment

import (
	"context"
	"errors"
	"fmt"
	"reflect"

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
	ScenarioPlanningBackendID = "bounded-scenario-plan-v1"
	ScenarioAgentMaxCalls     = 16
	ScenarioAgentMaxDecisions = 64

	ScenarioAgentCompleted = "completed"
	ScenarioAgentStopped   = "stopped"

	ScenarioAgentPlanInvalid     = "plan-invalid"
	ScenarioAgentExecutionFailed = "execution-failed"
)

type ScenarioAgentFeedback struct {
	Attempt             int                    `json:"attempt"`
	Outcome             string                 `json:"outcome"`
	ReasonCode          string                 `json:"reason_code,omitempty"`
	PreviousPlan        *ScenarioPlan          `json:"previous_plan,omitempty"`
	FailedStep          *ScenarioStep          `json:"failed_step,omitempty"`
	Steps               []ScenarioStepFeedback `json:"steps,omitempty"`
	NaturalProgress     []ScenarioStepFeedback `json:"natural_progress,omitempty"`
	NaturalProgressStop string                 `json:"natural_progress_stop,omitempty"`
	ProgressDelta       *ScenarioProgressDelta `json:"progress_delta,omitempty"`
}

type ScenarioAgentView struct {
	// Provider prompts use AcceptedHypothesis when present and omit the two
	// overlapping legacy contracts below. They remain available to trusted
	// validation and legacy Scenario paths.
	Knowledge          ProtocolKnowledgePack      `json:"knowledge"`
	Hypothesis         TestHypothesis             `json:"hypothesis"`
	AcceptedHypothesis *AcceptedHypothesisContext `json:"accepted_hypothesis,omitempty"`
	TargetSurface      *AgentTargetSurface        `json:"target_surface,omitempty"`
	OrderedMilestones  []string                   `json:"ordered_milestones"`
	Frontier           RiskFrontierView           `json:"root_frontier"`
	Semantics          ScenarioSemanticExposure   `json:"action_semantics"`
	MaxSteps           int                        `json:"max_steps"`
	Prior              *ScenarioAgentFeedback     `json:"prior_feedback,omitempty"`
}

type ScenarioAgentAttempt struct {
	Ordinal       int                   `json:"ordinal"`
	ResponseBytes []byte                `json:"response_bytes"`
	Plan          *ScenarioPlan         `json:"plan,omitempty"`
	Execution     *ScenarioExecution    `json:"execution,omitempty"`
	Feedback      ScenarioAgentFeedback `json:"feedback"`
	ModelWork     ModelWork             `json:"model_work"`
}

type ScenarioAgentResult struct {
	Status        string                 `json:"status"`
	Attempts      []ScenarioAgentAttempt `json:"attempts"`
	Execution     *ScenarioExecution     `json:"execution,omitempty"`
	ExecutionWork StatelessDFSWork       `json:"execution_work"`
	ModelWork     ModelWork              `json:"model_work"`
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
	var prior *ScenarioAgentFeedback
	for ordinal := 1; ordinal <= maxCalls; ordinal++ {
		remaining := maxDecisions
		if result.Execution != nil {
			remaining -= len(result.Execution.FinalTrace.Records) - len(root.Records)
		}
		if remaining <= 0 {
			break
		}
		viewMaxSteps := maxPlanSteps
		if remaining < viewMaxSteps {
			viewMaxSteps = remaining
		}
		view := ScenarioAgentView{
			Knowledge:          cloneProtocolKnowledge(knowledge),
			TargetSurface:      cloneAgentTargetSurface(targetSurface),
			Hypothesis:         hypothesis,
			AcceptedHypothesis: cloneAcceptedHypothesisContext(acceptedHypothesis),
			OrderedMilestones:  scenarioMilestoneIDs(spec),
			Frontier:           cloneScenarioFrontier(currentFrontier),
			Semantics:          cloneScenarioSemantics(currentSemantics),
			MaxSteps:           viewMaxSteps,
			Prior:              cloneScenarioFeedback(prior),
		}
		response, work, err := planner(ctx, view)
		if err != nil {
			return result, err
		}
		if !validStatelessPlannerWork(work) {
			return result, errors.New("EXPERIMENT_SCENARIO_AGENT_MODEL_WORK_INVALID")
		}
		addScenarioModelWork(&result.ModelWork, work)
		attempt := ScenarioAgentAttempt{
			Ordinal: ordinal, ResponseBytes: append([]byte(nil), response...), ModelWork: work,
		}
		attemptRootTrace, attemptRootRisk := currentTrace, currentRisk
		plan, parseErr := ParseScenarioPlan(response)
		if parseErr != nil {
			attempt.Feedback = ScenarioAgentFeedback{
				Attempt: ordinal, Outcome: ScenarioAgentStopped, ReasonCode: ScenarioAgentPlanInvalid,
			}
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		attempt.Plan = &plan
		execution, err := ExecuteBoundedScenarioPlan(
			ctx, plan.ID, plan, viewMaxSteps, remaining, spec, currentRisk, currentTrace,
			runtimeConfig, faultEnvelope, newAdapter, projector, preparer...,
		)
		addScenarioExecutionWork(&result.ExecutionWork, execution.Work)
		if err != nil {
			attempt.Execution = &execution
			attempt.Feedback = ScenarioAgentFeedback{
				Attempt: ordinal, Outcome: ScenarioAgentStopped,
				ReasonCode: ScenarioAgentExecutionFailed, PreviousPlan: cloneScenarioPlan(&plan),
				Steps: cloneScenarioStepFeedback(execution.Steps),
			}
			result.Attempts = append(result.Attempts, attempt)
			return result, err
		}
		attempt.Execution = &execution
		attempt.Feedback = ScenarioAgentFeedback{
			Attempt: ordinal, Outcome: execution.Status, PreviousPlan: cloneScenarioPlan(&plan),
			Steps:           cloneScenarioStepFeedback(execution.Steps),
			NaturalProgress: cloneScenarioStepFeedback(execution.AutomaticProgress),
		}
		delta, deltaErr := NewScenarioProgressDelta(
			spec, attemptRootRisk, attemptRootTrace, execution.FinalRisk, execution.FinalTrace,
		)
		if deltaErr != nil {
			return result, deltaErr
		}
		attempt.Feedback.ProgressDelta = &delta
		if execution.Status == ScenarioStatusStopped && len(execution.Steps) > 0 {
			failed := execution.Steps[len(execution.Steps)-1]
			attempt.Feedback.ReasonCode = failed.ReasonCode
			attempt.Feedback.FailedStep = scenarioPlanStepByID(plan, failed.StepID)
		}
		result.Attempts = append(result.Attempts, attempt)
		committed, ok := committedScenarioPrefix(execution)
		if ok {
			mergeScenarioExecution(&result, committed)
			currentTrace, currentRisk = execution.FinalTrace, execution.FinalRisk
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			if len(result.Execution.FinalTrace.Records)-len(root.Records) >= maxDecisions {
				result.Status = ScenarioAgentCompleted
				return result, nil
			}
			if execution.Status == ScenarioStatusCompleted {
				remaining = maxDecisions - (len(result.Execution.FinalTrace.Records) - len(root.Records))
				if remaining > 0 {
					progress, err := ExecuteScenarioNaturalProgress(
						ctx, fmt.Sprintf("scenario-natural-progress-%02d", ordinal), remaining,
						spec, currentRisk, currentTrace, runtimeConfig, faultEnvelope, newAdapter, projector,
					)
					addScenarioExecutionWork(&result.ExecutionWork, progress.Execution.Work)
					if err != nil {
						return result, err
					}
					stored := &result.Attempts[len(result.Attempts)-1].Feedback
					stored.NaturalProgress = append(
						stored.NaturalProgress,
						cloneScenarioStepFeedback(progress.Execution.Steps)...,
					)
					stored.NaturalProgressStop = progress.StopReason
					prior = stored
					if len(progress.Execution.Steps) > 0 {
						mergeScenarioAutomaticProgress(&result, progress.Execution)
						currentTrace, currentRisk = progress.Execution.FinalTrace, progress.Execution.FinalRisk
					}
					delta, deltaErr := NewScenarioProgressDelta(
						spec, attemptRootRisk, attemptRootTrace, currentRisk, currentTrace,
					)
					if deltaErr != nil {
						return result, deltaErr
					}
					stored.ProgressDelta = &delta
					if currentRisk.Status == semantic.RiskWitnessReached ||
						len(result.Execution.FinalTrace.Records)-len(root.Records) >= maxDecisions {
						result.Status = ScenarioAgentCompleted
						return result, nil
					}
				}
			}
			frontier, snapshot, reconstruction, err := ReconstructRiskFrontierState(
				ctx, "scenario-continuation-frontier", spec, currentRisk, currentTrace,
				len(currentTrace.Records), runtimeConfig, faultEnvelope, newAdapter,
			)
			addDFSPhase(&result.ExecutionWork.FrontierReconstruction, reconstruction)
			result.ExecutionWork.TotalWorkUnits = result.ExecutionWork.FrontierReconstruction.WorkUnits +
				result.ExecutionWork.ChildMaterialization.WorkUnits +
				result.ExecutionWork.ChildVerification.WorkUnits
			if err != nil {
				return result, err
			}
			semantics, err := semanticProjector(currentTrace, frontier, snapshot)
			if err != nil || semantics.Validate(frontier) != nil {
				return result, errors.New("EXPERIMENT_SCENARIO_CONTINUATION_SEMANTICS_INVALID")
			}
			currentFrontier, currentSemantics = frontier, semantics
			continue
		}
		prior = &result.Attempts[len(result.Attempts)-1].Feedback
	}
	if result.Execution != nil {
		result.Status = ScenarioAgentCompleted
	}
	return result, nil
}

func scenarioMilestoneIDs(spec semantic.RiskWitnessSpec) []string {
	result := make([]string, len(spec.Milestones))
	for index, milestone := range spec.Milestones {
		result[index] = milestone.ID
	}
	return result
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
	result.Execution.FinalTrace = execution.FinalTrace
	result.Execution.FinalRisk = execution.FinalRisk
	addScenarioExecutionWork(&result.Execution.Work, execution.Work)
}

func mergeScenarioAutomaticProgress(result *ScenarioAgentResult, execution ScenarioExecution) {
	if result.Execution == nil {
		value := execution
		value.AutomaticProgress = cloneScenarioStepFeedback(execution.Steps)
		value.Steps = nil
		result.Execution = &value
		return
	}
	result.Execution.Status = execution.Status
	result.Execution.AutomaticProgress = append(
		result.Execution.AutomaticProgress,
		cloneScenarioStepFeedback(execution.Steps)...,
	)
	result.Execution.FinalTrace = execution.FinalTrace
	result.Execution.FinalRisk = execution.FinalRisk
	addScenarioExecutionWork(&result.Execution.Work, execution.Work)
}

func cloneScenarioSemantics(exposure ScenarioSemanticExposure) ScenarioSemanticExposure {
	exposure.ActionHints = append([]ConsensusActionHint(nil), exposure.ActionHints...)
	return exposure
}

func cloneScenarioFrontier(view RiskFrontierView) RiskFrontierView {
	view.Actions = append([]FrontierActionRef(nil), view.Actions...)
	view.Progress = cloneRiskProgress(view.Progress)
	return view
}

func cloneScenarioFeedback(feedback *ScenarioAgentFeedback) *ScenarioAgentFeedback {
	if feedback == nil {
		return nil
	}
	value := *feedback
	value.PreviousPlan = cloneScenarioPlan(feedback.PreviousPlan)
	if feedback.FailedStep != nil {
		step := *feedback.FailedStep
		value.FailedStep = &step
	}
	value.Steps = cloneScenarioStepFeedback(feedback.Steps)
	value.NaturalProgress = cloneScenarioStepFeedback(feedback.NaturalProgress)
	value.ProgressDelta = cloneScenarioProgressDelta(feedback.ProgressDelta)
	return &value
}

func cloneScenarioProgressDelta(delta *ScenarioProgressDelta) *ScenarioProgressDelta {
	if delta == nil {
		return nil
	}
	value := *delta
	value.NewMilestones = append([]string(nil), delta.NewMilestones...)
	value.RecentActions = append([]ScenarioRecentAction(nil), delta.RecentActions...)
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
		result[index].Available = append([]FrontierActionRef(nil), feedback[index].Available...)
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

func addScenarioExecutionWork(total *StatelessDFSWork, value StatelessDFSWork) {
	addDFSPhase(&total.FrontierReconstruction, value.FrontierReconstruction)
	addDFSPhase(&total.ChildMaterialization, value.ChildMaterialization)
	addDFSPhase(&total.ChildVerification, value.ChildVerification)
	total.TotalWorkUnits = total.FrontierReconstruction.WorkUnits +
		total.ChildMaterialization.WorkUnits + total.ChildVerification.WorkUnits
}

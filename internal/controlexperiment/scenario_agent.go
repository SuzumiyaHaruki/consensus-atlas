package controlexperiment

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	ScenarioPlanningBackendID = "bounded-scenario-plan-v1"
	ScenarioAgentMaxCalls     = 16
	ScenarioAgentMaxDecisions = 64

	ScenarioAgentCompleted = "completed"
	ScenarioAgentStopped   = "stopped"

	ScenarioAgentPlanInvalid = "plan-invalid"
)

type ScenarioAgentFeedback struct {
	Attempt    int                    `json:"attempt"`
	Outcome    string                 `json:"outcome"`
	ReasonCode string                 `json:"reason_code,omitempty"`
	Steps      []ScenarioStepFeedback `json:"steps,omitempty"`
}

type ScenarioAgentView struct {
	Knowledge  ProtocolKnowledgePack    `json:"knowledge"`
	Hypothesis TestHypothesis           `json:"hypothesis"`
	Frontier   RiskFrontierView         `json:"root_frontier"`
	Semantics  ScenarioSemanticExposure `json:"action_semantics"`
	MaxSteps   int                      `json:"max_steps"`
	Prior      *ScenarioAgentFeedback   `json:"prior_feedback,omitempty"`
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
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	semanticProjector ScenarioSemanticProjector,
	planner ScenarioPlanner,
) (ScenarioAgentResult, error) {
	if maxCalls <= 0 || maxCalls > ScenarioAgentMaxCalls || maxPlanSteps <= 0 ||
		maxPlanSteps > ScenarioPlanMaxSteps || maxDecisions <= 0 ||
		maxDecisions > ScenarioAgentMaxDecisions || planner == nil || semanticProjector == nil ||
		hypothesis.Validate(knowledge, spec, ScenarioPlanningBackendID) != nil ||
		rootFrontier.Validate(spec) != nil || rootSemantics.Validate(rootFrontier) != nil ||
		rootRisk.Validate(spec) != nil || root.Validate() != nil ||
		rootFrontier.PrefixTraceDigest != root.Digest || rootRisk.ExecutionDigest != root.Digest ||
		rootFrontier.Progress.ValidateSource(spec, rootRisk) != nil {
		return ScenarioAgentResult{}, errors.New("EXPERIMENT_SCENARIO_AGENT_INPUT_INVALID")
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
			remaining -= len(result.Execution.Steps)
		}
		if remaining <= 0 {
			break
		}
		viewMaxSteps := maxPlanSteps
		if remaining < viewMaxSteps {
			viewMaxSteps = remaining
		}
		view := ScenarioAgentView{
			Knowledge: cloneProtocolKnowledge(knowledge), Hypothesis: hypothesis,
			Frontier:  cloneScenarioFrontier(currentFrontier),
			Semantics: cloneScenarioSemantics(currentSemantics),
			MaxSteps:  viewMaxSteps,
			Prior:     cloneScenarioFeedback(prior),
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
			ctx, plan.ID, plan, viewMaxSteps, spec, currentRisk, currentTrace,
			runtimeConfig, faultEnvelope, newAdapter, projector,
		)
		if err != nil {
			return result, err
		}
		addScenarioExecutionWork(&result.ExecutionWork, execution.Work)
		attempt.Execution = &execution
		attempt.Feedback = ScenarioAgentFeedback{
			Attempt: ordinal, Outcome: execution.Status, Steps: cloneScenarioStepFeedback(execution.Steps),
		}
		if execution.Status == ScenarioStatusStopped && len(execution.Steps) > 0 {
			attempt.Feedback.ReasonCode = execution.Steps[len(execution.Steps)-1].ReasonCode
		}
		result.Attempts = append(result.Attempts, attempt)
		if execution.Status == ScenarioStatusCompleted {
			mergeScenarioExecution(&result, execution)
			currentTrace, currentRisk = execution.FinalTrace, execution.FinalRisk
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			if currentRisk.Status == semantic.RiskWitnessReached ||
				len(result.Execution.Steps) >= maxDecisions || ordinal == maxCalls {
				result.Status = ScenarioAgentCompleted
				return result, nil
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

func mergeScenarioExecution(result *ScenarioAgentResult, execution ScenarioExecution) {
	if result.Execution == nil {
		value := execution
		value.Steps = cloneScenarioStepFeedback(execution.Steps)
		result.Execution = &value
		return
	}
	result.Execution.PlanID = execution.PlanID
	result.Execution.Status = execution.Status
	result.Execution.Steps = append(
		result.Execution.Steps, cloneScenarioStepFeedback(execution.Steps)...,
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
	value.Steps = cloneScenarioStepFeedback(feedback.Steps)
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

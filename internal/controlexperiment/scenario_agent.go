package controlexperiment

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	ScenarioPlanningBackendID = "bounded-scenario-plan-v1"
	ScenarioAgentMaxAttempts  = 2

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
	Knowledge  ProtocolKnowledgePack  `json:"knowledge"`
	Hypothesis TestHypothesis         `json:"hypothesis"`
	Frontier   RiskFrontierView       `json:"root_frontier"`
	MaxSteps   int                    `json:"max_steps"`
	Prior      *ScenarioAgentFeedback `json:"prior_feedback,omitempty"`
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

func ExploreScenarioWithPlanner(
	ctx context.Context,
	maxAttempts int,
	maxSteps int,
	knowledge ProtocolKnowledgePack,
	hypothesis TestHypothesis,
	spec semantic.RiskWitnessSpec,
	rootFrontier RiskFrontierView,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	planner ScenarioPlanner,
) (ScenarioAgentResult, error) {
	if maxAttempts <= 0 || maxAttempts > ScenarioAgentMaxAttempts || maxSteps <= 0 ||
		maxSteps > ScenarioPlanMaxSteps || planner == nil ||
		hypothesis.Validate(knowledge, spec, ScenarioPlanningBackendID) != nil ||
		rootFrontier.Validate(spec) != nil || rootRisk.Validate(spec) != nil || root.Validate() != nil ||
		rootFrontier.PrefixTraceDigest != root.Digest || rootRisk.ExecutionDigest != root.Digest ||
		rootFrontier.Progress.ValidateSource(spec, rootRisk) != nil {
		return ScenarioAgentResult{}, errors.New("EXPERIMENT_SCENARIO_AGENT_INPUT_INVALID")
	}
	result := ScenarioAgentResult{Status: ScenarioAgentStopped}
	var prior *ScenarioAgentFeedback
	for ordinal := 1; ordinal <= maxAttempts; ordinal++ {
		view := ScenarioAgentView{
			Knowledge: cloneProtocolKnowledge(knowledge), Hypothesis: hypothesis,
			Frontier: cloneScenarioFrontier(rootFrontier), MaxSteps: maxSteps,
			Prior: cloneScenarioFeedback(prior),
		}
		response, work, err := planner(ctx, view)
		if err != nil {
			return ScenarioAgentResult{}, err
		}
		if !validStatelessPlannerWork(work) {
			return ScenarioAgentResult{}, errors.New("EXPERIMENT_SCENARIO_AGENT_MODEL_WORK_INVALID")
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
			ctx, plan.ID, plan, maxSteps, spec, rootRisk, root,
			runtimeConfig, faultEnvelope, newAdapter, projector,
		)
		if err != nil {
			return ScenarioAgentResult{}, err
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
			result.Status, result.Execution = ScenarioAgentCompleted, &execution
			return result, nil
		}
		prior = &result.Attempts[len(result.Attempts)-1].Feedback
	}
	return result, nil
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

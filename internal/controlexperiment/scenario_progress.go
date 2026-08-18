package controlexperiment

import (
	"context"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	ScenarioProgressClientTerminal = "client-terminal"
	ScenarioProgressQuiescent      = "natural-progress-quiescent"
	ScenarioProgressBudget         = "natural-progress-budget-exhausted"
)

var scenarioNaturalProgressPriority = []control.ActionKind{
	control.ActionCompleteEffect,
	control.ActionDeliverMessage,
	control.ActionFireTemporal,
}

type ScenarioProgressResult struct {
	StopReason string            `json:"stop_reason"`
	Execution  ScenarioExecution `json:"execution"`
}

// ExecuteScenarioNaturalProgress advances only host effects, ordinary message
// delivery and naturally due temporal events until a mechanical terminal or
// the supplied remaining decision budget. One exclusively owned live Runtime
// carries the branch; the promoted final Trace is independently fresh-replayed
// once before it is returned.
func ExecuteScenarioNaturalProgress(
	ctx context.Context,
	id string,
	maxDecisions int,
	spec semantic.RiskWitnessSpec,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
) (ScenarioProgressResult, error) {
	if !validMethodToken(id) || maxDecisions <= 0 || maxDecisions > ScenarioAgentMaxDecisions ||
		spec.Validate() != nil || rootRisk.Validate(spec) != nil || root.Validate() != nil ||
		rootRisk.ExecutionDigest != root.Digest || newAdapter == nil ||
		isNilSemanticComponent(projector) || projector.ID() != rootRisk.ProjectorID {
		return ScenarioProgressResult{}, errors.New("EXPERIMENT_SCENARIO_PROGRESS_INPUT_INVALID")
	}
	result := ScenarioProgressResult{Execution: ScenarioExecution{
		PlanID: id, Status: ScenarioStatusCompleted, FinalTrace: root, FinalRisk: rootRisk,
	}}
	view, snapshot, runtime, reconstruction, err := reconstructRiskFrontierRuntime(
		ctx, id+"-frontier-01", spec, rootRisk, root, len(root.Records),
		runtimeConfig, faultEnvelope, newAdapter,
	)
	addScenarioPhase(&result.Execution.Work.FrontierReconstruction, reconstruction)
	if err != nil {
		return ScenarioProgressResult{}, err
	}
	closeWith := func(cause error) error {
		return errors.Join(cause, runtime.Close())
	}
	live, liveErr := executeScenarioNaturalProgressOnLiveRuntime(
		ctx, id, maxDecisions, spec, rootRisk, root, view, snapshot,
		faultEnvelope, runtime, projector,
	)
	addScenarioPhase(&result.Execution.Work.ChildMaterialization, live.Work.ChildMaterialization)
	result.StopReason = live.StopReason
	result.Execution.Steps = live.Steps
	result.Execution.FinalTrace, result.Execution.FinalRisk = live.FinalTrace, live.FinalRisk
	if liveErr != nil {
		result.Execution.Work.TotalWorkUnits = result.Execution.Work.FrontierReconstruction.WorkUnits +
			result.Execution.Work.ChildMaterialization.WorkUnits +
			result.Execution.Work.ChildVerification.WorkUnits
		return result, &ScenarioExecutionError{
			Work: result.Execution.Work, cause: closeWith(liveErr),
		}
	}
	if err := runtime.Close(); err != nil {
		return ScenarioProgressResult{}, err
	}
	if len(result.Execution.Steps) > 0 {
		verification, err := verifyScenarioChild(
			ctx, result.Execution.FinalTrace, runtimeConfig, newAdapter,
		)
		addScenarioPhase(&result.Execution.Work.ChildVerification, verification)
		if err != nil {
			return result, &ScenarioExecutionError{Work: result.Execution.Work, cause: err}
		}
	}
	result.Execution.Work.TotalWorkUnits = result.Execution.Work.FrontierReconstruction.WorkUnits +
		result.Execution.Work.ChildMaterialization.WorkUnits +
		result.Execution.Work.ChildVerification.WorkUnits
	return result, nil
}

type scenarioLiveProgressResult struct {
	StopReason string
	Steps      []ScenarioStepFeedback
	FinalTrace controlruntime.Trace
	FinalRisk  semantic.RiskWitnessResult
	Work       ScenarioExecutionWork
}

func executeScenarioNaturalProgressOnLiveRuntime(
	ctx context.Context,
	id string,
	maxDecisions int,
	spec semantic.RiskWitnessSpec,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	view RiskFrontierView,
	snapshot controlruntime.Snapshot,
	faultEnvelope *FaultEnvelope,
	runtime *controlruntime.Runtime,
	projector SemanticPrefixProjector,
) (scenarioLiveProgressResult, error) {
	result := scenarioLiveProgressResult{FinalTrace: root, FinalRisk: rootRisk}
	for decision := 0; decision < maxDecisions; decision++ {
		if scenarioClientTerminal(snapshot) {
			result.StopReason = ScenarioProgressClientTerminal
			break
		}
		action, ok := scenarioNaturalProgressAction(view.Actions)
		if !ok {
			result.StopReason = ScenarioProgressQuiescent
			break
		}
		choice, err := NewFrontierChoice(
			fmt.Sprintf("%s-choice-%02d", id, decision+1), view, spec, action.ActionID,
		)
		if err != nil {
			return result, err
		}
		frontier, err := scenarioActionFrontier(view)
		if err != nil {
			return result, err
		}
		child, materialization, err := executeScenarioChildOnLiveRuntime(
			ctx, frontier, choice.Action, runtime,
		)
		addScenarioPhase(&result.Work.ChildMaterialization, materialization)
		if err != nil {
			return result, err
		}
		risk, err := projector.Project(
			fmt.Sprintf("%s-risk-%02d", id, decision+1), spec, child,
		)
		if err != nil || risk.Validate(spec) != nil || risk.ProjectorID != projector.ID() ||
			risk.ExecutionDigest != child.Digest || risk.TargetIdentityDigest != child.ManifestDigest {
			return result, errors.New("EXPERIMENT_SCENARIO_PROGRESS_RISK_INVALID")
		}
		progress, err := semantic.NewRiskWitnessProgress(spec, risk)
		if err != nil {
			return result, err
		}
		result.Steps = append(result.Steps, ScenarioStepFeedback{
			StepID: fmt.Sprintf("natural-progress-%02d", decision+1), Outcome: ScenarioStepApplied,
			Decision: view.NextDecision, ViewDigest: view.Digest, MatchCount: 1,
			Choice: &choice, RiskProgress: progress,
		})
		result.FinalTrace, result.FinalRisk = child, risk
		if decision+1 < maxDecisions {
			view, snapshot, err = projectRiskFrontierFromLiveRuntime(
				ctx, fmt.Sprintf("%s-frontier-%02d", id, decision+2), spec,
				result.FinalRisk, result.FinalTrace, faultEnvelope, runtime,
			)
			if err != nil {
				return result, err
			}
		}
	}
	if result.StopReason == "" {
		result.StopReason = ScenarioProgressBudget
	}
	result.Work.TotalWorkUnits = result.Work.ChildMaterialization.WorkUnits
	return result, nil
}

func scenarioNaturalProgressAction(actions []FrontierActionRef) (FrontierActionRef, bool) {
	for _, kind := range scenarioNaturalProgressPriority {
		var selected FrontierActionRef
		for _, action := range actions {
			if action.Kind == kind && (selected.ActionID == "" || scenarioProgressActionLess(action, selected)) {
				selected = action
			}
		}
		if selected.ActionID != "" {
			return selected, true
		}
	}
	return FrontierActionRef{}, false
}

func scenarioProgressActionLess(left, right FrontierActionRef) bool {
	if left.Node.Node != right.Node.Node {
		return left.Node.Node < right.Node.Node
	}
	if left.Node.Incarnation != right.Node.Incarnation {
		return left.Node.Incarnation < right.Node.Incarnation
	}
	if left.ItemID != right.ItemID {
		return left.ItemID < right.ItemID
	}
	return left.ActionID < right.ActionID
}

func scenarioClientTerminal(snapshot controlruntime.Snapshot) bool {
	for _, item := range snapshot.Items {
		if item.Kind == control.ItemClientResult && item.State == control.ItemCompleted &&
			item.Value.Response != nil {
			return true
		}
	}
	return false
}

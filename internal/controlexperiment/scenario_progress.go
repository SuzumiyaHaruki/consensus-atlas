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

// ScenarioNaturalProgressPriority returns the exact trusted closure order for
// experiment identity. The returned slice cannot mutate executor state.
func ScenarioNaturalProgressPriority() []control.ActionKind {
	return append([]control.ActionKind(nil), scenarioNaturalProgressPriority...)
}

type ScenarioProgressResult struct {
	StopReason string            `json:"stop_reason"`
	Execution  ScenarioExecution `json:"execution"`
}

// ExecuteScenarioNaturalProgress advances only host effects, ordinary message
// delivery and naturally due temporal events until a mechanical terminal or
// the supplied remaining decision budget. Every choice still comes from a
// freshly reconstructed admissible frontier and is fresh-replay verified.
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
	closureLimit := maxDecisions
	for decision := 0; decision < closureLimit; decision++ {
		view, snapshot, runtime, reconstruction, err := reconstructRiskFrontierRuntime(
			ctx, fmt.Sprintf("%s-frontier-%02d", id, decision+1), spec,
			result.Execution.FinalRisk, result.Execution.FinalTrace,
			len(result.Execution.FinalTrace.Records), runtimeConfig, faultEnvelope, newAdapter,
		)
		addDFSPhase(&result.Execution.Work.FrontierReconstruction, reconstruction)
		if err != nil {
			return ScenarioProgressResult{}, err
		}
		if scenarioClientTerminal(snapshot) {
			if err := runtime.Close(); err != nil {
				return ScenarioProgressResult{}, err
			}
			result.StopReason = ScenarioProgressClientTerminal
			break
		}
		action, ok := scenarioNaturalProgressAction(view.Actions)
		if !ok {
			if err := runtime.Close(); err != nil {
				return ScenarioProgressResult{}, err
			}
			result.StopReason = ScenarioProgressQuiescent
			break
		}
		choice, err := NewFrontierChoice(
			fmt.Sprintf("%s-choice-%02d", id, decision+1), view, spec, action.ActionID,
		)
		if err != nil {
			return ScenarioProgressResult{}, errors.Join(err, runtime.Close())
		}
		frontier, err := scenarioActionFrontier(view)
		if err != nil {
			return ScenarioProgressResult{}, errors.Join(err, runtime.Close())
		}
		child, materialization, verification, err := materializeDFSChildFromRuntime(
			ctx, frontier, choice.Action, runtime, runtimeConfig, newAdapter,
		)
		addDFSPhase(&result.Execution.Work.ChildMaterialization, materialization)
		addDFSPhase(&result.Execution.Work.ChildVerification, verification)
		if err != nil {
			result.Execution.Work.TotalWorkUnits =
				result.Execution.Work.FrontierReconstruction.WorkUnits +
					result.Execution.Work.ChildMaterialization.WorkUnits +
					result.Execution.Work.ChildVerification.WorkUnits
			return result, &StatelessDFSExecutionError{Work: result.Execution.Work, cause: err}
		}
		risk, err := projector.Project(
			fmt.Sprintf("%s-risk-%02d", id, decision+1), spec, child,
		)
		if err != nil || risk.Validate(spec) != nil || risk.ProjectorID != projector.ID() ||
			risk.ExecutionDigest != child.Digest || risk.TargetIdentityDigest != child.ManifestDigest {
			return ScenarioProgressResult{}, errors.New("EXPERIMENT_SCENARIO_PROGRESS_RISK_INVALID")
		}
		progress, err := semantic.NewRiskWitnessProgress(spec, risk)
		if err != nil {
			return ScenarioProgressResult{}, err
		}
		result.Execution.Steps = append(result.Execution.Steps, ScenarioStepFeedback{
			StepID: fmt.Sprintf("natural-progress-%02d", decision+1), Outcome: ScenarioStepApplied,
			Decision: view.NextDecision, ViewDigest: view.Digest, MatchCount: 1,
			Choice: &choice, RiskProgress: progress,
		})
		result.Execution.FinalTrace, result.Execution.FinalRisk = child, risk
	}
	if result.StopReason == "" {
		result.StopReason = ScenarioProgressBudget
	}
	result.Execution.Work.TotalWorkUnits = result.Execution.Work.FrontierReconstruction.WorkUnits +
		result.Execution.Work.ChildMaterialization.WorkUnits +
		result.Execution.Work.ChildVerification.WorkUnits
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

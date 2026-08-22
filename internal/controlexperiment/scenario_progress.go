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
	ScenarioProgressClientTerminal      = "client-terminal"
	ScenarioProgressQuiescent           = "natural-progress-quiescent"
	ScenarioProgressBudget              = "natural-progress-budget-exhausted"
	ScenarioProgressSlice               = "natural-progress-slice-exhausted"
	ScenarioProgressSemanticYield       = "natural-progress-semantic-yield"
	ScenarioProgressWitnessInstantiated = "witness-instantiated"
)

var scenarioNaturalProgressPriority = []control.ActionKind{
	control.ActionCompleteEffect,
	control.ActionDeliverMessage,
	control.ActionFireTemporal,
}

// scenarioCausalProgressFocus preserves the participant direction selected by
// the most recent strategic Action. It is only a tie-breaker over already
// enabled natural-progress Actions; it cannot create, enable or admit one.
type scenarioCausalProgressFocus struct {
	nodes map[control.NodeID]struct{}
	items map[control.ItemID]struct{}
}

// scenarioAutomaticProgressGoal is derived from the trusted predicate for the
// next missing milestone. It is in-memory execution guidance, not a persisted
// Agent assertion or a new scheduling contract.
type scenarioAutomaticProgressGoal struct {
	autoInvoke         bool
	yieldForStrategic  bool
	reserveStrategic   bool
	targetMilestone    string
	naturalPredicate   *semantic.ObservationPredicate
	strategicPredicate *semantic.ObservationPredicate
}

func newScenarioCausalProgressFocus(action FrontierActionRef) *scenarioCausalProgressFocus {
	nodes := make(map[control.NodeID]struct{}, 4)
	for _, node := range []control.NodeID{
		action.Node.Node, action.Owner.Node, action.MessageSource.Node, action.MessageTarget,
	} {
		if node != "" {
			nodes[node] = struct{}{}
		}
	}
	if len(nodes) == 0 {
		if action.ItemID == "" && len(action.Dependencies) == 0 {
			return nil
		}
	}
	items := make(map[control.ItemID]struct{}, len(action.Dependencies)+1)
	if action.ItemID != "" {
		items[action.ItemID] = struct{}{}
	}
	for _, dependency := range action.Dependencies {
		if dependency != "" {
			items[dependency] = struct{}{}
		}
	}
	return &scenarioCausalProgressFocus{nodes: nodes, items: items}
}

func (focus *scenarioCausalProgressFocus) include(action FrontierActionRef) {
	if focus == nil {
		return
	}
	if focus.items == nil {
		focus.items = make(map[control.ItemID]struct{})
	}
	if action.ItemID != "" {
		focus.items[action.ItemID] = struct{}{}
	}
	for _, dependency := range action.Dependencies {
		if dependency != "" {
			focus.items[dependency] = struct{}{}
		}
	}
}

func scenarioCausalProgressFocusFromTrace(trace controlruntime.Trace) *scenarioCausalProgressFocus {
	for index := len(trace.Records) - 1; index >= 0; index-- {
		action := trace.Records[index].Action
		if action.Node.Node == "" {
			continue
		}
		return newScenarioCausalProgressFocus(FrontierActionRef{
			Kind: action.Kind, Node: action.Node, ItemID: action.Item,
		})
	}
	return nil
}

type ScenarioProgressResult struct {
	StopReason string              `json:"stop_reason"`
	Execution  ScenarioExecution   `json:"execution"`
	Frontier   *ActionFrontierView `json:"frontier,omitempty"`
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
	return executeScenarioNaturalProgress(
		ctx, id, maxDecisions, spec, rootRisk, root, runtimeConfig,
		faultEnvelope, newAdapter, projector, scenarioAutomaticProgressGoal{}, nil, nil, nil,
	)
}

func executeScenarioNaturalProgress(
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
	goal scenarioAutomaticProgressGoal,
	preparer ScenarioActionPreparer,
	semanticProjector ScenarioSemanticProjector,
	rootSemantics *ScenarioSemanticExposure,
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
	focus := scenarioNaturalPredicateFocus(goal.naturalPredicate, rootRisk)
	live, liveErr := executeScenarioNaturalProgressOnLiveRuntime(
		ctx, id, maxDecisions, spec, rootRisk, root, view, snapshot,
		faultEnvelope, runtime, projector, focus, semanticProjector, rootSemantics, goal, preparer,
	)
	addScenarioPhase(&result.Execution.Work.ChildMaterialization, live.Work.ChildMaterialization)
	result.StopReason = live.StopReason
	result.Frontier = live.Frontier
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
	Frontier   *ActionFrontierView
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
	focus *scenarioCausalProgressFocus,
	semanticProjector ScenarioSemanticProjector,
	rootSemantics *ScenarioSemanticExposure,
	goal scenarioAutomaticProgressGoal,
	preparer ScenarioActionPreparer,
) (scenarioLiveProgressResult, error) {
	result := scenarioLiveProgressResult{FinalTrace: root, FinalRisk: rootRisk}
	rootInterventions, err := scenarioStrategicActionKeys(view.Actions)
	if err != nil {
		return result, err
	}
	automaticInvokeAttempted := false
	currentSemantics := rootSemantics
	for decision := 0; decision < maxDecisions; decision++ {
		if scenarioClientTerminal(snapshot) {
			result.StopReason = ScenarioProgressClientTerminal
			break
		}
		var preparedInvoke control.ActionID
		preparedAction := false
		if goal.autoInvoke && !automaticInvokeAttempted && preparer != nil &&
			scenarioCoordinationInvokeReady(currentSemantics) {
			automaticInvokeAttempted = true
			preparedID, prepared, prepareErr := preparer(
				ctx, FrontierActionSelector{Kind: control.ActionInvoke}, result.FinalTrace, runtime,
			)
			if prepareErr != nil {
				return result, prepareErr
			}
			if prepared {
				preparedAction = true
				chargePrepareActions(&result.Work.ChildMaterialization, 1)
				preparedInvoke = preparedID
				enabled, enabledErr := runtime.EnabledActions(ctx)
				if enabledErr != nil {
					return result, enabledErr
				}
				snapshot = runtime.Snapshot()
				admissible := admissibleActions(
					faultEnvelope, faultUsageFromRecords(result.FinalTrace.Records), enabled, snapshot,
				)
				actionFrontier, frontierErr := newActionFrontierView(
					fmt.Sprintf("%s-auto-invoke-frontier", id), result.FinalTrace,
					snapshot, enabled, admissible,
				)
				if frontierErr != nil {
					return result, frontierErr
				}
				progress, progressErr := semantic.NewRiskWitnessProgress(spec, result.FinalRisk)
				if progressErr != nil {
					return result, progressErr
				}
				view, frontierErr = newRiskFrontierView(
					actionFrontier.ID, spec, progress, actionFrontier,
				)
				if frontierErr != nil {
					return result, frontierErr
				}
				if semanticProjector != nil {
					projected, semanticErr := semanticProjector(result.FinalTrace, view, snapshot)
					if semanticErr != nil || projected.Validate(view) != nil {
						return result, errors.Join(
							errors.New("EXPERIMENT_SCENARIO_PROGRESS_SEMANTICS_INVALID"), semanticErr,
						)
					}
					currentSemantics = &projected
				}
			}
		}
		var action FrontierActionRef
		var ok bool
		if preparedInvoke != "" {
			action, ok = scenarioActionByID(view.Actions, preparedInvoke)
			if !ok || action.Kind != control.ActionInvoke {
				return result, errors.New("EXPERIMENT_SCENARIO_AUTOMATIC_INVOKE_MISMATCH")
			}
		} else {
			action, ok = scenarioNaturalProgressActionForGoal(view.Actions, focus, goal)
		}
		if !ok {
			result.StopReason = ScenarioProgressQuiescent
			break
		}
		focus.include(action)
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
		var preparedPrefixes []controlruntime.Trace
		if preparedAction {
			preparedPrefixes = append(preparedPrefixes, result.FinalTrace)
		}
		child, materialization, err := executeScenarioChildOnLiveRuntime(
			ctx, frontier, choice.Action, runtime, preparedPrefixes...,
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
		snapshot = runtime.Snapshot()
		if scenarioClientTerminal(snapshot) {
			result.StopReason = ScenarioProgressClientTerminal
			break
		}
		if result.FinalRisk.Status == semantic.RiskWitnessReached {
			result.StopReason = ScenarioProgressWitnessInstantiated
			break
		}
		if decision+1 < maxDecisions {
			view, snapshot, err = projectRiskFrontierFromLiveRuntime(
				ctx, fmt.Sprintf("%s-frontier-%02d", id, decision+2), spec,
				result.FinalRisk, result.FinalTrace, faultEnvelope, runtime,
			)
			if err != nil {
				return result, err
			}
			var projectedSemantics *ScenarioSemanticExposure
			if semanticProjector != nil && rootSemantics != nil {
				projected, semanticErr := semanticProjector(result.FinalTrace, view, snapshot)
				if semanticErr != nil || projected.Validate(view) != nil {
					return result, errors.Join(errors.New("EXPERIMENT_SCENARIO_PROGRESS_SEMANTICS_INVALID"), semanticErr)
				}
				projectedSemantics = &projected
				currentSemantics = &projected
			}
			if scenarioPublicProgressShouldYield(
				rootRisk, result.FinalRisk, rootInterventions, view.Actions,
				rootSemantics, projectedSemantics, goal,
			) {
				result.StopReason = ScenarioProgressSemanticYield
				frontier, frontierErr := scenarioActionFrontier(view)
				if frontierErr != nil {
					return result, frontierErr
				}
				result.Frontier = &frontier
				break
			}
		}
	}
	if result.StopReason == "" {
		result.StopReason = ScenarioProgressBudget
	}
	result.Work.TotalWorkUnits = result.Work.ChildMaterialization.WorkUnits
	return result, nil
}

func scenarioCoordinationInvokeReady(semantics *ScenarioSemanticExposure) bool {
	return semantics != nil && semantics.Coordination != nil &&
		semantics.Coordination.Status == ConsensusCoordinatorPresent &&
		semantics.Coordination.CoordinatorNode != "" && semantics.Coordination.InvokeReady
}

func scenarioActionByID(actions []FrontierActionRef, id control.ActionID) (FrontierActionRef, bool) {
	for _, action := range actions {
		if action.ActionID == id {
			return action, true
		}
	}
	return FrontierActionRef{}, false
}

func scenarioPublicProgressShouldYield(
	rootRisk semantic.RiskWitnessResult,
	currentRisk semantic.RiskWitnessResult,
	rootInterventions map[string]struct{},
	actions []FrontierActionRef,
	rootSemantics *ScenarioSemanticExposure,
	currentSemantics *ScenarioSemanticExposure,
	goal scenarioAutomaticProgressGoal,
) bool {
	if goal.targetMilestone != "" {
		return scenarioRiskHasMilestone(currentRisk, goal.targetMilestone)
	}
	if goal.strategicPredicate != nil {
		return scenarioStrategicFrontierAvailable(
			*goal.strategicPredicate, currentRisk, actions, currentSemantics,
		)
	}
	if len(currentRisk.SatisfiedMilestones) > len(rootRisk.SatisfiedMilestones) {
		return true
	}
	if rootSemantics != nil && currentSemantics != nil &&
		rootSemantics.Coordination != nil && currentSemantics.Coordination != nil {
		rootCoordination := *rootSemantics.Coordination
		currentCoordination := *currentSemantics.Coordination
		if scenarioCoordinationMeaningfullyChanged(rootCoordination, currentCoordination) {
			if goal.autoInvoke && rootCoordination.Status != ConsensusCoordinatorPresent &&
				scenarioCoordinationInvokeReady(currentSemantics) {
				return false
			}
			return true
		}
		if rootCoordination.Status != ConsensusCoordinatorPresent && !goal.yieldForStrategic {
			// During bootstrap, newly offered Drop/Duplicate controls are a
			// mechanical consequence of ordinary election traffic. Preserve the
			// Agent-selected participant direction until the trusted coordination
			// state changes or the bounded slice ends. If the next trusted
			// milestone requires an intervention, the newly exposed control is
			// instead the point at which the Agent regains ownership.
			return false
		}
	}
	if !goal.yieldForStrategic {
		// A candidate investigates one strategic intervention at a time. Once
		// that intervention is recorded and the next trusted milestone is
		// ordinary protocol progress, newly derived fault controls are choices
		// for another investigation, not a reason to interrupt this one.
		return false
	}
	current, err := scenarioStrategicActionKeys(actions)
	if err != nil {
		return true
	}
	for key := range current {
		if _, existed := rootInterventions[key]; !existed {
			return true
		}
	}
	return false
}

func scenarioStrategicFrontierAvailable(
	predicate semantic.ObservationPredicate,
	risk semantic.RiskWitnessResult,
	actions []FrontierActionRef,
	semantics *ScenarioSemanticExposure,
) bool {
	if semantics == nil {
		return false
	}
	selector, ok := scenarioStrategicPredicateSelector(predicate, risk)
	return ok && len(scenarioMatches(actions, *semantics, selector)) > 0
}

func scenarioStrategicPredicateSelector(
	predicate semantic.ObservationPredicate,
	risk semantic.RiskWitnessResult,
) (FrontierActionSelector, bool) {
	selector := FrontierActionSelector{}
	hasOperationStage := false
	for _, constraint := range predicate.Constraints {
		if constraint.Field == semantic.ObservationFieldOperationStage {
			hasOperationStage = true
			break
		}
	}
	switch predicate.Kind {
	case semantic.ObservationMessageDropped:
		selector.Kind = control.ActionDropMessage
	case semantic.ObservationNodeCrashed:
		selector.Kind = control.ActionCrash
	case semantic.ObservationNodeRestarted:
		selector.Kind = control.ActionRestart
	default:
		return FrontierActionSelector{}, false
	}
	for _, constraint := range predicate.Constraints {
		value := constraint.Equals
		if value == "" && constraint.BindAs != "" {
			value = scenarioResolvedRiskBinding(risk, constraint.BindAs)
		}
		if value == "" {
			continue
		}
		switch constraint.Field {
		case semantic.ObservationFieldParticipantNode:
			selector.Node = control.NodeID(value)
		case semantic.ObservationFieldMessageSourceNode:
			selector.MessageSource = control.NodeID(value)
		case semantic.ObservationFieldMessageTargetNode:
			selector.MessageTarget = control.NodeID(value)
		case semantic.ObservationFieldMessageRole:
			switch value {
			case ConsensusMessageVote, ConsensusMessageProposal,
				ConsensusMessageReplication, ConsensusMessageHeartbeat,
				ConsensusMessageRecovery:
				selector.MessageClass = value
			default:
				// A target-local role accompanied by operation-stage is matched
				// through the closed, protocol-neutral OperationState projection.
				// Core does not enumerate target vocabulary values.
				if hasOperationStage {
					continue
				}
				selector.MessageTypeHint = value
			}
		case semantic.ObservationFieldOperationStage:
			selector.OperationState = value
		}
	}
	return selector, selector.validate() == nil
}

func scenarioResolvedRiskBinding(risk semantic.RiskWitnessResult, name string) string {
	for _, milestone := range risk.Milestones {
		for _, binding := range milestone.Bindings {
			if binding.Name == name {
				return binding.Value
			}
		}
	}
	return ""
}

func scenarioNaturalPredicateFocus(
	predicate *semantic.ObservationPredicate,
	risk semantic.RiskWitnessResult,
) *scenarioCausalProgressFocus {
	if predicate == nil {
		return nil
	}
	nodes := make(map[control.NodeID]struct{})
	for _, constraint := range predicate.Constraints {
		value := constraint.Equals
		if value == "" && constraint.BindAs != "" {
			value = scenarioResolvedRiskBinding(risk, constraint.BindAs)
		}
		if value == "" {
			continue
		}
		switch constraint.Field {
		case semantic.ObservationFieldParticipantNode,
			semantic.ObservationFieldRelatedNode,
			semantic.ObservationFieldMessageSourceNode,
			semantic.ObservationFieldMessageTargetNode,
			semantic.ObservationFieldNewCoordinatorNode,
			semantic.ObservationFieldPreviousCoordinatorNode:
			nodes[control.NodeID(value)] = struct{}{}
		}
	}
	if len(nodes) == 0 {
		return nil
	}
	return &scenarioCausalProgressFocus{nodes: nodes, items: make(map[control.ItemID]struct{})}
}

func scenarioCoordinationMeaningfullyChanged(
	root ConsensusCoordinationStatus,
	current ConsensusCoordinationStatus,
) bool {
	// Candidate formation and ordinary term/ballot churn are bootstrap
	// mechanics, not a strategic decision boundary. Keep progressing until a
	// unique coordinator is actually ready, or until the bounded slice ends.
	if root.Status != ConsensusCoordinatorPresent &&
		current.Status != ConsensusCoordinatorPresent {
		return false
	}
	return root.Status != current.Status || root.CoordinatorNode != current.CoordinatorNode ||
		root.InvokeReady != current.InvokeReady ||
		root.ElectionProgress.TermOrBallotChanged != current.ElectionProgress.TermOrBallotChanged
}

func scenarioStrategicActionKeys(actions []FrontierActionRef) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	for _, action := range actions {
		if scenarioNaturalProgressKind(action.Kind) {
			continue
		}
		normalized := action
		normalized.ActionID = ""
		normalized.ActionDigest = ""
		digest, err := control.CanonicalDigest(normalized)
		if err != nil {
			return nil, err
		}
		result[digest] = struct{}{}
	}
	return result, nil
}

func scenarioNaturalProgressAction(actions []FrontierActionRef) (FrontierActionRef, bool) {
	return scenarioNaturalProgressActionWithFocus(actions, nil)
}

func scenarioNaturalProgressActionForGoal(
	actions []FrontierActionRef,
	focus *scenarioCausalProgressFocus,
	goal scenarioAutomaticProgressGoal,
) (FrontierActionRef, bool) {
	priority := scenarioNaturalProgressPriority
	if goal.naturalPredicate != nil {
		switch goal.naturalPredicate.Kind {
		case semantic.ObservationMessageDelivered:
			priority = []control.ActionKind{control.ActionDeliverMessage}
		case semantic.ObservationTemporalFired:
			priority = []control.ActionKind{control.ActionFireTemporal}
		}
	}
	selected, ok := scenarioNaturalProgressActionByPriority(actions, focus, priority)
	if ok || len(priority) == len(scenarioNaturalProgressPriority) {
		return selected, ok
	}
	// A direct observation may not yet be enabled. Ordinary prerequisite work
	// remains eligible, but the requested natural Action wins as soon as it is
	// present in the authoritative frontier.
	return scenarioNaturalProgressActionByPriority(
		actions, focus, scenarioNaturalProgressPriority,
	)
}

func scenarioNaturalProgressActionWithFocus(
	actions []FrontierActionRef,
	focus *scenarioCausalProgressFocus,
) (FrontierActionRef, bool) {
	return scenarioNaturalProgressActionByPriority(
		actions, focus, scenarioNaturalProgressPriority,
	)
}

func scenarioNaturalProgressActionByPriority(
	actions []FrontierActionRef,
	focus *scenarioCausalProgressFocus,
	priority []control.ActionKind,
) (FrontierActionRef, bool) {
	// First preserve the selected causal direction across Action kinds. An
	// unrelated CompleteEffect must not preempt a vote/prepare delivery that
	// continues the participant direction the planner just selected.
	if focus != nil {
		for _, kind := range priority {
			var dependent FrontierActionRef
			for _, action := range actions {
				if action.Kind == kind && scenarioProgressActionDependsOnFocus(action, focus) &&
					(dependent.ActionID == "" || scenarioProgressActionLess(action, dependent)) {
					dependent = action
				}
			}
			if dependent.ActionID != "" {
				return dependent, true
			}
		}
		for _, kind := range priority {
			var focused FrontierActionRef
			for _, action := range actions {
				if action.Kind == kind && scenarioProgressActionInFocus(action, focus) &&
					(focused.ActionID == "" || scenarioProgressActionLess(action, focused)) {
					focused = action
				}
			}
			if focused.ActionID != "" {
				return focused, true
			}
		}
	}
	for _, kind := range priority {
		var selected FrontierActionRef
		for _, action := range actions {
			if action.Kind != kind {
				continue
			}
			if selected.ActionID == "" || scenarioProgressActionLess(action, selected) {
				selected = action
			}
		}
		if selected.ActionID != "" {
			return selected, true
		}
	}
	return FrontierActionRef{}, false
}

func scenarioProgressActionDependsOnFocus(
	action FrontierActionRef,
	focus *scenarioCausalProgressFocus,
) bool {
	if focus == nil || len(focus.items) == 0 {
		return false
	}
	if _, ok := focus.items[action.ItemID]; ok && action.ItemID != "" {
		return true
	}
	for _, dependency := range action.Dependencies {
		if _, ok := focus.items[dependency]; ok && dependency != "" {
			return true
		}
	}
	return false
}

func scenarioProgressActionInFocus(
	action FrontierActionRef,
	focus *scenarioCausalProgressFocus,
) bool {
	if focus == nil || len(focus.nodes) == 0 {
		return false
	}
	for _, node := range []control.NodeID{
		action.Node.Node, action.Owner.Node, action.MessageSource.Node, action.MessageTarget,
	} {
		if _, ok := focus.nodes[node]; ok && node != "" {
			return true
		}
	}
	return false
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

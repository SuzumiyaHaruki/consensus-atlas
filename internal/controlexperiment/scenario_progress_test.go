package controlexperiment

import (
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestScenarioNaturalProgressPrefersSelectedParticipantCausalDirection(t *testing.T) {
	n2FirstByStableOrder := FrontierActionRef{
		ActionID: "deliver-a", ActionDigest: "digest-a", Kind: control.ActionDeliverMessage,
		MessageSource: control.NodeRef{Node: "n2", Incarnation: 1}, MessageTarget: "n3",
	}
	n1Causal := FrontierActionRef{
		ActionID: "deliver-z", ActionDigest: "digest-z", Kind: control.ActionDeliverMessage,
		MessageSource: control.NodeRef{Node: "n1", Incarnation: 1}, MessageTarget: "n4",
	}
	actions := []FrontierActionRef{n2FirstByStableOrder, n1Causal}
	withoutFocus, ok := scenarioNaturalProgressAction(actions)
	if !ok || withoutFocus.ActionID != n2FirstByStableOrder.ActionID {
		t.Fatalf("public fallback order drifted: %#v", withoutFocus)
	}
	focus := newScenarioCausalProgressFocus(FrontierActionRef{
		Kind: control.ActionFireTemporal, Node: control.NodeRef{Node: "n1", Incarnation: 1},
	})
	withFocus, ok := scenarioNaturalProgressActionWithFocus(actions, focus)
	if !ok || withFocus.ActionID != n1Causal.ActionID {
		t.Fatalf("causal participant direction was not preferred: %#v", withFocus)
	}
	if !reflect.DeepEqual(actions, []FrontierActionRef{n2FirstByStableOrder, n1Causal}) {
		t.Fatal("causal selection mutated the authoritative frontier")
	}
}

func TestScenarioAgentDistinguishesSliceExhaustionFromEpisodeBudgetAndStall(t *testing.T) {
	if got := scenarioAgentNaturalProgressStop(ScenarioProgressBudget, 9, 4, 64); got != ScenarioProgressSlice {
		t.Fatalf("bounded progress slice was reported as an episode limit: %q", got)
	}
	if got := scenarioAgentNaturalProgressStop(ScenarioProgressBudget, 60, 4, 64); got != ScenarioProgressBudget {
		t.Fatalf("episode decision limit was reported as a slice: %q", got)
	}
	if got := scenarioAgentNaturalProgressStop(ScenarioProgressQuiescent, 9, 4, 64); got != ScenarioProgressQuiescent {
		t.Fatalf("true quiescence was rewritten as a slice limit: %q", got)
	}
}

func TestScenarioAutomaticSetupReservesOneStrategicDecision(t *testing.T) {
	goal := scenarioAutomaticProgressGoal{autoInvoke: true, reserveStrategic: true}
	if got := scenarioAutomaticSetupAllowance(64, goal); got != 63 {
		t.Fatalf("setup did not reserve one strategic decision: %d", got)
	}
	if got := scenarioAutomaticSetupAllowance(1, goal); got != 0 {
		t.Fatalf("setup consumed the only strategic decision: %d", got)
	}
	if got := scenarioAutomaticSetupAllowance(64, scenarioAutomaticProgressGoal{autoInvoke: true}); got != 64 {
		t.Fatalf("terminal Invoke-only witness unnecessarily reserved a decision: %d", got)
	}
}

func TestScenarioAutomaticGoalAdvancesOnlyTheImmediateMissingMilestone(t *testing.T) {
	accepted := &AcceptedHypothesisContext{Candidate: RiskCandidate{Predicates: []semantic.ObservationPredicate{
		{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
		{MilestoneID: "append", Kind: semantic.ObservationMessageDelivered},
		{MilestoneID: "ack", Kind: semantic.ObservationMessageDelivered},
		{MilestoneID: "crash", Kind: semantic.ObservationNodeCrashed},
	}}}
	goal := scenarioAutomaticGoalFor(accepted, semantic.RiskWitnessResult{
		MissingMilestones: []string{"invoke", "append", "ack", "crash"},
	})
	if !goal.autoInvoke || goal.targetMilestone != "invoke" || goal.yieldForStrategic ||
		goal.naturalPredicate != nil || !goal.reserveStrategic {
		t.Fatalf("Invoke was not retained as the immediate trusted goal: %#v", goal)
	}
	goal = scenarioAutomaticGoalFor(accepted, semantic.RiskWitnessResult{
		SatisfiedMilestones: []string{"invoke"},
		MissingMilestones:   []string{"append", "ack", "crash"},
	})
	if goal.autoInvoke || goal.targetMilestone != "append" || goal.yieldForStrategic ||
		goal.naturalPredicate == nil || goal.naturalPredicate.Kind != semantic.ObservationMessageDelivered ||
		!goal.reserveStrategic {
		t.Fatalf("natural prerequisite was skipped in favor of a future fault: %#v", goal)
	}
	goal = scenarioAutomaticGoalFor(accepted, semantic.RiskWitnessResult{
		SatisfiedMilestones: []string{"invoke", "append", "ack"},
		MissingMilestones:   []string{"crash"},
	})
	if goal.autoInvoke || goal.targetMilestone != "" || !goal.yieldForStrategic ||
		goal.naturalPredicate != nil || !goal.reserveStrategic ||
		goal.strategicPredicate == nil || goal.strategicPredicate.MilestoneID != "crash" {
		t.Fatalf("current strategic milestone was not returned to the Agent: %#v", goal)
	}
	accepted.Candidate.Predicates[2] = semantic.ObservationPredicate{
		MilestoneID: "second-invoke", Kind: semantic.ObservationWorkloadInvoked,
	}
	goal = scenarioAutomaticGoalFor(accepted, semantic.RiskWitnessResult{
		MissingMilestones: []string{"invoke", "append", "second-invoke", "crash"},
	})
	if !goal.autoInvoke || goal.reserveStrategic || goal.yieldForStrategic ||
		goal.strategicPredicate != nil {
		t.Fatalf("automatic setup crossed a non-public prerequisite: %#v", goal)
	}
}

func TestScenarioImmediateStrategicGoalWaitsForMatchingFrontier(t *testing.T) {
	predicate := semantic.ObservationPredicate{
		MilestoneID: "drop", Kind: semantic.ObservationMessageDropped,
		Constraints: []semantic.ObservationConstraint{{
			Field: semantic.ObservationFieldMessageTargetNode, Equals: "n2",
		}},
	}
	goal := scenarioAutomaticProgressGoal{
		yieldForStrategic: true, reserveStrategic: true, strategicPredicate: &predicate,
	}
	semantics := ScenarioSemanticExposure{
		Mode: ScenarioSemanticExposureFull,
		ActionHints: []ConsensusActionHint{{
			ActionID: "unrelated", ActionDigest: "unrelated-digest",
			ActorRole: ConsensusSemanticUnknown, MessageClass: ConsensusSemanticUnknown,
			EpochRelation: ConsensusSemanticUnknown, OperationState: ConsensusSemanticUnknown,
		}},
	}
	unrelated := []FrontierActionRef{{
		ActionID: "unrelated", ActionDigest: "unrelated-digest", Kind: control.ActionDropMessage,
		MessageSource: control.NodeRef{Node: "n1", Incarnation: 1}, MessageTarget: "n3",
	}}
	if scenarioPublicProgressShouldYield(
		semantic.RiskWitnessResult{}, semantic.RiskWitnessResult{}, map[string]struct{}{},
		unrelated, nil, &semantics, goal,
	) {
		t.Fatal("unrelated strategic Action preempted the immediate typed milestone")
	}
	matching := append([]FrontierActionRef(nil), unrelated...)
	matching[0].MessageTarget = "n2"
	if !scenarioPublicProgressShouldYield(
		semantic.RiskWitnessResult{}, semantic.RiskWitnessResult{}, map[string]struct{}{},
		matching, nil, &semantics, goal,
	) {
		t.Fatal("matching strategic frontier was not returned to the Agent")
	}
}

func TestScenarioNaturalMilestonePrefersItsActionKind(t *testing.T) {
	actions := []FrontierActionRef{
		{ActionID: "effect", ActionDigest: "effect-digest", Kind: control.ActionCompleteEffect},
		{ActionID: "deliver", ActionDigest: "deliver-digest", Kind: control.ActionDeliverMessage},
		{ActionID: "timer", ActionDigest: "timer-digest", Kind: control.ActionFireTemporal},
	}
	timer := semantic.ObservationPredicate{Kind: semantic.ObservationTemporalFired}
	selected, ok := scenarioNaturalProgressActionForGoal(
		actions, nil, scenarioAutomaticProgressGoal{naturalPredicate: &timer},
	)
	if !ok || selected.ActionID != "timer" {
		t.Fatalf("timer milestone was preempted by unrelated natural work: %#v", selected)
	}
	delivered := semantic.ObservationPredicate{Kind: semantic.ObservationMessageDelivered}
	selected, ok = scenarioNaturalProgressActionForGoal(
		actions, nil, scenarioAutomaticProgressGoal{naturalPredicate: &delivered},
	)
	if !ok || selected.ActionID != "deliver" {
		t.Fatalf("delivery milestone was preempted by unrelated natural work: %#v", selected)
	}
}

func TestScenarioNaturalMilestoneUsesTrustedResolvedParticipantBinding(t *testing.T) {
	predicate := semantic.ObservationPredicate{
		Kind: semantic.ObservationTemporalFired,
		Constraints: []semantic.ObservationConstraint{{
			Field: semantic.ObservationFieldParticipantNode, BindAs: "pulse-node",
		}},
	}
	risk := semantic.RiskWitnessResult{Milestones: []semantic.RiskWitnessMilestoneEvidence{{
		MilestoneID: "pulse-a",
		Bindings: []semantic.RiskWitnessBindingEvidence{{
			Name: "pulse-node", Field: semantic.ObservationFieldParticipantNode, Value: "n2",
		}},
	}}}
	focus := scenarioNaturalPredicateFocus(&predicate, risk)
	actions := []FrontierActionRef{
		{ActionID: "n1-timer", ActionDigest: "n1-digest", Kind: control.ActionFireTemporal,
			Node: control.NodeRef{Node: "n1", Incarnation: 1}},
		{ActionID: "n2-timer", ActionDigest: "n2-digest", Kind: control.ActionFireTemporal,
			Node: control.NodeRef{Node: "n2", Incarnation: 1}},
	}
	selected, ok := scenarioNaturalProgressActionForGoal(
		actions, focus, scenarioAutomaticProgressGoal{naturalPredicate: &predicate},
	)
	if !ok || selected.ActionID != "n2-timer" {
		t.Fatalf("trusted participant binding did not focus natural progress: %#v", selected)
	}
	if !reflect.DeepEqual(actions[0].Node.Node, control.NodeID("n1")) {
		t.Fatal("natural selection mutated the authoritative frontier")
	}
}

func TestScenarioAgentStrategicProjectionHidesAndRejectsNaturalActions(t *testing.T) {
	frontier := RiskFrontierView{
		PrefixTraceDigest: "trace", SnapshotDigest: "snapshot",
		Actions: []FrontierActionRef{
			{ActionID: "complete", ActionDigest: "complete-digest", Kind: control.ActionCompleteEffect},
			{ActionID: "deliver", ActionDigest: "deliver-digest", Kind: control.ActionDeliverMessage},
			{ActionID: "timer", ActionDigest: "timer-digest", Kind: control.ActionFireTemporal},
			{ActionID: "drop", ActionDigest: "drop-digest", Kind: control.ActionDropMessage},
			{ActionID: "crash", ActionDigest: "crash-digest", Kind: control.ActionCrash},
		},
	}
	hint := func(action FrontierActionRef) ConsensusActionHint {
		return ConsensusActionHint{
			ActionID: action.ActionID, ActionDigest: action.ActionDigest,
			ActorRole: ConsensusSemanticUnknown, MessageClass: ConsensusSemanticUnknown,
			EpochRelation: ConsensusSemanticUnknown, OperationState: ConsensusSemanticUnknown,
		}
	}
	semantics := ScenarioSemanticExposure{
		Mode: ScenarioSemanticExposureFull, PrefixTraceDigest: frontier.PrefixTraceDigest,
		SnapshotDigest: frontier.SnapshotDigest,
		ActionHints: []ConsensusActionHint{hint(frontier.Actions[0]), hint(frontier.Actions[1]),
			hint(frontier.Actions[2]), hint(frontier.Actions[3]), hint(frontier.Actions[4])},
	}
	projected, projectedSemantics, err := scenarioStrategicAgentView(frontier, semantics)
	if err != nil || len(projected.Actions) != 2 || len(projectedSemantics.ActionHints) != 2 ||
		projected.Actions[0].ActionID != "drop" || projected.Actions[1].ActionID != "crash" ||
		len(frontier.Actions) != 5 || len(semantics.ActionHints) != 5 {
		t.Fatalf("strategic projection drifted: %#v/%#v/%v", projected, projectedSemantics, err)
	}
	natural := ScenarioPlan{ID: "natural", Steps: []ScenarioStep{{
		ID: "deliver", Selector: FrontierActionSelector{Kind: control.ActionDeliverMessage},
	}}}
	if issue := scenarioStrategicPlanIssue(natural, projected, true); issue == nil ||
		issue.Code != ScenarioProposalIssueActionNotStrategic {
		t.Fatalf("semantic natural Action escaped the strategic boundary: %#v", issue)
	}
	forgedExact := ScenarioPlan{ID: "forged", Steps: []ScenarioStep{{
		ID: "deliver", Selector: FrontierActionSelector{ActionID: "deliver"},
	}}}
	if issue := scenarioStrategicPlanIssue(forgedExact, projected, true); issue == nil ||
		issue.Code != ScenarioProposalIssueActionNotStrategic {
		t.Fatalf("hidden natural ActionID escaped the strategic boundary: %#v", issue)
	}
	strategic := ScenarioPlan{ID: "strategic", Steps: []ScenarioStep{{
		ID: "drop", Selector: FrontierActionSelector{ActionID: "drop"},
	}}}
	if issue := scenarioStrategicPlanIssue(strategic, projected, true); issue != nil {
		t.Fatalf("visible strategic Action was rejected: %#v", issue)
	}
}

func TestScenarioStrategicSelectorDoesNotEnumerateTargetMessageRoles(t *testing.T) {
	predicate := semantic.ObservationPredicate{
		Kind: semantic.ObservationMessageDropped,
		Constraints: []semantic.ObservationConstraint{
			{Field: semantic.ObservationFieldMessageRole, Equals: "target-local-operation-role"},
			{Field: semantic.ObservationFieldOperationStage, Equals: ConsensusOperationInflight},
		},
	}
	selector, ok := scenarioStrategicPredicateSelector(predicate, semantic.RiskWitnessResult{})
	if !ok || selector.Kind != control.ActionDropMessage ||
		selector.OperationState != ConsensusOperationInflight ||
		selector.MessageClass != "" || selector.MessageTypeHint != "" {
		t.Fatalf("target role leaked into the protocol-neutral selector: %#v", selector)
	}
	predicate.Constraints = predicate.Constraints[:1]
	selector, ok = scenarioStrategicPredicateSelector(predicate, semantic.RiskWitnessResult{})
	if !ok || selector.MessageTypeHint != "target-local-operation-role" {
		t.Fatalf("unqualified concrete message hint was discarded: %#v", selector)
	}
}

func TestScenarioNaturalProgressPrefersExactItemDependencyBeforeParticipantFallback(t *testing.T) {
	dependency := control.ItemID("timer-cause")
	participantOnly := FrontierActionRef{
		ActionID: "participant-first", ActionDigest: "participant-digest",
		Kind:          control.ActionDeliverMessage,
		MessageSource: control.NodeRef{Node: "n1", Incarnation: 1}, MessageTarget: "n2",
	}
	dependent := FrontierActionRef{
		ActionID: "dependent-later", ActionDigest: "dependent-digest",
		Kind:          control.ActionDeliverMessage,
		MessageSource: control.NodeRef{Node: "n3", Incarnation: 1}, MessageTarget: "n4",
		Dependencies: []control.ItemID{dependency},
	}
	focus := newScenarioCausalProgressFocus(FrontierActionRef{
		ActionID: "fire", Kind: control.ActionFireTemporal,
		Node: control.NodeRef{Node: "n1", Incarnation: 1}, ItemID: dependency,
	})
	selected, ok := scenarioNaturalProgressActionWithFocus(
		[]FrontierActionRef{participantOnly, dependent}, focus,
	)
	if !ok || selected.ActionID != dependent.ActionID {
		t.Fatalf("exact causal dependency did not precede participant fallback: %#v", selected)
	}
}

func TestScenarioBootstrapProgressYieldsOnlyOnTrustedSemanticChange(t *testing.T) {
	rootRisk := semantic.RiskWitnessResult{}
	currentRisk := semantic.RiskWitnessResult{}
	root := ScenarioSemanticExposure{Coordination: &ConsensusCoordinationStatus{
		Status: ConsensusCoordinatorAbsent,
	}}
	unchanged := ScenarioSemanticExposure{Coordination: &ConsensusCoordinationStatus{
		Status: ConsensusCoordinatorAbsent,
	}}
	candidate := ScenarioSemanticExposure{Coordination: &ConsensusCoordinationStatus{
		Status: ConsensusCoordinatorAmbiguous,
		ElectionProgress: ConsensusElectionProgress{
			CandidateNodes: []control.NodeID{"n1"}, TermOrBallotChanged: true,
		},
	}}
	newFaultChoice := []FrontierActionRef{{
		ActionID: "drop", ActionDigest: "drop-digest", Kind: control.ActionDropMessage,
	}}
	if scenarioPublicProgressShouldYield(
		rootRisk, currentRisk, map[string]struct{}{}, newFaultChoice, &root, &unchanged,
		scenarioAutomaticProgressGoal{},
	) {
		t.Fatal("bootstrap progress yielded merely because ordinary traffic exposed a fault control")
	}
	if scenarioPublicProgressShouldYield(
		rootRisk, currentRisk, map[string]struct{}{}, newFaultChoice, &root, &candidate,
		scenarioAutomaticProgressGoal{},
	) {
		t.Fatal("bootstrap progress yielded for ordinary candidate/term progress")
	}
	if !scenarioPublicProgressShouldYield(
		rootRisk, currentRisk, map[string]struct{}{}, newFaultChoice, &root, &candidate,
		scenarioAutomaticProgressGoal{yieldForStrategic: true},
	) {
		t.Fatal("bootstrap progress did not return a newly exposed strategic intervention")
	}
	present := ScenarioSemanticExposure{Coordination: &ConsensusCoordinationStatus{
		Status: ConsensusCoordinatorPresent, CoordinatorNode: "n1", InvokeReady: true,
	}}
	if !scenarioPublicProgressShouldYield(
		rootRisk, currentRisk, map[string]struct{}{}, newFaultChoice, &root, &present,
		scenarioAutomaticProgressGoal{},
	) {
		t.Fatal("bootstrap progress did not yield when trusted coordination changed")
	}
	if scenarioPublicProgressShouldYield(
		rootRisk, currentRisk, map[string]struct{}{}, newFaultChoice, &root, &present,
		scenarioAutomaticProgressGoal{autoInvoke: true, targetMilestone: "invoke"},
	) {
		t.Fatal("operation Risk yielded before typed automatic Invoke could run")
	}
}

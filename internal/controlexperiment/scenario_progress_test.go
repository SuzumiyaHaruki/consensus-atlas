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
		scenarioAutomaticProgressGoal{autoInvoke: true, invokeMilestone: "invoke"},
	) {
		t.Fatal("operation Risk yielded before typed automatic Invoke could run")
	}
}

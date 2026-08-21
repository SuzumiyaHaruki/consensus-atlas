package controlexperiment

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
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
		Kind: control.ActionFireTemporal,
		Node: control.NodeRef{Node: "n1", Incarnation: 1},
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
	newFaultChoice := []FrontierActionRef{{
		ActionID: "drop", ActionDigest: "drop-digest", Kind: control.ActionDropMessage,
	}}
	if scenarioPublicProgressShouldYield(
		rootRisk, currentRisk, map[string]struct{}{}, newFaultChoice, &root, &unchanged,
	) {
		t.Fatal("bootstrap progress yielded merely because ordinary traffic exposed a fault control")
	}
	present := ScenarioSemanticExposure{Coordination: &ConsensusCoordinationStatus{
		Status: ConsensusCoordinatorPresent, CoordinatorNode: "n1", InvokeReady: true,
	}}
	if !scenarioPublicProgressShouldYield(
		rootRisk, currentRisk, map[string]struct{}{}, newFaultChoice, &root, &present,
	) {
		t.Fatal("bootstrap progress did not yield when trusted coordination changed")
	}
}

func TestScenarioClosureSelectionRejectsInterventionsAndForeignActions(t *testing.T) {
	allowed := []control.ActionKind{
		control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionFireTemporal,
	}
	for _, kind := range allowed {
		action := FrontierActionRef{
			ActionID: control.ActionID("allowed-" + kind), ActionDigest: "digest-" + string(kind),
			Kind: kind,
		}
		selected, ok, stop, frontier, _, err := scenarioClosureAction(
			RiskFrontierView{Actions: []FrontierActionRef{action}},
			func(ActionFrontierView) (ScenarioClosureSelection, error) {
				return ScenarioClosureSelection{Status: ScenarioClosureSelected, Action: action}, nil
			},
		)
		if err != nil || !ok || stop != "" || frontier != nil || selected.ActionID != action.ActionID {
			t.Fatalf("allowed closure action %s rejected: %#v/%t/%q/%#v/%v",
				kind, selected, ok, stop, frontier, err)
		}
	}

	for _, kind := range []control.ActionKind{control.ActionDropMessage, control.ActionCrash} {
		action := FrontierActionRef{
			ActionID: control.ActionID("forbidden-" + kind), ActionDigest: "digest-" + string(kind),
			Kind: kind,
		}
		_, _, _, _, _, err := scenarioClosureAction(
			RiskFrontierView{Actions: []FrontierActionRef{action}},
			func(ActionFrontierView) (ScenarioClosureSelection, error) {
				return ScenarioClosureSelection{Status: ScenarioClosureSelected, Action: action}, nil
			},
		)
		if err == nil || err.Error() != "EXPERIMENT_SCENARIO_CLOSURE_ACTION_KIND_FORBIDDEN" {
			t.Fatalf("closure intervention %s was not rejected: %v", kind, err)
		}
	}

	enabled := FrontierActionRef{
		ActionID: "enabled-delivery", ActionDigest: "enabled-digest", Kind: control.ActionDeliverMessage,
	}
	foreign := enabled
	foreign.ActionID = "foreign-delivery"
	_, _, _, _, _, err := scenarioClosureAction(
		RiskFrontierView{Actions: []FrontierActionRef{enabled}},
		func(ActionFrontierView) (ScenarioClosureSelection, error) {
			return ScenarioClosureSelection{Status: ScenarioClosureSelected, Action: foreign}, nil
		},
	)
	if err == nil || err.Error() != "EXPERIMENT_SCENARIO_CLOSURE_ACTION_NOT_ADMISSIBLE" {
		t.Fatalf("foreign closure action was not rejected: %v", err)
	}

	mutable := FrontierActionRef{
		ActionID: "mutable-drop", ActionDigest: "mutable-drop-digest", Kind: control.ActionDropMessage,
	}
	view := RiskFrontierView{Actions: []FrontierActionRef{mutable}}
	_, _, _, _, _, err = scenarioClosureAction(
		view,
		func(frontier ActionFrontierView) (ScenarioClosureSelection, error) {
			frontier.Actions[0].Kind = control.ActionDeliverMessage
			return ScenarioClosureSelection{
				Status: ScenarioClosureSelected, Action: frontier.Actions[0],
			}, nil
		},
	)
	if err == nil || err.Error() != "EXPERIMENT_SCENARIO_CLOSURE_ACTION_KIND_FORBIDDEN" ||
		view.Actions[0].Kind != control.ActionDropMessage {
		t.Fatalf("selector mutation bypassed the authoritative frontier: %#v/%v", view, err)
	}
}

func TestScenarioClosureUnderdeterminedCandidatesAreAuthoritativeAndNarrow(t *testing.T) {
	first := FrontierActionRef{
		ActionID: "candidate-a", ActionDigest: "digest-a", Kind: control.ActionDeliverMessage,
	}
	second := FrontierActionRef{
		ActionID: "candidate-b", ActionDigest: "digest-b", Kind: control.ActionCompleteEffect,
	}
	unrelated := FrontierActionRef{
		ActionID: "unrelated", ActionDigest: "digest-c", Kind: control.ActionFireTemporal,
	}
	_, ok, stop, frontier, candidates, err := scenarioClosureAction(
		RiskFrontierView{Actions: []FrontierActionRef{first, second, unrelated}},
		func(ActionFrontierView) (ScenarioClosureSelection, error) {
			return ScenarioClosureSelection{
				Status: ScenarioClosureUnderdetermined, Candidates: []FrontierActionRef{first, second},
			}, nil
		},
	)
	if err != nil || ok || stop != ScenarioProgressClosureUnderdetermined || frontier == nil ||
		len(frontier.Actions) != 3 || len(candidates) != 2 ||
		candidates[0].ActionID != first.ActionID || candidates[1].ActionID != second.ActionID {
		t.Fatalf("narrow closure candidates were not preserved: %#v/%#v/%v", frontier, candidates, err)
	}

	foreign := first
	foreign.ActionID = "foreign"
	_, _, _, _, _, err = scenarioClosureAction(
		RiskFrontierView{Actions: []FrontierActionRef{first}},
		func(ActionFrontierView) (ScenarioClosureSelection, error) {
			return ScenarioClosureSelection{
				Status: ScenarioClosureUnderdetermined, Candidates: []FrontierActionRef{foreign},
			}, nil
		},
	)
	if err == nil || err.Error() != "EXPERIMENT_SCENARIO_CLOSURE_CANDIDATE_NOT_ADMISSIBLE" {
		t.Fatalf("foreign closure candidate was not rejected: %v", err)
	}
}

func TestScenarioClosureNormalStopsPreserveCurrentFrontier(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "636c6f737572652d73746f7073", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-closure-stop-risk", "fixture-cft", "closure-stop",
		[]string{"temporal-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := actionKindSemanticProjector{}
	rootRisk, err := projector.Project("fixture-closure-stop-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name      string
		selection string
		wantStop  string
	}{
		{name: "underdetermined", selection: ScenarioClosureUnderdetermined,
			wantStop: ScenarioProgressClosureUnderdetermined},
		{name: "no-eligible", selection: ScenarioClosureNoEligible,
			wantStop: ScenarioProgressClosureQuiescent},
	} {
		t.Run(test.name, func(t *testing.T) {
			var originalFirstKind control.ActionKind
			result, err := ExecuteScenarioNaturalProgressWithClosure(
				ctx, "fixture-closure-"+test.name, 4, spec, rootRisk, root,
				runtimeConfig, nil, factory, projector,
				func(frontier ActionFrontierView) (ScenarioClosureSelection, error) {
					if test.selection == ScenarioClosureUnderdetermined && len(frontier.Actions) < 2 {
						return ScenarioClosureSelection{}, errors.New("fixture frontier is not ambiguous")
					}
					if test.selection == ScenarioClosureUnderdetermined {
						originalFirstKind = frontier.Actions[0].Kind
						frontier.Actions[0].Kind = control.ActionDropMessage
						if originalFirstKind == control.ActionDropMessage {
							frontier.Actions[0].Kind = control.ActionCrash
						}
					}
					return ScenarioClosureSelection{Status: test.selection}, nil
				},
			)
			if err != nil || result.StopReason != test.wantStop || result.Frontier == nil ||
				result.Frontier.Digest == "" || len(result.Execution.Steps) != 0 ||
				result.Execution.FinalTrace.Digest != root.Digest ||
				result.Execution.FinalRisk.Digest != rootRisk.Digest {
				t.Fatalf("closure stop did not preserve feedback: %#v/%v", result, err)
			}
			sealed, sealErr := result.Frontier.seal()
			if sealErr != nil || sealed.Digest != result.Frontier.Digest ||
				test.selection == ScenarioClosureUnderdetermined &&
					result.Frontier.Actions[0].Kind != originalFirstKind {
				t.Fatalf("closure stop returned selector-mutated frontier: %#v/%v", result.Frontier, sealErr)
			}
		})
	}
}

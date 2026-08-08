package controlexperiment

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestPolicyUsesExactRuleBeforeFallbackPriority(t *testing.T) {
	policy := Policy{
		Version: PolicyVersion, ID: "lifecycle",
		Rules:    []DecisionRule{{Decision: 1, Kind: control.ActionCrash, Node: "n2"}},
		Priority: []control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal},
	}
	if err := policy.Validate(2); err != nil {
		t.Fatal(err)
	}
	actions := []control.Action{
		{ID: "timer", Kind: control.ActionFireTemporal},
		{ID: "crash-n1", Kind: control.ActionCrash, Node: control.NodeRef{Node: "n1", Incarnation: 1}},
		{ID: "crash-n2", Kind: control.ActionCrash, Node: control.NodeRef{Node: "n2", Incarnation: 1}},
	}
	selected, err := policy.selectAction(1, actions)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != "crash-n2" {
		t.Fatalf("rule selected %s, want crash-n2", selected.ID)
	}
	selected, err = policy.selectAction(2, actions)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != "timer" {
		t.Fatalf("fallback selected %s, want timer", selected.ID)
	}
}

func TestConfigRejectsOptionalReplayAndDuplicateRun(t *testing.T) {
	config := Config{
		SchemaVersion: SchemaVersion, ID: "test", PSSID: "pss", DecisionsPerRun: 2,
		Runtime: RuntimeConfig{SeedHex: "01", MaxClones: 1},
		Runs: []RunPlan{{Run: 1, Policy: Policy{
			Version: PolicyVersion, ID: "progress", Priority: []control.ActionKind{control.ActionFireTemporal},
		}}},
	}
	if err := config.Validate(); err == nil || err.Error() != "EXPERIMENT_REPLAY_REQUIRED" {
		t.Fatalf("Validate() error = %v, want replay requirement", err)
	}
	config.RequireReplay = true
	config.Runs = append(config.Runs, config.Runs[0])
	if err := config.Validate(); err == nil || err.Error() != "EXPERIMENT_RUN_INVALID: 1" {
		t.Fatalf("Validate() error = %v, want duplicate run", err)
	}
}

func TestRandomPolicyIsSeededAndDeterministic(t *testing.T) {
	actions := []control.Action{
		{ID: "a", Kind: control.ActionCrash},
		{ID: "b", Kind: control.ActionDeliverMessage},
		{ID: "c", Kind: control.ActionFireTemporal},
	}
	left := Policy{Version: RandomPolicyVersion, ID: "left", SeedHex: "01"}
	right := Policy{Version: RandomPolicyVersion, ID: "right", SeedHex: "02"}
	if err := left.Validate(32); err != nil {
		t.Fatal(err)
	}
	invalid := left
	invalid.Priority = []control.ActionKind{control.ActionCrash}
	if err := invalid.Validate(32); err == nil || err.Error() != "EXPERIMENT_RANDOM_POLICY_INVALID" {
		t.Fatalf("Validate() error = %v, want mixed-policy rejection", err)
	}
	var leftIDs, replayIDs, rightIDs []control.ActionID
	for decision := 1; decision <= 32; decision++ {
		leftAction, err := left.selectAction(decision, actions)
		if err != nil {
			t.Fatal(err)
		}
		replayAction, _ := left.selectAction(decision, actions)
		rightAction, _ := right.selectAction(decision, actions)
		leftIDs = append(leftIDs, leftAction.ID)
		replayIDs = append(replayIDs, replayAction.ID)
		rightIDs = append(rightIDs, rightAction.ID)
	}
	if !reflect.DeepEqual(leftIDs, replayIDs) {
		t.Fatal("same random policy seed changed the selection sequence")
	}
	if reflect.DeepEqual(leftIDs, rightIDs) {
		t.Fatal("different random policy seeds produced the same selection sequence")
	}
}

func TestActionClassRandomDoesNotWeightClassesByActionMultiplicity(t *testing.T) {
	actions := []control.Action{{ID: "deliver", Kind: control.ActionDeliverMessage}}
	for index := 0; index < 15; index++ {
		actions = append(actions, control.Action{
			ID: control.ActionID(fmt.Sprintf("drop-%02d", index)), Kind: control.ActionDropMessage,
		})
	}
	policy := Policy{Version: ActionClassPolicyVersion, ID: "class-random", SeedHex: "01"}
	if err := policy.Validate(256); err != nil {
		t.Fatal(err)
	}
	deliveries := 0
	var first []control.ActionID
	for decision := 1; decision <= 256; decision++ {
		selected, err := policy.selectAction(decision, actions)
		if err != nil {
			t.Fatal(err)
		}
		repeated, _ := policy.selectAction(decision, actions)
		if repeated.ID != selected.ID {
			t.Fatalf("decision %d changed from %s to %s", decision, selected.ID, repeated.ID)
		}
		if selected.Kind == control.ActionDeliverMessage {
			deliveries++
		}
		if decision <= 8 {
			first = append(first, selected.ID)
		}
	}
	if deliveries < 96 || deliveries > 160 {
		t.Fatalf("deliver class selected %d/256 times, want approximately half", deliveries)
	}
	reordered := append([]control.Action(nil), actions...)
	slices.Reverse(reordered)
	for decision, want := range first {
		selected, err := policy.selectAction(decision+1, reordered)
		if err != nil || selected.ID != want {
			t.Fatalf("reordered decision %d = %s/%v, want %s", decision+1, selected.ID, err, want)
		}
	}
}

func TestActionClassRandomHonorsFrozenWorkloadPriority(t *testing.T) {
	policy := Policy{
		Version: ActionClassPolicyVersion, ID: "workload-class-random", SeedHex: "01",
		Priority: []control.ActionKind{control.ActionInvoke},
	}
	if err := policy.Validate(1); err != nil {
		t.Fatal(err)
	}
	selected, err := policy.selectAction(1, []control.Action{
		{ID: "drop", Kind: control.ActionDropMessage},
		{ID: "invoke", Kind: control.ActionInvoke},
	})
	if err != nil || selected.ID != "invoke" {
		t.Fatalf("selected = %s/%v, want frozen invoke", selected.ID, err)
	}
}

func TestAdjacentTraceMutationUsesExactSpliceAndDeclaredSuffix(t *testing.T) {
	trace, err := (controlruntime.Trace{Records: []controlruntime.ActionRecord{
		{Action: control.Action{ID: "a", Kind: control.ActionCompleteEffect}},
		{Action: control.Action{ID: "b", Kind: control.ActionDeliverMessage}},
		{Action: control.Action{ID: "c", Kind: control.ActionCompleteEffect}},
		{Action: control.Action{ID: "d", Kind: control.ActionFireTemporal}},
	}}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	source := Policy{
		Version: PolicyVersion, ID: "source",
		Priority: []control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal},
	}
	plan, err := NewAdjacentTraceMutation("swap-2-3", trace, source, 2)
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{Version: TraceMutationPolicyVersion, ID: "mutation", TraceMutation: &plan}
	if err := policy.Validate(4); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		decision int
		enabled  []control.Action
		want     control.ActionID
	}{
		{1, []control.Action{{ID: "a", Kind: control.ActionCompleteEffect}}, "a"},
		{2, []control.Action{{ID: "b", Kind: control.ActionDeliverMessage}, {ID: "c", Kind: control.ActionCompleteEffect}}, "c"},
		{3, []control.Action{{ID: "b", Kind: control.ActionDeliverMessage}}, "b"},
		{4, []control.Action{{ID: "x", Kind: control.ActionFireTemporal}, {ID: "y", Kind: control.ActionDeliverMessage}}, "y"},
	}
	for _, test := range tests {
		selected, err := policy.selectAction(test.decision, test.enabled)
		if err != nil || selected.ID != test.want {
			t.Fatalf("decision %d selected %s/%v, want %s", test.decision, selected.ID, err, test.want)
		}
	}
	_, err = policy.selectAction(2, []control.Action{{ID: "b", Kind: control.ActionDeliverMessage}})
	var selection *policySelectionError
	if !errors.As(err, &selection) || selection.failureCode() != "EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED" {
		t.Fatalf("missing swapped action error = %v", err)
	}
	tampered := plan
	tampered.FirstActionID = "changed"
	if err := tampered.Validate(4); err == nil || err.Error() != "EXPERIMENT_TRACE_MUTATION_DIGEST_MISMATCH" {
		t.Fatalf("tampered plan error = %v", err)
	}
}

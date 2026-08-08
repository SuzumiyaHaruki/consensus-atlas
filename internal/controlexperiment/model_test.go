package controlexperiment

import (
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
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

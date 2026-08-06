package testplan_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/explore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

func TestConcretizeAddsDeterministicBootstrapAndPreservesBounds(t *testing.T) {
	profile := fixtureProfile(coverage.StatusSupported)
	suite := validSuite(t, profile)
	if err := suite.Validate(profile); err != nil {
		t.Fatal(err)
	}
	planBefore, _ := json.Marshal(suite.Plans[0])
	concrete, err := testplan.Concretize(profile, suite.Plans[0])
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []scenario.Step{
		{Op: "inject", Kind: core.EventStart, Target: "n1"},
		{Op: "inject", Kind: core.EventStart, Target: "n2"},
		{Op: "run", Count: testplan.BootstrapDrainLimit},
	}
	if !reflect.DeepEqual(concrete.Setup.Steps[:3], wantPrefix) {
		t.Fatalf("trusted bootstrap = %#v, want %#v", concrete.Setup.Steps[:3], wantPrefix)
	}
	last := concrete.Setup.Steps[len(concrete.Setup.Steps)-1]
	if last.Kind != core.EventCampaign || last.Target != "n1" {
		t.Fatalf("stimulus was not concretized last: %#v", last)
	}
	if concrete.Search.Config.DecisionBudget != 2 || concrete.Targets[0] != "target" {
		t.Fatalf("concretizer altered search or targets: %#v", concrete)
	}
	planAfter, _ := json.Marshal(suite.Plans[0])
	if string(planBefore) != string(planAfter) {
		t.Fatal("concretizer mutated the untrusted plan")
	}
}

func TestSuiteRejectsUnknownUnsupportedAndExcessBudgetTargets(t *testing.T) {
	profile := fixtureProfile(coverage.StatusSupported)
	suite := validSuite(t, profile)
	suite.Plans[0].Targets = []string{"unknown"}
	if err := suite.Validate(profile); err == nil {
		t.Fatal("suite accepted an unknown coverage target")
	}

	unsupported := fixtureProfile(coverage.StatusUnsupported)
	suite = validSuite(t, unsupported)
	if err := suite.Validate(unsupported); err == nil {
		t.Fatal("suite accepted an unsupported target as actionable debt")
	}

	suite = validSuite(t, profile)
	suite.MaxTotalDecisions = 1
	if err := suite.Validate(profile); err == nil {
		t.Fatal("suite accepted plans above its frozen global decision budget")
	}
}

func TestSuiteRejectsControlBoundaryEscapes(t *testing.T) {
	profile := fixtureProfile(coverage.StatusSupported)
	suite := validSuite(t, profile)
	suite.Plans[0].Stimuli[0].Kind = core.EventMessage
	if err := suite.Validate(profile); err == nil {
		t.Fatal("suite accepted direct message injection")
	}

	suite = validSuite(t, profile)
	suite.Plans[0].Prepare = []testplan.Action{{
		Op:    testplan.OpExecute,
		Match: &scenario.Selector{ID: "e000001", Kind: core.EventCampaign},
	}}
	if err := suite.Validate(profile); err == nil {
		t.Fatal("suite accepted a transient event ID selector")
	}

	suite = validSuite(t, profile)
	suite.Plans[0].Prepare = []testplan.Action{{Op: testplan.OpPartition, Groups: [][]string{{"n1"}, {}}}}
	if err := suite.Validate(profile); err == nil {
		t.Fatal("suite accepted an incomplete partition")
	}

	suite = validSuite(t, profile)
	suite.Plans[0].Prepare = []testplan.Action{{
		Op: testplan.OpExecute, Match: &scenario.Selector{Kind: core.EventKind("agent-invented")},
	}}
	if err := suite.Validate(profile); err == nil {
		t.Fatal("suite accepted an invented event kind")
	}

	suite = validSuite(t, profile)
	suite.Plans[0].Prepare = []testplan.Action{{
		Op: testplan.OpCaptureMessage, Ref: "old-message", Match: &scenario.Selector{Kind: core.EventMessage},
	}, {
		Op: testplan.OpExecuteRef, Ref: "old-message",
	}}
	if err := suite.Validate(profile); err != nil {
		t.Fatalf("suite rejected a selector-derived message reference: %v", err)
	}

	suite = validSuite(t, profile)
	suite.Plans[0].Prepare = []testplan.Action{{Op: testplan.OpExecuteRef, Ref: "not-captured"}}
	if err := suite.Validate(profile); err == nil {
		t.Fatal("suite accepted a reference before capture")
	}

	suite = validSuite(t, profile)
	suite.Plans[0].Prepare = []testplan.Action{{
		Op: testplan.OpCaptureMessage, Ref: "not-message", Match: &scenario.Selector{Kind: core.EventCampaign},
	}}
	if err := suite.Validate(profile); err == nil {
		t.Fatal("suite captured a non-message event")
	}

	suite = validSuite(t, profile)
	suite.Plans[0].Prepare = []testplan.Action{{
		Op: testplan.OpExecuteOptional, Match: &scenario.Selector{Kind: core.EventPersist, Target: "n1"},
	}}
	if err := suite.Validate(profile); err != nil {
		t.Fatalf("suite rejected optional host work: %v", err)
	}

	suite = validSuite(t, profile)
	suite.Plans[0].Prepare = []testplan.Action{{
		Op: testplan.OpExecuteOptional, Match: &scenario.Selector{Kind: core.EventMessage, Target: "n1"},
	}}
	if err := suite.Validate(profile); err == nil {
		t.Fatal("suite accepted optional message delivery")
	}
}

func TestValidateInputCapabilitiesUsesDriverDeclaredVocabulary(t *testing.T) {
	plan := testplan.Plan{
		ID: "generic-input",
		Stimuli: []testplan.Input{{
			Kind: core.EventProtocolInput, Operation: "submit", Target: "n1",
			Payload: json.RawMessage(`{"value":"x"}`),
		}},
	}
	manifest := driver.Manifest{Inputs: []driver.InputCapability{{
		ID: "submit", Kind: core.EventProtocolInput, PayloadMode: "required",
	}}}
	if err := testplan.ValidateInputCapabilities(plan, manifest); err != nil {
		t.Fatalf("declared generic input rejected: %v", err)
	}
	plan.Stimuli[0].Operation = "invented"
	if err := testplan.ValidateInputCapabilities(plan, manifest); err == nil {
		t.Fatal("undeclared generic input was accepted")
	}
	plan.Stimuli[0].Operation = "submit"
	plan.Stimuli[0].Payload = nil
	if err := testplan.ValidateInputCapabilities(plan, manifest); err == nil {
		t.Fatal("required input payload was accepted as absent")
	}
}

func validSuite(t *testing.T, profile coverage.Profile) testplan.Suite {
	t.Helper()
	digest, err := coverage.Digest(profile)
	if err != nil {
		t.Fatal(err)
	}
	return testplan.Suite{
		Version: testplan.Version, ID: "fixture-suite", ProfileID: profile.ID, ProfileDigest: digest,
		MaxTotalRuns: 2, MaxTotalDecisions: 2,
		Plans: []testplan.Plan{{
			ID: "fixture-plan", Targets: []string{"target"},
			Prepare: []testplan.Action{{Op: testplan.OpAdvance, Ticks: 1}},
			Stimuli: []testplan.Input{{Kind: core.EventCampaign, Target: "n1"}},
			Search: testplan.Search{
				Strategy: explore.StrategyDFS,
				Config:   explore.Config{Runs: 2, BudgetPerRun: 1, DecisionBudget: 2},
			},
		}},
	}
}

func fixtureProfile(status string) coverage.Profile {
	predicate := coverage.TracePredicate{EventKind: core.EventCampaign, ObservationLabel: "covered"}
	return coverage.Profile{
		Version: coverage.ProfileVersion, ID: "profile", Protocol: "fixture", Nodes: []string{"n2", "n1"},
		Coverage: coverage.CoverageDefinition{
			Weights: map[string]float64{"transition": 1},
			Obligations: []coverage.Obligation{{
				ID: "target", Category: "transition", Description: "target",
				Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}},
				Monitors: []string{"trace-integrity"}, Risk: coverage.RiskHigh, Status: status,
			}},
		},
		Threshold: coverage.Threshold{Score: 80, MinCategory: 0.5},
	}
}

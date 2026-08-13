package controlexperiment

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestA4ScenarioPlanConcretizesTwoLifecycleStepsAndReturnsMechanicalFailures(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "61342d7363656e6172696f2d706c616e", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	envelope := &FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	frontier, _, err := ReconstructActionFrontierView(
		ctx, "fixture-a4-root", root, len(root.Records), runtimeConfig, envelope, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	var crash FrontierActionRef
	for _, action := range frontier.Actions {
		if action.Kind == control.ActionCrash {
			crash = action
			break
		}
	}
	if crash.ActionID == "" {
		t.Fatalf("fixture root has no crash action: %#v", frontier.Actions)
	}
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-a4-risk", "fixture-cft", "crash-restart",
		[]string{"crash-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := actionKindSemanticProjector{}
	rootRisk, err := projector.Project("fixture-a4-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	plan := ScenarioPlan{ID: "fixture-a4-crash-restart", Steps: []ScenarioStep{
		{ID: "crash-current", Selector: FrontierActionSelector{ActionID: crash.ActionID}},
		{ID: "restart-node", Selector: FrontierActionSelector{Kind: control.ActionRestart, Node: crash.Node.Node}},
	}}
	result, err := ExecuteBoundedScenarioPlan(
		ctx, "fixture-a4-execution", plan, 2, spec, rootRisk, root,
		runtimeConfig, envelope, factory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScenarioStatusCompleted || len(result.Steps) != 2 ||
		result.Steps[0].Outcome != ScenarioStepApplied || result.Steps[1].Outcome != ScenarioStepApplied ||
		result.Steps[1].Choice == nil || result.Steps[1].Choice.Action.Kind != control.ActionRestart ||
		len(result.FinalTrace.Records) != len(root.Records)+2 || result.Work.ChildVerification.WorkUnits == 0 {
		t.Fatalf("two-step scenario did not execute through trusted frontiers: %#v", result)
	}
	policy, err := CompileScenarioPolicy(
		"fixture-a4-qualified-policy", root, result,
		[]control.ActionKind{control.ActionFireTemporal},
	)
	if err != nil || len(policy.Rules) != len(result.FinalTrace.Records) ||
		policy.Rules[len(policy.Rules)-1].ActionID != result.Steps[1].Choice.Action.ActionID {
		t.Fatalf("successful scenario did not compile to exact policy: %#v/%v", policy, err)
	}
	tamperedExecution := result
	tamperedChoice := *tamperedExecution.Steps[1].Choice
	tamperedChoice.Action.ActionID = "not-the-executed-action"
	tamperedExecution.Steps[1].Choice = &tamperedChoice
	if _, err := CompileScenarioPolicy("fixture-a4-tampered", root, tamperedExecution, nil); err == nil {
		t.Fatal("scenario choice detached from the executed Trace compiled to a policy")
	}

	noMatch := ScenarioPlan{ID: "fixture-a4-no-match", Steps: []ScenarioStep{{
		ID: "restart-running", Selector: FrontierActionSelector{Kind: control.ActionRestart},
	}}}
	noMatchResult, err := ExecuteBoundedScenarioPlan(
		ctx, "fixture-a4-no-match-execution", noMatch, 1, spec, rootRisk, root,
		runtimeConfig, envelope, factory, projector,
	)
	if err != nil || noMatchResult.Status != ScenarioStatusStopped || len(noMatchResult.Steps) != 1 ||
		noMatchResult.Steps[0].ReasonCode != ScenarioReasonNoMatch || noMatchResult.Steps[0].MatchCount != 0 ||
		len(noMatchResult.FinalTrace.Records) != len(root.Records) {
		t.Fatalf("no-match feedback is not mechanical: %#v/%v", noMatchResult, err)
	}

	ambiguous := ScenarioPlan{ID: "fixture-a4-ambiguous", Steps: []ScenarioStep{{
		ID: "any-crash", Selector: FrontierActionSelector{Kind: control.ActionCrash},
	}}}
	ambiguousResult, err := ExecuteBoundedScenarioPlan(
		ctx, "fixture-a4-ambiguous-execution", ambiguous, 1, spec, rootRisk, root,
		runtimeConfig, envelope, factory, projector,
	)
	if err != nil || ambiguousResult.Status != ScenarioStatusStopped || len(ambiguousResult.Steps) != 1 ||
		ambiguousResult.Steps[0].ReasonCode != ScenarioReasonAmbiguous ||
		ambiguousResult.Steps[0].MatchCount < 2 {
		t.Fatalf("ambiguous feedback is not mechanical: %#v/%v", ambiguousResult, err)
	}

	budgetResult, err := ExecuteBoundedScenarioPlan(
		ctx, "fixture-a4-budget-execution", plan, 1, spec, rootRisk, root,
		runtimeConfig, envelope, factory, projector,
	)
	if err != nil || budgetResult.Status != ScenarioStatusStopped || len(budgetResult.Steps) != 2 ||
		budgetResult.Steps[0].Outcome != ScenarioStepApplied ||
		budgetResult.Steps[1].ReasonCode != ScenarioReasonBudgetExhausted ||
		len(budgetResult.FinalTrace.Records) != len(root.Records)+1 {
		t.Fatalf("external step budget was not enforced: %#v/%v", budgetResult, err)
	}
}

func TestA4ScenarioPlanParsingRejectsUnknownAuthorityAndMixedSelector(t *testing.T) {
	valid := []byte(`{"id":"fixture-plan","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}`)
	if plan, err := ParseScenarioPlan(valid); err != nil || plan.Steps[0].Selector.Node != "n1" {
		t.Fatalf("valid scenario plan rejected: %#v/%v", plan, err)
	}
	unknown := []byte(`{"id":"fixture-plan","steps":[{"id":"crash","selector":{"kind":"crash"}}],"verdict":"safe"}`)
	if _, err := ParseScenarioPlan(unknown); err == nil {
		t.Fatal("Agent-authored verdict field was accepted")
	}
	mixed := ScenarioPlan{ID: "fixture-mixed", Steps: []ScenarioStep{{
		ID: "mixed", Selector: FrontierActionSelector{ActionID: "action-1", Kind: control.ActionCrash},
	}}}
	if err := mixed.Validate(); err == nil {
		t.Fatal("exact ActionID mixed with semantic selector fields was accepted")
	}
}

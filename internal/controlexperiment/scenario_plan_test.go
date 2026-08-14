package controlexperiment

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
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

func TestScenarioAgentCommitsVerifiedPrefixBeforeRepair(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "61342d7363656e6172696f2d707265666978", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	envelope := &FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-scenario-prefix-risk", "fixture-cft", "repair-prefix",
		[]string{"crash-prefix", "temporal-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := actionKindSemanticProjector{}
	rootRisk, err := projector.Project("fixture-scenario-prefix-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err := ReconstructRiskFrontierState(
		ctx, "fixture-scenario-prefix-frontier", spec, rootRisk, root, len(root.Records),
		runtimeConfig, envelope, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	semantics := unknownScenarioSemantics(t, frontier)
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-scenario-prefix-knowledge", Family: spec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{ID: "repair", Text: "Continue from each verified execution prefix."}},
		Risks: []ProtocolRisk{{
			ID: spec.RiskID, Summary: "Exercise repair after a partially applicable plan.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionCrash, control.ActionRestart},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"fixture-scenario-prefix-hypothesis", knowledge, spec,
		"Repair only the rejected suffix while retaining the verified prefix.", ScenarioPlanningBackendID,
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := ExploreScenarioWithPlanner(
		ctx, 2, 2, 2, knowledge, hypothesis, spec, frontier, semantics, rootRisk, root,
		runtimeConfig, envelope, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			calls++
			var plan ScenarioPlan
			switch calls {
			case 1:
				var crash FrontierActionRef
				for _, action := range view.Frontier.Actions {
					if action.Kind == control.ActionCrash {
						crash = action
						break
					}
				}
				plan = ScenarioPlan{ID: "partial-plan", Steps: []ScenarioStep{
					{ID: "crash", Selector: FrontierActionSelector{ActionID: crash.ActionID}},
					{ID: "missing-restart", Selector: FrontierActionSelector{
						Kind: control.ActionRestart, Node: "missing-node",
					}},
				}}
			case 2:
				if view.Prior == nil || view.Prior.PreviousPlan == nil || view.Prior.FailedStep == nil ||
					view.Prior.FailedStep.ID != "missing-restart" ||
					view.Frontier.PrefixDecisions != len(root.Records)+1 {
					t.Fatalf("repair view did not retain the verified prefix: %#v", view)
				}
				var restart FrontierActionRef
				for _, action := range view.Frontier.Actions {
					if action.Kind == control.ActionRestart {
						restart = action
						break
					}
				}
				plan = ScenarioPlan{ID: "repaired-plan", Steps: []ScenarioStep{{
					ID: "restart", Selector: FrontierActionSelector{ActionID: restart.ActionID},
				}}}
			}
			encoded, marshalErr := json.Marshal(plan)
			return encoded, ModelWork{Calls: 1, InputTokens: 10, OutputTokens: 10, TotalTokens: 20}, marshalErr
		},
	)
	if err != nil || calls != 2 || result.Status != ScenarioAgentCompleted || result.Execution == nil ||
		len(result.Execution.Steps) != 2 || len(result.Execution.FinalTrace.Records) != len(root.Records)+2 ||
		result.Execution.Steps[0].Choice == nil ||
		result.Execution.Steps[0].Choice.Action.Kind != control.ActionCrash ||
		result.Execution.Steps[1].Choice == nil ||
		result.Execution.Steps[1].Choice.Action.Kind != control.ActionRestart ||
		len(result.Attempts) != 2 || result.Attempts[0].Execution == nil ||
		result.Attempts[0].Execution.Status != ScenarioStatusStopped ||
		len(result.Attempts[0].Execution.Steps) != 2 {
		t.Fatalf("verified prefix was not committed across repair: %#v calls=%d err=%v", result, calls, err)
	}
}

func unknownScenarioSemantics(t *testing.T, frontier RiskFrontierView) ScenarioSemanticExposure {
	t.Helper()
	hints := make([]ConsensusActionHint, len(frontier.Actions))
	for index, action := range frontier.Actions {
		hints[index] = ConsensusActionHint{
			ActionID: action.ActionID, ActionDigest: action.ActionDigest,
			ActorRole: ConsensusSemanticUnknown, MessageClass: ConsensusSemanticUnknown,
			EpochRelation: ConsensusSemanticUnknown, OperationState: ConsensusOperationNone,
		}
	}
	exposure, err := NewScenarioSemanticExposure(ScenarioSemanticExposureFull, frontier, hints)
	if err != nil {
		t.Fatal(err)
	}
	return exposure
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

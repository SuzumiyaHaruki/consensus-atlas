package controlexperiment

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type fixtureSemanticPrefixProjector struct {
	preferred control.ActionID
}

func (fixtureSemanticPrefixProjector) ID() string { return "fixture-semantic-prefix-projector-v1" }

func (projector fixtureSemanticPrefixProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	milestones := make([]semantic.RiskWitnessMilestoneEvidence, 0, 1)
	for _, record := range trace.Records {
		if record.Action.ID != projector.preferred {
			continue
		}
		digest, err := control.CanonicalDigest(record)
		if err != nil {
			return semantic.RiskWitnessResult{}, err
		}
		milestones = append(milestones, semantic.RiskWitnessMilestoneEvidence{
			MilestoneID: "preferred-prefix", Step: record.Step,
			Kind: "trace-action", EvidenceDigest: digest,
		})
		break
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, projector.ID(), milestones,
	)
}

type actionKindSemanticProjector struct{}

func (actionKindSemanticProjector) ID() string { return "fixture-action-kind-projector-v1" }

func (actionKindSemanticProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	seen := make(map[string]bool)
	milestones := make([]semantic.RiskWitnessMilestoneEvidence, 0, 2)
	for _, record := range trace.Records {
		milestoneID := ""
		switch record.Action.Kind {
		case control.ActionFireTemporal:
			milestoneID = "temporal-prefix"
		case control.ActionCrash:
			milestoneID = "crash-prefix"
		}
		if milestoneID == "" || seen[milestoneID] {
			continue
		}
		digest, err := control.CanonicalDigest(record)
		if err != nil {
			return semantic.RiskWitnessResult{}, err
		}
		seen[milestoneID] = true
		milestones = append(milestones, semantic.RiskWitnessMilestoneEvidence{
			MilestoneID: milestoneID, Step: record.Step,
			Kind: "trace-action", EvidenceDigest: digest,
		})
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, actionKindSemanticProjector{}.ID(), milestones,
	)
}

// clientTerminalFixtureAdapter adds one deterministic client result to an
// otherwise ordinary fixture Invoke. It keeps this regression focused on the
// coordinator's terminal semantics without changing the production fixture.
type clientTerminalFixtureAdapter struct {
	*fixture.Adapter
	pendingInvoke *control.AdapterCommand
	responses     map[control.YieldID]control.ProducedItem
}

func newClientTerminalFixtureAdapter() *clientTerminalFixtureAdapter {
	return &clientTerminalFixtureAdapter{
		Adapter: fixture.New(), responses: make(map[control.YieldID]control.ProducedItem),
	}
}

func (adapter *clientTerminalFixtureAdapter) Reset(ctx context.Context, seed []byte) error {
	adapter.pendingInvoke = nil
	adapter.responses = make(map[control.YieldID]control.ProducedItem)
	return adapter.Adapter.Reset(ctx, seed)
}

func (adapter *clientTerminalFixtureAdapter) Submit(ctx context.Context, command control.AdapterCommand) error {
	if command.Kind == control.ActionInvoke {
		copyCommand := command
		adapter.pendingInvoke = &copyCommand
	}
	return adapter.Adapter.Submit(ctx, command)
}

func (adapter *clientTerminalFixtureAdapter) RunUntilYield(ctx context.Context) (control.Yield, error) {
	yield, err := adapter.Adapter.RunUntilYield(ctx)
	command := adapter.pendingInvoke
	adapter.pendingInvoke = nil
	if err != nil || command == nil {
		return yield, err
	}
	payload, err := control.NewPayload(
		"consensus-atlas/fixture-client-result/v1", "json", []byte(`{"ok":true}`),
	)
	if err != nil {
		return control.Yield{}, err
	}
	itemID, err := control.StableID("fixture-client-result", string(command.ID))
	if err != nil {
		return control.Yield{}, err
	}
	adapter.responses[yield.ID] = control.ProducedItem{
		ID: control.ItemID(itemID), Kind: control.ItemClientResult, Owner: command.Node,
		Response: &control.ClientResponse{
			RequestID: string(command.ID), Owner: command.Node, Status: "ok", Payload: payload,
		},
	}
	return yield, nil
}

func (adapter *clientTerminalFixtureAdapter) Collect(
	ctx context.Context,
	yieldID control.YieldID,
) (control.Emission, error) {
	emission, err := adapter.Adapter.Collect(ctx, yieldID)
	if err != nil {
		return control.Emission{}, err
	}
	response, ok := adapter.responses[yieldID]
	if !ok {
		return emission, nil
	}
	emission.Items = append(emission.Items, response)
	return emission.Seal()
}

func TestScenarioPlanConcretizesTwoLifecycleStepsAndReturnsMechanicalFailures(t *testing.T) {
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
		ctx, "fixture-a4-execution", plan, 2, 2, spec, rootRisk, root,
		runtimeConfig, envelope, factory, projector, 0,
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
	if result.Work.FrontierReconstruction.SetupAttempts != 1 ||
		result.Work.ChildMaterialization.SetupAttempts != 0 ||
		result.Work.ChildMaterialization.PrepareActions != 0 ||
		result.Work.ChildMaterialization.SchedulerDecisions != 2 ||
		result.Work.ChildMaterialization.WorkUnits != 2 ||
		result.Work.ChildVerification.SetupAttempts != 1 ||
		result.Work.ChildVerification.SchedulerDecisions != len(result.FinalTrace.Records) {
		t.Fatalf("scenario did not retain one live branch and one promotion replay: %#v", result.Work)
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
		ctx, "fixture-a4-no-match-execution", noMatch, 1, 1, spec, rootRisk, root,
		runtimeConfig, envelope, factory, projector, 0,
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
		ctx, "fixture-a4-ambiguous-execution", ambiguous, 1, 1, spec, rootRisk, root,
		runtimeConfig, envelope, factory, projector, 0,
	)
	if err != nil || ambiguousResult.Status != ScenarioStatusStopped || len(ambiguousResult.Steps) != 1 ||
		ambiguousResult.Steps[0].ReasonCode != ScenarioReasonAmbiguous ||
		ambiguousResult.Steps[0].MatchCount < 2 ||
		len(ambiguousResult.Steps[0].SelectorTrace) != 1 ||
		ambiguousResult.Steps[0].SelectorTrace[0].Field != "kind" ||
		ambiguousResult.Steps[0].SelectorTrace[0].CandidateCount != ambiguousResult.Steps[0].MatchCount {
		t.Fatalf("ambiguous feedback is not mechanical: %#v/%v", ambiguousResult, err)
	}
	unknownMilestone := ScenarioPlan{ID: "fixture-a9e4-unknown-milestone", Steps: []ScenarioStep{{
		ID: "wait-unknown", AfterMilestone: "unknown-milestone",
		Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"},
	}}}
	unknownResult, err := ExecuteBoundedScenarioPlan(
		ctx, "fixture-a9e4-unknown-execution", unknownMilestone, 1, 1, spec, rootRisk, root,
		runtimeConfig, envelope, factory, projector, 0,
	)
	if err != nil || unknownResult.Status != ScenarioStatusStopped || len(unknownResult.Steps) != 1 ||
		unknownResult.Steps[0].ReasonCode != ScenarioReasonMilestoneUnknown ||
		len(unknownResult.FinalTrace.Records) != len(root.Records) {
		t.Fatalf("unknown milestone feedback is not mechanical: %#v/%v", unknownResult, err)
	}

	budgetResult, err := ExecuteBoundedScenarioPlan(
		ctx, "fixture-a4-budget-execution", plan, 1, 1, spec, rootRisk, root,
		runtimeConfig, envelope, factory, projector, 0,
	)
	if err != nil || budgetResult.Status != ScenarioStatusStopped || len(budgetResult.Steps) != 2 ||
		budgetResult.Steps[0].Outcome != ScenarioStepApplied ||
		budgetResult.Steps[1].ReasonCode != ScenarioReasonBudgetExhausted ||
		len(budgetResult.FinalTrace.Records) != len(root.Records)+1 {
		t.Fatalf("external step budget was not enforced: %#v/%v", budgetResult, err)
	}
}

func TestScenarioNaturalProgressUsesOneLiveBranchAndOnePromotionReplay(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "61342d6c6976652d6272616e6368", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-live-branch-risk", "fixture-cft", "natural-progress",
		[]string{"temporal-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := actionKindSemanticProjector{}
	rootRisk, err := projector.Project("fixture-live-branch-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteScenarioNaturalProgress(
		ctx, "fixture-live-branch", 8, spec, rootRisk, root,
		runtimeConfig, nil, factory, projector,
	)
	if err != nil || result.StopReason != ScenarioProgressBudget ||
		len(result.Execution.Steps) != 8 ||
		len(result.Execution.FinalTrace.Records) != len(root.Records)+8 ||
		result.Execution.Work.FrontierReconstruction.SetupAttempts != 1 ||
		result.Execution.Work.FrontierReconstruction.RuntimeInitializations != 1 ||
		result.Execution.Work.ChildMaterialization.SchedulerDecisions != 8 ||
		result.Execution.Work.ChildVerification.SetupAttempts != 1 ||
		result.Execution.Work.ChildVerification.RuntimeInitializations != 1 ||
		result.Execution.Work.ChildVerification.SchedulerDecisions != len(result.Execution.FinalTrace.Records) {
		t.Fatalf("natural progress did not retain one live branch and one promotion replay: %#v/%v",
			result, err)
	}
	delta, err := NewScenarioProgressDelta(
		spec, rootRisk, root, result.Execution.FinalRisk, result.Execution.FinalTrace,
	)
	if err != nil || delta.Decisions != 8 || len(delta.RecentActions) != 8 ||
		len(delta.NewMilestones) != 1 || delta.UniqueStateTransitions == 0 ||
		delta.MilestoneProgress != ScenarioMilestoneProgressReached ||
		len(delta.NewMilestoneEvidence) != 1 || len(delta.ActionCounts) == 0 ||
		delta.TemporalCallbacks == 0 || delta.LogicalClockAdvances > delta.TemporalCallbacks ||
		repeatedScenarioPatternDepth([]string{"a", "b", "a", "b", "a", "b"}) != 2 {
		t.Fatalf("live branch did not produce compact progress feedback: %#v/%v", delta, err)
	}
}

func TestScenarioAgentLongInvestigationReturnsPeriodicCompactFeedback(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "6139653464332d6c6f6e672d7472616365", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-long-investigation-risk", "fixture-cft", "periodic-feedback",
		[]string{"never-reached"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := fixtureSemanticPrefixProjector{preferred: "never-selected"}
	rootRisk, err := projector.Project("fixture-long-investigation-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err := ReconstructRiskFrontierState(
		ctx, "fixture-long-investigation-frontier", spec, rootRisk, root, len(root.Records),
		runtimeConfig, nil, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-long-investigation-knowledge", Family: spec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{
			ID: "periodic-feedback", Text: "A periodic natural-time loop must return bounded planning feedback.",
		}},
		Risks: []ProtocolRisk{{
			ID: spec.RiskID, Summary: "Observe a long periodic loop without inventing a verdict.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionFireTemporal},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"fixture-long-investigation-hypothesis", knowledge, spec,
		"Use repeated deterministic feedback to decide whether to continue.", ScenarioPlanningBackendID,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantRemaining := []int{256, 191, 126, 62}
	wantAllowance := []int{65, 65, 64, 62}
	wantDelta := []int{65, 65, 64, 62}
	calls := 0
	result, err := ExploreScenarioWithPlanner(
		ctx, 4, 1, 256, knowledge, hypothesis, spec, frontier,
		unknownScenarioSemantics(t, frontier), rootRisk, root, runtimeConfig, nil,
		nil, nil, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			if view.RemainingDecisions != wantRemaining[calls] ||
				view.DecisionAllowance != wantAllowance[calls] || view.MaxSteps != 1 {
				t.Fatalf("long investigation allowance drifted at call %d: %#v", calls+1, view)
			}
			if calls > 0 {
				if view.Prior == nil || view.Prior.ProgressDelta == nil ||
					view.Prior.ProgressDelta.Decisions != wantDelta[calls-1] ||
					view.Prior.ProgressDelta.RepeatedPatternDepth == 0 ||
					view.Prior.ProgressDelta.MilestoneProgress != ScenarioMilestoneProgressRepeated ||
					view.Prior.ProgressDelta.TemporalCallbacks == 0 ||
					view.Prior.ProgressDelta.LogicalClockAdvances > view.Prior.ProgressDelta.TemporalCallbacks ||
					len(view.Prior.ProgressDelta.ActionCounts) == 0 ||
					len(view.Prior.ProgressDelta.RecentActions) != scenarioProgressRecentActions {
					t.Fatalf("long loop did not return compact adaptive feedback at call %d: %#v",
						calls+1, view.Prior)
				}
			}
			var temporal FrontierActionRef
			for _, action := range view.Frontier.Actions {
				if action.Kind == control.ActionFireTemporal {
					temporal = action
					break
				}
			}
			if temporal.ActionID == "" {
				t.Fatalf("periodic temporal Action disappeared at call %d", calls+1)
			}
			calls++
			encoded, marshalErr := json.Marshal(ScenarioInvestigationProposal{
				Intent: ScenarioIntentContinue,
				Plan: ScenarioPlan{
					ID: fmt.Sprintf("long-plan-%02d", calls),
					Steps: []ScenarioStep{{
						ID:       fmt.Sprintf("fire-%02d", calls),
						Selector: FrontierActionSelector{ActionID: temporal.ActionID},
					}},
				},
			})
			return encoded, ModelWork{}, marshalErr
		},
	)
	if err != nil || calls != 4 || result.Status != ScenarioAgentCompleted ||
		result.Execution == nil || len(result.Execution.FinalTrace.Records) != 256 ||
		len(result.Attempts) != 4 || result.Attempts[3].Feedback.ProgressDelta == nil ||
		result.Attempts[3].Feedback.ProgressDelta.Decisions != 62 ||
		result.StopReason != ScenarioAgentStopDecisionBudget || result.DecisionsUsed != 256 ||
		result.SelectedPathDecisions != 256 || result.BranchExplorationDecisions != 0 ||
		result.ExecutionWork.ChildMaterialization.SchedulerDecisions != 256 ||
		result.ExecutionWork.FrontierReconstruction.SetupAttempts > 12 ||
		result.ExecutionWork.ChildVerification.SetupAttempts > 8 {
		decisions := 0
		if result.Execution != nil {
			decisions = len(result.Execution.FinalTrace.Records)
		}
		t.Fatalf("long investigation did not stay bounded and replayable: status=%s calls=%d attempts=%d decisions=%d reconstruction=%d verification_setups=%d verification_decisions=%d work=%d err=%v",
			result.Status, calls, len(result.Attempts), decisions,
			result.ExecutionWork.FrontierReconstruction.SetupAttempts,
			result.ExecutionWork.ChildVerification.SetupAttempts,
			result.ExecutionWork.ChildVerification.SchedulerDecisions,
			result.ExecutionWork.TotalWorkUnits, err)
	}
	t.Logf("256 actions: reconstruction=%d verification_setups=%d verification_decisions=%d work=%d",
		result.ExecutionWork.FrontierReconstruction.SetupAttempts,
		result.ExecutionWork.ChildVerification.SetupAttempts,
		result.ExecutionWork.ChildVerification.SchedulerDecisions,
		result.ExecutionWork.TotalWorkUnits)
}

func TestScenarioAfterMilestoneSharesStrategicLiveBranchAndPromotionReplay(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "61342d61667465722d6d696c6573746f6e65", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-after-milestone-risk", "fixture-cft", "wait-then-crash",
		[]string{"temporal-prefix", "crash-prefix"},
		[]semantic.RiskWitnessOrder{{Before: "temporal-prefix", After: "crash-prefix"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := actionKindSemanticProjector{}
	rootRisk, err := projector.Project("fixture-after-milestone-root", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	plan := ScenarioPlan{ID: "wait-then-crash", Steps: []ScenarioStep{{
		ID: "crash-after-timer", AfterMilestone: "temporal-prefix",
		Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"},
	}}}
	result, err := ExecuteBoundedScenarioPlan(
		ctx, "wait-then-crash", plan, 1, 4, spec, rootRisk, root,
		runtimeConfig, &FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		factory, projector, 0,
	)
	if err != nil || result.Status != ScenarioStatusCompleted ||
		len(result.AutomaticProgress) != 1 || len(result.Steps) != 1 ||
		result.FinalRisk.Status != semantic.RiskWitnessReached ||
		result.Work.FrontierReconstruction.SetupAttempts != 1 ||
		result.Work.ChildMaterialization.SchedulerDecisions != 2 ||
		result.Work.ChildVerification.SetupAttempts != 1 ||
		result.Work.ChildVerification.SchedulerDecisions != len(result.FinalTrace.Records) {
		t.Fatalf("after_milestone and strategic step did not share one live branch: %#v/%v", result, err)
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
		runtimeConfig, envelope, nil, nil, factory, projector,
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
				if view.Prior == nil || view.Prior.PreviousProposal == nil || view.Prior.FailedStep == nil ||
					view.Prior.FailedStep.ID != "missing-restart" ||
					len(view.Prior.Steps) != 2 || view.Prior.Steps[1].MatchCount != 0 ||
					len(view.Prior.Steps[1].SelectorTrace) != 2 ||
					view.Prior.Steps[1].SelectorTrace[0].Field != "kind" ||
					view.Prior.Steps[1].SelectorTrace[0].CandidateCount != 1 ||
					view.Prior.Steps[1].SelectorTrace[1] != (ScenarioSelectorFilter{
						Field: "node", Requested: "missing-node", CandidateCount: 0,
					}) ||
					view.Prior.ProgressDelta == nil || view.Prior.ProgressDelta.Decisions != 1 ||
					len(view.Prior.ProgressDelta.NewMilestones) != 1 ||
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
			intent := ScenarioIntentContinue
			if calls == 2 {
				intent = ScenarioIntentRevise
			}
			encoded, marshalErr := json.Marshal(ScenarioInvestigationProposal{Intent: intent, Plan: plan})
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

func TestScenarioAgentCanAbandonAfterMechanicalProgressFeedback(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "61342d636f6e74696e75652d696e74656e74", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	envelope := &FaultEnvelope{MaxCrashes: 2, MaxConcurrentCrashes: 2}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-continue-risk", "fixture-cft", "continue-after-complete-plan",
		[]string{"preferred-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := fixtureSemanticPrefixProjector{preferred: "never-selected"}
	rootRisk, err := projector.Project("fixture-continue-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err := ReconstructRiskFrontierState(
		ctx, "fixture-continue-frontier", spec, rootRisk, root, len(root.Records),
		runtimeConfig, envelope, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-continue-knowledge", Family: spec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{ID: "continue", Text: "A completed plan is not a reached hypothesis."}},
		Risks: []ProtocolRisk{{
			ID: spec.RiskID, Summary: "Continue while the witness and both budgets remain open.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionCrash},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"fixture-continue-hypothesis", knowledge, spec,
		"Continue after a valid plan when its witness remains missing.", ScenarioPlanningBackendID,
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := ExploreScenarioWithPlanner(
		ctx, 2, 2, 4, knowledge, hypothesis, spec, frontier,
		unknownScenarioSemantics(t, frontier), rootRisk, root, runtimeConfig, envelope,
		nil, nil, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			calls++
			if calls == 2 {
				if view.Prior == nil || view.Prior.ProgressDelta == nil ||
					view.Prior.ProgressDelta.Decisions != 2 ||
					view.Prior.NaturalProgressStop != ScenarioProgressQuiescent ||
					view.Frontier.Progress.FirstMissingMilestone != "preferred-prefix" ||
					!containsString(view.AvailableIntents, ScenarioIntentAbandon) {
					t.Fatalf("abandon call did not receive mechanical progress feedback: %#v", view)
				}
				return []byte(`{"intent":"abandon"}`), ModelWork{}, nil
			}
			encoded, marshalErr := json.Marshal(ScenarioInvestigationProposal{
				Intent: ScenarioIntentContinue,
				Plan: ScenarioPlan{ID: "crash-both", Steps: []ScenarioStep{
					{ID: "crash-n1", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"}},
					{ID: "crash-n2", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n2"}},
				}},
			})
			return encoded, ModelWork{}, marshalErr
		},
	)
	if err != nil || calls != 2 || result.Execution == nil || result.Status != ScenarioAgentCompleted ||
		result.StopReason != ScenarioAgentStopHypothesisAbandoned || result.DecisionsUsed != 2 ||
		len(result.Execution.Steps) != 2 || result.Execution.FinalRisk.Status == semantic.RiskWitnessReached ||
		result.Attempts[1].Feedback.ReasonCode != ScenarioAgentStopHypothesisAbandoned {
		t.Fatalf("Agent could not stop a low-yield hypothesis after trusted feedback: %#v calls=%d err=%v",
			result, calls, err)
	}
}

func TestScenarioSelectorTraceExplainsNoMatchAndAmbiguity(t *testing.T) {
	actions := []FrontierActionRef{
		{ActionID: "deliver-1", Kind: control.ActionDeliverMessage,
			MessageSource: control.NodeRef{Node: "n1", Incarnation: 1}, MessageTarget: "n2"},
		{ActionID: "deliver-2", Kind: control.ActionDeliverMessage,
			MessageSource: control.NodeRef{Node: "n2", Incarnation: 1}, MessageTarget: "n3"},
		{ActionID: "crash-1", Kind: control.ActionCrash,
			Node: control.NodeRef{Node: "n1", Incarnation: 1}},
	}
	semantics := ScenarioSemanticExposure{ActionHints: []ConsensusActionHint{
		{ActionID: "deliver-1", ActorRole: ConsensusActorLeader, MessageClass: ConsensusMessageReplication},
		{ActionID: "deliver-2", ActorRole: ConsensusActorReplica, MessageClass: ConsensusMessageVote},
		{ActionID: "crash-1", ActorRole: ConsensusActorLeader, MessageClass: ConsensusSemanticUnknown},
	}}
	noMatch := scenarioSelectorTrace(actions, semantics, FrontierActionSelector{
		Kind: control.ActionDeliverMessage, MessageSource: "n1", MessageTarget: "n3",
	})
	wantNoMatch := []ScenarioSelectorFilter{
		{Field: "kind", Requested: string(control.ActionDeliverMessage), CandidateCount: 2},
		{Field: "message_source", Requested: "n1", CandidateCount: 1},
		{Field: "message_target", Requested: "n3", CandidateCount: 0},
	}
	if !reflect.DeepEqual(noMatch, wantNoMatch) {
		t.Fatalf("no-match conflict was not localized: %#v", noMatch)
	}
	ambiguous := scenarioSelectorTrace(actions, semantics, FrontierActionSelector{Kind: control.ActionDeliverMessage})
	wantAmbiguous := []ScenarioSelectorFilter{{
		Field: "kind", Requested: string(control.ActionDeliverMessage), CandidateCount: 2,
	}}
	if !reflect.DeepEqual(ambiguous, wantAmbiguous) {
		t.Fatalf("ambiguous selector count was not retained: %#v", ambiguous)
	}
	semanticMatch := scenarioMatches(actions, semantics, FrontierActionSelector{
		Kind: control.ActionDeliverMessage, MessageClass: ConsensusMessageReplication,
	})
	if len(semanticMatch) != 1 || semanticMatch[0].ActionID != "deliver-1" {
		t.Fatalf("generic message_class did not select the bound Action: %#v", semanticMatch)
	}
}

func TestM4eScenarioProposalsRemainTypedRegressionInputs(t *testing.T) {
	responses := []struct {
		intent string
		steps  int
		json   string
	}{
		{ScenarioIntentContinue, 3, `{"intent":"continue","plan":{"id":"timer-symmetry-recovery-lapse-continue","steps":[{"id":"drop-vote-to-n2","selector":{"action_id":"action-8f4e6a98217dab22b0bdc21114b319ca26b1934a9f861104c3d2e8d58741d69b"}},{"id":"fire-n2-pulse","after_milestone":"follower-silenced","selector":{"kind":"fire-temporal-event","node":"n2","temporal_kind":"periodic-pulse"}},{"id":"fire-n1-pulse-after-handoff","after_milestone":"leadership-handoff","selector":{"kind":"fire-temporal-event","node":"n1","temporal_kind":"periodic-pulse"}}]}}`},
		{ScenarioIntentRevise, 4, `{"intent":"revise","plan":{"id":"timer-symmetry-recovery-lapse-revise-2","steps":[{"id":"deliver-vote-to-n3","selector":{"action_id":"action-4e8fa0db2b121921fcd9fc69fbbb9acabb399703ffeb0f8ab637be17f9b89b26"}},{"id":"deliver-replication-to-n2","selector":{"kind":"deliver-message","node":"n2","item_kind":"message","message_source":"n1","message_target":"n2"}},{"id":"invoke-client-at-n1","selector":{"kind":"invoke","node":"n1"}},{"id":"fire-n2-pulse","selector":{"kind":"fire-temporal-event","node":"n2","temporal_kind":"periodic-pulse"}}]}}`},
		{ScenarioIntentRevise, 4, `{"intent":"revise","plan":{"id":"timer-symmetry-recovery-lapse-revise-3","steps":[{"id":"deliver-replication-to-n3","selector":{"action_id":"action-55a1a01c2ee286dc5b8c3daf3ea76d93b5ecc6ca790a6793de8cfa5317a52c98"}},{"id":"deliver-vote-to-n1","selector":{"kind":"deliver-message","node":"n1","item_kind":"message","message_source":"n3","message_target":"n1"}},{"id":"invoke-client-at-n1","selector":{"kind":"invoke","node":"n1"}},{"id":"drop-replication-to-n2","selector":{"kind":"drop-message","node":"n2","item_kind":"message","message_source":"n1","message_target":"n2"}}]}}`},
	}
	for index, response := range responses {
		proposal, err := ParseScenarioInvestigationProposal([]byte(response.json))
		if err != nil || proposal.Intent != response.intent || len(proposal.Plan.Steps) != response.steps {
			t.Fatalf("M4e proposal %d no longer crosses the typed parser: %#v err=%v", index+1, proposal, err)
		}
	}
}

func TestClientTerminalContinuesToPlannerWhileRiskAndBudgetRemain(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "61342d636c69656e742d7465726d696e616c", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	envelope := &FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}
	factory := func() (control.Adapter, error) { return newClientTerminalFixtureAdapter(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-client-terminal-risk", "fixture-cft", "continue-after-client-terminal",
		[]string{"preferred-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := fixtureSemanticPrefixProjector{preferred: "never-selected"}
	rootRisk, err := projector.Project("fixture-client-terminal-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err := ReconstructRiskFrontierState(
		ctx, "fixture-client-terminal-frontier", spec, rootRisk, root, len(root.Records),
		runtimeConfig, envelope, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-client-terminal-knowledge", Family: spec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{
			ID: "client-terminal", Text: "A returned client operation does not end an unfinished investigation.",
		}},
		Risks: []ProtocolRisk{{
			ID: spec.RiskID, Summary: "Continue with strategic actions after the workload returns.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionInvoke, control.ActionCrash},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"fixture-client-terminal-hypothesis", knowledge, spec,
		"Continue after client-terminal while the witness remains missing.", ScenarioPlanningBackendID,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpOneShot, Delay: 1})
	if err != nil {
		t.Fatal(err)
	}
	preparer := func(
		prepareCtx context.Context,
		selector FrontierActionSelector,
		_ controlruntime.Trace,
		runtime *controlruntime.Runtime,
	) (control.ActionID, bool, error) {
		if selector.Kind != control.ActionInvoke || selector.ActionID != "" || selector.Node != "n1" {
			return "", false, nil
		}
		id, offerErr := runtime.OfferInvoke(prepareCtx, "n1", payload)
		return id, offerErr == nil, offerErr
	}
	calls := 0
	result, err := ExploreScenarioWithPlanner(
		ctx, 3, 1, 10, knowledge, hypothesis, spec, frontier,
		unknownScenarioSemantics(t, frontier), rootRisk, root, runtimeConfig, envelope,
		nil, nil, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			calls++
			wantAllowance := []int{5, 6, 8}
			if view.DecisionAllowance != wantAllowance[calls-1] {
				t.Fatalf("remaining decision budget was not rebalanced at call %d: got %d want %d",
					calls, view.DecisionAllowance, wantAllowance[calls-1])
			}
			if calls == 3 {
				return []byte(`{"not":"a-proposal"}`), ModelWork{}, nil
			}
			plan := ScenarioPlan{ID: "invoke-client", Steps: []ScenarioStep{{
				ID: "invoke", Selector: FrontierActionSelector{Kind: control.ActionInvoke, Node: "n1"},
			}}}
			if calls == 2 {
				if view.Prior == nil || view.Prior.ProgressDelta == nil ||
					view.Prior.ProgressDelta.Decisions != 1 ||
					view.Prior.ProgressDelta.MilestoneProgress != ScenarioMilestoneProgressStalled ||
					view.Prior.ProgressDelta.NaturalProgressStop != ScenarioProgressClientTerminal ||
					view.Prior.ProgressDelta.FaultAllowance == nil ||
					view.Prior.ProgressDelta.FaultAllowance.MaxCrashes != 1 ||
					view.Prior.ProgressDelta.FaultUsage == nil ||
					view.Prior.ProgressDelta.FaultUsage.Crashes != 0 ||
					view.Prior.ProgressDelta.FaultRemaining == nil ||
					view.Prior.ProgressDelta.FaultRemaining.Crashes != 1 ||
					!containsActionKind(view.Prior.ProgressDelta.AvailableInterventions, control.ActionCrash) ||
					view.Prior.NaturalProgressStop != ScenarioProgressClientTerminal ||
					view.Frontier.Progress.FirstMissingMilestone != "preferred-prefix" {
					t.Fatalf("client-terminal feedback did not reach the second planner call: %#v", view)
				}
				crashReachable := false
				for _, action := range view.Frontier.Actions {
					if action.Kind == control.ActionCrash {
						crashReachable = true
						break
					}
				}
				if !crashReachable {
					t.Fatalf("strategic crash action disappeared after client-terminal: %#v", view.Frontier.Actions)
				}
				plan = ScenarioPlan{ID: "crash-after-client", Steps: []ScenarioStep{{
					ID: "crash", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"},
				}}}
			}
			encoded, marshalErr := json.Marshal(ScenarioInvestigationProposal{
				Intent: ScenarioIntentContinue, Plan: plan,
			})
			return encoded, ModelWork{}, marshalErr
		},
		preparer,
	)
	if err != nil || calls != 3 || result.Execution == nil || len(result.Execution.Steps) != 2 ||
		result.Execution.Steps[0].Choice == nil ||
		result.Execution.Steps[0].Choice.Action.Kind != control.ActionInvoke ||
		result.Execution.Steps[1].Choice == nil ||
		result.Execution.Steps[1].Choice.Action.Kind != control.ActionCrash ||
		result.Execution.FinalRisk.Status == semantic.RiskWitnessReached {
		t.Fatalf("client-terminal ended investigation before strategic continuation: %#v calls=%d err=%v",
			result, calls, err)
	}
}

func TestScenarioMilestoneWaitDistinguishesClientTerminalAndQuiescent(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "6d34692d776169742d73746f702d726561736f6e", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-wait-stop-risk", "fixture-cft", "wait-stop-reason",
		[]string{"preferred-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := fixtureSemanticPrefixProjector{preferred: "never-selected"}
	rootRisk, err := projector.Project("fixture-wait-stop-root", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpOneShot, Delay: 1})
	if err != nil {
		t.Fatal(err)
	}
	preparer := func(
		prepareCtx context.Context,
		selector FrontierActionSelector,
		_ controlruntime.Trace,
		runtime *controlruntime.Runtime,
	) (control.ActionID, bool, error) {
		if selector.Kind != control.ActionInvoke || selector.Node != "n1" {
			return "", false, nil
		}
		id, offerErr := runtime.OfferInvoke(prepareCtx, "n1", payload)
		return id, offerErr == nil, offerErr
	}
	clientResult, err := ExecuteBoundedScenarioPlan(
		ctx, "fixture-wait-client-terminal",
		ScenarioPlan{ID: "wait-client-terminal", Steps: []ScenarioStep{
			{ID: "invoke", Selector: FrontierActionSelector{Kind: control.ActionInvoke, Node: "n1"}},
			{ID: "wait", AfterMilestone: "preferred-prefix", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"}},
		}},
		2, 4, spec, rootRisk, root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		func() (control.Adapter, error) { return newClientTerminalFixtureAdapter(), nil },
		projector, 0, preparer,
	)
	if err != nil || clientResult.Status != ScenarioStatusStopped || len(clientResult.Steps) != 2 ||
		clientResult.Steps[1].ReasonCode != ScenarioReasonMilestoneWaitClientTerminal ||
		clientResult.NaturalProgressStop != ScenarioProgressClientTerminal {
		t.Fatalf("client terminal was not distinguished while waiting for a milestone: %#v/%v", clientResult, err)
	}

	quiescent, err := ExecuteBoundedScenarioPlan(
		ctx, "fixture-wait-quiescent",
		ScenarioPlan{ID: "wait-quiescent", Steps: []ScenarioStep{
			{ID: "crash-n1", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"}},
			{ID: "crash-n2", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n2"}},
			{ID: "wait", AfterMilestone: "preferred-prefix", Selector: FrontierActionSelector{Kind: control.ActionRestart, Node: "n1"}},
		}},
		3, 5, spec, rootRisk, root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 2, MaxConcurrentCrashes: 2},
		func() (control.Adapter, error) { return fixture.New(), nil }, projector, 0,
	)
	if err != nil || quiescent.Status != ScenarioStatusStopped || len(quiescent.Steps) != 3 ||
		quiescent.Steps[2].ReasonCode != ScenarioReasonMilestoneWaitQuiescent ||
		quiescent.NaturalProgressStop != ScenarioProgressQuiescent {
		t.Fatalf("quiescent frontier was not distinguished while waiting for a milestone: %#v/%v", quiescent, err)
	}
}

func containsActionKind(values []control.ActionKind, want control.ActionKind) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestScenarioInvestigationBranchesControlAblationAndPromotesChosenPath(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "613964322d6272616e63682d636f6e74726f6c", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	envelope := &FaultEnvelope{MaxCrashes: 2, MaxConcurrentCrashes: 2}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-investigation-risk", "fixture-cft", "branch-control-ablate",
		[]string{"never-reached"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := fixtureSemanticPrefixProjector{preferred: "never-selected"}
	rootRisk, err := projector.Project("fixture-investigation-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err := ReconstructRiskFrontierState(
		ctx, "fixture-investigation-frontier", spec, rootRisk, root, len(root.Records),
		runtimeConfig, envelope, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-investigation-knowledge", Family: spec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{ID: "comparison", Text: "Compare interventions from one checkpoint."}},
		Risks: []ProtocolRisk{{
			ID: spec.RiskID, Summary: "Exercise treatment, control and ablation from one root.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionCrash, control.ActionRestart},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"fixture-investigation-hypothesis", knowledge, spec,
		"Use a same-root comparison before choosing the continuation path.", ScenarioPlanningBackendID,
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := ExploreScenarioWithPlanner(
		ctx, 5, 2, 40, knowledge, hypothesis, spec, frontier,
		unknownScenarioSemantics(t, frontier), rootRisk, root, runtimeConfig, envelope,
		nil, nil, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			calls++
			var proposal ScenarioInvestigationProposal
			switch calls {
			case 1:
				if !reflect.DeepEqual(view.AvailableIntents, []string{ScenarioIntentContinue}) {
					t.Fatalf("unexpected initial investigation intents: %#v", view.AvailableIntents)
				}
				proposal = ScenarioInvestigationProposal{
					Intent: ScenarioIntentContinue,
					Plan: ScenarioPlan{ID: "establish-current-path", Steps: []ScenarioStep{
						{ID: "crash-path", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n2"}},
						{ID: "restart-path", Selector: FrontierActionSelector{Kind: control.ActionRestart, Node: "n2"}},
					}},
				}
			case 2:
				if !containsString(view.AvailableIntents, ScenarioIntentBranch) {
					t.Fatalf("successful current path did not unlock comparison: %#v", view.AvailableIntents)
				}
				proposal = ScenarioInvestigationProposal{
					Intent: ScenarioIntentBranch, BranchID: "treatment",
					Plan: ScenarioPlan{ID: "treatment-plan", Steps: []ScenarioStep{
						{ID: "crash-treatment", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"}},
						{ID: "restart-treatment", Selector: FrontierActionSelector{Kind: control.ActionRestart, Node: "n1"}},
					}},
				}
			case 3:
				if len(view.Branches) != 1 || view.Branches[0].ID != "treatment" ||
					view.Branches[0].RootDecision != view.Frontier.PrefixDecisions+1 ||
					view.Branches[0].FinalDecision <= view.Branches[0].RootDecision ||
					len(view.Branches[0].AppliedInterventions) != 2 ||
					view.Branches[0].AppliedInterventions[0].Action.Kind != control.ActionCrash ||
					view.Branches[0].AppliedInterventions[1].Action.Kind != control.ActionRestart ||
					len(view.Branches[0].AvailableActions) == 0 ||
					!containsString(view.AvailableIntents, ScenarioIntentControl) ||
					!containsString(view.AvailableIntents, ScenarioIntentAblate) {
					t.Fatalf("treatment checkpoint was not exposed mechanically: %#v", view)
				}
				proposal = ScenarioInvestigationProposal{
					Intent: ScenarioIntentControl, BranchID: "control", ReferenceBranchID: "treatment",
					Plan: ScenarioPlan{ID: "control-plan", Steps: []ScenarioStep{
						{ID: "crash-control", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n2"}},
						{ID: "restart-control", Selector: FrontierActionSelector{Kind: control.ActionRestart, Node: "n2"}},
					}},
				}
			case 4:
				if len(view.Branches) != 2 ||
					view.Branches[1].RootDecision != view.Branches[0].RootDecision {
					t.Fatalf("control did not use the treatment root checkpoint: %#v", view.Branches)
				}
				proposal = ScenarioInvestigationProposal{
					Intent: ScenarioIntentAblate, BranchID: "ablation", ReferenceBranchID: "treatment",
					OmittedStepIDs: []string{"restart-treatment"},
					Plan: ScenarioPlan{ID: "ablation-plan", Steps: []ScenarioStep{
						{ID: "crash-treatment", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"}},
					}},
				}
			case 5:
				if len(view.Branches) != 3 || view.Branches[2].Intent != ScenarioIntentAblate ||
					view.Branches[2].RootDecision != view.Branches[0].RootDecision ||
					view.Branches[0].RootDecision != view.Frontier.PrefixDecisions+1 || view.MaxSteps != 0 ||
					view.DecisionAllowance != 0 ||
					!reflect.DeepEqual(view.AvailableIntents, []string{ScenarioIntentSelect}) {
					t.Fatalf("ablation did not retain the same root: %#v", view.Branches)
				}
				proposal = ScenarioInvestigationProposal{
					Intent: ScenarioIntentSelect, FromBranchID: "treatment",
				}
			}
			encoded, marshalErr := json.Marshal(proposal)
			return encoded, ModelWork{}, marshalErr
		},
	)
	seedDecisions := 0
	selectedBranchDecisions := 0
	if len(result.Branches) > 0 {
		seedDecisions = result.Branches[0].RootDecision - (len(root.Records) + 1)
		selectedBranchDecisions = result.Branches[0].FinalDecision - result.Branches[0].RootDecision
	}
	if err != nil || calls != 5 || result.Status != ScenarioAgentCompleted ||
		len(result.Branches) != 3 || len(result.Attempts) != 5 || result.Execution == nil ||
		result.StopReason != ScenarioAgentStopPathSelected || seedDecisions <= 0 ||
		result.BranchExplorationDecisions <= 0 ||
		result.DecisionsUsed != seedDecisions+result.BranchExplorationDecisions ||
		result.SelectedPathDecisions != seedDecisions+selectedBranchDecisions ||
		len(result.Execution.FinalTrace.Records) != len(root.Records)+result.SelectedPathDecisions {
		t.Fatalf("branch comparison did not promote the chosen deterministic path: %#v calls=%d err=%v",
			result, calls, err)
	}

	unselectedCalls := 0
	unselected, err := ExploreScenarioWithPlanner(
		ctx, 3, 2, 32, knowledge, hypothesis, spec, frontier,
		unknownScenarioSemantics(t, frontier), rootRisk, root, runtimeConfig, envelope,
		nil, nil, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			unselectedCalls++
			if unselectedCalls > 2 {
				return []byte(`{"not":"a-proposal"}`), ModelWork{}, nil
			}
			proposal := ScenarioInvestigationProposal{
				Intent: ScenarioIntentContinue,
				Plan: ScenarioPlan{ID: "unselected-seed", Steps: []ScenarioStep{
					{ID: "seed-crash", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n2"}},
					{ID: "seed-restart", Selector: FrontierActionSelector{Kind: control.ActionRestart, Node: "n2"}},
				}},
			}
			if unselectedCalls == 2 {
				if !containsString(view.AvailableIntents, ScenarioIntentBranch) {
					t.Fatalf("successful seed path did not unlock unselected branch: %#v", view.AvailableIntents)
				}
				proposal = ScenarioInvestigationProposal{
					Intent: ScenarioIntentBranch, BranchID: "unselected-treatment",
					Plan: ScenarioPlan{ID: "unselected-plan", Steps: []ScenarioStep{
						{ID: "crash", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"}},
						{ID: "restart", Selector: FrontierActionSelector{Kind: control.ActionRestart, Node: "n1"}},
					}},
				}
			}
			encoded, marshalErr := json.Marshal(proposal)
			return encoded, ModelWork{}, marshalErr
		},
	)
	if err != nil || unselected.Status != ScenarioAgentCompleted ||
		unselected.StopReason != ScenarioAgentStopCallBudget ||
		unselected.Execution == nil || len(unselected.Branches) != 1 ||
		unselected.DecisionsUsed == 0 || unselected.BranchExplorationDecisions == 0 ||
		unselected.SelectedPathDecisions+unselected.BranchExplorationDecisions != unselected.DecisionsUsed {
		t.Fatalf("unselected experiment branch became final evidence: %#v err=%v", unselected, err)
	}
}

func TestScenarioBranchAtDecisionLimitRetainsRiskReachedCandidate(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "6c6173742d6272616e63682d7269736b", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"last-branch-risk", "fixture-cft", "last-branch-risk",
		[]string{"preferred-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	probeProjector := fixtureSemanticPrefixProjector{preferred: "not-selected"}
	probeRisk, err := probeProjector.Project("last-branch-probe", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err := ReconstructRiskFrontierState(
		ctx, "last-branch-frontier", spec, probeRisk, root, len(root.Records),
		runtimeConfig, nil, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	var crash FrontierActionRef
	var seedCrash FrontierActionRef
	for _, action := range frontier.Actions {
		if action.Kind == control.ActionCrash && action.Node.Node == "n1" {
			crash = action
		}
		if action.Kind == control.ActionCrash && action.Node.Node == "n2" {
			seedCrash = action
		}
	}
	if crash.ActionID == "" || seedCrash.ActionID == "" {
		t.Fatal("fixture did not expose two distinct crash Actions")
	}
	projector := fixtureSemanticPrefixProjector{preferred: crash.ActionID}
	rootRisk, err := projector.Project("last-branch-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err = ReconstructRiskFrontierState(
		ctx, "last-branch-frontier", spec, rootRisk, root, len(root.Records),
		runtimeConfig, nil, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "last-branch-knowledge", Family: spec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{ID: "last-branch", Text: "Retain a replayed final branch."}},
		Risks: []ProtocolRisk{{
			ID: spec.RiskID, Summary: "Reach the witness on the final branch Action.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionCrash},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"last-branch-hypothesis", knowledge, spec,
		"Preserve the final replayed candidate for independent Oracle evaluation.", ScenarioPlanningBackendID,
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := ExploreScenarioWithPlanner(
		ctx, 3, 1, 3, knowledge, hypothesis, spec, frontier,
		unknownScenarioSemantics(t, frontier), rootRisk, root, runtimeConfig, nil,
		nil, nil, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			calls++
			proposal := ScenarioInvestigationProposal{}
			switch calls {
			case 1:
				proposal = ScenarioInvestigationProposal{
					Intent: ScenarioIntentContinue,
					Plan: ScenarioPlan{ID: "establish-current-path", Steps: []ScenarioStep{{
						ID: "seed", Selector: FrontierActionSelector{ActionID: seedCrash.ActionID},
					}}},
				}
			case 2:
				if !containsString(view.AvailableIntents, ScenarioIntentBranch) {
					t.Fatalf("successful current path did not unlock final branch: %#v", view.AvailableIntents)
				}
				proposal = ScenarioInvestigationProposal{
					Intent: ScenarioIntentBranch, BranchID: "final-treatment",
					Plan: ScenarioPlan{ID: "final-treatment-plan", Steps: []ScenarioStep{{
						ID: "reach-risk", Selector: FrontierActionSelector{ActionID: crash.ActionID},
					}}},
				}
			case 3:
				if view.RemainingDecisions != 0 || view.DecisionAllowance != 0 || view.MaxSteps != 0 ||
					!reflect.DeepEqual(view.AvailableIntents, []string{ScenarioIntentSelect}) {
					t.Fatalf("final selection call was not reserved after decision exhaustion: %#v", view)
				}
				proposal = ScenarioInvestigationProposal{
					Intent: ScenarioIntentSelect, FromBranchID: "final-treatment",
				}
			}
			encoded, marshalErr := json.Marshal(proposal)
			return encoded, ModelWork{}, marshalErr
		},
	)
	if err != nil || calls != 3 || result.Execution == nil || result.DecisionsUsed != 3 ||
		result.StopReason != ScenarioAgentStopRiskReached || result.SelectedPathDecisions != 3 ||
		len(result.CandidateExecutions) != 1 ||
		result.CandidateExecutions[0].Execution.FinalRisk.Status != semantic.RiskWitnessReached ||
		result.CandidateExecutions[0].Execution.FinalTrace.Digest == "" {
		t.Fatalf("last budget Action was not retained and selected: %#v/%v", result, err)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func TestScenarioPlanParsingRejectsUnknownAuthorityAndMixedSelector(t *testing.T) {
	valid := []byte(`{"id":"fixture-plan","steps":[{"id":"crash","after_milestone":"temporal-prefix","selector":{"kind":"crash","node":"n1"}}]}`)
	if plan, err := ParseScenarioPlan(valid); err != nil || plan.Steps[0].Selector.Node != "n1" ||
		plan.Steps[0].AfterMilestone != "temporal-prefix" {
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

func TestScenarioInvestigationProposalParsingSeparatesStrategyFromPlan(t *testing.T) {
	valid := []byte(`{"intent":"control","branch_id":"control-a","reference_branch_id":"treatment-a","plan":{"id":"control-plan","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`)
	proposal, err := ParseScenarioInvestigationProposal(valid)
	if err != nil || proposal.Intent != ScenarioIntentControl ||
		proposal.ReferenceBranchID != "treatment-a" || proposal.Plan.ID != "control-plan" {
		t.Fatalf("valid investigation proposal rejected: %#v/%v", proposal, err)
	}
	barePlan := []byte(`{"id":"old-plan","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}`)
	if _, err := ParseScenarioInvestigationProposal(barePlan); err == nil {
		t.Fatal("bare ScenarioPlan bypassed the investigation intent protocol")
	}
	verdict := []byte(`{"intent":"continue","verdict":"unsafe","plan":{"id":"bad-authority","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`)
	if _, err := ParseScenarioInvestigationProposal(verdict); err == nil {
		t.Fatal("Agent-authored verdict field was accepted by the investigation protocol")
	}
	exactControl := []byte(`{"intent":"control","branch_id":"control-b","reference_branch_id":"treatment-a","plan":{"id":"control-exact","steps":[{"id":"crash","selector":{"action_id":"stale-action"}}]}}`)
	if _, err := ParseScenarioInvestigationProposal(exactControl); err == nil {
		t.Fatal("control accepted a checkpoint-local exact ActionID")
	}
	exactAblation := []byte(`{"intent":"ablate","branch_id":"ablation-b","reference_branch_id":"treatment-a","omitted_step_ids":["restart"],"plan":{"id":"ablation-exact","steps":[{"id":"crash","selector":{"action_id":"stale-action"}}]}}`)
	if _, err := ParseScenarioInvestigationProposal(exactAblation); err == nil {
		t.Fatal("ablation accepted a checkpoint-local exact ActionID")
	}
	reviseBranch := []byte(`{"intent":"revise","from_branch_id":"treatment-a","plan":{"id":"revise-branch","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`)
	if _, err := ParseScenarioInvestigationProposal(reviseBranch); err == nil {
		t.Fatal("revise implicitly promoted an experimental branch")
	}
	selectBranch := []byte(`{"intent":"select","from_branch_id":"treatment-a"}`)
	selected, err := ParseScenarioInvestigationProposal(selectBranch)
	if err != nil || selected.Intent != ScenarioIntentSelect || selected.FromBranchID != "treatment-a" {
		t.Fatalf("zero-Action branch selection was rejected: %#v/%v", selected, err)
	}
	selectWithPlan := []byte(`{"intent":"select","from_branch_id":"treatment-a","plan":{"id":"not-zero-cost","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`)
	if _, err := ParseScenarioInvestigationProposal(selectWithPlan); err == nil {
		t.Fatal("select accepted a Runtime plan")
	}
	abandoned, err := ParseScenarioInvestigationProposal([]byte(`{"intent":"abandon"}`))
	if err != nil || abandoned.Intent != ScenarioIntentAbandon {
		t.Fatalf("zero-Action hypothesis abandonment was rejected: %#v/%v", abandoned, err)
	}
	if _, err := ParseScenarioInvestigationProposal([]byte(`{"intent":"abandon","plan":{"id":"work","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`)); err == nil {
		t.Fatal("abandon accepted a Runtime plan")
	}
	reference := ScenarioInvestigationBranch{
		ID: "partial", Intent: ScenarioIntentBranch, Outcome: ScenarioStatusStopped,
		Plan: ScenarioPlan{ID: "partial-plan", Steps: []ScenarioStep{
			{ID: "crash", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"}},
			{ID: "restart", Selector: FrontierActionSelector{Kind: control.ActionRestart, Node: "n1"}},
		}},
		AppliedInterventions: []ScenarioAppliedIntervention{{StepID: "crash"}},
	}
	ablation := ScenarioInvestigationProposal{
		Intent: ScenarioIntentAblate, BranchID: "partial-ablation", ReferenceBranchID: reference.ID,
		OmittedStepIDs: []string{"restart"},
		Plan: ScenarioPlan{ID: "partial-ablation-plan", Steps: []ScenarioStep{
			{ID: "crash", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"}},
		}},
	}
	if validScenarioAblation(ablation, reference) ||
		containsString(scenarioAvailableIntents(nil, []ScenarioInvestigationBranch{reference}), ScenarioIntentAblate) {
		t.Fatal("a non-applied intervention was accepted as an ablation reference")
	}
}

func TestM4dRealDeepSeekProposalsReceivePreciseRepairFeedback(t *testing.T) {
	responses := []string{
		`{"branch_id":"bounded-recovery-drop-coupling-b1","intent":"continue","plan":{"id":"bounded-recovery-drop-coupling-plan","steps":[{"id":"s1","selector":{"action_id":"action-7aa63ca911cfb7928180f252099b0d17a5af7d85bc905bcf22b800d0fb16bb25"}},{"id":"s2","selector":{"action_id":"action-8194c0e7da0f0419bd0befa4ac4d23b73ceb1e6b1cb99b43a0b6654cf42f29bc"}},{"id":"s3","selector":{"action_id":"action-575016e4961ca56db580d138c62e69577e212e29276bc9ca8c5da690e4fbaf4b"}}]}}`,
		`{"intent":"continue","branch_id":"bounded-recovery-drop-coupling-continue","plan":{"id":"bounded-recovery-drop-coupling-plan","steps":[{"id":"step-1","selector":{"kind":"drop-message","node":"n1","message_source":"n1","message_target":"n2"}},{"id":"step-2","selector":{"kind":"fire-temporal-event","node":"n2"}},{"id":"step-3","selector":{"kind":"deliver-message","node":"n2","message_source":"n1","message_target":"n2"}}]}}`,
		`{"intent":"continue","branch_id":"branch-1","plan":{"id":"plan-1","steps":[{"id":"step-1","selector":{"action_id":"action-7aa63ca911cfb7928180f252099b0d17a5af7d85bc905bcf22b800d0fb16bb25"}},{"id":"step-2","selector":{"kind":"fire-temporal-event","node":"n2","temporal_kind":"periodic-pulse"}},{"id":"step-3","selector":{"kind":"deliver-message","node":"n2","message_source":"n1","message_target":"n2"}}]}}`,
		`{"branch_id":"aligned-timer-ballot-oscillation-continue","intent":"continue","plan":{"id":"aligned-timer-ballot-oscillation-continue-plan","steps":[{"id":"step-1","selector":{"action_id":"action-8194c0e7da0f0419bd0befa4ac4d23b73ceb1e6b1cb99b43a0b6654cf42f29bc"}},{"id":"step-2","selector":{"action_id":"action-c57ca872170559dfe02dc674a896c7421a79b6fbd1b31de8804525efb6a2f3ef"}},{"id":"step-3","selector":{"action_id":"action-575016e4961ca56db580d138c62e69577e212e29276bc9ca8c5da690e4fbaf4b"}},{"id":"step-4","selector":{"action_id":"action-f867ad00a86ef76d1a0fc546c7c81f6c37104236a466608eb20f49fa83c9b714"}}]}}`,
		`{"intent":"continue","branch_id":"aligned-timer-ballot-oscillation-b1","plan":{"id":"aligned-timer-ballot-oscillation-b1-plan","steps":[{"id":"step-1","selector":{"kind":"invoke","node":"n1"}},{"id":"step-2","selector":{"kind":"fire-temporal-event","node":"n2","temporal_kind":"periodic-pulse"}},{"id":"step-3","selector":{"kind":"fire-temporal-event","node":"n3","temporal_kind":"periodic-pulse"}}]}}`,
		`{"intent":"continue","branch_id":"branch-aligned-timer-ballot-oscillation-3","plan":{"id":"plan-aligned-timer-ballot-oscillation-3","steps":[{"id":"step-1","selector":{"kind":"invoke","node":"n1","owner":"n1"}},{"id":"step-2","selector":{"kind":"fire-temporal-event","node":"n2","owner":"n2","temporal_kind":"periodic-pulse"}},{"id":"step-3","selector":{"kind":"fire-temporal-event","node":"n3","owner":"n3","temporal_kind":"periodic-pulse"}},{"id":"step-4","selector":{"kind":"deliver-message","node":"n3","owner":"n1","message_source":"n1","message_target":"n3"}}]}}`,
	}
	for index, response := range responses {
		proposal, issue := InspectScenarioInvestigationProposal([]byte(response))
		if issue == nil || issue.Code != ScenarioProposalIssueContinueBranch ||
			issue.Field != "branch_id" || issue.Validate() != nil ||
			proposal.Intent != ScenarioIntentContinue || proposal.BranchID == "" ||
			proposal.Plan.ID == "" {
			t.Fatalf("real response %d did not preserve precise repair evidence: %#v %#v", index+1, proposal, issue)
		}
	}

	laterExact := []byte(`{"intent":"continue","plan":{"id":"stale-later-action","steps":[{"id":"first","selector":{"kind":"invoke","node":"n1"}},{"id":"second","selector":{"action_id":"stale-after-first"}}]}}`)
	proposal, issue := InspectScenarioInvestigationProposal(laterExact)
	if issue == nil || issue.Code != ScenarioProposalIssueLaterExactActionID ||
		issue.Field != "plan.steps[].selector.action_id" || proposal.Plan.ID != "stale-later-action" {
		t.Fatalf("later ActionID did not receive precise repair feedback: %#v %#v", proposal, issue)
	}
}

func TestM4dScenarioIntentPhasesMatchRepairContract(t *testing.T) {
	if got := scenarioAvailableIntents(nil, nil); !reflect.DeepEqual(got, []string{ScenarioIntentContinue}) {
		t.Fatalf("initial phase exposed experimental intents: %#v", got)
	}
	stopped := &ScenarioAgentFeedback{Outcome: ScenarioAgentStopped}
	if got := scenarioAvailableIntents(stopped, nil); !reflect.DeepEqual(got, []string{ScenarioIntentRevise}) {
		t.Fatalf("repair phase exposed incompatible intents: %#v", got)
	}
	stopped.ProgressDelta = &ScenarioProgressDelta{Decisions: 1}
	if got := scenarioAvailableIntents(stopped, nil); !reflect.DeepEqual(
		got, []string{ScenarioIntentRevise, ScenarioIntentAbandon},
	) {
		t.Fatalf("progress repair phase lost bounded abandonment: %#v", got)
	}
	completed := &ScenarioAgentFeedback{Outcome: ScenarioStatusCompleted}
	if got := scenarioAvailableIntents(completed, nil); !reflect.DeepEqual(
		got, []string{ScenarioIntentContinue, ScenarioIntentBranch},
	) {
		t.Fatalf("completed path did not expose comparison phase: %#v", got)
	}
}

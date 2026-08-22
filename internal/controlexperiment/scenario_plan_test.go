package controlexperiment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type fixtureSemanticPrefixProjector struct {
	preferred control.ActionID
}

func fixtureInitialTrace(t *testing.T, ctx context.Context, config RuntimeConfig) controlruntime.Trace {
	t.Helper()
	runtimeConfig, err := config.runtimeConfig()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, fixture.New(), runtimeConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	return trace
}

func TestRecordedInvokePreparationRejectsParameterDrift(t *testing.T) {
	ctx := context.Background()
	runtimeConfig, err := (RuntimeConfig{SeedHex: "6d346e31342d696e766f6b652d74616d706572"}).runtimeConfig()
	if err != nil {
		t.Fatal(err)
	}
	original, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpOneShot, Delay: 1})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := controlruntime.New(ctx, fixture.New(), runtimeConfig)
	if err != nil {
		t.Fatal(err)
	}
	expectedID, err := reference.OfferInvoke(ctx, "n1", original)
	if closeErr := reference.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	tampered, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpOneShot, Delay: 2})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := json.Marshal(control.AdapterInvokeParameters{Input: tampered})
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{
		Version: PolicyVersion,
		ID:      "recorded-invoke-parameter-drift",
		Rules: []DecisionRule{{
			Decision:   1,
			Kind:       control.ActionInvoke,
			Node:       "n1",
			ActionID:   expectedID,
			Parameters: parameters,
		}},
		Priority: []control.ActionKind{control.ActionFireTemporal},
	}
	if err := policy.Validate(1); err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, fixture.New(), runtimeConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, _, _, err := offerPolicyPreparation(ctx, policy, 1, runtime); err == nil ||
		!strings.Contains(err.Error(), "EXPERIMENT_POLICY_PREPARATION_ID_MISMATCH") {
		t.Fatalf("tampered Invoke parameters were not rejected: %v", err)
	}
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
	policy, err := RecordedScenarioSchedulePolicy(
		"fixture-a4-recorded-schedule", root, result,
	)
	if err != nil || len(policy.Rules) != 0 || len(policy.Priority) == 0 {
		t.Fatalf("successful scenario did not validate as a recorded schedule: %#v/%v", policy, err)
	}
	tamperedExecution := result
	tamperedChoice := *tamperedExecution.Steps[1].Choice
	tamperedChoice.Action.ActionID = "not-the-executed-action"
	tamperedExecution.Steps[1].Choice = &tamperedChoice
	if _, err := RecordedScenarioSchedulePolicy("fixture-a4-tampered", root, tamperedExecution); err == nil {
		t.Fatal("scenario choice detached from the executed Trace validated as a schedule")
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
	if err != nil || result.StopReason != ScenarioProgressWitnessInstantiated ||
		len(result.Execution.Steps) != 1 ||
		len(result.Execution.FinalTrace.Records) != len(root.Records)+1 ||
		result.Execution.Work.FrontierReconstruction.SetupAttempts != 1 ||
		result.Execution.Work.FrontierReconstruction.RuntimeInitializations != 1 ||
		result.Execution.Work.ChildMaterialization.SchedulerDecisions != 1 ||
		result.Execution.Work.ChildVerification.SetupAttempts != 1 ||
		result.Execution.Work.ChildVerification.RuntimeInitializations != 1 ||
		result.Execution.Work.ChildVerification.SchedulerDecisions != len(result.Execution.FinalTrace.Records) {
		t.Fatalf("natural progress did not retain one live branch and one promotion replay: %#v/%v",
			result, err)
	}
	delta, err := NewScenarioProgressDelta(
		spec, rootRisk, root, result.Execution.FinalRisk, result.Execution.FinalTrace,
	)
	if err != nil || delta.Decisions != 1 || len(delta.RecentActions) != 1 ||
		len(delta.NewMilestones) != 1 || delta.UniqueStateTransitions == 0 ||
		delta.MilestoneProgress != ScenarioMilestoneProgressInstantiated ||
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
	wantRemaining := []int{256, 251, 246, 241}
	wantAllowance := []int{5, 5, 5, 5}
	wantDelta := []int{5, 5, 5, 5}
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
					(view.Prior.ProgressDelta.MilestoneProgress != ScenarioMilestoneProgressRepeated &&
						view.Prior.ProgressDelta.MilestoneProgress != ScenarioMilestoneProgressStalled) ||
					view.Prior.ProgressDelta.TemporalCallbacks == 0 ||
					view.Prior.ProgressDelta.LogicalClockAdvances > view.Prior.ProgressDelta.TemporalCallbacks ||
					len(view.Prior.ProgressDelta.ActionCounts) == 0 ||
					len(view.Prior.ProgressDelta.RecentActions) != wantDelta[calls-1] {
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
		result.Execution == nil || len(result.Execution.FinalTrace.Records) != 20 ||
		len(result.Attempts) != 4 || result.Attempts[3].Feedback.ProgressDelta == nil ||
		result.Attempts[3].Feedback.ProgressDelta.Decisions != 5 ||
		result.StopReason != ScenarioAgentStopCallBudget || result.DecisionsUsed != 20 ||
		result.SelectedPathDecisions != 0 ||
		result.ExecutionWork.ChildMaterialization.SchedulerDecisions != 20 ||
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
	t.Logf("small-slice progress: reconstruction=%d verification_setups=%d verification_decisions=%d work=%d",
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

func TestScenarioAgentPreservesVerifiedPrefixWhenLengthRepairFails(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "6d346e31312d6c656e6774682d726570616972", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-length-repair-risk", "fixture-cft", "length-repair-prefix",
		[]string{"never-reached"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := fixtureSemanticPrefixProjector{preferred: "never-selected"}
	rootRisk, err := projector.Project("fixture-length-repair-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err := ReconstructRiskFrontierState(
		ctx, "fixture-length-repair-frontier", spec, rootRisk, root, len(root.Records),
		runtimeConfig, nil, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-length-repair-knowledge", Family: spec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{ID: "periodic", Text: "A temporal callback advances one deterministic step."}},
		Risks: []ProtocolRisk{{
			ID: spec.RiskID, Summary: "Retain an executed prefix if a later provider response is truncated.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionFireTemporal},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"fixture-length-repair-hypothesis", knowledge, spec,
		"Execute one real action before a charged truncated response.", ScenarioPlanningBackendID,
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	verifiedDigest := ""
	var repairIntents []string
	result, err := ExploreScenarioWithPlanner(
		ctx, 3, 1, 8, knowledge, hypothesis, spec, frontier,
		unknownScenarioSemantics(t, frontier), rootRisk, root, runtimeConfig, nil,
		nil, nil, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			calls++
			switch calls {
			case 1:
				var temporal FrontierActionRef
				for _, action := range view.Frontier.Actions {
					if action.Kind == control.ActionFireTemporal {
						temporal = action
						break
					}
				}
				encoded, marshalErr := json.Marshal(ScenarioInvestigationProposal{
					Intent: ScenarioIntentContinue,
					Plan: ScenarioPlan{ID: "execute-prefix", Steps: []ScenarioStep{{
						ID: "fire", Selector: FrontierActionSelector{ActionID: temporal.ActionID},
					}}},
				})
				return encoded, ModelWork{Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5}, marshalErr
			case 2:
				verifiedDigest = view.Frontier.PrefixTraceDigest
				repairIntents = append([]string(nil), view.AvailableIntents...)
				return nil, ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7},
					&ScenarioPlannerResponseFailure{Code: ScenarioAgentReasonResponseFinishLength, Repairable: true}
			default:
				if view.Frontier.PrefixTraceDigest != verifiedDigest {
					t.Fatalf("repair call repeated or changed Runtime prefix: before=%s after=%s", verifiedDigest, view.Frontier.PrefixTraceDigest)
				}
				if !slices.Equal(view.AvailableIntents, repairIntents) {
					t.Fatalf("length repair changed the logical intent phase: before=%v after=%v", repairIntents, view.AvailableIntents)
				}
				return nil, ModelWork{Calls: 1, InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
					&ScenarioPlannerResponseFailure{Code: ScenarioAgentReasonResponseFinishLength}
			}
		},
	)
	if err != nil || calls != 3 || result.Execution == nil || result.Status != ScenarioAgentCompleted ||
		result.StopReason != ScenarioAgentStopProviderResponse || len(result.Attempts) != 3 ||
		result.Attempts[1].Feedback.ReasonCode != ScenarioAgentReasonResponseFinishLength ||
		result.Attempts[2].Feedback.ReasonCode != ScenarioAgentReasonResponseFinishLength ||
		result.Execution.FinalTrace.Digest != verifiedDigest || result.SelectedPathDecisions != 0 ||
		result.ModelWork != (ModelWork{Calls: 3, InputTokens: 12, OutputTokens: 8, TotalTokens: 20}) {
		t.Fatalf("failed repair did not stop in-band with the verified prefix: %#v calls=%d err=%v", result, calls, err)
	}
}

func TestScenarioAgentAttributionStartsAtFirstStrategicAction(t *testing.T) {
	execution := ScenarioExecution{
		FinalTrace: controlruntime.Trace{Records: make([]controlruntime.ActionRecord, 7)},
		Steps: []ScenarioStepFeedback{
			{
				Outcome: ScenarioStepApplied, Decision: 3,
				Choice: &FrontierChoice{Action: FrontierActionRef{Kind: control.ActionFireTemporal}},
			},
			{
				Outcome: ScenarioStepApplied, Decision: 5,
				Choice: &FrontierChoice{Action: FrontierActionRef{Kind: control.ActionDropMessage}},
			},
		},
		AutomaticProgress: []ScenarioStepFeedback{{
			Outcome: ScenarioStepApplied, Decision: 4,
			Choice: &FrontierChoice{Action: FrontierActionRef{Kind: control.ActionInvoke}},
		}},
	}
	if got := ScenarioAgentAttributionRootDecisions(execution); got != 4 {
		t.Fatalf("automatic setup was credited to the Agent: got root=%d want=4", got)
	}
	execution.Steps = execution.Steps[:1]
	if got := ScenarioAgentAttributionRootDecisions(execution); got != 7 {
		t.Fatalf("natural-only plan received Agent finding credit: got root=%d want=7", got)
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
		proposal, err := parseScenarioInvestigationProposalForTest([]byte(response.json))
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
			wantAllowance := []int{5, 5, 5}
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

func TestScenarioPlanValidationRejectsMixedSelector(t *testing.T) {
	mixed := ScenarioPlan{ID: "fixture-mixed", Steps: []ScenarioStep{{
		ID: "mixed", Selector: FrontierActionSelector{ActionID: "action-1", Kind: control.ActionCrash},
	}}}
	if err := mixed.Validate(); err == nil {
		t.Fatal("exact ActionID mixed with semantic selector fields was accepted")
	}
}

func TestScenarioInvestigationProposalKeepsOnlySinglePathIntents(t *testing.T) {
	valid := []byte(`{"intent":"continue","plan":{"id":"continue-plan","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`)
	proposal, err := parseScenarioInvestigationProposalForTest(valid)
	if err != nil || proposal.Intent != ScenarioIntentContinue || proposal.Plan.ID != "continue-plan" {
		t.Fatalf("valid single-path proposal rejected: %#v/%v", proposal, err)
	}
	for _, retired := range [][]byte{
		[]byte(`{"intent":"branch","branch_id":"treatment","plan":{"id":"branch-plan","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`),
		[]byte(`{"intent":"control","reference_branch_id":"treatment","plan":{"id":"control-plan","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`),
		[]byte(`{"intent":"ablate","omitted_step_ids":["crash"],"plan":{"id":"ablate-plan","steps":[{"id":"restart","selector":{"kind":"restart","node":"n1"}}]}}`),
		[]byte(`{"intent":"select","from_branch_id":"treatment"}`),
	} {
		if _, err := parseScenarioInvestigationProposalForTest(retired); err == nil {
			t.Fatalf("retired branch protocol was accepted: %s", retired)
		}
	}
	barePlan := []byte(`{"id":"old-plan","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}`)
	if _, err := parseScenarioInvestigationProposalForTest(barePlan); err == nil {
		t.Fatal("bare ScenarioPlan bypassed the investigation intent protocol")
	}
	verdict := []byte(`{"intent":"continue","verdict":"unsafe","plan":{"id":"bad-authority","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`)
	if _, err := parseScenarioInvestigationProposalForTest(verdict); err == nil {
		t.Fatal("Agent-authored verdict field was accepted")
	}
	abandoned, err := parseScenarioInvestigationProposalForTest([]byte(`{"intent":"abandon"}`))
	if err != nil || abandoned.Intent != ScenarioIntentAbandon {
		t.Fatalf("zero-Action abandonment rejected: %#v/%v", abandoned, err)
	}
	if _, err := parseScenarioInvestigationProposalForTest([]byte(`{"intent":"abandon","plan":{"id":"work","steps":[{"id":"crash","selector":{"kind":"crash","node":"n1"}}]}}`)); err == nil {
		t.Fatal("abandon accepted Runtime work")
	}
}

func parseScenarioInvestigationProposalForTest(
	data []byte,
) (ScenarioInvestigationProposal, error) {
	proposal, issue := InspectScenarioInvestigationProposal(data)
	if issue != nil {
		return ScenarioInvestigationProposal{}, errors.New(issue.Code)
	}
	return proposal, nil
}

func TestM4dScenarioIntentPhasesMatchRepairContract(t *testing.T) {
	if got := scenarioSinglePathIntents(nil); !reflect.DeepEqual(got, []string{ScenarioIntentContinue}) {
		t.Fatalf("initial phase exposed experimental intents: %#v", got)
	}
	stopped := &ScenarioAgentFeedback{Outcome: ScenarioAgentStopped}
	if got := scenarioSinglePathIntents(stopped); !reflect.DeepEqual(got, []string{ScenarioIntentRevise}) {
		t.Fatalf("repair phase exposed incompatible intents: %#v", got)
	}
	stopped.ProgressDelta = &ScenarioProgressDelta{Decisions: 1}
	if got := scenarioSinglePathIntents(stopped); !reflect.DeepEqual(
		got, []string{ScenarioIntentRevise, ScenarioIntentAbandon},
	) {
		t.Fatalf("progress repair phase lost bounded abandonment: %#v", got)
	}
	completed := &ScenarioAgentFeedback{Outcome: ScenarioStatusCompleted}
	if got := scenarioSinglePathIntents(completed); !reflect.DeepEqual(got, []string{ScenarioIntentContinue}) {
		t.Fatalf("completed path did not remain single-path: %#v", got)
	}
}

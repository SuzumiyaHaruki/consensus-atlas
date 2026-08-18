package controlexperiment

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestDescribedFixtureCapabilitiesReachFrontierTraceAndReplay(t *testing.T) {
	ctx := context.Background()
	manifest, err := fixture.NewDescribed().Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := fixture.InputPayload(fixture.Input{
		Operation: fixture.OpEmitDurableMessage, Target: "n2", Value: "value",
	})
	if err != nil {
		t.Fatal(err)
	}
	workload := WorkloadPlan{
		SchemaVersion: WorkloadPlanVersion, ID: "described-fixture-workload",
		TargetSelector: TargetSingleCoordinatingMember,
		Invocations:    []WorkloadInvocation{{ID: "request-1", Input: payload, ExpectedStatus: "ok"}},
	}
	composable := []control.ActionKind{
		control.ActionCompleteEffect, control.ActionDeliverMessage,
		control.ActionFailEffect, control.ActionInvoke,
	}
	sort.Slice(composable, func(i, j int) bool { return composable[i] < composable[j] })
	surface, err := NewAgentTargetSurface(
		"described-fixture", manifest, workload,
		RuntimeConfig{SeedHex: "010203", MaxClones: 1}, FaultEnvelope{},
		AgentTargetExtensions{
			ComposableActions: composable,
			ObservationCapabilities: []semantic.ObservationCapability{{
				Kind: semantic.ObservationWorkloadInvoked,
			}},
			OracleCapabilities: []AgentOracleCapability{{
				ID: "trace-integrity", Scope: AgentOracleScopeGeneric,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	undeclaredManifest := manifest
	undeclaredManifest.Capabilities.Actions = append(
		[]control.ActionKind(nil), manifest.Capabilities.Actions[1:]...,
	)
	err = validateAgentTargetExtensions(AgentTargetExtensions{
		ComposableActions: []control.ActionKind{control.ActionDropMessage},
		ObservationCapabilities: []semantic.ObservationCapability{{
			Kind: semantic.ObservationWorkloadInvoked,
		}},
		OracleCapabilities: []AgentOracleCapability{{
			ID: "trace-integrity", Scope: AgentOracleScopeGeneric,
		}},
	}, undeclaredManifest, FaultEnvelope{MaxMessageDrops: 1})
	if err == nil || err.Error() != "EXPERIMENT_AGENT_TARGET_ACTION_NOT_DECLARED" {
		t.Fatalf("target extension self-awarded undeclared Action: %v", err)
	}
	if surface.Capabilities.Message == nil ||
		len(surface.Capabilities.Message.TypeHints) != 1 ||
		surface.Capabilities.Message.TypeHints[0] != "fixture-message" ||
		len(surface.Capabilities.HostEffects) != 1 ||
		surface.Capabilities.HostEffects[0].Phases[0] != "storage" ||
		surface.Capabilities.HostEffects[0].AllowedFailures[0] != "io-error" {
		t.Fatalf("capability projection incomplete: %#v", surface.Capabilities)
	}

	validPlan := ScenarioPlan{ID: "valid-rich-control", Steps: []ScenarioStep{{
		ID: "complete-persist", Selector: FrontierActionSelector{
			Kind: control.ActionCompleteEffect, EffectKind: "persist", EffectPhase: "storage",
			EffectOutcome: "ok", Durability: control.DurabilityDurable,
		},
	}}}
	if gaps, err := surface.ScenarioCapabilityGaps(validPlan, nil); err != nil || len(gaps) != 0 {
		t.Fatalf("valid rich control was rejected: %#v/%v", gaps, err)
	}
	wrongPhase := validPlan
	wrongPhase.ID = "invalid-rich-control"
	wrongPhase.Steps = append([]ScenarioStep(nil), validPlan.Steps...)
	wrongPhase.Steps[0].Selector.EffectPhase = "network"
	if gaps, err := surface.ScenarioCapabilityGaps(wrongPhase, nil); err != nil || len(gaps) != 1 ||
		gaps[0].Code != AgentCapabilityGapMissingControl || gaps[0].Reference != "complete-persist" {
		t.Fatalf("undeclared effect phase was not identified: %#v/%v", gaps, err)
	}
	missingActionSurface := *cloneAgentTargetSurface(&surface)
	missingActionSurface.Capabilities.ComposableActions = []control.ActionKind{
		control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionInvoke,
	}
	missingActionPlan := ScenarioPlan{ID: "missing-action", Steps: []ScenarioStep{{
		ID: "fail-persist", Selector: FrontierActionSelector{Kind: control.ActionFailEffect},
	}}}
	if gaps, err := missingActionSurface.ScenarioCapabilityGaps(missingActionPlan, nil); err != nil ||
		len(gaps) != 1 || gaps[0].Code != AgentCapabilityGapMissingAction {
		t.Fatalf("non-composable Action was not identified: %#v/%v", gaps, err)
	}
	exactMissingActionPlan := ScenarioPlan{ID: "exact-missing-action", Steps: []ScenarioStep{{
		ID: "exact-failure", Selector: FrontierActionSelector{ActionID: "enabled-failure"},
	}}}
	if gaps, err := missingActionSurface.ScenarioCapabilityGaps(exactMissingActionPlan, []FrontierActionRef{{
		ActionID: "enabled-failure", Kind: control.ActionFailEffect,
	}}); err != nil || len(gaps) != 1 || gaps[0].Code != AgentCapabilityGapMissingAction {
		t.Fatalf("exact ActionID bypassed composable capability: %#v/%v", gaps, err)
	}
	legacySurface := *cloneAgentTargetSurface(&surface)
	legacySurface.Capabilities.Message = nil
	legacySurface.Capabilities.HostEffects = nil
	unassessedPlan := ScenarioPlan{ID: "legacy-unassessed", Steps: []ScenarioStep{{
		ID: "opaque-message", Selector: FrontierActionSelector{
			Kind: control.ActionDeliverMessage, MessageTypeHint: "target-local-unknown",
		},
	}}}
	if gaps, err := legacySurface.ScenarioCapabilityGaps(unassessedPlan, nil); err != nil || len(gaps) != 0 {
		t.Fatalf("legacy undeclared detail must remain unassessed: %#v/%v", gaps, err)
	}

	for _, outcome := range []struct {
		name string
		kind control.ActionKind
	}{
		{name: "complete", kind: control.ActionCompleteEffect},
		{name: "fail", kind: control.ActionFailEffect},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			runtimeConfig, err := (RuntimeConfig{SeedHex: "010203", MaxClones: 1}).runtimeConfig()
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := controlruntime.New(ctx, fixture.NewDescribed(), runtimeConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			invokeID, err := runtime.OfferInvoke(ctx, "n1", payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.Select(ctx, invokeID); err != nil {
				t.Fatal(err)
			}
			actions, err := runtime.EnabledActions(ctx)
			if err != nil {
				t.Fatal(err)
			}
			refs, err := frontierActionRefs(actions, runtime.Snapshot())
			if err != nil {
				t.Fatal(err)
			}
			selected := frontierRefOfKind(t, refs, outcome.kind)
			if selected.EffectKind != "persist" || selected.EffectPhase != "storage" ||
				selected.Durability != control.DurabilityDurable || selected.EffectOutcome == "" {
				t.Fatalf("effect frontier projection incomplete: %#v", selected)
			}
			effectSelector := FrontierActionSelector{
				Kind: outcome.kind, EffectKind: "persist", EffectPhase: "storage",
				EffectOutcome: selected.EffectOutcome, Durability: control.DurabilityDurable,
			}
			if !effectSelector.matches(selected) {
				t.Fatalf("rich effect selector did not match: %#v / %#v", effectSelector, selected)
			}
			if _, err := runtime.Select(ctx, selected.ActionID); err != nil {
				t.Fatal(err)
			}
			if outcome.kind == control.ActionCompleteEffect {
				actions, err = runtime.EnabledActions(ctx)
				if err != nil {
					t.Fatal(err)
				}
				refs, err = frontierActionRefs(actions, runtime.Snapshot())
				if err != nil {
					t.Fatal(err)
				}
				message := frontierRefOfKind(t, refs, control.ActionDeliverMessage)
				if message.MessageTypeHint != "fixture-message" ||
					message.MessageMetadata["channel"] != "peer" || len(message.Dependencies) != 1 {
					t.Fatalf("message frontier projection incomplete: %#v", message)
				}
				messageSelector := FrontierActionSelector{
					Kind: control.ActionDeliverMessage, MessageTypeHint: "fixture-message",
				}
				if !messageSelector.matches(message) {
					t.Fatalf("message type selector did not match: %#v", message)
				}
			}
			trace, err := runtime.Trace()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := controlruntime.Replay(ctx, fixture.NewDescribed(), runtimeConfig, trace); err != nil {
				t.Fatalf("Replay() error = %v", err)
			}
		})
	}
}

func TestScenarioAgentRevisesMechanicalCapabilityGapWithoutRuntimeWork(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "6d346a322d6361706162696c697479", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	envelope := &FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-capability-repair-risk", "fixture-cft", "capability-repair",
		[]string{"crash-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := actionKindSemanticProjector{}
	rootRisk, err := projector.Project("fixture-capability-repair-root", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err := ReconstructRiskFrontierState(
		ctx, "fixture-capability-repair-frontier", spec, rootRisk, root, len(root.Records),
		runtimeConfig, envelope, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := fixture.New().Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpEmitMessage, Target: "n2"})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := NewAgentTargetSurface(
		"fixture-capability-repair", manifest,
		WorkloadPlan{
			SchemaVersion: WorkloadPlanVersion, ID: "fixture-capability-repair-workload",
			TargetSelector: TargetSingleCoordinatingMember,
			Invocations: []WorkloadInvocation{{
				ID: "request-1", Input: payload, ExpectedStatus: "ok",
			}},
		},
		runtimeConfig, *envelope,
		AgentTargetExtensions{
			ComposableActions: []control.ActionKind{control.ActionCrash},
			ObservationCapabilities: []semantic.ObservationCapability{{
				Kind: semantic.ObservationWorkloadInvoked,
			}},
			OracleCapabilities: []AgentOracleCapability{{
				ID: "trace-integrity", Scope: AgentOracleScopeGeneric,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-capability-repair-knowledge", Family: spec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{ID: "repair", Text: "Revise controls that the Target cannot compose."}},
		Risks: []ProtocolRisk{{
			ID: spec.RiskID, Summary: "Reach a crash prefix after mechanical plan repair.",
			RequiredCapabilities: []string{"lifecycle-control"},
			RequiredActions:      []control.ActionKind{control.ActionCrash},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"fixture-capability-repair-hypothesis", knowledge, spec,
		"Use trusted capability feedback before executing a crash.", ScenarioPlanningBackendID,
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := ExploreScenarioWithPlanner(
		ctx, 2, 1, 1, knowledge, hypothesis, spec, frontier,
		unknownScenarioSemantics(t, frontier), rootRisk, root, runtimeConfig, envelope,
		&surface, nil, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			calls++
			plan := ScenarioPlan{ID: "unsupported-control", Steps: []ScenarioStep{{
				ID: "unsupported-failure", Selector: FrontierActionSelector{Kind: control.ActionFailEffect},
			}}}
			intent := ScenarioIntentContinue
			if calls == 2 {
				if view.Prior == nil || view.Prior.ReasonCode != AgentCapabilityGapMissingAction ||
					len(view.Prior.CapabilityGaps) != 1 || view.Prior.FailedStep == nil ||
					view.Prior.FailedStep.ID != "unsupported-failure" || view.RemainingDecisions != 1 {
					t.Fatalf("capability repair feedback incomplete: %#v", view.Prior)
				}
				plan = ScenarioPlan{ID: "supported-control", Steps: []ScenarioStep{{
					ID: "crash", Selector: FrontierActionSelector{Kind: control.ActionCrash, Node: "n1"},
				}}}
				intent = ScenarioIntentRevise
			}
			encoded, marshalErr := json.Marshal(ScenarioInvestigationProposal{Intent: intent, Plan: plan})
			return encoded, ModelWork{Calls: 1, InputTokens: 1, OutputTokens: 1, TotalTokens: 2}, marshalErr
		},
	)
	if err != nil || calls != 2 || result.StopReason != ScenarioAgentStopRiskReached ||
		result.DecisionsUsed != 1 || result.Execution == nil || len(result.Attempts) != 2 ||
		result.Attempts[0].Execution != nil || result.Attempts[0].Feedback.CapabilityGaps[0].Code != AgentCapabilityGapMissingAction {
		t.Fatalf("capability repair did not preserve Runtime budget: %#v calls=%d err=%v", result, calls, err)
	}
	baselineCalls := 0
	stopped, err := ExploreScenarioWithPlanner(
		ctx, 2, 1, 1, knowledge, hypothesis, spec, frontier,
		unknownScenarioSemantics(t, frontier), rootRisk, root, runtimeConfig, envelope,
		&surface, nil, factory, projector,
		func(_ controlruntime.Trace, next RiskFrontierView, _ controlruntime.Snapshot) (ScenarioSemanticExposure, error) {
			return unknownScenarioSemantics(t, next), nil
		},
		func(_ context.Context, view ScenarioAgentView) ([]byte, ModelWork, error) {
			baselineCalls++
			intent := ScenarioIntentContinue
			stepID := "unsupported-failure"
			planID := "terminal-gap"
			if baselineCalls == 2 {
				if view.Prior == nil || len(view.Prior.CapabilityGaps) != 1 {
					t.Fatalf("baseline did not receive the same structured feedback: %#v", view.Prior)
				}
				intent = ScenarioIntentRevise
				stepID = "renamed-unsupported-failure"
				planID = "repeated-terminal-gap"
			}
			encoded, marshalErr := json.Marshal(ScenarioInvestigationProposal{
				Intent: intent,
				Plan: ScenarioPlan{ID: planID, Steps: []ScenarioStep{{
					ID: stepID, Selector: FrontierActionSelector{Kind: control.ActionFailEffect},
				}}},
			})
			return encoded, ModelWork{Calls: 1, InputTokens: 1, OutputTokens: 1, TotalTokens: 2}, marshalErr
		},
	)
	if err != nil || baselineCalls != 2 || stopped.StopReason != ScenarioAgentStopCapabilityGap ||
		stopped.DecisionsUsed != 0 || stopped.Execution != nil || len(stopped.Attempts) != 2 ||
		len(stopped.Attempts[0].Feedback.CapabilityGaps) != 1 ||
		len(stopped.Attempts[1].Feedback.CapabilityGaps) != 1 ||
		stopped.Attempts[0].Feedback.CapabilityGaps[0].Summary !=
			stopped.Attempts[1].Feedback.CapabilityGaps[0].Summary {
		t.Fatalf("code-only baseline did not repeat the unavailable control: %#v/%v", stopped, err)
	}
}

func frontierRefOfKind(t *testing.T, refs []FrontierActionRef, kind control.ActionKind) FrontierActionRef {
	t.Helper()
	for _, ref := range refs {
		if ref.Kind == kind {
			return ref
		}
	}
	t.Fatalf("frontier has no %s action: %#v", kind, refs)
	return FrontierActionRef{}
}

package controlexperiment

import (
	"context"
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
	if surface.Capabilities.Message == nil ||
		len(surface.Capabilities.Message.TypeHints) != 1 ||
		surface.Capabilities.Message.TypeHints[0] != "fixture-message" ||
		len(surface.Capabilities.HostEffects) != 1 ||
		surface.Capabilities.HostEffects[0].Phases[0] != "storage" ||
		surface.Capabilities.HostEffects[0].AllowedFailures[0] != "io-error" {
		t.Fatalf("capability projection incomplete: %#v", surface.Capabilities)
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

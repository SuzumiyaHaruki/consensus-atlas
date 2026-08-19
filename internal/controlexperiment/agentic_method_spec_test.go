package controlexperiment

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestAgenticMethodSpecBindsActualMethodConfiguration(t *testing.T) {
	episode := AgenticLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: 128, MaxPrimaryWorkUnits: 256,
		MaxReplayWorkUnits: 256, MaxModelCalls: 6, MaxModelTokens: 50_000,
	}
	total, err := ScaleAgenticLogicalBudget(episode, 3)
	if err != nil {
		t.Fatal(err)
	}
	scenarioTransport := AgentTransportFreeze{
		Provider: "deepseek", Endpoint: "https://api.deepseek.com/chat/completions",
		Model: "deepseek-v4-flash", Thinking: "disabled", StructuredOutputMode: "json-object",
		RequestTimeoutMS: 900_000, RoutingPolicy: "provider-fixed",
		MaxOutputTokens: 16000, MaxCallsPerArm: 1,
	}
	spec, err := NewAgenticMethodSpec(AgenticMethodSpec{
		TargetID: "etcdraft-v2",
		Transport: AgentTransportFreeze{
			Provider: "openrouter", Endpoint: "https://openrouter.ai/api/v1/chat/completions",
			Model: "deepseek/deepseek-v4-flash", Thinking: "high", ExcludeReasoning: true,
			StructuredOutputMode: "json-schema", RequestTimeoutMS: 900_000,
			RoutingPolicy: "openrouter-default", AllowProviderFallback: true,
			MaxOutputTokens: 32000, MaxCallsPerArm: 1,
		},
		ScenarioTransport: &scenarioTransport,
		RiskPromptVersion: "risk-agent-navigation-v2", ScenarioPromptVersion: "scenario-agent-investigation-v9",
		SemanticInputSchema:      "etcdraft-agentic-input-v1",
		SemanticInputDigest:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ScenarioSemanticExposure: ScenarioSemanticExposureFull,
		ClosureMode:              AgenticClosureModeTargetLocal,
		SourceExposure: AgenticSourceExposureSpec{
			Mode:              AgenticSourceExposureDossierV2,
			ReferencePrefixes: []string{"upstream", "repo"},
			CatalogDigest:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		EpisodeLimits: AgenticEpisodeLimits{
			MaxRiskCalls: 3, MaxScenarioCalls: 3, MaxTotalCalls: 6,
			MaxObservedTokens: 50_000, MaxScenarioPlanSteps: 4, MaxRuntimeDecisions: 128,
			SessionWallClockMS: 600_000,
		},
		InvestigationEpisodes: 3, EpisodeBudget: episode, InvestigationBudget: total,
	})
	if err != nil || spec.Validate() != nil || spec.Digest == "" ||
		spec.SourceExposure.ReferencePrefixes[0] != "repo" ||
		spec.ImplementationID != AgenticMethodImplementationID {
		t.Fatalf("Agentic MethodSpec invalid: %#v/%v", spec, err)
	}
	legacy := spec
	legacy.ImplementationID = agenticMethodLegacyImplementationID
	legacy.ClosureMode = ""
	legacy.Digest = ""
	legacyDigest, err := control.CanonicalDigest(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Digest = legacyDigest
	if legacy.Validate() != nil || legacy.Digest == spec.Digest {
		t.Fatalf("legacy MethodSpec lost read-only validation: %#v", legacy)
	}
	publicInput := spec
	publicInput.ClosureMode = AgenticClosureModePublicFixed
	publicInput.Digest = ""
	public, err := NewAgenticMethodSpec(publicInput)
	if err != nil || public.Validate() != nil || public.Digest == spec.Digest {
		t.Fatalf("closure mode did not acquire distinct method identity: %#v/%v", public, err)
	}
	structuredInput := spec
	structuredInput.CapabilityFeedbackMode = AgenticCapabilityFeedbackStructuredGaps
	structuredInput.Digest = ""
	structured, err := NewAgenticMethodSpec(structuredInput)
	if err != nil || structured.Validate() != nil || structured.Digest == spec.Digest {
		t.Fatalf("structured feedback did not acquire distinct method identity: %#v/%v", structured, err)
	}
	probeInput := structured
	probeInput.Digest = ""
	probeInput.InvestigationEpisodes = 1
	probeInput.InvestigationBudget = probeInput.EpisodeBudget
	probeInput.EpisodeLimits.MaxScenarioCalls = 1
	probeInput.CapabilityFeedbackProbe = &AgenticCapabilityFeedbackProbe{
		ID: "missing-crash-probe", MaxScenarioCalls: 1,
		Plan: ScenarioPlan{ID: "missing-crash-plan", Steps: []ScenarioStep{{
			ID: "crash-n2", Selector: FrontierActionSelector{Kind: "crash", Node: "n2"},
		}}},
	}
	probed, err := NewAgenticMethodSpec(probeInput)
	if err != nil || probed.Validate() != nil || probed.Digest == structured.Digest {
		t.Fatalf("capability probe did not acquire distinct method identity: %#v/%v", probed, err)
	}
	tamperedProbe := probed
	tamperedProbe.CapabilityFeedbackProbe.Plan.Steps[0].Selector.Node = "n3"
	if tamperedProbe.Validate() == nil {
		t.Fatal("changed capability probe retained the old method identity")
	}
	reasonInput := spec
	reasonInput.CapabilityFeedbackMode = AgenticCapabilityFeedbackReasonCodes
	reasonInput.Digest = ""
	reasonOnly, err := NewAgenticMethodSpec(reasonInput)
	if err != nil || reasonOnly.Validate() != nil || reasonOnly.Digest == structured.Digest ||
		reasonOnly.Digest == spec.Digest {
		t.Fatalf("reason-code feedback did not acquire distinct method identity: %#v/%v", reasonOnly, err)
	}
	tamperedFeedback := structured
	tamperedFeedback.CapabilityFeedbackMode = AgenticCapabilityFeedbackReasonCodes
	if tamperedFeedback.Validate() == nil {
		t.Fatal("changed feedback exposure retained the old method identity")
	}
	tampered := spec
	tampered.Transport.Model = "openai/gpt-5"
	if tampered.Validate() == nil {
		t.Fatal("changed model retained the old method identity")
	}
	tampered = spec
	tamperedScenario := *tampered.ScenarioTransport
	tamperedScenario.Thinking = "high"
	tampered.ScenarioTransport = &tamperedScenario
	if tampered.Validate() == nil {
		t.Fatal("changed Scenario transport retained the old method identity")
	}
	tampered = spec
	tampered.EpisodeLimits.MaxScenarioPlanSteps++
	if tampered.Validate() == nil {
		t.Fatal("changed planning limits retained the old method identity")
	}
	if _, err := ScaleAgenticLogicalBudget(episode, int(^uint(0)>>1)); err == nil {
		t.Fatal("overflowing investigation budget was accepted")
	}
}

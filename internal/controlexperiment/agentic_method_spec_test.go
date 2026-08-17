package controlexperiment

import "testing"

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
		spec.SourceExposure.ReferencePrefixes[0] != "repo" {
		t.Fatalf("Agentic MethodSpec invalid: %#v/%v", spec, err)
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

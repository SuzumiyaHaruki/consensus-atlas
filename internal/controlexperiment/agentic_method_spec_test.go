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
		RiskInputMode:            AgenticRiskInputAgentGenerated,
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
	m4n27Legacy := spec
	m4n27Legacy.ImplementationID = agenticMethodImplementationM4n27
	m4n27Legacy.Digest = ""
	m4n27LegacyDigest, err := control.CanonicalDigest(m4n27Legacy)
	if err != nil {
		t.Fatal(err)
	}
	m4n27Legacy.Digest = m4n27LegacyDigest
	if m4n27Legacy.Validate() != nil || m4n27Legacy.Digest == spec.Digest ||
		!AgenticMethodRequiresOracleAttribution(m4n27Legacy.ImplementationID) {
		t.Fatalf("M4n27 legacy MethodSpec lost read-only validation: %#v", m4n27Legacy)
	}
	m4n26Legacy := spec
	m4n26Legacy.ImplementationID = agenticMethodImplementationM4n26
	m4n26Legacy.Digest = ""
	m4n26LegacyDigest, err := control.CanonicalDigest(m4n26Legacy)
	if err != nil {
		t.Fatal(err)
	}
	m4n26Legacy.Digest = m4n26LegacyDigest
	if m4n26Legacy.Validate() != nil || m4n26Legacy.Digest == spec.Digest ||
		!AgenticMethodRequiresOracleAttribution(m4n26Legacy.ImplementationID) {
		t.Fatalf("M4n26 legacy MethodSpec lost read-only validation: %#v", m4n26Legacy)
	}
	m4n25Legacy := spec
	m4n25Legacy.ImplementationID = agenticMethodImplementationM4n25
	m4n25Legacy.Digest = ""
	m4n25LegacyDigest, err := control.CanonicalDigest(m4n25Legacy)
	if err != nil {
		t.Fatal(err)
	}
	m4n25Legacy.Digest = m4n25LegacyDigest
	if m4n25Legacy.Validate() != nil || m4n25Legacy.Digest == spec.Digest ||
		!AgenticMethodRequiresOracleAttribution(m4n25Legacy.ImplementationID) {
		t.Fatalf("M4n25 legacy MethodSpec lost read-only validation: %#v", m4n25Legacy)
	}
	m4n24Legacy := spec
	m4n24Legacy.ImplementationID = agenticMethodImplementationM4n24
	m4n24Legacy.Digest = ""
	m4n24LegacyDigest, err := control.CanonicalDigest(m4n24Legacy)
	if err != nil {
		t.Fatal(err)
	}
	m4n24Legacy.Digest = m4n24LegacyDigest
	if m4n24Legacy.Validate() != nil || m4n24Legacy.Digest == spec.Digest ||
		!AgenticMethodRequiresOracleAttribution(m4n24Legacy.ImplementationID) {
		t.Fatalf("M4n24 legacy MethodSpec lost read-only validation: %#v", m4n24Legacy)
	}
	m4n23Legacy := spec
	m4n23Legacy.ImplementationID = agenticMethodImplementationM4n23
	m4n23Legacy.Digest = ""
	m4n23LegacyDigest, err := control.CanonicalDigest(m4n23Legacy)
	if err != nil {
		t.Fatal(err)
	}
	m4n23Legacy.Digest = m4n23LegacyDigest
	if m4n23Legacy.Validate() != nil || m4n23Legacy.Digest == spec.Digest ||
		!AgenticMethodRequiresOracleAttribution(m4n23Legacy.ImplementationID) {
		t.Fatalf("M4n23 legacy MethodSpec lost read-only validation: %#v", m4n23Legacy)
	}
	m4n22Legacy := spec
	m4n22Legacy.ImplementationID = agenticMethodImplementationM4n22
	m4n22Legacy.Digest = ""
	m4n22LegacyDigest, err := control.CanonicalDigest(m4n22Legacy)
	if err != nil {
		t.Fatal(err)
	}
	m4n22Legacy.Digest = m4n22LegacyDigest
	if m4n22Legacy.Validate() != nil || m4n22Legacy.Digest == spec.Digest ||
		!AgenticMethodRequiresOracleAttribution(m4n22Legacy.ImplementationID) {
		t.Fatalf("M4n22 legacy MethodSpec lost read-only validation: %#v", m4n22Legacy)
	}
	m4n20Legacy := spec
	m4n20Legacy.ImplementationID = agenticMethodImplementationM4n20
	m4n20Legacy.Digest = ""
	m4n20LegacyDigest, err := control.CanonicalDigest(m4n20Legacy)
	if err != nil {
		t.Fatal(err)
	}
	m4n20Legacy.Digest = m4n20LegacyDigest
	if m4n20Legacy.Validate() != nil || m4n20Legacy.Digest == spec.Digest ||
		!AgenticMethodRequiresOracleAttribution(m4n20Legacy.ImplementationID) {
		t.Fatalf("M4n20 legacy MethodSpec lost read-only validation: %#v", m4n20Legacy)
	}
	m4n2Legacy := spec
	m4n2Legacy.ImplementationID = agenticMethodM4n2LegacyID
	m4n2Legacy.Digest = ""
	m4n2LegacyDigest, err := control.CanonicalDigest(m4n2Legacy)
	if err != nil {
		t.Fatal(err)
	}
	m4n2Legacy.Digest = m4n2LegacyDigest
	if m4n2Legacy.Validate() != nil || m4n2Legacy.Digest == spec.Digest {
		t.Fatalf("M4n2 legacy MethodSpec lost read-only validation: %#v", m4n2Legacy)
	}
	closureOwnershipLegacy := spec
	closureOwnershipLegacy.ImplementationID = agenticMethodM4m1LegacyID
	closureOwnershipLegacy.Digest = ""
	closureOwnershipLegacyDigest, err := control.CanonicalDigest(closureOwnershipLegacy)
	if err != nil {
		t.Fatal(err)
	}
	closureOwnershipLegacy.Digest = closureOwnershipLegacyDigest
	if closureOwnershipLegacy.Validate() != nil || closureOwnershipLegacy.Digest == spec.Digest {
		t.Fatalf("closure ownership legacy MethodSpec lost read-only validation: %#v", closureOwnershipLegacy)
	}
	riskFidelityLegacy := spec
	riskFidelityLegacy.ImplementationID = agenticMethodRiskFidelityLegacyID
	riskFidelityLegacy.Digest = ""
	riskFidelityLegacyDigest, err := control.CanonicalDigest(riskFidelityLegacy)
	if err != nil {
		t.Fatal(err)
	}
	riskFidelityLegacy.Digest = riskFidelityLegacyDigest
	if riskFidelityLegacy.Validate() != nil || riskFidelityLegacy.Digest == spec.Digest {
		t.Fatalf("risk-fidelity legacy MethodSpec lost read-only validation: %#v", riskFidelityLegacy)
	}
	legacy := spec
	legacy.ImplementationID = agenticMethodLegacyImplementationID
	legacy.ClosureMode = ""
	legacy.RiskInputMode = ""
	legacy.RiskInputDigest = ""
	legacy.Digest = ""
	legacyDigest, err := control.CanonicalDigest(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Digest = legacyDigest
	if legacy.Validate() != nil || legacy.Digest == spec.Digest {
		t.Fatalf("legacy MethodSpec lost read-only validation: %#v", legacy)
	}
	closureLegacy := spec
	closureLegacy.ImplementationID = agenticMethodClosureLegacyImplementationID
	closureLegacy.RiskInputMode = agenticRiskInputLegacyAgentDiscovery
	closureLegacy.Digest = ""
	closureLegacyDigest, err := control.CanonicalDigest(closureLegacy)
	if err != nil {
		t.Fatal(err)
	}
	closureLegacy.Digest = closureLegacyDigest
	if closureLegacy.Validate() != nil || closureLegacy.Digest == spec.Digest {
		t.Fatalf("closure legacy MethodSpec lost read-only validation: %#v", closureLegacy)
	}
	handoffLegacy := spec
	handoffLegacy.ImplementationID = agenticMethodClosureHandoffLegacyImplementationID
	handoffLegacy.Digest = ""
	handoffLegacyDigest, err := control.CanonicalDigest(handoffLegacy)
	if err != nil {
		t.Fatal(err)
	}
	handoffLegacy.Digest = handoffLegacyDigest
	if handoffLegacy.Validate() != nil || handoffLegacy.Digest == spec.Digest {
		t.Fatalf("closure handoff legacy MethodSpec lost read-only validation: %#v", handoffLegacy)
	}
	publicInput := spec
	publicInput.ClosureMode = AgenticClosureModePublicFixed
	publicInput.Digest = ""
	public, err := NewAgenticMethodSpec(publicInput)
	if err != nil || public.Validate() != nil || public.Digest == spec.Digest {
		t.Fatalf("closure mode did not acquire distinct method identity: %#v/%v", public, err)
	}
	existingRiskInput := spec
	existingRiskInput.RiskInputMode = AgenticRiskInputExistingCandidate
	existingRiskInput.RiskInputDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	existingRiskInput.Digest = ""
	existingRisk, err := NewAgenticMethodSpec(existingRiskInput)
	if err != nil || existingRisk.Validate() != nil || existingRisk.Digest == spec.Digest {
		t.Fatalf("existing Risk input did not acquire distinct method identity: %#v/%v", existingRisk, err)
	}
	tamperedRisk := existingRisk
	tamperedRisk.RiskInputDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if tamperedRisk.Validate() == nil {
		t.Fatal("changed existing Risk input retained the old method identity")
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
	boundSourceInput := spec
	boundSourceInput.SourceExposure.ReferencePrefixes = append(
		boundSourceInput.SourceExposure.ReferencePrefixes, "go.etcd.io/raft/v3@v3.6.0/",
	)
	boundSourceInput.SourceExposure.SUTBindings = []AgenticSUTSourceBinding{{
		ReferencePrefix: "go.etcd.io/raft/v3@v3.6.0/", ModulePath: "go.etcd.io/raft/v3",
		ModuleVersion: "v3.6.0",
		ContentDigest: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}}
	boundSourceInput.Digest = ""
	boundSource, err := NewAgenticMethodSpec(boundSourceInput)
	if err != nil || boundSource.Validate() != nil || boundSource.Digest == spec.Digest {
		t.Fatalf("bound SUT source did not acquire distinct method identity: %#v/%v", boundSource, err)
	}
	tamperedSource := boundSource
	tamperedSource.SourceExposure.SUTBindings[0].ContentDigest =
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if tamperedSource.Validate() == nil {
		t.Fatal("changed SUT source retained the old method identity")
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

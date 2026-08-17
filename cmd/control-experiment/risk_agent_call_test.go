package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestRiskAgentUsesSharedDurableJournalAndStructuredOutput(t *testing.T) {
	knowledge, err := controlexperiment.NewProtocolKnowledgePack(controlexperiment.ProtocolKnowledgePack{
		ID: "risk-call-fixture", Family: "paxos", Protocol: "fixture-paxos",
		Knowledge: []controlexperiment.KnowledgeStatement{{
			ID: "message-progress", Text: "A decision follows ordered message processing.",
		}},
		Properties: []controlexperiment.ProtocolProperty{{
			ID: "decision-continuity", Summary: "An operation remains related to its decision.",
			EvidenceLevel: controlexperiment.PropertyEvidenceObservable,
		}},
		IssuePatterns: []controlexperiment.HistoricalIssuePattern{{
			ID: "message-loss-progress", Summary: "Message loss changes progress state.",
			Mechanism:     "Stale peer state survives a lost message.",
			Applicability: "A produced message can be selected.",
			Boundary:      "A delay alone is not a finding.",
		}},
		TargetDossier: &controlexperiment.TargetDossier{
			Scope: "Fixture implementation with a controlled message path.",
			Components: []controlexperiment.TargetMaterial{{
				ID: "fixture-core", Summary: "The fixture exposes decision progress.",
				EvidenceRefs: []string{"fixture.go:1"},
			}},
			BlindSpots: []controlexperiment.TargetMaterial{{
				ID: "no-restart", Summary: "The fixture cannot restart participants.",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := []semantic.ObservationCapability{
		{Kind: semantic.ObservationWorkloadInvoked, Fields: []semantic.ObservationField{
			semantic.ObservationFieldParticipant, semantic.ObservationFieldParticipantNode,
		}},
		{Kind: semantic.ObservationMessageDropped},
		{Kind: semantic.ObservationDecisionAdvanced},
	}
	actions := []control.ActionKind{control.ActionInvoke, control.ActionDropMessage}
	memory := []controlexperiment.RiskExplorationMemoryEntry{{
		Episode: 1, CandidateID: "earlier-risk", Summary: "Earlier candidate.",
		SuspectedMechanism: "An earlier ordering was already investigated.",
		EpisodeOutcome:     controlexperiment.RiskMemoryOutcomeRiskNearMiss, RiskStatus: semantic.RiskWitnessNotReached,
		SatisfiedMilestones: []string{"invoke"}, FirstMissingMilestone: "decision",
		ProtocolPSSStates: 2, NewProtocolPSSStates: 1,
		CapabilityGaps: []controlexperiment.AgentCapabilityGap{{
			Code: controlexperiment.AgentCapabilityGapMissingControl, Reference: "persist-step",
			Summary: "the requested persist failure is unavailable",
		}},
		ModelCalls: 2, ModelTokens: 12, SearchWorkUnits: 5, ExecutionWorkUnits: 4,
	}}
	candidate := controlexperiment.RiskCandidate{
		ID: "decision-after-message-loss", PropertyRef: "decision-continuity",
		InspirationRef: "message-loss-progress", Summary: "Observe a decision after one message is dropped.",
		MechanismSteps: []controlexperiment.RiskMechanismStep{
			{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Start the client operation.", SupportRefs: []string{"primer/message-progress"}},
			{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped, Rationale: "Remove one protocol message while progress is active.", SupportRefs: []string{"primer/message-progress"}},
			{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced, Rationale: "Observe the later decision frontier.", SupportRefs: []string{"primer/message-progress"}},
		},
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped},
			{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced},
		},
	}
	alternative := candidate
	alternative.ID = "decision-after-alternative-message-loss"
	alternative.Summary = "Observe a decision after an alternative message-loss hypothesis."
	content, err := json.Marshal(controlexperiment.RiskCandidatePortfolio{
		Candidates: []controlexperiment.RiskCandidate{candidate, alternative},
	})
	if err != nil {
		t.Fatal(err)
	}
	transportCalls := 0
	client := fixtureOpenRouterIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		transportCalls++
		if request.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatal("risk call did not use activated fixture credential")
		}
		var payload openRouterChatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil ||
			payload.ResponseFormat.JSONSchema.Name != "risk_candidate_portfolio" ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("observation_capabilities")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("binding_domains")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("node-incarnation")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("issue_patterns")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("target_dossier")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("observable-only")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("exploration_memory")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("persist-step")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("do not repeat a mechanism")) ||
			bytes.Contains([]byte(payload.Messages[1].Content), []byte("oracle_findings")) ||
			!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("property_ref")) ||
			!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("mechanism_steps")) ||
			!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("support_refs")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("available_support_refs")) ||
			!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("candidates")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("Every claimed causal trigger")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("Every bind_as token must occur")) ||
			!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("^[a-z0-9]")) {
			t.Fatalf("unexpected risk request: %#v/%v", payload, err)
		}
		response := fixtureOpenRouterResponse(t, transportCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "risk-provider")
	journal, err := newStatelessAgentCallJournal(directory, client, "fixture-key")
	if err != nil || journal.SetRoot("risk-agent-fixture") != nil {
		t.Fatalf("journal setup failed: %#v/%v", journal, err)
	}
	var calledView controlexperiment.RiskAgentView
	planner := func(
		ctx context.Context, view controlexperiment.RiskAgentView,
	) ([]byte, controlexperiment.ModelWork, error) {
		calledView = view
		if err := journal.ActivateKey("fixture-key"); err != nil {
			return nil, controlexperiment.ModelWork{}, err
		}
		return planRiskCandidate(ctx, journal, view)
	}
	result, err := controlexperiment.DiscoverRiskWithPlanner(
		context.Background(), controlexperiment.RiskAgentBudget{MaxCalls: 1, MaxTokens: 20},
		knowledge, capabilities, actions, memory, nil, nil, planner,
	)
	if err != nil || result.Status != controlexperiment.RiskAgentAccepted || result.Accepted == nil ||
		result.ModelWork != (controlexperiment.ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7}) ||
		transportCalls != 1 {
		t.Fatalf("unexpected risk discovery: %#v/%v calls=%d", result, err, transportCalls)
	}
	audits, err := journal.Audits()
	if err != nil || len(audits) != 1 || audits[0].Status != controlexperiment.StatelessAgentCallContentReady {
		t.Fatalf("risk call audit missing: %#v/%v", audits, err)
	}
	recovered, err := recoverStatelessAgentCallJournal(directory, client)
	if err != nil || recovered.SetRoot("risk-agent-fixture") != nil {
		t.Fatalf("risk journal recovery failed: %#v/%v", recovered, err)
	}
	replayed, work, err := planRiskCandidate(context.Background(), recovered, calledView)
	if err != nil || !bytes.Equal(replayed, content) || work != result.ModelWork || transportCalls != 1 {
		t.Fatalf("risk recovery changed evidence: %q/%#v/%v calls=%d", replayed, work, err, transportCalls)
	}
	selectionOutput, err := scenarioInvestigationStructuredOutput(controlexperiment.ScenarioAgentView{
		MaxSteps: 0, DecisionAllowance: 0, RemainingDecisions: 0,
		AvailableIntents: []string{controlexperiment.ScenarioIntentSelect},
	})
	if err != nil || !bytes.Contains(selectionOutput.Schema, []byte("from_branch_id")) ||
		bytes.Contains(selectionOutput.Schema, []byte(`"plan"`)) {
		t.Fatalf("final selection schema can still request Runtime work: %s/%v", selectionOutput.Schema, err)
	}
	abandonOutput, err := scenarioInvestigationStructuredOutput(controlexperiment.ScenarioAgentView{
		MaxSteps: 2, DecisionAllowance: 4, RemainingDecisions: 8,
		AvailableIntents: []string{
			controlexperiment.ScenarioIntentContinue, controlexperiment.ScenarioIntentAbandon,
		},
	})
	if err != nil || !bytes.Contains(abandonOutput.Schema, []byte(`"abandon"`)) ||
		!bytes.Contains(abandonOutput.Schema, []byte(`"required":["intent"]`)) {
		t.Fatalf("hypothesis abandonment schema is not a zero-Action alternative: %s/%v", abandonOutput.Schema, err)
	}
	for _, intent := range []string{
		controlexperiment.ScenarioIntentContinue, controlexperiment.ScenarioIntentRevise,
	} {
		minimal, err := scenarioInvestigationStructuredOutput(controlexperiment.ScenarioAgentView{
			MaxSteps: 2, DecisionAllowance: 4, RemainingDecisions: 8,
			AvailableIntents: []string{intent},
		})
		if err != nil || !bytes.Contains(minimal.Schema, []byte(`"required":["intent","plan"]`)) ||
			!bytes.Contains(minimal.Schema, []byte(`"message_type_hint"`)) ||
			!bytes.Contains(minimal.Schema, []byte(`"effect_phase"`)) ||
			!bytes.Contains(minimal.Schema, []byte(`"effect_outcome"`)) ||
			bytes.Contains(minimal.Schema, []byte(`"branch_id"`)) ||
			bytes.Contains(minimal.Schema, []byte(`"from_branch_id"`)) ||
			bytes.Contains(minimal.Schema, []byte(`"reference_branch_id"`)) {
			t.Fatalf("%s phase did not receive a minimal path schema: %s/%v", intent, minimal.Schema, err)
		}
	}
}

func TestRiskAgentPromptUsesKnowledgeQueryThenPortfolioResponse(t *testing.T) {
	knowledge, _, _, err := loadEtcdraftAgenticAuthoringSource(etcdraftAgenticTestInputPath)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := controlexperiment.KnowledgeSourceCatalog(knowledge)
	if err != nil {
		t.Fatal(err)
	}
	var selected controlexperiment.KnowledgeSource
	for _, source := range sources {
		if source.Reference == "adapters/etcdraftv2/adapter.go:Check" {
			selected = source
			break
		}
	}
	if selected.Reference == "" {
		t.Fatal("active etcd/raft Dossier lost its declared Check source")
	}
	view := controlexperiment.RiskAgentView{
		Knowledge: knowledge,
		ObservationCapabilities: []semantic.ObservationCapability{
			{Kind: semantic.ObservationWorkloadInvoked},
			{Kind: semantic.ObservationMessageDropped},
			{Kind: semantic.ObservationDecisionAdvanced},
		},
		Actions:              []control.ActionKind{control.ActionInvoke, control.ActionDropMessage},
		MaxMilestones:        controlexperiment.RiskCandidateMaxSteps,
		MaxCandidates:        controlexperiment.RiskCandidatePortfolioMax,
		KnowledgeSources:     sources,
		MaxKnowledgeRequests: controlexperiment.RiskKnowledgeRequestsPerCall,
	}
	view.BindingDomains, err = semantic.ObservationBindingDomains(view.ObservationCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	view.AvailableSupportRefs = controlexperiment.VisibleRiskSupportRefs(
		view.Knowledge, view.TargetSurface, view.KnowledgeResults,
	)
	system, user, err := riskAgentPrompt(view)
	if err != nil {
		t.Fatal(err)
	}
	output, err := riskAgentStructuredOutput(view)
	if err != nil || !strings.Contains(system, "RiskAgentResponseEnvelope") ||
		!strings.Contains(user, "Prefer a direct portfolio") ||
		!bytes.Contains([]byte(user), []byte("adapter.go:Check")) ||
		output.Name != "risk_grounding_or_portfolio" ||
		len(output.Schema) > 32<<10 ||
		!bytes.Contains(output.Schema, []byte("knowledge_requests")) ||
		!bytes.Contains(output.Schema, []byte("adapters/etcdraftv2/adapter.go:Check")) ||
		!bytes.Contains(output.Schema, []byte("candidates")) ||
		!bytes.Contains(output.Schema, []byte("response_kind")) {
		t.Fatalf("knowledge-assisted Risk contract incomplete: %s\n%s\n%s\n%v", system, user, output.Schema, err)
	}
	view.KnowledgeResults = []controlexperiment.KnowledgeReadResult{{
		Status: controlexperiment.KnowledgeDiscoveryCompleted, Source: selected,
		StartLine: 10, EndLine: 12, TotalLines: 100,
		Text: "func (adapter *Adapter) Check(action control.Action) error {\n  return nil\n}", Truncated: true,
	}}
	view.AvailableSupportRefs = controlexperiment.VisibleRiskSupportRefs(
		view.Knowledge, view.TargetSurface, view.KnowledgeResults,
	)
	system, user, err = riskAgentPrompt(view)
	output, outputErr := riskAgentStructuredOutput(view)
	if err != nil || outputErr != nil || output.Name != "risk_grounding_or_portfolio" ||
		!strings.Contains(user, "end_line plus one") ||
		!bytes.Contains([]byte(user), []byte("func (adapter *Adapter) Check")) ||
		!bytes.Contains(output.Schema, []byte("start_line")) ||
		!bytes.Contains(output.Schema, []byte("knowledge_requests")) {
		t.Fatalf("completed source window did not preserve bounded navigation: %s\n%s\n%s\n%v/%v",
			system, user, output.Schema, err, outputErr)
	}
	view.MaxKnowledgeRequests = 0
	system, user, err = riskAgentPrompt(view)
	output, outputErr = riskAgentStructuredOutput(view)
	var portfolioSchema struct {
		Properties struct {
			Candidates struct {
				MinItems int `json:"minItems"`
			} `json:"candidates"`
		} `json:"properties"`
	}
	schemaErr := json.Unmarshal(output.Schema, &portfolioSchema)
	if err != nil || outputErr != nil || !strings.Contains(system, "RiskCandidatePortfolio") ||
		!strings.Contains(user, "bounded knowledge_results") ||
		!bytes.Contains([]byte(user), []byte("func (adapter *Adapter) Check")) ||
		output.Name != "risk_candidate_portfolio" ||
		schemaErr != nil || portfolioSchema.Properties.Candidates.MinItems != 1 ||
		!bytes.Contains(output.Schema, []byte("candidates")) ||
		bytes.Contains(output.Schema, []byte("knowledge_requests")) {
		t.Fatalf("knowledge result did not transition to portfolio: %s\n%s\n%s\n%v/%v",
			system, user, output.Schema, err, outputErr)
	}
}

func TestRiskAgentProviderJournalReadsSourceThenAcceptsPortfolio(t *testing.T) {
	knowledge, _, _, err := loadOmnipaxosAgenticAuthoringSource(omnipaxosAgenticTestInputPath)
	if err != nil {
		t.Fatal(err)
	}
	reference := "adapters/omnipaxosv2/adapter.go:Manifest"
	requestBytes, err := json.Marshal(struct {
		ResponseKind      string                                   `json:"response_kind"`
		Candidates        []controlexperiment.RiskCandidate        `json:"candidates"`
		KnowledgeRequests []controlexperiment.KnowledgeReadRequest `json:"knowledge_requests"`
	}{
		ResponseKind: "knowledge-query", Candidates: []controlexperiment.RiskCandidate{},
		KnowledgeRequests: []controlexperiment.KnowledgeReadRequest{{
			Reference: reference, MaxLines: 40,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	portfolioBytes, err := json.Marshal(omnipaxosDiscoveredRiskPortfolio())
	if err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	client := fixtureOpenRouterIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		var payload openRouterChatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		var content []byte
		switch providerCalls {
		case 1:
			if payload.ResponseFormat.JSONSchema.Name != "risk_grounding_or_portfolio" ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("knowledge_requests")) ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("candidates")) ||
				!bytes.Contains([]byte(payload.Messages[1].Content), []byte(reference)) {
				t.Fatalf("first Risk call did not expose declared sources: %#v", payload)
			}
			content = requestBytes
		case 2:
			if payload.ResponseFormat.JSONSchema.Name != "risk_candidate_portfolio" ||
				!bytes.Contains([]byte(payload.Messages[1].Content), []byte("knowledge_results")) ||
				!bytes.Contains([]byte(payload.Messages[1].Content), []byte("func (adapter *Adapter) Manifest")) {
				t.Fatalf("second Risk call did not receive the source excerpt: %#v", payload.Messages)
			}
			content = portfolioBytes
		default:
			t.Fatalf("unexpected provider call %d", providerCalls)
		}
		response := fixtureOpenRouterResponse(t, providerCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	journal, err := newStatelessAgentCallJournal(filepath.Join(t.TempDir(), "knowledge-risk"), client, "fixture-key")
	if err != nil || journal.SetRoot("knowledge-risk-root") != nil {
		t.Fatalf("journal setup failed: %#v/%v", journal, err)
	}
	planner := func(ctx context.Context, view controlexperiment.RiskAgentView) (
		[]byte, controlexperiment.ModelWork, error,
	) {
		if err := journal.ActivateKey("fixture-key"); err != nil {
			return nil, controlexperiment.ModelWork{}, err
		}
		return planRiskCandidate(ctx, journal, view)
	}
	reader := func(request controlexperiment.KnowledgeReadRequest) (
		controlexperiment.KnowledgeReadResult, error,
	) {
		return controlexperiment.ReadDeclaredKnowledgeSource("../..", knowledge, request)
	}
	result, err := controlexperiment.DiscoverRiskWithPlanner(
		context.Background(), controlexperiment.RiskAgentBudget{MaxCalls: 2, MaxTokens: 20},
		knowledge, (omnipaxosv2.ObservationProjector{}).Capabilities(),
		[]control.ActionKind{control.ActionInvoke, control.ActionDropMessage}, nil, nil, reader, planner,
	)
	audits, auditErr := journal.Audits()
	if err != nil || auditErr != nil || result.Status != controlexperiment.RiskAgentAccepted ||
		result.Accepted == nil || len(result.Attempts) != 2 || len(audits) != 2 || providerCalls != 2 ||
		result.Attempts[0].Feedback.ReasonCode != controlexperiment.RiskAgentReasonKnowledgeRead ||
		len(result.Attempts[0].KnowledgeResults) != 1 ||
		audits[0].ProviderUsageStatus != agentProviderUsageObserved ||
		audits[1].ProviderUsageStatus != agentProviderUsageObserved ||
		result.ModelWork != (controlexperiment.ModelWork{Calls: 2, InputTokens: 8, OutputTokens: 6, TotalTokens: 14}) {
		t.Fatalf("provider-backed knowledge loop failed: %#v audits=%#v calls=%d err=%v/%v",
			result, audits, providerCalls, err, auditErr)
	}
}

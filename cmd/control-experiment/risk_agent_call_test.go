package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
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
		PropertyRef: "decision-continuity", EvidenceLevel: controlexperiment.PropertyEvidenceObservable,
		SuspectedMechanism: "An earlier ordering was already investigated.",
		EpisodeOutcome:     controlexperiment.RiskMemoryOutcomeWitnessNearMiss, RiskStatus: semantic.RiskWitnessNotReached,
		SatisfiedMilestones: []string{"invoke"}, FirstMissingMilestone: "decision",
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
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("property_ref")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("evidence_level")) ||
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

func TestRiskAgentCompactsPortfolioAfterLengthWithoutRepeatingGrounding(t *testing.T) {
	knowledge, _, _, err := loadEtcdraftAgenticAuthoringSource(etcdraftAgenticTestInputPath)
	if err != nil {
		t.Fatal(err)
	}
	candidate := etcdraftAlternateQuorumRiskCandidate()
	for _, predicate := range candidate.Predicates {
		candidate.MechanismSteps = append(candidate.MechanismSteps, controlexperiment.RiskMechanismStep{
			MilestoneID: predicate.MilestoneID,
			Kind:        predicate.Kind,
			Rationale:   "Exercise the ordered etcd/raft replication-loss milestone.",
			SupportRefs: []string{"primer/epoch-and-replication"},
		})
	}
	candidate.SuspectedMechanism = ""
	content, err := json.Marshal(controlexperiment.RiskCandidatePortfolio{
		Candidates: []controlexperiment.RiskCandidate{candidate},
	})
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
		if providerCalls == 1 {
			if payload.ResponseFormat.JSONSchema.Name != "risk_candidate_portfolio" {
				t.Fatalf("initial call did not request a portfolio: %s", payload.ResponseFormat.JSONSchema.Name)
			}
			response, marshalErr := json.Marshal(map[string]any{
				"id": "risk-length", "model": openRouterFixtureModel,
				"system_fingerprint": "fixture-provider", "choices": []any{map[string]any{
					"index": 0, "message": map[string]any{
						"role": "assistant", "content": `{"candidates":[`,
					}, "finish_reason": "length",
				}},
				"usage": map[string]int{"prompt_tokens": 4, "completion_tokens": 32, "total_tokens": 36},
			})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
		}
		if providerCalls != 2 ||
			payload.ResponseFormat.JSONSchema.Name != "risk_candidate_portfolio_compact_repair" ||
			!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"maxItems":1`)) ||
			!strings.Contains(payload.Messages[1].Content, "preceding portfolio response reached") ||
			strings.Contains(payload.Messages[1].Content, "source grounding is mandatory") {
			t.Fatalf("compact repair request drifted: call=%d name=%s", providerCalls,
				payload.ResponseFormat.JSONSchema.Name)
		}
		response := fixtureOpenRouterResponse(t, providerCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "risk-length-repair")
	journal, err := newStatelessAgentCallJournal(directory, client, "")
	if err != nil || journal.SetRoot("risk-length-root") != nil {
		t.Fatalf("journal setup failed: %#v/%v", journal, err)
	}
	planner := func(ctx context.Context, view controlexperiment.RiskAgentView) (
		[]byte, controlexperiment.ModelWork, error,
	) {
		response, work, callErr := planRiskCandidate(ctx, journal, view)
		if !errors.Is(callErr, errStatelessAgentCallKeyRequired) {
			return response, work, callErr
		}
		if err := journal.ActivateKey("fixture-key"); err != nil {
			return nil, work, err
		}
		return planRiskCandidate(ctx, journal, view)
	}
	result, err := controlexperiment.DiscoverRiskWithPlanner(
		context.Background(), controlexperiment.RiskAgentBudget{MaxCalls: 2, MaxTokens: 100},
		knowledge, (etcdraftv2.ObservationProjector{}).Capabilities(),
		[]control.ActionKind{
			control.ActionInvoke, control.ActionCompleteEffect, control.ActionDeliverMessage,
			control.ActionDropMessage, control.ActionFireTemporal,
		}, nil, nil, nil, planner,
	)
	if err != nil || result.Status != controlexperiment.RiskAgentAccepted || result.Accepted == nil ||
		len(result.Attempts) != 2 ||
		result.Attempts[0].Feedback.ReasonCode != controlexperiment.RiskAgentReasonResponseFinishLength ||
		len(result.Attempts[0].ResponseBytes) != 0 || result.ModelWork.Calls != 2 ||
		result.ModelWork.TotalTokens != 43 || providerCalls != 2 {
		t.Fatalf("Risk length repair did not close: result=%#v err=%v calls=%d", result, err, providerCalls)
	}
	audits, err := journal.Audits()
	if err != nil || len(audits) != 2 ||
		audits[0].Status != controlexperiment.StatelessAgentCallFailed ||
		journal.recovered[0].result.FailureCode != statelessAgentFailureFinishLength ||
		audits[1].Status != controlexperiment.StatelessAgentCallContentReady {
		t.Fatalf("Risk length evidence missing: %#v/%v", audits, err)
	}
	recovered, err := recoverStatelessAgentCallJournal(directory, client)
	if err != nil || recovered.SetRoot("risk-length-root") != nil {
		t.Fatalf("Risk length repair journal did not recover: %#v/%v", recovered, err)
	}
	recoveredAudits, err := recovered.Audits()
	if err != nil || len(recoveredAudits) != 2 || providerCalls != 2 {
		t.Fatalf("Risk recovery repeated provider work: %#v/%v calls=%d",
			recoveredAudits, err, providerCalls)
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
	var unmatched controlexperiment.KnowledgeSource
	for _, source := range sources {
		if source.Reference == "adapters/etcdraftv2/adapter.go:Check" {
			selected = source
		} else if unmatched.Reference == "" {
			unmatched = source
		}
	}
	if selected.Reference == "" || unmatched.Reference == "" {
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
		!strings.Contains(user, "source grounding is mandatory") ||
		!strings.Contains(user, "must perform one neutral keyword search") ||
		!strings.Contains(user, "choose a property; identify the supplied protocol invariant") ||
		!strings.Contains(user, "same concrete message instance") ||
		!strings.Contains(user, "term/ballot advance") ||
		!strings.Contains(user, "Prefer including at least one mechanically executable candidate for an oracle-backed property") ||
		!strings.Contains(user, "not an admission requirement") ||
		strings.Contains(user, "Prefer a direct portfolio") ||
		strings.Contains(user, "msgsAfterAppend") ||
		!bytes.Contains([]byte(user), []byte("adapter.go:Check")) ||
		output.Name != "risk_grounding_search" ||
		len(output.Schema) > 32<<10 ||
		!bytes.Contains(output.Schema, []byte("knowledge_requests")) ||
		!bytes.Contains(output.Schema, []byte(`"query"`)) ||
		bytes.Contains(output.Schema, []byte(`"reference"`)) ||
		bytes.Contains(output.Schema, []byte("adapters/etcdraftv2/adapter.go:Check")) ||
		!bytes.Contains(output.Schema, []byte("candidates")) ||
		!bytes.Contains(output.Schema, []byte("response_kind")) {
		t.Fatalf("knowledge-assisted Risk contract incomplete: %s\n%s\n%s\n%v", system, user, output.Schema, err)
	}
	view.KnowledgeResults = []controlexperiment.KnowledgeReadResult{{
		Status:     controlexperiment.KnowledgeDiscoveryStopped,
		ReasonCode: controlexperiment.KnowledgeDiscoverySearchNoMatch,
		Query:      "missing phrase",
	}}
	view.AvailableSupportRefs = controlexperiment.VisibleRiskSupportRefs(
		view.Knowledge, view.TargetSurface, view.KnowledgeResults,
	)
	system, user, err = riskAgentPrompt(view)
	output, outputErr := riskAgentStructuredOutput(view)
	if err != nil || outputErr != nil || output.Name != "risk_grounding_search" ||
		!strings.Contains(user, "preceding search stopped or had no match") ||
		!strings.Contains(user, "one case-insensitive literal substring") ||
		!strings.Contains(user, "does not tokenize words") ||
		!strings.Contains(user, "different short literal substring") ||
		bytes.Contains(output.Schema, []byte(`"reference"`)) {
		t.Fatalf("stopped search did not remain in the search phase: %s\n%s\n%s\n%v/%v",
			system, user, output.Schema, err, outputErr)
	}
	view.KnowledgeResults = []controlexperiment.KnowledgeReadResult{{
		Status: controlexperiment.KnowledgeDiscoveryCompleted, Query: "adapter check",
		Matches: []controlexperiment.KnowledgeSearchMatch{{
			Reference: selected.Reference, Line: 10, Preview: "func (adapter *Adapter) Check",
		}},
	}}
	view.AvailableSupportRefs = controlexperiment.VisibleRiskSupportRefs(
		view.Knowledge, view.TargetSurface, view.KnowledgeResults,
	)
	system, user, err = riskAgentPrompt(view)
	output, outputErr = riskAgentStructuredOutput(view)
	if err != nil || outputErr != nil || output.Name != "risk_grounding_read" ||
		!strings.Contains(user, "repository search has completed") ||
		!strings.Contains(user, "do not select an arbitrary Dossier-declared source") ||
		!bytes.Contains(output.Schema, []byte(selected.Reference)) ||
		bytes.Contains(output.Schema, []byte(unmatched.Reference)) ||
		!bytes.Contains(output.Schema, []byte(`"reference"`)) ||
		bytes.Contains(output.Schema, []byte(`"query"`)) {
		t.Fatalf("completed search did not narrow the read phase: %s\n%s\n%s\n%v/%v",
			system, user, output.Schema, err, outputErr)
	}
	view.KnowledgeResults = append(view.KnowledgeResults, controlexperiment.KnowledgeReadResult{
		Status:     controlexperiment.KnowledgeDiscoveryStopped,
		ReasonCode: controlexperiment.KnowledgeDiscoveryLocatorNotFound,
		Source:     selected,
	})
	view.AvailableSupportRefs = controlexperiment.VisibleRiskSupportRefs(
		view.Knowledge, view.TargetSurface, view.KnowledgeResults,
	)
	system, user, err = riskAgentPrompt(view)
	output, outputErr = riskAgentStructuredOutput(view)
	if err != nil || outputErr != nil || output.Name != "risk_grounding_read" ||
		!strings.Contains(user, "preceding read stopped") ||
		!bytes.Contains(output.Schema, []byte(selected.Reference)) ||
		bytes.Contains(output.Schema, []byte(unmatched.Reference)) ||
		bytes.Contains(output.Schema, []byte(`"query"`)) {
		t.Fatalf("stopped read escaped the search-matched read phase: %s\n%s\n%s\n%v/%v",
			system, user, output.Schema, err, outputErr)
	}
	view.KnowledgeResults = append(view.KnowledgeResults, controlexperiment.KnowledgeReadResult{
		Status: controlexperiment.KnowledgeDiscoveryCompleted, Source: selected,
		StartLine: 10, EndLine: 12, TotalLines: 100,
		Text: "func (adapter *Adapter) Check(action control.Action) error {\n  return nil\n}", Truncated: true,
	})
	view.AvailableSupportRefs = controlexperiment.VisibleRiskSupportRefs(
		view.Knowledge, view.TargetSurface, view.KnowledgeResults,
	)
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
	reference := "adapters/omnipaxosv2/adapter.go"
	searchBytes, err := json.Marshal(struct {
		ResponseKind      string                                   `json:"response_kind"`
		Candidates        []controlexperiment.RiskCandidate        `json:"candidates"`
		KnowledgeRequests []controlexperiment.KnowledgeReadRequest `json:"knowledge_requests"`
	}{
		ResponseKind: "knowledge-query", Candidates: []controlexperiment.RiskCandidate{},
		KnowledgeRequests: []controlexperiment.KnowledgeReadRequest{{
			Query: "package omnipaxosv2", MaxResults: 10,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	readBytes, err := json.Marshal(struct {
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
			if payload.ResponseFormat.JSONSchema.Name != "risk_grounding_search" ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("knowledge_requests")) ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"query"`)) ||
				bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"reference"`)) ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("candidates")) ||
				!bytes.Contains([]byte(payload.Messages[1].Content), []byte("No repository search has completed yet")) {
				t.Fatalf("first Risk call did not expose declared sources: %#v", payload)
			}
			content = searchBytes
		case 2:
			if payload.ResponseFormat.JSONSchema.Name != "risk_grounding_read" ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("knowledge_requests")) ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"reference"`)) ||
				bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"query"`)) ||
				!bytes.Contains([]byte(payload.Messages[1].Content), []byte("knowledge_results")) ||
				!bytes.Contains([]byte(payload.Messages[1].Content), []byte(reference)) {
				t.Fatalf("second Risk call did not receive the neutral search result: %#v", payload.Messages)
			}
			content = readBytes
		case 3:
			if payload.ResponseFormat.JSONSchema.Name != "risk_candidate_portfolio" ||
				!bytes.Contains([]byte(payload.Messages[1].Content), []byte("knowledge_results")) ||
				!bytes.Contains([]byte(payload.Messages[1].Content), []byte("package omnipaxosv2")) {
				t.Fatalf("third Risk call did not receive the source excerpt: %#v", payload.Messages)
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
		mounts := []controlexperiment.KnowledgeSourceMount{{Directory: "../.."}}
		if request.Query != "" {
			return controlexperiment.SearchMountedKnowledgeSources(mounts, request)
		}
		return controlexperiment.ReadMountedKnowledgeSourceFromMounts(
			mounts,
			controlexperiment.KnowledgeSource{Reference: request.Reference, Path: request.Reference},
			request,
		)
	}
	result, err := controlexperiment.DiscoverRiskWithPlanner(
		context.Background(), controlexperiment.RiskAgentBudget{MaxCalls: 3, MaxTokens: 30},
		knowledge, (omnipaxosv2.ObservationProjector{}).Capabilities(),
		[]control.ActionKind{control.ActionInvoke, control.ActionDropMessage}, nil, nil, reader, planner,
	)
	audits, auditErr := journal.Audits()
	if err != nil || auditErr != nil || result.Status != controlexperiment.RiskAgentAccepted ||
		result.Accepted == nil || len(result.Attempts) != 3 || len(audits) != 3 || providerCalls != 3 ||
		result.Attempts[0].Feedback.ReasonCode != controlexperiment.RiskAgentReasonKnowledgeRead ||
		len(result.Attempts[0].KnowledgeResults) != 1 ||
		result.Attempts[1].Feedback.ReasonCode != controlexperiment.RiskAgentReasonKnowledgeRead ||
		len(result.Attempts[1].KnowledgeResults) != 1 ||
		audits[0].ProviderUsageStatus != agentProviderUsageObserved ||
		audits[1].ProviderUsageStatus != agentProviderUsageObserved ||
		audits[2].ProviderUsageStatus != agentProviderUsageObserved ||
		result.ModelWork != (controlexperiment.ModelWork{Calls: 3, InputTokens: 12, OutputTokens: 9, TotalTokens: 21}) {
		t.Fatalf("provider-backed knowledge loop failed: %#v audits=%#v calls=%d err=%v/%v",
			result, audits, providerCalls, err, auditErr)
	}
}

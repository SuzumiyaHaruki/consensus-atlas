package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"

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
		Risks: []controlexperiment.ProtocolRisk{{
			ID: "existing-message-risk", Summary: "Existing message risk.",
			RequiredCapabilities: []string{"runtime-message-control"},
			RequiredActions:      []control.ActionKind{control.ActionDropMessage, control.ActionInvoke},
			AllowedBackendIDs:    []string{controlexperiment.ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := []semantic.ObservationCapability{
		{Kind: semantic.ObservationWorkloadInvoked},
		{Kind: semantic.ObservationMessageDropped},
		{Kind: semantic.ObservationDecisionAdvanced},
	}
	actions := []control.ActionKind{control.ActionInvoke, control.ActionDropMessage}
	candidate := controlexperiment.RiskCandidate{
		ID: "decision-after-message-loss", Summary: "Observe a decision after one message is dropped.",
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped},
			{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced},
		},
	}
	content, err := json.Marshal(candidate)
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
			payload.ResponseFormat.JSONSchema.Name != "risk_candidate" ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("observation_capabilities")) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("Every bind_as token must occur")) ||
			!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte("^[a-z0-9]")) {
			t.Fatalf("unexpected risk request: %#v/%v", payload, err)
		}
		response := a2b2OpenRouterResponse(t, transportCalls, content)
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
		knowledge, capabilities, actions, planner,
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
}

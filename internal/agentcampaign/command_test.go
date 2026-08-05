package agentcampaign_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/agentcampaign"
)

func TestCommandPlannerUsesVersionedIsolatedBoundary(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONSENSUS_ATLAS_PARENT_SECRET", "must-not-be-inherited")
	planner := &agentcampaign.CommandPlanner{
		Path: os.Args[0], Args: []string{"-test.run=TestCommandPlannerHelper", "--"}, Dir: root,
		Env: []string{"CONSENSUS_ATLAS_PLANNER_HELPER=1"},
	}
	proposal, err := planner.Generate(context.Background(), agentcampaign.GenerationRequest{Version: 1, Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ID != "command-proposal" || proposal.Plan.ID != "command-plan" {
		t.Fatalf("unexpected command proposal: %+v", proposal)
	}
	audit := planner.LastGenerationAudit()
	if audit.Provider != "fixture" || audit.Model != "fixture-model" || len(audit.RequestDigest) != 64 || len(audit.ResponseDigest) != 64 {
		t.Fatalf("unexpected command audit: %+v", audit)
	}
}

func TestCommandPlannerRetainsUsageWhenProposalIsRejected(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	planner := &agentcampaign.CommandPlanner{
		Path: os.Args[0], Args: []string{"-test.run=TestCommandPlannerHelper", "--"}, Dir: root,
		Env: []string{"CONSENSUS_ATLAS_PLANNER_HELPER=1", "CONSENSUS_ATLAS_INVALID_PROPOSAL=1"},
	}
	if _, err := planner.Generate(context.Background(), agentcampaign.GenerationRequest{Version: 1, Attempt: 1}); err == nil {
		t.Fatal("proposal with an unknown field was accepted")
	}
	audit := planner.LastGenerationAudit()
	if audit.Provider != "fixture" || audit.TotalTokens != 7 || len(audit.ResponseDigest) != 64 {
		t.Fatalf("usage audit was lost with invalid proposal: %+v", audit)
	}
}

func TestCommandPlannerHelper(t *testing.T) {
	if os.Getenv("CONSENSUS_ATLAS_PLANNER_HELPER") != "1" {
		return
	}
	if os.Getenv("CONSENSUS_ATLAS_PARENT_SECRET") != "" {
		os.Exit(20)
	}
	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&envelope); err != nil || envelope.Version != 1 {
		os.Exit(21)
	}
	proposal := map[string]any{
		"version": 1, "id": "command-proposal",
		"plan": map[string]any{
			"id": "command-plan", "targets": []string{"target"},
			"stimuli": []any{map[string]any{"kind": "campaign", "target": "n1"}},
			"search": map[string]any{
				"strategy": "dfs",
				"config": map[string]any{
					"max_runs": 1, "budget_per_run": 1, "decision_budget": 1, "seed": 0,
					"actions": map[string]any{
						"drop_messages": false, "duplicate_messages": false, "max_duplicates_per_run": 0,
					},
				},
			},
		},
	}
	if os.Getenv("CONSENSUS_ATLAS_INVALID_PROPOSAL") == "1" {
		proposal["unknown"] = true
	}
	response := map[string]any{
		"version":  1,
		"proposal": proposal,
		"audit":    map[string]any{"provider": "fixture", "model": "fixture-model", "total_tokens": 7},
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		os.Exit(22)
	}
	os.Exit(0)
}

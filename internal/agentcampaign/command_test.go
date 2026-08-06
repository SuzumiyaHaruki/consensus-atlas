package agentcampaign_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/agentcampaign"
)

func TestBlindCommandPlannerUsesOnlyBlindRequest(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONSENSUS_ATLAS_PARENT_SECRET", "must-not-be-inherited")
	planner := &agentcampaign.BlindCommandPlanner{Path: os.Args[0], Args: []string{"-test.run=TestBlindCommandPlannerHelper", "--"}, Dir: root, Env: []string{"CONSENSUS_ATLAS_PLANNER_HELPER=1"}}
	proposal, err := planner.GenerateBlind(context.Background(), agentcampaign.BlindGenerationRequest{Version: 1, Attempt: 1, Debt: []agentcampaign.BlindDebt{{Ref: "debt-opaque-ref", Category: "property", Risk: "high"}}})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ID != "blind-command-proposal" || proposal.Plan.Targets[0] != "debt-opaque-ref" {
		t.Fatalf("unexpected blind command proposal: %+v", proposal)
	}
	audit := planner.LastBlindGenerationAudit()
	if audit.Provider != "fixture" || audit.Model != "fixture-model" || len(audit.RequestDigest) != 64 || len(audit.ResponseDigest) != 64 {
		t.Fatalf("unexpected blind command audit: %+v", audit)
	}
}

func TestBlindCommandPlannerHelper(t *testing.T) {
	if os.Getenv("CONSENSUS_ATLAS_PLANNER_HELPER") != "1" {
		return
	}
	if os.Getenv("CONSENSUS_ATLAS_PARENT_SECRET") != "" {
		os.Exit(20)
	}
	var envelope struct {
		Version int             `json:"version"`
		Request json.RawMessage `json:"request"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&envelope); err != nil || envelope.Version != 1 {
		os.Exit(21)
	}
	if bytes.Contains(envelope.Request, []byte("must-not-be-inherited")) || bytes.Contains(envelope.Request, []byte("private-trigger-id")) {
		os.Exit(23)
	}
	proposal := map[string]any{"version": 1, "id": "blind-command-proposal", "plan": map[string]any{"id": "blind-command-plan", "targets": []string{"debt-opaque-ref"}, "stimuli": []any{map[string]any{"kind": "campaign", "target": "n1"}}, "search": map[string]any{"strategy": "dfs", "config": map[string]any{"max_runs": 1, "budget_per_run": 1, "decision_budget": 1, "seed": 0, "actions": map[string]any{"drop_messages": false, "duplicate_messages": false, "max_duplicates_per_run": 0}}}}}
	response := map[string]any{"version": 1, "proposal": proposal, "audit": map[string]any{"provider": "fixture", "model": "fixture-model", "total_tokens": 7}}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		os.Exit(22)
	}
	os.Exit(0)
}

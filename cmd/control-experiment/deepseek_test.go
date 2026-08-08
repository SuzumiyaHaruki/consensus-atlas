package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestDeepSeekRequestExposesOnlyReadOnlyScopeProjection(t *testing.T) {
	scope := controlexperiment.PlannerScope{
		ExperimentID: "private-experiment", PSSID: "private-pss",
		Runtime:         controlexperiment.RuntimeConfig{SeedHex: "736563726574", MaxClones: 1},
		DecisionsPerRun: 32, RequireReplay: true, Runs: []int{1, 2},
	}
	request, err := deepSeekRequest(scope)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"private-experiment", "private-pss", "736563726574", "seed_hex", "pss_id"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("request leaked %q: %s", forbidden, text)
		}
	}
	if request.Scope.Digest == "" || len(request.Scope.Runs) != 2 || len(request.ActionKinds) == 0 {
		t.Fatalf("incomplete request projection: %#v", request)
	}
}

func TestDecodeDeepSeekPlannerResponseBindsAuditAndProposal(t *testing.T) {
	digest := strings.Repeat("a", 64)
	response := map[string]any{
		"version": 1,
		"proposal": map[string]any{
			"schema_version": controlexperiment.PlannerProposalVersion,
			"policies": []any{
				map[string]any{"run": 1, "seed_hex": "01"},
				map[string]any{"run": 2, "seed_hex": "02"},
			},
		},
		"audit": map[string]any{
			"provider": "deepseek", "requested_model": "deepseek-v4-flash",
			"response_model": "deepseek-v4-flash",
			"endpoint":       "https://api.deepseek.com/chat/completions",
			"prompt_digest":  digest, "finish_reason": "stop", "thinking_mode": "disabled",
			"temperature": 0, "max_tokens": 1800, "prompt_tokens": 10,
			"prompt_cache_hit_tokens": 6, "prompt_cache_miss_tokens": 4,
			"completion_tokens": 5, "reasoning_tokens": 0, "total_tokens": 15,
			"duration_millis": 1,
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	proposal, audit, err := decodeDeepSeekPlannerResponse([]byte("request\n"), encoded, deepSeekPlannerConfig{
		Model: "deepseek-v4-flash", Endpoint: "https://api.deepseek.com/chat/completions",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Policies) != 2 || audit.RequestDigest != byteDigest([]byte("request\n")) ||
		audit.ResponseDigest != byteDigest(encoded) {
		t.Fatalf("unexpected decoded response: %#v %#v", proposal, audit)
	}
}

func TestDeepSeekEndpointAllowlist(t *testing.T) {
	if !officialDeepSeekEndpoint("https://api.deepseek.com/chat/completions") ||
		officialDeepSeekEndpoint("https://example.com/chat/completions") {
		t.Fatal("official endpoint allowlist mismatch")
	}
}

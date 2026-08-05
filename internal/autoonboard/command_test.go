package autoonboard_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

func TestCommandGeneratorUsesVersionedIsolatedBoundary(t *testing.T) {
	root := filepath.Join("..", "..")
	contract, err := protocolcontract.Load(filepath.Join(root, "contracts", "etcdraft-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONSENSUS_ATLAS_PARENT_SECRET", "must-not-be-inherited")
	generator := &autoonboard.CommandGenerator{
		Path: os.Args[0], Args: []string{"-test.run=TestCommandGeneratorHelper", "--"}, Dir: root,
		Env: []string{"CONSENSUS_ATLAS_COMMAND_HELPER=1"},
	}
	binding, err := generator.Generate(context.Background(), autoonboard.GenerationRequest{Contract: contract, Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	if binding.ID != "command-generated" {
		t.Fatalf("binding id = %q", binding.ID)
	}
	audit := generator.LastGenerationAudit()
	if audit.Provider != "fixture" || audit.Model != "fixture-model" || len(audit.RequestDigest) != 64 || len(audit.ResponseDigest) != 64 {
		t.Fatalf("unexpected audit: %+v", audit)
	}
}

func TestCommandGeneratorHelper(t *testing.T) {
	if os.Getenv("CONSENSUS_ATLAS_COMMAND_HELPER") != "1" {
		return
	}
	if os.Getenv("CONSENSUS_ATLAS_PARENT_SECRET") != "" {
		os.Exit(20)
	}
	var request struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil || request.Version != 1 {
		os.Exit(21)
	}
	response := map[string]any{
		"version": 1,
		"binding": map[string]any{
			"version": 1, "id": "command-generated", "contract_id": "fixture",
			"contract_digest": "fixture", "protocol": "fixture", "driver": "fixture",
			"capabilities": []any{}, "operations": []any{}, "witnesses": []any{},
		},
		"audit": map[string]any{"provider": "fixture", "model": "fixture-model"},
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		os.Exit(22)
	}
	os.Exit(0)
}

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/agentcampaign"
)

func TestValidateSecretFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "key.txt")
	if err := os.WriteFile(path, []byte("not-a-real-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSecretFile(path); err != nil {
		t.Fatalf("private key file rejected: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateSecretFile(path); err == nil {
		t.Fatal("group-readable key file accepted")
	}
	link := filepath.Join(directory, "key-link.txt")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := validateSecretFile(link); err == nil {
		t.Fatal("symlink key file accepted")
	}
}

func TestRunRejectsMissingBlindScopeBeforeReadingKey(t *testing.T) {
	err := run(context.Background(), config{
		repoRoot: ".", keyPath: filepath.Join(t.TempDir(), "missing-key.txt"),
		model: "fixture", endpoint: "https://example.invalid",
	})
	if err == nil || !strings.Contains(err.Error(), "blind scope") {
		t.Fatalf("missing blind scope did not fail first: %v", err)
	}
}

func TestRunRequiresTrustedReplayAndCampaignOutputsTogether(t *testing.T) {
	err := run(context.Background(), config{
		repoRoot: ".", keyPath: filepath.Join(t.TempDir(), "missing-key.txt"), model: "fixture", endpoint: "https://example.invalid",
		trustedCampaignOut: "campaign.json",
		scope:              agentcampaign.BlindScope{BenchmarkID: "fixture", BenchmarkDigest: strings.Repeat("a", 64), TrialID: "trial-01"},
	})
	if err == nil || !strings.Contains(err.Error(), "trusted-campaign-out") {
		t.Fatalf("unpaired trusted output was accepted or read key first: %v", err)
	}
}

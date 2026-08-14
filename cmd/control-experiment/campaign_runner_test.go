package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestRetiredCampaignCLIFlagsFailClosed(t *testing.T) {
	if err := run(context.Background(), []string{
		"-strategy", "campaign-etcdraft-agent-v1",
		"-campaign-dir", "unused",
		"-out", "unused.json",
	}, io.Discard); err == nil || !strings.Contains(err.Error(), "Campaign flags require") {
		t.Fatalf("retired Agent Campaign remains reachable: %v", err)
	}
	if err := run(context.Background(), []string{
		"-strategy", etcdraftStatelessCanonicalCampaignStrategy,
		"-campaign-model-tokens-per-attempt", "100",
	}, io.Discard); err == nil || !strings.Contains(err.Error(), "requires only") {
		t.Fatalf("Stateless zero-model Campaign accepted model budget: %v", err)
	}
}

func TestRetiredAgentStrategiesAreNotReachable(t *testing.T) {
	for _, strategy := range []string{
		"workload-stateless-agent-m5.23g",
		"workload-guarded-agent-one-shot",
		"workload-agent-feedback-batch",
		"workload-agent-follow-up-baseline",
		"workload-agent-b4-pair",
		"workload-agent-b4-preflight",
		"workload-agent-b4-freeze",
	} {
		err := run(context.Background(), []string{
			"-strategy", strategy, "-agent-key-file", "unused",
		}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "Agent flags require an explicit opt-in Agent strategy") {
			t.Fatalf("retired strategy %q remains reachable: %v", strategy, err)
		}
	}
}

func readEtcdraftCampaignSummary(
	t *testing.T,
	path string,
) controlexperiment.CampaignSummary {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var summary controlexperiment.CampaignSummary
	if err := json.Unmarshal(encoded, &summary); err != nil {
		t.Fatal(err)
	}
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
	return summary
}

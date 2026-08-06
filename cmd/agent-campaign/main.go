package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/bindings"
	"github.com/SuzumiyaHaruki/consensus-atlas/families"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/agentcampaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

type config struct {
	repoRoot, profilePath, keyPath string
	model, endpoint, outputPath    string
	trustedCampaignOut             string
	trustedReplayOut               string
	limits                         agentcampaign.Config
	scope                          agentcampaign.BlindScope
}

func main() {
	repoRoot := flag.String("repo", ".", "repository root")
	profilePath := flag.String("profile", "artifacts/profiles/etcdraft-campaign-v1.json", "frozen Campaign Profile v2")
	keyPath := flag.String("key-file", "key.txt", "permission-restricted DeepSeek API key file")
	model := flag.String("model", "deepseek-v4-flash", "DeepSeek model ID")
	endpoint := flag.String("endpoint", "https://api.deepseek.com/chat/completions", "official DeepSeek endpoint")
	campaignID := flag.String("campaign-id", "deepseek-etcdraft-blind-v1", "stable blind agent campaign id")
	benchmarkID := flag.String("blind-benchmark-id", "", "opaque frozen benchmark identifier (required)")
	benchmarkDigest := flag.String("blind-benchmark-digest", "", "SHA-256 digest of the frozen private benchmark manifest (required)")
	trialID := flag.String("blind-trial-id", "", "opaque trial identifier (required)")
	maxAttempts := flag.Int("max-attempts", 6, "maximum Planner proposals")
	maxNoProgress := flag.Int("max-no-progress", 3, "stop after this many consecutive non-progress rounds")
	maxRuns := flag.Int("max-runs", 20, "total runtime run budget")
	maxDecisions := flag.Int("max-decisions", 1024, "total scheduler decision budget")
	maxTokens := flag.Int("max-tokens", 200000, "total reported model token budget")
	timeout := flag.Duration("timeout", 20*time.Minute, "overall generation and execution deadline")
	outputPath := flag.String("out", "artifacts/agent-campaigns/deepseek-etcdraft-blind-v1.json", "blind agent campaign report")
	trustedCampaignOut := flag.String("trusted-campaign-out", "", "private trusted Campaign report; requires -trusted-replay-out")
	trustedReplayOut := flag.String("trusted-replay-out", "", "private trusted replay bundle; requires -trusted-campaign-out")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	err := run(ctx, config{
		repoRoot: *repoRoot, profilePath: *profilePath, keyPath: *keyPath,
		model: *model, endpoint: *endpoint, outputPath: *outputPath,
		trustedCampaignOut: *trustedCampaignOut, trustedReplayOut: *trustedReplayOut,
		limits: agentcampaign.Config{
			ID: *campaignID, MaxAttempts: *maxAttempts, MaxNoProgress: *maxNoProgress,
			MaxTotalRuns: *maxRuns, MaxTotalDecisions: *maxDecisions, MaxTotalTokens: *maxTokens,
		},
		scope: agentcampaign.BlindScope{BenchmarkID: *benchmarkID, BenchmarkDigest: *benchmarkDigest, TrialID: *trialID},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-campaign:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	root, err := filepath.Abs(cfg.repoRoot)
	if err != nil {
		return err
	}
	if err := cfg.scope.Validate(); err != nil {
		return fmt.Errorf("validate blind scope: %w", err)
	}
	if cfg.model == "" || cfg.endpoint == "" {
		return errors.New("model and endpoint are required")
	}
	if (cfg.trustedCampaignOut == "") != (cfg.trustedReplayOut == "") {
		return errors.New("-trusted-campaign-out and -trusted-replay-out must be provided together")
	}
	keyPath, err := filepath.Abs(cfg.keyPath)
	if err != nil {
		return err
	}
	if err := validateSecretFile(keyPath); err != nil {
		return err
	}
	var profile coverage.Profile
	if err := readStrictJSON(resolve(root, cfg.profilePath), &profile); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("validate profile: %w", err)
	}
	_, manifest, err := bindings.New(profile)
	if err != nil {
		return err
	}
	var matcher coverage.SemanticMatcher
	var monitors []oracle.Monitor
	if profile.PSSID != "" {
		matcher, err = families.CoverageMatcher(profile.PSSID)
		if err != nil {
			return err
		}
		monitors, err = families.CampaignMonitors(profile.PSSID, manifest)
		if err != nil {
			return err
		}
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return err
	}
	planner := &agentcampaign.BlindCommandPlanner{
		Path: python, Args: []string{filepath.Join(root, "agents", "deepseek_planner.py")}, Dir: root,
		Env: []string{
			"CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE=" + keyPath,
			"CONSENSUS_ATLAS_DEEPSEEK_MODEL=" + cfg.model,
			"CONSENSUS_ATLAS_DEEPSEEK_ENDPOINT=" + cfg.endpoint,
		},
	}
	newAdapter := func() (adapter.Adapter, error) {
		protocolAdapter, _, createErr := bindings.New(profile)
		return protocolAdapter, createErr
	}
	outputPath := resolve(root, cfg.outputPath)
	report, err := agentcampaign.CoordinateBlind(
		ctx, profile, manifest, matcher, cfg.limits, cfg.scope, planner, newAdapter,
		campaign.Options{Artifact: "trial://" + cfg.scope.TrialID, Monitors: monitors},
	)
	if err != nil {
		return err
	}
	if err := writeJSON(outputPath, report); err != nil {
		return err
	}
	if cfg.trustedCampaignOut != "" {
		bundle, err := report.TrustedReplayBundle()
		if err != nil {
			return err
		}
		if err := writeJSON(resolve(root, cfg.trustedCampaignOut), report.TrustedCampaign); err != nil {
			return err
		}
		if err := writeJSON(resolve(root, cfg.trustedReplayOut), bundle); err != nil {
			return err
		}
	}
	rejected, noProgress, planErrors := 0, 0, 0
	for _, round := range report.Rounds {
		switch round.Finding.Code {
		case agentcampaign.FindingProposalRejected, agentcampaign.FindingDuplicateProposal, agentcampaign.FindingBudgetRejected:
			rejected++
		case agentcampaign.FindingNoProgress:
			noProgress++
		case agentcampaign.FindingPlanError:
			planErrors++
		}
	}
	fmt.Printf("wrote %s\nstatus: %s, rounds: %d, coverage: %d/%d, score: %.2f, debt: %d, runs: %d, decisions: %d, primary-work: %d, replay-work: %d, tokens: %d, rejected: %d, no-progress: %d, plan-errors: %d\n",
		outputPath, report.Status, len(report.Rounds), report.Final.Covered, report.Final.Total,
		report.Final.Score, report.Final.Debt, report.TrustedCampaign.ChargedRuns,
		report.TrustedCampaign.ChargedDecisions, report.TrustedCampaign.Cost.Primary.WorkUnits,
		report.TrustedCampaign.Cost.Replay.WorkUnits, report.TotalTokens, rejected, noProgress, planErrors)
	return nil
}

func validateSecretFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("DeepSeek key path must be a regular file, not a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("DeepSeek key file permissions must not grant group or other access")
	}
	return nil
}

func resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.Clean(path))
}

func readStrictJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

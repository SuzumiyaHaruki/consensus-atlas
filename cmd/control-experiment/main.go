package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "control-experiment:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("control-experiment", flag.ContinueOnError)
	out := flags.String("out", "", "report output path")
	strategy := flags.String("strategy", "fixed", "policy set: fixed, random, stub-planner, or deepseek-planner")
	decisions := flags.Int("decisions", 32, "charged decisions per run")
	policySeed := flags.Uint64("policy-seed", 1, "public random-policy seed")
	repoRoot := flags.String("repo", ".", "repository root for the model client")
	keyFile := flags.String("key-file", "../key.txt", "permission-restricted DeepSeek API key file")
	model := flags.String("model", "deepseek-v4-flash", "DeepSeek model ID")
	endpoint := flags.String("endpoint", "https://api.deepseek.com/chat/completions", "official DeepSeek endpoint")
	modelTimeout := flags.Duration("model-timeout", 5*time.Minute, "single model-call deadline")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required")
	}
	if *strategy == "stub-planner" || *strategy == "deepseek-planner" {
		var attempt controlexperiment.PlannerAttempt
		var err error
		if *strategy == "stub-planner" {
			attempt, err = etcdraftPlannerAttempt(ctx, *decisions, stubPlannerProposal())
		} else {
			if *modelTimeout <= 0 {
				return errors.New("-model-timeout must be positive")
			}
			root, pathErr := filepath.Abs(*repoRoot)
			if pathErr != nil {
				return pathErr
			}
			keyPath := *keyFile
			if !filepath.IsAbs(keyPath) {
				keyPath = filepath.Join(root, keyPath)
			}
			scope := etcdraftPlannerScope("public-etcdraft-v2-deepseek-planner-m5.13", *decisions)
			modelCtx, cancel := context.WithTimeout(ctx, *modelTimeout)
			defer cancel()
			proposal, audit, generationErr := generateDeepSeekPlannerProposal(modelCtx, deepSeekPlannerConfig{
				RepoRoot: root, KeyPath: filepath.Clean(keyPath), Model: *model, Endpoint: *endpoint,
			}, scope)
			if generationErr != nil {
				return generationErr
			}
			attempt, err = etcdraftAuditedPlannerAttempt(modelCtx, scope, proposal, audit)
		}
		if err != nil {
			return err
		}
		return persistPlannerAttempt(*out, attempt, stdout)
	}
	report, err := etcdraftReport(ctx, *strategy, *decisions, *policySeed)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	var persisted controlexperiment.Report
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		return err
	}
	if err := persisted.Validate(); err != nil {
		return fmt.Errorf("validate persisted report: %w", err)
	}
	if err := writeReport(*out, encoded); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\nruns=%d decisions=%d states=%d replay=%t digest=%s\n",
		*out, len(report.Runs), report.StateDiscovery.TotalDecisions,
		report.StateDiscovery.UniqueStates, allReplayStable(report), report.Digest)
	return nil
}

func persistPlannerAttempt(path string, attempt controlexperiment.PlannerAttempt, stdout io.Writer) error {
	encoded, err := json.MarshalIndent(attempt, "", "  ")
	if err != nil {
		return err
	}
	var persisted controlexperiment.PlannerAttempt
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		return err
	}
	if err := persisted.Validate(); err != nil {
		return fmt.Errorf("validate persisted planner attempt: %w", err)
	}
	if err := writeReport(path, encoded); err != nil {
		return err
	}
	if attempt.Experiment == nil {
		reason := "none"
		if attempt.Failure != nil {
			reason = attempt.Failure.ReasonCode
		}
		fmt.Fprintf(stdout, "wrote %s\nstatus=%s proposals=%d reason=%s digest=%s\n",
			path, attempt.Status, attempt.Work.ProposalAttempts, reason, attempt.Digest)
		return nil
	}
	report := attempt.Experiment
	fmt.Fprintf(stdout, "wrote %s\nstatus=%s proposals=%d runs=%d decisions=%d states=%d digest=%s\n",
		path, attempt.Status, attempt.Work.ProposalAttempts, len(report.Runs),
		report.StateDiscovery.TotalDecisions, report.StateDiscovery.UniqueStates, attempt.Digest)
	return nil
}

func writeReport(path string, encoded []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func etcdraftReport(
	ctx context.Context,
	strategy string,
	decisions int,
	policySeed uint64,
) (controlexperiment.Report, error) {
	progress := []control.ActionKind{
		control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionFireTemporal,
	}
	var experimentID string
	var runs []controlexperiment.RunPlan
	switch strategy {
	case "fixed":
		experimentID = "public-etcdraft-v2-fixed-baselines-m5.10"
		runs = []controlexperiment.RunPlan{
			{Run: 1, Policy: controlexperiment.Policy{
				Version: controlexperiment.PolicyVersion, ID: "progress-v1", Priority: progress,
			}},
			{Run: 2, Policy: controlexperiment.Policy{
				Version: controlexperiment.PolicyVersion, ID: "lifecycle-v1",
				Rules: []controlexperiment.DecisionRule{
					{Decision: 1, Kind: control.ActionCrash, Node: "n3"},
					{Decision: 2, Kind: control.ActionRestart, Node: "n3"},
				},
				Priority: progress,
			}},
		}
	case "random":
		experimentID = fmt.Sprintf("public-etcdraft-v2-random-baseline-m5.11-seed-%d", policySeed)
		for run := 1; run <= 2; run++ {
			runs = append(runs, controlexperiment.RunPlan{Run: run, Policy: controlexperiment.Policy{
				Version: controlexperiment.RandomPolicyVersion,
				ID:      fmt.Sprintf("uniform-random-v1/run-%d", run),
				SeedHex: randomPolicySeed(policySeed, run),
			}})
		}
	default:
		return controlexperiment.Report{}, fmt.Errorf("unsupported -strategy %q", strategy)
	}
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersion,
		ID:            experimentID,
		PSSID:         etcdraftv2.CorePSSMappingID,
		Runtime: controlexperiment.RuntimeConfig{
			SeedHex: hex.EncodeToString([]byte("official-etcdraft-v2-cluster-seed")), MaxClones: 1,
		},
		DecisionsPerRun: decisions, RequireReplay: true, Runs: runs,
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	return controlexperiment.Execute(ctx, config, factory, etcdraftv2.CorePSSMapper{})
}

func randomPolicySeed(base uint64, run int) string {
	var input [16]byte
	binary.BigEndian.PutUint64(input[:8], base)
	binary.BigEndian.PutUint64(input[8:], uint64(run))
	sum := sha256.Sum256(append([]byte("consensus-atlas/policy-seed/v1\x00"), input[:]...))
	return hex.EncodeToString(sum[:])
}

func stubPlannerProposal() controlexperiment.PlannerProposal {
	progress := []control.ActionKind{
		control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionFireTemporal,
	}
	return controlexperiment.PlannerProposal{
		SchemaVersion: controlexperiment.PlannerProposalVersion,
		Policies: []controlexperiment.ProposedPolicy{
			{Run: 1, Priority: progress},
			{Run: 2, Rules: []controlexperiment.DecisionRule{
				{Decision: 1, Kind: control.ActionCrash, Node: "n3"},
				{Decision: 2, Kind: control.ActionRestart, Node: "n3"},
			}, Priority: progress},
		},
	}
}

func etcdraftPlannerAttempt(
	ctx context.Context,
	decisions int,
	proposal controlexperiment.PlannerProposal,
) (controlexperiment.PlannerAttempt, error) {
	scope := etcdraftPlannerScope("public-etcdraft-v2-stub-planner-m5.12", decisions)
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	return controlexperiment.RunPlannerAttempt(
		ctx, "deterministic-stub-planner/v1", scope, proposal, factory, etcdraftv2.CorePSSMapper{},
	)
}

func etcdraftPlannerScope(experimentID string, decisions int) controlexperiment.PlannerScope {
	return controlexperiment.PlannerScope{
		ExperimentID: experimentID,
		PSSID:        etcdraftv2.CorePSSMappingID,
		Runtime: controlexperiment.RuntimeConfig{
			SeedHex: hex.EncodeToString([]byte("official-etcdraft-v2-cluster-seed")), MaxClones: 1,
		},
		DecisionsPerRun: decisions, RequireReplay: true, Runs: []int{1, 2},
	}
}

func etcdraftAuditedPlannerAttempt(
	ctx context.Context,
	scope controlexperiment.PlannerScope,
	proposal controlexperiment.PlannerProposal,
	audit controlexperiment.PlannerModelAudit,
) (controlexperiment.PlannerAttempt, error) {
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	return controlexperiment.RunAuditedPlannerAttempt(
		ctx, "deepseek-restricted-planner/v1", scope, proposal, audit, factory, etcdraftv2.CorePSSMapper{},
	)
}

func allReplayStable(report controlexperiment.Report) bool {
	for _, run := range report.Runs {
		if !run.Replay.Stable {
			return false
		}
	}
	return true
}

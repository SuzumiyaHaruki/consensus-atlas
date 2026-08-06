package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/bindings"
	"github.com/SuzumiyaHaruki/consensus-atlas/families"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/explore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
)

type measurementWindow struct {
	ID                string `json:"id"`
	SetupScenario     string `json:"setup_scenario"`
	SetupTraceRecords int    `json:"setup_trace_records"`
	StartAfterStep    int    `json:"start_after_step"`
	SetupFingerprint  string `json:"setup_fingerprint"`
	BudgetUnit        string `json:"budget_unit"`
	InitialState      string `json:"initial_state"`
}

type reportedRun struct {
	explore.RunResult
	Oracle oracle.Result `json:"oracle"`
}

type report struct {
	Version           int                             `json:"version"`
	ProfileID         string                          `json:"profile_id"`
	PSSID             string                          `json:"pss_id"`
	Protocol          string                          `json:"protocol"`
	Strategy          explore.Strategy                `json:"strategy"`
	Config            explore.Config                  `json:"config"`
	MeasurementWindow measurementWindow               `json:"measurement_window"`
	ReplayStable      bool                            `json:"replay_stable"`
	ReplayErrors      []string                        `json:"replay_errors,omitempty"`
	TargetDecisions   int                             `json:"target_decisions"`
	ChargedDecisions  int                             `json:"charged_decisions"`
	BudgetReached     bool                            `json:"budget_reached"`
	StopReason        string                          `json:"stop_reason"`
	Capabilities      driver.Manifest                 `json:"capabilities"`
	SetupTrace        []core.TraceRecord              `json:"setup_trace"`
	Discovery         protocolstate.ExperimentSummary `json:"protocol_state_discovery"`
	Runs              []reportedRun                   `json:"runs"`
}

func main() {
	profilePath := flag.String("profile", "artifacts/onboarding/etcdraft-profile-v1.json", "validated coverage profile JSON")
	setupPath := flag.String("setup", "scenarios/etcdraft-explore-setup.json", "deterministic setup scenario JSON")
	strategyName := flag.String("strategy", string(explore.StrategyRandom), "explorer: random or dfs")
	runs := flag.Int("runs", 8, "maximum measured runs")
	budget := flag.Int("budget", 64, "scheduler decisions per run")
	decisionBudget := flag.Int("decision-budget", 128, "total charged scheduler decisions")
	seed := flag.Int64("seed", 1, "base random seed")
	dropMessages := flag.Bool("drop-messages", true, "include message drop decisions")
	duplicateMessages := flag.Bool("duplicate-messages", false, "include message duplicate decisions")
	maxDuplicates := flag.Int("max-duplicates", 0, "maximum duplicate decisions per run")
	verifyReplay := flag.Bool("verify-replay", true, "force each recorded decision log from a fresh setup root")
	outPath := flag.String("out", "", "write the full report to this path; stdout when empty")
	flag.Parse()

	config := explore.Config{
		Runs: *runs, BudgetPerRun: *budget, DecisionBudget: *decisionBudget, Seed: *seed,
		Actions: explore.ActionPolicy{
			DropMessages: *dropMessages, DuplicateMessages: *duplicateMessages, MaxDuplicates: *maxDuplicates,
		},
	}
	if err := run(context.Background(), *profilePath, *setupPath, explore.Strategy(*strategyName), config,
		*verifyReplay, *outPath); err != nil {
		fmt.Fprintln(os.Stderr, "experiment:", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	profilePath, setupPath string,
	strategy explore.Strategy,
	config explore.Config,
	verifyReplay bool,
	outPath string,
) error {
	var profile coverage.Profile
	if err := readJSON(profilePath, &profile); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("validate profile: %w", err)
	}
	if profile.PSSID == "" {
		return fmt.Errorf("profile %s has no pss_id for state-discovery experiments", profile.ID)
	}
	var setup scenario.Spec
	if err := readJSON(setupPath, &setup); err != nil {
		return err
	}
	if err := setup.Validate(); err != nil {
		return fmt.Errorf("validate setup: %w", err)
	}
	projector, err := families.Projector(profile.PSSID)
	if err != nil {
		return err
	}
	searcher, err := explore.New(strategy)
	if err != nil {
		return err
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate experiment config: %w", err)
	}

	_, capabilities, err := bindings.New(profile)
	if err != nil {
		return err
	}
	factory := func(factoryCtx context.Context) (*engine.Engine, error) {
		protocolAdapter, _, err := bindings.New(profile)
		if err != nil {
			return nil, err
		}
		e, err := engine.New(protocolAdapter)
		if err != nil {
			return nil, err
		}
		if err := scenario.Run(factoryCtx, e, setup); err != nil {
			return nil, err
		}
		if err := e.CheckConformance(); err != nil {
			return nil, fmt.Errorf("setup conformance: %w", err)
		}
		return e, nil
	}

	first, err := searcher.Explore(ctx, factory, config)
	if err != nil {
		return err
	}
	if len(first.Runs) == 0 {
		return fmt.Errorf("explorer %s produced no runs", strategy)
	}
	replayStable := true
	var replayErrors []string
	if verifyReplay {
		for _, expected := range first.Runs {
			if _, err := explore.ReplayRun(ctx, factory, config.Actions, expected); err != nil {
				replayStable = false
				replayErrors = append(replayErrors, err.Error())
			}
		}
	}
	setupFingerprint := first.Runs[0].SetupFingerprint
	for _, current := range first.Runs[1:] {
		if current.SetupFingerprint != setupFingerprint {
			return fmt.Errorf("run %d has a different setup fingerprint", current.Run)
		}
	}
	measured := make([]protocolstate.MeasuredRun, 0, len(first.Runs))
	reported := make([]reportedRun, 0, len(first.Runs))
	for _, current := range first.Runs {
		measured = append(measured, protocolstate.MeasuredRun{
			Run: current.Run, DecisionCount: len(current.Decisions),
			InitialSnapshot: current.InitialSnapshot, Trace: current.Trace,
		})
		reported = append(reported, reportedRun{
			RunResult: current,
			Oracle:    oracle.Check(current.FullTrace, oracle.TraceIntegrity{}, oracle.Agreement{}),
		})
	}
	discovery, err := protocolstate.Aggregate(measured, projector)
	if err != nil {
		return err
	}
	setupTrace := append([]core.TraceRecord(nil), first.Runs[0].SetupTrace...)
	window := measurementWindow{
		ID:            windowID(profile.ID, profile.PSSID, setupFingerprint),
		SetupScenario: setup.Name, SetupTraceRecords: len(setupTrace),
		StartAfterStep: lastStep(setupTrace), SetupFingerprint: setupFingerprint,
		BudgetUnit: "scheduler-decision", InitialState: "included-at-budget-zero",
	}
	result := report{
		Version: 1, ProfileID: profile.ID, PSSID: profile.PSSID, Protocol: profile.Protocol,
		Strategy: strategy, Config: config, MeasurementWindow: window, ReplayStable: replayStable,
		ReplayErrors:    replayErrors,
		TargetDecisions: first.TargetDecisionBudget, ChargedDecisions: first.ChargedDecisions,
		BudgetReached: first.BudgetReached, StopReason: first.StopReason,
		Capabilities: capabilities, SetupTrace: setupTrace, Discovery: discovery, Runs: reported,
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if outPath == "" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outPath, encoded, 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	violations := 0
	executionErrors := 0
	for _, current := range reported {
		violations += len(current.Oracle.Violations)
		if current.ExecutionError != "" {
			executionErrors++
		}
	}
	fmt.Printf("wrote %s\nstrategy: %s, runs: %d, decisions: %d, states: %d, replay stable: %v, execution errors: %d, oracle violations: %d\n",
		outPath, strategy, len(first.Runs), discovery.TotalDecisions, discovery.UniqueStates,
		replayStable, executionErrors, violations)
	return nil
}

func windowID(profileID, pssID, setupFingerprint string) string {
	sum := sha256.Sum256([]byte(profileID + "\x00" + pssID + "\x00" + setupFingerprint))
	return hex.EncodeToString(sum[:])
}

func lastStep(trace []core.TraceRecord) int {
	if len(trace) == 0 {
		return 0
	}
	return trace[len(trace)-1].Step
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

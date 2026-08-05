package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/toy"
)

type runOutput struct {
	ProfileID         string             `json:"profile_id"`
	Scenario          string             `json:"scenario"`
	Protocol          string             `json:"protocol"`
	ReplayStable      bool               `json:"replay_stable"`
	ReplayFingerprint string             `json:"replay_fingerprint"`
	CanonicalKey      string             `json:"canonical_key"`
	CanonicalTrace    any                `json:"canonical_trace"`
	Oracle            oracle.Result      `json:"oracle"`
	Coverage          coverage.Summary   `json:"coverage"`
	Trace             []core.TraceRecord `json:"trace"`
	PendingAtEnd      []core.Event       `json:"pending_at_end,omitempty"`
}

func main() {
	profilePath := flag.String("profile", "profiles/toy-v1.json", "coverage profile JSON")
	scenarioPath := flag.String("scenario", "scenarios/toy-election.json", "scenario JSON")
	outPath := flag.String("out", "", "write the full result to this path; stdout when empty")
	flag.Parse()

	if err := run(*profilePath, *scenarioPath, *outPath); err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		os.Exit(1)
	}
}

func run(profilePath, scenarioPath, outPath string) error {
	var profile coverage.Profile
	if err := readJSON(profilePath, &profile); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("validate profile: %w", err)
	}
	var spec scenario.Spec
	if err := readJSON(scenarioPath, &spec); err != nil {
		return err
	}
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("validate scenario: %w", err)
	}

	first, firstConform, err := execute(profile, spec)
	if err != nil {
		return err
	}
	second, secondConform, err := execute(profile, spec)
	if err != nil {
		return fmt.Errorf("deterministic replay: %w", err)
	}
	firstHash, err := semantic.ExecutionFingerprint(first.trace)
	if err != nil {
		return err
	}
	secondHash, err := semantic.ExecutionFingerprint(second.trace)
	if err != nil {
		return err
	}
	replayStable := firstHash == secondHash
	canonicalKey, err := semantic.CanonicalFingerprint(first.trace)
	if err != nil {
		return err
	}

	oracleResult := oracle.Check(first.trace, oracle.TraceIntegrity{}, oracle.Agreement{})
	summary := coverage.Evaluate(profile, first.trace, oracleResult, replayStable, firstConform && secondConform)
	result := runOutput{
		ProfileID:         profile.ID,
		Scenario:          spec.Name,
		Protocol:          profile.Protocol,
		ReplayStable:      replayStable,
		ReplayFingerprint: firstHash,
		CanonicalKey:      canonicalKey,
		CanonicalTrace:    semantic.StructuralCanonicalize(first.trace),
		Oracle:            oracleResult,
		Coverage:          summary,
		Trace:             first.trace,
		PendingAtEnd:      first.pending,
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
	fmt.Printf("wrote %s\nscore: %.2f, high: %v, replay stable: %v, violations: %d\n",
		outPath, summary.Score, summary.High, replayStable, len(oracleResult.Violations))
	return nil
}

type execution struct {
	trace   []core.TraceRecord
	pending []core.Event
}

func execute(profile coverage.Profile, spec scenario.Spec) (execution, bool, error) {
	protocolAdapter, err := newAdapter(profile)
	if err != nil {
		return execution{}, false, err
	}
	conform := protocolAdapter.CheckConformance() == nil
	e := engine.New(protocolAdapter)
	if err := scenario.Run(context.Background(), e, spec); err != nil {
		return execution{}, conform, err
	}
	if err := protocolAdapter.CheckConformance(); err != nil {
		conform = false
	}
	return execution{trace: e.Trace(), pending: e.Pending()}, conform, nil
}

func newAdapter(profile coverage.Profile) (adapter.Adapter, error) {
	switch profile.Protocol {
	case toy.Protocol:
		return toy.New(profile.Nodes), nil
	default:
		return nil, fmt.Errorf("no adapter registered for protocol %q", profile.Protocol)
	}
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

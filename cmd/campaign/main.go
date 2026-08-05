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
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/bindings"
	"github.com/SuzumiyaHaruki/consensus-atlas/families"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

func main() {
	profilePath := flag.String("profile", "artifacts/profiles/etcdraft-campaign-v1.json", "frozen Campaign Profile v2")
	suitePath := flag.String("plans", "plans/etcdraft-expert-v1.json", "bounded Test Plan Suite v1")
	outPath := flag.String("out", "artifacts/campaigns/etcdraft-expert-v1.json", "campaign report output")
	flag.Parse()
	if err := run(context.Background(), *profilePath, *suitePath, *outPath); err != nil {
		fmt.Fprintln(os.Stderr, "campaign:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, profilePath, suitePath, outPath string) error {
	var profile coverage.Profile
	if err := readStrictJSON(profilePath, &profile); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("validate profile: %w", err)
	}
	var suite testplan.Suite
	if err := readStrictJSON(suitePath, &suite); err != nil {
		return err
	}
	_, manifest, err := bindings.New(profile)
	if err != nil {
		return err
	}
	var matcher coverage.SemanticMatcher
	if profile.PSSID != "" {
		matcher, err = families.CoverageMatcher(profile.PSSID)
		if err != nil {
			return err
		}
	}
	newAdapter := func() (adapter.Adapter, error) {
		protocolAdapter, _, createErr := bindings.New(profile)
		return protocolAdapter, createErr
	}
	report, err := campaign.Run(ctx, profile, manifest, matcher, suite, newAdapter, campaign.Options{Artifact: outPath})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(outPath); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	if err := os.WriteFile(outPath, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	violations := 0
	unstable := 0
	planErrors := 0
	for _, plan := range report.Plans {
		if plan.ExecutionError != "" {
			planErrors++
		}
		for _, current := range plan.Runs {
			violations += len(current.Oracle.Violations)
			if !current.ReplayStable {
				unstable++
			}
		}
	}
	fmt.Printf("wrote %s\ncoverage: %d/%d, score: %.2f, debt: %d, runs: %d, decisions: %d, plan errors: %d, unstable: %d, violations: %d\n",
		outPath, report.Final.Covered, report.Final.Total, report.Final.Score, report.Final.Debt,
		report.ChargedRuns, report.ChargedDecisions, planErrors, unstable, violations)
	return nil
}

func readStrictJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode %s: multiple JSON values", path)
		}
		return fmt.Errorf("decode %s trailing data: %w", path, err)
	}
	return nil
}

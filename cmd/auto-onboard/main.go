package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/bindings"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

func main() {
	repoRoot := flag.String("repo", ".", "repository root used to resolve witness scenarios")
	contractPath := flag.String("contract", "contracts/etcdraft-v1.json", "protocol knowledge contract")
	bindingPath := flag.String("binding", "onboarding/etcdraft-binding-v1.json", "agent-produced binding proposal")
	reportPath := flag.String("report-out", "artifacts/onboarding/etcdraft-v1.json", "validation report output")
	profilePath := flag.String("profile-out", "artifacts/onboarding/etcdraft-profile-v1.json", "validated runtime profile output")
	flag.Parse()
	if err := run(context.Background(), *repoRoot, *contractPath, *bindingPath, *reportPath, *profilePath); err != nil {
		fmt.Fprintln(os.Stderr, "auto-onboard:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, repoRoot, contractPath, bindingPath, reportPath, profilePath string) error {
	contract, err := protocolcontract.Load(contractPath)
	if err != nil {
		return err
	}
	binding, err := autoonboard.Load(bindingPath)
	if err != nil {
		return err
	}
	validator := func(ctx context.Context, proposal *autoonboard.Binding) (autoonboard.Report, error) {
		return autoonboard.Validate(ctx, repoRoot, contract, proposal, bindings.New)
	}
	loop, err := autoonboard.Coordinate(ctx, contract, autoonboard.GenerationContext{}, 1, autoonboard.StaticGenerator{Binding: binding}, validator)
	if err != nil {
		return err
	}
	if err := writeJSON(reportPath, loop); err != nil {
		return err
	}
	if loop.Status != "validated" {
		return errors.New("binding was not mechanically validated; inspect the onboarding report")
	}
	if err := writeJSON(profilePath, loop.Final.Profile); err != nil {
		return err
	}
	fmt.Printf("validated %s: capabilities %.2f, obligations %.2f, fully supported: %v\n", binding.ID, loop.Final.CapabilitySupport, loop.Final.ObligationSupport, loop.Final.FullySupported)
	return nil
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

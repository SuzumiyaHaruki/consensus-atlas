package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/bindings"
	"github.com/SuzumiyaHaruki/consensus-atlas/families"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
)

func main() {
	basePath := flag.String("base-profile", "artifacts/onboarding/etcdraft-profile-v1.json", "validated integration Profile v2")
	specPath := flag.String("spec", "profiles/raft/three-node-cft-v1.json", "bounded Raft campaign spec")
	outPath := flag.String("out", "artifacts/profiles/etcdraft-campaign-v1.json", "compiled campaign Profile v2")
	flag.Parse()
	if err := run(*basePath, *specPath, *outPath); err != nil {
		fmt.Fprintln(os.Stderr, "coverage-compile:", err)
		os.Exit(1)
	}
}

func run(basePath, specPath, outPath string) error {
	var base coverage.Profile
	if err := readStrictJSON(basePath, &base); err != nil {
		return err
	}
	if err := base.Validate(); err != nil {
		return fmt.Errorf("validate base profile: %w", err)
	}
	specJSON, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", specPath, err)
	}
	_, manifest, err := bindings.New(base)
	if err != nil {
		return fmt.Errorf("resolve driver manifest: %w", err)
	}
	profile, err := families.CompileCampaign(base, manifest, specJSON)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(profile, "", "  ")
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
	digest, err := coverage.Digest(profile)
	if err != nil {
		return err
	}
	items, _ := profile.Obligations()
	unsupported := 0
	for _, item := range items {
		if item.Status == coverage.StatusUnsupported {
			unsupported++
		}
	}
	fmt.Printf("wrote %s\nprofile: %s\ndigest: %s\nobligations: %d (%d unsupported)\n", outPath, profile.ID, digest, len(items), unsupported)
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

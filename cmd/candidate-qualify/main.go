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
	catalogetcdraft "github.com/SuzumiyaHaruki/consensus-atlas/catalogs/etcdraft"
	"github.com/SuzumiyaHaruki/consensus-atlas/families"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "candidate-qualify:", err)
		os.Exit(1)
	}
}

func run() error {
	catalogPath := flag.String("catalog", "", "typed Candidate Catalog v1")
	profilePath := flag.String("profile", "", "current validated Coverage Profile")
	raftSpecPath := flag.String("raft-spec", "", "current bounded Raft campaign spec")
	snapshotOut := flag.String("snapshot-out", "", "trusted CapabilitySnapshot output")
	reportOut := flag.String("out", "", "mechanical QualificationReport output")
	flag.Parse()
	if *catalogPath == "" || *profilePath == "" || *raftSpecPath == "" || *snapshotOut == "" || *reportOut == "" {
		return errors.New("-catalog, -profile, -raft-spec, -snapshot-out, and -out are required")
	}
	var candidateCatalog defectbench.CandidateCatalog
	if err := readStrictJSON(*catalogPath, &candidateCatalog); err != nil {
		return err
	}
	var profile coverage.Profile
	if err := readStrictJSON(*profilePath, &profile); err != nil {
		return err
	}
	var raftSpec catalogetcdraft.CampaignSpec
	if err := readSelectedJSON(*raftSpecPath, &raftSpec); err != nil {
		return err
	}
	_, manifest, err := bindings.New(profile)
	if err != nil {
		return err
	}
	registered, err := families.RegisteredMonitors(profile.PSSID)
	if err != nil {
		return err
	}
	monitorNames := make([]string, 0, len(registered))
	for _, monitor := range registered {
		monitorNames = append(monitorNames, monitor.Name())
	}
	snapshot, err := catalogetcdraft.BuildCapabilitySnapshot(profile, manifest, monitorNames, raftSpec)
	if err != nil {
		return err
	}
	report, err := defectbench.QualifyCandidates(candidateCatalog, snapshot)
	if err != nil {
		return err
	}
	if err := writeJSON(*snapshotOut, snapshot); err != nil {
		return err
	}
	if err := writeJSON(*reportOut, report); err != nil {
		return err
	}
	qualified := 0
	for _, result := range report.Results {
		if result.Status == defectbench.QualificationQualified {
			qualified++
		}
	}
	fmt.Printf("wrote %s and %s\ncandidates: %d, qualified: %d, deferred: %d\n",
		*snapshotOut, *reportOut, len(report.Results), qualified, len(report.Results)-qualified)
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
		return fmt.Errorf("decode %s: trailing JSON data", path)
	}
	return nil
}

// CampaignSpec intentionally selects only identity and bounds from the larger
// concrete Raft profile source.
func readSelectedJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// benchmark-preflight checks whether a private benchmark has the mechanical
// prerequisites for a formal method comparison. It never emits a blind view.
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

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "benchmark-preflight:", err)
		os.Exit(1)
	}
}

func run() error {
	manifestPath := flag.String("manifest", "", "private defect benchmark Manifest v2")
	outPath := flag.String("out", "", "private readiness report output")
	minRoots := flag.Int("min-distinct-root-causes", 3, "minimum distinct defect root-cause labels")
	minControls := flag.Int("min-controls", 3, "minimum correct controls")
	requireHistorical := flag.Bool("require-historical", true, "require historical provenance for every variant")
	requireArtifacts := flag.Bool("require-artifact-bindings", true, "require build audit and binary digests for every variant")
	flag.Parse()
	if *manifestPath == "" || *outPath == "" {
		return errors.New("-manifest and -out are required")
	}
	var manifest defectbench.Manifest
	if err := readStrictJSON(*manifestPath, &manifest); err != nil {
		return err
	}
	report, err := defectbench.PreflightReadiness(manifest, defectbench.ReadinessPolicy{
		MinDistinctRootCauses: *minRoots, MinControls: *minControls,
		RequireHistorical: *requireHistorical, RequireArtifactBinds: *requireArtifacts,
	})
	if err != nil {
		return err
	}
	if err := writeJSON(*outPath, report); err != nil {
		return err
	}
	if !report.Passed {
		return errors.New("benchmark is not ready for a formal comparison; see private readiness report")
	}
	fmt.Printf("wrote %s: ready (%d roots, %d controls, %d artifact-bound trials)\n", *outPath, report.DistinctRootCauses, report.Controls, report.ArtifactBoundTrials)
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

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

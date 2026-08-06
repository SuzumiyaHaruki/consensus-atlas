// blind-audit is a curator-side preflight check. It consumes a private
// benchmark Manifest locally, but writes only a redacted ExposureAudit report.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

type paths []string

func (values *paths) String() string { return fmt.Sprint([]string(*values)) }
func (values *paths) Set(value string) error {
	if value == "" {
		return errors.New("public artifact path is required")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "blind-audit:", err)
		os.Exit(1)
	}
}

func run() error {
	manifestPath := flag.String("manifest", "", "private defect benchmark manifest")
	blindPath := flag.String("blind", "", "Agent-facing blind manifest")
	outPath := flag.String("out", "", "redacted exposure audit report")
	var public paths
	flag.Var(&public, "public", "additional public JSON artifact to audit; repeatable")
	flag.Parse()
	if *manifestPath == "" || *blindPath == "" || *outPath == "" {
		return errors.New("-manifest, -blind, and -out are required")
	}
	var manifest defectbench.Manifest
	if err := readStrictJSON(*manifestPath, &manifest); err != nil {
		return err
	}
	var blind defectbench.BlindManifest
	blindBytes, err := readStrictJSONBytes(*blindPath, &blind)
	if err != nil {
		return err
	}
	artifacts := []defectbench.PublicArtifact{{Bytes: blindBytes}}
	for _, path := range public {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read public artifact: %w", err)
		}
		artifacts = append(artifacts, defectbench.PublicArtifact{Bytes: data})
	}
	report, err := defectbench.AuditExposure(manifest, blind, artifacts)
	if err != nil {
		return err
	}
	if err := writeJSON(*outPath, report); err != nil {
		return err
	}
	if !report.Passed {
		return errors.New("exposure audit failed; see redacted report")
	}
	fmt.Printf("wrote %s: passed (%d public artifacts)\n", *outPath, len(report.PublicArtifacts))
	return nil
}

func readStrictJSON(path string, target any) error {
	_, err := readStrictJSONBytes(path, target)
	return err
}

func readStrictJSONBytes(path string, target any) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode %s: multiple JSON values", path)
		}
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return data, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

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

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sut-build:", err)
		os.Exit(1)
	}
}

func run() error {
	repoPath := flag.String("repo", ".", "ConsensusAtlas repository root")
	specPath := flag.String("spec", "", "controlled SUT build spec v1")
	auditPath := flag.String("audit-out", "", "build audit output")
	flag.Parse()
	if *specPath == "" || *auditPath == "" {
		return errors.New("-spec and -audit-out are required")
	}
	var spec sutbuild.Spec
	if err := readStrictJSON(*specPath, &spec); err != nil {
		return err
	}
	audit, err := sutbuild.Build(*repoPath, spec)
	if err != nil {
		return err
	}
	if err := writeJSON(*auditPath, audit); err != nil {
		return err
	}
	fmt.Printf("wrote %s\ntrial: %s, module: %s@%s, source: %s -> %s, binary: %s\n",
		*auditPath, audit.TrialID, audit.Module.Path, audit.Module.Version,
		audit.InputSourceDigest, audit.OutputSourceDigest, audit.BinaryDigest)
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

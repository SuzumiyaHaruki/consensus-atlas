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

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "defect-eval:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("defect-eval", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "", "bundle benchmark manifest")
	controlPath := flags.String("control-bundle", "", "correct control ExecutionBundle")
	candidatePath := flags.String("candidate-bundle", "", "public calibration candidate ExecutionBundle")
	outPath := flags.String("out", "", "trusted bundle evaluation output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *manifestPath == "" || *controlPath == "" || *candidatePath == "" || *outPath == "" {
		return errors.New("-manifest, -control-bundle, -candidate-bundle, and -out are required")
	}
	var manifest defectbench.BundleBenchmark
	if err := readStrictJSON(*manifestPath, &manifest); err != nil {
		return err
	}
	controlTrial, candidateTrial, err := pairTrials(manifest)
	if err != nil {
		return err
	}
	var controlBundle, candidateBundle controlexperiment.ExecutionBundle
	if err := readStrictJSON(*controlPath, &controlBundle); err != nil {
		return err
	}
	if err := readStrictJSON(*candidatePath, &candidateBundle); err != nil {
		return err
	}
	report, err := defectbench.EvaluateBundles(
		manifest,
		map[string]controlexperiment.ExecutionBundle{
			controlTrial: controlBundle, candidateTrial: candidateBundle,
		},
		etcdraftv2.DecisionProjector{},
	)
	if err != nil {
		return err
	}
	if err := writeJSON(*outPath, report); err != nil {
		return err
	}
	fmt.Printf("wrote %s\ncontrols=%d false-positive=%d candidates=%d killed=%d invalid=%d digest=%s\n",
		*outPath, report.Summary.Controls, report.Summary.FalsePositives,
		report.Summary.Candidates, report.Summary.KilledCandidates,
		report.Summary.InvalidTrials, report.Digest)
	return nil
}

func pairTrials(manifest defectbench.BundleBenchmark) (string, string, error) {
	if err := manifest.Validate(); err != nil {
		return "", "", err
	}
	var control, candidate string
	for _, variant := range manifest.Variants {
		switch variant.Kind {
		case defectbench.BundleKindControl:
			if control != "" {
				return "", "", errors.New("bundle evaluator requires exactly one control")
			}
			control = variant.TrialID
		case defectbench.BundleKindCalibration:
			if candidate != "" {
				return "", "", errors.New("bundle evaluator requires exactly one calibration candidate")
			}
			candidate = variant.TrialID
		}
	}
	if control == "" || candidate == "" {
		return "", "", errors.New("bundle evaluator requires a control/candidate pair")
	}
	return control, candidate, nil
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

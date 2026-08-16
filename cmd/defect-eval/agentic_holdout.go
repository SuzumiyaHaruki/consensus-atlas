package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const agenticHoldoutInputsSchemaVersion = "consensus-atlas/agentic-holdout-inputs/v1"

type agenticHoldoutTrialInput struct {
	TrialID    string `json:"trial_id"`
	EpisodeDir string `json:"episode_dir"`
}

// agenticHoldoutInputs is private evaluator I/O. Directory paths never enter
// the evaluation result or any Agent-facing material.
type agenticHoldoutInputs struct {
	SchemaVersion string                     `json:"schema_version"`
	Trials        []agenticHoldoutTrialInput `json:"trials"`
}

// agenticEpisodeSummaryProjection deliberately reads only method-reported
// navigation/accounting fields. Unknown summary fields remain outside the
// trusted verdict path; the full Bundle is validated independently.
type agenticEpisodeSummaryProjection struct {
	TargetID string `json:"target_id"`
	Status   string `json:"status"`
	Work     struct {
		Model controlexperiment.ModelWork `json:"model"`
	} `json:"work"`
	Assessment struct {
		Status string `json:"status"`
	} `json:"evidence_assessment"`
}

func runAgenticHoldoutEvaluation(
	contractPath string,
	exposurePath string,
	inputsPath string,
	outPath string,
) error {
	var contract defectbench.FormalBenchmarkContract
	if err := readStrictJSON(contractPath, &contract); err != nil {
		return err
	}
	var exposure defectbench.FormalExposureAudit
	if err := readStrictJSON(exposurePath, &exposure); err != nil {
		return err
	}
	var inputs agenticHoldoutInputs
	if err := readStrictJSON(inputsPath, &inputs); err != nil {
		return err
	}
	evidence, err := loadAgenticHoldoutEvidence(inputsPath, inputs, contract)
	if err != nil {
		return err
	}
	projector, monitors, err := agenticHoldoutComposition(contract.Composition.ProjectorID)
	if err != nil {
		return err
	}
	report, err := defectbench.EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, projector, monitors...,
	)
	if err != nil {
		return err
	}
	if err := writeJSONExclusive(outPath, report); err != nil {
		return err
	}
	fmt.Printf(
		"wrote %s\ntarget=%s pairs=%d controls=%d false-positive=%d candidates=%d killed=%d invalid=%d\n",
		outPath, report.TargetID, len(report.Pairs), report.Summary.Controls,
		report.Summary.FalsePositives, report.Summary.Candidates,
		report.Summary.KilledCandidates, report.Summary.InvalidTrials,
	)
	return nil
}

func loadAgenticHoldoutEvidence(
	inputsPath string,
	inputs agenticHoldoutInputs,
	contract defectbench.FormalBenchmarkContract,
) (map[string]defectbench.AgenticTrialEvidence, error) {
	if inputs.SchemaVersion != agenticHoldoutInputsSchemaVersion ||
		len(inputs.Trials) != len(contract.Pairs)*2 {
		return nil, errors.New("AGENTIC_HOLDOUT_CLI_INPUT_SET_INVALID")
	}
	wanted := make(map[string]bool, len(inputs.Trials))
	for _, pair := range contract.Pairs {
		wanted[pair.Control.TrialID] = true
		wanted[pair.Candidate.TrialID] = true
	}
	base, err := filepath.Abs(filepath.Dir(inputsPath))
	if err != nil {
		return nil, err
	}
	seenTrials, seenDirectories := map[string]bool{}, map[string]bool{}
	evidence := make(map[string]defectbench.AgenticTrialEvidence, len(inputs.Trials))
	for _, input := range inputs.Trials {
		if !wanted[input.TrialID] || seenTrials[input.TrialID] || strings.TrimSpace(input.EpisodeDir) == "" {
			return nil, errors.New("AGENTIC_HOLDOUT_CLI_INPUT_SET_INVALID")
		}
		directory := input.EpisodeDir
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(base, filepath.Clean(directory))
		}
		directory, err = filepath.Abs(directory)
		if err != nil || seenDirectories[directory] {
			return nil, errors.New("AGENTIC_HOLDOUT_CLI_INPUT_SET_INVALID")
		}
		info, statErr := os.Lstat(directory)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_EPISODE_DIRECTORY_INVALID: %s", input.TrialID)
		}
		summary, err := readAgenticEpisodeSummary(filepath.Join(directory, "summary.json"))
		if err != nil {
			return nil, fmt.Errorf("load Agentic summary %s: %w", input.TrialID, err)
		}
		current := defectbench.AgenticTrialEvidence{
			TargetID: summary.TargetID, EpisodeStatus: summary.Status,
			EvidenceStatus: summary.Assessment.Status, ModelWork: summary.Work.Model,
		}
		bundlePath := filepath.Join(directory, "bundle.json")
		if bundleInfo, bundleErr := os.Lstat(bundlePath); bundleErr == nil {
			if !bundleInfo.Mode().IsRegular() || bundleInfo.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BUNDLE_INVALID: %s", input.TrialID)
			}
			var bundle controlexperiment.ExecutionBundle
			if err := readStrictJSON(bundlePath, &bundle); err != nil {
				return nil, err
			}
			current.Bundle = &bundle
		} else if !errors.Is(bundleErr, os.ErrNotExist) {
			return nil, bundleErr
		}
		evidence[input.TrialID] = current
		seenTrials[input.TrialID], seenDirectories[directory] = true, true
	}
	return evidence, nil
}

func readAgenticEpisodeSummary(path string) (agenticEpisodeSummaryProjection, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 512<<10 {
		return agenticEpisodeSummaryProjection{}, errors.New("AGENTIC_HOLDOUT_CLI_SUMMARY_INVALID")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return agenticEpisodeSummaryProjection{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var summary agenticEpisodeSummaryProjection
	if err := decoder.Decode(&summary); err != nil {
		return agenticEpisodeSummaryProjection{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agenticEpisodeSummaryProjection{}, errors.New("AGENTIC_HOLDOUT_CLI_SUMMARY_TRAILING_JSON")
	}
	return summary, nil
}

func agenticHoldoutComposition(
	projectorID string,
) (semantic.DecisionProjector, []oracle.BundleMonitor, error) {
	monitors := []oracle.BundleMonitor{oracle.BundleAgreement{}}
	switch projectorID {
	case etcdraftv2.DecisionProjectionID:
		return etcdraftv2.DecisionProjector{}, monitors, nil
	case omnipaxosv2.DecisionProjectionID:
		return omnipaxosv2.DecisionProjector{}, monitors, nil
	default:
		return nil, nil, errors.New("AGENTIC_HOLDOUT_CLI_PROJECTOR_UNSUPPORTED")
	}
}

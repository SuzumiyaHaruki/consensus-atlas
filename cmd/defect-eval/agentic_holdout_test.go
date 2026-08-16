package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func TestAgenticHoldoutCLIConsumesEpisodeDirectoriesAndWritesTrustedResults(t *testing.T) {
	spec, _, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	contract, _, _ := writeFormalCLIFixture(t, root, spec, bundle)
	inputs := agenticHoldoutInputs{SchemaVersion: agenticHoldoutInputsSchemaVersion}
	episodeByTrial := make(map[string]string, len(contract.Pairs)*2)
	for _, pair := range contract.Pairs {
		for _, trialID := range []string{pair.Control.TrialID, pair.Candidate.TrialID} {
			relative := "episode-" + trialID
			directory := filepath.Join(root, relative)
			if err := os.MkdirAll(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			summary := map[string]any{
				"target_id": "etcdraft-v2", "status": defectbench.AgenticEpisodeCompleted,
				"work":                      map[string]any{"model": controlexperiment.ModelWork{}},
				"evidence_assessment":       map[string]any{"status": "oracle-finding"},
				"forward_compatible_detail": map[string]any{"ignored_by_evaluator": true},
			}
			if err := writeJSON(filepath.Join(directory, "summary.json"), summary); err != nil {
				t.Fatal(err)
			}
			if err := writeJSON(filepath.Join(directory, "bundle.json"), bundle); err != nil {
				t.Fatal(err)
			}
			inputs.Trials = append(inputs.Trials, agenticHoldoutTrialInput{
				TrialID: trialID, EpisodeDir: relative,
			})
			episodeByTrial[trialID] = directory
		}
	}
	inputsPath := filepath.Join(root, "agentic-inputs.json")
	if err := writeJSON(inputsPath, inputs); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "agentic-evaluation.json")
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath, output,
	); err != nil {
		t.Fatal(err)
	}
	var evaluation defectbench.AgenticHoldoutEvaluation
	if err := readStrictJSON(output, &evaluation); err != nil {
		t.Fatal(err)
	}
	if err := evaluation.Validate(); err != nil {
		t.Fatal(err)
	}
	want := defectbench.FormalEvaluationSummary{Controls: 3, Candidates: 3, RootCauses: 3}
	if evaluation.Summary != want {
		t.Fatalf("self-reported findings affected holdout verdict: %#v", evaluation.Summary)
	}
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath, output,
	); err == nil || !os.IsExist(err) {
		t.Fatalf("existing private output error = %v", err)
	}

	incompleteTrial := contract.Pairs[0].Candidate.TrialID
	incompleteDirectory := episodeByTrial[incompleteTrial]
	if err := writeJSON(filepath.Join(incompleteDirectory, "summary.json"), map[string]any{
		"target_id": "etcdraft-v2", "status": defectbench.AgenticEpisodeRiskStopped,
		"work":                map[string]any{"model": controlexperiment.ModelWork{}},
		"evidence_assessment": map[string]any{"status": "planning-failed"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(incompleteDirectory, "bundle.json")); err != nil {
		t.Fatal(err)
	}
	incompleteOutput := filepath.Join(root, "agentic-incomplete-evaluation.json")
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"),
		inputsPath, incompleteOutput,
	); err != nil {
		t.Fatal(err)
	}
	if err := readStrictJSON(incompleteOutput, &evaluation); err != nil {
		t.Fatal(err)
	}
	if evaluation.Summary.InvalidTrials != 1 || evaluation.Summary.KilledCandidates != 0 {
		t.Fatalf("incomplete Episode was not isolated as invalid: %#v", evaluation.Summary)
	}
}

func TestAgenticHoldoutModeRejectsRunnerFlags(t *testing.T) {
	err := run([]string{
		"-formal-contract", "contract.json", "-formal-exposure-audit", "exposure.json",
		"-agentic-inputs", "episodes.json", "-out", "evaluation.json",
		"-method-spec", "method.json",
	})
	if err == nil || !strings.Contains(err.Error(), "no runner/public-pair flags") {
		t.Fatalf("mixed Agentic holdout mode error = %v", err)
	}
}

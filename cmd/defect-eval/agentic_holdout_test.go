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
	agenticBudget := controlexperiment.AgenticLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: contract.Budget.MaxDecisions,
		MaxPrimaryWorkUnits: contract.Budget.MaxPrimaryWorkUnits,
		MaxReplayWorkUnits:  2 * bundle.Work.Replay.WorkUnits,
		MaxModelCalls:       6, MaxModelTokens: 50_000,
	}
	contract.AgenticBudget = &agenticBudget
	contract, err := contract.Seal()
	if err != nil {
		t.Fatal(err)
	}
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	exposure, err := defectbench.AuditFormalExposure(
		contract, view,
		[]defectbench.FormalPublicArtifact{{Bytes: []byte(`{"trial_id":"opaque-01"}`)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(root, "contract.json"), contract); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(root, "exposure.json"), exposure); err != nil {
		t.Fatal(err)
	}
	inputs := agenticHoldoutInputs{SchemaVersion: agenticHoldoutInputsSchemaVersion}
	episodeByTrial := make(map[string]string, len(contract.Pairs)*2)
	for _, pair := range contract.Pairs {
		for _, trialID := range []string{pair.Control.TrialID, pair.Candidate.TrialID} {
			relative := "episode-" + trialID
			directory := filepath.Join(root, relative)
			if err := os.MkdirAll(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			summary := agenticHoldoutTestSummary(contract, bundle, false)
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
	branchOnlyTrial := contract.Pairs[2].Candidate.TrialID
	branchOnlyDirectory := episodeByTrial[branchOnlyTrial]
	if err := os.Remove(filepath.Join(branchOnlyDirectory, "bundle.json")); err != nil {
		t.Fatal(err)
	}
	branchEvidence := []map[string]any{{
		"branch_id": "treatment-final", "intent": "branch",
		"testing": map[string]any{
			"plan_id": "branch-plan", "risk": map[string]any{"id": "branch-risk"},
			"execution_bundle": bundle,
		},
	}}
	if err := writeJSON(
		filepath.Join(branchOnlyDirectory, "summary.json"),
		agenticHoldoutTestSummary(contract, bundle, true),
	); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(branchOnlyDirectory, "branch-evidence.json"), branchEvidence); err != nil {
		t.Fatal(err)
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
	undeclaredTrial := contract.Pairs[0].Control.TrialID
	undeclaredDirectory := episodeByTrial[undeclaredTrial]
	if err := writeJSON(filepath.Join(undeclaredDirectory, "branch-evidence.json"), branchEvidence); err != nil {
		t.Fatal(err)
	}
	undeclaredOutput := filepath.Join(root, "agentic-undeclared-branch.json")
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"),
		inputsPath, undeclaredOutput,
	); err == nil || !strings.Contains(err.Error(), "BRANCH_EVIDENCE_INVALID") {
		t.Fatalf("summary-undeclared branch evidence was accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(undeclaredDirectory, "branch-evidence.json")); err != nil {
		t.Fatal(err)
	}
	_, _, otherMethodBundle := formalCLITestExecutionForMethod(t, "other-formal-method")
	replacedTrial := contract.Pairs[1].Candidate.TrialID
	replacedDirectory := episodeByTrial[replacedTrial]
	if err := writeJSON(filepath.Join(replacedDirectory, "bundle.json"), otherMethodBundle); err != nil {
		t.Fatal(err)
	}
	replacedOutput := filepath.Join(root, "agentic-cross-method.json")
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"),
		inputsPath, replacedOutput,
	); err == nil || !strings.Contains(err.Error(), "BUNDLE_INVALID") {
		t.Fatalf("cross-method Bundle replacement was accepted: %v", err)
	}
	if err := writeJSON(filepath.Join(replacedDirectory, "bundle.json"), bundle); err != nil {
		t.Fatal(err)
	}
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath, output,
	); err == nil || !os.IsExist(err) {
		t.Fatalf("existing private output error = %v", err)
	}

	incompleteTrial := contract.Pairs[0].Candidate.TrialID
	incompleteDirectory := episodeByTrial[incompleteTrial]
	if err := writeJSON(filepath.Join(incompleteDirectory, "summary.json"), map[string]any{
		"target_id":           "etcdraft-v2",
		"method_spec_digest":  contract.MethodSpecDigest,
		"status":              defectbench.AgenticEpisodeRiskStopped,
		"budget":              agenticHoldoutTestBudget(contract),
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
	oversized := make([]map[string]any, controlexperiment.ScenarioAgentMaxCalls+1)
	for index := range oversized {
		oversized[index] = map[string]any{
			"branch_id": "oversized-" + string(rune('a'+index)), "intent": "branch",
			"testing": map[string]any{"execution_bundle": bundle},
		}
	}
	if err := writeJSON(filepath.Join(branchOnlyDirectory, "branch-evidence.json"), oversized); err != nil {
		t.Fatal(err)
	}
	oversizedOutput := filepath.Join(root, "agentic-oversized-evaluation.json")
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"),
		inputsPath, oversizedOutput,
	); err == nil || !strings.Contains(err.Error(), "BRANCH_EVIDENCE_INVALID") {
		t.Fatalf("oversized branch evidence was accepted: %v", err)
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

func agenticHoldoutTestBudget(
	contract defectbench.FormalBenchmarkContract,
) map[string]any {
	return map[string]any{
		"max_risk_calls": 3, "max_scenario_calls": 3,
		"max_total_calls":         contract.AgenticBudget.MaxModelCalls,
		"max_observed_tokens":     contract.AgenticBudget.MaxModelTokens,
		"max_scenario_plan_steps": 4,
		"max_runtime_decisions":   contract.AgenticBudget.MaxPrimarySchedulerDecisions,
		"logical_budget":          *contract.AgenticBudget,
	}
}

func agenticHoldoutTestSummary(
	contract defectbench.FormalBenchmarkContract,
	bundle controlexperiment.ExecutionBundle,
	branchOnly bool,
) map[string]any {
	model := controlexperiment.ModelWork{}
	work := map[string]any{
		"model": model, "scenario_frontier": controlexperiment.PhaseWork{},
		"scenario_search":     controlexperiment.StatelessDFSWork{},
		"qualified_execution": bundle.Work,
	}
	summary := map[string]any{
		"target_id": "etcdraft-v2", "method_spec_digest": contract.MethodSpecDigest,
		"status": defectbench.AgenticEpisodeCompleted, "budget": agenticHoldoutTestBudget(contract),
		"plan_id": "primary-plan", "risk_result_id": "primary-risk",
		"trace_digest": bundle.Trace.Digest, "work": work,
		"evidence_assessment":       map[string]any{"status": "oracle-finding"},
		"forward_compatible_detail": map[string]any{"ignored_by_evaluator": true},
	}
	if !branchOnly {
		return summary
	}
	model = controlexperiment.ModelWork{Calls: 1, InputTokens: 1, TotalTokens: 1}
	work["model"] = model
	work["qualified_execution"] = controlexperiment.WorkLedger{}
	work["branch_qualified_executions"] = []map[string]any{{
		"branch_id": "treatment-final", "work": bundle.Work,
	}}
	delete(summary, "plan_id")
	delete(summary, "risk_result_id")
	delete(summary, "trace_digest")
	summary["scenario_attempts"] = 1
	summary["branch_evidence"] = []map[string]any{{
		"branch_id": "treatment-final", "intent": "branch", "plan_id": "branch-plan",
		"risk_result_id": "branch-risk", "trace_digest": bundle.Trace.Digest, "work": bundle.Work,
	}}
	return summary
}

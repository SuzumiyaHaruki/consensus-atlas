package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

func TestAgenticHoldoutCLIConsumesEpisodeDirectoriesAndWritesTrustedResults(t *testing.T) {
	episodeBudget := controlexperiment.AgenticLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: 64, MaxPrimaryWorkUnits: 66,
		MaxReplayWorkUnits: 68, MaxModelCalls: 6, MaxModelTokens: 50_000,
	}
	methodSpec := agenticHoldoutCLIMethodSpec(t, 1, episodeBudget)
	spec, report, bundle := formalCLITestExecutionWithDigest(t, methodSpec.Digest)
	bundle = agenticHoldoutAttachRecipe(t, bundle, report.Config)
	root := t.TempDir()
	contract, _, _ := writeFormalCLIFixture(t, root, spec, bundle)
	contract.MethodSpecDigest = methodSpec.Digest
	contract.AgenticBudget = &methodSpec.InvestigationBudget
	contract.Budget.MaxDecisions = methodSpec.InvestigationBudget.MaxPrimarySchedulerDecisions
	contract.Budget.MaxPrimaryWorkUnits = methodSpec.InvestigationBudget.MaxPrimaryWorkUnits
	contract.Composition.MonitorIDs = []string{
		"agreement", targetoracles.ClientApplicationBindingMonitorID,
		targetoracles.ElectionSafetyMonitorID, targetoracles.LogProgressMonitorID,
	}
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
			summary := agenticHoldoutTestSummary(t, contract, bundle)
			if err := writeJSON(filepath.Join(directory, "summary.json"), summary); err != nil {
				t.Fatal(err)
			}
			writeAgenticHoldoutTestJournals(t, directory, summary)
			if err := writeJSON(filepath.Join(directory, "method-spec.json"), methodSpec); err != nil {
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
	accountingDirectory := episodeByTrial[contract.Pairs[2].Candidate.TrialID]

	inputsPath := filepath.Join(root, "agentic-inputs.json")
	if err := writeJSON(inputsPath, inputs); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "agentic-evaluation.json")
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath, output,
		agenticHoldoutTestReplayFactory,
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
	wantOracle := targetoracles.EtcdraftV2Registry().Check(bundle)
	for _, result := range evaluation.Results {
		if !reflect.DeepEqual(result.Result.Oracle, wantOracle) {
			t.Fatalf("online and holdout Oracle composition drifted: got=%#v want=%#v",
				result.Result.Oracle, wantOracle)
		}
	}
	boundaryTrial := contract.Pairs[0].Control.TrialID
	boundaryDirectory := episodeByTrial[boundaryTrial]
	tamperedBoundary := agenticHoldoutTestSummary(t, contract, bundle)
	tamperedBoundary["oracle_attribution"] = map[string]any{"root_decisions": 2}
	if err := writeJSON(filepath.Join(boundaryDirectory, "summary.json"), tamperedBoundary); err != nil {
		t.Fatal(err)
	}
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath,
		filepath.Join(root, "boundary-tamper-evaluation.json"), agenticHoldoutTestReplayFactory,
	); err == nil || !strings.Contains(err.Error(), "AGENTIC_HOLDOUT_CLI_ORACLE_BOUNDARY_INVALID") {
		t.Fatalf("off-by-one root boundary was accepted: %v", err)
	}
	if err := writeJSON(
		filepath.Join(boundaryDirectory, "summary.json"),
		agenticHoldoutTestSummary(t, contract, bundle),
	); err != nil {
		t.Fatal(err)
	}
	tamperedAccounting := agenticHoldoutTestSummary(t, contract, bundle)
	tamperedWork := tamperedAccounting["work"].(map[string]any)
	tamperedWork["model"] = controlexperiment.ModelWork{}
	if err := writeJSON(filepath.Join(accountingDirectory, "summary.json"), tamperedAccounting); err != nil {
		t.Fatal(err)
	}
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath,
		filepath.Join(root, "agentic-tampered-accounting.json"), agenticHoldoutTestReplayFactory,
	); err == nil || !strings.Contains(err.Error(), "SUMMARY_INVALID") {
		t.Fatalf("summary model work drift from provider audits was accepted: %v", err)
	}
	if err := writeJSON(
		filepath.Join(accountingDirectory, "summary.json"),
		agenticHoldoutTestSummary(t, contract, bundle),
	); err != nil {
		t.Fatal(err)
	}
	journalResultPath := filepath.Join(
		accountingDirectory, "scenario-agent", "model-calls", "001-scenario-root", "result.json",
	)
	var journalResult controlexperiment.StatelessAgentCallResult
	if err := readStrictJSON(journalResultPath, &journalResult); err != nil {
		t.Fatal(err)
	}
	journalResult.Work.TotalTokens++
	if err := writeJSON(journalResultPath, journalResult); err != nil {
		t.Fatal(err)
	}
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath,
		filepath.Join(root, "agentic-tampered-journal.json"), agenticHoldoutTestReplayFactory,
	); err == nil || !strings.Contains(err.Error(), "SCENARIO_JOURNAL_INVALID") {
		t.Fatalf("tampered provider journal was accepted: %v", err)
	}
	writeAgenticHoldoutTestJournals(
		t, accountingDirectory, agenticHoldoutTestSummary(t, contract, bundle),
	)

	_, _, otherMethodBundle := formalCLITestExecutionForMethod(t, "other-formal-method")
	replacedTrial := contract.Pairs[1].Candidate.TrialID
	replacedDirectory := episodeByTrial[replacedTrial]
	if err := writeJSON(filepath.Join(replacedDirectory, "bundle.json"), otherMethodBundle); err != nil {
		t.Fatal(err)
	}
	replacedOutput := filepath.Join(root, "agentic-cross-method.json")
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"),
		inputsPath, replacedOutput, agenticHoldoutTestReplayFactory,
	); err == nil || !strings.Contains(err.Error(), "BUNDLE_INVALID") {
		t.Fatalf("cross-method Bundle replacement was accepted: %v", err)
	}
	if err := writeJSON(filepath.Join(replacedDirectory, "bundle.json"), bundle); err != nil {
		t.Fatal(err)
	}
	driftSummary := agenticHoldoutTestSummary(t, contract, bundle)
	driftBudget := driftSummary["budget"].(map[string]any)
	driftBudget["max_scenario_plan_steps"] = 3
	if err := writeJSON(filepath.Join(replacedDirectory, "summary.json"), driftSummary); err != nil {
		t.Fatal(err)
	}
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath,
		filepath.Join(root, "agentic-method-limit-drift.json"), agenticHoldoutTestReplayFactory,
	); err == nil || !strings.Contains(err.Error(), "SUMMARY_INVALID") {
		t.Fatalf("Episode limits drift was accepted: %v", err)
	}
	if err := writeJSON(
		filepath.Join(replacedDirectory, "summary.json"), agenticHoldoutTestSummary(t, contract, bundle),
	); err != nil {
		t.Fatal(err)
	}
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath, output,
		agenticHoldoutTestReplayFactory,
	); err == nil || !os.IsExist(err) {
		t.Fatalf("existing private output error = %v", err)
	}

	incompleteTrial := contract.Pairs[0].Candidate.TrialID
	incompleteDirectory := episodeByTrial[incompleteTrial]
	incompleteSummary := map[string]any{
		"target_id":           "etcdraft-v2",
		"method_spec_digest":  contract.MethodSpecDigest,
		"status":              defectbench.AgenticEpisodeRiskStopped,
		"budget":              agenticHoldoutTestBudget(contract),
		"work":                map[string]any{"model": controlexperiment.ModelWork{}},
		"evidence_assessment": map[string]any{"status": "planning-failed"},
	}
	if err := writeJSON(filepath.Join(incompleteDirectory, "summary.json"), incompleteSummary); err != nil {
		t.Fatal(err)
	}
	writeAgenticHoldoutTestJournals(t, incompleteDirectory, incompleteSummary)
	if err := os.Remove(filepath.Join(incompleteDirectory, "bundle.json")); err != nil {
		t.Fatal(err)
	}
	incompleteOutput := filepath.Join(root, "agentic-incomplete-evaluation.json")
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"),
		inputsPath, incompleteOutput, agenticHoldoutTestReplayFactory,
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

func TestAgenticHoldoutCLIRequiresCompleteInvestigationAndAggregatesPriorEpisodes(t *testing.T) {
	episodeBudget := controlexperiment.AgenticLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: 64, MaxPrimaryWorkUnits: 66,
		MaxReplayWorkUnits: 68, MaxModelCalls: 6, MaxModelTokens: 50_000,
	}
	methodSpec := agenticHoldoutCLIMethodSpec(t, 2, episodeBudget)
	carrier, report, bundle := formalCLITestExecutionWithDigest(t, methodSpec.Digest)
	bundle = agenticHoldoutAttachRecipe(t, bundle, report.Config)
	root := t.TempDir()
	contract, _, _ := writeFormalCLIFixture(t, root, carrier, bundle)
	contract.MethodSpecDigest = methodSpec.Digest
	contract.AgenticBudget = &methodSpec.InvestigationBudget
	contract.Budget.MaxDecisions = methodSpec.InvestigationBudget.MaxPrimarySchedulerDecisions
	contract.Budget.MaxPrimaryWorkUnits = methodSpec.InvestigationBudget.MaxPrimaryWorkUnits
	contract, err := contract.Seal()
	if err != nil {
		t.Fatal(err)
	}
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	exposure, err := defectbench.AuditFormalExposure(
		contract, view, []defectbench.FormalPublicArtifact{{Bytes: []byte(`{"trial_id":"opaque-01"}`)}},
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
	var firstInvestigation string
	for _, pair := range contract.Pairs {
		for _, trialID := range []string{pair.Control.TrialID, pair.Candidate.TrialID} {
			relative := "investigation-" + trialID
			if firstInvestigation == "" {
				firstInvestigation = relative
			}
			for episode := 1; episode <= methodSpec.InvestigationEpisodes; episode++ {
				directory := filepath.Join(root, relative, "episode-000"+string(rune('0'+episode)))
				if err := os.MkdirAll(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := writeJSON(filepath.Join(directory, "method-spec.json"), methodSpec); err != nil {
					t.Fatal(err)
				}
				if episode == 1 {
					riskAudit := agenticHoldoutTestAudit(t, "risk-root", controlexperiment.ModelWork{
						Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5,
					})
					summary := map[string]any{
						"target_id": "etcdraft-v2", "method_spec_digest": methodSpec.Digest,
						"status":        defectbench.AgenticEpisodeRiskStopped,
						"risk_attempts": 1, "budget": agenticHoldoutTestBudgetForLogical(episodeBudget),
						"risk_provider_calls": []controlexperiment.StatelessAgentCallAudit{riskAudit},
						"work": map[string]any{
							"model":             controlexperiment.ModelWork{Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
							"scenario_frontier": controlexperiment.PhaseWork{SetupAttempts: 1, WorkUnits: 1},
						},
						"evidence_assessment": map[string]any{"status": "witness-near-miss"},
					}
					if err := writeJSON(filepath.Join(directory, "summary.json"), summary); err != nil {
						t.Fatal(err)
					}
					writeAgenticHoldoutTestJournals(t, directory, summary)
					continue
				}
				summary := agenticHoldoutTestSummary(t, contract, bundle)
				summary["budget"] = agenticHoldoutTestBudgetForLogical(episodeBudget)
				if err := writeJSON(filepath.Join(directory, "summary.json"), summary); err != nil {
					t.Fatal(err)
				}
				writeAgenticHoldoutTestJournals(t, directory, summary)
				if err := writeJSON(filepath.Join(directory, "bundle.json"), bundle); err != nil {
					t.Fatal(err)
				}
			}
			inputs.Trials = append(inputs.Trials, agenticHoldoutTrialInput{
				TrialID: trialID, InvestigationDir: relative,
			})
		}
	}
	inputsPath := filepath.Join(root, "investigation-inputs.json")
	if err := writeJSON(inputsPath, inputs); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "investigation-evaluation.json")
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath, output,
		agenticHoldoutTestReplayFactory,
	); err != nil {
		t.Fatal(err)
	}
	var evaluation defectbench.AgenticHoldoutEvaluation
	if err := readStrictJSON(output, &evaluation); err != nil {
		t.Fatal(err)
	}
	for _, result := range evaluation.Results {
		if result.ModelWork.Calls != 2 || result.ModelWork.TotalTokens != 7 ||
			result.Result.PrimaryWork != bundle.Work.Primary.WorkUnits+1+len(bundle.Trace.Records) {
			t.Fatalf("prior Episode work was not aggregated: %#v", result)
		}
	}
	inputs.Trials[0].InvestigationDir = ""
	inputs.Trials[0].EpisodeDir = filepath.Join(firstInvestigation, "episode-0002")
	directPath := filepath.Join(root, "cherry-pick-inputs.json")
	if err := writeJSON(directPath, inputs); err != nil {
		t.Fatal(err)
	}
	if err := runAgenticHoldoutEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), directPath,
		filepath.Join(root, "cherry-pick-evaluation.json"), agenticHoldoutTestReplayFactory,
	); err == nil || !strings.Contains(err.Error(), "SINGLE_EPISODE_METHOD_INVALID") {
		t.Fatalf("final Episode cherry-pick was accepted: %v", err)
	}
}

func agenticHoldoutCLIMethodSpec(
	t *testing.T,
	episodes int,
	episodeBudget controlexperiment.AgenticLogicalBudget,
) controlexperiment.AgenticMethodSpec {
	t.Helper()
	total, err := controlexperiment.ScaleAgenticLogicalBudget(episodeBudget, episodes)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := controlexperiment.NewAgenticMethodSpec(controlexperiment.AgenticMethodSpec{
		TargetID: "etcdraft-v2",
		Transport: controlexperiment.AgentTransportFreeze{
			Provider: "openrouter", Endpoint: "https://openrouter.ai/api/v1/chat/completions",
			Model: "fixture/agentic-model", Thinking: "low", ExcludeReasoning: true,
			StructuredOutputMode: "json-schema", RequestTimeoutMS: 900_000,
			RoutingPolicy: "openrouter-default", AllowProviderFallback: true,
			MaxOutputTokens: 32000, MaxCallsPerArm: 1,
		},
		RiskPromptVersion: "risk-agent-navigation-v2", ScenarioPromptVersion: "scenario-agent-investigation-v9",
		SemanticInputSchema:      "etcdraft-agentic-input-v1",
		SemanticInputDigest:      digestBytes([]byte("agentic-cli-semantic-input")),
		ScenarioSemanticExposure: controlexperiment.ScenarioSemanticExposureFull,
		RiskInputMode:            controlexperiment.AgenticRiskInputAgentGenerated,
		ClosureMode:              controlexperiment.AgenticClosureModePublicFixed,
		SourceExposure:           controlexperiment.AgenticSourceExposureSpec{Mode: controlexperiment.AgenticSourceExposureNone},
		EpisodeLimits: controlexperiment.AgenticEpisodeLimits{
			MaxRiskCalls: 3, MaxScenarioCalls: 3, MaxTotalCalls: episodeBudget.MaxModelCalls,
			MaxObservedTokens: episodeBudget.MaxModelTokens, MaxScenarioPlanSteps: 4,
			MaxRuntimeDecisions: episodeBudget.MaxPrimarySchedulerDecisions,
			SessionWallClockMS:  600_000, PreparationWallClockMS: 600_000,
		},
		InvestigationEpisodes: episodes, EpisodeBudget: episodeBudget, InvestigationBudget: total,
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
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
	return agenticHoldoutTestBudgetForLogical(*contract.AgenticBudget)
}

func agenticHoldoutTestBudgetForLogical(
	logical controlexperiment.AgenticLogicalBudget,
) map[string]any {
	return map[string]any{
		"max_risk_calls": 3, "max_scenario_calls": 3,
		"max_total_calls":         logical.MaxModelCalls,
		"max_observed_tokens":     logical.MaxModelTokens,
		"max_scenario_plan_steps": 4,
		"max_runtime_decisions":   logical.MaxPrimarySchedulerDecisions,
		"logical_budget":          logical,
	}
}

func agenticHoldoutTestSummary(
	t *testing.T,
	contract defectbench.FormalBenchmarkContract,
	bundle controlexperiment.ExecutionBundle,
) map[string]any {
	rootDecisions := 1
	pathDecisions := len(bundle.Trace.Records) - rootDecisions
	model := controlexperiment.ModelWork{Calls: 1, InputTokens: 1, OutputTokens: 1, TotalTokens: 2}
	executionWork := controlexperiment.ScenarioExecutionWork{
		ChildMaterialization: controlexperiment.PhaseWork{
			SchedulerDecisions: pathDecisions, WorkUnits: pathDecisions,
		},
		TotalWorkUnits: pathDecisions,
	}
	work := map[string]any{
		"preparation": controlexperiment.AgenticPreparationWork{WallClockMS: 1},
		"model":       model, "scenario_frontier": controlexperiment.PhaseWork{
			SchedulerDecisions: rootDecisions, WorkUnits: rootDecisions,
		},
		"scenario_search":     executionWork,
		"qualified_execution": bundle.Work,
	}
	summary := map[string]any{
		"target_id": "etcdraft-v2", "method_spec_digest": contract.MethodSpecDigest,
		"status": defectbench.AgenticEpisodeCompleted, "budget": agenticHoldoutTestBudget(contract),
		"plan_id": "primary-plan", "risk_result_id": "primary-risk",
		"trace_digest": bundle.Trace.Digest, "work": work,
		"scenario_decisions_used": pathDecisions,
		"selected_path_decisions": pathDecisions,
		"decision_provenance":     controlexperiment.AgenticDecisionProvenance{PublicProgress: pathDecisions},
		"scenario_attempts":       1,
		"scenario_provider_calls": []controlexperiment.StatelessAgentCallAudit{
			agenticHoldoutTestAudit(t, "scenario-root", model),
		},
		"scenario_attempt_feedback": []map[string]any{{
			"ordinal": 1, "entered_execution": true, "execution_work": &executionWork,
		}},
		"oracle_attribution":        map[string]any{"root_decisions": rootDecisions},
		"evidence_assessment":       map[string]any{"status": "oracle-finding"},
		"forward_compatible_detail": map[string]any{"ignored_by_evaluator": true},
	}
	return summary
}

func agenticHoldoutTestReplayFactory(
	_ map[string]defectbench.AgenticTrialEvidence,
) defectbench.AgenticReplayRunner {
	return func(_ string, bundle controlexperiment.ExecutionBundle) (controlexperiment.ExecutionBundle, error) {
		return bundle, nil
	}
}

func agenticHoldoutAttachRecipe(
	t *testing.T,
	bundle controlexperiment.ExecutionBundle,
	config controlexperiment.Config,
) controlexperiment.ExecutionBundle {
	t.Helper()
	targetConfig, err := json.Marshal(etcdraftv2.ThreeNodeConfig())
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = bundle.WithExecutionRecipe(controlexperiment.ExecutionRecipe{
		TargetID: "etcdraft-v2", Config: config, TargetConfig: targetConfig,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func agenticHoldoutTestAudit(
	t *testing.T,
	rootID string,
	work controlexperiment.ModelWork,
) controlexperiment.StatelessAgentCallAudit {
	t.Helper()
	audit, _, _, _ := agenticHoldoutTestCall(t, rootID, work)
	return audit
}

func agenticHoldoutTestCall(
	t *testing.T,
	rootID string,
	work controlexperiment.ModelWork,
) (
	controlexperiment.StatelessAgentCallAudit,
	controlexperiment.StatelessAgentCallIntent,
	controlexperiment.StatelessAgentCallDispatch,
	controlexperiment.StatelessAgentCallResult,
) {
	t.Helper()
	requestDigest := strings.Repeat("3", 64)
	transport := controlexperiment.AgentTransportFreeze{
		Provider: "fixture", Endpoint: "https://example.invalid/v1", Model: "fixture-model",
		Thinking: "disabled", StructuredOutputMode: "json-object", RequestTimeoutMS: 1_000,
		RoutingPolicy: "fixture-fixed", MaxOutputTokens: 200, MaxCallsPerArm: 1,
	}
	intent, err := controlexperiment.NewPlanningAgentCallIntent(
		"formal-agent-call", 1, rootID, requestDigest, transport,
		[]byte(`[{"role":"user","content":"formal"}]`), []byte(`{"model":"fixture-model"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := controlexperiment.NewStatelessAgentCallDispatch(intent)
	if err != nil {
		t.Fatal(err)
	}
	result, err := controlexperiment.NewStatelessAgentCallResult(
		intent, dispatch, controlexperiment.StatelessAgentCallResult{
			Status:  controlexperiment.StatelessAgentCallContentReady,
			Content: []byte(`{"proposal":"fixture"}`), ResponseDigest: strings.Repeat("7", 64),
			Response: &controlexperiment.AgentResponseIdentity{
				ID: "fixture-response", Model: "fixture-model", FinishReason: "stop",
			},
			Work: work, ProviderUsageStatus: "observed",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := controlexperiment.NewStatelessAgentCallAudit(intent, &dispatch, &result)
	if err != nil {
		t.Fatal(err)
	}
	return audit, intent, dispatch, result
}

func writeAgenticHoldoutTestJournals(
	t *testing.T,
	directory string,
	summary map[string]any,
) {
	t.Helper()
	for _, current := range []struct {
		name  string
		field string
	}{
		{name: "risk-agent", field: "risk_provider_calls"},
		{name: "scenario-agent", field: "scenario_provider_calls"},
	} {
		root := filepath.Join(directory, current.name, "model-calls")
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		audits, _ := summary[current.field].([]controlexperiment.StatelessAgentCallAudit)
		for index, audit := range audits {
			want, intent, dispatch, result := agenticHoldoutTestCall(t, audit.RootID, audit.Work)
			if !reflect.DeepEqual(want, audit) {
				t.Fatalf("test journal audit cannot be reconstructed: got=%#v want=%#v", want, audit)
			}
			callDirectory := filepath.Join(root, fmt.Sprintf("%03d-%s", index+1, audit.RootID))
			if err := os.MkdirAll(callDirectory, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]any{
				"intent.json": intent, "dispatch.json": dispatch, "result.json": result,
			} {
				if err := writeJSON(filepath.Join(callDirectory, name), value); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

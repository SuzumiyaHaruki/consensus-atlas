package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestOmnipaxosAgenticEpisodeBoundsAccountsAndRecoversBothAgents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	workerPath := buildOmnipaxosScenarioWorker(t)
	inputs, err := prepareOmnipaxosAgenticEpisode(
		ctx, workerPath, "../../plans/agent/omnipaxos-agentic-calibration-v1.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	configuredBudget, err := agenticEpisodeBudgetFromExperiment(
		inputs.Experiment.ScenarioMaxCalls, inputs.Experiment.ScenarioMaxSteps,
		inputs.Experiment.ScenarioMaxDecisions, inputs.Experiment.SessionBudget,
	)
	if err != nil || configuredBudget.MaxRiskCalls != 3 ||
		configuredBudget.MaxScenarioCalls != 1 || configuredBudget.MaxTotalCalls != 4 {
		t.Fatalf("OmniPaxos A9e1 budget does not permit Risk repair: %#v/%v", configuredBudget, err)
	}
	candidateBytes, err := json.Marshal(omnipaxosDiscoveredRiskCandidate())
	if err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	client := fixtureOpenRouterIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		var payload openRouterChatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		var content []byte
		switch payload.ResponseFormat.JSONSchema.Name {
		case "risk_candidate":
			content = candidateBytes
		case scenarioPlanStructuredOutputName:
			view := a4bScenarioViewFromPayload(t, payload)
			for index, action := range view.Frontier.Actions {
				if action.Kind == control.ActionDropMessage &&
					view.Semantics.ActionHints[index].MessageClass == controlexperiment.ConsensusMessageReplication {
					content, err = json.Marshal(controlexperiment.ScenarioPlan{
						ID: "agentic-episode-scenario", Steps: []controlexperiment.ScenarioStep{{
							ID: "drop-replication", Selector: controlexperiment.FrontierActionSelector{
								ActionID: action.ActionID,
							},
						}},
					})
					break
				}
			}
			if len(content) == 0 {
				t.Fatal("Scenario Agent prompt had no replication message")
			}
		default:
			t.Fatalf("unexpected structured output %q", payload.ResponseFormat.JSONSchema.Name)
		}
		if err != nil {
			t.Fatal(err)
		}
		response := a2b2OpenRouterResponse(t, providerCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := t.TempDir()
	riskJournal, err := newStatelessAgentCallJournal(filepath.Join(directory, "risk"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	scenarioJournal, err := newScenarioAgentCallJournal(filepath.Join(directory, "scenario"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	keyActivations := 0
	activateRisk := func() error {
		keyActivations++
		return riskJournal.ActivateKey("fixture-key")
	}
	activateScenario := func() error {
		keyActivations++
		return scenarioJournal.ActivateKey("fixture-key")
	}
	budget := agenticEpisodeBudget{
		MaxRiskCalls: 1, MaxScenarioCalls: 1, MaxTotalCalls: 2,
		MaxObservedTokens: 20, MaxScenarioPlanSteps: 1,
		MaxRuntimeDecisions: inputs.Experiment.ScenarioMaxDecisions,
	}
	result, err := runOmnipaxosAgenticEpisode(
		ctx, inputs, riskJournal, scenarioJournal, budget, activateRisk, activateScenario,
	)
	if err != nil || result.Status != agenticEpisodeCompleted || result.Testing == nil ||
		!result.Metrics.CandidateAccepted || !result.Metrics.RiskReached ||
		result.Metrics.CorePSSSamples == 0 || result.Metrics.UniquePSSStates == 0 ||
		result.Metrics.OracleFindings != 0 || !result.Testing.Replay.Stable ||
		result.Work.Model != (controlexperiment.ModelWork{
			Calls: 2, InputTokens: 8, OutputTokens: 6, TotalTokens: 14,
		}) || result.Work.ScenarioSearch.TotalWorkUnits == 0 ||
		len(result.RiskProviderCalls) != 1 || len(result.ScenarioProviderCalls) != 1 ||
		providerCalls != 2 || keyActivations != 2 {
		t.Fatalf("bounded agentic episode incomplete: %#v calls=%d keys=%d err=%v",
			result, providerCalls, keyActivations, err)
	}
	artifactDirectory := t.TempDir()
	artifact, err := persistAgenticEpisodeArtifacts(
		artifactDirectory, "omnipaxos-v2", budget, result,
	)
	if err != nil {
		t.Fatal(err)
	}
	recoveredArtifact, terminal, err := recoverAgenticEpisodeArtifacts(
		artifactDirectory, omnipaxosAgenticEpisodeRecoveryBinding(),
	)
	if err != nil || !terminal || recoveredArtifact.Testing == nil ||
		recoveredArtifact.Summary.Metrics != artifact.Metrics ||
		recoveredArtifact.Testing.Bundle.Trace.Digest != result.Testing.Bundle.Trace.Digest ||
		recoveredArtifact.Testing.Risk.Digest != result.Testing.Risk.Digest ||
		!reflect.DeepEqual(recoveredArtifact.Testing.Oracle, result.Testing.Oracle) {
		t.Fatalf("terminal artifact recovery drifted: %#v terminal=%t err=%v",
			recoveredArtifact, terminal, err)
	}
	if _, terminal, err := recoverAgenticEpisodeArtifacts(
		t.TempDir(), omnipaxosAgenticEpisodeRecoveryBinding(),
	); err != nil || terminal {
		t.Fatalf("empty partial directory was not resumable: terminal=%t err=%v", terminal, err)
	}
	summaryBytes, err := os.ReadFile(filepath.Join(artifactDirectory, agenticEpisodeSummaryFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(summaryBytes, []byte("\"trace\"")) ||
		bytes.Contains(summaryBytes, []byte("\"execution_bundle\"")) {
		t.Fatalf("compact summary duplicated execution evidence: %s", summaryBytes)
	}
	tamperedDirectory := t.TempDir()
	if _, err := persistAgenticEpisodeArtifacts(
		tamperedDirectory, "omnipaxos-v2", budget, result,
	); err != nil {
		t.Fatal(err)
	}
	var tampered agenticEpisodeArtifact
	if err := readStrictJSONFile(
		filepath.Join(tamperedDirectory, agenticEpisodeSummaryFile), 512<<10, &tampered,
	); err != nil {
		t.Fatal(err)
	}
	tampered.Metrics.UniquePSSStates++
	encoded, err := json.MarshalIndent(tampered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(tamperedDirectory, agenticEpisodeSummaryFile), append(encoded, '\n'), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, terminal, err := recoverAgenticEpisodeArtifacts(
		tamperedDirectory, omnipaxosAgenticEpisodeRecoveryBinding(),
	); err == nil || terminal {
		t.Fatal("tampered terminal summary was accepted")
	}
	recoveredRisk, err := recoverStatelessAgentCallJournal(filepath.Join(directory, "risk"), client)
	if err != nil {
		t.Fatal(err)
	}
	recoveredScenario, err := recoverScenarioAgentCallJournal(filepath.Join(directory, "scenario"), client)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := runOmnipaxosAgenticEpisode(
		ctx, inputs, recoveredRisk, recoveredScenario, budget,
		func() error {
			keyActivations++
			return recoveredRisk.ActivateKey("unexpected-key")
		},
		func() error {
			keyActivations++
			return recoveredScenario.ActivateKey("unexpected-key")
		},
	)
	if err != nil || recovered.Status != result.Status || recovered.Metrics != result.Metrics ||
		recovered.Work.Model != result.Work.Model || recovered.Testing == nil ||
		recovered.Testing.Bundle.Trace.Digest != result.Testing.Bundle.Trace.Digest ||
		providerCalls != 2 || keyActivations != 2 {
		t.Fatalf("dual-Agent recovery repeated a model call or drifted: %#v calls=%d keys=%d err=%v",
			recovered, providerCalls, keyActivations, err)
	}
	limitedRisk, err := newStatelessAgentCallJournal(filepath.Join(directory, "limited-risk"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	limitedScenario, err := newScenarioAgentCallJournal(filepath.Join(directory, "limited-scenario"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	limitedBudget := budget
	limitedBudget.MaxObservedTokens = 13
	limited, err := runOmnipaxosAgenticEpisode(
		ctx, inputs, limitedRisk, limitedScenario, limitedBudget,
		func() error {
			keyActivations++
			return limitedRisk.ActivateKey("fixture-key")
		},
		func() error {
			keyActivations++
			return limitedScenario.ActivateKey("fixture-key")
		},
	)
	if err != nil || limited.Status != agenticEpisodeTokenStopped || limited.Testing != nil ||
		!limited.Metrics.CandidateAccepted || limited.Metrics.RiskReached ||
		limited.Work.Model.TotalTokens != 14 || providerCalls != 4 || keyActivations != 4 {
		t.Fatalf("token threshold did not stop before execution: %#v calls=%d keys=%d err=%v",
			limited, providerCalls, keyActivations, err)
	}
	runnerTarget, err := newOmnipaxosAgenticEpisodeTarget(inputs)
	if err != nil {
		t.Fatal(err)
	}
	runnerDirectory := filepath.Join(t.TempDir(), "agentic-episode")
	keyReads := 0
	prepareCalls := 0
	runner, err := runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory: runnerDirectory, AgentKeyFile: "fixture-key.txt",
		ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
		Recovery: omnipaxosAgenticEpisodeRecoveryBinding(),
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			prepareCalls++
			return agenticEpisodeComposition{
				Target: runnerTarget, Budget: budget, Client: client,
			}, nil
		},
	})
	if err != nil || runner.Summary.Status != agenticEpisodeCompleted || runner.Testing == nil ||
		providerCalls != 6 || keyReads != 2 || prepareCalls != 1 {
		t.Fatalf("directory runner incomplete: %#v calls=%d keys=%d prepare=%d err=%v",
			runner, providerCalls, keyReads, prepareCalls, err)
	}
	terminalRunner, err := runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory: runnerDirectory, Resume: true,
		Recovery: omnipaxosAgenticEpisodeRecoveryBinding(),
	})
	if err != nil || terminalRunner.Testing == nil || terminalRunner.Summary.Status != agenticEpisodeCompleted ||
		providerCalls != 6 || keyReads != 2 || prepareCalls != 1 {
		t.Fatalf("terminal runner accessed active inputs: %#v calls=%d keys=%d prepare=%d err=%v",
			terminalRunner, providerCalls, keyReads, prepareCalls, err)
	}
	var terminalOutput bytes.Buffer
	if err := run(ctx, []string{
		"-strategy", agenticEpisodeStrategy,
		"-target", "omnipaxos-v2",
		"-campaign-dir", runnerDirectory,
		"-campaign-resume",
	}, &terminalOutput); err != nil ||
		!bytes.Contains(terminalOutput.Bytes(), []byte("target=omnipaxos-v2 status=completed")) ||
		providerCalls != 6 || keyReads != 2 || prepareCalls != 1 {
		t.Fatalf("terminal CLI recovery failed or accessed active inputs: %q err=%v",
			terminalOutput.String(), err)
	}
}

func a4bScenarioViewFromPayload(
	t *testing.T,
	payload openRouterChatRequest,
) controlexperiment.ScenarioAgentView {
	t.Helper()
	var prompt struct {
		AgentView controlexperiment.ScenarioAgentView `json:"agent_view"`
	}
	if len(payload.Messages) != 2 {
		t.Fatal("Scenario prompt has unexpected message count")
	}
	marker := "Frozen input JSON:\n"
	index := bytes.Index([]byte(payload.Messages[1].Content), []byte(marker))
	if index < 0 {
		t.Fatal("Scenario prompt has no frozen input marker")
	}
	if err := json.Unmarshal([]byte(payload.Messages[1].Content)[index+len(marker):], &prompt); err != nil {
		t.Fatal(err)
	}
	return prompt.AgentView
}

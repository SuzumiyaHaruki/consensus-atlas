package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestA7cOpenRouterJournalDrivesQualifiedOmniPaxosEpisodeAndRecovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	workerPath := buildOmnipaxosScenarioWorker(t)
	client := fixtureOpenRouterIntentClient()
	providerCalls := 0
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		view := a4bScenarioViewFromRequest(t, request)
		var selected controlexperiment.FrontierActionRef
		for index, action := range view.Frontier.Actions {
			if action.Kind == control.ActionDropMessage &&
				view.Semantics.ActionHints[index].MessageClass == controlexperiment.ConsensusMessageReplication {
				selected = action
				break
			}
		}
		if selected.ActionID == "" {
			t.Fatalf("no in-flight OmniPaxos replication message can be dropped: %#v", view.Frontier.Actions)
		}
		content, err := json.Marshal(controlexperiment.ScenarioPlan{
			ID: "omnipaxos-a7c-plan", Steps: []controlexperiment.ScenarioStep{{
				ID: "drop-replication", Selector: controlexperiment.FrontierActionSelector{ActionID: selected.ActionID},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		response := a2b2OpenRouterResponse(t, providerCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "omnipaxos-a7c")
	keyReads := 0
	options := omnipaxosScenarioCalibrationRunOptions{
		Directory: directory, WorkerPath: workerPath,
		SemanticInputPath: "../../plans/agent/omnipaxos-message-loss-before-decision-v1.json",
		AgentKeyFile:      "fixture-key-source", Client: client,
		ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
	}
	summary, err := runOmnipaxosScenarioCalibration(ctx, options)
	if err != nil || summary.AgentStatus != controlexperiment.ScenarioAgentCompleted ||
		summary.Attempts != 1 || summary.Testing == nil || len(summary.ProviderCalls) != 1 ||
		summary.Testing.Outcome != scenarioTestingPassed ||
		summary.Testing.RiskStatus != semantic.RiskWitnessReached ||
		summary.Testing.CorePSSSamples == 0 || summary.Testing.UniqueCorePSSStates == 0 ||
		!summary.Testing.ReplayStable || summary.Testing.OracleViolations != 0 ||
		providerCalls != 1 || keyReads != 1 {
		t.Fatalf("OmniPaxos Agent run incomplete: %#v calls=%d reads=%d err=%v",
			summary, providerCalls, keyReads, err)
	}
	var persisted scenarioCalibrationSummary
	if err := readStrictJSONFile(filepath.Join(directory, "summary.json"), 1<<20, &persisted); err != nil ||
		!reflect.DeepEqual(persisted, summary) {
		t.Fatalf("OmniPaxos compact summary did not persist: %#v/%v", persisted, err)
	}
	options.Resume = true
	options.ReadKey = func(string) (string, error) {
		keyReads++
		return "unexpected-key-read", nil
	}
	recovered, err := runOmnipaxosScenarioCalibration(ctx, options)
	if err != nil || !reflect.DeepEqual(recovered, summary) || providerCalls != 1 || keyReads != 1 {
		t.Fatalf("OmniPaxos recovery drifted or contacted provider: %#v calls=%d reads=%d err=%v",
			recovered, providerCalls, keyReads, err)
	}
	t.Logf("attempts=%d pss=%d/%d risk=%s provider=%d replay=true recovery=true",
		summary.Attempts, summary.Testing.CorePSSSamples, summary.Testing.UniqueCorePSSStates,
		summary.Testing.RiskStatus, providerCalls)
}

func buildOmnipaxosScenarioWorker(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required for the OmniPaxos Scenario test")
	}
	manifest := filepath.Join("..", "..", "adapters", "omnipaxosv2", "worker", "Cargo.toml")
	command := exec.Command("cargo", "build", "--locked", "--quiet", "--manifest-path", manifest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build OmniPaxos worker: %v\n%s", err, output)
	}
	path, err := filepath.Abs(filepath.Join(
		"..", "..", "adapters", "omnipaxosv2", "worker", "target", "debug",
		"consensus-atlas-omnipaxos-worker",
	))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

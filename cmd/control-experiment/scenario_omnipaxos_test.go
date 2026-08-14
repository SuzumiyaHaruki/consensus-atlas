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
	inputs, err := prepareOmnipaxosScenario(
		ctx, workerPath,
		"../../plans/agent/omnipaxos-message-loss-before-decision-v1.json",
	)
	if err != nil {
		t.Fatal(err)
	}
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
	journalDirectory := filepath.Join(t.TempDir(), "omnipaxos-provider")
	journal, err := newScenarioAgentCallJournal(journalDirectory, client, "")
	if err != nil {
		t.Fatal(err)
	}
	keyReads := 0
	result, err := runOmnipaxosScenarioAgentEpisode(ctx, inputs, journal, func() error {
		keyReads++
		return journal.ActivateKey("fixture-key")
	})
	if err != nil || result.Agent.Status != controlexperiment.ScenarioAgentCompleted ||
		result.Agent.Execution == nil || result.Testing == nil || len(result.ProviderCalls) != 1 ||
		result.Testing.Outcome != scenarioTestingPassed ||
		result.Testing.Bundle.Trace.Digest != result.Agent.Execution.FinalTrace.Digest ||
		result.Testing.Risk.Status != semantic.RiskWitnessReached ||
		result.Testing.CorePSSSamples == 0 || result.Testing.UniqueCorePSSStates == 0 ||
		!result.Testing.Replay.Stable || len(result.Testing.Oracle.Violations) != 0 ||
		!reflect.DeepEqual(result.Testing.Oracle.Checked, []string{"trace-integrity", "agreement"}) ||
		providerCalls != 1 || keyReads != 1 {
		t.Fatalf("OmniPaxos Agent episode incomplete: %#v calls=%d reads=%d err=%v",
			result, providerCalls, keyReads, err)
	}
	recovered, err := recoverScenarioAgentCallJournal(journalDirectory, client)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := runOmnipaxosScenarioAgentEpisode(ctx, inputs, recovered, func() error {
		keyReads++
		return errStatelessAgentCallKeyRequired
	})
	if err != nil || replayed.Testing == nil ||
		replayed.Testing.Bundle.Digest != result.Testing.Bundle.Digest ||
		providerCalls != 1 || keyReads != 1 {
		t.Fatalf("OmniPaxos Agent recovery drifted or contacted provider: %#v calls=%d reads=%d err=%v",
			replayed, providerCalls, keyReads, err)
	}
	t.Logf("root=%d extension=%d pss=%d/%d provider=%d replay=true recovery=true",
		len(inputs.Root.Records), len(result.Agent.Execution.Steps), result.Testing.CorePSSSamples,
		result.Testing.UniqueCorePSSStates, providerCalls)
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

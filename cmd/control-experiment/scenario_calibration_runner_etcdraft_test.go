package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestA4cScenarioCalibrationWritesCompactSummaryAndResumesWithoutProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	client := fixtureOpenRouterIntentClient()
	providerCalls := 0
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		view := a4bScenarioViewFromRequest(t, request)
		if len(view.Frontier.Actions) == 0 ||
			view.Frontier.PrefixDecisions != 28 {
			t.Fatalf("scenario continuation frontier did not advance: %#v", view.Frontier)
		}
		content, err := json.Marshal(controlexperiment.ScenarioPlan{
			ID: "a4c-continuation-plan", Steps: []controlexperiment.ScenarioStep{{
				ID: "advance", Selector: controlexperiment.FrontierActionSelector{
					ActionID: view.Frontier.Actions[0].ActionID,
				},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		response := a2b2OpenRouterResponse(t, providerCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "a4c-scenario-calibration")
	keyReads := 0
	options := etcdraftScenarioCalibrationRunOptions{
		Directory: directory, CorpusPath: etcdraftTestRootCorpusPath,
		SemanticInputPath: etcdraftScenarioTestSemanticInput(t, 1, 4),
		AgentKeyFile:      "fixture-key-source", Client: client,
		ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
	}
	summary, err := runEtcdraftScenarioCalibration(ctx, options)
	if err != nil || summary.AgentStatus != controlexperiment.ScenarioAgentCompleted ||
		summary.Attempts != 1 || summary.FinalPlan == nil || summary.Testing == nil ||
		!summary.Testing.ReplayStable || summary.Testing.CorePSSSamples == 0 ||
		summary.Testing.OracleViolations != 0 || summary.ModelWork.Calls != 1 ||
		len(summary.ProviderCalls) != 1 || providerCalls != 1 || keyReads != 1 {
		t.Fatalf("A4c summary incomplete: %#v calls=%d reads=%d err=%v", summary, providerCalls, keyReads, err)
	}
	var persisted etcdraftScenarioCalibrationSummary
	if err := readStrictJSONFile(filepath.Join(directory, "summary.json"), 1<<20, &persisted); err != nil ||
		!reflect.DeepEqual(persisted, summary) {
		t.Fatalf("A4c compact summary did not persist exactly: %#v/%v", persisted, err)
	}
	options.Resume = true
	options.ReadKey = func(string) (string, error) {
		keyReads++
		return "unexpected-key-read", nil
	}
	recovered, err := runEtcdraftScenarioCalibration(ctx, options)
	if err != nil || !reflect.DeepEqual(recovered, summary) || providerCalls != 1 || keyReads != 1 {
		t.Fatalf("A4c recovery repeated provider/key access or drifted: %#v calls=%d reads=%d err=%v",
			recovered, providerCalls, keyReads, err)
	}
}

func TestA4cScenarioCalibrationPersistsTerminalProviderFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	client := fixtureOpenRouterIntentClient()
	providerCalls := 0
	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		providerCalls++
		return nil, errors.New("fixture transport failure")
	})
	directory := filepath.Join(t.TempDir(), "a4c-provider-failure")
	keyReads := 0
	options := etcdraftScenarioCalibrationRunOptions{
		Directory: directory, CorpusPath: etcdraftTestRootCorpusPath,
		SemanticInputPath: etcdraftScenarioTestSemanticInput(t, 2, 4),
		AgentKeyFile:      "fixture-key-source", Client: client,
		ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
	}
	summary, err := runEtcdraftScenarioCalibration(ctx, options)
	if err == nil || summary.AgentStatus != etcdraftScenarioCalibrationProviderFailed ||
		summary.Attempts != 1 || summary.ModelWork.Calls != 1 ||
		len(summary.ProviderCalls) != 1 || providerCalls != 3 || keyReads != 1 ||
		summary.ProviderCalls[0].TransportAttempts != 3 {
		t.Fatalf("terminal provider failure was not summarized: %#v calls=%d reads=%d err=%v",
			summary, providerCalls, keyReads, err)
	}
	options.Resume = true
	recovered, err := runEtcdraftScenarioCalibration(ctx, options)
	if err == nil || !reflect.DeepEqual(recovered, summary) || providerCalls != 3 || keyReads != 1 {
		t.Fatalf("terminal provider failure was retried or drifted: %#v calls=%d reads=%d err=%v",
			recovered, providerCalls, keyReads, err)
	}
}

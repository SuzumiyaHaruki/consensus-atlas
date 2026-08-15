package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestA8PairedScenarioComparesOneEpisodeOnSameExecutionSubstrate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	client := fixtureOpenRouterIntentClient()
	providerCalls := 0
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		view := a4bScenarioViewFromRequest(t, request)
		wanted := control.ActionCrash
		if view.Prior != nil {
			wanted = control.ActionRestart
		}
		var selected controlexperiment.FrontierActionRef
		for _, action := range view.Frontier.Actions {
			if action.Kind == wanted && action.Node.Node == "n1" {
				selected = action
				break
			}
		}
		if selected.ActionID == "" {
			t.Fatalf("paired fixture cannot select %s: %#v", wanted, view.Frontier.Actions)
		}
		content, err := json.Marshal(controlexperiment.ScenarioPlan{
			ID: "a8-paired-agent-plan", Steps: []controlexperiment.ScenarioStep{{
				ID: "advance", Selector: controlexperiment.FrontierActionSelector{ActionID: selected.ActionID},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		response := a2b2OpenRouterResponse(t, providerCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "a8-paired")
	keyReads := 0
	options := etcdraftA8PairedScenarioOptions{
		Directory:         directory,
		SemanticInputPath: etcdraftTestSemanticInputPath, AgentKeyFile: "fixture-key-source",
		Client: client, ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
	}
	summary, err := runEtcdraftA8PairedScenario(ctx, options)
	if err != nil || summary.Classification != etcdraftA8PairClass || !summary.SameTrace ||
		summary.SemanticMode != string(controlexperiment.ScenarioSemanticExposureFull) ||
		summary.RootMode != etcdraftA8FreshRootMode || len(summary.SourceDigest) != 64 ||
		len(summary.CorpusDigest) != 64 || summary.RootRule != "first-invoke-crash-restart-actions-v1" ||
		len(summary.RootDigest) != 64 || summary.RootDecisions <= 0 ||
		summary.Deterministic.Planner != scenarioPlannerDeterministic ||
		summary.Agent.Planner != scenarioPlannerAgent ||
		summary.Deterministic.PrimaryWorkUnits != summary.Agent.PrimaryWorkUnits ||
		summary.Deterministic.ReplayWorkUnits != summary.Agent.ReplayWorkUnits ||
		summary.Deterministic.ModelCalls != 0 || summary.Deterministic.ModelTokens != 0 ||
		summary.Agent.ModelCalls != 2 || summary.Agent.ModelTokens == 0 ||
		summary.Deterministic.UniqueCorePSSStates != summary.Agent.UniqueCorePSSStates ||
		summary.Deterministic.RiskStatus != semantic.RiskWitnessReached ||
		summary.Agent.RiskStatus != semantic.RiskWitnessReached ||
		summary.Deterministic.OracleViolations != 0 || summary.Agent.OracleViolations != 0 ||
		providerCalls != 2 || keyReads != 2 {
		t.Fatalf("A8 paired result is not comparable: %#v calls=%d reads=%d err=%v",
			summary, providerCalls, keyReads, err)
	}
	options.Resume = true
	recovered, err := runEtcdraftA8PairedScenario(ctx, options)
	if err != nil || !reflect.DeepEqual(recovered, summary) || providerCalls != 2 || keyReads != 2 {
		t.Fatalf("A8 paired recovery repeated provider work or drifted: %#v calls=%d reads=%d err=%v",
			recovered, providerCalls, keyReads, err)
	}
}

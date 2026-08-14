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

func TestA6bScenarioSessionAggregatesTwoEpisodesKeepsFeedbackMechanicalAndResumes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	client := fixtureOpenRouterIntentClient()
	providerCalls := 0
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		if providerCalls == 3 && (bytes.Contains(raw, []byte("core_pss_state_keys")) ||
			bytes.Contains(raw, []byte("oracle_violations")) ||
			bytes.Contains(raw, []byte("bundle_digest"))) {
			t.Fatalf("terminal testing facts leaked into second episode prompt: %s", raw)
		}
		view := a4bScenarioViewFromRequest(t, request)
		if view.Semantics.Mode != controlexperiment.ScenarioSemanticExposureMasked ||
			view.Semantics.Validate(view.Frontier) != nil {
			t.Fatalf("session semantic override did not reach the Agent view: %#v", view.Semantics)
		}
		if providerCalls == 1 && view.Prior != nil {
			t.Fatal("first session episode received invented feedback")
		}
		if providerCalls > 1 && (view.Prior == nil ||
			view.Prior.Outcome != controlexperiment.ScenarioAgentCompleted ||
			len(view.Prior.Steps) != 1) {
			t.Fatalf("continued planning did not receive prior mechanical feedback: %#v", view.Prior)
		}
		wantDecisions := 28
		if providerCalls == 2 || providerCalls == 4 {
			wantDecisions = 29
		}
		if len(view.Frontier.Actions) == 0 || view.Frontier.PrefixDecisions != wantDecisions {
			t.Fatalf("session continuation/reset frontier mismatch: %#v", view.Frontier)
		}
		content, err := json.Marshal(controlexperiment.ScenarioPlan{
			ID: "a6a-session-plan", Steps: []controlexperiment.ScenarioStep{{
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
	directory := filepath.Join(t.TempDir(), "a6a-session")
	keyReads := 0
	options := etcdraftScenarioSessionOptions{
		Directory: directory, CorpusPath: etcdraftTestRootCorpusPath,
		SemanticInputPath: etcdraftScenarioTestSemanticInput(t, 2, 4),
		AgentKeyFile:      "fixture-key-source", Client: client,
		SemanticExposure: controlexperiment.ScenarioSemanticExposureMasked,
		ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
	}
	summary, err := runEtcdraftScenarioSession(ctx, options)
	if err != nil || summary.SemanticExposure != controlexperiment.ScenarioSemanticExposureMasked ||
		summary.Campaign.Status != controlexperiment.CampaignSummaryStatusStopped ||
		summary.Campaign.StopReason != controlexperiment.CampaignStopAttemptLimit ||
		summary.Campaign.Sequence != 2 || len(summary.Campaign.Attempts) != 2 ||
		summary.Campaign.Totals.Model.Calls != 4 ||
		summary.Campaign.Totals.Primary.WorkUnits == 0 || summary.Campaign.Totals.Replay.WorkUnits == 0 ||
		summary.AgentCompletedEpisodes != 2 || summary.AgentStoppedEpisodes != 0 ||
		summary.TestingEpisodes != 2 || summary.ReplayStableEpisodes != 2 ||
		summary.CorePSSSamples == 0 || summary.UniqueCorePSSStates == 0 ||
		summary.UniqueCorePSSStates != len(summary.CorePSSStateKeys) ||
		summary.BestRiskEpisode != 1 || summary.BestRiskStatus == "" || summary.OracleViolations != 0 ||
		providerCalls != 4 || keyReads != 4 {
		t.Fatalf("A6a session incomplete: %#v calls=%d reads=%d err=%v",
			summary, providerCalls, keyReads, err)
	}
	for _, attempt := range summary.Campaign.Attempts {
		if attempt.Record.Outcome != controlexperiment.CampaignAttemptCompleted {
			t.Fatalf("session episode did not commit: %#v", attempt)
		}
	}
	config, err := controlexperiment.NewCampaignConfig(
		summary.Campaign.CampaignID, summary.Campaign.TargetID,
		summary.Campaign.TargetIdentityDigest, summary.Campaign.ExperimentSpecDigest,
		summary.Campaign.Budget, summary.Campaign.WallClockCeilingMillis,
	)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := controlexperiment.RecoverCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := stored.ReadAttemptArtifact(1)
	if err != nil {
		t.Fatal(err)
	}
	var first etcdraftScenarioSessionEpisodeArtifact
	if err := json.Unmarshal(firstBytes, &first); err != nil || first.validate() != nil ||
		first.Episode.Testing == nil ||
		summary.CorePSSSamples != 2*first.Episode.Testing.CorePSSSamples ||
		!reflect.DeepEqual(summary.CorePSSStateKeys, first.CorePSSStateKeys) {
		t.Fatalf("session PSS union was summed or cannot be audited: %#v/%v", first, err)
	}
	options.Resume = true
	options.ReadKey = func(string) (string, error) {
		keyReads++
		return "unexpected-key-read", nil
	}
	recovered, err := runEtcdraftScenarioSession(ctx, options)
	if err != nil || !reflect.DeepEqual(recovered, summary) || providerCalls != 4 || keyReads != 4 {
		t.Fatalf("A6a recovery repeated provider/key access or drifted: %#v calls=%d reads=%d err=%v",
			recovered, providerCalls, keyReads, err)
	}
}

func TestA6eRScenarioSessionChargesDurableCallsWhenEpisodeFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	client := fixtureOpenRouterIntentClient()
	transportAttempts := 0
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		transportAttempts++
		if transportAttempts > 1 {
			return nil, errors.New("fixture transport failure after one completed plan")
		}
		view := a4bScenarioViewFromRequest(t, request)
		content, err := json.Marshal(controlexperiment.ScenarioPlan{
			ID: "a6er-failure-cost-plan", Steps: []controlexperiment.ScenarioStep{{
				ID: "advance", Selector: controlexperiment.FrontierActionSelector{
					ActionID: view.Frontier.Actions[0].ActionID,
				},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		response := a2b2OpenRouterResponse(t, 1, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "a6er-session-failure-cost")
	keyReads := 0
	options := etcdraftScenarioSessionOptions{
		Directory: directory, CorpusPath: etcdraftTestRootCorpusPath,
		SemanticInputPath: etcdraftScenarioTestSemanticInput(t, 2, 4),
		AgentKeyFile:      "fixture-key-source", Client: client,
		ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
	}
	summary, err := runEtcdraftScenarioSession(ctx, options)
	if err == nil || summary.Campaign.Status != controlexperiment.CampaignSummaryStatusFailed ||
		summary.Campaign.Sequence != 0 || summary.Campaign.Failure == nil ||
		summary.Campaign.Failure.Code != controlexperiment.CampaignFailureProvider ||
		summary.Campaign.Failure.Work.Model.Calls != 2 ||
		summary.Campaign.Failure.Work.Model.TotalTokens == 0 ||
		summary.Campaign.Totals.Model != summary.Campaign.Failure.Work.Model ||
		transportAttempts != 4 || keyReads != 2 {
		t.Fatalf("durable failed calls were not charged: %#v attempts=%d reads=%d err=%v",
			summary, transportAttempts, keyReads, err)
	}
	options.Resume = true
	options.ReadKey = func(string) (string, error) {
		keyReads++
		return "unexpected-key-read", nil
	}
	recovered, err := runEtcdraftScenarioSession(ctx, options)
	if err == nil || !reflect.DeepEqual(recovered, summary) || transportAttempts != 4 || keyReads != 2 {
		t.Fatalf("failed-cost recovery repeated provider/key access or drifted: %#v attempts=%d reads=%d err=%v",
			recovered, transportAttempts, keyReads, err)
	}
}

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

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
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
		if providerCalls%2 == 1 && view.Prior != nil {
			t.Fatalf("reset session episode received continuation feedback: %#v", view.Prior)
		}
		if providerCalls%2 == 0 && (view.Prior == nil ||
			view.Prior.Outcome != controlexperiment.ScenarioAgentCompleted ||
			len(view.Prior.Steps) != 1 || len(view.Prior.NaturalProgress) != 0 ||
			view.Prior.NaturalProgressStop == "") {
			t.Fatalf("same-episode continuation did not receive mechanical feedback: %#v", view.Prior)
		}
		if len(view.Frontier.Actions) == 0 ||
			(providerCalls%2 == 1 && view.Frontier.PrefixDecisions != 28) ||
			(providerCalls%2 == 0 && view.Frontier.PrefixDecisions <= 28) {
			t.Fatalf("session continuation/reset frontier mismatch: %#v", view.Frontier)
		}
		wantKind := control.ActionCrash
		wantNode := control.NodeID("n1")
		if providerCalls%2 == 0 {
			wantKind = control.ActionRestart
		}
		var selected controlexperiment.FrontierActionRef
		for _, action := range view.Frontier.Actions {
			if action.Kind == wantKind && action.Node.Node == wantNode {
				selected = action
				break
			}
		}
		if selected.ActionID == "" {
			t.Fatalf("strategic session action %s/%s unavailable: %#v", wantKind, wantNode, view.Frontier.Actions)
		}
		content, err := json.Marshal(controlexperiment.ScenarioPlan{
			ID: "a6a-session-plan", Steps: []controlexperiment.ScenarioStep{{
				ID: "advance", Selector: controlexperiment.FrontierActionSelector{
					ActionID: selected.ActionID,
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
		SemanticInputPath: etcdraftScenarioTestSemanticInput(t, 2, 32),
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
	if err := json.Unmarshal(firstBytes, &first); err != nil ||
		first.validate(etcdraftScenarioCalibrationClass, validateEtcdraftScenarioTesting) != nil ||
		first.Episode.Testing == nil || first.Testing == nil || first.Testing.Bundle.Validate() != nil ||
		summary.CorePSSSamples != 2*first.Episode.Testing.CorePSSSamples {
		t.Fatalf("session PSS union was summed or cannot be audited: %#v/%v", first, err)
	}
	firstKeys, err := scenarioBundleStateKeys(&first.Testing.Bundle)
	if err != nil || !reflect.DeepEqual(summary.CorePSSStateKeys, firstKeys) {
		t.Fatalf("session PSS union cannot be derived from persisted Bundle: %#v/%v", firstKeys, err)
	}
	tampered := first
	testing := *first.Testing
	bundle := testing.Bundle
	bundle.Digest = ""
	testing.Bundle = bundle
	tampered.Testing = &testing
	if tampered.validate(etcdraftScenarioCalibrationClass, validateEtcdraftScenarioTesting) == nil {
		t.Fatal("session artifact accepted a tampered persisted Bundle")
	}
	tampered = first
	testing = *first.Testing
	testing.Risk.Digest = ""
	tampered.Testing = &testing
	if tampered.validate(etcdraftScenarioCalibrationClass, validateEtcdraftScenarioTesting) == nil {
		t.Fatal("session artifact accepted a tampered persisted Risk result")
	}
	tampered = first
	testing = *first.Testing
	testing.Oracle.Checked = append([]string(nil), testing.Oracle.Checked...)
	testing.Oracle.Checked[0] = "invented-monitor"
	tampered.Testing = &testing
	if tampered.validate(etcdraftScenarioCalibrationClass, validateEtcdraftScenarioTesting) == nil {
		t.Fatal("session artifact accepted a tampered persisted Oracle result")
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
		var selected controlexperiment.FrontierActionRef
		for _, action := range view.Frontier.Actions {
			if action.Kind == control.ActionCrash && action.Node.Node == "n1" {
				selected = action
				break
			}
		}
		if selected.ActionID == "" {
			t.Fatalf("fixture continuation action unavailable: %#v", view.Frontier.Actions)
		}
		content, err := json.Marshal(controlexperiment.ScenarioPlan{
			ID: "a6er-failure-cost-plan", Steps: []controlexperiment.ScenarioStep{{
				ID: "advance", Selector: controlexperiment.FrontierActionSelector{
					ActionID: selected.ActionID,
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
	committedModelCalls := 0
	committedModelTokens := 0
	for _, attempt := range summary.Campaign.Attempts {
		committedModelCalls += attempt.Record.Work.Model.Calls
		committedModelTokens += attempt.Record.Work.Model.TotalTokens
	}
	if err == nil || summary.Campaign.Status != controlexperiment.CampaignSummaryStatusFailed ||
		summary.Campaign.Failure == nil ||
		summary.Campaign.Failure.Code != controlexperiment.CampaignFailureProvider ||
		summary.Campaign.Failure.Work.Model.Calls <= 0 ||
		summary.Campaign.Totals.Model.Calls !=
			committedModelCalls+summary.Campaign.Failure.Work.Model.Calls ||
		summary.Campaign.Totals.Model.TotalTokens !=
			committedModelTokens+summary.Campaign.Failure.Work.Model.TotalTokens ||
		transportAttempts != summary.Campaign.Totals.Model.Calls || keyReads != 2 {
		t.Fatalf("durable failed calls were not charged: %#v attempts=%d reads=%d err=%v",
			summary, transportAttempts, keyReads, err)
	}
	options.Resume = true
	options.ReadKey = func(string) (string, error) {
		keyReads++
		return "unexpected-key-read", nil
	}
	recovered, err := runEtcdraftScenarioSession(ctx, options)
	if err == nil || !reflect.DeepEqual(recovered, summary) ||
		transportAttempts != summary.Campaign.Totals.Model.Calls || keyReads != 2 {
		t.Fatalf("failed-cost recovery repeated provider/key access or drifted: %#v attempts=%d reads=%d err=%v",
			recovered, transportAttempts, keyReads, err)
	}
}

func TestScenarioSessionChargesChildVerificationAsReplay(t *testing.T) {
	result := scenarioAgentEpisodeResult{
		FrontierWork: controlexperiment.PhaseWork{SchedulerDecisions: 2},
		Agent: controlexperiment.ScenarioAgentResult{ExecutionWork: controlexperiment.StatelessDFSWork{
			FrontierReconstruction: controlexperiment.PhaseWork{SchedulerDecisions: 3},
			ChildMaterialization:   controlexperiment.PhaseWork{SchedulerDecisions: 4},
			ChildVerification:      controlexperiment.PhaseWork{SchedulerDecisions: 5},
		}},
	}
	work := scenarioSessionWork(result, nil)
	if work.Primary.SchedulerDecisions != 9 || work.Primary.WorkUnits != 9 ||
		work.Replay.SchedulerDecisions != 5 || work.Replay.WorkUnits != 5 {
		t.Fatalf("scenario verification work classification drifted: %#v", work)
	}
}

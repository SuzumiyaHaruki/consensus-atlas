package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestScenarioAgentRepairsNoMatchAndProducesQualifiedTestingResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	client := fixtureOpenRouterIntentClient()
	providerCalls := 0
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		view := a4bScenarioViewFromRequest(t, request)
		if view.Semantics.Mode != controlexperiment.ScenarioSemanticExposureFull ||
			view.Semantics.Validate(view.Frontier) != nil ||
			len(view.Semantics.ActionHints) != len(view.Frontier.Actions) {
			t.Fatalf("scenario prompt lacks trusted full semantics: %#v", view.Semantics)
		}
		semanticBytes, err := json.Marshal(view.Semantics)
		if err != nil {
			t.Fatal(err)
		}
		semanticText := string(semanticBytes)
		for _, forbidden := range []string{"StateLeader", "MsgApp", "payload", "oracle", "verdict"} {
			if strings.Contains(semanticText, forbidden) {
				t.Fatalf("scenario semantics exposed forbidden detail %q: %s", forbidden, semanticText)
			}
		}
		var plan controlexperiment.ScenarioPlan
		if providerCalls == 1 {
			if view.Prior != nil {
				t.Fatal("initial scenario call received invented prior feedback")
			}
			plan = controlexperiment.ScenarioPlan{ID: "etcdraft-scenario-first", Steps: []controlexperiment.ScenarioStep{{
				ID: "restart-running", Selector: controlexperiment.FrontierActionSelector{Kind: control.ActionRestart},
			}}}
		} else {
			if view.Prior == nil || view.Prior.ReasonCode != controlexperiment.ScenarioReasonNoMatch ||
				view.Prior.PreviousPlan == nil || view.Prior.FailedStep == nil ||
				view.Prior.FailedStep.ID != "restart-running" || len(view.Prior.Steps) != 1 ||
				len(view.Prior.Steps[0].Available) != 0 {
				t.Fatalf("repaired call did not receive mechanical no-match feedback: %#v", view.Prior)
			}
			var crash controlexperiment.FrontierActionRef
			for _, action := range view.Frontier.Actions {
				if action.Kind == control.ActionCrash {
					crash = action
					break
				}
			}
			if crash.ActionID == "" {
				t.Fatalf("scenario root has no crash action: %#v", view.Frontier.Actions)
			}
			plan = controlexperiment.ScenarioPlan{ID: "etcdraft-scenario-repaired", Steps: []controlexperiment.ScenarioStep{
				{ID: "crash-current", Selector: controlexperiment.FrontierActionSelector{ActionID: crash.ActionID}},
				{ID: "restart-node", Selector: controlexperiment.FrontierActionSelector{
					Kind: control.ActionRestart, Node: crash.Node.Node,
				}},
			}}
		}
		content, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		response := a2b2OpenRouterResponse(t, providerCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, etcdraftTestRootCorpusPath, etcdraftScenarioTestSemanticInput(t, 2, 4), client,
	)
	if err != nil {
		t.Fatal(err)
	}
	journalDirectory := filepath.Join(t.TempDir(), "scenario-provider")
	journal, err := newScenarioAgentCallJournal(journalDirectory, client, "fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	result, err := runEtcdraftScenarioAgentEpisode(ctx, inputs, journal, 2, 2, 4, func() error {
		return journal.ActivateKey("fixture-key")
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent.Status != controlexperiment.ScenarioAgentCompleted ||
		len(result.Agent.Attempts) != 2 || result.Agent.Execution == nil || result.Testing == nil ||
		result.Agent.Attempts[0].Feedback.ReasonCode != controlexperiment.ScenarioReasonNoMatch ||
		result.Agent.Attempts[1].Feedback.Outcome != controlexperiment.ScenarioStatusCompleted ||
		result.Testing.Bundle.Trace.Digest != result.Agent.Execution.FinalTrace.Digest ||
		!result.Testing.Replay.Stable || result.Testing.CorePSSSamples == 0 ||
		len(result.Testing.Oracle.Violations) != 0 || len(result.ProviderCalls) != 2 ||
		result.ProviderCalls[0].Status != controlexperiment.StatelessAgentCallContentReady ||
		result.ProviderCalls[1].Status != controlexperiment.StatelessAgentCallContentReady ||
		result.Agent.ModelWork.Calls != 2 || providerCalls != 2 {
		t.Fatalf("scenario repair did not close qualified testing: %#v provider_calls=%d", result, providerCalls)
	}
	recovered, err := recoverScenarioAgentCallJournal(journalDirectory, client)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := runEtcdraftScenarioAgentEpisode(ctx, inputs, recovered, 2, 2, 4, func() error {
		return errStatelessAgentCallKeyRequired
	})
	if err != nil || replayed.Testing == nil ||
		replayed.Testing.Bundle.Digest != result.Testing.Bundle.Digest || providerCalls != 2 {
		t.Fatalf("recovered scenario calls changed execution or contacted provider: %#v calls=%d err=%v",
			replayed, providerCalls, err)
	}
}

func TestDeterministicScenarioUsesAgentExecutionSubstrateWithoutModelWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, etcdraftTestRootCorpusPath, etcdraftScenarioTestSemanticInput(t, 2, 32),
		fixtureOpenRouterIntentClient(),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runEtcdraftDeterministicScenarioEpisode(ctx, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent.Status != controlexperiment.ScenarioAgentCompleted ||
		len(result.Agent.Attempts) != 2 || result.Agent.Execution == nil || result.Testing == nil ||
		len(result.ProviderCalls) != 0 || result.Agent.ModelWork != (controlexperiment.ModelWork{}) ||
		len(result.Agent.Attempts[0].Feedback.Steps) != 1 ||
		result.Agent.Attempts[0].Feedback.Steps[0].Choice == nil ||
		result.Agent.Attempts[0].Feedback.Steps[0].Choice.Action.Kind != control.ActionCrash ||
		len(result.Agent.Attempts[0].Feedback.NaturalProgress) == 0 ||
		len(result.Agent.Attempts[1].Feedback.Steps) != 1 ||
		result.Agent.Attempts[1].Feedback.Steps[0].Choice == nil ||
		result.Agent.Attempts[1].Feedback.Steps[0].Choice.Action.Kind != control.ActionRestart ||
		result.Agent.Attempts[1].Feedback.NaturalProgressStop == "" ||
		result.Testing.Risk.Status != semantic.RiskWitnessReached || !result.Testing.Replay.Stable ||
		len(result.Testing.Oracle.Violations) != 0 ||
		result.Testing.Bundle.Trace.Digest != result.Agent.Execution.FinalTrace.Digest {
		t.Fatalf("deterministic Scenario did not produce comparable qualified evidence: %#v", result)
	}
}

func TestScenarioClientTerminalReturnsControlToPlanner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, etcdraftTestRootCorpusPath, etcdraftScenarioTestSemanticInput(t, 2, 32),
		fixtureOpenRouterIntentClient(),
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := runScenarioEpisodeCore(
		ctx, etcdraftScenarioCoreInputs(inputs), 2, 1, 32,
		func(_ context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			calls++
			wanted := control.ActionDeliverMessage
			if calls == 2 {
				if view.Prior == nil ||
					view.Prior.NaturalProgressStop != controlexperiment.ScenarioProgressClientTerminal {
					t.Fatalf("client terminal was not exposed as a planning checkpoint: %#v", view.Prior)
				}
				wanted = control.ActionCrash
			}
			var selected controlexperiment.FrontierActionRef
			for _, action := range view.Frontier.Actions {
				if action.Kind == wanted {
					selected = action
					break
				}
			}
			if selected.ActionID == "" {
				t.Fatalf("A9e2 checkpoint action %s unavailable: %#v", wanted, view.Frontier.Actions)
			}
			encoded, marshalErr := json.Marshal(controlexperiment.ScenarioPlan{
				ID: "a9e2-client-terminal-plan",
				Steps: []controlexperiment.ScenarioStep{{
					ID: "a9e2-intervention",
					Selector: controlexperiment.FrontierActionSelector{
						ActionID: selected.ActionID,
					},
				}},
			})
			return encoded, controlexperiment.ModelWork{}, marshalErr
		},
	)
	if err != nil || calls != 2 || result.Agent.Execution == nil || len(result.Agent.Attempts) != 2 ||
		result.Agent.Attempts[0].Feedback.NaturalProgressStop !=
			controlexperiment.ScenarioProgressClientTerminal ||
		len(result.Agent.Attempts[1].Feedback.Steps) != 1 ||
		result.Agent.Attempts[1].Feedback.Steps[0].Choice == nil ||
		result.Agent.Attempts[1].Feedback.Steps[0].Choice.Action.Kind != control.ActionCrash {
		t.Fatalf("client terminal ended the investigation instead of returning control: %#v calls=%d err=%v",
			result, calls, err)
	}
}

func TestCompleteScenarioIntentEndsAtClientTerminalWithoutReplanning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, etcdraftTestRootCorpusPath, etcdraftScenarioTestSemanticInput(t, 2, 32),
		fixtureOpenRouterIntentClient(),
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := runScenarioEpisodeCore(
		ctx, etcdraftScenarioCoreInputs(inputs), 2, 2, 32,
		func(_ context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			calls++
			var selected controlexperiment.FrontierActionRef
			for _, action := range view.Frontier.Actions {
				if action.Kind == control.ActionDeliverMessage {
					selected = action
					break
				}
			}
			if selected.ActionID == "" {
				t.Fatalf("complete intent root has no deliver action: %#v", view.Frontier.Actions)
			}
			encoded, marshalErr := json.Marshal(controlexperiment.ScenarioPlan{
				ID: "a9e4-complete-intent",
				Steps: []controlexperiment.ScenarioStep{{
					ID: "deliver-current", Selector: controlexperiment.FrontierActionSelector{
						ActionID: selected.ActionID,
					},
				}},
			})
			return encoded, controlexperiment.ModelWork{}, marshalErr
		},
	)
	if err != nil || calls != 1 || result.Agent.Status != controlexperiment.ScenarioAgentCompleted ||
		len(result.Agent.Attempts) != 1 || result.Agent.Execution == nil ||
		result.Agent.Attempts[0].Feedback.NaturalProgressStop !=
			controlexperiment.ScenarioProgressClientTerminal {
		t.Fatalf("complete intent replanned after terminal: status=%s attempts=%d calls=%d err=%v",
			result.Agent.Status, len(result.Agent.Attempts), calls, err)
	}
}

func a4bScenarioViewFromRequest(
	t *testing.T,
	request *http.Request,
) controlexperiment.ScenarioAgentView {
	t.Helper()
	if request.Header.Get("Authorization") != "Bearer fixture-key" {
		t.Fatal("scenario provider received an unexpected credential boundary")
	}
	var payload openRouterChatRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || len(payload.Messages) != 2 {
		t.Fatalf("invalid scenario provider payload: %#v/%v", payload, err)
	}
	for _, required := range []string{"Never copy an ActionID from prior_feedback"} {
		if !strings.Contains(payload.Messages[0].Content, required) {
			t.Fatalf("scenario system prompt lacks future-selector rule %q: %s", required, payload.Messages[0].Content)
		}
	}
	const marker = "Frozen input JSON:\n"
	index := strings.LastIndex(payload.Messages[1].Content, marker)
	if index < 0 {
		t.Fatal("scenario prompt did not contain frozen input")
	}
	var prompt struct {
		PromptVersion  string                              `json:"prompt_version"`
		SelectorFields []string                            `json:"selector_fields"`
		AgentView      controlexperiment.ScenarioAgentView `json:"agent_view"`
	}
	if err := json.Unmarshal([]byte(payload.Messages[1].Content[index+len(marker):]), &prompt); err != nil ||
		prompt.PromptVersion != scenarioAgentPromptVersion || len(prompt.SelectorFields) != 10 ||
		prompt.SelectorFields[0] != "action_id" || prompt.SelectorFields[1] != "kind" {
		t.Fatalf("scenario prompt input cannot be decoded: %#v/%v", prompt, err)
	}
	wantRule := "Return exactly one step using the exact action_id"
	if prompt.AgentView.MaxSteps > 1 {
		wantRule = "For every step after the first, omit action_id"
	}
	if !strings.Contains(payload.Messages[0].Content, wantRule) {
		t.Fatalf("scenario system prompt lacks max-step rule %q: %s", wantRule, payload.Messages[0].Content)
	}
	return prompt.AgentView
}

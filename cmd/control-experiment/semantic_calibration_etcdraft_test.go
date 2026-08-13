package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const etcdraftTestSemanticInputPath = "../../plans/agent/etcdraft-leader-change-inflight-v1.json"

func TestA2b3EtcdraftPublicSemanticCalibrationInputsAreValidAndSourceBound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	client := fixtureOpenRouterIntentClient()
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, etcdraftTestRootCorpusPath, etcdraftTestSemanticInputPath, client,
	)
	if err != nil {
		t.Fatal(err)
	}
	if inputs.spec.ValidateInputs(
		inputs.campaign, inputs.root, inputs.frontier, inputs.riskSpec,
		inputs.knowledge, inputs.hypothesis, inputs.experiment, inputs.searchSpec,
	) != nil || inputs.spec.RootID != "invoked" || inputs.spec.RootDecisions != 28 ||
		inputs.spec.RootFrontierActions < 2 || inputs.spec.ExplorerBudget.MaxCalls != 2 ||
		inputs.spec.Transport.Provider != openRouterProvider ||
		inputs.spec.Transport.Model != openRouterFixtureModel || inputs.spec.Transport.MaxRetries != 2 ||
		inputs.client.MaxOutputTokens != inputs.experiment.ModelMaxOutputTokens ||
		inputs.searchSpec.Runtime != inputs.experiment.Runtime || inputs.searchSpec.FaultEnvelope == nil ||
		*inputs.searchSpec.FaultEnvelope != inputs.experiment.FaultEnvelope ||
		inputs.searchSpec.MaxDepth != inputs.experiment.SearchMaxDepth ||
		inputs.searchSpec.MaxWorkItems != inputs.experiment.SearchMaxWorkItems ||
		inputs.searchSpec.MaxWorkUnits != inputs.experiment.SearchMaxWorkUnits {
		t.Fatalf("public semantic calibration inputs drifted: %#v", inputs.spec)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	baseline, err := controlexperiment.ExploreBoundedSemanticBestFirst(
		ctx, inputs.searchSpec, inputs.root, inputs.riskSpec, factory,
		etcdraftSemanticPrefixProjector{}, controlexperiment.NewDeterministicSemanticBestFirstGuidance(),
	)
	if err != nil || baseline.ValidateSources(
		ctx, inputs.root, inputs.riskSpec, factory,
		etcdraftSemanticPrefixProjector{}, controlexperiment.NewDeterministicSemanticBestFirstGuidance(),
	) != nil || len(baseline.Search.Items) != inputs.searchSpec.MaxWorkItems ||
		baseline.Search.StopReason != controlexperiment.StatelessDFSStopItems || len(baseline.ExpansionOrder) != 1 {
		t.Fatalf("etcd/raft semantic baseline was not source-bound: %#v/%v", baseline, err)
	}
	var crash controlexperiment.FrontierActionRef
	for _, action := range inputs.frontier.Actions {
		if action.Kind == control.ActionCrash {
			crash = action
			break
		}
	}
	if crash.ActionID == "" {
		t.Fatalf("etcd/raft semantic root has no admissible crash: %#v", inputs.frontier.Actions)
	}
	projector := etcdraftSemanticPrefixProjector{}
	rootRisk, err := projector.Project("etcdraft-a4-root-risk", inputs.riskSpec, inputs.root)
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := controlexperiment.ExecuteBoundedScenarioPlan(
		ctx, "etcdraft-a4-crash-restart",
		controlexperiment.ScenarioPlan{ID: "etcdraft-a4-lifecycle", Steps: []controlexperiment.ScenarioStep{
			{ID: "crash-current", Selector: controlexperiment.FrontierActionSelector{ActionID: crash.ActionID}},
			{ID: "restart-node", Selector: controlexperiment.FrontierActionSelector{
				Kind: control.ActionRestart, Node: crash.Node.Node,
			}},
		}},
		2, inputs.riskSpec, rootRisk, inputs.root, inputs.experiment.Runtime,
		inputs.experiment.faultEnvelope(), factory, projector,
	)
	if err != nil || scenario.Status != controlexperiment.ScenarioStatusCompleted ||
		len(scenario.Steps) != 2 || scenario.Steps[1].Choice == nil ||
		scenario.Steps[1].Choice.Action.Kind != control.ActionRestart ||
		len(scenario.FinalTrace.Records) != len(inputs.root.Records)+2 {
		t.Fatalf("etcd/raft two-step scenario did not concretize and execute: %#v/%v", scenario, err)
	}
	tampered := inputs.spec
	tampered.RootID = "selected-after-model-output"
	if tampered.ValidateInputs(
		inputs.campaign, inputs.root, inputs.frontier, inputs.riskSpec,
		inputs.knowledge, inputs.hypothesis, inputs.experiment, inputs.searchSpec,
	) == nil {
		t.Fatal("model-dependent public calibration root was accepted")
	}
}

func TestA2b3EtcdraftSemanticCalibrationCreatesResumesAndSealsTerminalArtifacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	corpusPath := etcdraftTestRootCorpusPath
	semanticInputPath := filepath.Join(t.TempDir(), "semantic-input.json")
	semanticInputBytes, err := os.ReadFile(etcdraftTestSemanticInputPath)
	if err != nil {
		t.Fatalf("copy semantic input: %v", err)
	}
	if err := os.WriteFile(semanticInputPath, semanticInputBytes, 0o600); err != nil {
		t.Fatalf("copy semantic input: %v", err)
	}
	transportCalls := 0
	client := fixtureOpenRouterIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		transportCalls++
		view := a2b3SemanticViewFromRequest(t, request)
		content := []byte(`{"unexpected_authority":true}`)
		if transportCalls > 1 {
			ordered := make([]string, len(view.Request.Queue.Candidates))
			for index, candidate := range view.Request.Queue.Candidates {
				ordered[len(ordered)-index-1] = candidate.CandidateID
			}
			var err error
			content, err = json.Marshal(controlexperiment.SemanticExplorerProposal{
				SchemaVersion: controlexperiment.SemanticExplorerProposalVersion,
				ID:            view.Request.ID, RequestDigest: view.Request.Digest,
				QueueDigest: view.Request.Queue.Digest, OrderedCandidateIDs: ordered,
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		response := a2b2OpenRouterResponse(t, transportCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "etcdraft-semantic-calibration")
	readAttempts := 0
	options := etcdraftSemanticCalibrationRunOptions{
		Directory: directory, CorpusPath: corpusPath,
		SemanticInputPath: semanticInputPath, AgentKeyFile: "fixture-key-source",
		Client: client, ReadKey: func(string) (string, error) {
			readAttempts++
			return "", errors.New("fixture key not yet available")
		},
	}
	if _, err := runEtcdraftSemanticCalibration(ctx, options); err == nil || transportCalls != 0 || readAttempts != 1 {
		t.Fatalf("prepared run did not defer before transport: calls=%d reads=%d err=%v", transportCalls, readAttempts, err)
	} else {
		var deferred *controlexperiment.CampaignAttemptDeferredError
		if !errors.As(err, &deferred) {
			t.Fatalf("prepared run did not preserve deferred identity: %v", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(directory, "spec.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "artifact.json")); !os.IsNotExist(err) {
		t.Fatalf("deferred run consumed terminal artifact path: %v", err)
	}
	options.Resume = true
	options.ReadKey = func(string) (string, error) { return "fixture-key", nil }
	artifact, err := runEtcdraftSemanticCalibration(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Status != etcdraftSemanticCalibrationSucceeded || artifact.Explorer == nil ||
		artifact.Comparison == nil || !artifact.Comparison.DistinctFirstExpansion || artifact.Testing == nil ||
		artifact.Testing.SelectedCandidateID != artifact.Explorer.Search.ExpansionOrder[0] ||
		artifact.Testing.Bundle.Trace.Digest != artifact.Testing.SelectedPrefix ||
		!artifact.Testing.Replay.Stable || artifact.Testing.CorePSSSamples == 0 ||
		len(artifact.Testing.Oracle.Violations) != 0 ||
		len(artifact.ProviderCalls) != 2 ||
		artifact.Explorer.Calls[0].Status != controlexperiment.SemanticExplorerCallRejected ||
		artifact.Explorer.Calls[1].Status != controlexperiment.SemanticExplorerCallAccepted ||
		artifact.ModelWork != (controlexperiment.ModelWork{
			Calls: 2, InputTokens: 8, OutputTokens: 6, TotalTokens: 14,
		}) || transportCalls != 2 {
		t.Fatalf("resumed calibration artifact is incomplete: %#v calls=%d", artifact, transportCalls)
	}
	recovered, err := runEtcdraftSemanticCalibration(ctx, options)
	if err != nil || recovered.Digest != artifact.Digest || recovered.Testing == nil ||
		recovered.Testing.Bundle.Digest != artifact.Testing.Bundle.Digest || transportCalls != 2 {
		t.Fatalf("completed artifact did not recover without provider: calls=%d err=%v", transportCalls, err)
	}
	var changedInput etcdraftSemanticAuthoringSource
	if err := readStrictJSONFile(semanticInputPath, etcdraftSemanticInputLimit, &changedInput); err != nil {
		t.Fatal(err)
	}
	changedInput.Experiment.SearchMaxWorkUnits++
	changedBytes, err := json.Marshal(changedInput)
	if err != nil {
		t.Fatalf("change semantic input: %v", err)
	}
	if err := os.WriteFile(semanticInputPath, changedBytes, 0o600); err != nil {
		t.Fatalf("change semantic input: %v", err)
	}
	if _, err := runEtcdraftSemanticCalibration(ctx, options); err == nil ||
		!strings.Contains(err.Error(), "RUN_SPEC_DRIFT") || transportCalls != 2 {
		t.Fatalf("changed runtime budget resumed an old run: calls=%d err=%v", transportCalls, err)
	}

	failureCalls := 0
	failingClient := fixtureOpenRouterIntentClient()
	failingClient.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		failureCalls++
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"fixture-unavailable"}`))),
		}, nil
	})
	failureOptions := etcdraftSemanticCalibrationRunOptions{
		Directory:  filepath.Join(t.TempDir(), "etcdraft-semantic-failure"),
		CorpusPath: corpusPath, SemanticInputPath: etcdraftTestSemanticInputPath,
		AgentKeyFile: "fixture-key-source", Client: failingClient,
		ReadKey: func(string) (string, error) { return "fixture-key", nil },
	}
	failed, failureErr := runEtcdraftSemanticCalibration(ctx, failureOptions)
	var explorerFailure *controlexperiment.SemanticExplorerExecutionError
	if !errors.As(failureErr, &explorerFailure) || failed.Status != etcdraftSemanticCalibrationFailed ||
		failed.Failure == nil || failed.Failure.ReasonCode != controlexperiment.SemanticExplorerFailurePlanner ||
		len(failed.ProviderCalls) != 1 || failed.ProviderCalls[0].Status != controlexperiment.StatelessAgentCallFailed ||
		failed.ModelWork != (controlexperiment.ModelWork{Calls: 1}) || failureCalls != 3 ||
		failed.ProviderCalls[0].TransportAttempts != 3 {
		t.Fatalf("terminal provider failure was not sealed: %#v calls=%d err=%v", failed, failureCalls, failureErr)
	}
	if _, err := os.Lstat(filepath.Join(failureOptions.Directory, "artifact.json")); err != nil {
		t.Fatal(err)
	}
}

func a2b3SemanticViewFromRequest(
	t *testing.T,
	request *http.Request,
) controlexperiment.SemanticExplorerAgentView {
	t.Helper()
	if request.Header.Get("Authorization") != "Bearer fixture-key" {
		t.Fatal("fixture provider received an unexpected credential boundary")
	}
	var payload openRouterChatRequest
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(&payload); err != nil || len(payload.Messages) != 2 {
		t.Fatalf("invalid provider payload: %#v/%v", payload, err)
	}
	const marker = "Frozen input JSON:\n"
	index := strings.LastIndex(payload.Messages[1].Content, marker)
	if index < 0 {
		t.Fatal("semantic prompt did not contain a frozen input marker")
	}
	var prompt struct {
		PromptVersion string                                      `json:"prompt_version"`
		AgentView     controlexperiment.SemanticExplorerAgentView `json:"agent_view"`
	}
	if err := json.Unmarshal([]byte(payload.Messages[1].Content[index+len(marker):]), &prompt); err != nil ||
		prompt.PromptVersion != semanticExplorerPromptVersion || prompt.AgentView.Request.Validate() != nil {
		t.Fatalf("semantic prompt input cannot be decoded: %#v/%v", prompt, err)
	}
	return prompt.AgentView
}

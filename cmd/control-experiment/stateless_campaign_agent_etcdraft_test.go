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

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestM523R4bStatelessAgentCampaignResumesPreparedAttemptAndSealsCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 420*time.Second)
	defer cancel()
	root := t.TempDir()
	client, providerCalls := statelessAgentCampaignMockClient(t)
	keyAvailable := false
	keyReads := 0
	readKey := func(string) (string, error) {
		keyReads++
		if !keyAvailable {
			return "", errors.New("offline fixture key unavailable")
		}
		return "fixture-key", nil
	}
	options := statelessAgentCampaignTestOptions(root, false, "prepared", client, readKey)
	if err := runEtcdraftStatelessCampaign(ctx, options, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "ATTEMPT_DEFERRED") || *providerCalls != 0 || keyReads != 1 {
		t.Fatalf("prepared attempt crossed authority: calls=%d reads=%d err=%v", *providerCalls, keyReads, err)
	}
	for _, path := range []string{options.SummaryOut, options.ObservationOut} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("deferred attempt occupied terminal output path %s: %v", path, err)
		}
	}
	var config controlexperiment.CampaignConfig
	if err := readM523gJSON(filepath.Join(options.Directory, "config.json"), 64<<10, &config, true); err != nil {
		t.Fatal(err)
	}
	recovered, err := controlexperiment.RecoverCampaignDirectory(options.Directory, config)
	if err != nil {
		t.Fatal(err)
	}
	preparedSummary, err := controlexperiment.NewCampaignSummary(&recovered)
	if err != nil {
		t.Fatal(err)
	}
	if preparedSummary.Sequence != 0 || preparedSummary.Status != controlexperiment.CampaignSummaryStatusRunning ||
		preparedSummary.Totals.Primary.WorkUnits != 0 || preparedSummary.Totals.Replay.WorkUnits != 0 ||
		preparedSummary.Totals.Model != (controlexperiment.ModelWork{}) {
		t.Fatalf("prepared attempt invented a terminal ledger: %#v", preparedSummary)
	}
	sidecar, err := controlexperiment.CampaignAttemptSidecarDirectory(
		options.Directory, "stateless-agent", 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	callEntries, err := os.ReadDir(filepath.Join(sidecar, "model-calls"))
	if err != nil || len(callEntries) != 1 {
		t.Fatalf("prepared call was not durable: entries=%d err=%v", len(callEntries), err)
	}

	keyAvailable = true
	resumed := statelessAgentCampaignTestOptions(root, true, "resumed", client, readKey)
	if err := runEtcdraftStatelessCampaign(ctx, resumed, io.Discard); err != nil {
		t.Fatal(err)
	}
	if *providerCalls != statelessAgentMaxCalls || keyReads != statelessAgentMaxCalls+1 {
		t.Fatalf("resume did not dispatch each durable call exactly once: calls=%d reads=%d", *providerCalls, keyReads)
	}
	summary := readEtcdraftCampaignSummary(t, resumed.SummaryOut)
	observationBytes, err := os.ReadFile(resumed.ObservationOut)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := controlexperiment.DecodeStatelessCampaignObservation(observationBytes)
	if err != nil || summary.Sequence != 1 || len(observation.Attempts) != 1 ||
		observation.Attempts[0].QualifiedExecutionAttempts != 18 ||
		len(observation.Attempts[0].AgentCalls) != statelessAgentMaxCalls ||
		summary.Totals.Model != (controlexperiment.ModelWork{
			Calls: statelessAgentMaxCalls, InputTokens: 24, OutputTokens: 18, TotalTokens: 42,
		}) {
		t.Fatalf("completed Agent Campaign evidence drifted: summary=%#v observation=%#v err=%v", summary, observation, err)
	}
	for index, call := range observation.Attempts[0].AgentCalls {
		if call.Ordinal != index+1 || call.Status != controlexperiment.StatelessAgentCallCompleted ||
			call.Validate() != nil {
			t.Fatalf("call %d was not independently auditable: %#v", index+1, call)
		}
	}
	checkedCalls := *providerCalls
	completedResume := statelessAgentCampaignTestOptions(root, true, "completed-resume", client,
		func(string) (string, error) { return "", errors.New("key must not be read") })
	if err := runEtcdraftStatelessCampaign(ctx, completedResume, io.Discard); err != nil ||
		*providerCalls != checkedCalls {
		t.Fatalf("completed Campaign invoked provider again: calls=%d err=%v", *providerCalls, err)
	}
	firstCallEntries, err := os.ReadDir(filepath.Join(sidecar, "model-calls"))
	if err != nil || len(firstCallEntries) == 0 {
		t.Fatal("completed sidecar call is missing")
	}
	if err := os.WriteFile(
		filepath.Join(sidecar, "model-calls", firstCallEntries[0].Name(), "intent.json"),
		[]byte("{}\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	tampered := statelessAgentCampaignTestOptions(root, true, "tampered", client,
		func(string) (string, error) { return "", errors.New("key must not be read") })
	if err := runEtcdraftStatelessCampaign(ctx, tampered, io.Discard); err == nil ||
		*providerCalls != checkedCalls {
		t.Fatalf("tampered committed sidecar was accepted or dispatched: calls=%d err=%v", *providerCalls, err)
	}
	for _, path := range []string{tampered.SummaryOut, tampered.ObservationOut} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("sidecar failure occupied terminal output path %s: %v", path, err)
		}
	}
}

func TestM523R4bStatelessAgentCampaignMakesAmbiguousDispatchTerminalWithoutRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	root := t.TempDir()
	client, providerCalls := statelessAgentCampaignMockClient(t)
	options := statelessAgentCampaignTestOptions(root, false, "ambiguous-prepared", client,
		func(string) (string, error) { return "", errors.New("fixture key unavailable") })
	if err := runEtcdraftStatelessCampaign(ctx, options, io.Discard); err == nil {
		t.Fatal("fixture did not stop after durable intent")
	}
	sidecar, err := controlexperiment.CampaignAttemptSidecarDirectory(options.Directory, "stateless-agent", 1)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(sidecar, "model-calls"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("prepared call layout drifted: %v/%v", entries, err)
	}
	callDirectory := filepath.Join(sidecar, "model-calls", entries[0].Name())
	var intent controlexperiment.StatelessAgentCallIntent
	if err := readM523gJSON(filepath.Join(callDirectory, "intent.json"), 128<<10, &intent, true); err != nil {
		t.Fatal(err)
	}
	dispatch, err := controlexperiment.NewStatelessAgentCallDispatch(intent)
	if err != nil || writeStatelessAgentJSON(callDirectory, "dispatch.json", dispatch) != nil {
		t.Fatalf("ambiguous fixture creation failed: %v", err)
	}
	keyReads := 0
	resumed := statelessAgentCampaignTestOptions(root, true, "ambiguous-resume", client,
		func(string) (string, error) { keyReads++; return "forbidden", nil })
	if err := runEtcdraftStatelessCampaign(ctx, resumed, io.Discard); err != nil {
		t.Fatal(err)
	}
	if *providerCalls != 0 || keyReads != 0 {
		t.Fatalf("ambiguous dispatch was retried: provider=%d key=%d", *providerCalls, keyReads)
	}
	observationBytes, err := os.ReadFile(resumed.ObservationOut)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := controlexperiment.DecodeStatelessCampaignObservation(observationBytes)
	if err != nil || observation.AttemptCount != 1 || observation.Failed != 1 ||
		len(observation.Attempts[0].AgentCalls) != 1 ||
		observation.Attempts[0].AgentCalls[0].Status != controlexperiment.StatelessAgentCallAmbiguous ||
		observation.Work.Model != (controlexperiment.ModelWork{Calls: 1}) {
		t.Fatalf("ambiguous terminal evidence drifted: %#v/%v", observation, err)
	}
}

func statelessAgentCampaignTestOptions(
	root string,
	resume bool,
	suffix string,
	client deepSeekIntentClient,
	readKey agentKeyReader,
) etcdraftStatelessCampaignRunOptions {
	return etcdraftStatelessCampaignRunOptions{
		Directory: filepath.Join(root, "campaign"), SummaryOut: filepath.Join(root, "summary-"+suffix+".json"),
		ObservationOut: filepath.Join(root, "observation-"+suffix+".json"), Resume: resume,
		Strategy: etcdraftStatelessAgentCampaignStrategy, Attempts: 1, FirstSeed: 1,
		WallClockCeilingMillis: 360_000, CorpusPath: "../../" + m523gCorpusPath,
		AgentKeyFile: "fixture-key-file", ModelTokensPerAttempt: 100,
		Client: client, ReadKey: readKey,
	}
}

func statelessAgentCampaignMockClient(
	t *testing.T,
) (deepSeekIntentClient, *int) {
	t.Helper()
	calls := 0
	client := defaultDeepSeekIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var chat deepSeekChatRequest
		if json.Unmarshal(body, &chat) != nil || len(chat.Messages) != 2 {
			t.Fatal("mock received an invalid frozen request")
		}
		const marker = "Frozen input JSON:\n"
		index := strings.LastIndex(chat.Messages[1].Content, marker)
		if index < 0 {
			t.Fatal("mock could not find the frozen Agent view")
		}
		var input struct {
			Proposal controlexperiment.StatelessFrontierOrderProposal `json:"proposal_template"`
		}
		if json.Unmarshal([]byte(chat.Messages[1].Content[index+len(marker):]), &input) != nil ||
			len(input.Proposal.ActionIDs) == 0 {
			t.Fatal("mock could not decode proposal template")
		}
		for left, right := 0, len(input.Proposal.ActionIDs)-1; left < right; left, right = left+1, right-1 {
			input.Proposal.ActionIDs[left], input.Proposal.ActionIDs[right] =
				input.Proposal.ActionIDs[right], input.Proposal.ActionIDs[left]
		}
		content, err := json.Marshal(input.Proposal)
		if err != nil {
			t.Fatal(err)
		}
		response, err := json.Marshal(map[string]any{
			"id": "fixture-response", "model": deepSeekV4Flash,
			"choices": []any{map[string]any{
				"index": 0, "message": map[string]any{"role": "assistant", "content": string(content)},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 4, "completion_tokens": 3, "total_tokens": 7},
		})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	return client, &calls
}

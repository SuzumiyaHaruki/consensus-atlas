package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM521d3ReadsKeyAfterEachDurableIntent(t *testing.T) {
	root := t.TempDir()
	options := modelRunnerOptions(root, 2, false, "initial")
	content := modelRunnerProposal(t, options)
	keyReads, invocations := 0, 0
	readKey := func(string) (string, error) {
		keyReads++
		callDir := filepath.Join(options.Directory, "model-calls")
		if _, err := os.Stat(filepath.Join(callDir, fmt.Sprintf("%020d.intent.json", keyReads))); err != nil {
			return "", errors.New("intent was not durable before key read")
		}
		if _, err := os.Stat(filepath.Join(callDir, fmt.Sprintf("%020d.dispatch.json", keyReads))); !os.IsNotExist(err) {
			return "", errors.New("dispatch existed before key read")
		}
		return fmt.Sprintf("key-%d", keyReads), nil
	}
	invoke := func(
		_ context.Context, key string, prepared deepSeekPreparedRequest,
	) (deepSeekCall, error) {
		invocations++
		if key != fmt.Sprintf("key-%d", invocations) {
			return deepSeekCall{}, errors.New("key crossed attempt boundary")
		}
		return modelRunnerSuccessCall(prepared, content, invocations), nil
	}
	var stdout bytes.Buffer
	err := runEtcdraftModelCampaign(
		context.Background(), options, "unused", &stdout, newEtcdraftCampaignProvider,
		readKey, defaultDeepSeekIntentClient(), invoke,
	)
	if err != nil || keyReads != 2 || invocations != 2 {
		t.Fatalf("per-attempt key ordering failed: reads=%d calls=%d err=%v", keyReads, invocations, err)
	}
	summary := readEtcdraftCampaignSummary(t, options.SummaryOut)
	if summary.Sequence != 2 || summary.Totals.Model != (controlexperiment.ModelWork{
		Calls: 2, InputTokens: 8, OutputTokens: 6, TotalTokens: 14,
	}) || !strings.Contains(stdout.String(), "model_calls=2 model_tokens=14") {
		t.Fatalf("model runner accounting drifted: %#v stdout=%q", summary, stdout.String())
	}
}

func TestEtcdraftM521d3PreparedIntentResumesAfterKeyFailure(t *testing.T) {
	root := t.TempDir()
	options := modelRunnerOptions(root, 1, false, "first")
	content := modelRunnerProposal(t, options)
	reads, calls := 0, 0
	readKey := func(string) (string, error) {
		reads++
		if reads == 1 {
			return "", errors.New("offline key unavailable")
		}
		return "resume-key", nil
	}
	invoke := func(
		_ context.Context, _ string, prepared deepSeekPreparedRequest,
	) (deepSeekCall, error) {
		calls++
		return modelRunnerSuccessCall(prepared, content, calls), nil
	}
	err := runEtcdraftModelCampaign(
		context.Background(), options, "unused", io.Discard, newEtcdraftCampaignProvider,
		readKey, defaultDeepSeekIntentClient(), invoke,
	)
	if err == nil || reads != 1 || calls != 0 ||
		readEtcdraftCampaignSummary(t, options.SummaryOut).Status != controlexperiment.CampaignSummaryStatusRunning {
		t.Fatalf("key failure did not leave prepared Campaign: reads=%d calls=%d err=%v", reads, calls, err)
	}
	resumed := modelRunnerOptions(root, 1, true, "resume")
	err = runEtcdraftModelCampaign(
		context.Background(), resumed, "unused", io.Discard, newEtcdraftCampaignProvider,
		readKey, defaultDeepSeekIntentClient(), invoke,
	)
	summary := readEtcdraftCampaignSummary(t, resumed.SummaryOut)
	if err != nil || reads != 2 || calls != 1 || summary.Sequence != 1 || summary.Totals.Model.Calls != 1 {
		t.Fatalf("prepared resume drifted: reads=%d calls=%d summary=%#v err=%v", reads, calls, summary, err)
	}
}

func TestEtcdraftM521d3TerminalRecoveryNeverReadsKey(t *testing.T) {
	for _, state := range []string{"completed", "ambiguous", "failed"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			options := modelRunnerOptions(root, 1, true, state)
			provider, config, recovered := seedModelRunnerState(t, options, state)
			_ = provider
			keyReads, calls := 0, 0
			err := runEtcdraftModelCampaign(
				context.Background(), options, "unused", io.Discard, newEtcdraftCampaignProvider,
				func(string) (string, error) { keyReads++; return "forbidden", nil },
				defaultDeepSeekIntentClient(),
				func(context.Context, string, deepSeekPreparedRequest) (deepSeekCall, error) {
					calls++
					return deepSeekCall{}, errors.New("forbidden")
				},
			)
			if state == "completed" && err != nil || state != "completed" && err == nil ||
				keyReads != 0 || calls != 0 {
				t.Fatalf("terminal recovery crossed authority: reads=%d calls=%d err=%v", keyReads, calls, err)
			}
			checked, recoverErr := controlexperiment.RecoverCampaignDirectory(options.Directory, config)
			if recoverErr != nil {
				t.Fatal(recoverErr)
			}
			if state == "completed" {
				if checked.Head.Sequence != 1 || checked.Head.Totals.Model.Calls != 1 {
					t.Fatalf("completed result did not execute: %#v", checked.Head)
				}
			} else if checked.Failure == nil || len(recovered.ModelCalls) != 1 ||
				state == "failed" && recovered.ModelCalls[0].Result.Work.Calls != 1 {
				t.Fatalf("terminal state was not durable: %#v", checked)
			}
		})
	}
}

func seedModelRunnerState(
	t *testing.T, options etcdraftCampaignRunOptions, state string,
) (etcdraftDurableModelCampaignProvider, controlexperiment.CampaignConfig, controlexperiment.CampaignRecovery) {
	t.Helper()
	content := modelRunnerProposal(t, options)
	spec, _ := newEtcdraftCampaignSpec("etcdraft-model-campaign-spec-v1", options.DecisionsPerAttempt, options.FirstPolicySeed)
	base, err := newEtcdraftCampaignProvider(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newEtcdraftDurableModelCampaignProvider(
		base, nil, defaultDeepSeekIntentClient(), func(
			_ context.Context, prepared deepSeekPreparedRequest,
		) (deepSeekCall, error) {
			if state == "failed" {
				return deepSeekCall{PromptDigest: prepared.PromptDigest, RequestDigest: prepared.RequestDigest,
					FailureCode: deepSeekFailureTransport, Work: controlexperiment.ModelWork{Calls: 1}}, nil
			}
			return modelRunnerSuccessCall(prepared, content, 1), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	config, err := provider.campaignConfig(
		"etcdraft-model-campaign-v1", options.Attempts,
		options.ModelTokensPerAttempt, options.WallClockCeilingMillis,
	)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := controlexperiment.CreateCampaignDirectory(options.Directory, config)
	if err != nil {
		t.Fatal(err)
	}
	provider.planned.recovered = &recovered
	request, _ := controlexperiment.NewCampaignAttemptRequest(config, recovered.Head)
	if state == "ambiguous" {
		if needed, err := provider.freezeNextCall(request); err != nil || !needed {
			t.Fatalf("call did not freeze: %v/%v", needed, err)
		}
		if _, err := recovered.DispatchModelCall(); err != nil {
			t.Fatal(err)
		}
	} else {
		view, _ := provider.planned.plannerView(request)
		prepared, _ := provider.prepareRequest(view)
		if _, err := provider.resolveCall(context.Background(), request, view, prepared); state == "completed" && err != nil {
			t.Fatal(err)
		}
	}
	return provider, config, recovered
}

func modelRunnerOptions(root string, attempts int, resume bool, suffix string) etcdraftCampaignRunOptions {
	return etcdraftCampaignRunOptions{
		Directory: filepath.Join(root, "campaign"), SummaryOut: filepath.Join(root, "summary-"+suffix+".json"),
		ObservationOut: filepath.Join(root, "observation-"+suffix+".json"), Resume: resume,
		Attempts: attempts, DecisionsPerAttempt: 8, FirstPolicySeed: 91,
		WallClockCeilingMillis: 120_000, ModelTokensPerAttempt: 100,
	}
}

func modelRunnerProposal(t *testing.T, options etcdraftCampaignRunOptions) []byte {
	t.Helper()
	spec, _ := newEtcdraftCampaignSpec("etcdraft-model-campaign-spec-v1", options.DecisionsPerAttempt, options.FirstPolicySeed)
	base, err := newEtcdraftCampaignProvider(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	proposal := base.intent
	proposal.Digest = ""
	encoded, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func modelRunnerSuccessCall(prepared deepSeekPreparedRequest, content []byte, ordinal int) deepSeekCall {
	return deepSeekCall{
		PromptDigest: prepared.PromptDigest, RequestDigest: prepared.RequestDigest,
		ResponseDigest: controlexperiment.AgentInvocationDigest([]byte(fmt.Sprintf("offline-%d", ordinal))),
		Response: &controlexperiment.AgentResponseIdentity{
			ID: fmt.Sprintf("offline-%d", ordinal), Model: deepSeekV4Flash, FinishReason: "stop",
		},
		Content: content, DurationMillis: 1,
		Work: controlexperiment.ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7},
	}
}

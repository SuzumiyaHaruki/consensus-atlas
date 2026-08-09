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

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM518b4PersistsFixedOrderPairLedger(t *testing.T) {
	freeze, err := newEtcdraftAgentB4RequestFreezeFromInputsAndMethods(
		context.Background(), 96, 1,
		sharedEtcdraftB4IntentInputsFixture(t),
		sharedEtcdraftActionV2MethodFixture(t), sharedEtcdraftUniformMethodFixture(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	noFeedback := b4ConsumerProposal(
		t, freeze.Baseline, "offline-pair-no-feedback", etcdraftBackendActionClass,
	)
	withFeedback := freeze.Baseline
	withFeedback.ID, withFeedback.Digest = "offline-pair-hard-change", ""
	withFeedback.Must.Decisions--
	client, calls := b4PairMockClient(
		t, freeze, []controlexperiment.GuardedTestIntent{noFeedback, withFeedback}, nil,
	)
	result, pairErr := consumeEtcdraftAgentB4Pair(
		context.Background(), "test-secret", client, freeze,
	)
	if pairErr == nil || !strings.Contains(pairErr.Error(), deepSeekFailureBaseline) {
		t.Fatalf("mixed pair error = %v", pairErr)
	}
	if *calls != 2 || result.NoFeedback.Audit.Status != controlexperiment.AgentInvocationCompleted ||
		result.WithFeedback.Audit.Status != controlexperiment.AgentInvocationCompileRejected ||
		result.NoFeedback.Outcome == nil || result.WithFeedback.Plan != nil ||
		result.Ledger.FreezeDigest != freeze.Freeze.Digest || len(result.Ledger.Arms) != 2 ||
		result.Ledger.Totals.Model.Calls != 2 {
		t.Fatalf("unexpected mixed pair: ledger=%#v", result.Ledger)
	}
	noRecord, withRecord := result.Ledger.Arms[0], result.Ledger.Arms[1]
	if noRecord.ArmID != controlexperiment.AgentAblationArmNoFeedback ||
		withRecord.ArmID != controlexperiment.AgentAblationArmWithFeedback ||
		noRecord.FeedbackExposed || !withRecord.FeedbackExposed ||
		noRecord.PostFreezeWork.ExecutionAttempts != 1 ||
		withRecord.PostFreezeWork.ExecutionAttempts != 0 ||
		noRecord.SourceWork != freeze.Spec.SourceObservedWork ||
		withRecord.SourceWork != freeze.Spec.SourceObservedWork ||
		withRecord.ChargedWork.PrimaryWorkUnits != freeze.Spec.SourceObservedWork.PrimaryWorkUnits {
		t.Fatalf("pair work or arm order drifted: no=%#v with=%#v", noRecord, withRecord)
	}
	want, err := newEtcdraftAgentB4PairLedger(
		freeze, result.NoFeedback, result.WithFeedback,
	)
	if err != nil || want.Digest != result.Ledger.Digest {
		t.Fatalf("pair ledger does not revalidate: %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("test-secret")) {
		t.Fatal("pair result contains the model key")
	}

	directory := filepath.Join(t.TempDir(), "pair")
	if err := persistEtcdraftAgentB4Pair(directory, freeze, result); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"pair-ledger.json", "freeze.json", "view.json", "feedback.json",
		"follow-up-spec.json", "hard-baseline.json",
		"no-feedback/prompt.json", "no-feedback/request.json", "no-feedback/audit.json",
		"no-feedback/plan.json", "no-feedback/execution-instance.json",
		"no-feedback/report.json", "no-feedback/bundle.json", "no-feedback/intent-outcome.json",
		"with-feedback/prompt.json", "with-feedback/request.json", "with-feedback/audit.json",
		"with-feedback/intent.json",
	} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("missing persisted pair artifact %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "with-feedback/plan.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected arm unexpectedly persisted a plan: %v", err)
	}
	persisted, err := os.ReadFile(filepath.Join(directory, "pair-ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	var checked etcdraftAgentB4PairLedger
	if err := json.Unmarshal(persisted, &checked); err != nil || checked.Digest != result.Ledger.Digest {
		t.Fatalf("persisted pair ledger drifted: %v", err)
	}
	exact, err := os.ReadFile(filepath.Join(directory, "no-feedback/request.json"))
	if err != nil || !bytes.Equal(exact, freeze.NoFeedback.RequestBytes) {
		t.Fatalf("persisted request bytes drifted: %v", err)
	}
	if err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		artifact, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(artifact, []byte("test-secret")) {
			return errors.New("persisted pair artifact contains the model key")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := persistEtcdraftAgentB4Pair(directory, freeze, result); err == nil ||
		!strings.Contains(err.Error(), "DIRECTORY_NOT_NEW") {
		t.Fatalf("pair artifact overwrite was allowed: %v", err)
	}
	tampered := result
	tampered.Ledger.Classification = "tampered-with-original-digest"
	tamperedDirectory := filepath.Join(t.TempDir(), "tampered-pair")
	if err := persistEtcdraftAgentB4Pair(tamperedDirectory, freeze, tampered); err == nil ||
		!strings.Contains(err.Error(), "LEDGER_MISMATCH") {
		t.Fatalf("tampered pair ledger was persisted: %v", err)
	}
	if _, err := os.Stat(tamperedDirectory); !os.IsNotExist(err) {
		t.Fatalf("tampered pair created an artifact directory: %v", err)
	}

	t.Run("first-arm-failure-does-not-block-second", func(t *testing.T) {
		second := withFeedback
		second.ID = "offline-pair-second-after-failure"
		client, calls := b4PairMockClient(
			t, freeze,
			[]controlexperiment.GuardedTestIntent{freeze.Baseline, second},
			[]error{errors.New("private transport detail"), nil},
		)
		failedPair, err := consumeEtcdraftAgentB4Pair(
			context.Background(), "test-secret", client, freeze,
		)
		if err == nil || *calls != 2 ||
			failedPair.NoFeedback.Audit.Status != controlexperiment.AgentInvocationTransportFailed ||
			failedPair.WithFeedback.Audit.Status != controlexperiment.AgentInvocationCompileRejected ||
			failedPair.Ledger.Totals.Model.Calls != 2 {
			t.Fatalf("first failure blocked or rewrote second arm: calls=%d err=%v ledger=%#v",
				*calls, err, failedPair.Ledger)
		}
		encoded, marshalErr := json.Marshal(failedPair)
		if marshalErr != nil || bytes.Contains(encoded, []byte("private transport detail")) {
			t.Fatalf("private transport diagnostic escaped the audit boundary: %v", marshalErr)
		}
	})
}

func b4PairMockClient(
	t *testing.T,
	freeze etcdraftAgentB4RequestFreeze,
	proposals []controlexperiment.GuardedTestIntent,
	transportErrors []error,
) (deepSeekIntentClient, *int) {
	t.Helper()
	if len(proposals) != 2 || transportErrors != nil && len(transportErrors) != 2 {
		t.Fatal("pair mock requires exactly two responses")
	}
	expected := [][]byte{freeze.NoFeedback.RequestBytes, freeze.WithFeedback.RequestBytes}
	responses := make([][]byte, 2)
	for index, proposal := range proposals {
		proposalJSON, err := json.Marshal(proposal)
		if err != nil {
			t.Fatal(err)
		}
		responses[index], err = json.Marshal(map[string]any{
			"id": "offline-pair-response", "model": deepSeekV4Flash,
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": string(proposalJSON)},
			}},
			"usage": map[string]any{"prompt_tokens": 80, "completion_tokens": 20, "total_tokens": 100},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	client := defaultDeepSeekIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		index := calls
		calls++
		if index >= len(expected) {
			t.Fatal("pair performed more than two calls")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, expected[index]) {
			t.Fatalf("pair request order drifted at call %d", index+1)
		}
		if transportErrors != nil && transportErrors[index] != nil {
			return nil, transportErrors[index]
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(responses[index])),
		}, nil
	})
	return client, &calls
}

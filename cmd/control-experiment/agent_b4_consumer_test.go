package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM518b4ConsumesFrozenArmThroughV2Outcome(t *testing.T) {
	freeze, err := newEtcdraftAgentB4RequestFreezeFromInputsAndMethods(
		context.Background(), 96, 1,
		sharedEtcdraftB4IntentInputsFixture(t),
		sharedEtcdraftActionV2MethodFixture(t), sharedEtcdraftUniformMethodFixture(t),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("completed", func(t *testing.T) {
		proposal := b4ConsumerProposal(t, freeze.Baseline, "offline-no-feedback", etcdraftBackendActionClass)
		client, calls := b4ConsumerMockClient(t, freeze.NoFeedback.RequestBytes, proposal, nil)
		result, err := consumeEtcdraftAgentB4Arm(
			context.Background(), "test-secret", client, freeze,
			controlexperiment.AgentAblationArmNoFeedback,
		)
		if err != nil {
			t.Fatal(err)
		}
		if *calls != 1 || result.Audit.Status != controlexperiment.AgentInvocationCompleted ||
			result.Intent == nil || result.Plan == nil || result.Instance == nil ||
			result.Report == nil || result.Bundle == nil || result.Outcome == nil ||
			result.Instance.PolicySeed != freeze.Freeze.FollowUpSeed ||
			result.Instance.Budget != freeze.Freeze.ExecutionBudget ||
			result.Audit.RequestDigest != freeze.NoFeedback.RequestDigest ||
			result.Audit.Work.Model.Calls != 1 {
			t.Fatalf("unexpected completed consumer result: audit=%#v", result.Audit)
		}
		if err := validateEtcdraftAgentB4ArmResult(freeze, result); err != nil {
			t.Fatalf("completed result does not revalidate: %v", err)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte("test-secret")) {
			t.Fatal("consumer result contains the model key")
		}
	})

	t.Run("baseline-rejected-before-execution", func(t *testing.T) {
		proposal := freeze.Baseline
		proposal.ID = "offline-hard-change"
		proposal.Must.Decisions--
		proposal.Digest = ""
		client, calls := b4ConsumerMockClient(t, freeze.WithFeedback.RequestBytes, proposal, nil)
		result, err := consumeEtcdraftAgentB4Arm(
			context.Background(), "test-secret", client, freeze,
			controlexperiment.AgentAblationArmWithFeedback,
		)
		if err == nil || !strings.Contains(err.Error(), deepSeekFailureBaseline) {
			t.Fatalf("hard-baseline change error = %v", err)
		}
		if *calls != 1 || result.Audit.Status != controlexperiment.AgentInvocationCompileRejected ||
			result.Audit.FailureCode != deepSeekFailureBaseline || result.Intent == nil ||
			result.Plan != nil || result.Instance != nil || result.Report != nil ||
			result.Bundle != nil || result.Outcome != nil || result.Audit.Work.Model.Calls != 1 ||
			result.Audit.Work.Primary.WorkUnits != 0 || result.Audit.Work.Replay.WorkUnits != 0 {
			t.Fatalf("baseline failure escaped its boundary: audit=%#v", result.Audit)
		}
		if err := validateEtcdraftAgentB4ArmResult(freeze, result); err != nil {
			t.Fatalf("rejected result does not revalidate: %v", err)
		}
	})

	t.Run("transport-failure-is-durable", func(t *testing.T) {
		client, calls := b4ConsumerMockClient(
			t, freeze.NoFeedback.RequestBytes, freeze.Baseline,
			errors.New("provider detail that must not persist"),
		)
		result, err := consumeEtcdraftAgentB4Arm(
			context.Background(), "test-secret", client, freeze,
			controlexperiment.AgentAblationArmNoFeedback,
		)
		if err == nil || !strings.Contains(err.Error(), deepSeekFailureTransport) {
			t.Fatalf("transport error = %v", err)
		}
		if *calls != 1 || result.Audit.Status != controlexperiment.AgentInvocationTransportFailed ||
			result.Audit.FailureCode != deepSeekFailureTransport || result.Audit.Response != nil ||
			result.Audit.ResponseDigest != "" || result.Audit.Work.Model.Calls != 1 ||
			strings.Contains(result.Audit.FailureCode, "provider detail") {
			t.Fatalf("transport diagnostic leaked or was misclassified: %#v", result.Audit)
		}
	})

	t.Run("tampered-request-stops-before-call", func(t *testing.T) {
		tampered := freeze
		tampered.NoFeedback.RequestBytes = append([]byte(nil), freeze.NoFeedback.RequestBytes...)
		tampered.NoFeedback.RequestBytes[0] ^= 1
		client, calls := b4ConsumerMockClient(t, freeze.NoFeedback.RequestBytes, freeze.Baseline, nil)
		if _, err := consumeEtcdraftAgentB4Arm(
			context.Background(), "test-secret", client, tampered,
			controlexperiment.AgentAblationArmNoFeedback,
		); err == nil {
			t.Fatal("tampered request was consumed")
		}
		if *calls != 0 {
			t.Fatalf("tampered request reached transport: calls=%d", *calls)
		}
	})
}

func b4ConsumerProposal(
	t *testing.T,
	baseline controlexperiment.GuardedTestIntent,
	id string,
	backend string,
) controlexperiment.GuardedTestIntent {
	t.Helper()
	proposal := baseline
	proposal.ID, proposal.Digest = id, ""
	proposal.Prefer.BackendIDs = []string{backend}
	return proposal
}

func b4ConsumerMockClient(
	t *testing.T,
	expectedRequest []byte,
	proposal controlexperiment.GuardedTestIntent,
	transportErr error,
) (deepSeekIntentClient, *int) {
	t.Helper()
	proposalJSON, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	responseJSON, err := json.Marshal(map[string]any{
		"id": "offline-b4-response", "model": deepSeekV4Flash,
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop",
			"message": map[string]any{"role": "assistant", "content": string(proposalJSON)},
		}},
		"usage": map[string]any{"prompt_tokens": 80, "completion_tokens": 20, "total_tokens": 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP: agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(body, expectedRequest) {
				t.Fatal("consumer did not send the exact frozen request bytes")
			}
			if transportErr != nil {
				return nil, transportErr
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(responseJSON)),
			}, nil
		}),
	}
	clock := time.Unix(200, 0)
	client.Now = func() time.Time {
		current := clock
		clock = clock.Add(5 * time.Millisecond)
		return current
	}
	return client, &calls
}

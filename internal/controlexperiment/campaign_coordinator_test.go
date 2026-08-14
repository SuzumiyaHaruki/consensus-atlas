package controlexperiment

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCampaignCoordinatorRunsDeterministicProviderToAttemptLimit(t *testing.T) {
	config := campaignCoordinatorTestConfig(t, "coordinator", CampaignLogicalBudget{
		MaxAttempts: 3, MaxPrimarySchedulerDecisions: 15,
		MaxPrimaryWorkUnits: 18, MaxReplayWorkUnits: 18,
	}, 1_000)
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	requests := make([]CampaignAttemptRequest, 0, 3)
	provider := CampaignAttemptProviderFunc(func(
		ctx context.Context,
		request CampaignAttemptRequest,
	) (CampaignAttemptResult, error) {
		if err := request.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("provider call did not receive the remaining wall-clock deadline")
		}
		requests = append(requests, request)
		result := CampaignAttemptResult{
			Outcome: CampaignAttemptCompleted,
			Work:    campaignTestWork(5, 5, 0, 0),
			Artifact: []byte(fmt.Sprintf(
				`{"ordinal":%d,"request_digest":"%s"}`, request.Ordinal, request.Digest,
			)),
		}
		if request.Ordinal == 2 {
			result.Outcome = CampaignAttemptFailed
			result.Failure = &MethodFailure{Phase: "provider", Code: "FIXTURE_TERMINAL_FAILURE"}
		}
		return result, nil
	})
	coordinator, err := newCampaignCoordinator(
		&recovered, provider, newCampaignTestClock(0, 0, 10).Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstHead, err := coordinator.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if firstHead.Sequence != 1 || firstHead.ElapsedMillis != 10 {
		t.Fatalf("first coordinator step drifted: %#v", firstHead)
	}
	mid, err := RecoverCampaignDirectory(directory, config)
	if err != nil || mid.Head.Digest != firstHead.Digest {
		t.Fatalf("running campaign did not recover: %#v/%v", mid, err)
	}
	resumed, err := newCampaignCoordinator(
		&mid, provider, newCampaignTestClock(0, 0, 10, 10, 20).Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	head, err := resumed.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if head.StopReason != CampaignStopAttemptLimit || head.Sequence != 3 || len(requests) != 3 ||
		head.Totals.Primary.SchedulerDecisions != 15 || head.Totals.Primary.WorkUnits != 18 ||
		head.Totals.Replay.WorkUnits != 18 || head.ElapsedMillis != 30 {
		t.Fatalf("unexpected deterministic campaign result: head=%#v requests=%#v", head, requests)
	}
	wantAttempts := []int{3, 2, 1}
	wantDecisions := []int{15, 10, 5}
	wantWork := []int{18, 12, 6}
	for index, request := range requests {
		if request.Ordinal != index+1 || request.PreviousDigest == "" ||
			request.Allowance.RemainingAttempts != wantAttempts[index] ||
			request.Allowance.PrimarySchedulerDecisions != wantDecisions[index] ||
			request.Allowance.PrimaryWorkUnits != wantWork[index] ||
			request.Allowance.ReplayWorkUnits != wantWork[index] {
			t.Fatalf("allowance %d drifted: %#v", index+1, request)
		}
	}
	tampered := requests[0]
	tampered.Allowance.PrimaryWorkUnits--
	if err := tampered.Validate(); err == nil || !strings.Contains(err.Error(), "DIGEST_MISMATCH") {
		t.Fatalf("tampered request remained valid: %v", err)
	}

	checked, err := RecoverCampaignDirectory(directory, config)
	if err != nil || checked.Head.Digest != head.Digest || len(checked.Checkpoints) != 4 ||
		checked.Checkpoints[2].Record.Outcome != CampaignAttemptFailed ||
		checked.Checkpoints[2].Record.Failure == nil {
		t.Fatalf("persisted coordinator result drifted: %#v/%v", checked, err)
	}
	stoppedCalls := 0
	stoppedProvider := CampaignAttemptProviderFunc(func(
		context.Context,
		CampaignAttemptRequest,
	) (CampaignAttemptResult, error) {
		stoppedCalls++
		return CampaignAttemptResult{}, errors.New("must not be called")
	})
	stopped, err := newCampaignCoordinator(&checked, stoppedProvider, newCampaignTestClock(0).Now)
	if err != nil {
		t.Fatal(err)
	}
	if resumedHead, err := stopped.Run(context.Background()); err != nil ||
		resumedHead.Digest != head.Digest || stoppedCalls != 0 {
		t.Fatalf("stopped campaign invoked provider: head=%#v calls=%d err=%v", resumedHead, stoppedCalls, err)
	}
}

func TestCampaignCoordinatorRejectsInvalidProviderResultWithoutRetry(t *testing.T) {
	tests := []struct {
		name          string
		result        CampaignAttemptResult
		providerError error
		want          string
		preserveWork  bool
	}{
		{
			name: "provider-error", providerError: errors.New("fixture provider error"),
			want: "PROVIDER_FAILED",
		},
		{
			name: "provider-error-with-work",
			result: CampaignAttemptResult{
				Work: campaignTestWork(2, 2, 0, 0),
			},
			providerError: errors.New("fixture provider error after work"),
			want:          "PROVIDER_FAILED",
			preserveWork:  true,
		},
		{
			name: "over-allowance",
			result: CampaignAttemptResult{
				Outcome: CampaignAttemptCompleted, Work: campaignTestWork(11, 5, 0, 0),
				Artifact: []byte("over allowance"),
			},
			want: "ALLOWANCE_EXCEEDED", preserveWork: true,
		},
		{
			name: "empty-artifact",
			result: CampaignAttemptResult{
				Outcome: CampaignAttemptCompleted, Work: campaignTestWork(1, 1, 0, 0),
			},
			want: "ARTIFACT_INVALID",
		},
		{
			name: "invalid-outcome",
			result: CampaignAttemptResult{
				Outcome: "provider-invented", Work: campaignTestWork(1, 1, 0, 0),
				Artifact: []byte("invalid outcome"),
			},
			want: "ATTEMPT_OUTCOME_INVALID",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := campaignCoordinatorTestConfig(t, test.name, CampaignLogicalBudget{
				MaxAttempts: 2, MaxPrimarySchedulerDecisions: 10,
				MaxPrimaryWorkUnits: 12, MaxReplayWorkUnits: 12,
			}, 1_000)
			directory := filepath.Join(t.TempDir(), "campaign")
			recovered, err := CreateCampaignDirectory(directory, config)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			provider := CampaignAttemptProviderFunc(func(
				context.Context,
				CampaignAttemptRequest,
			) (CampaignAttemptResult, error) {
				calls++
				return test.result, test.providerError
			})
			coordinator, err := newCampaignCoordinator(
				&recovered, provider, newCampaignTestClock(0, 0, 1).Now,
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := coordinator.Step(context.Background()); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid provider result was accepted: %v", err)
			}
			if _, err := coordinator.Step(context.Background()); err == nil ||
				!strings.Contains(err.Error(), "COORDINATOR_FAILED") || calls != 1 {
				t.Fatalf("failed provider was retried: calls=%d err=%v", calls, err)
			}
			checked, err := RecoverCampaignDirectory(directory, config)
			if err != nil || checked.Head.Sequence != 0 || len(checked.OrphanArtifactDigests) != 0 ||
				checked.Failure == nil || checked.Failure.Ordinal != 1 ||
				checked.Failure.RequestDigest == "" {
				t.Fatalf("invalid provider changed durable campaign: %#v/%v", checked, err)
			}
			wantCode := CampaignFailureResult
			if test.providerError != nil {
				wantCode = CampaignFailureProvider
			}
			if checked.Failure.Code != wantCode {
				t.Fatalf("durable failure code = %q, want %q", checked.Failure.Code, wantCode)
			}
			wantFailureWork := emptyWork()
			if test.preserveWork && validateMethodWork(test.result.Work) == nil {
				wantFailureWork = test.result.Work
			}
			if checked.Failure.Work != wantFailureWork {
				t.Fatalf("durable provider work = %#v, want %#v", checked.Failure.Work, wantFailureWork)
			}
			summary, summaryErr := NewCampaignSummary(&checked)
			if summaryErr != nil || summary.Totals != wantFailureWork {
				t.Fatalf("provider failure work missing from terminal summary: %#v/%v", summary, summaryErr)
			}
			if _, err := newCampaignCoordinator(
				&checked, provider, newCampaignTestClock(0).Now,
			); err == nil || !strings.Contains(err.Error(), "COORDINATOR_INPUT_INVALID") || calls != 1 {
				t.Fatalf("recovered failed request became retryable: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestCampaignCoordinatorRecordsWallClockStopAndDoesNotStartAfterCeiling(t *testing.T) {
	config := campaignCoordinatorTestConfig(t, "wall-stop", CampaignLogicalBudget{
		MaxAttempts: 3, MaxPrimarySchedulerDecisions: 30,
		MaxPrimaryWorkUnits: 36, MaxReplayWorkUnits: 36,
	}, 100)
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	provider := CampaignAttemptProviderFunc(func(
		context.Context,
		CampaignAttemptRequest,
	) (CampaignAttemptResult, error) {
		calls++
		return CampaignAttemptResult{
			Outcome: CampaignAttemptCompleted, Work: campaignTestWork(1, 1, 0, 0),
			Artifact: []byte("wall-clock terminal artifact"),
		}, nil
	})
	coordinator, err := newCampaignCoordinator(
		&recovered, provider, newCampaignTestClock(0, 0, 100).Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	head, err := coordinator.Step(context.Background())
	if err != nil || calls != 1 || head.StopReason != CampaignStopWallClock || head.ElapsedMillis != 100 {
		t.Fatalf("wall-clock stop was not committed: head=%#v calls=%d err=%v", head, calls, err)
	}
	checked, err := RecoverCampaignDirectory(directory, config)
	if err != nil || checked.Head.Digest != head.Digest {
		t.Fatalf("wall-clock stop did not recover: %#v/%v", checked, err)
	}

	config = campaignCoordinatorTestConfig(t, "wall-before", CampaignLogicalBudget{
		MaxAttempts: 3, MaxPrimarySchedulerDecisions: 30,
		MaxPrimaryWorkUnits: 36, MaxReplayWorkUnits: 36,
	}, 100)
	directory = filepath.Join(t.TempDir(), "campaign")
	recovered, err = CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	calls = 0
	coordinator, err = newCampaignCoordinator(
		&recovered, provider, newCampaignTestClock(0, 100).Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Step(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "WALL_CLOCK_BEFORE_ATTEMPT") || calls != 0 {
		t.Fatalf("provider started after wall ceiling: calls=%d err=%v", calls, err)
	}
}

func TestCampaignCoordinatorStopsAtLogicalAllowance(t *testing.T) {
	config := campaignCoordinatorTestConfig(t, "logical-stop", CampaignLogicalBudget{
		MaxAttempts: 3, MaxPrimarySchedulerDecisions: 1,
		MaxPrimaryWorkUnits: 6, MaxReplayWorkUnits: 6,
	}, 1_000)
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	provider := CampaignAttemptProviderFunc(func(
		context.Context,
		CampaignAttemptRequest,
	) (CampaignAttemptResult, error) {
		calls++
		return CampaignAttemptResult{
			Outcome: CampaignAttemptCompleted, Work: campaignTestWork(1, 1, 0, 0),
			Artifact: []byte("logical allowance terminal artifact"),
		}, nil
	})
	coordinator, err := newCampaignCoordinator(
		&recovered, provider, newCampaignTestClock(0, 0, 1).Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	head, err := coordinator.Run(context.Background())
	if err != nil || calls != 1 || head.StopReason != CampaignStopLogicalBudget || head.Sequence != 1 {
		t.Fatalf("logical allowance did not stop campaign: head=%#v calls=%d err=%v", head, calls, err)
	}
}

func campaignCoordinatorTestConfig(
	t *testing.T,
	id string,
	budget CampaignLogicalBudget,
	wallClockMillis int64,
) CampaignConfig {
	t.Helper()
	config, err := NewCampaignConfig(
		id, "fixture-target", strings.Repeat("a", 64), strings.Repeat("c", 64),
		budget, wallClockMillis,
	)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

type campaignTestClock struct {
	times []time.Time
	next  int
}

func newCampaignTestClock(milliseconds ...int64) *campaignTestClock {
	base := time.Unix(1_700_000_000, 0)
	times := make([]time.Time, len(milliseconds))
	for index, elapsed := range milliseconds {
		times[index] = base.Add(time.Duration(elapsed) * time.Millisecond)
	}
	return &campaignTestClock{times: times}
}

func (clock *campaignTestClock) Now() time.Time {
	if len(clock.times) == 0 {
		panic("campaign test clock has no values")
	}
	if clock.next >= len(clock.times) {
		return clock.times[len(clock.times)-1]
	}
	current := clock.times[clock.next]
	clock.next++
	return current
}

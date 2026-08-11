package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM518b3FollowUpUsesUnseenSeedAndChargesSharedSources(t *testing.T) {
	batch, err := newEtcdraftComparableAgentFeedbackBatchFromMethods(
		context.Background(), sharedEtcdraftActionV2MethodFixture(t),
		sharedEtcdraftUniformMethodFixture(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("comparable feedback=%s action-workload=%d/%d uniform-workload=%d/%d",
		batch.Feedback.Digest,
		batch.Feedback.Backends[0].WorkloadCompleted, batch.Feedback.Backends[0].WorkloadPlanned,
		batch.Feedback.Backends[1].WorkloadCompleted, batch.Feedback.Backends[1].WorkloadPlanned)
	result, err := newEtcdraftAgentFollowUpBaselineFromBatch(
		context.Background(), 96, 1, batch,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Spec.ValidateInputs(
		batch.IntentInputs, batch.Feedback, batch.Sources,
	); err != nil {
		t.Fatal(err)
	}
	if err := result.Spec.ValidatePlan(result.Intent, result.Plan); err != nil {
		t.Fatal(err)
	}
	if result.Spec.DeterministicBackendID != etcdraftBackendActionClass ||
		result.Spec.FollowUpSeed != 4 || result.Plan.PolicySeed != 1 ||
		result.Summary.FollowUpSeed != 4 || result.Summary.BackendID != etcdraftBackendActionClass ||
		result.Spec.SourceBudget != (controlexperiment.MethodBudget{
			MaxExecutionAttempts: 6, MaxPrimaryWorkUnits: 588, MaxReplayWorkUnits: 588,
		}) || result.Spec.FollowUpBudget != (controlexperiment.MethodBudget{
		MaxExecutionAttempts: 1, MaxPrimaryWorkUnits: 98, MaxReplayWorkUnits: 98,
	}) || result.Spec.PerArmBudget != (controlexperiment.MethodBudget{
		MaxExecutionAttempts: 7, MaxPrimaryWorkUnits: 686, MaxReplayWorkUnits: 686,
	}) || result.Spec.SourceObservedWork.ExecutionAttempts != 6 ||
		result.Spec.SourceObservedWork.PrimaryWorkUnits != 588 ||
		result.Spec.SourceObservedWork.ReplayWorkUnits != 588 ||
		result.Summary.ChargedExecutionAttempts != 7 ||
		result.Summary.FollowUpPrimaryWorkUnits != 97 ||
		result.Summary.FollowUpReplayWorkUnits != 97 ||
		result.Summary.ChargedPrimaryWorkUnits != 685 ||
		result.Summary.ChargedReplayWorkUnits != 685 ||
		result.Summary.Status != "execution-failed" ||
		result.Summary.FailureCode != "EXPERIMENT_COMPILED_INTENT_HARD_ACTION_MISSING" ||
		result.Summary.ModelCalls != 0 {
		t.Fatalf("unexpected follow-up boundary: spec=%#v summary=%#v", result.Spec, result.Summary)
	}
	if len(result.Report.Config.Runs) != 1 ||
		result.Report.Config.Runs[0].Policy.SeedHex != randomPolicySeed(4, 1) ||
		result.Report.Runs[0].Replay.Stable == false ||
		result.Summary.ReportDigest != result.Report.Digest ||
		result.Summary.BundleDigest != result.Bundle.Digest {
		t.Fatalf("follow-up did not execute the frozen unseen seed: %#v", result.Summary)
	}
	checkedDirectory := "../../benchmarks/experiments/etcdraft-v2-agent-follow-up-m5.18b3"
	feedbackBytes, err := os.ReadFile(checkedDirectory + "/feedback.json")
	if err != nil {
		t.Fatal(err)
	}
	var checkedFeedback controlexperiment.AgentBatchFeedbackView
	if err := json.Unmarshal(feedbackBytes, &checkedFeedback); err != nil {
		t.Fatal(err)
	}
	if err := checkedFeedback.ValidateInputs(
		batch.IntentInputs, batch.Sources,
	); err != nil || checkedFeedback.Digest != result.Batch.Feedback.Digest {
		t.Fatalf("checked feedback drifted: %v", err)
	}
	specBytes, err := os.ReadFile(checkedDirectory + "/spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var checkedSpec controlexperiment.AgentFollowUpSpec
	if err := json.Unmarshal(specBytes, &checkedSpec); err != nil {
		t.Fatal(err)
	}
	if err := checkedSpec.ValidateInputs(
		batch.IntentInputs, batch.Feedback, batch.Sources,
	); err != nil || checkedSpec.Digest != result.Spec.Digest {
		t.Fatalf("checked follow-up spec drifted: %v", err)
	}
	summaryBytes, err := os.ReadFile(checkedDirectory + "/summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var checkedSummary etcdraftAgentFollowUpSummary
	if err := json.Unmarshal(summaryBytes, &checkedSummary); err != nil {
		t.Fatal(err)
	}
	if checkedSummary != result.Summary {
		t.Fatalf("checked follow-up summary drifted: %#v", checkedSummary)
	}

	overlap := result.Spec
	overlap.FollowUpSeed = 3
	if err := overlap.Validate(); err == nil || !strings.Contains(err.Error(), "SOURCE_INVALID") {
		t.Fatalf("source/follow-up seed overlap was accepted: %v", err)
	}
	tampered := result.Spec
	tampered.SourceObservedWork.PrimaryWorkUnits++
	if err := tampered.ValidateInputs(
		batch.IntentInputs, batch.Feedback, batch.Sources,
	); err == nil {
		t.Fatal("tampered source cost was accepted")
	}
	if _, err := newEtcdraftAgentFollowUpBaselineFromBatch(
		context.Background(), 96, ^uint64(0)-2, batch,
	); err == nil || err.Error() != "ETCDRAFT_FOLLOW_UP_SEED_OVERFLOW" {
		t.Fatalf("overflowing follow-up seed was accepted: %v", err)
	}
	t.Logf("spec=%s intent=%s plan=%s report=%s bundle=%s summary=%s states=%d charged=%d/%d",
		result.Spec.Digest, result.Intent.Digest, result.Plan.Digest, result.Report.Digest,
		result.Bundle.Digest, result.Summary.Digest, result.Summary.UniquePSSStates,
		result.Summary.ChargedPrimaryWorkUnits, result.Summary.ChargedReplayWorkUnits)
}

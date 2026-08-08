package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM518b2FeedbackIsComparableReprojectedAndDefectBlind(t *testing.T) {
	actionClassFixture := sharedEtcdraftActionMethodFixture(t)
	uniformFixture := sharedEtcdraftUniformMethodFixture(t)
	batch, err := newEtcdraftAgentFeedbackBatchFromMethods(
		context.Background(), actionClassFixture, uniformFixture,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Feedback.ValidateInputs(batch.IntentInputs, batch.Sources); err != nil {
		t.Fatal(err)
	}
	checked, err := os.ReadFile("../../benchmarks/experiments/etcdraft-v2-agent-feedback-m5.18b2/feedback.json")
	if err != nil {
		t.Fatal(err)
	}
	var checkedFeedback controlexperiment.AgentBatchFeedbackView
	if err := json.Unmarshal(checked, &checkedFeedback); err != nil {
		t.Fatal(err)
	}
	if err := checkedFeedback.ValidateInputs(batch.IntentInputs, batch.Sources); err != nil {
		t.Fatal(err)
	}
	if checkedFeedback.Digest != batch.Feedback.Digest {
		t.Fatalf("checked feedback = %s, want %s", checkedFeedback.Digest, batch.Feedback.Digest)
	}
	if batch.Feedback.Budget != (controlexperiment.MethodBudget{
		MaxExecutionAttempts: 3, MaxPrimaryWorkUnits: 294, MaxReplayWorkUnits: 294,
	}) || len(batch.Feedback.Backends) != 2 {
		t.Fatalf("unexpected common feedback boundary: %#v", batch.Feedback)
	}
	byBackend := make(map[string]controlexperiment.AgentBackendFeedback)
	for _, backend := range batch.Feedback.Backends {
		byBackend[backend.BackendID] = backend
	}
	actionClass := byBackend[etcdraftBackendActionClass]
	uniform := byBackend[etcdraftBackendUniform]
	if actionClass.Status != controlexperiment.AgentFeedbackFailed ||
		actionClass.ExecutionAttempts != 3 || actionClass.CompletedAttempts != 2 ||
		actionClass.FailedAttempts != 1 || actionClass.RejectedProposals != 0 ||
		actionClass.ObservedRuns != 2 || actionClass.PSSSamples != 194 ||
		actionClass.UniquePSSStates != 165 ||
		actionClass.Work.PrimaryWorkUnits != 294 || actionClass.Work.ReplayWorkUnits != 196 ||
		actionClass.Work.Model.Calls != 0 ||
		!equalInts(actionClass.NewStatesByRun, []int{83, 82}) ||
		len(actionClass.Failures) != 1 ||
		actionClass.Failures[0] != (controlexperiment.AgentFeedbackFailure{
			Class: controlexperiment.AgentFeedbackExecutionFailed, Count: 1,
		}) {
		t.Fatalf("action-class failure accounting drifted: %#v", actionClass)
	}
	if uniform.Status != controlexperiment.AgentFeedbackComplete ||
		uniform.ExecutionAttempts != 3 || uniform.CompletedAttempts != 3 ||
		uniform.FailedAttempts != 0 || uniform.RejectedProposals != 0 ||
		uniform.ObservedRuns != 3 || uniform.PSSSamples != 291 ||
		uniform.Work.PrimaryWorkUnits != 294 || uniform.Work.ReplayWorkUnits != 294 ||
		uniform.Work.Model.Calls != 0 || len(uniform.NewStatesByRun) != 3 {
		t.Fatalf("uniform feedback is not common-budget complete: %#v", uniform)
	}
	if uniform.UniquePSSStates != 238 ||
		!equalInts(uniform.NewStatesByRun, []int{82, 75, 81}) {
		t.Fatalf("uniform feedback drifted: %#v", uniform)
	}

	encoded, err := json.Marshal(batch.Feedback)
	if err != nil {
		t.Fatal(err)
	}
	lower := bytes.ToLower(encoded)
	for _, forbidden := range []string{
		"bundle", "trace", "manifest", "qualification", "build", "candidate",
		"root_cause", "oracle", "state_key", "failure_code",
	} {
		if bytes.Contains(lower, []byte(forbidden)) {
			t.Fatalf("Agent feedback leaks forbidden token %q", forbidden)
		}
	}

	swapped := append([]controlexperiment.AgentBatchFeedbackInput(nil), batch.Sources...)
	swapped[0].Observation, swapped[1].Observation = swapped[1].Observation, swapped[0].Observation
	swapped[0].Bundles, swapped[1].Bundles = swapped[1].Bundles, swapped[0].Bundles
	if err := batch.Feedback.ValidateInputs(batch.IntentInputs, swapped); err == nil ||
		!strings.Contains(err.Error(), "INPUT_MISMATCH") {
		t.Fatalf("feedback accepted different backend evidence: %v", err)
	}
	t.Logf("feedback=%s action-class=%#v uniform=%#v",
		batch.Feedback.Digest, actionClass, uniform)

	baseline, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID: "without-feedback", ViewDigest: batch.IntentInputs.Digest,
		RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{Decisions: 96, FaultEnvelope: controlexperiment.FaultEnvelope{
			MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
			MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
		}},
		Prefer: controlexperiment.IntentPrefer{BackendIDs: []string{etcdraftBackendActionClass}},
	})
	if err != nil {
		t.Fatal(err)
	}
	withFeedback := baseline
	withFeedback.ID = "with-feedback"
	withFeedback.Prefer.BackendIDs = []string{etcdraftBackendUniform}
	withFeedback, err = controlexperiment.NewGuardedTestIntent(withFeedback)
	if err != nil {
		t.Fatal(err)
	}
	if err := controlexperiment.ValidateFeedbackPreferenceAblation(
		batch.IntentInputs, baseline, withFeedback,
	); err != nil {
		t.Fatal(err)
	}
	_, prompt, err := guardedFeedbackIntentPrompt(
		batch.IntentInputs, batch.Feedback, batch.Sources, baseline,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "AgentBatchFeedbackView JSON") ||
		!strings.Contains(prompt, batch.Feedback.Digest) ||
		strings.Contains(strings.ToLower(prompt), "root_cause") {
		t.Fatal("feedback prompt is missing its trusted view or leaks a defect label")
	}
	changedHard := withFeedback
	changedHard.ID = "changed-hard"
	changedHard.Must.Decisions--
	changedHard, err = controlexperiment.NewGuardedTestIntent(changedHard)
	if err != nil {
		t.Fatal(err)
	}
	if err := controlexperiment.ValidateFeedbackPreferenceAblation(
		batch.IntentInputs, baseline, changedHard,
	); err == nil || !strings.Contains(err.Error(), "HARD_CONSTRAINT_CHANGED") {
		t.Fatalf("feedback changed a hard field: %v", err)
	}
	if _, err := etcdraftPSSGuidedCorpusMethodFromUniformSources(
		context.Background(), 96, 1,
		actionClassFixture.Reports[:2], actionClassFixture.Bundles[:2],
	); err == nil || !strings.Contains(err.Error(), "SOURCE_IDENTITY_INVALID") {
		t.Fatalf("guided method accepted action-class sources: %v", err)
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

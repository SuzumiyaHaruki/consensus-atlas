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

func TestEtcdraftM518b4FreezesPreferenceOnlyRequestsBeforeModelCalls(t *testing.T) {
	result, err := newEtcdraftAgentB4RequestFreezeFromInputsAndMethods(
		context.Background(), 96, 1,
		sharedEtcdraftB4IntentInputsFixture(t),
		sharedEtcdraftActionV2MethodFixture(t), sharedEtcdraftUniformMethodFixture(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	// The production constructor already performs the full source-backed
	// validation. Repeating it here reprojects all six immutable bundles under
	// race instrumentation without testing a different boundary; the checked
	// commitments and adversarial mutations below exercise the public
	// validators independently.
	if result.Freeze.ModelCalls != 0 || result.Freeze.FollowUpSeed != 4 ||
		result.Freeze.ExecutionBudget != (controlexperiment.MethodBudget{
			MaxExecutionAttempts: 1, MaxPrimaryWorkUnits: 98, MaxReplayWorkUnits: 98,
		}) || result.Freeze.PerArmBudget != (controlexperiment.MethodBudget{
		MaxExecutionAttempts: 7, MaxPrimaryWorkUnits: 686, MaxReplayWorkUnits: 686,
	}) || result.Freeze.Transport.MaxCallsPerArm != 1 ||
		result.Freeze.Transport.MaxRetries != 0 || len(result.Freeze.Arms) != 2 {
		t.Fatalf("unexpected b4 freeze: %#v", result.Freeze)
	}
	if len(result.Baseline.Prefer.BackendIDs) != 0 || len(result.Baseline.Prefer.Actions) != 0 ||
		result.Baseline.Must.Decisions != 96 || result.Baseline.Must.FaultEnvelope.MaxPartitions != 0 ||
		result.Baseline.Must.FaultEnvelope.MaxActivePartitions != 0 {
		t.Fatalf("hard baseline is biased or overstates the fault surface: %#v", result.Baseline)
	}
	if bytes.Contains(result.NoFeedback.RequestBytes, []byte(result.Batch.Feedback.Digest)) ||
		!bytes.Contains(result.WithFeedback.RequestBytes, []byte(result.Batch.Feedback.Digest)) {
		t.Fatal("feedback exposure does not match the frozen arm")
	}
	for _, request := range [][]byte{result.NoFeedback.RequestBytes, result.WithFeedback.RequestBytes} {
		lower := bytes.ToLower(request)
		for _, forbidden := range []string{
			"1ce8ae13dc180451833b57d8e8bd79baf75565badfb0dc92fb5ec339ef8588a",
			"31916d15ea26a1eebc7bfd0b5b0413a9e02be8bcc828947509b49e7312b8f6d",
			"7d7c1765a855d13b0dea1b43cfc21f3f726fb6426e53912a2dffeb18468e8c1e",
			"be4779d5d6851261ad3c7a72c284fa93d1101b0b5a75c87f29c4dfcd2529a980",
			"root_cause", "oracle_status", "not-reached",
		} {
			if bytes.Contains(lower, []byte(forbidden)) {
				t.Fatalf("request leaked a known outcome token %q", forbidden)
			}
		}
	}

	withoutFeedback := preferenceProposalFromBaseline(
		t, result.Baseline, "no-feedback-proposal", etcdraftBackendUniform,
	)
	withFeedback := preferenceProposalFromBaseline(
		t, result.Baseline, "with-feedback-proposal", etcdraftBackendActionClass,
	)
	for _, proposal := range []controlexperiment.GuardedTestIntent{withoutFeedback, withFeedback} {
		if err := controlexperiment.ValidatePreferenceOnlyProposal(
			result.Inputs.View, result.Baseline, proposal,
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := controlexperiment.ValidateFeedbackPreferenceAblation(
		result.Inputs.View, withoutFeedback, withFeedback,
	); err != nil {
		t.Fatal(err)
	}
	for _, intent := range []controlexperiment.GuardedTestIntent{withoutFeedback, withFeedback} {
		plan, err := controlexperiment.CompileGuardedTestIntentV2(
			result.Inputs.View, result.Inputs.Knowledge, result.Inputs.Catalog,
			result.Inputs.Qualification.Manifest, result.Inputs.Qualification.Qualification, intent,
		)
		if err != nil {
			t.Fatal(err)
		}
		instance, err := controlexperiment.NewIntentExecutionInstance(
			intent.ID+"-seed-4", plan, result.Freeze.FollowUpSeed, result.Freeze.ExecutionBudget,
		)
		if err != nil || instance.PolicySeed != 4 {
			t.Fatalf("proposal did not bind the common execution seed: %#v/%v", instance, err)
		}
	}
	tamperedProposal := withFeedback
	tamperedProposal.ID = "with-feedback-hard-change"
	tamperedProposal.Must.Decisions--
	tamperedProposal.Digest = ""
	tamperedProposal, err = controlexperiment.NewGuardedTestIntent(tamperedProposal)
	if err != nil {
		t.Fatal(err)
	}
	if err := controlexperiment.ValidatePreferenceOnlyProposal(
		result.Inputs.View, result.Baseline, tamperedProposal,
	); err == nil || !strings.Contains(err.Error(), "HARD_CONSTRAINT_CHANGED") {
		t.Fatalf("proposal changed the frozen hard baseline: %v", err)
	}

	tamperedMaterials := etcdraftAgentB4RequestMaterials(result.NoFeedback, result.WithFeedback)
	tamperedMaterials[0].RequestBytes = append([]byte(nil), tamperedMaterials[0].RequestBytes...)
	tamperedMaterials[0].RequestBytes[0] ^= 1
	if err := result.Freeze.ValidateBindings(
		result.Inputs.View, result.Batch.Feedback, result.Spec,
		result.Baseline, tamperedMaterials,
	); err == nil || !strings.Contains(err.Error(), "INPUT_MISMATCH") {
		t.Fatalf("tampered frozen request was accepted: %v", err)
	}
	checkedDirectory := "../../benchmarks/experiments/etcdraft-v2-agent-b4-freeze-m5.18b4"
	var checkedFreeze controlexperiment.AgentPreferenceAblationFreeze
	readCheckedB4FreezeJSON(t, checkedDirectory+"/freeze.json", &checkedFreeze)
	if err := checkedFreeze.ValidateBindings(
		result.Inputs.View, result.Batch.Feedback, result.Spec,
		result.Baseline, etcdraftAgentB4RequestMaterials(result.NoFeedback, result.WithFeedback),
	); err != nil || checkedFreeze.Digest != result.Freeze.Digest {
		t.Fatalf("checked request freeze drifted: %v", err)
	}
	var checkedSpec controlexperiment.AgentFollowUpSpec
	readCheckedB4FreezeJSON(t, checkedDirectory+"/follow-up-spec.json", &checkedSpec)
	if err := checkedSpec.Validate(); err != nil || checkedSpec.Digest != result.Spec.Digest {
		t.Fatalf("checked follow-up spec drifted: %v", err)
	}
	var checkedBaseline controlexperiment.GuardedTestIntent
	readCheckedB4FreezeJSON(t, checkedDirectory+"/hard-baseline.json", &checkedBaseline)
	if err := checkedBaseline.Validate(); err != nil || checkedBaseline.Digest != result.Baseline.Digest {
		t.Fatalf("checked hard baseline drifted: %v", err)
	}
	t.Logf("freeze=%s spec=%s baseline=%s no=%s with=%s feedback=%s",
		result.Freeze.Digest, result.Spec.Digest, result.Baseline.Digest,
		result.NoFeedback.RequestDigest, result.WithFeedback.RequestDigest,
		result.Batch.Feedback.Digest)
}

func readCheckedB4FreezeJSON(t *testing.T, path string, destination any) {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		t.Fatal(err)
	}
}

func preferenceProposalFromBaseline(
	t *testing.T,
	baseline controlexperiment.GuardedTestIntent,
	id string,
	backendID string,
) controlexperiment.GuardedTestIntent {
	t.Helper()
	proposal := baseline
	proposal.ID = id
	proposal.Digest = ""
	proposal.Prefer.BackendIDs = []string{backendID}
	encoded, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := controlexperiment.ParseGuardedTestIntentProposal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

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

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type etcdraftAgentB4RequestFreeze struct {
	Inputs       etcdraftIntentInputs
	Batch        etcdraftAgentFeedbackBatch
	Spec         controlexperiment.AgentFollowUpSpec
	Baseline     controlexperiment.GuardedTestIntent
	NoFeedback   deepSeekPreparedRequest
	WithFeedback deepSeekPreparedRequest
	Freeze       controlexperiment.AgentPreferenceAblationFreeze
}

func newEtcdraftAgentB4RequestFreeze(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
) (etcdraftAgentB4RequestFreeze, error) {
	actionClass, err := etcdraftActionClassMethodV2(ctx, decisions, baseSeed)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	uniform, err := etcdraftAdmissibleUniformMethod(ctx, decisions, baseSeed)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	return newEtcdraftAgentB4RequestFreezeFromMethods(
		ctx, decisions, baseSeed, actionClass, uniform,
	)
}

func newEtcdraftAgentB4RequestFreezeFromMethods(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	actionClass etcdraftMethodExecution,
	uniform etcdraftMethodExecution,
) (etcdraftAgentB4RequestFreeze, error) {
	if baseSeed > ^uint64(0)-3 {
		return etcdraftAgentB4RequestFreeze{}, errors.New("ETCDRAFT_B4_FREEZE_SEED_OVERFLOW")
	}
	inputs, err := newEtcdraftB4IntentInputs(ctx)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	return newEtcdraftAgentB4RequestFreezeFromInputsAndMethods(
		ctx, decisions, baseSeed, inputs, actionClass, uniform,
	)
}

func newEtcdraftAgentB4RequestFreezeFromInputsAndMethods(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	inputs etcdraftIntentInputs,
	actionClass etcdraftMethodExecution,
	uniform etcdraftMethodExecution,
) (etcdraftAgentB4RequestFreeze, error) {
	if baseSeed > ^uint64(0)-3 {
		return etcdraftAgentB4RequestFreeze{}, errors.New("ETCDRAFT_B4_FREEZE_SEED_OVERFLOW")
	}
	batch, err := newEtcdraftAgentFeedbackBatchFromInputsWithID(
		inputs, actionClass, uniform, true, "etcdraft-agent-feedback-m5-18b4-pre",
	)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	spec, err := controlexperiment.NewAgentFollowUpSpec(
		"etcdraft-agent-preference-ablation-m5-18b4",
		inputs.View, batch.Feedback, batch.Sources,
		[]uint64{baseSeed, baseSeed + 1, baseSeed + 2}, baseSeed+3, decisions,
		controlexperiment.MethodBudget{
			MaxExecutionAttempts: 1,
			MaxPrimaryWorkUnits:  decisions + 2,
			MaxReplayWorkUnits:   decisions + 2,
		},
		1, deepSeekDefaultTokens,
	)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	baseline, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID:         "etcdraft-preference-ablation-hard-baseline-m5-18b4",
		ViewDigest: inputs.View.Digest, RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{
			Decisions: decisions,
			FaultEnvelope: controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1,
			},
		},
	})
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	noSystem, noUser, err := guardedPreferenceAblationPrompt(inputs.View, baseline, nil)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	withSystem, withUser, err := guardedPreferenceAblationPrompt(
		inputs.View, baseline, &batch.Feedback,
	)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	if noSystem != withSystem {
		return etcdraftAgentB4RequestFreeze{}, errors.New("ETCDRAFT_B4_FREEZE_SYSTEM_PROMPT_DRIFT")
	}
	client := defaultDeepSeekIntentClient()
	noFeedback, err := client.prepare(noSystem, noUser)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	withFeedback, err := client.prepare(withSystem, withUser)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	materials := etcdraftAgentB4RequestMaterials(noFeedback, withFeedback)
	freeze, err := controlexperiment.NewAgentPreferenceAblationFreeze(
		"etcdraft-agent-preference-ablation-request-freeze-m5-18b4",
		inputs.View, batch.Feedback, spec, baseline,
		controlexperiment.AgentTransportFreeze{
			Provider: deepSeekProvider, Endpoint: client.Endpoint, Model: client.Model,
			Thinking: "disabled", Temperature: 0, MaxOutputTokens: client.MaxOutputTokens,
			MaxCallsPerArm: 1, MaxRetries: 0,
		},
		materials,
	)
	if err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	result := etcdraftAgentB4RequestFreeze{
		Inputs: inputs, Batch: batch, Spec: spec, Baseline: baseline,
		NoFeedback: noFeedback, WithFeedback: withFeedback, Freeze: freeze,
	}
	// NewAgentFollowUpSpec has already revalidated the complete source-backed
	// feedback. Check the remaining bindings without projecting all six source
	// bundles a third time; persistence performs the full independent audit.
	if err := validateEtcdraftAgentB4RequestFreezeBindings(result); err != nil {
		return etcdraftAgentB4RequestFreeze{}, err
	}
	return result, nil
}

func etcdraftAgentB4RequestMaterials(
	noFeedback deepSeekPreparedRequest,
	withFeedback deepSeekPreparedRequest,
) []controlexperiment.AgentRequestMaterial {
	return []controlexperiment.AgentRequestMaterial{
		{
			ArmID:       controlexperiment.AgentAblationArmNoFeedback,
			PromptBytes: noFeedback.PromptBytes, RequestBytes: noFeedback.RequestBytes,
		},
		{
			ArmID: controlexperiment.AgentAblationArmWithFeedback, FeedbackExposed: true,
			PromptBytes: withFeedback.PromptBytes, RequestBytes: withFeedback.RequestBytes,
		},
	}
}

func validateEtcdraftAgentB4RequestFreeze(result etcdraftAgentB4RequestFreeze) error {
	if err := result.Spec.ValidateInputs(
		result.Inputs.View, result.Batch.Feedback, result.Batch.Sources,
	); err != nil {
		return err
	}
	return validateEtcdraftAgentB4RequestFreezeBindings(result)
}

func validateEtcdraftAgentB4RequestFreezeBindings(result etcdraftAgentB4RequestFreeze) error {
	if err := result.Freeze.ValidateBindings(
		result.Inputs.View, result.Batch.Feedback, result.Spec,
		result.Baseline, etcdraftAgentB4RequestMaterials(result.NoFeedback, result.WithFeedback),
	); err != nil {
		return err
	}
	client := defaultDeepSeekIntentClient()
	if err := result.NoFeedback.validate(client); err != nil {
		return err
	}
	if err := result.WithFeedback.validate(client); err != nil {
		return err
	}
	noInput, noSystem, err := decodePreferenceAblationRequest(result.NoFeedback)
	if err != nil {
		return err
	}
	withInput, withSystem, err := decodePreferenceAblationRequest(result.WithFeedback)
	if err != nil {
		return err
	}
	if noSystem != withSystem || noInput.Feedback != nil || withInput.Feedback == nil ||
		noInput.SemanticView.Digest != result.Inputs.View.Digest ||
		withInput.SemanticView.Digest != result.Inputs.View.Digest ||
		noInput.Baseline.Digest != result.Baseline.Digest ||
		withInput.Baseline.Digest != result.Baseline.Digest ||
		withInput.Feedback.Digest != result.Batch.Feedback.Digest {
		return errors.New("ETCDRAFT_B4_FREEZE_ARM_INPUT_DRIFT")
	}
	// The feedback object must be the entire structured-input difference. Do
	// not reduce this check to a few selected fields: a future prompt change
	// must fail closed if it accidentally gives either arm extra information.
	withInput.Feedback = nil
	canonicalNo, err := json.Marshal(noInput)
	if err != nil {
		return err
	}
	canonicalWith, err := json.Marshal(withInput)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonicalNo, canonicalWith) {
		return errors.New("ETCDRAFT_B4_FREEZE_ARM_INFORMATION_DRIFT")
	}
	return nil
}

func decodePreferenceAblationRequest(
	prepared deepSeekPreparedRequest,
) (preferenceAblationPromptInput, string, error) {
	var messages []deepSeekMessage
	if err := json.Unmarshal(prepared.PromptBytes, &messages); err != nil || len(messages) != 2 ||
		messages[0].Role != "system" || messages[1].Role != "user" {
		return preferenceAblationPromptInput{}, "", errors.New("ETCDRAFT_B4_FREEZE_PROMPT_INVALID")
	}
	const prefix = "Produce the preference-only proposal from this frozen input:\n"
	if !strings.HasPrefix(messages[1].Content, prefix) {
		return preferenceAblationPromptInput{}, "", errors.New("ETCDRAFT_B4_FREEZE_PROMPT_INVALID")
	}
	var input preferenceAblationPromptInput
	if err := json.Unmarshal([]byte(strings.TrimPrefix(messages[1].Content, prefix)), &input); err != nil {
		return preferenceAblationPromptInput{}, "", err
	}
	return input, messages[0].Content, nil
}

func persistEtcdraftAgentB4RequestFreeze(
	out string,
	artifactDir string,
	result etcdraftAgentB4RequestFreeze,
	stdout io.Writer,
) error {
	if err := validateEtcdraftAgentB4RequestFreeze(result); err != nil {
		return err
	}
	structured := []struct {
		name  string
		value any
	}{
		{name: "view.json", value: result.Inputs.View},
		{name: "feedback.json", value: result.Batch.Feedback},
		{name: "follow-up-spec.json", value: result.Spec},
		{name: "hard-baseline.json", value: result.Baseline},
	}
	for _, artifact := range structured {
		encoded, err := json.MarshalIndent(artifact.value, "", "  ")
		if err != nil {
			return err
		}
		if err := writeReport(filepath.Join(artifactDir, artifact.name), encoded); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	exact := []struct {
		name string
		data []byte
	}{
		{name: "no-feedback-prompt.json", data: result.NoFeedback.PromptBytes},
		{name: "no-feedback-request.json", data: result.NoFeedback.RequestBytes},
		{name: "with-feedback-prompt.json", data: result.WithFeedback.PromptBytes},
		{name: "with-feedback-request.json", data: result.WithFeedback.RequestBytes},
	}
	for _, artifact := range exact {
		if len(artifact.data) == 0 {
			return errors.New("ETCDRAFT_B4_FREEZE_EXACT_ARTIFACT_EMPTY")
		}
		if err := os.WriteFile(filepath.Join(artifactDir, artifact.name), artifact.data, 0o644); err != nil {
			return err
		}
	}
	encoded, err := json.MarshalIndent(result.Freeze, "", "  ")
	if err != nil {
		return err
	}
	if err := writeReport(out, encoded); err != nil {
		return err
	}
	if bytes.Contains(result.NoFeedback.RequestBytes, []byte(result.Batch.Feedback.Digest)) ||
		!bytes.Contains(result.WithFeedback.RequestBytes, []byte(result.Batch.Feedback.Digest)) {
		return errors.New("ETCDRAFT_B4_FREEZE_FEEDBACK_EXPOSURE_INVALID")
	}
	fmt.Fprintf(stdout, "wrote %s\narms=2 seed=%d model_calls=0 digest=%s\n",
		out, result.Freeze.FollowUpSeed, result.Freeze.Digest)
	return nil
}

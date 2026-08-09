package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const etcdraftAgentB4PairLedgerVersion = "consensus-atlas/etcdraft-agent-b4-pair-ledger/v1"

type etcdraftAgentB4PairResult struct {
	NoFeedback   etcdraftAgentB4ArmResult
	WithFeedback etcdraftAgentB4ArmResult
	Ledger       etcdraftAgentB4PairLedger
}

type etcdraftAgentB4ArmLedger struct {
	ArmID           string `json:"arm_id"`
	FeedbackExposed bool   `json:"feedback_exposed"`

	Invocation              controlexperiment.AgentInvocationAudit      `json:"invocation"`
	ExecutionInstanceDigest string                                      `json:"execution_instance_digest,omitempty"`
	IntentOutcomeDigest     string                                      `json:"intent_outcome_digest,omitempty"`
	SourceWork              controlexperiment.AgentFollowUpObservedWork `json:"source_work"`
	PostFreezeWork          controlexperiment.AgentFollowUpObservedWork `json:"post_freeze_work"`
	ChargedWork             controlexperiment.AgentFollowUpObservedWork `json:"charged_work"`
}

type etcdraftAgentB4PairLedger struct {
	SchemaVersion  string `json:"schema_version"`
	ID             string `json:"id"`
	Classification string `json:"classification"`

	FreezeDigest         string                                      `json:"freeze_digest"`
	SemanticViewDigest   string                                      `json:"semantic_view_digest"`
	FeedbackDigest       string                                      `json:"feedback_digest"`
	FollowUpSpecDigest   string                                      `json:"follow_up_spec_digest"`
	BaselineIntentDigest string                                      `json:"baseline_intent_digest"`
	SourceReusePolicy    string                                      `json:"source_reuse_policy"`
	Arms                 []etcdraftAgentB4ArmLedger                  `json:"arms"`
	Totals               controlexperiment.AgentFollowUpObservedWork `json:"totals"`
	Digest               string                                      `json:"digest"`
}

func consumeEtcdraftAgentB4Pair(
	ctx context.Context,
	key string,
	client deepSeekIntentClient,
	freeze etcdraftAgentB4RequestFreeze,
) (etcdraftAgentB4PairResult, error) {
	if err := validateEtcdraftAgentB4RequestFreezeBindings(freeze); err != nil {
		return etcdraftAgentB4PairResult{}, err
	}
	noFeedback, noFeedbackErr := consumeEtcdraftAgentB4Arm(
		ctx, key, client, freeze, controlexperiment.AgentAblationArmNoFeedback,
	)
	withFeedback, withFeedbackErr := consumeEtcdraftAgentB4Arm(
		ctx, key, client, freeze, controlexperiment.AgentAblationArmWithFeedback,
	)
	result := etcdraftAgentB4PairResult{
		NoFeedback: noFeedback, WithFeedback: withFeedback,
	}
	ledger, err := newEtcdraftAgentB4PairLedger(freeze, noFeedback, withFeedback)
	if err != nil {
		return result, err
	}
	result.Ledger = ledger
	return result, errors.Join(noFeedbackErr, withFeedbackErr)
}

func newEtcdraftAgentB4PairLedger(
	freeze etcdraftAgentB4RequestFreeze,
	noFeedback etcdraftAgentB4ArmResult,
	withFeedback etcdraftAgentB4ArmResult,
) (etcdraftAgentB4PairLedger, error) {
	if err := validateEtcdraftAgentB4RequestFreezeBindings(freeze); err != nil {
		return etcdraftAgentB4PairLedger{}, err
	}
	if noFeedback.ArmID != controlexperiment.AgentAblationArmNoFeedback ||
		withFeedback.ArmID != controlexperiment.AgentAblationArmWithFeedback {
		return etcdraftAgentB4PairLedger{}, errors.New("ETCDRAFT_B4_PAIR_ARM_ORDER_INVALID")
	}
	if err := validateEtcdraftAgentB4ArmResult(freeze, noFeedback); err != nil {
		return etcdraftAgentB4PairLedger{}, err
	}
	if err := validateEtcdraftAgentB4ArmResult(freeze, withFeedback); err != nil {
		return etcdraftAgentB4PairLedger{}, err
	}
	if noFeedback.Plan != nil && withFeedback.Plan != nil {
		if err := controlexperiment.ValidateFeedbackPreferenceAblation(
			freeze.Inputs.View, *noFeedback.Intent, *withFeedback.Intent,
		); err != nil {
			return etcdraftAgentB4PairLedger{}, err
		}
	}
	noRecord, err := newEtcdraftAgentB4ArmLedger(freeze, noFeedback)
	if err != nil {
		return etcdraftAgentB4PairLedger{}, err
	}
	withRecord, err := newEtcdraftAgentB4ArmLedger(freeze, withFeedback)
	if err != nil {
		return etcdraftAgentB4PairLedger{}, err
	}
	ledger := etcdraftAgentB4PairLedger{
		SchemaVersion:        etcdraftAgentB4PairLedgerVersion,
		ID:                   "etcdraft-preference-ablation-pair-m5-18b4",
		Classification:       "preference-ablation-pair-calibration-only",
		FreezeDigest:         freeze.Freeze.Digest,
		SemanticViewDigest:   freeze.Inputs.View.Digest,
		FeedbackDigest:       freeze.Batch.Feedback.Digest,
		FollowUpSpecDigest:   freeze.Spec.Digest,
		BaselineIntentDigest: freeze.Baseline.Digest,
		SourceReusePolicy:    freeze.Spec.SourceReusePolicy,
		Arms:                 []etcdraftAgentB4ArmLedger{noRecord, withRecord},
		Totals:               addEtcdraftAgentB4Work(noRecord.ChargedWork, withRecord.ChargedWork),
	}
	digest, err := control.CanonicalDigest(ledger)
	if err != nil {
		return etcdraftAgentB4PairLedger{}, err
	}
	ledger.Digest = digest
	return ledger, nil
}

func newEtcdraftAgentB4ArmLedger(
	freeze etcdraftAgentB4RequestFreeze,
	result etcdraftAgentB4ArmResult,
) (etcdraftAgentB4ArmLedger, error) {
	_, commitment, err := etcdraftAgentB4ArmMaterial(freeze, result.ArmID)
	if err != nil {
		return etcdraftAgentB4ArmLedger{}, err
	}
	postFreeze := controlexperiment.AgentFollowUpObservedWork{
		PrimaryWorkUnits: result.Audit.Work.Primary.WorkUnits,
		ReplayWorkUnits:  result.Audit.Work.Replay.WorkUnits,
		Model:            result.Audit.Work.Model,
	}
	instanceDigest, outcomeDigest := "", ""
	if result.Instance != nil {
		postFreeze.ExecutionAttempts = 1
		instanceDigest = result.Instance.Digest
	}
	if result.Outcome != nil {
		outcomeDigest = result.Outcome.Digest
	}
	source := freeze.Spec.SourceObservedWork
	charged := addEtcdraftAgentB4Work(source, postFreeze)
	if charged.ExecutionAttempts > freeze.Freeze.PerArmBudget.MaxExecutionAttempts ||
		charged.PrimaryWorkUnits > freeze.Freeze.PerArmBudget.MaxPrimaryWorkUnits ||
		charged.ReplayWorkUnits > freeze.Freeze.PerArmBudget.MaxReplayWorkUnits ||
		postFreeze.Model.Calls != 1 {
		return etcdraftAgentB4ArmLedger{}, errors.New("ETCDRAFT_B4_PAIR_WORK_INVALID")
	}
	return etcdraftAgentB4ArmLedger{
		ArmID: result.ArmID, FeedbackExposed: commitment.FeedbackExposed,
		Invocation:              result.Audit,
		ExecutionInstanceDigest: instanceDigest, IntentOutcomeDigest: outcomeDigest,
		SourceWork: source, PostFreezeWork: postFreeze, ChargedWork: charged,
	}, nil
}

func addEtcdraftAgentB4Work(
	left controlexperiment.AgentFollowUpObservedWork,
	right controlexperiment.AgentFollowUpObservedWork,
) controlexperiment.AgentFollowUpObservedWork {
	return controlexperiment.AgentFollowUpObservedWork{
		ExecutionAttempts: left.ExecutionAttempts + right.ExecutionAttempts,
		PrimaryWorkUnits:  left.PrimaryWorkUnits + right.PrimaryWorkUnits,
		ReplayWorkUnits:   left.ReplayWorkUnits + right.ReplayWorkUnits,
		Model: controlexperiment.ModelWork{
			Calls:        left.Model.Calls + right.Model.Calls,
			InputTokens:  left.Model.InputTokens + right.Model.InputTokens,
			OutputTokens: left.Model.OutputTokens + right.Model.OutputTokens,
			TotalTokens:  left.Model.TotalTokens + right.Model.TotalTokens,
		},
	}
}

func persistEtcdraftAgentB4Pair(
	directory string,
	freeze etcdraftAgentB4RequestFreeze,
	result etcdraftAgentB4PairResult,
) error {
	want, err := newEtcdraftAgentB4PairLedger(freeze, result.NoFeedback, result.WithFeedback)
	provided := result.Ledger
	providedDigest := provided.Digest
	provided.Digest = ""
	providedBodyDigest, providedErr := control.CanonicalDigest(provided)
	if err != nil || providedErr != nil || want.Digest != providedDigest ||
		providedBodyDigest != providedDigest {
		return errors.New("ETCDRAFT_B4_PAIR_LEDGER_MISMATCH")
	}
	clean := filepath.Clean(directory)
	if directory == "" || clean == "." || clean == string(filepath.Separator) {
		return errors.New("ETCDRAFT_B4_PAIR_DIRECTORY_INVALID")
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(clean, 0o755); err != nil {
		return errors.New("ETCDRAFT_B4_PAIR_DIRECTORY_NOT_NEW")
	}
	structured := []struct {
		path  string
		value any
	}{
		{path: "pair-ledger.json", value: result.Ledger},
		{path: "freeze.json", value: freeze.Freeze},
		{path: "view.json", value: freeze.Inputs.View},
		{path: "feedback.json", value: freeze.Batch.Feedback},
		{path: "follow-up-spec.json", value: freeze.Spec},
		{path: "hard-baseline.json", value: freeze.Baseline},
	}
	for _, arm := range []struct {
		name   string
		result etcdraftAgentB4ArmResult
	}{
		{name: controlexperiment.AgentAblationArmNoFeedback, result: result.NoFeedback},
		{name: controlexperiment.AgentAblationArmWithFeedback, result: result.WithFeedback},
	} {
		structured = append(structured, etcdraftAgentB4ArmArtifacts(arm.name, arm.result)...)
	}
	for _, artifact := range structured {
		encoded, err := json.MarshalIndent(artifact.value, "", "  ")
		if err != nil {
			return err
		}
		if err := writeReport(filepath.Join(clean, artifact.path), encoded); err != nil {
			return err
		}
	}
	exact := []struct {
		path string
		data []byte
	}{
		{path: "no-feedback/prompt.json", data: freeze.NoFeedback.PromptBytes},
		{path: "no-feedback/request.json", data: freeze.NoFeedback.RequestBytes},
		{path: "with-feedback/prompt.json", data: freeze.WithFeedback.PromptBytes},
		{path: "with-feedback/request.json", data: freeze.WithFeedback.RequestBytes},
	}
	for _, artifact := range exact {
		if err := os.WriteFile(filepath.Join(clean, artifact.path), artifact.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func etcdraftAgentB4ArmArtifacts(
	armID string,
	result etcdraftAgentB4ArmResult,
) []struct {
	path  string
	value any
} {
	artifacts := []struct {
		path  string
		value any
	}{{path: filepath.Join(armID, "audit.json"), value: result.Audit}}
	appendArtifact := func(name string, value any) {
		artifacts = append(artifacts, struct {
			path  string
			value any
		}{path: filepath.Join(armID, name), value: value})
	}
	if result.Intent != nil {
		appendArtifact("intent.json", result.Intent)
	}
	if result.Plan != nil {
		appendArtifact("plan.json", result.Plan)
	}
	if result.Instance != nil {
		appendArtifact("execution-instance.json", result.Instance)
	}
	if result.Report != nil {
		appendArtifact("report.json", result.Report)
	}
	if result.Bundle != nil {
		appendArtifact("bundle.json", result.Bundle)
	}
	if result.Outcome != nil {
		appendArtifact("intent-outcome.json", result.Outcome)
	}
	return artifacts
}

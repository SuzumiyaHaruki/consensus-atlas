package controlexperiment

import (
	"errors"
	"sort"
)

const AgentPreferenceAblationFreezeVersion = "consensus-atlas/agent-preference-ablation-freeze/v1"

const (
	AgentAblationArmNoFeedback   = "no-feedback"
	AgentAblationArmWithFeedback = "with-feedback"
)

// AgentRequestMaterial supplies exact public transport bytes to the trusted
// freeze constructor. The bytes are persisted separately; only their digest
// and length enter the small canonical freeze object.
type AgentRequestMaterial struct {
	ArmID           string
	FeedbackExposed bool
	PromptBytes     []byte
	RequestBytes    []byte
}

type AgentTransportFreeze struct {
	Provider        string `json:"provider"`
	Endpoint        string `json:"endpoint"`
	Model           string `json:"model"`
	Thinking        string `json:"thinking"`
	Temperature     int    `json:"temperature"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	MaxCallsPerArm  int    `json:"max_calls_per_arm"`
	MaxRetries      int    `json:"max_retries"`
}

type AgentRequestCommitment struct {
	ArmID           string `json:"arm_id"`
	FeedbackExposed bool   `json:"feedback_exposed"`
	PromptDigest    string `json:"prompt_digest"`
	PromptBytes     int    `json:"prompt_bytes"`
	RequestDigest   string `json:"request_digest"`
	RequestBytes    int    `json:"request_bytes"`
}

// AgentPreferenceAblationFreeze binds the two exact model requests before
// either arm can read a key or contact a provider. The FollowUpSpec owns the
// shared source split, unseen seed and per-arm execution budget. This object
// only adds the common hard baseline and transport-byte commitments.
type AgentPreferenceAblationFreeze struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`

	SemanticViewDigest   string       `json:"semantic_view_digest"`
	FeedbackDigest       string       `json:"feedback_digest"`
	FollowUpSpecDigest   string       `json:"follow_up_spec_digest"`
	BaselineIntentDigest string       `json:"baseline_intent_digest"`
	FollowUpSeed         uint64       `json:"follow_up_seed"`
	ExecutionBudget      MethodBudget `json:"execution_budget"`
	PerArmBudget         MethodBudget `json:"per_arm_budget"`

	Transport  AgentTransportFreeze     `json:"transport"`
	Arms       []AgentRequestCommitment `json:"arms"`
	ModelCalls int                      `json:"model_calls"`
	Digest     string                   `json:"digest"`
}

func NewAgentPreferenceAblationFreeze(
	id string,
	semantic AgentSemanticView,
	feedback AgentBatchFeedbackView,
	spec AgentFollowUpSpec,
	baseline GuardedTestIntent,
	transport AgentTransportFreeze,
	materials []AgentRequestMaterial,
) (AgentPreferenceAblationFreeze, error) {
	if !validMethodToken(id) || len(materials) != 2 {
		return AgentPreferenceAblationFreeze{}, errors.New("EXPERIMENT_AGENT_ABLATION_FREEZE_INPUT_INVALID")
	}
	if err := semantic.Validate(); err != nil {
		return AgentPreferenceAblationFreeze{}, err
	}
	if err := feedback.Validate(); err != nil {
		return AgentPreferenceAblationFreeze{}, err
	}
	if err := spec.Validate(); err != nil {
		return AgentPreferenceAblationFreeze{}, err
	}
	if err := baseline.Validate(); err != nil {
		return AgentPreferenceAblationFreeze{}, err
	}
	if feedback.SemanticViewDigest != semantic.Digest || spec.SemanticViewDigest != semantic.Digest ||
		spec.FeedbackDigest != feedback.Digest || baseline.ViewDigest != semantic.Digest ||
		baseline.Must.Decisions != spec.Decisions ||
		len(baseline.Prefer.BackendIDs) != 0 || len(baseline.Prefer.Actions) != 0 {
		return AgentPreferenceAblationFreeze{}, errors.New("EXPERIMENT_AGENT_ABLATION_BASELINE_INVALID")
	}
	freeze := AgentPreferenceAblationFreeze{
		SchemaVersion: AgentPreferenceAblationFreezeVersion, ID: id,
		SemanticViewDigest: semantic.Digest, FeedbackDigest: feedback.Digest,
		FollowUpSpecDigest: spec.Digest, BaselineIntentDigest: baseline.Digest,
		FollowUpSeed: spec.FollowUpSeed, ExecutionBudget: spec.FollowUpBudget,
		PerArmBudget: spec.PerArmBudget, Transport: transport, ModelCalls: 0,
	}
	for _, material := range materials {
		freeze.Arms = append(freeze.Arms, AgentRequestCommitment{
			ArmID: material.ArmID, FeedbackExposed: material.FeedbackExposed,
			PromptDigest: AgentInvocationDigest(material.PromptBytes), PromptBytes: len(material.PromptBytes),
			RequestDigest: AgentInvocationDigest(material.RequestBytes), RequestBytes: len(material.RequestBytes),
		})
	}
	var err error
	freeze, err = freeze.seal()
	if err != nil {
		return AgentPreferenceAblationFreeze{}, err
	}
	if err := freeze.Validate(); err != nil {
		return AgentPreferenceAblationFreeze{}, err
	}
	return freeze, nil
}

func (freeze AgentPreferenceAblationFreeze) Validate() error {
	if freeze.SchemaVersion != AgentPreferenceAblationFreezeVersion || !validMethodToken(freeze.ID) ||
		!validSHA256(freeze.SemanticViewDigest) || !validSHA256(freeze.FeedbackDigest) ||
		!validSHA256(freeze.FollowUpSpecDigest) || !validSHA256(freeze.BaselineIntentDigest) ||
		freeze.ExecutionBudget.validate() != nil || freeze.ExecutionBudget.MaxExecutionAttempts != 1 ||
		freeze.PerArmBudget.validate() != nil || freeze.ModelCalls != 0 || len(freeze.Arms) != 2 ||
		!freeze.Transport.valid() {
		return errors.New("EXPERIMENT_AGENT_ABLATION_FREEZE_INVALID")
	}
	wantArms := []struct {
		id       string
		feedback bool
	}{{AgentAblationArmNoFeedback, false}, {AgentAblationArmWithFeedback, true}}
	for index, arm := range freeze.Arms {
		if arm.ArmID != wantArms[index].id || arm.FeedbackExposed != wantArms[index].feedback ||
			!validSHA256(arm.PromptDigest) || arm.PromptBytes <= 0 ||
			!validSHA256(arm.RequestDigest) || arm.RequestBytes <= 0 {
			return errors.New("EXPERIMENT_AGENT_ABLATION_FREEZE_ARM_INVALID")
		}
	}
	if freeze.Arms[0].PromptDigest == freeze.Arms[1].PromptDigest ||
		freeze.Arms[0].RequestDigest == freeze.Arms[1].RequestDigest {
		return errors.New("EXPERIMENT_AGENT_ABLATION_FREEZE_DIFFERENCE_MISSING")
	}
	sealed, err := freeze.seal()
	if err != nil || !validSHA256(freeze.Digest) || sealed.Digest != freeze.Digest {
		return errors.New("EXPERIMENT_AGENT_ABLATION_FREEZE_DIGEST_MISMATCH")
	}
	return nil
}

func (freeze AgentPreferenceAblationFreeze) ValidateInputs(
	semantic AgentSemanticView,
	feedback AgentBatchFeedbackView,
	inputs []AgentBatchFeedbackInput,
	spec AgentFollowUpSpec,
	baseline GuardedTestIntent,
	materials []AgentRequestMaterial,
) error {
	if err := spec.ValidateInputs(semantic, feedback, inputs); err != nil {
		return err
	}
	return freeze.ValidateBindings(semantic, feedback, spec, baseline, materials)
}

// ValidateBindings rechecks already source-validated semantic/feedback/spec
// objects without reprojecting every bound ExecutionBundle. A composition
// must call AgentFollowUpSpec.ValidateInputs once before using this shortcut.
func (freeze AgentPreferenceAblationFreeze) ValidateBindings(
	semantic AgentSemanticView,
	feedback AgentBatchFeedbackView,
	spec AgentFollowUpSpec,
	baseline GuardedTestIntent,
	materials []AgentRequestMaterial,
) error {
	if err := freeze.Validate(); err != nil {
		return err
	}
	want, err := NewAgentPreferenceAblationFreeze(
		freeze.ID, semantic, feedback, spec, baseline, freeze.Transport, materials,
	)
	if err != nil {
		return err
	}
	if want.Digest != freeze.Digest {
		return errors.New("EXPERIMENT_AGENT_ABLATION_FREEZE_INPUT_MISMATCH")
	}
	return nil
}

func (transport AgentTransportFreeze) valid() bool {
	return validMethodToken(transport.Provider) && transport.Endpoint != "" &&
		validMethodToken(transport.Model) && transport.Thinking == "disabled" &&
		transport.Temperature == 0 && transport.MaxOutputTokens > 0 &&
		transport.MaxOutputTokens <= 4096 && transport.MaxCallsPerArm == 1 &&
		transport.MaxRetries == 0
}

func (freeze AgentPreferenceAblationFreeze) seal() (AgentPreferenceAblationFreeze, error) {
	freeze.Arms = append([]AgentRequestCommitment(nil), freeze.Arms...)
	sort.Slice(freeze.Arms, func(i, j int) bool { return freeze.Arms[i].ArmID < freeze.Arms[j].ArmID })
	freeze.Digest = ""
	digest, err := portableJSONDigest(freeze)
	if err != nil {
		return AgentPreferenceAblationFreeze{}, err
	}
	freeze.Digest = digest
	return freeze, nil
}

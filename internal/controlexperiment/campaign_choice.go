package controlexperiment

import "errors"

const CampaignExecutionChoiceVersion = "consensus-atlas/campaign-execution-choice/v1"

// CampaignExecutionChoice binds one trusted macro plan to its coordinator-
// owned execution instance. It says what was selected, not what was reached.
type CampaignExecutionChoice struct {
	SchemaVersion           string `json:"schema_version"`
	IntentDigest            string `json:"intent_digest"`
	PlanDigest              string `json:"plan_digest"`
	ExecutionInstanceDigest string `json:"execution_instance_digest"`
	BackendID               string `json:"backend_id"`
	Strategy                string `json:"strategy"`
	PolicySeed              uint64 `json:"policy_seed"`
	Digest                  string `json:"digest"`
}

func NewCampaignExecutionChoice(
	intent GuardedTestIntent,
	plan CompiledIntentPlanV2,
	instance IntentExecutionInstance,
) (CampaignExecutionChoice, error) {
	if err := intent.Validate(); err != nil {
		return CampaignExecutionChoice{}, err
	}
	if err := plan.Validate(); err != nil {
		return CampaignExecutionChoice{}, err
	}
	if plan.IntentDigest != intent.Digest {
		return CampaignExecutionChoice{}, errors.New("EXPERIMENT_CAMPAIGN_EXECUTION_CHOICE_INTENT_MISMATCH")
	}
	if err := instance.ValidatePlan(plan); err != nil {
		return CampaignExecutionChoice{}, err
	}
	choice := CampaignExecutionChoice{
		SchemaVersion: CampaignExecutionChoiceVersion,
		IntentDigest:  intent.Digest, PlanDigest: plan.Digest,
		ExecutionInstanceDigest: instance.Digest,
		BackendID:               plan.BackendID, Strategy: plan.Strategy, PolicySeed: instance.PolicySeed,
	}
	sealed, err := choice.seal()
	if err != nil {
		return CampaignExecutionChoice{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CampaignExecutionChoice{}, err
	}
	return sealed, nil
}

func (choice CampaignExecutionChoice) Validate() error {
	if choice.SchemaVersion != CampaignExecutionChoiceVersion ||
		!validSHA256(choice.IntentDigest) || !validSHA256(choice.PlanDigest) ||
		!validSHA256(choice.ExecutionInstanceDigest) || !validMethodToken(choice.BackendID) ||
		!validMethodToken(choice.Strategy) {
		return errors.New("EXPERIMENT_CAMPAIGN_EXECUTION_CHOICE_INVALID")
	}
	sealed, err := choice.seal()
	if err != nil || !validSHA256(choice.Digest) || sealed.Digest != choice.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_EXECUTION_CHOICE_DIGEST_MISMATCH")
	}
	return nil
}

func (choice CampaignExecutionChoice) ValidateInputs(
	intent GuardedTestIntent,
	plan CompiledIntentPlanV2,
	instance IntentExecutionInstance,
) error {
	if err := choice.Validate(); err != nil {
		return err
	}
	want, err := NewCampaignExecutionChoice(intent, plan, instance)
	if err != nil {
		return err
	}
	if want.Digest != choice.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_EXECUTION_CHOICE_INPUT_MISMATCH")
	}
	return nil
}

func (choice CampaignExecutionChoice) seal() (CampaignExecutionChoice, error) {
	choice.Digest = ""
	digest, err := portableJSONDigest(choice)
	if err != nil {
		return CampaignExecutionChoice{}, err
	}
	choice.Digest = digest
	return choice, nil
}

// CampaignChoiceObservation is the Agent-safe projection. In particular it
// omits policy seed and execution-instance identity.
type CampaignChoiceObservation struct {
	ChoiceDigest string `json:"choice_digest"`
	IntentDigest string `json:"intent_digest"`
	PlanDigest   string `json:"plan_digest"`
	BackendID    string `json:"backend_id"`
	Strategy     string `json:"strategy"`
}

func projectCampaignChoice(choice CampaignExecutionChoice) CampaignChoiceObservation {
	return CampaignChoiceObservation{
		ChoiceDigest: choice.Digest, IntentDigest: choice.IntentDigest, PlanDigest: choice.PlanDigest,
		BackendID: choice.BackendID, Strategy: choice.Strategy,
	}
}

func (choice CampaignChoiceObservation) validate() error {
	if !validSHA256(choice.ChoiceDigest) || !validSHA256(choice.IntentDigest) ||
		!validSHA256(choice.PlanDigest) || !validMethodToken(choice.BackendID) ||
		!validMethodToken(choice.Strategy) {
		return errors.New("EXPERIMENT_CAMPAIGN_CHOICE_OBSERVATION_INVALID")
	}
	return nil
}

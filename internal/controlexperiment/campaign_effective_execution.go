package controlexperiment

import "errors"

const CampaignEffectiveExecutionVersion = "consensus-atlas/campaign-effective-execution/v1"

// CampaignEffectiveExecution identifies the trusted inputs that can change a
// target execution. It intentionally excludes proposal, compiler-work, plan
// and instance digests: those remain audit identities but may differ because
// of preference metadata that never reaches the executor.
type CampaignEffectiveExecution struct {
	SchemaVersion              string        `json:"schema_version"`
	TargetID                   string        `json:"target_id"`
	TargetIdentityDigest       string        `json:"target_identity_digest"`
	ExecutionEnvironmentDigest string        `json:"execution_environment_digest"`
	Strategy                   string        `json:"strategy"`
	PolicySeed                 uint64        `json:"policy_seed"`
	PolicyDigest               string        `json:"policy_digest"`
	Decisions                  int           `json:"decisions"`
	FaultEnvelope              FaultEnvelope `json:"fault_envelope"`
	Budget                     MethodBudget  `json:"budget"`
	Digest                     string        `json:"digest"`
}

func NewCampaignEffectiveExecution(
	targetID string,
	targetIdentityDigest string,
	executionEnvironmentDigest string,
	policyDigest string,
	plan CompiledIntentPlanV2,
	instance IntentExecutionInstance,
) (CampaignEffectiveExecution, error) {
	if err := plan.Validate(); err != nil {
		return CampaignEffectiveExecution{}, err
	}
	if err := instance.ValidatePlan(plan); err != nil {
		return CampaignEffectiveExecution{}, err
	}
	identity := CampaignEffectiveExecution{
		SchemaVersion: CampaignEffectiveExecutionVersion,
		TargetID:      targetID, TargetIdentityDigest: targetIdentityDigest,
		ExecutionEnvironmentDigest: executionEnvironmentDigest,
		Strategy:                   plan.Strategy, PolicySeed: instance.PolicySeed, PolicyDigest: policyDigest,
		Decisions: plan.Decisions, FaultEnvelope: plan.FaultEnvelope, Budget: instance.Budget,
	}
	sealed, err := identity.seal()
	if err != nil {
		return CampaignEffectiveExecution{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CampaignEffectiveExecution{}, err
	}
	return sealed, nil
}

func (identity CampaignEffectiveExecution) Validate() error {
	if identity.SchemaVersion != CampaignEffectiveExecutionVersion ||
		!validMethodToken(identity.TargetID) || !validSHA256(identity.TargetIdentityDigest) ||
		!validSHA256(identity.ExecutionEnvironmentDigest) || !validMethodToken(identity.Strategy) ||
		!validSHA256(identity.PolicyDigest) || identity.Decisions <= 0 ||
		identity.FaultEnvelope.Validate() != nil || identity.Budget.validate() != nil ||
		identity.Budget.MaxExecutionAttempts != 1 ||
		identity.Budget.MaxPrimaryWorkUnits < identity.Decisions ||
		identity.Budget.MaxReplayWorkUnits < identity.Decisions {
		return errors.New("EXPERIMENT_CAMPAIGN_EFFECTIVE_EXECUTION_INVALID")
	}
	sealed, err := identity.seal()
	if err != nil || !validSHA256(identity.Digest) || sealed.Digest != identity.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_EFFECTIVE_EXECUTION_DIGEST_MISMATCH")
	}
	return nil
}

func (identity CampaignEffectiveExecution) seal() (CampaignEffectiveExecution, error) {
	identity.Digest = ""
	digest, err := portableJSONDigest(identity)
	if err != nil {
		return CampaignEffectiveExecution{}, err
	}
	identity.Digest = digest
	return identity, nil
}

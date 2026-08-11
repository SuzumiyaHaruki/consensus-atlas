package main

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	etcdraftCampaignExecutionEnvironmentVersion = "consensus-atlas/etcdraft-campaign-execution-environment/v1"
	etcdraftCampaignExecutorID                  = "consensus-atlas/etcdraft-qualified-executor/v1"
)

func etcdraftCampaignRuntimeConfig() controlexperiment.RuntimeConfig {
	return controlexperiment.RuntimeConfig{
		SeedHex:   "6f6666696369616c2d65746364726166742d76322d636c75737465722d73656564",
		MaxClones: 1,
	}
}

func etcdraftCampaignExecutionPolicy(
	strategy string,
	policySeed uint64,
	decisions int,
) (controlexperiment.Policy, error) {
	policy := controlexperiment.Policy{
		ID:       "action-class-random-v1/run-1",
		Version:  controlexperiment.ActionClassPolicyVersion,
		SeedHex:  randomPolicySeed(policySeed, 1),
		Priority: []control.ActionKind{control.ActionInvoke},
	}
	if strategy == "workload-admissible-uniform-b4" {
		policy.ID = "admissible-uniform-v1/run-1"
		policy.Version = controlexperiment.AdmissibleUniformPolicyVersion
	} else if strategy != "workload-action-class-random-b4" {
		return controlexperiment.Policy{}, errors.New("ETCDRAFT_CAMPAIGN_EFFECTIVE_STRATEGY_INVALID")
	}
	if err := policy.Validate(decisions); err != nil {
		return controlexperiment.Policy{}, err
	}
	return policy, nil
}

func (provider etcdraftCampaignProvider) executionEnvironmentDigest() (string, error) {
	if err := provider.validate(); err != nil {
		return "", err
	}
	workload, err := etcdraftCampaignWorkload()
	if err != nil {
		return "", err
	}
	workloadDigest, err := workload.Digest()
	if err != nil {
		return "", err
	}
	return control.CanonicalDigest(struct {
		SchemaVersion        string                          `json:"schema_version"`
		ExecutorID           string                          `json:"executor_id"`
		TargetID             string                          `json:"target_id"`
		TargetIdentityDigest string                          `json:"target_identity_digest"`
		Runtime              controlexperiment.RuntimeConfig `json:"runtime"`
		WorkloadRouterID     string                          `json:"workload_router_id"`
		WorkloadDigest       string                          `json:"workload_digest"`
		RequireReplay        bool                            `json:"require_replay"`
	}{
		SchemaVersion: etcdraftCampaignExecutionEnvironmentVersion,
		ExecutorID:    etcdraftCampaignExecutorID, TargetID: etcdraftCampaignTargetID,
		TargetIdentityDigest: provider.targetIdentityDigest,
		Runtime:              etcdraftCampaignRuntimeConfig(),
		WorkloadRouterID:     etcdraftv2.WorkloadRouterID,
		WorkloadDigest:       workloadDigest,
		RequireReplay:        true,
	})
}

func (provider etcdraftCampaignProvider) effectiveExecution(
	planned controlexperiment.CampaignPlannedAttempt,
) (controlexperiment.CampaignEffectiveExecution, error) {
	if err := planned.Validate(); err != nil {
		return controlexperiment.CampaignEffectiveExecution{}, err
	}
	if err := provider.validate(); err != nil {
		return controlexperiment.CampaignEffectiveExecution{}, err
	}
	if planned.View.Request.TargetID != etcdraftCampaignTargetID ||
		planned.View.Request.TargetIdentityDigest != provider.targetIdentityDigest ||
		planned.Plan.Decisions != provider.spec.DecisionsPerAttempt {
		return controlexperiment.CampaignEffectiveExecution{},
			errors.New("ETCDRAFT_CAMPAIGN_EFFECTIVE_INPUT_MISMATCH")
	}
	seed, err := provider.spec.seed(planned.View.Request.Ordinal)
	if err != nil || seed != planned.Instance.PolicySeed {
		return controlexperiment.CampaignEffectiveExecution{},
			errors.New("ETCDRAFT_CAMPAIGN_EFFECTIVE_SEED_MISMATCH")
	}
	policy, err := etcdraftCampaignExecutionPolicy(
		planned.Plan.Strategy, planned.Instance.PolicySeed, planned.Plan.Decisions,
	)
	if err != nil {
		return controlexperiment.CampaignEffectiveExecution{}, err
	}
	policyDigest, err := policy.Digest()
	if err != nil {
		return controlexperiment.CampaignEffectiveExecution{}, err
	}
	environmentDigest, err := provider.executionEnvironmentDigest()
	if err != nil {
		return controlexperiment.CampaignEffectiveExecution{}, err
	}
	return controlexperiment.NewCampaignEffectiveExecution(
		etcdraftCampaignTargetID, provider.targetIdentityDigest, environmentDigest,
		policyDigest, planned.Plan, planned.Instance,
	)
}

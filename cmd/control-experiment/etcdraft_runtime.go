package main

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const etcdraftCampaignTargetID = "etcdraft-v2"

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
		ID: "action-class-random-v1/run-1", Version: controlexperiment.ActionClassPolicyVersion,
		SeedHex: randomPolicySeed(policySeed, 1), Priority: []control.ActionKind{control.ActionInvoke},
	}
	if strategy == "workload-admissible-uniform-b4" {
		policy.ID, policy.Version = "admissible-uniform-v1/run-1", controlexperiment.AdmissibleUniformPolicyVersion
	} else if strategy != "workload-action-class-random-b4" {
		return controlexperiment.Policy{}, errors.New("ETCDRAFT_EXECUTION_STRATEGY_INVALID")
	}
	if err := policy.Validate(decisions); err != nil {
		return controlexperiment.Policy{}, err
	}
	return policy, nil
}

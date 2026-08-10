package main

import (
	"context"
	"errors"
	"io"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const etcdraftCampaignModelRunnerStrategy = "campaign-etcdraft-agent-v1"

type etcdraftCampaignModelInvoker func(
	context.Context, string, deepSeekPreparedRequest,
) (deepSeekCall, error)

func runEtcdraftModelCampaignOptIn(
	ctx context.Context,
	options etcdraftCampaignRunOptions,
	keyFile string,
	stdout io.Writer,
) error {
	client := defaultDeepSeekIntentClient()
	return runEtcdraftModelCampaign(
		ctx, options, keyFile, stdout, newEtcdraftCampaignProvider, readAgentKey, client,
		func(ctx context.Context, key string, prepared deepSeekPreparedRequest) (deepSeekCall, error) {
			return client.invokePrepared(ctx, key, prepared)
		},
	)
}

func runEtcdraftModelCampaign(
	ctx context.Context,
	options etcdraftCampaignRunOptions,
	keyFile string,
	stdout io.Writer,
	newProvider etcdraftCampaignProviderFactory,
	readKey agentKeyReader,
	client deepSeekIntentClient,
	invoke etcdraftCampaignModelInvoker,
) error {
	directory, summaryOut, observationOut, err := prepareEtcdraftCampaignPaths(options)
	if err != nil {
		return err
	}
	if stdout == nil || newProvider == nil || readKey == nil || invoke == nil || keyFile == "" ||
		options.Attempts <= 0 || options.DecisionsPerAttempt <= 0 || options.FirstPolicySeed == 0 ||
		options.WallClockCeilingMillis <= 0 || options.ModelTokensPerAttempt <= 0 {
		return errors.New("ETCDRAFT_CAMPAIGN_MODEL_RUNNER_OPTIONS_INVALID")
	}
	spec, err := newEtcdraftCampaignSpec(
		"etcdraft-model-campaign-spec-v1", options.DecisionsPerAttempt, options.FirstPolicySeed,
	)
	if err != nil {
		return err
	}
	base, err := newProvider(ctx, spec)
	if err != nil {
		return err
	}
	activeKey := ""
	provider, err := newEtcdraftDurableModelCampaignProvider(
		base, nil, client,
		func(ctx context.Context, prepared deepSeekPreparedRequest) (deepSeekCall, error) {
			if activeKey == "" {
				return deepSeekCall{}, errors.New("ETCDRAFT_CAMPAIGN_MODEL_KEY_NOT_ACTIVE")
			}
			return invoke(ctx, activeKey, prepared)
		},
	)
	if err != nil {
		return err
	}
	config, err := provider.campaignConfig(
		"etcdraft-model-campaign-v1", options.Attempts,
		options.ModelTokensPerAttempt, options.WallClockCeilingMillis,
	)
	if err != nil {
		return err
	}
	var recovered controlexperiment.CampaignRecovery
	if options.Resume {
		recovered, err = controlexperiment.RecoverCampaignDirectory(directory, config)
	} else {
		recovered, err = controlexperiment.CreateCampaignDirectory(directory, config)
	}
	if err != nil {
		return err
	}
	provider.planned.recovered = &recovered
	var stageErr error
	if recovered.Failure != nil {
		stageErr = errors.New("ETCDRAFT_CAMPAIGN_DURABLY_FAILED")
	} else {
		coordinator, coordinatorErr := controlexperiment.NewCampaignCoordinator(&recovered, provider)
		if coordinatorErr != nil {
			stageErr = coordinatorErr
		} else {
			for recovered.Head.StopReason == controlexperiment.CampaignStopRunning {
				request, requestErr := controlexperiment.NewCampaignAttemptRequest(config, recovered.Head)
				if requestErr != nil {
					stageErr = requestErr
					break
				}
				needsKey, freezeErr := provider.freezeNextCall(request)
				if freezeErr != nil {
					stageErr = freezeErr
					break
				}
				if needsKey {
					if stageErr = ctx.Err(); stageErr != nil {
						break
					}
					activeKey, stageErr = readKey(keyFile)
					if stageErr != nil || activeKey == "" {
						if stageErr == nil {
							stageErr = errors.New("ETCDRAFT_CAMPAIGN_MODEL_KEY_EMPTY")
						}
						activeKey = ""
						break
					}
				}
				_, stepErr := coordinator.Step(ctx)
				activeKey = ""
				if stepErr != nil {
					stageErr = stepErr
					break
				}
			}
		}
	}
	activeKey = ""
	return finishEtcdraftCampaignRun(
		&recovered, provider.planned.base, summaryOut, observationOut, stdout, stageErr,
	)
}

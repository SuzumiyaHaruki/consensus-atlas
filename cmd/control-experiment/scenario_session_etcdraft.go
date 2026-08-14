package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	etcdraftScenarioSessionStrategy  = "etcdraft-agent-session-v1"
	etcdraftScenarioSessionID        = "etcdraft-agent-session-v1"
	etcdraftScenarioSidecar          = "etcdraft-scenario"
	etcdraftScenarioCalibrationClass = "public-calibration-not-agent-effectiveness-holdout-or-correctness"
)

type etcdraftScenarioSessionOptions struct {
	Directory         string
	CorpusPath        string
	SemanticInputPath string
	Resume            bool
	AgentKeyFile      string
	SemanticExposure  controlexperiment.ScenarioSemanticExposureMode
	Client            openRouterIntentClient
	ReadKey           agentKeyReader
}

type etcdraftScenarioSessionSummary = scenarioSessionSummary

func runEtcdraftScenarioSession(
	ctx context.Context,
	options etcdraftScenarioSessionOptions,
) (etcdraftScenarioSessionSummary, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.CorpusPath == "" || options.SemanticInputPath == "" ||
		(options.SemanticExposure != "" && options.SemanticExposure.Validate() != nil) ||
		!validateAgentKeyFileName(options.AgentKeyFile) ||
		options.Client.HTTP == nil || options.ReadKey == nil ||
		openRouterTransportFreeze(options.Client).Validate() != nil {
		return etcdraftScenarioSessionSummary{}, errors.New("ETCDRAFT_SCENARIO_SESSION_OPTIONS_INVALID")
	}
	if options.Resume {
		summary, terminal, err := recoverTerminalScenarioSession(
			clean, etcdraftScenarioSessionID, etcdraftCampaignTargetID,
			etcdraftScenarioCalibrationClass, validateEtcdraftScenarioTesting,
		)
		if err != nil {
			return etcdraftScenarioSessionSummary{}, err
		}
		if terminal {
			if summary.Campaign.Status == controlexperiment.CampaignSummaryStatusFailed {
				return summary, errors.New("ETCDRAFT_SCENARIO_SESSION_DURABLY_FAILED")
			}
			return summary, nil
		}
	}
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, options.CorpusPath, options.SemanticInputPath, options.Client,
	)
	if err != nil {
		return etcdraftScenarioSessionSummary{}, err
	}
	if options.SemanticExposure != "" {
		inputs, err = overrideEtcdraftScenarioSemanticExposure(inputs, options.SemanticExposure)
		if err != nil {
			return etcdraftScenarioSessionSummary{}, err
		}
	}
	config, err := newEtcdraftScenarioSessionConfig(inputs)
	if err != nil {
		return etcdraftScenarioSessionSummary{}, err
	}
	var recovered controlexperiment.CampaignRecovery
	if options.Resume {
		recovered, err = controlexperiment.RecoverCampaignDirectory(clean, config)
	} else {
		recovered, err = controlexperiment.CreateCampaignDirectory(clean, config)
	}
	if err != nil {
		return etcdraftScenarioSessionSummary{}, err
	}
	if recovered.Failure != nil {
		summary, summaryErr := summarizeEtcdraftScenarioSession(&recovered, inputs.experiment.ScenarioSemanticExposure)
		if summaryErr != nil {
			return etcdraftScenarioSessionSummary{}, summaryErr
		}
		return summary, errors.New("ETCDRAFT_SCENARIO_SESSION_DURABLY_FAILED")
	}
	provider := controlexperiment.CampaignAttemptProviderFunc(func(
		attemptContext context.Context,
		request controlexperiment.CampaignAttemptRequest,
	) (controlexperiment.CampaignAttemptResult, error) {
		return executeEtcdraftScenarioSessionEpisode(
			attemptContext, request, clean, config, inputs, options,
		)
	})
	coordinator, err := controlexperiment.NewCampaignCoordinator(&recovered, provider)
	if err != nil {
		return etcdraftScenarioSessionSummary{}, err
	}
	_, runErr := coordinator.Run(ctx)
	summary, summaryErr := summarizeEtcdraftScenarioSession(&recovered, inputs.experiment.ScenarioSemanticExposure)
	if summaryErr != nil {
		return etcdraftScenarioSessionSummary{}, summaryErr
	}
	return summary, runErr
}

func overrideEtcdraftScenarioSemanticExposure(
	inputs etcdraftSemanticCalibrationInputs,
	mode controlexperiment.ScenarioSemanticExposureMode,
) (etcdraftSemanticCalibrationInputs, error) {
	if mode.Validate() != nil {
		return etcdraftSemanticCalibrationInputs{}, errors.New("ETCDRAFT_SCENARIO_SEMANTIC_OVERRIDE_INVALID")
	}
	inputs.experiment.ScenarioSemanticExposure = mode
	spec, err := newEtcdraftSemanticCalibrationSpec(
		inputs.campaign, inputs.root, inputs.frontier, inputs.riskSpec, inputs.knowledge,
		inputs.hypothesis, inputs.experiment, inputs.searchSpec, openRouterTransportFreeze(inputs.client),
	)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	inputs.spec = spec
	return inputs, nil
}

func newEtcdraftScenarioSessionConfig(
	inputs etcdraftSemanticCalibrationInputs,
) (controlexperiment.CampaignConfig, error) {
	experimentDigest, err := scenarioSessionExperimentDigest(
		struct {
			BaseSpecDigest string `json:"base_spec_digest"`
		}{BaseSpecDigest: inputs.spec.Digest},
		inputs.experiment.ScenarioMaxSteps,
		etcdraftSemanticPrefixProjectorID,
		etcdraftScenarioSemanticProjectorID,
	)
	if err != nil {
		return controlexperiment.CampaignConfig{}, err
	}
	return controlexperiment.NewCampaignConfig(
		etcdraftScenarioSessionID, etcdraftCampaignTargetID,
		inputs.campaign.source.Identity.ManifestDigest, experimentDigest,
		inputs.experiment.SessionBudget, inputs.experiment.SessionWallClockMS,
	)
}

func executeEtcdraftScenarioSessionEpisode(
	ctx context.Context,
	request controlexperiment.CampaignAttemptRequest,
	directory string,
	config controlexperiment.CampaignConfig,
	inputs etcdraftSemanticCalibrationInputs,
	options etcdraftScenarioSessionOptions,
) (controlexperiment.CampaignAttemptResult, error) {
	return executeScenarioSessionAttempt(ctx, request, scenarioSessionAttemptOptions{
		Directory: directory, Sidecar: etcdraftScenarioSidecar,
		CampaignID: etcdraftScenarioSessionID, TargetID: etcdraftCampaignTargetID,
		TargetIdentityDigest: config.TargetIdentityDigest,
		ExperimentSpecDigest: config.ExperimentSpecDigest,
		Classification:       etcdraftScenarioCalibrationClass,
		SemanticExposure:     inputs.experiment.ScenarioSemanticExposure,
		Client:               options.Client, AgentKeyFile: options.AgentKeyFile, ReadKey: options.ReadKey,
		ScenarioMaxCalls: inputs.experiment.ScenarioMaxCalls,
		Run: func(ctx context.Context, journal *scenarioAgentCallJournal, maxCalls int,
			activateKey func() error) (scenarioAgentEpisodeResult, error) {
			return runEtcdraftScenarioAgentEpisode(
				ctx, inputs, journal, maxCalls, inputs.experiment.ScenarioMaxSteps,
				inputs.experiment.ScenarioMaxDecisions, activateKey,
			)
		},
		Work: func(result scenarioAgentEpisodeResult, includeSource bool) controlexperiment.WorkLedger {
			var base *controlexperiment.WorkLedger
			if includeSource {
				base = &inputs.campaign.source.Work
			}
			return scenarioSessionWork(result, base)
		},
		ValidateTesting: validateEtcdraftScenarioTesting,
	})
}

func summarizeEtcdraftScenarioSession(
	recovered *controlexperiment.CampaignRecovery,
	exposure controlexperiment.ScenarioSemanticExposureMode,
) (etcdraftScenarioSessionSummary, error) {
	return summarizeScenarioSession(
		recovered, exposure, etcdraftScenarioCalibrationClass, validateEtcdraftScenarioTesting,
	)
}

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	agenticEpisodeRiskJournal     = "risk-agent"
	agenticEpisodeScenarioJournal = "scenario-agent"
)

type agenticEpisodeComposition struct {
	Target agenticEpisodeTarget
	Budget agenticEpisodeBudget
	Client openRouterIntentClient
}

type agenticEpisodeDirectoryOptions struct {
	Directory    string
	Resume       bool
	AgentKeyFile string
	ReadKey      agentKeyReader
	Recovery     agenticEpisodeRecoveryBinding
	Prepare      func(context.Context) (agenticEpisodeComposition, error)
}

func runAgenticEpisodeDirectory(
	ctx context.Context,
	options agenticEpisodeDirectoryOptions,
) (recoveredAgenticEpisode, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.Recovery.validate() != nil {
		return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_DIRECTORY_OPTIONS_INVALID")
	}
	info, statErr := os.Lstat(clean)
	if options.Resume {
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_RESUME_DIRECTORY_INVALID")
		}
		recovered, terminal, err := recoverAgenticEpisodeArtifacts(clean, options.Recovery)
		if err != nil || terminal {
			return recovered, err
		}
	} else if statErr == nil || !os.IsNotExist(statErr) {
		return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_NEW_DIRECTORY_REQUIRED")
	}
	if !validateAgentKeyFileName(options.AgentKeyFile) || options.ReadKey == nil || options.Prepare == nil {
		return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_ACTIVE_OPTIONS_INVALID")
	}
	composition, err := options.Prepare(ctx)
	if err != nil || composition.Target.validate() != nil || composition.Budget.validate() != nil ||
		composition.Client.HTTP == nil || openRouterTransportFreeze(composition.Client).Validate() != nil ||
		composition.Target.ID != options.Recovery.TargetID {
		return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_COMPOSITION_INVALID")
	}
	riskJournal, err := openAgenticRiskJournal(
		filepath.Join(clean, agenticEpisodeRiskJournal), options.Resume, composition.Client,
	)
	if err != nil {
		return recoveredAgenticEpisode{}, err
	}
	scenarioJournal, err := openAgenticScenarioJournal(
		filepath.Join(clean, agenticEpisodeScenarioJournal), options.Resume, composition.Client,
	)
	if err != nil {
		return recoveredAgenticEpisode{}, err
	}
	activateRiskKey := func() error {
		key, err := options.ReadKey(options.AgentKeyFile)
		if err != nil {
			return err
		}
		err = riskJournal.ActivateKey(key)
		key = ""
		return err
	}
	activateScenarioKey := func() error {
		key, err := options.ReadKey(options.AgentKeyFile)
		if err != nil {
			return err
		}
		err = scenarioJournal.ActivateKey(key)
		key = ""
		return err
	}
	result, err := runAgenticEpisode(
		ctx, composition.Target, riskJournal, scenarioJournal, composition.Budget,
		activateRiskKey, activateScenarioKey,
	)
	if err != nil {
		return recoveredAgenticEpisode{}, err
	}
	artifact, err := persistAgenticEpisodeArtifacts(
		clean, composition.Target.ID, composition.Budget, result,
	)
	if err != nil {
		return recoveredAgenticEpisode{}, err
	}
	recovered := recoveredAgenticEpisode{Summary: artifact}
	if result.Testing != nil {
		testing := *result.Testing
		recovered.Testing = &testing
	}
	return recovered, nil
}

func openAgenticRiskJournal(
	directory string,
	resume bool,
	client openRouterIntentClient,
) (*statelessAgentCallJournal, error) {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return newStatelessAgentCallJournal(directory, client, "")
	}
	if err != nil || !resume || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("AGENTIC_EPISODE_RISK_JOURNAL_INVALID")
	}
	return recoverStatelessAgentCallJournal(directory, client)
}

func openAgenticScenarioJournal(
	directory string,
	resume bool,
	client openRouterIntentClient,
) (*scenarioAgentCallJournal, error) {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return newScenarioAgentCallJournal(directory, client, "")
	}
	if err != nil || !resume || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("AGENTIC_EPISODE_SCENARIO_JOURNAL_INVALID")
	}
	return recoverScenarioAgentCallJournal(directory, client)
}

func agenticEpisodeBudgetFromExperiment(
	scenarioCalls int,
	maxPlanSteps int,
	maxRuntimeDecisions int,
	logical controlexperiment.CampaignLogicalBudget,
) (agenticEpisodeBudget, error) {
	riskCalls := logical.MaxModelCalls - scenarioCalls
	if riskCalls > controlexperiment.RiskAgentMaxCalls {
		riskCalls = controlexperiment.RiskAgentMaxCalls
	}
	budget := agenticEpisodeBudget{
		MaxRiskCalls: riskCalls, MaxScenarioCalls: scenarioCalls,
		MaxTotalCalls: logical.MaxModelCalls, MaxObservedTokens: logical.MaxModelTokens,
		MaxScenarioPlanSteps: maxPlanSteps, MaxRuntimeDecisions: maxRuntimeDecisions,
	}
	return budget, budget.validate()
}

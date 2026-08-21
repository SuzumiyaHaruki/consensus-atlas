package main

import (
	"context"
)

type omnipaxosAgenticEpisodeResult = agenticEpisodeResult

func runOmnipaxosAgenticEpisode(
	ctx context.Context,
	inputs omnipaxosAgenticEpisodeInputs,
	riskJournal *statelessAgentCallJournal,
	scenarioJournal *scenarioAgentCallJournal,
	budget agenticEpisodeBudget,
	activateRiskKey func() error,
	activateScenarioKey func() error,
) (omnipaxosAgenticEpisodeResult, error) {
	target, err := newOmnipaxosAgenticEpisodeTarget(inputs)
	if err != nil {
		return omnipaxosAgenticEpisodeResult{Status: agenticEpisodeRiskStopped}, err
	}
	return runAgenticEpisode(
		ctx, target, riskJournal, scenarioJournal, budget, nil, nil, nil,
		activateRiskKey, activateScenarioKey,
	)
}

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	omnipaxosScenarioCalibrationStrategy       = "omnipaxos-openrouter-scenario-a7c"
	omnipaxosScenarioCalibrationClass          = "public-integration-not-agent-effectiveness-holdout-or-correctness"
	omnipaxosScenarioCalibrationProviderFailed = "provider-failed"
)

type omnipaxosScenarioCalibrationRunOptions struct {
	Directory         string
	WorkerPath        string
	SemanticInputPath string
	Resume            bool
	AgentKeyFile      string
	Client            openRouterIntentClient
	ReadKey           agentKeyReader
}

func runOmnipaxosScenarioCalibration(
	ctx context.Context,
	options omnipaxosScenarioCalibrationRunOptions,
) (scenarioCalibrationSummary, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.WorkerPath == "" || options.SemanticInputPath == "" ||
		!validateAgentKeyFileName(options.AgentKeyFile) || options.Client.HTTP == nil ||
		options.ReadKey == nil || openRouterTransportFreeze(options.Client).Validate() != nil {
		return scenarioCalibrationSummary{}, errors.New("OMNIPAXOS_SCENARIO_CALIBRATION_OPTIONS_INVALID")
	}
	inputs, err := prepareOmnipaxosScenario(ctx, options.WorkerPath, options.SemanticInputPath)
	if err != nil {
		return scenarioCalibrationSummary{}, err
	}
	providerDirectory := filepath.Join(clean, "provider")
	var journal *scenarioAgentCallJournal
	if options.Resume {
		if err := validateScenarioCalibrationDirectory(clean); err != nil {
			return scenarioCalibrationSummary{}, err
		}
		journal, err = recoverScenarioAgentCallJournal(providerDirectory, options.Client)
	} else {
		if err := os.MkdirAll(filepath.Dir(clean), 0o700); err != nil {
			return scenarioCalibrationSummary{}, err
		}
		if err := os.Mkdir(clean, 0o700); err != nil {
			return scenarioCalibrationSummary{}, err
		}
		if err := syncStatelessAgentDirectory(filepath.Dir(clean)); err != nil {
			return scenarioCalibrationSummary{}, err
		}
		journal, err = newScenarioAgentCallJournal(providerDirectory, options.Client, "")
	}
	if err != nil {
		return scenarioCalibrationSummary{}, err
	}
	activateKey := func() error {
		key, readErr := options.ReadKey(options.AgentKeyFile)
		if readErr != nil {
			return readErr
		}
		activateErr := journal.ActivateKey(key)
		key = ""
		return activateErr
	}
	result, err := runOmnipaxosScenarioAgentEpisode(ctx, inputs, journal, activateKey)
	if err != nil {
		audits, auditErr := journal.Audits()
		if auditErr != nil || len(audits) == 0 ||
			audits[len(audits)-1].Status != controlexperiment.StatelessAgentCallFailed {
			return scenarioCalibrationSummary{}, err
		}
		failed := scenarioCalibrationSummary{
			Classification: omnipaxosScenarioCalibrationClass,
			Transport:      openRouterTransportFreeze(options.Client),
			AgentStatus:    omnipaxosScenarioCalibrationProviderFailed,
			Attempts:       len(audits), Feedback: []controlexperiment.ScenarioAgentFeedback{},
			ProviderCalls: audits,
		}
		for _, audit := range audits {
			addAgentModelWork(&failed.ModelWork, audit.Work)
		}
		if persistErr := persistScenarioCalibrationSummary(clean, options.Resume, failed); persistErr != nil {
			return scenarioCalibrationSummary{}, persistErr
		}
		return failed, err
	}
	summary := summarizeScenarioCalibration(
		omnipaxosScenarioCalibrationClass, options.Client, result,
	)
	if err := persistScenarioCalibrationSummary(clean, options.Resume, summary); err != nil {
		return scenarioCalibrationSummary{}, err
	}
	return summary, nil
}

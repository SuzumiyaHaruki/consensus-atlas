package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	etcdraftScenarioCalibrationStrategy       = "etcdraft-openrouter-scenario-a4c"
	etcdraftScenarioCalibrationClass          = "public-calibration-not-agent-effectiveness-holdout-or-correctness"
	etcdraftScenarioCalibrationProviderFailed = "provider-failed"
)

type etcdraftScenarioCalibrationRunOptions struct {
	Directory         string
	CorpusPath        string
	SemanticInputPath string
	Resume            bool
	AgentKeyFile      string
	Client            openRouterIntentClient
	ReadKey           agentKeyReader
}

type etcdraftScenarioCalibrationSummary = scenarioCalibrationSummary

type etcdraftScenarioCalibrationTestingSummary = scenarioCalibrationTestingSummary

func runEtcdraftScenarioCalibration(
	ctx context.Context,
	options etcdraftScenarioCalibrationRunOptions,
) (etcdraftScenarioCalibrationSummary, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.CorpusPath == "" || options.SemanticInputPath == "" ||
		!validateAgentKeyFileName(options.AgentKeyFile) ||
		options.Client.HTTP == nil || options.ReadKey == nil ||
		openRouterTransportFreeze(options.Client).Validate() != nil {
		return etcdraftScenarioCalibrationSummary{}, errors.New("ETCDRAFT_SCENARIO_CALIBRATION_OPTIONS_INVALID")
	}
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, options.CorpusPath, options.SemanticInputPath, options.Client,
	)
	if err != nil {
		return etcdraftScenarioCalibrationSummary{}, err
	}
	providerDirectory := filepath.Join(clean, "provider")
	var journal *scenarioAgentCallJournal
	if options.Resume {
		if err := validateScenarioCalibrationDirectory(clean); err != nil {
			return etcdraftScenarioCalibrationSummary{}, err
		}
		journal, err = recoverScenarioAgentCallJournal(providerDirectory, inputs.client)
	} else {
		if err := os.MkdirAll(filepath.Dir(clean), 0o700); err != nil {
			return etcdraftScenarioCalibrationSummary{}, err
		}
		if err := os.Mkdir(clean, 0o700); err != nil {
			return etcdraftScenarioCalibrationSummary{}, err
		}
		if err := syncStatelessAgentDirectory(filepath.Dir(clean)); err != nil {
			return etcdraftScenarioCalibrationSummary{}, err
		}
		journal, err = newScenarioAgentCallJournal(providerDirectory, inputs.client, "")
	}
	if err != nil {
		return etcdraftScenarioCalibrationSummary{}, err
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
	result, err := runEtcdraftScenarioAgentEpisode(
		ctx, inputs, journal, inputs.experiment.ScenarioMaxCalls,
		inputs.experiment.ScenarioMaxSteps, inputs.experiment.ScenarioMaxDecisions, activateKey,
	)
	if err != nil {
		audits, auditErr := journal.Audits()
		if auditErr != nil || len(audits) == 0 ||
			audits[len(audits)-1].Status != controlexperiment.StatelessAgentCallFailed {
			return etcdraftScenarioCalibrationSummary{}, err
		}
		failed := etcdraftScenarioCalibrationSummary{
			Classification: etcdraftScenarioCalibrationClass,
			Transport:      openRouterTransportFreeze(inputs.client),
			AgentStatus:    etcdraftScenarioCalibrationProviderFailed,
			Attempts:       len(audits),
			Feedback:       []controlexperiment.ScenarioAgentFeedback{},
			ProviderCalls:  audits,
		}
		for _, audit := range audits {
			addAgentModelWork(&failed.ModelWork, audit.Work)
		}
		if persistErr := persistScenarioCalibrationSummary(clean, options.Resume, failed); persistErr != nil {
			return etcdraftScenarioCalibrationSummary{}, persistErr
		}
		return failed, err
	}
	summary := summarizeEtcdraftScenarioCalibration(inputs.client, result)
	if err := persistScenarioCalibrationSummary(clean, options.Resume, summary); err != nil {
		return etcdraftScenarioCalibrationSummary{}, err
	}
	return summary, nil
}

func persistScenarioCalibrationSummary(
	directory string,
	resume bool,
	summary scenarioCalibrationSummary,
) error {
	summaryPath := filepath.Join(directory, "summary.json")
	if resume {
		var persisted scenarioCalibrationSummary
		if stat, err := os.Lstat(summaryPath); err == nil {
			if !stat.Mode().IsRegular() || stat.Mode()&os.ModeSymlink != 0 ||
				readStrictJSONFile(summaryPath, 1<<20, &persisted) != nil ||
				!reflect.DeepEqual(persisted, summary) {
				return errors.New("SCENARIO_CALIBRATION_SUMMARY_DRIFT")
			}
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return writeStatelessAgentJSON(directory, "summary.json", summary)
}

func summarizeEtcdraftScenarioCalibration(
	client openRouterIntentClient,
	result etcdraftScenarioEpisodeResult,
) etcdraftScenarioCalibrationSummary {
	return summarizeScenarioCalibration(etcdraftScenarioCalibrationClass, client, result)
}

func validateScenarioCalibrationDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("SCENARIO_CALIBRATION_DIRECTORY_INVALID")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) < 1 || len(entries) > 2 {
		return errors.New("SCENARIO_CALIBRATION_LAYOUT_INVALID")
	}
	providerSeen := false
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("SCENARIO_CALIBRATION_LAYOUT_INVALID")
		}
		switch entry.Name() {
		case "provider":
			providerSeen = entry.IsDir()
		case "summary.json":
			if entry.IsDir() {
				return errors.New("SCENARIO_CALIBRATION_LAYOUT_INVALID")
			}
		default:
			return errors.New("SCENARIO_CALIBRATION_LAYOUT_INVALID")
		}
	}
	if !providerSeen {
		return errors.New("SCENARIO_CALIBRATION_LAYOUT_INVALID")
	}
	return nil
}

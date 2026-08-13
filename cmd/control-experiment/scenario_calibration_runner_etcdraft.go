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

type etcdraftScenarioCalibrationSummary struct {
	Classification string                                      `json:"classification"`
	Transport      controlexperiment.AgentTransportFreeze      `json:"transport"`
	AgentStatus    string                                      `json:"agent_status"`
	Attempts       int                                         `json:"attempts"`
	Feedback       []controlexperiment.ScenarioAgentFeedback   `json:"feedback"`
	FinalPlan      *controlexperiment.ScenarioPlan             `json:"final_plan,omitempty"`
	ProviderCalls  []controlexperiment.StatelessAgentCallAudit `json:"provider_calls"`
	ModelWork      controlexperiment.ModelWork                 `json:"model_work"`
	Testing        *etcdraftScenarioCalibrationTestingSummary  `json:"testing,omitempty"`
}

type etcdraftScenarioCalibrationTestingSummary struct {
	Outcome             string   `json:"outcome"`
	TraceDigest         string   `json:"trace_digest"`
	BundleDigest        string   `json:"bundle_digest"`
	ReplayStable        bool     `json:"replay_stable"`
	CorePSSSamples      int      `json:"core_pss_samples"`
	UniqueCorePSSStates int      `json:"unique_core_pss_states"`
	RiskStatus          string   `json:"risk_status"`
	SatisfiedMilestones []string `json:"satisfied_milestones"`
	FirstMissing        string   `json:"first_missing_milestone,omitempty"`
	OracleViolations    int      `json:"oracle_violations"`
}

func runEtcdraftScenarioCalibration(
	ctx context.Context,
	options etcdraftScenarioCalibrationRunOptions,
) (etcdraftScenarioCalibrationSummary, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.CorpusPath == "" || options.SemanticInputPath == "" ||
		!validateEtcdraftSemanticRunKeyName(options.AgentKeyFile) ||
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
		if err := validateEtcdraftScenarioCalibrationDirectory(clean); err != nil {
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
		ctx, inputs, journal, nil, inputs.experiment.ScenarioMaxAttempts,
		inputs.experiment.ScenarioMaxSteps, activateKey,
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
			addEtcdraftSemanticModelWork(&failed.ModelWork, audit.Work)
		}
		if persistErr := persistEtcdraftScenarioCalibrationSummary(clean, options.Resume, failed); persistErr != nil {
			return etcdraftScenarioCalibrationSummary{}, persistErr
		}
		return failed, err
	}
	summary := summarizeEtcdraftScenarioCalibration(inputs.client, result)
	if err := persistEtcdraftScenarioCalibrationSummary(clean, options.Resume, summary); err != nil {
		return etcdraftScenarioCalibrationSummary{}, err
	}
	return summary, nil
}

func persistEtcdraftScenarioCalibrationSummary(
	directory string,
	resume bool,
	summary etcdraftScenarioCalibrationSummary,
) error {
	summaryPath := filepath.Join(directory, "summary.json")
	if resume {
		var persisted etcdraftScenarioCalibrationSummary
		if stat, err := os.Lstat(summaryPath); err == nil {
			if !stat.Mode().IsRegular() || stat.Mode()&os.ModeSymlink != 0 ||
				readStrictJSONFile(summaryPath, 1<<20, &persisted) != nil ||
				!reflect.DeepEqual(persisted, summary) {
				return errors.New("ETCDRAFT_SCENARIO_CALIBRATION_SUMMARY_DRIFT")
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
	summary := etcdraftScenarioCalibrationSummary{
		Classification: etcdraftScenarioCalibrationClass,
		Transport:      openRouterTransportFreeze(client),
		AgentStatus:    result.Agent.Status,
		Attempts:       len(result.Agent.Attempts),
		Feedback:       make([]controlexperiment.ScenarioAgentFeedback, 0, len(result.Agent.Attempts)),
		ProviderCalls:  append([]controlexperiment.StatelessAgentCallAudit(nil), result.ProviderCalls...),
		ModelWork:      result.Agent.ModelWork,
	}
	for _, attempt := range result.Agent.Attempts {
		summary.Feedback = append(summary.Feedback, attempt.Feedback)
	}
	if result.Agent.Execution != nil {
		for index := len(result.Agent.Attempts) - 1; index >= 0; index-- {
			if result.Agent.Attempts[index].Plan != nil {
				plan := *result.Agent.Attempts[index].Plan
				plan.Steps = append([]controlexperiment.ScenarioStep(nil), plan.Steps...)
				summary.FinalPlan = &plan
				break
			}
		}
	}
	if result.Testing != nil {
		firstMissing := ""
		if len(result.Testing.Risk.MissingMilestones) > 0 {
			firstMissing = result.Testing.Risk.MissingMilestones[0]
		}
		summary.Testing = &etcdraftScenarioCalibrationTestingSummary{
			Outcome:             result.Testing.Outcome,
			TraceDigest:         result.Testing.Bundle.Trace.Digest,
			BundleDigest:        result.Testing.Bundle.Digest,
			ReplayStable:        result.Testing.Replay.Stable,
			CorePSSSamples:      result.Testing.CorePSSSamples,
			UniqueCorePSSStates: result.Testing.UniqueCorePSSStates,
			RiskStatus:          result.Testing.Risk.Status,
			SatisfiedMilestones: append([]string(nil), result.Testing.Risk.SatisfiedMilestones...),
			FirstMissing:        firstMissing,
			OracleViolations:    len(result.Testing.Oracle.Violations),
		}
	}
	return summary
}

func validateEtcdraftScenarioCalibrationDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("ETCDRAFT_SCENARIO_CALIBRATION_DIRECTORY_INVALID")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) < 1 || len(entries) > 2 {
		return errors.New("ETCDRAFT_SCENARIO_CALIBRATION_LAYOUT_INVALID")
	}
	providerSeen := false
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("ETCDRAFT_SCENARIO_CALIBRATION_LAYOUT_INVALID")
		}
		switch entry.Name() {
		case "provider":
			providerSeen = entry.IsDir()
		case "summary.json":
			if entry.IsDir() {
				return errors.New("ETCDRAFT_SCENARIO_CALIBRATION_LAYOUT_INVALID")
			}
		default:
			return errors.New("ETCDRAFT_SCENARIO_CALIBRATION_LAYOUT_INVALID")
		}
	}
	if !providerSeen {
		return errors.New("ETCDRAFT_SCENARIO_CALIBRATION_LAYOUT_INVALID")
	}
	return nil
}

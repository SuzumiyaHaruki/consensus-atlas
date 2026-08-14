package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	omnipaxosScenarioSessionStrategy = "omnipaxos-openrouter-session-a7d"
	omnipaxosScenarioSessionID       = "omnipaxos-scenario-session-a7d"
	omnipaxosScenarioSessionTargetID = "omnipaxos-v2-agent"
	omnipaxosScenarioSidecar         = "omnipaxos-scenario"
)

type omnipaxosScenarioSessionOptions struct {
	Directory         string
	WorkerPath        string
	SemanticInputPath string
	Resume            bool
	AgentKeyFile      string
	Client            openRouterIntentClient
	ReadKey           agentKeyReader
}

func runOmnipaxosScenarioSession(
	ctx context.Context,
	options omnipaxosScenarioSessionOptions,
) (scenarioSessionSummary, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.WorkerPath == "" || options.SemanticInputPath == "" ||
		!validateAgentKeyFileName(options.AgentKeyFile) || options.Client.HTTP == nil ||
		options.ReadKey == nil || openRouterTransportFreeze(options.Client).Validate() != nil {
		return scenarioSessionSummary{}, errors.New("OMNIPAXOS_SCENARIO_SESSION_OPTIONS_INVALID")
	}
	if options.Resume {
		summary, terminal, err := recoverTerminalScenarioSession(
			clean, omnipaxosScenarioSessionID, omnipaxosScenarioSessionTargetID,
			omnipaxosScenarioCalibrationClass, validateOmnipaxosScenarioTesting,
		)
		if err != nil {
			return scenarioSessionSummary{}, err
		}
		if terminal {
			if summary.Campaign.Status == controlexperiment.CampaignSummaryStatusFailed {
				return summary, errors.New("OMNIPAXOS_SCENARIO_SESSION_DURABLY_FAILED")
			}
			return summary, nil
		}
	}
	inputs, err := prepareOmnipaxosScenario(ctx, options.WorkerPath, options.SemanticInputPath)
	if err != nil {
		return scenarioSessionSummary{}, err
	}
	config, err := newOmnipaxosScenarioSessionConfig(inputs, options.Client)
	if err != nil {
		return scenarioSessionSummary{}, err
	}
	var recovered controlexperiment.CampaignRecovery
	if options.Resume {
		recovered, err = controlexperiment.RecoverCampaignDirectory(clean, config)
	} else {
		recovered, err = controlexperiment.CreateCampaignDirectory(clean, config)
	}
	if err != nil {
		return scenarioSessionSummary{}, err
	}
	if recovered.Failure != nil {
		summary, summaryErr := summarizeScenarioSession(
			&recovered, inputs.Experiment.ScenarioSemanticExposure,
			omnipaxosScenarioCalibrationClass, validateOmnipaxosScenarioTesting,
		)
		if summaryErr != nil {
			return scenarioSessionSummary{}, summaryErr
		}
		return summary, errors.New("OMNIPAXOS_SCENARIO_SESSION_DURABLY_FAILED")
	}
	provider := controlexperiment.CampaignAttemptProviderFunc(func(
		attemptContext context.Context,
		request controlexperiment.CampaignAttemptRequest,
	) (controlexperiment.CampaignAttemptResult, error) {
		return executeScenarioSessionAttempt(attemptContext, request, scenarioSessionAttemptOptions{
			Directory: clean, Sidecar: omnipaxosScenarioSidecar,
			CampaignID: omnipaxosScenarioSessionID, TargetID: omnipaxosScenarioSessionTargetID,
			TargetIdentityDigest: config.TargetIdentityDigest,
			ExperimentSpecDigest: config.ExperimentSpecDigest,
			Classification:       omnipaxosScenarioCalibrationClass,
			SemanticExposure:     inputs.Experiment.ScenarioSemanticExposure,
			Client:               options.Client, AgentKeyFile: options.AgentKeyFile, ReadKey: options.ReadKey,
			ScenarioMaxCalls: inputs.Experiment.ScenarioMaxCalls,
			Run: func(ctx context.Context, journal *scenarioAgentCallJournal, maxCalls int,
				activateKey func() error) (scenarioAgentEpisodeResult, error) {
				return runOmnipaxosScenarioAgentEpisode(ctx, inputs, journal, maxCalls, activateKey)
			},
			Work: func(result scenarioAgentEpisodeResult, _ bool) controlexperiment.WorkLedger {
				return scenarioSessionWork(result, nil)
			},
			ValidateTesting: validateOmnipaxosScenarioTesting,
		})
	})
	coordinator, err := controlexperiment.NewCampaignCoordinator(&recovered, provider)
	if err != nil {
		return scenarioSessionSummary{}, err
	}
	_, runErr := coordinator.Run(ctx)
	summary, summaryErr := summarizeScenarioSession(
		&recovered, inputs.Experiment.ScenarioSemanticExposure,
		omnipaxosScenarioCalibrationClass, validateOmnipaxosScenarioTesting,
	)
	if summaryErr != nil {
		return scenarioSessionSummary{}, summaryErr
	}
	return summary, runErr
}

func newOmnipaxosScenarioSessionConfig(
	inputs omnipaxosScenarioInputs,
	client openRouterIntentClient,
) (controlexperiment.CampaignConfig, error) {
	if inputs.Knowledge.Validate() != nil || inputs.Experiment.validate() != nil ||
		inputs.Workload.Validate() != nil || inputs.Qualification.Admission.Validate() != nil ||
		openRouterTransportFreeze(client).Validate() != nil {
		return controlexperiment.CampaignConfig{}, errors.New("OMNIPAXOS_SCENARIO_SESSION_CONFIG_INPUT_INVALID")
	}
	workloadDigest, err := inputs.Workload.Digest()
	if err != nil {
		return controlexperiment.CampaignConfig{}, err
	}
	// A Campaign can reuse committed attempt artifacts without executing them.
	// This digest prevents changed authoring/runtime/provider inputs from being
	// mistaken for the original session during recovery.
	experimentDigest, err := control.CanonicalDigest(struct {
		KnowledgeDigest  string                                 `json:"knowledge_digest"`
		HypothesisDigest string                                 `json:"hypothesis_digest"`
		WorkloadDigest   string                                 `json:"workload_digest"`
		AdmissionDigest  string                                 `json:"admission_digest"`
		Experiment       omnipaxosScenarioExperimentConfig      `json:"experiment"`
		Transport        controlexperiment.AgentTransportFreeze `json:"transport"`
	}{
		KnowledgeDigest: inputs.Knowledge.Digest, HypothesisDigest: inputs.Hypothesis.Digest,
		WorkloadDigest: workloadDigest, AdmissionDigest: inputs.Qualification.Admission.Digest,
		Experiment: inputs.Experiment, Transport: openRouterTransportFreeze(client),
	})
	if err != nil {
		return controlexperiment.CampaignConfig{}, err
	}
	return controlexperiment.NewCampaignConfig(
		omnipaxosScenarioSessionID, omnipaxosScenarioSessionTargetID,
		inputs.Qualification.Admission.ManifestDigest, experimentDigest,
		inputs.Experiment.SessionBudget, inputs.Experiment.SessionWallClockMS,
	)
}

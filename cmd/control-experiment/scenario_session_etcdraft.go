package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	etcdraftScenarioSessionStrategy = "etcdraft-openrouter-session-a6a"
	etcdraftScenarioSessionID       = "etcdraft-scenario-session-a6a"
	etcdraftScenarioSidecar         = "etcdraft-scenario"
)

type etcdraftScenarioSessionOptions struct {
	Directory         string
	CorpusPath        string
	SemanticInputPath string
	Resume            bool
	AgentKeyFile      string
	Client            openRouterIntentClient
	ReadKey           agentKeyReader
}

// etcdraftScenarioSessionEpisodeArtifact extends the existing compact A4c
// summary only inside A6 Campaign attempts. State keys are trusted projections
// copied from the qualified bundle; they are terminal evidence, not feedback.
type etcdraftScenarioSessionEpisodeArtifact struct {
	Episode          etcdraftScenarioCalibrationSummary `json:"episode"`
	CorePSSStateKeys []string                           `json:"core_pss_state_keys,omitempty"`
}

// etcdraftScenarioSessionSummary is a derived terminal view. Campaign remains
// the durable ledger; this view is rebuilt from committed attempt artifacts.
type etcdraftScenarioSessionSummary struct {
	Campaign                controlexperiment.CampaignSummary `json:"campaign"`
	AgentCompletedEpisodes  int                               `json:"agent_completed_episodes"`
	AgentStoppedEpisodes    int                               `json:"agent_stopped_episodes"`
	TestingEpisodes         int                               `json:"testing_episodes"`
	ReplayStableEpisodes    int                               `json:"replay_stable_episodes"`
	CorePSSSamples          int                               `json:"core_pss_samples"`
	UniqueCorePSSStates     int                               `json:"unique_core_pss_states"`
	CorePSSStateKeys        []string                          `json:"core_pss_state_keys"`
	BestRiskEpisode         int                               `json:"best_risk_episode,omitempty"`
	BestRiskStatus          string                            `json:"best_risk_status,omitempty"`
	BestSatisfiedMilestones []string                          `json:"best_satisfied_milestones,omitempty"`
	BestFirstMissing        string                            `json:"best_first_missing_milestone,omitempty"`
	OracleViolations        int                               `json:"oracle_violations"`
}

func runEtcdraftScenarioSession(
	ctx context.Context,
	options etcdraftScenarioSessionOptions,
) (etcdraftScenarioSessionSummary, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.CorpusPath == "" || options.SemanticInputPath == "" ||
		!validateEtcdraftSemanticRunKeyName(options.AgentKeyFile) ||
		options.Client.HTTP == nil || options.ReadKey == nil ||
		openRouterTransportFreeze(options.Client).Validate() != nil {
		return etcdraftScenarioSessionSummary{}, errors.New("ETCDRAFT_SCENARIO_SESSION_OPTIONS_INVALID")
	}
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, options.CorpusPath, options.SemanticInputPath, options.Client,
	)
	if err != nil {
		return etcdraftScenarioSessionSummary{}, err
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
		summary, summaryErr := summarizeEtcdraftScenarioSession(&recovered)
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
			attemptContext, request, clean, &recovered, inputs, options,
		)
	})
	coordinator, err := controlexperiment.NewCampaignCoordinator(&recovered, provider)
	if err != nil {
		return etcdraftScenarioSessionSummary{}, err
	}
	_, runErr := coordinator.Run(ctx)
	summary, summaryErr := summarizeEtcdraftScenarioSession(&recovered)
	if summaryErr != nil {
		return etcdraftScenarioSessionSummary{}, summaryErr
	}
	return summary, runErr
}

func newEtcdraftScenarioSessionConfig(
	inputs etcdraftSemanticCalibrationInputs,
) (controlexperiment.CampaignConfig, error) {
	return controlexperiment.NewCampaignConfig(
		etcdraftScenarioSessionID, etcdraftCampaignTargetID,
		inputs.campaign.source.Identity.ManifestDigest, inputs.spec.Digest,
		inputs.experiment.SessionBudget, inputs.experiment.SessionWallClockMS,
	)
}

func executeEtcdraftScenarioSessionEpisode(
	ctx context.Context,
	request controlexperiment.CampaignAttemptRequest,
	directory string,
	recovered *controlexperiment.CampaignRecovery,
	inputs etcdraftSemanticCalibrationInputs,
	options etcdraftScenarioSessionOptions,
) (controlexperiment.CampaignAttemptResult, error) {
	if request.Validate() != nil || request.CampaignID != etcdraftScenarioSessionID ||
		request.TargetID != etcdraftCampaignTargetID ||
		request.TargetIdentityDigest != inputs.campaign.source.Identity.ManifestDigest ||
		request.ExperimentSpecDigest != inputs.spec.Digest {
		return controlexperiment.CampaignAttemptResult{}, errors.New("ETCDRAFT_SCENARIO_SESSION_REQUEST_INVALID")
	}
	prior, err := previousEtcdraftScenarioSessionFeedback(recovered, request.Ordinal)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	providerDirectory, err := controlexperiment.CampaignAttemptSidecarDirectory(
		directory, etcdraftScenarioSidecar, request.Ordinal,
	)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	var journal *scenarioAgentCallJournal
	if info, statErr := os.Lstat(providerDirectory); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return controlexperiment.CampaignAttemptResult{}, errors.New("ETCDRAFT_SCENARIO_SESSION_SIDECAR_INVALID")
		}
		journal, err = recoverScenarioAgentCallJournal(providerDirectory, inputs.client)
	} else if os.IsNotExist(statErr) {
		journal, err = newScenarioAgentCallJournal(providerDirectory, inputs.client, "")
	} else {
		err = statErr
	}
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
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
	maxAttempts := inputs.experiment.ScenarioMaxAttempts
	if request.Allowance.ModelCalls < maxAttempts {
		maxAttempts = request.Allowance.ModelCalls
	}
	if maxAttempts <= 0 {
		return controlexperiment.CampaignAttemptResult{}, errors.New("ETCDRAFT_SCENARIO_SESSION_MODEL_ALLOWANCE_EMPTY")
	}
	result, err := runEtcdraftScenarioAgentEpisode(
		ctx, inputs, journal, prior, maxAttempts, inputs.experiment.ScenarioMaxSteps, activateKey,
	)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	artifactValue, err := newEtcdraftScenarioSessionEpisodeArtifact(inputs.client, result)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	artifact, err := json.Marshal(artifactValue)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	return controlexperiment.CampaignAttemptResult{
		Outcome:  controlexperiment.CampaignAttemptCompleted,
		Work:     etcdraftScenarioSessionWork(result, request.Ordinal == 1, inputs),
		Artifact: artifact,
	}, nil
}

func previousEtcdraftScenarioSessionFeedback(
	recovered *controlexperiment.CampaignRecovery,
	ordinal int,
) (*controlexperiment.ScenarioAgentFeedback, error) {
	if ordinal == 1 {
		return nil, nil
	}
	if recovered == nil || ordinal <= 1 {
		return nil, errors.New("ETCDRAFT_SCENARIO_SESSION_PRIOR_INVALID")
	}
	encoded, err := recovered.ReadAttemptArtifact(ordinal - 1)
	if err != nil {
		return nil, err
	}
	var prior etcdraftScenarioSessionEpisodeArtifact
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&prior); err != nil {
		return nil, errors.New("ETCDRAFT_SCENARIO_SESSION_PRIOR_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF || prior.validate() != nil ||
		len(prior.Episode.Feedback) == 0 {
		return nil, errors.New("ETCDRAFT_SCENARIO_SESSION_PRIOR_INVALID")
	}
	feedback := prior.Episode.Feedback[len(prior.Episode.Feedback)-1]
	if !validEtcdraftScenarioInitialFeedback(&feedback, controlexperiment.ScenarioPlanMaxSteps) {
		return nil, errors.New("ETCDRAFT_SCENARIO_SESSION_PRIOR_INVALID")
	}
	return &feedback, nil
}

func newEtcdraftScenarioSessionEpisodeArtifact(
	client openRouterIntentClient,
	result etcdraftScenarioEpisodeResult,
) (etcdraftScenarioSessionEpisodeArtifact, error) {
	artifact := etcdraftScenarioSessionEpisodeArtifact{
		Episode: summarizeEtcdraftScenarioCalibration(client, result),
	}
	if result.Testing != nil {
		seen := make(map[string]bool, result.Testing.UniqueCorePSSStates)
		for _, sample := range result.Testing.Bundle.CorePSS {
			seen[sample.Key] = true
		}
		artifact.CorePSSStateKeys = make([]string, 0, len(seen))
		for key := range seen {
			artifact.CorePSSStateKeys = append(artifact.CorePSSStateKeys, key)
		}
		sort.Strings(artifact.CorePSSStateKeys)
	}
	if err := artifact.validate(); err != nil {
		return etcdraftScenarioSessionEpisodeArtifact{}, err
	}
	return artifact, nil
}

func (artifact etcdraftScenarioSessionEpisodeArtifact) validate() error {
	episode := artifact.Episode
	if episode.Classification != etcdraftScenarioCalibrationClass ||
		len(episode.Feedback) != episode.Attempts || len(episode.ProviderCalls) != episode.Attempts {
		return errors.New("ETCDRAFT_SCENARIO_SESSION_EPISODE_INVALID")
	}
	var modelWork controlexperiment.ModelWork
	for _, call := range episode.ProviderCalls {
		if call.Validate() != nil {
			return errors.New("ETCDRAFT_SCENARIO_SESSION_EPISODE_INVALID")
		}
		addEtcdraftSemanticModelWork(&modelWork, call.Work)
	}
	if modelWork != episode.ModelWork {
		return errors.New("ETCDRAFT_SCENARIO_SESSION_EPISODE_INVALID")
	}
	switch episode.AgentStatus {
	case controlexperiment.ScenarioAgentCompleted:
		if episode.Testing == nil || episode.FinalPlan == nil || !episode.Testing.ReplayStable ||
			episode.Testing.UniqueCorePSSStates != len(artifact.CorePSSStateKeys) ||
			episode.Testing.CorePSSSamples < episode.Testing.UniqueCorePSSStates ||
			episode.Testing.OracleViolations < 0 ||
			(episode.Testing.Outcome != etcdraftSemanticTestingPassed &&
				episode.Testing.Outcome != etcdraftSemanticTestingViolation) ||
			(episode.Testing.RiskStatus != semantic.RiskWitnessReached &&
				episode.Testing.RiskStatus != semantic.RiskWitnessNotReached) {
			return errors.New("ETCDRAFT_SCENARIO_SESSION_EPISODE_INVALID")
		}
	case controlexperiment.ScenarioAgentStopped:
		if episode.Testing != nil || episode.FinalPlan != nil || len(artifact.CorePSSStateKeys) != 0 {
			return errors.New("ETCDRAFT_SCENARIO_SESSION_EPISODE_INVALID")
		}
	default:
		return errors.New("ETCDRAFT_SCENARIO_SESSION_EPISODE_INVALID")
	}
	for index, key := range artifact.CorePSSStateKeys {
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 || index > 0 && artifact.CorePSSStateKeys[index-1] >= key {
			return errors.New("ETCDRAFT_SCENARIO_SESSION_PSS_KEYS_INVALID")
		}
	}
	return nil
}

func summarizeEtcdraftScenarioSession(
	recovered *controlexperiment.CampaignRecovery,
) (etcdraftScenarioSessionSummary, error) {
	campaign, err := controlexperiment.NewCampaignSummary(recovered)
	if err != nil {
		return etcdraftScenarioSessionSummary{}, err
	}
	summary := etcdraftScenarioSessionSummary{Campaign: campaign}
	states := make(map[string]bool)
	for ordinal := 1; ordinal <= campaign.Sequence; ordinal++ {
		encoded, err := recovered.ReadAttemptArtifact(ordinal)
		if err != nil {
			return etcdraftScenarioSessionSummary{}, err
		}
		var artifact etcdraftScenarioSessionEpisodeArtifact
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&artifact); err != nil || artifact.validate() != nil {
			return etcdraftScenarioSessionSummary{}, errors.New("ETCDRAFT_SCENARIO_SESSION_ARTIFACT_INVALID")
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return etcdraftScenarioSessionSummary{}, errors.New("ETCDRAFT_SCENARIO_SESSION_ARTIFACT_INVALID")
		}
		switch artifact.Episode.AgentStatus {
		case controlexperiment.ScenarioAgentCompleted:
			summary.AgentCompletedEpisodes++
		case controlexperiment.ScenarioAgentStopped:
			summary.AgentStoppedEpisodes++
		}
		if artifact.Episode.Testing == nil {
			continue
		}
		testing := artifact.Episode.Testing
		summary.TestingEpisodes++
		if testing.ReplayStable {
			summary.ReplayStableEpisodes++
		}
		summary.CorePSSSamples += testing.CorePSSSamples
		summary.OracleViolations += testing.OracleViolations
		for _, key := range artifact.CorePSSStateKeys {
			states[key] = true
		}
		if betterEtcdraftScenarioRisk(summary, ordinal, testing) {
			summary.BestRiskEpisode = ordinal
			summary.BestRiskStatus = testing.RiskStatus
			summary.BestSatisfiedMilestones = append([]string(nil), testing.SatisfiedMilestones...)
			summary.BestFirstMissing = testing.FirstMissing
		}
	}
	summary.CorePSSStateKeys = make([]string, 0, len(states))
	for key := range states {
		summary.CorePSSStateKeys = append(summary.CorePSSStateKeys, key)
	}
	sort.Strings(summary.CorePSSStateKeys)
	summary.UniqueCorePSSStates = len(summary.CorePSSStateKeys)
	return summary, nil
}

func betterEtcdraftScenarioRisk(
	summary etcdraftScenarioSessionSummary,
	ordinal int,
	testing *etcdraftScenarioCalibrationTestingSummary,
) bool {
	if testing == nil || ordinal <= 0 {
		return false
	}
	if summary.BestRiskEpisode == 0 {
		return true
	}
	if testing.RiskStatus == semantic.RiskWitnessReached &&
		summary.BestRiskStatus != semantic.RiskWitnessReached {
		return true
	}
	return testing.RiskStatus == summary.BestRiskStatus &&
		len(testing.SatisfiedMilestones) > len(summary.BestSatisfiedMilestones)
}

func etcdraftScenarioSessionWork(
	result etcdraftScenarioEpisodeResult,
	includeSource bool,
	inputs etcdraftSemanticCalibrationInputs,
) controlexperiment.WorkLedger {
	work := controlexperiment.WorkLedger{Resources: controlexperiment.ResourceAccounting{
		WallTime: controlexperiment.ResourceNotCollected,
		CPUTime:  controlexperiment.ResourceNotCollected,
		PeakRSS:  controlexperiment.ResourceNotCollected,
	}}
	if includeSource {
		addEtcdraftScenarioSessionLedger(&work, inputs.campaign.source.Work)
	}
	addEtcdraftScenarioSessionPhase(&work.Primary, result.FrontierWork)
	addEtcdraftScenarioSessionPhase(&work.Primary, result.Agent.ExecutionWork.FrontierReconstruction)
	addEtcdraftScenarioSessionPhase(&work.Primary, result.Agent.ExecutionWork.ChildMaterialization)
	addEtcdraftScenarioSessionPhase(&work.Primary, result.Agent.ExecutionWork.ChildVerification)
	if result.Testing != nil {
		addEtcdraftScenarioSessionLedger(&work, result.Testing.Bundle.Work)
	}
	work.Model = result.Agent.ModelWork
	return work
}

func addEtcdraftScenarioSessionLedger(
	total *controlexperiment.WorkLedger,
	delta controlexperiment.WorkLedger,
) {
	addEtcdraftScenarioSessionPhase(&total.Primary, delta.Primary)
	addEtcdraftScenarioSessionPhase(&total.Replay, delta.Replay)
}

func addEtcdraftScenarioSessionPhase(
	total *controlexperiment.PhaseWork,
	delta controlexperiment.PhaseWork,
) {
	total.SetupAttempts += delta.SetupAttempts
	total.RuntimeInitializations += delta.RuntimeInitializations
	total.PrepareActions += delta.PrepareActions
	total.SchedulerDecisions += delta.SchedulerDecisions
	total.WorkUnits = total.SetupAttempts + total.PrepareActions + total.SchedulerDecisions
}

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

// scenarioSessionEpisodeArtifact is the protocol-neutral terminal evidence
// committed by a Campaign attempt. The PSS keys come only from the qualified
// execution bundle and are never returned to a later planning episode.
type scenarioSessionEpisodeArtifact struct {
	Episode          scenarioCalibrationSummary `json:"episode"`
	CorePSSStateKeys []string                   `json:"core_pss_state_keys,omitempty"`
}

// scenarioSessionSummary is rebuilt from committed attempt artifacts. The
// Campaign recovery ledger remains the source of truth.
type scenarioSessionSummary struct {
	Campaign                controlexperiment.CampaignSummary              `json:"campaign"`
	SemanticExposure        controlexperiment.ScenarioSemanticExposureMode `json:"semantic_exposure"`
	AgentCompletedEpisodes  int                                            `json:"agent_completed_episodes"`
	AgentStoppedEpisodes    int                                            `json:"agent_stopped_episodes"`
	TestingEpisodes         int                                            `json:"testing_episodes"`
	ReplayStableEpisodes    int                                            `json:"replay_stable_episodes"`
	CorePSSSamples          int                                            `json:"core_pss_samples"`
	UniqueCorePSSStates     int                                            `json:"unique_core_pss_states"`
	CorePSSStateKeys        []string                                       `json:"core_pss_state_keys"`
	BestRiskEpisode         int                                            `json:"best_risk_episode,omitempty"`
	BestRiskStatus          string                                         `json:"best_risk_status,omitempty"`
	BestSatisfiedMilestones []string                                       `json:"best_satisfied_milestones,omitempty"`
	BestFirstMissing        string                                         `json:"best_first_missing_milestone,omitempty"`
	OracleViolations        int                                            `json:"oracle_violations"`
}

type scenarioSessionAttemptOptions struct {
	Directory            string
	Sidecar              string
	CampaignID           string
	TargetID             string
	TargetIdentityDigest string
	ExperimentSpecDigest string
	Classification       string
	Client               openRouterIntentClient
	AgentKeyFile         string
	ReadKey              agentKeyReader
	ScenarioMaxCalls     int
	Run                  func(context.Context, *scenarioAgentCallJournal, int, func() error) (scenarioAgentEpisodeResult, error)
	Work                 func(scenarioAgentEpisodeResult, bool) controlexperiment.WorkLedger
}

func executeScenarioSessionAttempt(
	ctx context.Context,
	request controlexperiment.CampaignAttemptRequest,
	options scenarioSessionAttemptOptions,
) (controlexperiment.CampaignAttemptResult, error) {
	if request.Validate() != nil || request.CampaignID != options.CampaignID ||
		request.TargetID != options.TargetID ||
		request.TargetIdentityDigest != options.TargetIdentityDigest ||
		request.ExperimentSpecDigest != options.ExperimentSpecDigest ||
		options.Directory == "" || options.Sidecar == "" || options.Classification == "" ||
		options.Client.HTTP == nil || !validateAgentKeyFileName(options.AgentKeyFile) ||
		options.ReadKey == nil || options.ScenarioMaxCalls <= 0 || options.Run == nil || options.Work == nil {
		return controlexperiment.CampaignAttemptResult{}, errors.New("SCENARIO_SESSION_REQUEST_INVALID")
	}
	providerDirectory, err := controlexperiment.CampaignAttemptSidecarDirectory(
		options.Directory, options.Sidecar, request.Ordinal,
	)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	var journal *scenarioAgentCallJournal
	if info, statErr := os.Lstat(providerDirectory); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return controlexperiment.CampaignAttemptResult{}, errors.New("SCENARIO_SESSION_SIDECAR_INVALID")
		}
		journal, err = recoverScenarioAgentCallJournal(providerDirectory, options.Client)
	} else if os.IsNotExist(statErr) {
		journal, err = newScenarioAgentCallJournal(providerDirectory, options.Client, "")
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
	maxCalls := options.ScenarioMaxCalls
	if request.Allowance.ModelCalls < maxCalls {
		maxCalls = request.Allowance.ModelCalls
	}
	if maxCalls <= 0 {
		return controlexperiment.CampaignAttemptResult{}, errors.New("SCENARIO_SESSION_MODEL_ALLOWANCE_EMPTY")
	}
	result, err := options.Run(ctx, journal, maxCalls, activateKey)
	if err != nil {
		modelWork, auditErr := scenarioSessionAuditedModelWork(result.ProviderCalls)
		if auditErr != nil {
			return controlexperiment.CampaignAttemptResult{}, err
		}
		result.Agent.ModelWork = modelWork
		return controlexperiment.CampaignAttemptResult{
			Work: options.Work(result, request.Ordinal == 1),
		}, err
	}
	artifactValue, err := newScenarioSessionEpisodeArtifact(
		options.Classification, options.Client, result,
	)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	artifact, err := json.Marshal(artifactValue)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	return controlexperiment.CampaignAttemptResult{
		Outcome:  controlexperiment.CampaignAttemptCompleted,
		Work:     options.Work(result, request.Ordinal == 1),
		Artifact: artifact,
	}, nil
}

func newScenarioSessionEpisodeArtifact(
	classification string,
	client openRouterIntentClient,
	result scenarioAgentEpisodeResult,
) (scenarioSessionEpisodeArtifact, error) {
	artifact := scenarioSessionEpisodeArtifact{
		Episode: summarizeScenarioCalibration(classification, client, result),
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
	if err := artifact.validate(classification); err != nil {
		return scenarioSessionEpisodeArtifact{}, err
	}
	return artifact, nil
}

func (artifact scenarioSessionEpisodeArtifact) validate(classification string) error {
	episode := artifact.Episode
	if classification == "" || episode.Classification != classification ||
		len(episode.Feedback) != episode.Attempts || len(episode.ProviderCalls) != episode.Attempts {
		return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
	}
	modelWork, err := scenarioSessionAuditedModelWork(episode.ProviderCalls)
	if err != nil || modelWork != episode.ModelWork {
		return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
	}
	switch episode.AgentStatus {
	case controlexperiment.ScenarioAgentCompleted:
		if episode.Testing == nil || episode.FinalPlan == nil || !episode.Testing.ReplayStable ||
			episode.Testing.UniqueCorePSSStates != len(artifact.CorePSSStateKeys) ||
			episode.Testing.CorePSSSamples < episode.Testing.UniqueCorePSSStates ||
			episode.Testing.OracleViolations < 0 ||
			(episode.Testing.Outcome != scenarioTestingPassed &&
				episode.Testing.Outcome != scenarioTestingViolation) ||
			(episode.Testing.RiskStatus != semantic.RiskWitnessReached &&
				episode.Testing.RiskStatus != semantic.RiskWitnessNotReached) {
			return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
		}
	case controlexperiment.ScenarioAgentStopped:
		if episode.Testing != nil || episode.FinalPlan != nil || len(artifact.CorePSSStateKeys) != 0 {
			return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
		}
	default:
		return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
	}
	for index, key := range artifact.CorePSSStateKeys {
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 || index > 0 && artifact.CorePSSStateKeys[index-1] >= key {
			return errors.New("SCENARIO_SESSION_PSS_KEYS_INVALID")
		}
	}
	return nil
}

func summarizeScenarioSession(
	recovered *controlexperiment.CampaignRecovery,
	exposure controlexperiment.ScenarioSemanticExposureMode,
	classification string,
) (scenarioSessionSummary, error) {
	if exposure.Validate() != nil || classification == "" {
		return scenarioSessionSummary{}, errors.New("SCENARIO_SESSION_SUMMARY_INPUT_INVALID")
	}
	campaign, err := controlexperiment.NewCampaignSummary(recovered)
	if err != nil {
		return scenarioSessionSummary{}, err
	}
	summary := scenarioSessionSummary{Campaign: campaign, SemanticExposure: exposure}
	states := make(map[string]bool)
	for ordinal := 1; ordinal <= campaign.Sequence; ordinal++ {
		encoded, err := recovered.ReadAttemptArtifact(ordinal)
		if err != nil {
			return scenarioSessionSummary{}, err
		}
		var artifact scenarioSessionEpisodeArtifact
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&artifact); err != nil || artifact.validate(classification) != nil {
			return scenarioSessionSummary{}, errors.New("SCENARIO_SESSION_ARTIFACT_INVALID")
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return scenarioSessionSummary{}, errors.New("SCENARIO_SESSION_ARTIFACT_INVALID")
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
		if betterScenarioSessionRisk(summary, ordinal, testing) {
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

func betterScenarioSessionRisk(
	summary scenarioSessionSummary,
	ordinal int,
	testing *scenarioCalibrationTestingSummary,
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

func scenarioSessionWork(
	result scenarioAgentEpisodeResult,
	base *controlexperiment.WorkLedger,
) controlexperiment.WorkLedger {
	work := controlexperiment.WorkLedger{Resources: controlexperiment.ResourceAccounting{
		WallTime: controlexperiment.ResourceNotCollected,
		CPUTime:  controlexperiment.ResourceNotCollected,
		PeakRSS:  controlexperiment.ResourceNotCollected,
	}}
	if base != nil {
		addScenarioSessionLedger(&work, *base)
	}
	addScenarioSessionPhase(&work.Primary, result.FrontierWork)
	addScenarioSessionPhase(&work.Primary, result.Agent.ExecutionWork.FrontierReconstruction)
	addScenarioSessionPhase(&work.Primary, result.Agent.ExecutionWork.ChildMaterialization)
	addScenarioSessionPhase(&work.Primary, result.Agent.ExecutionWork.ChildVerification)
	if result.Testing != nil {
		addScenarioSessionLedger(&work, result.Testing.Bundle.Work)
	}
	work.Model = result.Agent.ModelWork
	return work
}

func scenarioSessionAuditedModelWork(
	audits []controlexperiment.StatelessAgentCallAudit,
) (controlexperiment.ModelWork, error) {
	var work controlexperiment.ModelWork
	for index, audit := range audits {
		if audit.Validate() != nil || audit.Ordinal != index+1 {
			return controlexperiment.ModelWork{}, errors.New("SCENARIO_SESSION_CALL_AUDIT_INVALID")
		}
		addAgentModelWork(&work, audit.Work)
	}
	return work, nil
}

func addScenarioSessionLedger(total *controlexperiment.WorkLedger, delta controlexperiment.WorkLedger) {
	addScenarioSessionPhase(&total.Primary, delta.Primary)
	addScenarioSessionPhase(&total.Replay, delta.Replay)
}

func addScenarioSessionPhase(total *controlexperiment.PhaseWork, delta controlexperiment.PhaseWork) {
	total.SetupAttempts += delta.SetupAttempts
	total.RuntimeInitializations += delta.RuntimeInitializations
	total.PrepareActions += delta.PrepareActions
	total.SchedulerDecisions += delta.SchedulerDecisions
	total.WorkUnits = total.SetupAttempts + total.PrepareActions + total.SchedulerDecisions
}

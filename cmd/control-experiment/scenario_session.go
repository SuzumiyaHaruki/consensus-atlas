package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

// scenarioSessionEpisodeArtifact is the protocol-neutral terminal evidence
// committed by a Campaign attempt. The PSS keys come only from the qualified
// execution bundle and are never returned to a later planning episode.
type scenarioSessionEpisodeArtifact struct {
	Episode          scenarioCalibrationSummary                     `json:"episode"`
	SemanticExposure controlexperiment.ScenarioSemanticExposureMode `json:"semantic_exposure"`
	Testing          *scenarioTestingResult                         `json:"testing_evidence,omitempty"`
}

type scenarioTestingRevalidator func(scenarioTestingResult) error

type scenarioSessionMethodIdentity struct {
	PromptVersion           string               `json:"prompt_version"`
	StructuredOutputName    string               `json:"structured_output_name"`
	StructuredOutputSchema  json.RawMessage      `json:"structured_output_schema"`
	NaturalProgressPriority []control.ActionKind `json:"natural_progress_priority"`
	RiskProjectorID         string               `json:"risk_projector_id"`
	SemanticProjectorID     string               `json:"semantic_projector_id"`
}

func newScenarioSessionMethodIdentity(
	maxSteps int,
	riskProjectorID string,
	semanticProjectorID string,
) (scenarioSessionMethodIdentity, error) {
	if riskProjectorID == "" || semanticProjectorID == "" {
		return scenarioSessionMethodIdentity{}, errors.New("SCENARIO_SESSION_METHOD_IDENTITY_INVALID")
	}
	output, err := scenarioPlanStructuredOutput(maxSteps)
	if err != nil {
		return scenarioSessionMethodIdentity{}, err
	}
	return scenarioSessionMethodIdentity{
		PromptVersion:           scenarioAgentPromptVersion,
		StructuredOutputName:    output.Name,
		StructuredOutputSchema:  append(json.RawMessage(nil), output.Schema...),
		NaturalProgressPriority: controlexperiment.ScenarioNaturalProgressPriority(),
		RiskProjectorID:         riskProjectorID,
		SemanticProjectorID:     semanticProjectorID,
	}, nil
}

func scenarioSessionExperimentDigest(
	base any,
	maxSteps int,
	riskProjectorID string,
	semanticProjectorID string,
) (string, error) {
	if base == nil {
		return "", errors.New("SCENARIO_SESSION_EXPERIMENT_BASE_REQUIRED")
	}
	method, err := newScenarioSessionMethodIdentity(maxSteps, riskProjectorID, semanticProjectorID)
	if err != nil {
		return "", err
	}
	return control.CanonicalDigest(struct {
		Base   any                           `json:"base"`
		Method scenarioSessionMethodIdentity `json:"scenario_method"`
	}{Base: base, Method: method})
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
	SemanticExposure     controlexperiment.ScenarioSemanticExposureMode
	Client               openRouterIntentClient
	AgentKeyFile         string
	ReadKey              agentKeyReader
	ScenarioMaxCalls     int
	Run                  func(context.Context, *scenarioAgentCallJournal, int, func() error) (scenarioAgentEpisodeResult, error)
	Work                 func(scenarioAgentEpisodeResult, bool) controlexperiment.WorkLedger
	ValidateTesting      scenarioTestingRevalidator
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
		options.SemanticExposure.Validate() != nil ||
		options.Client.HTTP == nil || !validateAgentKeyFileName(options.AgentKeyFile) ||
		options.ReadKey == nil || options.ScenarioMaxCalls <= 0 || options.Run == nil || options.Work == nil ||
		options.ValidateTesting == nil {
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
		options.Classification, options.SemanticExposure, options.Client, result, options.ValidateTesting,
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
	exposure controlexperiment.ScenarioSemanticExposureMode,
	client openRouterIntentClient,
	result scenarioAgentEpisodeResult,
	revalidate scenarioTestingRevalidator,
) (scenarioSessionEpisodeArtifact, error) {
	artifact := scenarioSessionEpisodeArtifact{
		Episode: summarizeScenarioCalibration(classification, client, result), SemanticExposure: exposure,
	}
	if result.Testing != nil {
		testing := *result.Testing
		artifact.Testing = &testing
	}
	if err := artifact.validate(classification, revalidate); err != nil {
		return scenarioSessionEpisodeArtifact{}, err
	}
	return artifact, nil
}

func (artifact scenarioSessionEpisodeArtifact) validate(
	classification string,
	revalidate scenarioTestingRevalidator,
) error {
	episode := artifact.Episode
	if classification == "" || artifact.SemanticExposure.Validate() != nil || revalidate == nil ||
		episode.Classification != classification ||
		len(episode.Feedback) != episode.Attempts || len(episode.ProviderCalls) != episode.Attempts {
		return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
	}
	modelWork, err := scenarioSessionAuditedModelWork(episode.ProviderCalls)
	if err != nil || modelWork != episode.ModelWork {
		return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
	}
	switch episode.AgentStatus {
	case controlexperiment.ScenarioAgentCompleted:
		if artifact.Testing == nil || revalidate(*artifact.Testing) != nil {
			return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
		}
		bundle := &artifact.Testing.Bundle
		keys, bundleErr := scenarioBundleStateKeys(bundle)
		if bundleErr != nil || episode.Testing == nil || episode.FinalPlan == nil ||
			episode.Testing.TraceDigest != bundle.Trace.Digest ||
			episode.Testing.BundleDigest != bundle.Digest ||
			episode.Testing.ReplayStable != bundle.Run.Replay.Stable ||
			episode.Testing.CorePSSSamples != len(bundle.CorePSS) ||
			episode.Testing.UniqueCorePSSStates != len(keys) || !episode.Testing.ReplayStable ||
			episode.Testing.RiskStatus != artifact.Testing.Risk.Status ||
			!reflect.DeepEqual(episode.Testing.SatisfiedMilestones, artifact.Testing.Risk.SatisfiedMilestones) ||
			episode.Testing.OracleViolations != len(artifact.Testing.Oracle.Violations) ||
			episode.Testing.Outcome != artifact.Testing.Outcome ||
			episode.Testing.CorePSSSamples < episode.Testing.UniqueCorePSSStates ||
			episode.Testing.OracleViolations < 0 ||
			(episode.Testing.Outcome != scenarioTestingPassed &&
				episode.Testing.Outcome != scenarioTestingViolation) ||
			(episode.Testing.RiskStatus != semantic.RiskWitnessReached &&
				episode.Testing.RiskStatus != semantic.RiskWitnessNotReached) {
			return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
		}
		firstMissing := ""
		if len(artifact.Testing.Risk.MissingMilestones) > 0 {
			firstMissing = artifact.Testing.Risk.MissingMilestones[0]
		}
		if episode.Testing.FirstMissing != firstMissing {
			return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
		}
	case controlexperiment.ScenarioAgentStopped:
		if episode.Testing != nil || episode.FinalPlan != nil || artifact.Testing != nil {
			return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
		}
	default:
		return errors.New("SCENARIO_SESSION_EPISODE_INVALID")
	}
	return nil
}

func scenarioBundleStateKeys(bundle *controlexperiment.ExecutionBundle) ([]string, error) {
	if bundle == nil || bundle.Validate() != nil {
		return nil, errors.New("SCENARIO_SESSION_BUNDLE_INVALID")
	}
	seen := make(map[string]bool, len(bundle.CorePSS))
	for _, sample := range bundle.CorePSS {
		seen[sample.Key] = true
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for index, key := range keys {
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 || index > 0 && keys[index-1] >= key {
			return nil, errors.New("SCENARIO_SESSION_PSS_KEYS_INVALID")
		}
	}
	return keys, nil
}

func summarizeScenarioSession(
	recovered *controlexperiment.CampaignRecovery,
	exposure controlexperiment.ScenarioSemanticExposureMode,
	classification string,
	revalidate scenarioTestingRevalidator,
) (scenarioSessionSummary, error) {
	if exposure.Validate() != nil || classification == "" || revalidate == nil {
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
		artifact, err := decodeScenarioSessionArtifact(encoded, classification, revalidate)
		if err != nil || artifact.SemanticExposure != exposure ||
			artifact.Testing != nil &&
				artifact.Testing.Bundle.Identity.ManifestDigest != recovered.Config.TargetIdentityDigest {
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
		keys, err := scenarioBundleStateKeys(&artifact.Testing.Bundle)
		if err != nil {
			return scenarioSessionSummary{}, err
		}
		for _, key := range keys {
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

func decodeScenarioSessionArtifact(
	encoded []byte,
	classification string,
	revalidate scenarioTestingRevalidator,
) (scenarioSessionEpisodeArtifact, error) {
	var artifact scenarioSessionEpisodeArtifact
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil || artifact.validate(classification, revalidate) != nil {
		return scenarioSessionEpisodeArtifact{}, errors.New("SCENARIO_SESSION_ARTIFACT_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return scenarioSessionEpisodeArtifact{}, errors.New("SCENARIO_SESSION_ARTIFACT_INVALID")
	}
	return artifact, nil
}

func recoverTerminalScenarioSession(
	directory string,
	campaignID string,
	targetID string,
	classification string,
	revalidate scenarioTestingRevalidator,
) (scenarioSessionSummary, bool, error) {
	recovered, err := controlexperiment.RecoverStoredCampaignDirectory(directory)
	if err != nil {
		return scenarioSessionSummary{}, false, err
	}
	if recovered.Config.ID != campaignID || recovered.Config.TargetID != targetID {
		return scenarioSessionSummary{}, false, errors.New("SCENARIO_SESSION_STORED_IDENTITY_MISMATCH")
	}
	if recovered.Failure == nil && recovered.Head.StopReason == controlexperiment.CampaignStopRunning {
		return scenarioSessionSummary{}, false, nil
	}
	if recovered.Head.Sequence == 0 {
		return scenarioSessionSummary{}, false, errors.New("SCENARIO_SESSION_STORED_EVIDENCE_MISSING")
	}
	encoded, err := recovered.ReadAttemptArtifact(1)
	if err != nil {
		return scenarioSessionSummary{}, false, err
	}
	first, err := decodeScenarioSessionArtifact(encoded, classification, revalidate)
	if err != nil {
		return scenarioSessionSummary{}, false, err
	}
	summary, err := summarizeScenarioSession(
		&recovered, first.SemanticExposure, classification, revalidate,
	)
	if err != nil {
		return scenarioSessionSummary{}, false, err
	}
	return summary, true, nil
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
	addScenarioSessionPhase(&work.Replay, result.Agent.ExecutionWork.ChildVerification)
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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	etcdraftA8PairedScenarioStrategy = "etcdraft-a8-paired-scenario-v1"
	etcdraftA8DeterministicSessionID = "etcdraft-a8-deterministic-scenario-v1"
	etcdraftA8DeterministicClass     = "public-calibration-deterministic-scenario-control"
	etcdraftA8PairClass              = "public-calibration-not-agent-effectiveness-holdout-or-correctness"
	etcdraftA8FreshRootMode          = "sut-local-fresh"
)

type etcdraftA8PairedScenarioOptions struct {
	Directory         string
	CorpusPath        string
	SemanticInputPath string
	Resume            bool
	AgentKeyFile      string
	Client            openRouterIntentClient
	ReadKey           agentKeyReader
}

type etcdraftA8ScenarioArm struct {
	Planner             string                                  `json:"planner"`
	CampaignID          string                                  `json:"campaign_id"`
	Budget              controlexperiment.CampaignLogicalBudget `json:"budget"`
	PrimaryWorkUnits    int                                     `json:"primary_work_units"`
	ReplayWorkUnits     int                                     `json:"replay_work_units"`
	ModelCalls          int                                     `json:"model_calls"`
	ModelTokens         int                                     `json:"model_tokens"`
	TraceDigest         string                                  `json:"trace_digest"`
	UniqueCorePSSStates int                                     `json:"unique_core_pss_states"`
	RiskStatus          string                                  `json:"risk_status"`
	OracleViolations    int                                     `json:"oracle_violations"`
}

type etcdraftA8PairedScenarioSummary struct {
	Classification string                `json:"classification"`
	TargetID       string                `json:"target_id"`
	TargetIdentity string                `json:"target_identity_digest"`
	SemanticMode   string                `json:"semantic_exposure"`
	RootMode       string                `json:"root_mode,omitempty"`
	SourceDigest   string                `json:"source_bundle_digest,omitempty"`
	CorpusDigest   string                `json:"root_corpus_digest,omitempty"`
	RootRule       string                `json:"root_selection_rule,omitempty"`
	RootDigest     string                `json:"root_trace_digest,omitempty"`
	RootDecisions  int                   `json:"root_decisions,omitempty"`
	SameTrace      bool                  `json:"same_trace"`
	Deterministic  etcdraftA8ScenarioArm `json:"deterministic"`
	Agent          etcdraftA8ScenarioArm `json:"agent"`
}

func runEtcdraftA8PairedScenario(
	ctx context.Context,
	options etcdraftA8PairedScenarioOptions,
) (etcdraftA8PairedScenarioSummary, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.SemanticInputPath == "" ||
		!validateAgentKeyFileName(options.AgentKeyFile) || options.Client.HTTP == nil || options.ReadKey == nil {
		return etcdraftA8PairedScenarioSummary{}, errors.New("ETCDRAFT_A8_PAIR_OPTIONS_INVALID")
	}
	info, statErr := os.Lstat(clean)
	if options.Resume {
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return etcdraftA8PairedScenarioSummary{}, errors.New("ETCDRAFT_A8_PAIR_RESUME_DIRECTORY_INVALID")
		}
	} else {
		if statErr == nil || !os.IsNotExist(statErr) {
			return etcdraftA8PairedScenarioSummary{}, errors.New("ETCDRAFT_A8_PAIR_NEW_DIRECTORY_REQUIRED")
		}
		if err := os.MkdirAll(clean, 0o700); err != nil {
			return etcdraftA8PairedScenarioSummary{}, err
		}
	}
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, options.CorpusPath, options.SemanticInputPath, options.Client,
	)
	if err != nil || inputs.experiment.ScenarioSemanticExposure != controlexperiment.ScenarioSemanticExposureFull ||
		inputs.experiment.SessionBudget.MaxAttempts != 1 {
		return etcdraftA8PairedScenarioSummary{}, errors.New("ETCDRAFT_A8_PAIR_INPUT_INVALID")
	}
	baselineDirectory := filepath.Join(clean, "deterministic")
	agentDirectory := filepath.Join(clean, "agent")
	baselineResume, err := a8ChildResume(baselineDirectory, options.Resume)
	if err != nil {
		return etcdraftA8PairedScenarioSummary{}, err
	}
	if _, err := runEtcdraftA8DeterministicSession(ctx, baselineDirectory, inputs, baselineResume); err != nil {
		return etcdraftA8PairedScenarioSummary{}, err
	}
	agentResume, err := a8ChildResume(agentDirectory, options.Resume)
	if err != nil {
		return etcdraftA8PairedScenarioSummary{}, err
	}
	if _, err := runEtcdraftScenarioSessionWithInputs(ctx, agentDirectory, etcdraftScenarioSessionOptions{
		Directory: agentDirectory, Resume: agentResume,
		AgentKeyFile: options.AgentKeyFile, Client: options.Client, ReadKey: options.ReadKey,
	}, inputs); err != nil {
		return etcdraftA8PairedScenarioSummary{}, err
	}
	baseline, err := readEtcdraftA8ScenarioArm(
		baselineDirectory, etcdraftA8DeterministicSessionID,
		etcdraftA8DeterministicClass, scenarioPlannerDeterministic,
	)
	if err != nil {
		return etcdraftA8PairedScenarioSummary{}, err
	}
	agent, err := readEtcdraftA8ScenarioArm(
		agentDirectory, etcdraftScenarioSessionID,
		etcdraftScenarioCalibrationClass, scenarioPlannerAgent,
	)
	if err != nil || !sameScenarioExecutionBudget(baseline.Budget, agent.Budget) {
		return etcdraftA8PairedScenarioSummary{}, errors.New("ETCDRAFT_A8_PAIR_ARM_MISMATCH")
	}
	summary := etcdraftA8PairedScenarioSummary{
		Classification: etcdraftA8PairClass, TargetID: etcdraftCampaignTargetID,
		TargetIdentity: inputs.campaign.source.Identity.ManifestDigest,
		SemanticMode:   string(inputs.experiment.ScenarioSemanticExposure),
		SameTrace:      baseline.TraceDigest == agent.TraceDigest,
		Deterministic:  baseline, Agent: agent,
	}
	if options.CorpusPath == "" {
		summary.RootMode = etcdraftA8FreshRootMode
		summary.SourceDigest = inputs.campaign.source.Digest
		summary.CorpusDigest = inputs.campaign.corpus.Digest
		summary.RootRule = inputs.campaign.corpus.SelectionRuleID
		summary.RootDigest = inputs.root.Digest
		summary.RootDecisions = len(inputs.root.Records)
	}
	path := filepath.Join(clean, "summary.json")
	if options.Resume {
		var stored etcdraftA8PairedScenarioSummary
		if err := readStrictJSONFile(path, 64<<10, &stored); err == nil {
			if !reflect.DeepEqual(stored, summary) {
				return etcdraftA8PairedScenarioSummary{}, errors.New("ETCDRAFT_A8_PAIR_SUMMARY_DRIFT")
			}
			return stored, nil
		} else if !os.IsNotExist(err) {
			return etcdraftA8PairedScenarioSummary{}, err
		}
	}
	if err := writeStatelessAgentJSON(clean, "summary.json", summary); err != nil {
		return etcdraftA8PairedScenarioSummary{}, err
	}
	return summary, nil
}

func a8ChildResume(path string, parentResume bool) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil || !parentResume || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("ETCDRAFT_A8_PAIR_CHILD_DIRECTORY_INVALID")
	}
	return true, nil
}

func runEtcdraftA8DeterministicSession(
	ctx context.Context,
	directory string,
	inputs etcdraftSemanticCalibrationInputs,
	resume bool,
) (scenarioSessionSummary, error) {
	budget := inputs.experiment.SessionBudget
	budget.MaxModelCalls, budget.MaxModelTokens = 0, 0
	experimentDigest, err := scenarioSessionExperimentDigest(struct {
		BaseSpecDigest string `json:"base_spec_digest"`
		Planner        string `json:"planner"`
	}{inputs.spec.Digest, scenarioPlannerDeterministic}, inputs.experiment.ScenarioMaxSteps,
		etcdraftSemanticPrefixProjectorID, etcdraftScenarioSemanticProjectorID)
	if err != nil {
		return scenarioSessionSummary{}, err
	}
	config, err := controlexperiment.NewCampaignConfig(
		etcdraftA8DeterministicSessionID, etcdraftCampaignTargetID,
		inputs.campaign.source.Identity.ManifestDigest, experimentDigest,
		budget, inputs.experiment.SessionWallClockMS,
	)
	if err != nil {
		return scenarioSessionSummary{}, err
	}
	var recovered controlexperiment.CampaignRecovery
	if resume {
		recovered, err = controlexperiment.RecoverCampaignDirectory(directory, config)
	} else {
		recovered, err = controlexperiment.CreateCampaignDirectory(directory, config)
	}
	if err != nil {
		return scenarioSessionSummary{}, err
	}
	if recovered.Head.StopReason == controlexperiment.CampaignStopRunning {
		provider := controlexperiment.CampaignAttemptProviderFunc(func(
			attemptContext context.Context,
			request controlexperiment.CampaignAttemptRequest,
		) (controlexperiment.CampaignAttemptResult, error) {
			result, runErr := runEtcdraftDeterministicScenarioEpisode(attemptContext, inputs)
			if runErr != nil {
				return controlexperiment.CampaignAttemptResult{}, runErr
			}
			artifact, runErr := newDeterministicScenarioSessionEpisodeArtifact(
				etcdraftA8DeterministicClass, inputs.experiment.ScenarioSemanticExposure,
				result, validateEtcdraftScenarioTesting,
			)
			if runErr != nil {
				return controlexperiment.CampaignAttemptResult{}, runErr
			}
			encoded, runErr := json.Marshal(artifact)
			return controlexperiment.CampaignAttemptResult{
				Outcome: controlexperiment.CampaignAttemptCompleted,
				Work:    scenarioSessionWork(result, &inputs.campaign.source.Work), Artifact: encoded,
			}, runErr
		})
		coordinator, runErr := controlexperiment.NewCampaignCoordinator(&recovered, provider)
		if runErr != nil {
			return scenarioSessionSummary{}, runErr
		}
		if _, runErr = coordinator.Run(ctx); runErr != nil {
			return scenarioSessionSummary{}, runErr
		}
	}
	return summarizeScenarioSession(
		&recovered, inputs.experiment.ScenarioSemanticExposure,
		etcdraftA8DeterministicClass, validateEtcdraftScenarioTesting,
	)
}

func readEtcdraftA8ScenarioArm(
	directory string,
	campaignID string,
	classification string,
	planner string,
) (etcdraftA8ScenarioArm, error) {
	recovered, err := controlexperiment.RecoverStoredCampaignDirectory(directory)
	if err != nil || recovered.Config.ID != campaignID || recovered.Head.Sequence != 1 ||
		recovered.Head.StopReason != controlexperiment.CampaignStopAttemptLimit {
		return etcdraftA8ScenarioArm{}, errors.New("ETCDRAFT_A8_ARM_CAMPAIGN_INVALID")
	}
	encoded, err := recovered.ReadAttemptArtifact(1)
	if err != nil {
		return etcdraftA8ScenarioArm{}, err
	}
	artifact, err := decodeScenarioSessionArtifact(encoded, classification, validateEtcdraftScenarioTesting)
	if err != nil || artifact.Episode.Planner != planner || artifact.Episode.Testing == nil {
		return etcdraftA8ScenarioArm{}, errors.New("ETCDRAFT_A8_ARM_ARTIFACT_INVALID")
	}
	testing := artifact.Episode.Testing
	return etcdraftA8ScenarioArm{
		Planner: planner, CampaignID: campaignID, Budget: recovered.Config.Budget,
		PrimaryWorkUnits: recovered.Head.Totals.Primary.WorkUnits,
		ReplayWorkUnits:  recovered.Head.Totals.Replay.WorkUnits,
		ModelCalls:       recovered.Head.Totals.Model.Calls, ModelTokens: recovered.Head.Totals.Model.TotalTokens,
		TraceDigest: testing.TraceDigest, UniqueCorePSSStates: testing.UniqueCorePSSStates,
		RiskStatus: testing.RiskStatus, OracleViolations: testing.OracleViolations,
	}, nil
}

func sameScenarioExecutionBudget(left, right controlexperiment.CampaignLogicalBudget) bool {
	return left.MaxAttempts == right.MaxAttempts &&
		left.MaxPrimarySchedulerDecisions == right.MaxPrimarySchedulerDecisions &&
		left.MaxPrimaryWorkUnits == right.MaxPrimaryWorkUnits &&
		left.MaxReplayWorkUnits == right.MaxReplayWorkUnits
}

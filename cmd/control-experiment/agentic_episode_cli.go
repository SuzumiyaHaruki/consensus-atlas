package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const agenticEpisodeStrategy = controlexperiment.AgenticMethodStrategyID

func runAgenticEpisodeCLI(
	ctx context.Context,
	options controlExperimentOptions,
	stdout io.Writer,
) error {
	withoutTarget := agenticEpisodeNonSessionProjection(options)
	if options.CampaignDirectory == "" || options.Target == "" ||
		options.InvestigationEpisodes <= 0 || withoutTarget.hasNonSessionFlags() {
		return errors.New("Agentic Episode requires -campaign-dir, -target, target inputs, Agent inputs, optional -investigation-episodes, and optional -campaign-resume")
	}
	recovery, ok := agenticEpisodeRecoveryForTarget(options.Target)
	if !ok {
		return errors.New("AGENTIC_EPISODE_TARGET_UNSUPPORTED")
	}
	if options.InvestigationEpisodes > 1 {
		return runAgenticInvestigationCLI(ctx, options, recovery, stdout)
	}
	result, err := runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory: options.CampaignDirectory, Resume: options.CampaignResume,
		AgentKeyFile: options.AgentKeyFile, ReadKey: readAgentKey, Recovery: recovery,
		Prepare: func(ctx context.Context) (agenticEpisodeComposition, error) {
			return prepareAgenticEpisodeComposition(ctx, options)
		},
		PreparationWallClockMS: normalizedPreparationWallClockMS(options.PreparationWallClockMS),
	})
	if result.Summary.TargetID != "" {
		fmt.Fprintf(stdout,
			"episode=%s target=%s status=%s risk_calls=%d scenario_calls=%d model_calls=%d model_tokens=%d executable=%t witness_instantiated=%t pss_protocol=%d pss_control=%d pss_joint=%d pss_samples=%d oracle_findings=%d\n",
			options.CampaignDirectory, result.Summary.TargetID, result.Summary.Status,
			result.Summary.RiskAttempts, result.Summary.ScenarioAttempts,
			result.Summary.Work.Model.Calls, result.Summary.Work.Model.TotalTokens,
			result.Summary.Metrics.CandidateAccepted, result.Summary.Metrics.WitnessInstantiated,
			result.Summary.Metrics.ProtocolPSSStates, result.Summary.Metrics.ControlPSSStates,
			result.Summary.Metrics.UniquePSSStates, result.Summary.Metrics.CorePSSSamples,
			result.Summary.Metrics.OracleFindings,
		)
	} else if result.UnreconciledModelCalls > 0 {
		fmt.Fprintf(stdout, "episode=%s unreconciled_model_calls=%d\n",
			options.CampaignDirectory, result.UnreconciledModelCalls)
	}
	return err
}

func agenticEpisodeNonSessionProjection(options controlExperimentOptions) controlExperimentOptions {
	options.Target = ""
	options.MethodSpecDigest = ""
	options.InvestigationEpisodes = 1
	options.KnowledgeSourceMounts = nil
	options.ClosureMode = ""
	options.RiskInput = ""
	options.RepositoryRoot = ""
	return options
}

func runAgenticInvestigationCLI(
	ctx context.Context,
	options controlExperimentOptions,
	recovery agenticEpisodeRecoveryBinding,
	stdout io.Writer,
) error {
	composition, err := prepareAgenticEpisodeComposition(ctx, options)
	if err != nil {
		return err
	}
	budget, err := agenticInvestigationBudgetFromEpisode(
		options.InvestigationEpisodes, composition.Budget,
	)
	if err != nil {
		return err
	}
	result, err := runAgenticInvestigation(ctx, agenticInvestigationOptions{
		Directory: options.CampaignDirectory, Resume: options.CampaignResume,
		AgentKeyFile: options.AgentKeyFile, ReadKey: readAgentKey, Recovery: recovery,
		Budget:                 budget,
		PreparationWallClockMS: normalizedPreparationWallClockMS(options.PreparationWallClockMS),
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			return composition, nil
		},
	})
	fmt.Fprintf(stdout,
		"investigation=%s target=%s episodes=%d stop=%s risk_generation_episodes_with_executable_candidates=%d model_calls=%d model_tokens=%d reserved_decision_allowance=%d consumed_scenario_decisions=%d unreconciled_model_calls=%d capability_gap_attempts=%d repeated_capability_gap_attempts=%d capability_repair_attempts=%d capability_repair_executions=%d\n",
		options.CampaignDirectory, options.Target, len(result.Episodes), result.StopReason,
		result.RiskGenerationEpisodesWithExecutableCandidates,
		result.ModelWork.Calls, result.ModelWork.TotalTokens, result.ReservedDecisionAllowance,
		result.ConsumedScenarioDecisions,
		result.UnreconciledModelCalls,
		result.CapabilityAdaptation.GapAttempts,
		result.CapabilityAdaptation.RepeatedGapAttempts,
		result.CapabilityAdaptation.RepairAttempts,
		result.CapabilityAdaptation.RepairExecutions,
	)
	return err
}

func agenticInvestigationBudgetFromEpisode(
	episodes int,
	episode agenticEpisodeBudget,
) (agenticInvestigationBudget, error) {
	maxInt := int(^uint(0) >> 1)
	if episodes <= 1 || episode.validate() != nil ||
		episodes > maxInt/episode.MaxTotalCalls || episodes > maxInt/episode.MaxObservedTokens ||
		episodes > maxInt/episode.MaxRuntimeDecisions {
		return agenticInvestigationBudget{}, errors.New("AGENTIC_INVESTIGATION_BUDGET_OVERFLOW")
	}
	return agenticInvestigationBudget{
		MaxEpisodes:                 episodes,
		MaxModelCalls:               episodes * episode.MaxTotalCalls,
		MaxModelTokens:              episodes * episode.MaxObservedTokens,
		MaxRuntimeDecisionAllowance: episodes * episode.MaxRuntimeDecisions,
	}, nil
}

func agenticEpisodeRecoveryForTarget(
	targetID string,
) (agenticEpisodeRecoveryBinding, bool) {
	switch targetID {
	case "etcdraft-v2":
		return etcdraftAgenticEpisodeRecoveryBinding(), true
	case "omnipaxos-v2":
		return omnipaxosAgenticEpisodeRecoveryBinding(), true
	default:
		return agenticEpisodeRecoveryBinding{}, false
	}
}

func prepareAgenticEpisodeComposition(
	ctx context.Context,
	options controlExperimentOptions,
) (agenticEpisodeComposition, error) {
	preparationWallClockMS := options.PreparationWallClockMS
	if preparationWallClockMS == 0 {
		preparationWallClockMS = 1_200_000
	}
	if ctx == nil || preparationWallClockMS <= 0 {
		return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_PREPARATION_DEADLINE_INVALID")
	}
	preparationCtx, cancel := context.WithTimeout(
		ctx, time.Duration(preparationWallClockMS)*time.Millisecond,
	)
	defer cancel()
	started := time.Now()
	composition, err := prepareAgenticEpisodeCompositionBounded(preparationCtx, options)
	if err != nil {
		if errors.Is(preparationCtx.Err(), context.DeadlineExceeded) {
			return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_PREPARATION_DEADLINE_EXCEEDED")
		}
		return agenticEpisodeComposition{}, err
	}
	elapsed := time.Since(started).Milliseconds()
	if elapsed == 0 {
		elapsed = 1
	}
	composition.Preparation.WallClockMS = elapsed
	return composition, nil
}

func prepareAgenticEpisodeCompositionBounded(
	ctx context.Context,
	options controlExperimentOptions,
) (agenticEpisodeComposition, error) {
	if options.AgentModel == "" || options.AgentKeyFile == "" || options.SemanticInput == "" {
		return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_ACTIVE_INPUT_REQUIRED")
	}
	if options.MethodSpecDigest != "" && !validAgenticSHA256(options.MethodSpecDigest) {
		return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_METHOD_SPEC_INVALID")
	}
	knowledgeSourceMounts, err := prepareKnowledgeSourceMounts(options.KnowledgeSourceMounts)
	if err != nil {
		return agenticEpisodeComposition{}, err
	}
	feedbackMode := controlexperiment.AgenticCapabilityFeedbackMode(
		normalizedCapabilityFeedbackMode(options.CapabilityFeedbackMode),
	)
	if feedbackMode.Validate() != nil {
		return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_CAPABILITY_FEEDBACK_INVALID")
	}
	feedbackProbe, err := loadAgenticCapabilityFeedbackProbe(options.CapabilityFeedbackProbe)
	if err != nil || feedbackProbe != nil && options.InvestigationEpisodes != 1 {
		return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_CAPABILITY_PROBE_INVALID")
	}
	client, err := newAgentIntentTransport(options.AgentProvider, options.AgentModel)
	if err != nil {
		return agenticEpisodeComposition{}, err
	}
	scenarioClient, err := newScenarioAgentIntentTransport(options.AgentProvider, options.AgentModel)
	if err != nil {
		return agenticEpisodeComposition{}, err
	}
	switch options.Target {
	case "etcdraft-v2":
		if options.WorkerPath != "" {
			return agenticEpisodeComposition{}, errors.New("ETCDRAFT_AGENTIC_EPISODE_WORKER_UNEXPECTED")
		}
		knowledgeSourceMounts, err = bindEtcdraftLocalSUTSource(
			ctx, options.RepositoryRoot, options.SemanticInput, knowledgeSourceMounts,
		)
		if err != nil {
			return agenticEpisodeComposition{}, err
		}
		inputs, err := prepareEtcdraftAgenticEpisode(
			ctx, "", options.SemanticInput, client,
		)
		if err != nil {
			return agenticEpisodeComposition{}, err
		}
		target, err := newEtcdraftAgenticEpisodeTarget(inputs)
		if err != nil {
			return agenticEpisodeComposition{}, err
		}
		budget, err := agenticEpisodeBudgetFromExperiment(
			inputs.experiment.ScenarioMaxCalls, inputs.experiment.ScenarioMaxSteps,
			inputs.experiment.ScenarioMaxDecisions, inputs.experiment.SessionBudget,
		)
		if err != nil {
			return agenticEpisodeComposition{}, err
		}
		return finalizeAgenticEpisodeComposition(
			options, target, budget, inputs.client, scenarioClient, etcdraftSemanticInputSchema,
			inputs.experiment.ScenarioSemanticExposure, inputs.experiment.SessionWallClockMS,
			knowledgeSourceMounts, feedbackMode, feedbackProbe, inputs.preparation,
		)
	case "omnipaxos-v2":
		if options.WorkerPath == "" {
			return agenticEpisodeComposition{}, errors.New("OMNIPAXOS_AGENTIC_EPISODE_WORKER_REQUIRED")
		}
		knowledgeSourceMounts, err = bindOmnipaxosLocalSUTSource(
			ctx, options.RepositoryRoot, options.SemanticInput, options.WorkerPath, knowledgeSourceMounts,
		)
		if err != nil {
			return agenticEpisodeComposition{}, err
		}
		inputs, err := prepareOmnipaxosAgenticEpisode(ctx, options.WorkerPath, options.SemanticInput)
		if err != nil {
			return agenticEpisodeComposition{}, err
		}
		target, err := newOmnipaxosAgenticEpisodeTarget(inputs)
		if err != nil {
			return agenticEpisodeComposition{}, err
		}
		budget, err := agenticEpisodeBudgetFromExperiment(
			inputs.Experiment.ScenarioMaxCalls, inputs.Experiment.ScenarioMaxSteps,
			inputs.Experiment.ScenarioMaxDecisions, inputs.Experiment.SessionBudget,
		)
		if err != nil {
			return agenticEpisodeComposition{}, err
		}
		return finalizeAgenticEpisodeComposition(
			options, target, budget, client, scenarioClient, omnipaxosSemanticInputSchema,
			inputs.Experiment.ScenarioSemanticExposure, inputs.Experiment.SessionWallClockMS,
			knowledgeSourceMounts, feedbackMode, feedbackProbe, inputs.Preparation,
		)
	default:
		return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_TARGET_UNSUPPORTED")
	}
}

func finalizeAgenticEpisodeComposition(
	options controlExperimentOptions,
	target agenticEpisodeTarget,
	budget agenticEpisodeBudget,
	client agentIntentTransport,
	scenarioClient agentIntentTransport,
	semanticInputSchema string,
	semanticExposure controlexperiment.ScenarioSemanticExposureMode,
	sessionWallClockMS int64,
	knowledgeSourceMounts []controlexperiment.KnowledgeSourceMount,
	feedbackMode controlexperiment.AgenticCapabilityFeedbackMode,
	feedbackProbe *controlexperiment.AgenticCapabilityFeedbackProbe,
	preparation controlexperiment.AgenticPreparationWork,
) (agenticEpisodeComposition, error) {
	var err error
	target, err = bindRequestedClosureMode(target, options.ClosureMode)
	if err != nil {
		return agenticEpisodeComposition{}, err
	}
	if err := validateClosureCallBudget(target, budget); err != nil {
		return agenticEpisodeComposition{}, err
	}
	existingRisk, riskInputDigest, err := loadExistingRiskInput(options.RiskInput, target)
	if err != nil {
		return agenticEpisodeComposition{}, err
	}
	budget, err = agenticEpisodeBudgetForCapabilityProbe(budget, feedbackProbe)
	if err != nil {
		return agenticEpisodeComposition{}, err
	}
	memory, err := agenticCapabilityFeedbackProbeMemory(target.Surface, feedbackProbe)
	if err != nil {
		return agenticEpisodeComposition{}, err
	}
	spec, err := buildAgenticMethodSpec(
		options, target, budget, client, scenarioClient, semanticInputSchema,
		semanticExposure, sessionWallClockMS, knowledgeSourceMounts, feedbackProbe,
		riskInputDigest,
	)
	if err != nil {
		return agenticEpisodeComposition{}, err
	}
	target.MethodSpecDigest = spec.Digest
	return agenticEpisodeComposition{
		Target: target, Budget: budget, MethodSpec: spec,
		KnowledgeSourceMounts:   knowledgeSourceMounts,
		Client:                  client,
		ScenarioClient:          scenarioClient,
		SessionWallClockMS:      sessionWallClockMS,
		CapabilityFeedbackMode:  feedbackMode,
		CapabilityFeedbackProbe: feedbackProbe,
		ExistingRisk:            existingRisk,
		RiskInputDigest:         riskInputDigest,
		Memory:                  memory,
		Preparation:             preparation,
	}, nil
}

func validateClosureCallBudget(target agenticEpisodeTarget, budget agenticEpisodeBudget) error {
	if target.ClosureFactory != nil &&
		target.ClosureMinimumScenarioCalls > budget.MaxScenarioCalls {
		return errors.New("AGENTIC_EPISODE_TARGET_LOCAL_CLOSURE_CALL_BUDGET_INSUFFICIENT")
	}
	return nil
}

// bindRequestedClosureMode resolves the experiment arm against the actual
// Target composition. MethodSpec is derived from the resulting factory field,
// so a caller cannot label a public-fixed execution as target-local.
func bindRequestedClosureMode(
	target agenticEpisodeTarget,
	requested string,
) (agenticEpisodeTarget, error) {
	switch controlexperiment.AgenticClosureMode(requested) {
	case "":
		target.ClosureFactory = nil
		target.ClosureSupport = nil
		return target, nil
	case controlexperiment.AgenticClosureModePublicFixed:
		target.ClosureFactory = nil
		target.ClosureSupport = nil
		return target, nil
	case controlexperiment.AgenticClosureModeTargetLocal:
		if target.ClosureFactory == nil || target.ClosureSupport == nil {
			return agenticEpisodeTarget{}, errors.New("AGENTIC_EPISODE_TARGET_LOCAL_CLOSURE_UNAVAILABLE")
		}
		return target, nil
	default:
		return agenticEpisodeTarget{}, errors.New("AGENTIC_EPISODE_CLOSURE_MODE_INVALID")
	}
}

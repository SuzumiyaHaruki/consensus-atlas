package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	agenticEpisodeRiskJournal     = "risk-agent"
	agenticEpisodeScenarioJournal = "scenario-agent"

	agenticInvestigationEpisodeLimit  = "episode-limit-reached"
	agenticInvestigationCallLimit     = "model-call-limit-reached"
	agenticInvestigationTokenLimit    = "model-token-threshold-reached"
	agenticInvestigationDecisionLimit = "runtime-decision-allowance-reached"
)

type agenticEpisodeComposition struct {
	Target                  agenticEpisodeTarget
	Budget                  agenticEpisodeBudget
	MethodSpec              controlexperiment.AgenticMethodSpec
	Memory                  []controlexperiment.RiskExplorationMemoryEntry
	KnowledgeSourceMounts   []controlexperiment.KnowledgeSourceMount
	Client                  agentIntentTransport
	ScenarioClient          agentIntentTransport
	SessionWallClockMS      int64
	CapabilityFeedbackMode  controlexperiment.AgenticCapabilityFeedbackMode
	CapabilityFeedbackProbe *controlexperiment.AgenticCapabilityFeedbackProbe
	ExistingRisk            *controlexperiment.RiskCandidateAssessment
	// PortfolioRisk is selected from a previously persisted generated
	// portfolio. Unlike ExistingRisk it is not user input and therefore does
	// not alter MethodSpec risk-input identity.
	PortfolioRisk     *controlexperiment.RiskCandidateAssessment
	RiskInputDigest   string
	Preparation       controlexperiment.AgenticPreparationWork
	PreparationCached bool
}

type agenticEpisodeDirectoryOptions struct {
	Directory              string
	Resume                 bool
	AgentKeyFile           string
	ReadKey                agentKeyReader
	Recovery               agenticEpisodeRecoveryBinding
	Prepare                func(context.Context) (agenticEpisodeComposition, error)
	PreparationWallClockMS int64
}

type agenticInvestigationBudget struct {
	MaxEpisodes                 int
	MaxModelCalls               int
	MaxModelTokens              int
	MaxRuntimeDecisionAllowance int
}

type agenticInvestigationOptions struct {
	Directory              string
	Resume                 bool
	AgentKeyFile           string
	ReadKey                agentKeyReader
	Recovery               agenticEpisodeRecoveryBinding
	Budget                 agenticInvestigationBudget
	Prepare                func(context.Context) (agenticEpisodeComposition, error)
	PreparationWallClockMS int64
}

type agenticInvestigationResult struct {
	StopReason               string
	Episodes                 []recoveredAgenticEpisode
	ExplorationMemory        []controlexperiment.RiskExplorationMemoryEntry
	ModelWork                controlexperiment.ModelWork
	RuntimeDecisionAllowance int
	UnreconciledModelCalls   int
	CapabilityAdaptation     agenticCapabilityAdaptationMetrics
}

func (budget agenticInvestigationBudget) validate() error {
	if budget.MaxEpisodes <= 0 || budget.MaxModelCalls <= 0 || budget.MaxModelTokens <= 0 ||
		budget.MaxRuntimeDecisionAllowance <= 0 {
		return errors.New("AGENTIC_INVESTIGATION_BUDGET_INVALID")
	}
	return nil
}

// runAgenticInvestigation composes existing durable episode directories. It
// deliberately writes no session ledger; Memory is rebuilt after every round
// from the episode artifacts that were just recovered from disk.
func runAgenticInvestigation(
	ctx context.Context,
	options agenticInvestigationOptions,
) (agenticInvestigationResult, error) {
	var result agenticInvestigationResult
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.Budget.validate() != nil || options.Recovery.validate() != nil ||
		!validateAgentKeyFileName(options.AgentKeyFile) || options.ReadKey == nil || options.Prepare == nil {
		return result, errors.New("AGENTIC_INVESTIGATION_OPTIONS_INVALID")
	}
	existing, partial, err := recoverAgenticInvestigationEpisodes(
		clean, options.Resume, options.Recovery, options.Budget.MaxEpisodes,
	)
	if err != nil {
		return result, err
	}
	result.Episodes = existing
	for _, episode := range existing {
		addAgentModelWork(&result.ModelWork, episode.Summary.Work.Model)
		result.RuntimeDecisionAllowance += episode.Summary.Budget.MaxRuntimeDecisions
		if episode.Summary.Status == agenticEpisodeTokenStopped {
			result.StopReason = agenticInvestigationTokenLimit
		}
	}
	startOrdinal := len(existing) + 1
	if startOrdinal > options.Budget.MaxEpisodes {
		result.StopReason = agenticInvestigationEpisodeLimit
	}
	var prepared agenticEpisodeComposition
	if result.StopReason == "" {
		prepared, err = prepareAgenticCompositionWithinDeadline(
			ctx, options.PreparationWallClockMS, options.Prepare,
		)
		if err != nil {
			return result, err
		}
		if prepared.Target.validate() != nil || prepared.Budget.validate() != nil ||
			prepared.Target.ID != options.Recovery.TargetID {
			return result, errors.New("AGENTIC_INVESTIGATION_COMPOSITION_INVALID")
		}
		if prepared.MethodSpec.Digest != "" {
			for _, recovered := range result.Episodes {
				if recovered.MethodSpec == nil || recovered.MethodSpec.Digest != prepared.MethodSpec.Digest {
					return result, errors.New("AGENTIC_INVESTIGATION_METHOD_SPEC_DRIFT")
				}
			}
		}
	}
	for ordinal := startOrdinal; ordinal <= options.Budget.MaxEpisodes; ordinal++ {
		if result.StopReason != "" {
			break
		}
		memory, err := deriveAgenticExplorationMemory(result.Episodes)
		if err != nil {
			return result, err
		}
		composition := prepared
		portfolioRisk, queueErr := nextAgenticPortfolioRisk(result.Episodes)
		if queueErr != nil {
			return result, queueErr
		}
		composition.PortfolioRisk = portfolioRisk
		remainingCalls := options.Budget.MaxModelCalls - result.ModelWork.Calls
		remainingTokens := options.Budget.MaxModelTokens - result.ModelWork.TotalTokens
		remainingDecisions := options.Budget.MaxRuntimeDecisionAllowance - result.RuntimeDecisionAllowance
		if remainingCalls < composition.Budget.MaxTotalCalls {
			result.StopReason = agenticInvestigationCallLimit
			break
		}
		if remainingTokens < composition.Budget.MaxObservedTokens {
			result.StopReason = agenticInvestigationTokenLimit
			break
		}
		if remainingDecisions < composition.Budget.MaxRuntimeDecisions {
			result.StopReason = agenticInvestigationDecisionLimit
			break
		}
		if composition.CapabilityFeedbackProbe != nil && len(memory) > 0 {
			return result, errors.New("AGENTIC_INVESTIGATION_CAPABILITY_PROBE_REQUIRES_ONE_EPISODE")
		}
		composition.Memory = append(
			append([]controlexperiment.RiskExplorationMemoryEntry(nil), prepared.Memory...), memory...,
		)
		if ordinal > startOrdinal {
			composition.Preparation = controlexperiment.AgenticPreparationWork{}
			composition.PreparationCached = true
		}
		directory := filepath.Join(clean, fmt.Sprintf("episode-%04d", ordinal))
		resumeEpisode := partial && ordinal == startOrdinal
		partialEpisode, runErr := runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
			Directory: directory, Resume: resumeEpisode,
			AgentKeyFile: options.AgentKeyFile, ReadKey: options.ReadKey,
			Recovery: options.Recovery,
			Prepare: func(context.Context) (agenticEpisodeComposition, error) {
				return composition, nil
			},
			PreparationWallClockMS: options.PreparationWallClockMS,
		})
		if runErr != nil {
			result.UnreconciledModelCalls += partialEpisode.UnreconciledModelCalls
			return result, runErr
		}
		recovered, terminal, err := recoverAgenticEpisodeArtifacts(directory, options.Recovery)
		if err != nil || !terminal {
			return result, errors.New("AGENTIC_INVESTIGATION_EPISODE_RECOVERY_FAILED")
		}
		result.Episodes = append(result.Episodes, recovered)
		partial = false
		addAgentModelWork(&result.ModelWork, recovered.Summary.Work.Model)
		result.RuntimeDecisionAllowance += composition.Budget.MaxRuntimeDecisions
		if recovered.Summary.Status == agenticEpisodeTokenStopped {
			result.StopReason = agenticInvestigationTokenLimit
			break
		}
		if result.ModelWork.Calls >= options.Budget.MaxModelCalls {
			result.StopReason = agenticInvestigationCallLimit
			break
		}
		if result.ModelWork.TotalTokens >= options.Budget.MaxModelTokens {
			result.StopReason = agenticInvestigationTokenLimit
			break
		}
		if result.RuntimeDecisionAllowance >= options.Budget.MaxRuntimeDecisionAllowance {
			result.StopReason = agenticInvestigationDecisionLimit
			break
		}
	}
	if result.StopReason == "" {
		result.StopReason = agenticInvestigationEpisodeLimit
	}
	result.ExplorationMemory, err = deriveAgenticExplorationMemory(result.Episodes)
	result.CapabilityAdaptation = agenticInvestigationCapabilityAdaptation(result.Episodes)
	return result, err
}

func agenticInvestigationCapabilityAdaptation(
	episodes []recoveredAgenticEpisode,
) agenticCapabilityAdaptationMetrics {
	var attempts []agenticScenarioAttemptArtifact
	for _, episode := range episodes {
		attempts = append(attempts, episode.Summary.ScenarioAttemptFeedback...)
	}
	return agenticCapabilityAdaptationFromAttempts(attempts)
}

// nextAgenticPortfolioRisk recovers the generated portfolio from ordinary
// Episode summaries and selects the first semantically distinct candidate
// that has not yet received its own Scenario Episode. The Investigation's
// existing aggregate call/token/decision limits remain the only budget; this
// queue creates no hidden work allowance or additional durable ledger.
func nextAgenticPortfolioRisk(
	episodes []recoveredAgenticEpisode,
) (*controlexperiment.RiskCandidateAssessment, error) {
	var offered []controlexperiment.RiskCandidateAssessment
	var investigated []controlexperiment.RiskCandidateAssessment
	for _, episode := range episodes {
		if episode.Summary.validateCompact() != nil {
			return nil, errors.New("AGENTIC_INVESTIGATION_PORTFOLIO_EPISODE_INVALID")
		}
		offered = append(offered, episode.Summary.ExecutableRisks...)
		if !agenticEpisodeRiskEnteredScenario(episode.Summary) {
			continue
		}
		investigated = append(investigated, *episode.Summary.Accepted)
	}
	return selectNextAgenticPortfolioRisk(offered, investigated)
}

// agenticEpisodeRiskEnteredScenario distinguishes accepting an executable
// candidate from actually spending Scenario work on it. In particular, a
// charged Risk response can cross the Episode token threshold after Accepted
// has been persisted but before the first Scenario attempt. Such a candidate
// must remain at the head of the Investigation queue in the next Episode.
func agenticEpisodeRiskEnteredScenario(artifact agenticEpisodeArtifact) bool {
	return artifact.Accepted != nil && artifact.ScenarioAttempts > 0
}

func selectNextAgenticPortfolioRisk(
	offered []controlexperiment.RiskCandidateAssessment,
	investigatedValues []controlexperiment.RiskCandidateAssessment,
) (*controlexperiment.RiskCandidateAssessment, error) {
	investigated := make(map[string]bool, len(investigatedValues))
	for _, assessment := range investigatedValues {
		identity, err := controlexperiment.RiskCandidateSemanticIdentity(assessment.Candidate)
		if err != nil {
			return nil, errors.New("AGENTIC_INVESTIGATION_PORTFOLIO_CANDIDATE_INVALID")
		}
		investigated[identity] = true
	}
	seenOffered := make(map[string]bool)
	for _, assessment := range offered {
		identity, err := controlexperiment.RiskCandidateSemanticIdentity(assessment.Candidate)
		if err != nil {
			return nil, errors.New("AGENTIC_INVESTIGATION_PORTFOLIO_CANDIDATE_INVALID")
		}
		if seenOffered[identity] {
			continue
		}
		seenOffered[identity] = true
		if !investigated[identity] {
			selected := assessment
			return &selected, nil
		}
	}
	return nil, nil
}

func recoverAgenticInvestigationEpisodes(
	directory string,
	resume bool,
	recovery agenticEpisodeRecoveryBinding,
	maxEpisodes int,
) ([]recoveredAgenticEpisode, bool, error) {
	info, err := os.Lstat(directory)
	if !resume {
		if !os.IsNotExist(err) {
			return nil, false, errors.New("AGENTIC_INVESTIGATION_NEW_DIRECTORY_REQUIRED")
		}
		return nil, false, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, errors.New("AGENTIC_INVESTIGATION_RESUME_DIRECTORY_INVALID")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) > maxEpisodes {
		return nil, false, errors.New("AGENTIC_INVESTIGATION_RESUME_LAYOUT_INVALID")
	}
	episodes := make([]recoveredAgenticEpisode, 0, len(entries))
	partial := false
	for index, entry := range entries {
		name := fmt.Sprintf("episode-%04d", index+1)
		if entry.Name() != name || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || partial {
			return nil, false, errors.New("AGENTIC_INVESTIGATION_RESUME_SEQUENCE_INVALID")
		}
		recovered, terminal, err := recoverAgenticEpisodeArtifacts(
			filepath.Join(directory, name), recovery,
		)
		if err != nil {
			return nil, false, err
		}
		if !terminal {
			partial = true
			continue
		}
		episodes = append(episodes, recovered)
	}
	return episodes, partial, nil
}

func runAgenticEpisodeDirectory(
	ctx context.Context,
	options agenticEpisodeDirectoryOptions,
) (recoveredAgenticEpisode, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.Recovery.validate() != nil {
		return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_DIRECTORY_OPTIONS_INVALID")
	}
	info, statErr := os.Lstat(clean)
	if options.Resume {
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_RESUME_DIRECTORY_INVALID")
		}
		recovered, terminal, err := recoverAgenticEpisodeArtifacts(clean, options.Recovery)
		if err != nil || terminal {
			return recovered, err
		}
	} else if statErr == nil || !os.IsNotExist(statErr) {
		return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_NEW_DIRECTORY_REQUIRED")
	}
	if !validateAgentKeyFileName(options.AgentKeyFile) || options.ReadKey == nil || options.Prepare == nil {
		return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_ACTIVE_OPTIONS_INVALID")
	}
	composition, err := prepareAgenticCompositionWithinDeadline(
		ctx, options.PreparationWallClockMS, options.Prepare,
	)
	if err != nil {
		return recoveredAgenticEpisode{}, err
	}
	if err := verifyKnowledgeSourceMountIdentities(composition.KnowledgeSourceMounts); err != nil {
		return recoveredAgenticEpisode{}, err
	}
	probeMemory, probeErr := agenticCapabilityFeedbackProbeMemory(
		composition.Target.Surface, composition.CapabilityFeedbackProbe,
	)
	sourceExposure, sourceExposureErr := agenticSourceExposureSpec(
		composition.Target.Knowledge, composition.KnowledgeSourceMounts,
	)
	riskInputMode, riskInputDigest, riskInputValid := agenticRiskInputIdentity(
		composition.ExistingRisk, composition.RiskInputDigest,
	)
	methodBound := composition.MethodSpec.Digest != ""
	if composition.Target.validate() != nil || composition.Budget.validate() != nil ||
		!riskInputValid ||
		composition.PortfolioRisk != nil &&
			validateAgenticEpisodeAssessment(*composition.PortfolioRisk) != nil ||
		methodBound && (composition.MethodSpec.Validate() != nil ||
			sourceExposureErr != nil ||
			!reflect.DeepEqual(composition.MethodSpec.SourceExposure, sourceExposure) ||
			composition.Target.MethodSpecDigest != composition.MethodSpec.Digest ||
			composition.MethodSpec.ClosureMode != agenticClosureModeForTarget(composition.Target) ||
			composition.MethodSpec.EpisodeLimits.PreparationWallClockMS <= 0 ||
			composition.Preparation.Validate() != nil ||
			composition.Preparation.WallClockMS > composition.MethodSpec.EpisodeLimits.PreparationWallClockMS ||
			composition.MethodSpec.RiskInputMode != riskInputMode ||
			composition.MethodSpec.RiskInputDigest != riskInputDigest ||
			composition.MethodSpec.CapabilityFeedbackMode != composition.CapabilityFeedbackMode ||
			!reflect.DeepEqual(composition.MethodSpec.CapabilityFeedbackProbe, composition.CapabilityFeedbackProbe)) ||
		!methodBound && composition.Target.MethodSpecDigest != "" ||
		composition.CapabilityFeedbackMode.Validate() != nil ||
		probeErr != nil || composition.CapabilityFeedbackProbe != nil &&
		!reflect.DeepEqual(composition.Memory, probeMemory) ||
		len(composition.KnowledgeSourceMounts) > 0 &&
			controlexperiment.ValidateKnowledgeSourceMounts(composition.KnowledgeSourceMounts) != nil ||
		composition.Client == nil || !composition.Client.ready() || composition.SessionWallClockMS <= 0 ||
		composition.Client.freeze().Validate() != nil ||
		composition.Target.ID != options.Recovery.TargetID {
		return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_COMPOSITION_INVALID")
	}
	if methodBound {
		if err := bindAgenticMethodSpec(clean, options.Resume, composition.MethodSpec); err != nil {
			return recoveredAgenticEpisode{}, err
		}
	}
	riskJournal, err := openAgenticRiskJournal(
		filepath.Join(clean, agenticEpisodeRiskJournal), options.Resume, composition.Client,
	)
	if err != nil {
		return recoveredAgenticEpisode{}, err
	}
	scenarioClient := composition.ScenarioClient
	if scenarioClient == nil {
		scenarioClient = composition.Client
	}
	if !scenarioClient.ready() || scenarioClient.freeze().Validate() != nil {
		return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_SCENARIO_TRANSPORT_INVALID")
	}
	scenarioJournal, err := openAgenticScenarioJournal(
		filepath.Join(clean, agenticEpisodeScenarioJournal), options.Resume, scenarioClient,
	)
	if err != nil {
		return recoveredAgenticEpisode{}, err
	}
	activateRiskKey := func() error {
		key, err := options.ReadKey(options.AgentKeyFile)
		if err != nil {
			return err
		}
		err = riskJournal.ActivateKey(key)
		key = ""
		return err
	}
	activateScenarioKey := func() error {
		key, err := options.ReadKey(options.AgentKeyFile)
		if err != nil {
			return err
		}
		err = scenarioJournal.ActivateKey(key)
		key = ""
		return err
	}
	var knowledgeReader controlexperiment.RiskKnowledgeReader
	if len(composition.KnowledgeSourceMounts) > 0 {
		knowledgeReader = func(request controlexperiment.KnowledgeReadRequest) (
			controlexperiment.KnowledgeReadResult, error,
		) {
			if err := verifyKnowledgeSourceMountIdentities(composition.KnowledgeSourceMounts); err != nil {
				return controlexperiment.KnowledgeReadResult{}, err
			}
			if request.Query != "" {
				return controlexperiment.SearchMountedKnowledgeSources(
					composition.KnowledgeSourceMounts, request,
				)
			}
			catalog, catalogErr := controlexperiment.KnowledgeSourceCatalog(composition.Target.Knowledge)
			if catalogErr != nil {
				return controlexperiment.KnowledgeReadResult{}, catalogErr
			}
			for _, source := range catalog {
				if source.Reference == request.Reference {
					return controlexperiment.ReadMountedKnowledgeSourceFromMounts(
						composition.KnowledgeSourceMounts, source, request,
					)
				}
			}
			return controlexperiment.ReadMountedKnowledgeSourceFromMounts(
				composition.KnowledgeSourceMounts,
				controlexperiment.KnowledgeSource{
					Reference: request.Reference, Path: request.Reference,
				},
				request,
			)
		}
	}
	sessionCtx, cancelSession, err := agenticSessionContext(ctx, composition.SessionWallClockMS)
	if err != nil {
		return recoveredAgenticEpisode{}, err
	}
	defer cancelSession()
	presetRisk := composition.ExistingRisk
	if composition.PortfolioRisk != nil {
		if presetRisk != nil {
			return recoveredAgenticEpisode{}, errors.New("AGENTIC_EPISODE_RISK_SOURCE_AMBIGUOUS")
		}
		presetRisk = composition.PortfolioRisk
	}
	result, err := runAgenticEpisode(
		sessionCtx, composition.Target, riskJournal, scenarioJournal, composition.Budget,
		projectAgenticCapabilityFeedbackMemory(
			composition.Memory, composition.CapabilityFeedbackMode,
		), knowledgeReader, presetRisk,
		activateRiskKey, activateScenarioKey,
	)
	if err != nil {
		return recoveredAgenticEpisode{UnreconciledModelCalls: countUnreconciledProviderCalls(
			riskJournal, scenarioJournal,
		)}, err
	}
	result.Work.Preparation = composition.Preparation
	if err := verifyKnowledgeSourceMountIdentities(composition.KnowledgeSourceMounts); err != nil {
		return recoveredAgenticEpisode{}, err
	}
	artifact, err := persistAgenticEpisodeArtifacts(
		clean, composition.Target.ID, composition.Budget, result,
	)
	if err != nil {
		return recoveredAgenticEpisode{}, err
	}
	recovered := recoveredAgenticEpisode{Summary: artifact}
	if methodBound {
		spec := composition.MethodSpec
		recovered.MethodSpec = &spec
	}
	if result.Testing != nil {
		testing := *result.Testing
		recovered.Testing = &testing
	}
	recovered.BranchTesting = append(
		[]agenticBranchTestingResult(nil), result.BranchTesting...,
	)
	return recovered, nil
}

func prepareAgenticCompositionWithinDeadline(
	parent context.Context,
	wallClockMS int64,
	prepare func(context.Context) (agenticEpisodeComposition, error),
) (agenticEpisodeComposition, error) {
	if parent == nil || prepare == nil {
		return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_PREPARATION_OPTIONS_INVALID")
	}
	if wallClockMS == 0 {
		wallClockMS = 1_200_000
	}
	if wallClockMS < 0 {
		return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_PREPARATION_DEADLINE_INVALID")
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(wallClockMS)*time.Millisecond)
	defer cancel()
	started := time.Now()
	composition, err := prepare(ctx)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return agenticEpisodeComposition{}, errors.New("AGENTIC_EPISODE_PREPARATION_DEADLINE_EXCEEDED")
		}
		return agenticEpisodeComposition{}, err
	}
	if composition.Preparation.WallClockMS == 0 && !composition.PreparationCached {
		composition.Preparation.WallClockMS = time.Since(started).Milliseconds()
		if composition.Preparation.WallClockMS == 0 {
			composition.Preparation.WallClockMS = 1
		}
	}
	return composition, nil
}

func projectAgenticCapabilityFeedbackMemory(
	memory []controlexperiment.RiskExplorationMemoryEntry,
	mode controlexperiment.AgenticCapabilityFeedbackMode,
) []controlexperiment.RiskExplorationMemoryEntry {
	projected := append([]controlexperiment.RiskExplorationMemoryEntry(nil), memory...)
	for index := range projected {
		projected[index].CapabilityGaps = append(
			[]controlexperiment.AgentCapabilityGap(nil), memory[index].CapabilityGaps...,
		)
		if mode == "" || mode == controlexperiment.AgenticCapabilityFeedbackReasonCodes {
			projected[index].CapabilityGaps = nil
		}
	}
	return projected
}

func countUnreconciledProviderCalls(
	risk *statelessAgentCallJournal,
	scenario *scenarioAgentCallJournal,
) int {
	var audits []controlexperiment.StatelessAgentCallAudit
	if risk != nil {
		if current, err := risk.Audits(); err == nil {
			audits = append(audits, current...)
		}
	}
	if scenario != nil {
		if current, err := scenario.Audits(); err == nil {
			audits = append(audits, current...)
		}
	}
	count := 0
	for _, audit := range audits {
		if audit.Work.Calls == 1 && audit.ProviderUsageStatus != agentProviderUsageObserved {
			count++
		}
	}
	return count
}

func agenticSessionContext(
	parent context.Context,
	wallClockMS int64,
) (context.Context, context.CancelFunc, error) {
	if parent == nil || wallClockMS <= 0 {
		return nil, nil, errors.New("AGENTIC_EPISODE_SESSION_DEADLINE_INVALID")
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(wallClockMS)*time.Millisecond)
	return ctx, cancel, nil
}

func openAgenticRiskJournal(
	directory string,
	resume bool,
	client agentIntentTransport,
) (*statelessAgentCallJournal, error) {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return newStatelessAgentCallJournal(directory, client, "")
	}
	if err != nil || !resume || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("AGENTIC_EPISODE_RISK_JOURNAL_INVALID")
	}
	return recoverStatelessAgentCallJournal(directory, client)
}

func openAgenticScenarioJournal(
	directory string,
	resume bool,
	client agentIntentTransport,
) (*scenarioAgentCallJournal, error) {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return newScenarioAgentCallJournal(directory, client, "")
	}
	if err != nil || !resume || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("AGENTIC_EPISODE_SCENARIO_JOURNAL_INVALID")
	}
	return recoverScenarioAgentCallJournal(directory, client)
}

func agenticEpisodeBudgetFromExperiment(
	scenarioCalls int,
	maxPlanSteps int,
	maxRuntimeDecisions int,
	logical controlexperiment.AgenticLogicalBudget,
) (agenticEpisodeBudget, error) {
	if logical.Validate() != nil {
		return agenticEpisodeBudget{}, errors.New("AGENTIC_EPISODE_LOGICAL_BUDGET_INVALID")
	}
	riskCalls := logical.MaxModelCalls - scenarioCalls
	if riskCalls > controlexperiment.RiskAgentMaxCalls {
		riskCalls = controlexperiment.RiskAgentMaxCalls
	}
	logicalCopy := logical
	budget := agenticEpisodeBudget{
		MaxRiskCalls: riskCalls, MaxScenarioCalls: scenarioCalls,
		MaxTotalCalls: logical.MaxModelCalls, MaxObservedTokens: logical.MaxModelTokens,
		MaxScenarioPlanSteps: maxPlanSteps, MaxRuntimeDecisions: maxRuntimeDecisions,
		Logical: &logicalCopy,
	}
	return budget, budget.validate()
}

package defectbench

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	// v4 requires evaluator-owned SUT replay. Historical v3 reports only
	// recorded which side claimed Replay authority and remain read-only data.
	AgenticHoldoutEvaluationSchemaVersion = "consensus-atlas/agentic-holdout-evaluation/v4"

	AgenticEpisodeCompleted       = "completed"
	AgenticEpisodeRiskStopped     = "risk-agent-stopped"
	AgenticEpisodeScenarioStopped = "scenario-agent-stopped"
	AgenticEpisodeTokenStopped    = "model-token-threshold-reached"
	AgenticEpisodeExecutionFailed = "execution-failed"

	AgenticReplayAuthorityEvaluatorOwned = "evaluator-owned-sut-replay"
)

type AgenticReplayRunner func(
	trialID string,
	bundle controlexperiment.ExecutionBundle,
) (controlexperiment.ExecutionBundle, error)

// AgenticTrialEvidence is the narrow evaluator projection of one Episode.
// EpisodeStatus and EvidenceStatus are method-reported explanatory data.
// The CLI reconstructs ModelWork from the durable provider-call journals;
// only the evaluator's projector and monitors may determine Result.
type AgenticTrialEvidence struct {
	TargetID               string
	MethodSpecDigest       string
	MethodSpec             controlexperiment.AgenticMethodSpec
	EpisodeCount           int
	EpisodeStatus          string
	EvidenceStatus         string
	Budget                 controlexperiment.AgenticLogicalBudget
	RiskAttempts           int
	ScenarioAttempts       int
	ScenarioDecisionsUsed  int
	ModelWork              controlexperiment.ModelWork
	ScenarioFrontier       controlexperiment.PhaseWork
	ScenarioSearch         controlexperiment.ScenarioExecutionWork
	Preparation            controlexperiment.AgenticPreparationWork
	DecisionProvenance     controlexperiment.AgenticDecisionProvenance
	Bundle                 *controlexperiment.ExecutionBundle
	BundleRootDecisions    int
	CandidateBundles       []controlexperiment.ExecutionBundle
	CandidateRootDecisions []int
}

type AgenticHoldoutTrialResult struct {
	TrialID               string                                      `json:"trial_id"`
	TargetID              string                                      `json:"target_id"`
	EpisodeStatus         string                                      `json:"episode_status"`
	EvidenceStatus        string                                      `json:"reported_evidence_status"`
	ModelWork             controlexperiment.ModelWork                 `json:"model_work"`
	ReplayAuthority       string                                      `json:"replay_authority"`
	ScenarioDecisionsUsed int                                         `json:"scenario_decisions_used"`
	DecisionProvenance    controlexperiment.AgenticDecisionProvenance `json:"decision_provenance"`
	RootPrefixFindings    int                                         `json:"root_prefix_oracle_findings,omitempty"`
	Result                BundleTrialResult                           `json:"trusted_result"`
}

type AgenticHoldoutEvaluation struct {
	SchemaVersion       string                      `json:"schema_version"`
	BenchmarkID         string                      `json:"benchmark_id"`
	ContractDigest      string                      `json:"contract_digest"`
	MethodSpecDigest    string                      `json:"method_spec_digest"`
	ExposureAuditDigest string                      `json:"exposure_audit_digest"`
	TargetID            string                      `json:"target_id"`
	ReplayAuthority     string                      `json:"replay_authority"`
	Pairs               []FormalPairEvaluation      `json:"pairs"`
	Results             []AgenticHoldoutTrialResult `json:"results"`
	Summary             FormalEvaluationSummary     `json:"summary"`
}

// EvaluateAgenticHoldoutBundles consumes Agentic search artifacts but never
// trusts their recorded Replay as the final execution authority. The supplied
// private runner starts the trial SUT and executes each sealed recipe again;
// projection and Oracle evaluation consume only that fresh Bundle.
func EvaluateAgenticHoldoutBundles(
	contract FormalBenchmarkContract,
	exposure FormalExposureAudit,
	evidence map[string]AgenticTrialEvidence,
	replay AgenticReplayRunner,
	projector semantic.DecisionProjector,
	registeredMonitors ...oracle.BundleMonitor,
) (AgenticHoldoutEvaluation, error) {
	if replay == nil {
		return AgenticHoldoutEvaluation{}, errors.New("AGENTIC_HOLDOUT_REPLAY_RUNNER_REQUIRED")
	}
	monitors, err := admitAgenticHoldoutEvaluation(
		contract, exposure, projector, registeredMonitors...,
	)
	if err != nil {
		return AgenticHoldoutEvaluation{}, err
	}
	if len(evidence) != len(contract.Pairs)*2 {
		return AgenticHoldoutEvaluation{}, errors.New("AGENTIC_HOLDOUT_EVIDENCE_SET_MISMATCH")
	}
	targetID := ""
	for trialID, current := range evidence {
		if !validBundleID(trialID) || strings.TrimSpace(current.TargetID) == "" ||
			current.TargetID != strings.TrimSpace(current.TargetID) ||
			(targetID != "" && current.TargetID != targetID) {
			return AgenticHoldoutEvaluation{}, errors.New("AGENTIC_HOLDOUT_TARGET_SET_INVALID")
		}
		targetID = current.TargetID
	}
	report := AgenticHoldoutEvaluation{
		SchemaVersion: AgenticHoldoutEvaluationSchemaVersion,
		BenchmarkID:   contract.ID, ContractDigest: contract.Digest,
		MethodSpecDigest:    contract.MethodSpecDigest,
		ExposureAuditDigest: exposure.Digest, TargetID: targetID,
		ReplayAuthority: AgenticReplayAuthorityEvaluatorOwned,
	}
	budget := BundleBenchmark{Budget: contract.Budget}
	trusted := make([]BundleTrialResult, 0, len(evidence))
	for _, pair := range contract.Pairs {
		report.Pairs = append(report.Pairs, FormalPairEvaluation{
			PairID: pair.PairID, RootCauseID: pair.RootCauseID,
			ControlTrialID: pair.Control.TrialID, CandidateTrialID: pair.Candidate.TrialID,
		})
		for _, input := range []struct {
			variant FormalVariant
			kind    string
			root    string
		}{
			{variant: pair.Control, kind: BundleKindControl},
			{variant: pair.Candidate, kind: FormalBundleKindCandidate, root: pair.RootCauseID},
		} {
			current, ok := evidence[input.variant.TrialID]
			if !ok {
				return AgenticHoldoutEvaluation{}, fmt.Errorf(
					"AGENTIC_HOLDOUT_TRIAL_MISSING: %s", input.variant.TrialID,
				)
			}
			variant := formalBundleVariant(input.variant, input.kind, input.root)
			rootPrefixFindings := 0
			result := evaluateAgenticHoldoutTrial(
				contract, budget, variant, current, replay, projector, monitors,
				&rootPrefixFindings,
			)
			trusted = append(trusted, result)
			report.Results = append(report.Results, AgenticHoldoutTrialResult{
				TrialID: input.variant.TrialID, TargetID: current.TargetID,
				EpisodeStatus: current.EpisodeStatus, EvidenceStatus: current.EvidenceStatus,
				ModelWork:             current.ModelWork,
				ScenarioDecisionsUsed: current.ScenarioDecisionsUsed,
				DecisionProvenance:    current.DecisionProvenance,
				RootPrefixFindings:    rootPrefixFindings,
				ReplayAuthority:       AgenticReplayAuthorityEvaluatorOwned, Result: result,
			})
		}
	}
	sort.Slice(report.Pairs, func(i, j int) bool { return report.Pairs[i].PairID < report.Pairs[j].PairID })
	sort.Slice(report.Results, func(i, j int) bool { return report.Results[i].TrialID < report.Results[j].TrialID })
	report.Summary = summarizeFormalResults(trusted)
	if err := report.Validate(); err != nil {
		return AgenticHoldoutEvaluation{}, err
	}
	return report, nil
}

func admitAgenticHoldoutEvaluation(
	contract FormalBenchmarkContract,
	exposure FormalExposureAudit,
	projector semantic.DecisionProjector,
	registeredMonitors ...oracle.BundleMonitor,
) ([]oracle.BundleMonitor, error) {
	if err := contract.Validate(); err != nil {
		return nil, err
	}
	view, err := contract.OpaqueView()
	if err != nil {
		return nil, err
	}
	if err := exposure.Validate(); err != nil || !exposure.Passed ||
		exposure.BenchmarkID != contract.ID || exposure.ContractDigest != contract.Digest ||
		exposure.ExpectedOpaqueViewDigest != view.Digest {
		return nil, errors.New("AGENTIC_HOLDOUT_EXPOSURE_AUDIT_REQUIRED")
	}
	if contract.RequiredBundleSchema != controlexperiment.ExecutionBundleSchemaVersion &&
		contract.RequiredBundleSchema != controlexperiment.ExecutionBundleSchemaVersionV2 &&
		contract.RequiredBundleSchema != controlexperiment.ExecutionBundleSchemaVersionV3 {
		return nil, errors.New("AGENTIC_HOLDOUT_BUNDLE_SCHEMA_UNSUPPORTED")
	}
	if contract.RequiredBundleSchema != controlexperiment.ExecutionBundleSchemaVersionV3 ||
		contract.AgenticBudget == nil || contract.AgenticBudget.Validate() != nil {
		return nil, errors.New("AGENTIC_HOLDOUT_FORMAL_METHOD_BUDGET_REQUIRED")
	}
	return ResolveFormalComposition(contract, projector, registeredMonitors...)
}

func evaluateAgenticHoldoutTrial(
	contract FormalBenchmarkContract,
	budget BundleBenchmark,
	variant BundleVariant,
	evidence AgenticTrialEvidence,
	replay AgenticReplayRunner,
	projector semantic.DecisionProjector,
	monitors []oracle.BundleMonitor,
	rootPrefixFindings *int,
) BundleTrialResult {
	bundles, rootBoundaries, boundariesOK := agenticEvidenceBundles(evidence)
	invalid := func(reason string) BundleTrialResult {
		result := BundleTrialResult{
			TrialID: variant.TrialID, VariantID: variant.VariantID, Kind: variant.Kind,
			RootCauseID: variant.RootCauseID, Status: BundleStatusInvalid, InvalidReason: reason,
		}
		if len(bundles) != 0 {
			result.BundleDigest = bundles[0].Digest
			result.BuildID = bundles[0].Qualification.Manifest.BuildID
		}
		return result
	}
	if !boundariesOK || !validAgenticTrialEvidence(evidence) {
		return invalid("AGENTIC_HOLDOUT_EPISODE_METADATA_INVALID")
	}
	if evidence.DecisionProvenance.Validate(evidence.ScenarioDecisionsUsed) != nil {
		return invalid("AGENTIC_HOLDOUT_DECISION_PROVENANCE_INVALID")
	}
	if evidence.EpisodeStatus != AgenticEpisodeCompleted {
		if len(bundles) != 0 {
			return invalid("AGENTIC_HOLDOUT_UNEXPECTED_BUNDLE")
		}
		return invalid("AGENTIC_HOLDOUT_EPISODE_INCOMPLETE")
	}
	if len(bundles) == 0 {
		return invalid("AGENTIC_HOLDOUT_COMPLETED_BUNDLE_MISSING")
	}
	if evidence.MethodSpecDigest != contract.MethodSpecDigest {
		return invalid("AGENTIC_HOLDOUT_METHOD_IDENTITY_MISMATCH")
	}
	if evidence.MethodSpec.Validate() != nil ||
		evidence.MethodSpec.Digest != evidence.MethodSpecDigest ||
		evidence.MethodSpec.TargetID != evidence.TargetID ||
		evidence.EpisodeCount != evidence.MethodSpec.InvestigationEpisodes {
		return invalid("AGENTIC_HOLDOUT_METHOD_SPEC_MISMATCH")
	}
	if evidence.Budget != *contract.AgenticBudget {
		return invalid("AGENTIC_HOLDOUT_AGENTIC_BUDGET_MISMATCH")
	}
	if evidence.MethodSpec.InvestigationBudget != evidence.Budget ||
		len(bundles) > evidence.Budget.MaxAttempts {
		return invalid("AGENTIC_HOLDOUT_ATTEMPT_BUDGET_EXCEEDED")
	}
	if evidence.ModelWork.Calls > evidence.Budget.MaxModelCalls ||
		evidence.ModelWork.TotalTokens > evidence.Budget.MaxModelTokens {
		return invalid("AGENTIC_HOLDOUT_MODEL_BUDGET_EXCEEDED")
	}
	searchDecisions, searchPrimary, searchReplay, searchOK := agenticSearchWork(evidence)
	if !searchOK || evidence.ScenarioDecisionsUsed >
		evidence.ScenarioSearch.ChildMaterialization.SchedulerDecisions {
		return invalid("AGENTIC_HOLDOUT_SEARCH_WORK_INVALID")
	}
	if evidence.Preparation.Validate() != nil ||
		evidence.MethodSpec.EpisodeLimits.PreparationWallClockMS <= 0 ||
		evidence.Preparation.WallClockMS > evidence.MethodSpec.EpisodeLimits.PreparationWallClockMS {
		return invalid("AGENTIC_HOLDOUT_PREPARATION_WORK_INVALID")
	}
	var preparationOK bool
	searchDecisions, preparationOK = addAgenticPreparationWork(
		searchDecisions, evidence.Preparation.Qualification.SchedulerDecisions,
	)
	if preparationOK {
		searchPrimary, preparationOK = addAgenticPreparationWork(
			searchPrimary, evidence.Preparation.Qualification.WorkUnits,
		)
	}
	if !preparationOK {
		return invalid("AGENTIC_HOLDOUT_PREPARATION_WORK_INVALID")
	}
	searchDecisions, preparationOK = addAgenticPreparationWork(
		searchDecisions, evidence.Preparation.Root.Primary.SchedulerDecisions,
	)
	if preparationOK {
		searchPrimary, preparationOK = addAgenticPreparationWork(
			searchPrimary, evidence.Preparation.Root.Primary.WorkUnits,
		)
	}
	if preparationOK {
		searchReplay, preparationOK = addAgenticPreparationWork(
			searchReplay, evidence.Preparation.Root.Replay.WorkUnits,
		)
	}
	if !preparationOK {
		return invalid("AGENTIC_HOLDOUT_PREPARATION_WORK_INVALID")
	}
	results := make([]BundleTrialResult, 0, len(bundles))
	for index, bundle := range bundles {
		if bundle.SchemaVersion != contract.RequiredBundleSchema ||
			bundle.Qualification.Profile.Digest != contract.ProfileDigest ||
			bundle.Identity.MethodSpecDigest != contract.MethodSpecDigest || bundle.Recipe == nil ||
			bundle.Recipe.TargetID != evidence.TargetID {
			return invalid("AGENTIC_HOLDOUT_BUNDLE_CONTRACT_MISMATCH")
		}
		fresh, err := replay(variant.TrialID, bundle)
		if err != nil || !sameAgenticReplayEvidence(bundle, fresh) {
			return invalid("AGENTIC_HOLDOUT_EVALUATOR_REPLAY_MISMATCH")
		}
		result := evaluateBundleVariantWithMonitors(
			budget, variant, fresh, projector, monitors,
		)
		result, rootFindings := attributeAgenticPostRootFinding(
			result, rootBoundaries[index], fresh.Trace.Digest,
		)
		if rootPrefixFindings != nil {
			*rootPrefixFindings += rootFindings
		}
		results = append(results, result)
	}
	for _, result := range results {
		if result.Status == BundleStatusInvalid {
			return result
		}
	}
	totalDecisions, totalPrimary, totalReplay, totalsOK := agenticAggregateTrialWork(results)
	if totalsOK {
		totalDecisions, totalsOK = safeAgenticAdd(totalDecisions, searchDecisions)
	}
	if totalsOK {
		totalPrimary, totalsOK = safeAgenticAdd(totalPrimary, searchPrimary)
	}
	if totalsOK {
		totalReplay, totalsOK = safeAgenticAdd(totalReplay, searchReplay)
	}
	if !totalsOK || totalDecisions > contract.Budget.MaxDecisions ||
		totalPrimary > contract.Budget.MaxPrimaryWorkUnits {
		result := invalid("AGENTIC_HOLDOUT_AGGREGATE_BUDGET_EXCEEDED")
		result.Decisions, result.PrimaryWork, result.ReplayWork = totalDecisions, totalPrimary, totalReplay
		return result
	}
	if totalReplay > evidence.Budget.MaxReplayWorkUnits {
		result := invalid("AGENTIC_HOLDOUT_REPLAY_BUDGET_EXCEEDED")
		result.Decisions, result.PrimaryWork, result.ReplayWork = totalDecisions, totalPrimary, totalReplay
		return result
	}
	for _, result := range results {
		if result.Finding != nil {
			result.Decisions, result.PrimaryWork, result.ReplayWork = totalDecisions, totalPrimary, totalReplay
			return result
		}
	}
	result := results[0]
	result.Decisions, result.PrimaryWork, result.ReplayWork = totalDecisions, totalPrimary, totalReplay
	return result
}

// attributeAgenticPostRootFinding keeps the evaluator-owned complete Oracle
// result for audit, but permits only a violation emitted after the sealed root
// prefix to determine whether the Agent method found an issue. Trace-integrity
// failures remain invalid before this function is called.
func attributeAgenticPostRootFinding(
	result BundleTrialResult,
	rootDecisions int,
	traceDigest string,
) (BundleTrialResult, int) {
	if result.Status == BundleStatusInvalid {
		return result, 0
	}
	rootFindings := 0
	var finding *oracle.Violation
	for index := range result.Oracle.Violations {
		violation := &result.Oracle.Violations[index]
		if violation.Step <= rootDecisions {
			rootFindings++
			continue
		}
		if finding == nil {
			finding = violation
		}
	}
	result.Finding = nil
	if finding != nil {
		result.Finding = &BundleFinding{
			Monitor: finding.Monitor, Step: finding.Step, Message: finding.Message,
			TraceDigest: traceDigest,
		}
	}
	if result.Kind == BundleKindControl {
		if finding == nil {
			result.Status = BundleStatusControlPass
		} else {
			result.Status = BundleStatusFalsePositive
		}
	} else if finding == nil {
		result.Status = BundleStatusSurvived
	} else {
		result.Status = BundleStatusKilled
	}
	return result, rootFindings
}

func sameAgenticReplayEvidence(
	source, fresh controlexperiment.ExecutionBundle,
) bool {
	return source.Validate() == nil && fresh.Validate() == nil && source.Recipe != nil && fresh.Recipe != nil &&
		fresh.SchemaVersion == source.SchemaVersion && fresh.Identity == source.Identity &&
		fresh.Trace.Digest == source.Trace.Digest && len(fresh.Trace.Records) == len(source.Trace.Records) &&
		reflect.DeepEqual(fresh.Recipe, source.Recipe) &&
		reflect.DeepEqual(fresh.Qualification, source.Qualification)
}

func addAgenticPreparationWork(left, right int) (int, bool) {
	return safeAgenticAdd(left, right)
}

func agenticSearchWork(evidence AgenticTrialEvidence) (int, int, int, bool) {
	primaryPhases := []controlexperiment.PhaseWork{
		evidence.ScenarioFrontier,
		evidence.ScenarioSearch.FrontierReconstruction,
		evidence.ScenarioSearch.ChildMaterialization,
	}
	decisions, primary := 0, 0
	for _, phase := range primaryPhases {
		if phase.SetupAttempts < 0 || phase.RuntimeInitializations < 0 ||
			phase.RuntimeInitializations > phase.SetupAttempts || phase.PrepareActions < 0 ||
			phase.SchedulerDecisions < 0 ||
			phase.WorkUnits != phase.SetupAttempts+phase.PrepareActions+phase.SchedulerDecisions {
			return 0, 0, 0, false
		}
		var ok bool
		decisions, ok = safeAgenticAdd(decisions, phase.SchedulerDecisions)
		if !ok {
			return 0, 0, 0, false
		}
		primary, ok = safeAgenticAdd(primary, phase.WorkUnits)
		if !ok {
			return 0, 0, 0, false
		}
	}
	verification := evidence.ScenarioSearch.ChildVerification
	if verification.SetupAttempts < 0 || verification.RuntimeInitializations < 0 ||
		verification.RuntimeInitializations > verification.SetupAttempts || verification.PrepareActions < 0 ||
		verification.SchedulerDecisions < 0 ||
		verification.WorkUnits != verification.SetupAttempts+verification.PrepareActions+
			verification.SchedulerDecisions {
		return 0, 0, 0, false
	}
	searchTotal, ok := safeAgenticAdd(
		evidence.ScenarioSearch.FrontierReconstruction.WorkUnits,
		evidence.ScenarioSearch.ChildMaterialization.WorkUnits,
	)
	if ok {
		searchTotal, ok = safeAgenticAdd(searchTotal, verification.WorkUnits)
	}
	if !ok || evidence.ScenarioSearch.TotalWorkUnits != searchTotal {
		return 0, 0, 0, false
	}
	return decisions, primary, verification.WorkUnits, true
}

func agenticAggregateTrialWork(results []BundleTrialResult) (int, int, int, bool) {
	decisions, primary, replay := 0, 0, 0
	for _, result := range results {
		if result.Decisions < 0 || result.PrimaryWork < 0 || result.ReplayWork < 0 {
			return decisions, primary, replay, false
		}
		var ok bool
		decisions, ok = safeAgenticAdd(decisions, result.Decisions)
		if !ok {
			return decisions, primary, replay, false
		}
		primary, ok = safeAgenticAdd(primary, result.PrimaryWork)
		if !ok {
			return decisions, primary, replay, false
		}
		replay, ok = safeAgenticAdd(replay, result.ReplayWork)
		if !ok {
			return decisions, primary, replay, false
		}
	}
	return decisions, primary, replay, true
}

func safeAgenticAdd(left int, right int) (int, bool) {
	maxInt := int(^uint(0) >> 1)
	if left < 0 || right < 0 || right > maxInt-left {
		return left, false
	}
	return left + right, true
}

func agenticEvidenceBundles(
	evidence AgenticTrialEvidence,
) ([]controlexperiment.ExecutionBundle, []int, bool) {
	bundles := make([]controlexperiment.ExecutionBundle, 0, len(evidence.CandidateBundles)+1)
	rootBoundaries := make([]int, 0, len(evidence.CandidateBundles)+1)
	if evidence.Bundle != nil {
		bundles = append(bundles, *evidence.Bundle)
		rootBoundaries = append(rootBoundaries, evidence.BundleRootDecisions)
	}
	bundles = append(bundles, evidence.CandidateBundles...)
	rootBoundaries = append(rootBoundaries, evidence.CandidateRootDecisions...)
	if len(bundles) != len(rootBoundaries) {
		return bundles, rootBoundaries, false
	}
	for index, boundary := range rootBoundaries {
		if boundary < 0 || boundary > len(bundles[index].Trace.Records) {
			return bundles, rootBoundaries, false
		}
	}
	return bundles, rootBoundaries, true
}

func validAgenticTrialEvidence(evidence AgenticTrialEvidence) bool {
	if !validAgenticTrialMetadata(
		evidence.TargetID, evidence.EpisodeStatus, evidence.EvidenceStatus, evidence.ModelWork,
	) ||
		!bundleDigestValid(evidence.MethodSpecDigest) || evidence.Budget.Validate() != nil ||
		evidence.MethodSpec.Validate() != nil || evidence.EpisodeCount <= 0 ||
		evidence.RiskAttempts < 0 || evidence.ScenarioAttempts < 0 ||
		evidence.RiskAttempts+evidence.ScenarioAttempts != evidence.ModelWork.Calls ||
		evidence.ScenarioDecisionsUsed < 0 ||
		(evidence.Bundle == nil && evidence.BundleRootDecisions != 0) ||
		len(evidence.CandidateRootDecisions) != len(evidence.CandidateBundles) ||
		len(evidence.CandidateBundles)+boolAgenticInt(evidence.Bundle != nil) > evidence.Budget.MaxAttempts {
		return false
	}
	return true
}

func boolAgenticInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func validAgenticTrialMetadata(
	targetID string,
	episodeStatus string,
	evidenceStatus string,
	modelWork controlexperiment.ModelWork,
) bool {
	if strings.TrimSpace(targetID) == "" || strings.TrimSpace(evidenceStatus) == "" ||
		modelWork.Calls < 0 || modelWork.InputTokens < 0 || modelWork.OutputTokens < 0 ||
		modelWork.TotalTokens < 0 ||
		modelWork.TotalTokens != modelWork.InputTokens+modelWork.OutputTokens {
		return false
	}
	switch episodeStatus {
	case AgenticEpisodeCompleted, AgenticEpisodeRiskStopped, AgenticEpisodeScenarioStopped,
		AgenticEpisodeTokenStopped, AgenticEpisodeExecutionFailed:
		return true
	default:
		return false
	}
}

func (report AgenticHoldoutEvaluation) Validate() error {
	if report.SchemaVersion != AgenticHoldoutEvaluationSchemaVersion ||
		!validBundleID(report.BenchmarkID) || !bundleDigestValid(report.ContractDigest) ||
		!bundleDigestValid(report.MethodSpecDigest) ||
		!bundleDigestValid(report.ExposureAuditDigest) || strings.TrimSpace(report.TargetID) == "" ||
		report.ReplayAuthority != AgenticReplayAuthorityEvaluatorOwned ||
		len(report.Pairs) < 3 || len(report.Results) != len(report.Pairs)*2 {
		return errors.New("AGENTIC_HOLDOUT_EVALUATION_INVALID")
	}
	trusted := make([]BundleTrialResult, 0, len(report.Results))
	seen := make(map[string]bool, len(report.Results))
	for _, current := range report.Results {
		_, provenanceOK := current.DecisionProvenance.Total()
		if !validBundleID(current.TrialID) || seen[current.TrialID] || current.TargetID != report.TargetID ||
			current.ReplayAuthority != report.ReplayAuthority ||
			current.RootPrefixFindings < 0 ||
			!provenanceOK || current.DecisionProvenance.Validate(current.ScenarioDecisionsUsed) != nil ||
			current.TrialID != current.Result.TrialID || !validAgenticTrialMetadata(
			current.TargetID, current.EpisodeStatus, current.EvidenceStatus, current.ModelWork,
		) {
			return errors.New("AGENTIC_HOLDOUT_TRIAL_RESULT_INVALID")
		}
		if err := validateAgenticTrustedResult(current.Result); err != nil {
			return err
		}
		seen[current.TrialID] = true
		trusted = append(trusted, current.Result)
	}
	if report.Summary != summarizeFormalResults(trusted) {
		return errors.New("AGENTIC_HOLDOUT_SUMMARY_INVALID")
	}
	return validateAgenticHoldoutPairs(report.Pairs, report.Results)
}

func validateAgenticTrustedResult(result BundleTrialResult) error {
	if !validBundleID(result.VariantID) || result.Decisions < 0 || result.PrimaryWork < 0 || result.ReplayWork < 0 {
		return errors.New("AGENTIC_HOLDOUT_TRUSTED_RESULT_INVALID")
	}
	switch result.Kind {
	case BundleKindControl:
		if result.RootCauseID != "" ||
			(result.Status != BundleStatusControlPass && result.Status != BundleStatusFalsePositive &&
				result.Status != BundleStatusInvalid) {
			return errors.New("AGENTIC_HOLDOUT_CONTROL_RESULT_INVALID")
		}
	case FormalBundleKindCandidate:
		if !validBundleID(result.RootCauseID) ||
			(result.Status != BundleStatusSurvived && result.Status != BundleStatusKilled &&
				result.Status != BundleStatusInvalid) {
			return errors.New("AGENTIC_HOLDOUT_CANDIDATE_RESULT_INVALID")
		}
	default:
		return errors.New("AGENTIC_HOLDOUT_RESULT_KIND_INVALID")
	}
	if result.Status == BundleStatusInvalid {
		if result.InvalidReason == "" || result.Finding != nil {
			return errors.New("AGENTIC_HOLDOUT_INVALID_RESULT_MALFORMED")
		}
		return nil
	}
	if !bundleDigestValid(result.BundleDigest) || result.BuildID == "" || result.InvalidReason != "" ||
		((result.Status == BundleStatusKilled || result.Status == BundleStatusFalsePositive) !=
			(result.Finding != nil)) {
		return errors.New("AGENTIC_HOLDOUT_TRUSTED_RESULT_INVALID")
	}
	if result.Finding != nil &&
		(result.Finding.Monitor == "" || result.Finding.Message == "" ||
			!bundleDigestValid(result.Finding.TraceDigest)) {
		return errors.New("AGENTIC_HOLDOUT_FINDING_INVALID")
	}
	return nil
}

func validateAgenticHoldoutPairs(
	pairs []FormalPairEvaluation,
	results []AgenticHoldoutTrialResult,
) error {
	byTrial := make(map[string]BundleTrialResult, len(results))
	for _, result := range results {
		byTrial[result.TrialID] = result.Result
	}
	used, pairIDs, roots := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, pair := range pairs {
		controlResult, controlOK := byTrial[pair.ControlTrialID]
		candidateResult, candidateOK := byTrial[pair.CandidateTrialID]
		if !validBundleID(pair.PairID) || pairIDs[pair.PairID] || !validBundleID(pair.RootCauseID) ||
			!controlOK || !candidateOK || used[pair.ControlTrialID] || used[pair.CandidateTrialID] ||
			controlResult.Kind != BundleKindControl || candidateResult.Kind != FormalBundleKindCandidate ||
			candidateResult.RootCauseID != pair.RootCauseID {
			return errors.New("AGENTIC_HOLDOUT_PAIR_INVALID")
		}
		pairIDs[pair.PairID], roots[pair.RootCauseID] = true, true
		used[pair.ControlTrialID], used[pair.CandidateTrialID] = true, true
	}
	if len(used) != len(results) || len(roots) < 3 {
		return errors.New("AGENTIC_HOLDOUT_PAIR_SET_INVALID")
	}
	return nil
}

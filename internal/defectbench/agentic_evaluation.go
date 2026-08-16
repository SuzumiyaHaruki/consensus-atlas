package defectbench

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	AgenticHoldoutEvaluationSchemaVersion = "consensus-atlas/agentic-holdout-evaluation/v1"

	AgenticEpisodeCompleted       = "completed"
	AgenticEpisodeRiskStopped     = "risk-agent-stopped"
	AgenticEpisodeScenarioStopped = "scenario-agent-stopped"
	AgenticEpisodeTokenStopped    = "model-token-threshold-reached"
	AgenticEpisodeExecutionFailed = "execution-failed"
)

// AgenticTrialEvidence is the narrow evaluator projection of one Episode.
// EpisodeStatus, EvidenceStatus and ModelWork are method-reported explanatory
// data. Only the evaluator's projector and monitors may determine Result.
type AgenticTrialEvidence struct {
	TargetID       string
	EpisodeStatus  string
	EvidenceStatus string
	ModelWork      controlexperiment.ModelWork
	Bundle         *controlexperiment.ExecutionBundle
}

type AgenticHoldoutTrialResult struct {
	TrialID        string                      `json:"trial_id"`
	TargetID       string                      `json:"target_id"`
	EpisodeStatus  string                      `json:"episode_status"`
	EvidenceStatus string                      `json:"reported_evidence_status"`
	ModelWork      controlexperiment.ModelWork `json:"model_work"`
	Result         BundleTrialResult           `json:"trusted_result"`
}

type AgenticHoldoutEvaluation struct {
	SchemaVersion       string                      `json:"schema_version"`
	BenchmarkID         string                      `json:"benchmark_id"`
	ContractDigest      string                      `json:"contract_digest"`
	ExposureAuditDigest string                      `json:"exposure_audit_digest"`
	TargetID            string                      `json:"target_id"`
	Pairs               []FormalPairEvaluation      `json:"pairs"`
	Results             []AgenticHoldoutTrialResult `json:"results"`
	Summary             FormalEvaluationSummary     `json:"summary"`
}

// EvaluateAgenticHoldoutBundles consumes already-produced Agentic artifacts.
// It does not execute a second search method. The existing formal contract
// supplies private pairing, budget, build identity and monitor composition;
// the existing Bundle evaluator recomputes every verdict.
func EvaluateAgenticHoldoutBundles(
	contract FormalBenchmarkContract,
	exposure FormalExposureAudit,
	evidence map[string]AgenticTrialEvidence,
	projector semantic.DecisionProjector,
	registeredMonitors ...oracle.BundleMonitor,
) (AgenticHoldoutEvaluation, error) {
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
		ExposureAuditDigest: exposure.Digest, TargetID: targetID,
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
			result := evaluateAgenticHoldoutTrial(
				contract, budget, variant, current, projector, monitors,
			)
			trusted = append(trusted, result)
			report.Results = append(report.Results, AgenticHoldoutTrialResult{
				TrialID: input.variant.TrialID, TargetID: current.TargetID,
				EpisodeStatus: current.EpisodeStatus, EvidenceStatus: current.EvidenceStatus,
				ModelWork: current.ModelWork, Result: result,
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
	return ResolveFormalComposition(contract, projector, registeredMonitors...)
}

func evaluateAgenticHoldoutTrial(
	contract FormalBenchmarkContract,
	budget BundleBenchmark,
	variant BundleVariant,
	evidence AgenticTrialEvidence,
	projector semantic.DecisionProjector,
	monitors []oracle.BundleMonitor,
) BundleTrialResult {
	invalid := func(reason string) BundleTrialResult {
		result := BundleTrialResult{
			TrialID: variant.TrialID, VariantID: variant.VariantID, Kind: variant.Kind,
			RootCauseID: variant.RootCauseID, Status: BundleStatusInvalid, InvalidReason: reason,
		}
		if evidence.Bundle != nil {
			result.BundleDigest = evidence.Bundle.Digest
			result.BuildID = evidence.Bundle.Qualification.Manifest.BuildID
		}
		return result
	}
	if !validAgenticTrialEvidence(evidence) {
		return invalid("AGENTIC_HOLDOUT_EPISODE_METADATA_INVALID")
	}
	if evidence.EpisodeStatus != AgenticEpisodeCompleted {
		if evidence.Bundle != nil {
			return invalid("AGENTIC_HOLDOUT_UNEXPECTED_BUNDLE")
		}
		return invalid("AGENTIC_HOLDOUT_EPISODE_INCOMPLETE")
	}
	if evidence.Bundle == nil {
		return invalid("AGENTIC_HOLDOUT_COMPLETED_BUNDLE_MISSING")
	}
	if evidence.Bundle.SchemaVersion != contract.RequiredBundleSchema ||
		evidence.Bundle.Qualification.Profile.Digest != contract.ProfileDigest {
		return invalid("AGENTIC_HOLDOUT_BUNDLE_CONTRACT_MISMATCH")
	}
	return evaluateBundleVariantWithMonitors(
		budget, variant, *evidence.Bundle, projector, monitors,
	)
}

func validAgenticTrialEvidence(evidence AgenticTrialEvidence) bool {
	if strings.TrimSpace(evidence.TargetID) == "" || strings.TrimSpace(evidence.EvidenceStatus) == "" ||
		evidence.ModelWork.Calls < 0 || evidence.ModelWork.InputTokens < 0 ||
		evidence.ModelWork.OutputTokens < 0 || evidence.ModelWork.TotalTokens < 0 ||
		evidence.ModelWork.TotalTokens != evidence.ModelWork.InputTokens+evidence.ModelWork.OutputTokens {
		return false
	}
	switch evidence.EpisodeStatus {
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
		!bundleDigestValid(report.ExposureAuditDigest) || strings.TrimSpace(report.TargetID) == "" ||
		len(report.Pairs) < 3 || len(report.Results) != len(report.Pairs)*2 {
		return errors.New("AGENTIC_HOLDOUT_EVALUATION_INVALID")
	}
	trusted := make([]BundleTrialResult, 0, len(report.Results))
	seen := make(map[string]bool, len(report.Results))
	for _, current := range report.Results {
		if !validBundleID(current.TrialID) || seen[current.TrialID] || current.TargetID != report.TargetID ||
			current.TrialID != current.Result.TrialID || !validAgenticTrialEvidence(AgenticTrialEvidence{
			TargetID: current.TargetID, EpisodeStatus: current.EpisodeStatus,
			EvidenceStatus: current.EvidenceStatus, ModelWork: current.ModelWork,
		}) {
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

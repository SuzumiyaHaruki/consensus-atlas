package defectbench

import (
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	FormalFreshEvaluationSchemaVersion = "consensus-atlas/formal-fresh-evaluation/v1"
	FormalBundleKindCandidate          = "candidate"
)

type FormalPairEvaluation struct {
	PairID           string `json:"pair_id"`
	RootCauseID      string `json:"root_cause_id"`
	ControlTrialID   string `json:"control_trial_id"`
	CandidateTrialID string `json:"candidate_trial_id"`
}

type FormalEvaluationSummary struct {
	Controls         int `json:"controls"`
	FalsePositives   int `json:"false_positives"`
	Candidates       int `json:"candidates"`
	KilledCandidates int `json:"killed_candidates"`
	RootCauses       int `json:"root_causes"`
	KilledRootCauses int `json:"killed_root_causes"`
	InvalidTrials    int `json:"invalid_trials"`
}

// EvaluateFormalMethodBundlesInMemory applies the contract's SUT pairing,
// composition, profile and budget to one method's bundle set. The caller owns
// build-audit admission and supplies the method's actual bundle schema. The
// returned existing trial result type is not sealed as a formal evaluation.
func EvaluateFormalMethodBundlesInMemory(
	contract FormalBenchmarkContract,
	requiredBundleSchema string,
	bundles map[string]controlexperiment.ExecutionBundle,
	projector semantic.DecisionProjector,
	registeredMonitors ...oracle.BundleMonitor,
) ([]BundleTrialResult, FormalEvaluationSummary, error) {
	if requiredBundleSchema != controlexperiment.ExecutionBundleSchemaVersion &&
		requiredBundleSchema != controlexperiment.ExecutionBundleSchemaVersionV2 &&
		requiredBundleSchema != controlexperiment.ExecutionBundleSchemaVersionV3 {
		return nil, FormalEvaluationSummary{}, errors.New("FORMAL_METHOD_BUNDLE_SCHEMA_UNSUPPORTED")
	}
	monitors, err := ResolveFormalComposition(contract, projector, registeredMonitors...)
	if err != nil {
		return nil, FormalEvaluationSummary{}, err
	}
	if len(bundles) != len(contract.Pairs)*2 {
		return nil, FormalEvaluationSummary{}, errors.New("FORMAL_METHOD_EVIDENCE_SET_MISMATCH")
	}
	benchmark := BundleBenchmark{Budget: contract.Budget}
	results := make([]BundleTrialResult, 0, len(bundles))
	for _, pair := range contract.Pairs {
		for _, input := range []struct {
			variant FormalVariant
			kind    string
			root    string
		}{
			{variant: pair.Control, kind: BundleKindControl},
			{variant: pair.Candidate, kind: FormalBundleKindCandidate, root: pair.RootCauseID},
		} {
			bundle, ok := bundles[input.variant.TrialID]
			if !ok {
				return nil, FormalEvaluationSummary{}, fmt.Errorf(
					"FORMAL_METHOD_TRIAL_MISSING: %s", input.variant.TrialID,
				)
			}
			variant := formalBundleVariant(input.variant, input.kind, input.root)
			if bundle.SchemaVersion != requiredBundleSchema ||
				bundle.Qualification.Profile.Digest != contract.ProfileDigest {
				results = append(results, BundleTrialResult{
					TrialID: variant.TrialID, VariantID: variant.VariantID, Kind: variant.Kind,
					RootCauseID: variant.RootCauseID, Status: BundleStatusInvalid,
					InvalidReason: "FORMAL_METHOD_BUNDLE_CONTRACT_MISMATCH",
					BundleDigest:  bundle.Digest, BuildID: bundle.Qualification.Manifest.BuildID,
				})
				continue
			}
			results = append(results, evaluateBundleVariantWithMonitors(
				benchmark, variant, bundle, projector, monitors,
			))
		}
	}
	return results, summarizeFormalResults(results), nil
}

// FormalFreshEvaluation is private evaluator output. Pair/root/variant fields
// never cross into FormalOpaqueView or Agent-facing artifacts.
type FormalFreshEvaluation struct {
	SchemaVersion       string                  `json:"schema_version"`
	BenchmarkID         string                  `json:"benchmark_id"`
	ContractDigest      string                  `json:"contract_digest"`
	ExposureAuditDigest string                  `json:"exposure_audit_digest"`
	MethodSpecDigest    string                  `json:"method_spec_digest"`
	Pairs               []FormalPairEvaluation  `json:"pairs"`
	Results             []BundleTrialResult     `json:"results"`
	Summary             FormalEvaluationSummary `json:"summary"`
	Digest              string                  `json:"digest"`
}

// EvaluateFormalFreshBundles reuses the public fresh evaluator's trusted
// build/method/bundle/projection/budget checks for every opaque trial. The
// caller owns fresh execution and supplies one evidence record per trial.
func EvaluateFormalFreshBundles(
	contract FormalBenchmarkContract,
	exposure FormalExposureAudit,
	spec controlexperiment.MethodSpec,
	evidence map[string]FreshBundleEvidence,
	projector semantic.DecisionProjector,
	registeredMonitors ...oracle.BundleMonitor,
) (FormalFreshEvaluation, error) {
	monitors, err := admitFormalFreshEvaluation(contract, exposure, spec, projector, registeredMonitors...)
	if err != nil {
		return FormalFreshEvaluation{}, err
	}
	if len(evidence) != len(contract.Pairs)*2 {
		return FormalFreshEvaluation{}, errors.New("FORMAL_EVALUATION_EVIDENCE_SET_MISMATCH")
	}

	report := FormalFreshEvaluation{
		SchemaVersion: FormalFreshEvaluationSchemaVersion, BenchmarkID: contract.ID,
		ContractDigest: contract.Digest, ExposureAuditDigest: exposure.Digest,
		MethodSpecDigest: spec.Digest,
	}
	budget := BundleBenchmark{Budget: contract.Budget}
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
				return FormalFreshEvaluation{}, fmt.Errorf(
					"FORMAL_EVALUATION_TRIAL_MISSING: %s", input.variant.TrialID,
				)
			}
			variant := formalBundleVariant(input.variant, input.kind, input.root)
			if current.Bundle.Qualification.Profile.Digest != contract.ProfileDigest {
				report.Results = append(report.Results, formalProfileMismatchResult(variant, current))
				continue
			}
			report.Results = append(report.Results, evaluateFreshBundleVariantWithMonitors(
				budget, variant, spec, current, projector, monitors,
			))
		}
	}
	return report.Seal()
}

// ValidateFormalFreshEvaluationAdmission runs every contract/exposure/method/
// composition check that must pass before a curator executes any SUT binary.
func ValidateFormalFreshEvaluationAdmission(
	contract FormalBenchmarkContract,
	exposure FormalExposureAudit,
	spec controlexperiment.MethodSpec,
	projector semantic.DecisionProjector,
	registeredMonitors ...oracle.BundleMonitor,
) error {
	_, err := admitFormalFreshEvaluation(contract, exposure, spec, projector, registeredMonitors...)
	return err
}

func admitFormalFreshEvaluation(
	contract FormalBenchmarkContract,
	exposure FormalExposureAudit,
	spec controlexperiment.MethodSpec,
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
		return nil, errors.New("FORMAL_EVALUATION_EXPOSURE_AUDIT_REQUIRED")
	}
	monitors, err := ResolveFormalComposition(contract, projector, registeredMonitors...)
	if err != nil {
		return nil, err
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.Digest != contract.MethodSpecDigest || spec.ProjectorID != contract.Composition.ProjectorID ||
		spec.RequiredBundleSchema != contract.RequiredBundleSchema {
		return nil, errors.New("FORMAL_EVALUATION_METHOD_SPEC_MISMATCH")
	}
	return monitors, nil
}

func formalBundleVariant(variant FormalVariant, kind, root string) BundleVariant {
	return BundleVariant{
		TrialID: variant.TrialID, VariantID: variant.VariantID, Kind: kind, RootCauseID: root,
		ExpectedBuildID: variant.ExpectedBuildID, ExpectedConfigDigest: variant.ExpectedConfigDigest,
		ExpectedBuildAuditDigest: variant.ExpectedBuildAuditDigest,
		ExpectedBinaryDigest:     variant.ExpectedBinaryDigest,
	}
}

func formalProfileMismatchResult(variant BundleVariant, evidence FreshBundleEvidence) BundleTrialResult {
	return BundleTrialResult{
		TrialID: variant.TrialID, VariantID: variant.VariantID, Kind: variant.Kind,
		RootCauseID: variant.RootCauseID, Status: BundleStatusInvalid,
		InvalidReason:    "FORMAL_EVALUATION_PROFILE_MISMATCH",
		BundleDigest:     evidence.Bundle.Digest,
		BuildID:          evidence.Bundle.Qualification.Manifest.BuildID,
		BuildAuditDigest: evidence.BuildAuditDigest, BinaryDigest: evidence.BinaryDigest,
	}
}

func (report FormalFreshEvaluation) Seal() (FormalFreshEvaluation, error) {
	report.SchemaVersion = FormalFreshEvaluationSchemaVersion
	report.Pairs = append([]FormalPairEvaluation(nil), report.Pairs...)
	report.Results = append([]BundleTrialResult(nil), report.Results...)
	sort.Slice(report.Pairs, func(i, j int) bool { return report.Pairs[i].PairID < report.Pairs[j].PairID })
	sort.Slice(report.Results, func(i, j int) bool { return report.Results[i].TrialID < report.Results[j].TrialID })
	report.Summary = summarizeFormalResults(report.Results)
	report.Digest = ""
	if err := report.validateContent(); err != nil {
		return FormalFreshEvaluation{}, err
	}
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		return FormalFreshEvaluation{}, err
	}
	report.Digest = digest
	return report, nil
}

func (report FormalFreshEvaluation) Validate() error {
	if report.SchemaVersion != FormalFreshEvaluationSchemaVersion {
		return errors.New("FORMAL_EVALUATION_SCHEMA_MISMATCH")
	}
	sealed, err := report.Seal()
	if err != nil {
		return err
	}
	stored := report
	stored.Digest = ""
	digest, err := control.CanonicalDigest(stored)
	if err != nil || sealed.Digest != report.Digest || digest != report.Digest {
		return errors.New("FORMAL_EVALUATION_DIGEST_MISMATCH")
	}
	return nil
}

func (report FormalFreshEvaluation) validateContent() error {
	if !validBundleID(report.BenchmarkID) || !bundleDigestValid(report.ContractDigest) ||
		!bundleDigestValid(report.ExposureAuditDigest) || !bundleDigestValid(report.MethodSpecDigest) || len(report.Pairs) < 3 ||
		len(report.Results) != len(report.Pairs)*2 || report.Summary != summarizeFormalResults(report.Results) {
		return errors.New("FORMAL_EVALUATION_IDENTITY_INVALID")
	}
	results := make(map[string]BundleTrialResult, len(report.Results))
	for _, result := range report.Results {
		if !validBundleID(result.TrialID) || !validBundleID(result.VariantID) || results[result.TrialID].TrialID != "" {
			return errors.New("FORMAL_EVALUATION_RESULT_IDENTITY_INVALID")
		}
		switch result.Kind {
		case BundleKindControl:
			if result.RootCauseID != "" ||
				(result.Status != BundleStatusControlPass && result.Status != BundleStatusFalsePositive && result.Status != BundleStatusInvalid) {
				return errors.New("FORMAL_EVALUATION_CONTROL_RESULT_INVALID")
			}
		case FormalBundleKindCandidate:
			if !validBundleID(result.RootCauseID) ||
				(result.Status != BundleStatusSurvived && result.Status != BundleStatusKilled && result.Status != BundleStatusInvalid) {
				return errors.New("FORMAL_EVALUATION_CANDIDATE_RESULT_INVALID")
			}
		default:
			return errors.New("FORMAL_EVALUATION_RESULT_KIND_INVALID")
		}
		if (result.Status == BundleStatusInvalid) != (result.InvalidReason != "") ||
			((result.Status == BundleStatusKilled || result.Status == BundleStatusFalsePositive) != (result.Finding != nil)) {
			return errors.New("FORMAL_EVALUATION_RESULT_STATUS_INVALID")
		}
		if !bundleDigestValid(result.BundleDigest) || result.BuildID == "" {
			return errors.New("FORMAL_EVALUATION_BUNDLE_IDENTITY_INVALID")
		}
		if result.Finding != nil &&
			(result.Finding.Monitor == "" || result.Finding.Message == "" || !bundleDigestValid(result.Finding.TraceDigest)) {
			return errors.New("FORMAL_EVALUATION_FINDING_INVALID")
		}
		if result.Status != BundleStatusInvalid &&
			(!bundleDigestValid(result.BuildAuditDigest) || !bundleDigestValid(result.BinaryDigest) ||
				!bundleDigestValid(result.MethodConfigProjectionDigest) || !bundleDigestValid(result.OperationHistoryDigest)) {
			return errors.New("FORMAL_EVALUATION_FRESH_EVIDENCE_INVALID")
		}
		results[result.TrialID] = result
	}
	used, pairs, roots := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, pair := range report.Pairs {
		controlResult, controlOK := results[pair.ControlTrialID]
		candidateResult, candidateOK := results[pair.CandidateTrialID]
		if !validBundleID(pair.PairID) || pairs[pair.PairID] || !validBundleID(pair.RootCauseID) ||
			!controlOK || !candidateOK || used[pair.ControlTrialID] || used[pair.CandidateTrialID] ||
			controlResult.Kind != BundleKindControl || candidateResult.Kind != FormalBundleKindCandidate ||
			candidateResult.RootCauseID != pair.RootCauseID {
			return errors.New("FORMAL_EVALUATION_PAIR_INVALID")
		}
		pairs[pair.PairID], roots[pair.RootCauseID] = true, true
		used[pair.ControlTrialID], used[pair.CandidateTrialID] = true, true
	}
	if len(used) != len(results) || len(roots) < 3 {
		return errors.New("FORMAL_EVALUATION_PAIR_SET_INVALID")
	}
	return nil
}

func summarizeFormalResults(results []BundleTrialResult) FormalEvaluationSummary {
	summary := FormalEvaluationSummary{}
	roots, killed := map[string]bool{}, map[string]bool{}
	for _, result := range results {
		if result.Status == BundleStatusInvalid {
			summary.InvalidTrials++
		}
		if result.Kind == BundleKindControl {
			summary.Controls++
			if result.Status == BundleStatusFalsePositive {
				summary.FalsePositives++
			}
			continue
		}
		summary.Candidates++
		roots[result.RootCauseID] = true
		if result.Status == BundleStatusKilled {
			summary.KilledCandidates++
			killed[result.RootCauseID] = true
		}
	}
	summary.RootCauses, summary.KilledRootCauses = len(roots), len(killed)
	return summary
}

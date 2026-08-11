package defectbench

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

const (
	BundleBenchmarkSchemaVersion    = "consensus-atlas/bundle-defect-benchmark/v1"
	BundleBenchmarkSchemaVersionV2  = "consensus-atlas/bundle-defect-benchmark/v2"
	BundleEvaluationSchemaVersion   = "consensus-atlas/bundle-defect-evaluation/v1"
	BundleEvaluationSchemaVersionV2 = "consensus-atlas/bundle-defect-evaluation/v2"
	BundleKindControl               = "control"
	BundleKindCalibration           = "calibration-candidate"
	BundleStatusKilled              = "killed"
	BundleStatusSurvived            = "survived"
	BundleStatusControlPass         = "control-pass"
	BundleStatusFalsePositive       = "false-positive"
	BundleStatusInvalid             = "invalid"
)

type BundleBudget struct {
	MaxDecisions        int `json:"max_decisions"`
	MaxPrimaryWorkUnits int `json:"max_primary_work_units"`
}

type BundleVariant struct {
	TrialID                  string `json:"trial_id"`
	VariantID                string `json:"variant_id"`
	Kind                     string `json:"kind"`
	RootCauseID              string `json:"root_cause_id,omitempty"`
	ExpectedBuildID          string `json:"expected_build_id"`
	ExpectedConfigDigest     string `json:"expected_config_digest,omitempty"`
	ExpectedBuildAuditDigest string `json:"expected_build_audit_digest,omitempty"`
	ExpectedBinaryDigest     string `json:"expected_binary_digest,omitempty"`
}

type BundleBenchmark struct {
	SchemaVersion        string          `json:"schema_version"`
	ID                   string          `json:"id"`
	Classification       string          `json:"classification"`
	ProjectorID          string          `json:"projector_id"`
	MethodSpecDigest     string          `json:"method_spec_digest,omitempty"`
	RequiredBundleSchema string          `json:"required_bundle_schema,omitempty"`
	Budget               BundleBudget    `json:"budget"`
	Variants             []BundleVariant `json:"variants"`
	Digest               string          `json:"digest"`
}

func (manifest BundleBenchmark) Seal() (BundleBenchmark, error) {
	if manifest.SchemaVersion == "" {
		manifest.SchemaVersion = BundleBenchmarkSchemaVersion
	}
	if manifest.SchemaVersion != BundleBenchmarkSchemaVersion &&
		manifest.SchemaVersion != BundleBenchmarkSchemaVersionV2 {
		return BundleBenchmark{}, errors.New("BUNDLE_BENCHMARK_SCHEMA_MISMATCH")
	}
	manifest.Variants = append([]BundleVariant(nil), manifest.Variants...)
	sort.Slice(manifest.Variants, func(i, j int) bool { return manifest.Variants[i].TrialID < manifest.Variants[j].TrialID })
	manifest.Digest = ""
	if err := manifest.validateContent(); err != nil {
		return BundleBenchmark{}, err
	}
	digest, err := control.CanonicalDigest(manifest)
	if err != nil {
		return BundleBenchmark{}, err
	}
	manifest.Digest = digest
	return manifest, nil
}

func (manifest BundleBenchmark) Validate() error {
	if manifest.SchemaVersion != BundleBenchmarkSchemaVersion &&
		manifest.SchemaVersion != BundleBenchmarkSchemaVersionV2 {
		return errors.New("BUNDLE_BENCHMARK_SCHEMA_MISMATCH")
	}
	sealed, err := manifest.Seal()
	if err != nil {
		return err
	}
	if sealed.Digest != manifest.Digest {
		return errors.New("BUNDLE_BENCHMARK_DIGEST_MISMATCH")
	}
	return nil
}

func (manifest BundleBenchmark) validateContent() error {
	if !validBundleID(manifest.ID) || manifest.Classification != "public-calibration-only" ||
		manifest.ProjectorID == "" || manifest.Budget.MaxDecisions <= 0 ||
		manifest.Budget.MaxPrimaryWorkUnits <= 0 || len(manifest.Variants) < 2 {
		return errors.New("BUNDLE_BENCHMARK_IDENTITY_INVALID")
	}
	if manifest.SchemaVersion == BundleBenchmarkSchemaVersionV2 {
		if !bundleDigestValid(manifest.MethodSpecDigest) ||
			manifest.RequiredBundleSchema != controlexperiment.ExecutionBundleSchemaVersionV3 {
			return errors.New("BUNDLE_BENCHMARK_METHOD_IDENTITY_INVALID")
		}
	} else if manifest.MethodSpecDigest != "" || manifest.RequiredBundleSchema != "" {
		return errors.New("BUNDLE_BENCHMARK_LEGACY_METHOD_IDENTITY_FORBIDDEN")
	}
	trials := make(map[string]bool)
	variants := make(map[string]bool)
	controls, candidates := 0, 0
	for index, variant := range manifest.Variants {
		if !validBundleID(variant.TrialID) || !validBundleID(variant.VariantID) ||
			variant.TrialID == variant.VariantID || variant.ExpectedBuildID == "" ||
			trials[variant.TrialID] || variants[variant.VariantID] {
			return fmt.Errorf("BUNDLE_BENCHMARK_VARIANT_INVALID: %d", index)
		}
		if variant.ExpectedConfigDigest != "" && !bundleDigestValid(variant.ExpectedConfigDigest) {
			return fmt.Errorf("BUNDLE_BENCHMARK_CONFIG_DIGEST_INVALID: %s", variant.TrialID)
		}
		if manifest.SchemaVersion == BundleBenchmarkSchemaVersionV2 {
			if !bundleDigestValid(variant.ExpectedBuildAuditDigest) ||
				!bundleDigestValid(variant.ExpectedBinaryDigest) {
				return fmt.Errorf("BUNDLE_BENCHMARK_BUILD_EVIDENCE_INVALID: %s", variant.TrialID)
			}
		} else if variant.ExpectedBuildAuditDigest != "" || variant.ExpectedBinaryDigest != "" {
			return fmt.Errorf("BUNDLE_BENCHMARK_LEGACY_BUILD_EVIDENCE_FORBIDDEN: %s", variant.TrialID)
		}
		trials[variant.TrialID], variants[variant.VariantID] = true, true
		switch variant.Kind {
		case BundleKindControl:
			controls++
			if variant.RootCauseID != "" {
				return errors.New("BUNDLE_BENCHMARK_CONTROL_ROOT_CAUSE_FORBIDDEN")
			}
		case BundleKindCalibration:
			candidates++
			if !validBundleID(variant.RootCauseID) {
				return errors.New("BUNDLE_BENCHMARK_ROOT_CAUSE_REQUIRED")
			}
		default:
			return fmt.Errorf("BUNDLE_BENCHMARK_KIND_UNSUPPORTED: %s", variant.Kind)
		}
	}
	if controls == 0 || candidates == 0 {
		return errors.New("BUNDLE_BENCHMARK_PAIR_REQUIRED")
	}
	return nil
}

type BundleFinding struct {
	Monitor     string `json:"monitor"`
	Step        int    `json:"step,omitempty"`
	Message     string `json:"message"`
	TraceDigest string `json:"trace_digest"`
}

type BundleTrialResult struct {
	TrialID                      string         `json:"trial_id"`
	VariantID                    string         `json:"variant_id"`
	Kind                         string         `json:"kind"`
	RootCauseID                  string         `json:"root_cause_id,omitempty"`
	Status                       string         `json:"status"`
	InvalidReason                string         `json:"invalid_reason,omitempty"`
	BundleDigest                 string         `json:"bundle_digest"`
	BuildID                      string         `json:"build_id"`
	Decisions                    int            `json:"decisions"`
	PrimaryWork                  int            `json:"primary_work_units"`
	ReplayWork                   int            `json:"replay_work_units"`
	BuildAuditDigest             string         `json:"build_audit_digest,omitempty"`
	BinaryDigest                 string         `json:"binary_digest,omitempty"`
	MethodConfigProjectionDigest string         `json:"method_config_projection_digest,omitempty"`
	OperationHistoryDigest       string         `json:"operation_history_digest,omitempty"`
	Oracle                       oracle.Result  `json:"oracle"`
	Finding                      *BundleFinding `json:"finding,omitempty"`
}

type BundleEvaluationSummary struct {
	Controls         int `json:"controls"`
	FalsePositives   int `json:"false_positives"`
	Candidates       int `json:"calibration_candidates"`
	KilledCandidates int `json:"killed_calibration_candidates"`
	RootCauses       int `json:"root_causes"`
	KilledRootCauses int `json:"killed_root_causes"`
	InvalidTrials    int `json:"invalid_trials"`
}

type BundleEvaluation struct {
	SchemaVersion    string                  `json:"schema_version"`
	BenchmarkID      string                  `json:"benchmark_id"`
	BenchmarkDigest  string                  `json:"benchmark_digest"`
	Classification   string                  `json:"classification"`
	MethodSpecDigest string                  `json:"method_spec_digest,omitempty"`
	Results          []BundleTrialResult     `json:"results"`
	Summary          BundleEvaluationSummary `json:"summary"`
	Digest           string                  `json:"digest"`
}

func EvaluateBundles(
	manifest BundleBenchmark,
	bundles map[string]controlexperiment.ExecutionBundle,
	projector semantic.DecisionProjector,
) (BundleEvaluation, error) {
	if err := manifest.Validate(); err != nil {
		return BundleEvaluation{}, err
	}
	if manifest.SchemaVersion != BundleBenchmarkSchemaVersion {
		return BundleEvaluation{}, errors.New("BUNDLE_BENCHMARK_FRESH_EXECUTION_REQUIRED")
	}
	if projector == nil || projector.ID() != manifest.ProjectorID {
		return BundleEvaluation{}, errors.New("BUNDLE_BENCHMARK_PROJECTOR_MISMATCH")
	}
	if len(bundles) != len(manifest.Variants) {
		return BundleEvaluation{}, errors.New("BUNDLE_BENCHMARK_SUBMISSION_SET_MISMATCH")
	}
	report := BundleEvaluation{
		SchemaVersion: BundleEvaluationSchemaVersion, BenchmarkID: manifest.ID,
		BenchmarkDigest: manifest.Digest, Classification: manifest.Classification,
	}
	for _, variant := range manifest.Variants {
		bundle, ok := bundles[variant.TrialID]
		if !ok {
			return BundleEvaluation{}, fmt.Errorf("BUNDLE_BENCHMARK_TRIAL_MISSING: %s", variant.TrialID)
		}
		result := evaluateBundleVariant(manifest, variant, bundle, projector)
		report.Results = append(report.Results, result)
	}
	report.Summary = summarizeBundleResults(report.Results)
	report.Digest = ""
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		return BundleEvaluation{}, err
	}
	report.Digest = digest
	if err := report.Validate(); err != nil {
		return BundleEvaluation{}, err
	}
	return report, nil
}

// FreshBundleEvidence is produced only after the trusted evaluator has
// validated and executed the exact audited binary. Report and Bundle are the
// newly generated outputs of that evaluator-owned process.
type FreshBundleEvidence struct {
	Report           controlexperiment.Report
	Bundle           controlexperiment.ExecutionBundle
	BuildAudit       sutbuild.Audit
	BuildAuditDigest string
	BinaryDigest     string
}

func EvaluateFreshBundles(
	manifest BundleBenchmark,
	spec controlexperiment.MethodSpec,
	evidence map[string]FreshBundleEvidence,
	projector semantic.DecisionProjector,
) (BundleEvaluation, error) {
	if err := manifest.Validate(); err != nil {
		return BundleEvaluation{}, err
	}
	if manifest.SchemaVersion != BundleBenchmarkSchemaVersionV2 {
		return BundleEvaluation{}, errors.New("BUNDLE_BENCHMARK_FRESH_SCHEMA_REQUIRED")
	}
	if err := spec.Validate(); err != nil {
		return BundleEvaluation{}, err
	}
	if spec.Digest != manifest.MethodSpecDigest ||
		spec.RequiredBundleSchema != manifest.RequiredBundleSchema {
		return BundleEvaluation{}, errors.New("BUNDLE_BENCHMARK_METHOD_SPEC_MISMATCH")
	}
	if projector == nil || projector.ID() != manifest.ProjectorID || projector.ID() != spec.ProjectorID {
		return BundleEvaluation{}, errors.New("BUNDLE_BENCHMARK_PROJECTOR_MISMATCH")
	}
	if len(evidence) != len(manifest.Variants) {
		return BundleEvaluation{}, errors.New("BUNDLE_BENCHMARK_SUBMISSION_SET_MISMATCH")
	}
	report := BundleEvaluation{
		SchemaVersion: BundleEvaluationSchemaVersionV2, BenchmarkID: manifest.ID,
		BenchmarkDigest: manifest.Digest, Classification: manifest.Classification,
		MethodSpecDigest: spec.Digest,
	}
	for _, variant := range manifest.Variants {
		current, ok := evidence[variant.TrialID]
		if !ok {
			return BundleEvaluation{}, fmt.Errorf("BUNDLE_BENCHMARK_TRIAL_MISSING: %s", variant.TrialID)
		}
		report.Results = append(report.Results, evaluateFreshBundleVariant(
			manifest, variant, spec, current, projector,
		))
	}
	report.Summary = summarizeBundleResults(report.Results)
	report.Digest = ""
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		return BundleEvaluation{}, err
	}
	report.Digest = digest
	if err := report.Validate(); err != nil {
		return BundleEvaluation{}, err
	}
	return report, nil
}

func evaluateFreshBundleVariant(
	manifest BundleBenchmark,
	variant BundleVariant,
	spec controlexperiment.MethodSpec,
	evidence FreshBundleEvidence,
	projector semantic.DecisionProjector,
) BundleTrialResult {
	return evaluateFreshBundleVariantWithMonitors(
		manifest, variant, spec, evidence, projector, []oracle.BundleMonitor{oracle.BundleAgreement{}},
	)
}

func evaluateFreshBundleVariantWithMonitors(
	manifest BundleBenchmark,
	variant BundleVariant,
	spec controlexperiment.MethodSpec,
	evidence FreshBundleEvidence,
	projector semantic.DecisionProjector,
	monitors []oracle.BundleMonitor,
) BundleTrialResult {
	invalid := func(reason string) BundleTrialResult {
		return BundleTrialResult{
			TrialID: variant.TrialID, VariantID: variant.VariantID, Kind: variant.Kind,
			RootCauseID: variant.RootCauseID, Status: BundleStatusInvalid, InvalidReason: reason,
			BundleDigest:     evidence.Bundle.Digest,
			BuildID:          evidence.Bundle.Qualification.Manifest.BuildID,
			BuildAuditDigest: evidence.BuildAuditDigest, BinaryDigest: evidence.BinaryDigest,
		}
	}
	if err := evidence.BuildAudit.Validate(); err != nil {
		return invalid("BUNDLE_BENCHMARK_BUILD_AUDIT_INVALID: " + err.Error())
	}
	if evidence.BuildAudit.TrialID != variant.TrialID ||
		evidence.BuildAuditDigest != variant.ExpectedBuildAuditDigest ||
		evidence.BinaryDigest != variant.ExpectedBinaryDigest ||
		evidence.BuildAudit.BinaryDigest != evidence.BinaryDigest ||
		evidence.BuildAudit.SUTBuildIdentity != variant.ExpectedBuildID {
		return invalid("BUNDLE_BENCHMARK_BUILD_EVIDENCE_MISMATCH")
	}
	if err := spec.ValidateExecution(evidence.Report, evidence.Bundle); err != nil {
		return invalid(err.Error())
	}
	projection, err := controlexperiment.MethodConfigProjectionDigest(evidence.Report.Config)
	if err != nil || projection != spec.ConfigProjectionDigest {
		return invalid("BUNDLE_BENCHMARK_METHOD_CONFIG_MISMATCH")
	}
	result := evaluateBundleVariantWithMonitors(manifest, variant, evidence.Bundle, projector, monitors)
	result.BuildAuditDigest = evidence.BuildAuditDigest
	result.BinaryDigest = evidence.BinaryDigest
	result.MethodConfigProjectionDigest = projection
	result.OperationHistoryDigest = evidence.Bundle.OperationHistory.Digest
	return result
}

func evaluateBundleVariant(
	manifest BundleBenchmark,
	variant BundleVariant,
	bundle controlexperiment.ExecutionBundle,
	projector semantic.DecisionProjector,
) BundleTrialResult {
	return evaluateBundleVariantWithMonitors(
		manifest, variant, bundle, projector, []oracle.BundleMonitor{oracle.BundleAgreement{}},
	)
}

func evaluateBundleVariantWithMonitors(
	manifest BundleBenchmark,
	variant BundleVariant,
	bundle controlexperiment.ExecutionBundle,
	projector semantic.DecisionProjector,
	monitors []oracle.BundleMonitor,
) BundleTrialResult {
	result := BundleTrialResult{
		TrialID: variant.TrialID, VariantID: variant.VariantID, Kind: variant.Kind,
		RootCauseID: variant.RootCauseID, BundleDigest: bundle.Digest,
		BuildID: bundle.Qualification.Manifest.BuildID, Decisions: len(bundle.Trace.Records),
		PrimaryWork: bundle.Work.Primary.WorkUnits, ReplayWork: bundle.Work.Replay.WorkUnits,
	}
	invalid := func(reason string) BundleTrialResult {
		result.Status, result.InvalidReason = BundleStatusInvalid, reason
		return result
	}
	if err := bundle.Validate(); err != nil {
		return invalid(err.Error())
	}
	if err := bundle.ValidateProjection(projector); err != nil {
		return invalid(err.Error())
	}
	if result.BuildID != variant.ExpectedBuildID {
		return invalid("BUNDLE_BENCHMARK_BUILD_IDENTITY_MISMATCH")
	}
	if variant.ExpectedConfigDigest != "" && bundle.Identity.ConfigDigest != variant.ExpectedConfigDigest {
		return invalid("BUNDLE_BENCHMARK_CONFIG_IDENTITY_MISMATCH")
	}
	if result.Decisions > manifest.Budget.MaxDecisions || result.PrimaryWork > manifest.Budget.MaxPrimaryWorkUnits {
		return invalid("BUNDLE_BENCHMARK_BUDGET_EXCEEDED")
	}
	integrity := oracle.CheckBundle(bundle, oracle.BundleTraceIntegrity{})
	if len(integrity.Violations) != 0 {
		result.Oracle = integrity
		return invalid("BUNDLE_BENCHMARK_TRACE_INTEGRITY_FAILED")
	}
	checks := make([]oracle.BundleMonitor, 0, len(monitors)+1)
	checks = append(checks, oracle.BundleTraceIntegrity{})
	checks = append(checks, monitors...)
	result.Oracle = oracle.CheckBundle(bundle, checks...)
	var finding *oracle.Violation
	if len(result.Oracle.Violations) != 0 {
		finding = &result.Oracle.Violations[0]
	}
	if finding != nil {
		result.Finding = &BundleFinding{
			Monitor: finding.Monitor, Step: finding.Step, Message: finding.Message,
			TraceDigest: bundle.Trace.Digest,
		}
	}
	if variant.Kind == BundleKindControl {
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
	return result
}

func summarizeBundleResults(results []BundleTrialResult) BundleEvaluationSummary {
	summary := BundleEvaluationSummary{}
	causes, killed := make(map[string]bool), make(map[string]bool)
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
		causes[result.RootCauseID] = true
		if result.Status == BundleStatusKilled {
			summary.KilledCandidates++
			killed[result.RootCauseID] = true
		}
	}
	summary.RootCauses, summary.KilledRootCauses = len(causes), len(killed)
	return summary
}

func (report BundleEvaluation) Validate() error {
	if (report.SchemaVersion != BundleEvaluationSchemaVersion &&
		report.SchemaVersion != BundleEvaluationSchemaVersionV2) || !validBundleID(report.BenchmarkID) ||
		!bundleDigestValid(report.BenchmarkDigest) || report.Classification != "public-calibration-only" ||
		len(report.Results) < 2 || summarizeBundleResults(report.Results) != report.Summary {
		return errors.New("BUNDLE_EVALUATION_INVALID")
	}
	if report.SchemaVersion == BundleEvaluationSchemaVersionV2 {
		if !bundleDigestValid(report.MethodSpecDigest) {
			return errors.New("BUNDLE_EVALUATION_METHOD_IDENTITY_INVALID")
		}
		for _, result := range report.Results {
			if result.Status != BundleStatusInvalid &&
				(!bundleDigestValid(result.BuildAuditDigest) ||
					!bundleDigestValid(result.BinaryDigest) ||
					!bundleDigestValid(result.MethodConfigProjectionDigest) ||
					!bundleDigestValid(result.OperationHistoryDigest)) {
				return errors.New("BUNDLE_EVALUATION_FRESH_EVIDENCE_INVALID")
			}
		}
	} else if report.MethodSpecDigest != "" {
		return errors.New("BUNDLE_EVALUATION_LEGACY_METHOD_IDENTITY_FORBIDDEN")
	}
	copyReport := report
	copyReport.Digest = ""
	digest, err := control.CanonicalDigest(copyReport)
	if err != nil || !bundleDigestValid(report.Digest) || digest != report.Digest {
		return errors.New("BUNDLE_EVALUATION_DIGEST_MISMATCH")
	}
	return nil
}

func validBundleID(value string) bool {
	if value == "" || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func bundleDigestValid(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

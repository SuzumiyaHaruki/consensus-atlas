package semantic

import (
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const RiskWitnessProgressSchemaVersion = "consensus-atlas/risk-witness-progress/v1"

// RiskWitnessProgress is the minimal untrusted-planner projection of a
// trusted RiskWitnessResult. It deliberately omits milestone evidence,
// participants, projector identity, and target identity.
type RiskWitnessProgress struct {
	SchemaVersion         string   `json:"schema_version"`
	SpecDigest            string   `json:"spec_digest"`
	RiskID                string   `json:"risk_id"`
	EvidencePrefixDigest  string   `json:"evidence_prefix_digest"`
	SatisfiedMilestones   []string `json:"satisfied_milestones"`
	FirstMissingMilestone string   `json:"first_missing_milestone,omitempty"`
	Status                string   `json:"status"`
	SourceResultDigest    string   `json:"source_result_digest"`
	Digest                string   `json:"digest"`
}

func NewRiskWitnessProgress(
	spec RiskWitnessSpec,
	result RiskWitnessResult,
) (RiskWitnessProgress, error) {
	if err := result.Validate(spec); err != nil {
		return RiskWitnessProgress{}, err
	}
	progress := RiskWitnessProgress{
		SchemaVersion: RiskWitnessProgressSchemaVersion,
		SpecDigest:    spec.Digest, RiskID: result.RiskID,
		EvidencePrefixDigest: result.ExecutionDigest,
		SatisfiedMilestones:  append([]string(nil), result.SatisfiedMilestones...),
		Status:               result.Status,
		SourceResultDigest:   result.Digest,
	}
	if len(result.MissingMilestones) > 0 {
		progress.FirstMissingMilestone = result.MissingMilestones[0]
	}
	sealed, err := progress.seal()
	if err != nil {
		return RiskWitnessProgress{}, err
	}
	if err := sealed.Validate(spec); err != nil {
		return RiskWitnessProgress{}, err
	}
	return sealed, nil
}

func (progress RiskWitnessProgress) Validate(spec RiskWitnessSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if progress.SchemaVersion != RiskWitnessProgressSchemaVersion ||
		progress.SpecDigest != spec.Digest || progress.RiskID != spec.RiskID ||
		!validRiskWitnessSHA256(progress.EvidencePrefixDigest) ||
		!validRiskWitnessSHA256(progress.SourceResultDigest) {
		return errors.New("RISK_WITNESS_PROGRESS_IDENTITY_INVALID")
	}
	known := make(map[string]bool, len(spec.Milestones))
	for _, milestone := range spec.Milestones {
		known[milestone.ID] = true
	}
	seen := make(map[string]bool, len(progress.SatisfiedMilestones))
	for _, milestone := range progress.SatisfiedMilestones {
		if !known[milestone] || seen[milestone] {
			return errors.New("RISK_WITNESS_PROGRESS_MILESTONE_INVALID")
		}
		seen[milestone] = true
	}
	if progress.FirstMissingMilestone != "" &&
		(!known[progress.FirstMissingMilestone] || seen[progress.FirstMissingMilestone]) {
		return errors.New("RISK_WITNESS_PROGRESS_MISSING_INVALID")
	}
	switch progress.Status {
	case RiskWitnessReached:
		if len(progress.SatisfiedMilestones) != len(spec.Milestones) ||
			progress.FirstMissingMilestone != "" {
			return errors.New("RISK_WITNESS_PROGRESS_STATUS_INVALID")
		}
	case RiskWitnessNotReached:
		if len(progress.SatisfiedMilestones) >= len(spec.Milestones) ||
			progress.FirstMissingMilestone == "" {
			return errors.New("RISK_WITNESS_PROGRESS_STATUS_INVALID")
		}
	default:
		return errors.New("RISK_WITNESS_PROGRESS_STATUS_INVALID")
	}
	sealed, err := progress.seal()
	if err != nil || !validRiskWitnessSHA256(progress.Digest) || sealed.Digest != progress.Digest {
		return errors.New("RISK_WITNESS_PROGRESS_DIGEST_MISMATCH")
	}
	return nil
}

func (progress RiskWitnessProgress) ValidateSource(
	spec RiskWitnessSpec,
	result RiskWitnessResult,
) error {
	want, err := NewRiskWitnessProgress(spec, result)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(progress, want) {
		return errors.New("RISK_WITNESS_PROGRESS_SOURCE_MISMATCH")
	}
	return nil
}

func (progress RiskWitnessProgress) seal() (RiskWitnessProgress, error) {
	progress.SatisfiedMilestones = append([]string(nil), progress.SatisfiedMilestones...)
	progress.Digest = ""
	digest, err := control.CanonicalDigest(progress)
	if err != nil {
		return RiskWitnessProgress{}, err
	}
	progress.Digest = digest
	return progress, nil
}

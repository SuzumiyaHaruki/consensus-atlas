package semantic

import (
	"errors"
	"reflect"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	RiskWitnessProgressSchemaVersion = "consensus-atlas/risk-witness-progress/v2"
	RiskWitnessInstantiated          = "witness-instantiated"
	RiskWitnessNotInstantiated       = "witness-not-instantiated"
)

// RiskWitnessProgress is the minimal untrusted-planner projection of a
// trusted RiskWitnessResult. It deliberately omits milestone evidence,
// participants, projector identity, and target identity.
type RiskWitnessProgress struct {
	SchemaVersion         string                        `json:"schema_version"`
	SpecDigest            string                        `json:"spec_digest"`
	RiskID                string                        `json:"risk_id"`
	EvidencePrefixDigest  string                        `json:"evidence_prefix_digest"`
	SatisfiedMilestones   []string                      `json:"satisfied_milestones"`
	MilestoneEvidence     []RiskWitnessProgressEvidence `json:"milestone_evidence,omitempty"`
	ResolvedBindings      []RiskWitnessResolvedBinding  `json:"resolved_bindings,omitempty"`
	FirstMissingMilestone string                        `json:"first_missing_milestone,omitempty"`
	Status                string                        `json:"status"`
	SourceResultDigest    string                        `json:"source_result_digest"`
	Digest                string                        `json:"digest"`
}

type RiskWitnessProgressEvidence struct {
	MilestoneID        string                       `json:"milestone_id"`
	Step               uint64                       `json:"step"`
	Kind               string                       `json:"kind"`
	Participant        *control.NodeRef             `json:"participant,omitempty"`
	RelatedParticipant *control.NodeRef             `json:"related_participant,omitempty"`
	Bindings           []RiskWitnessBindingEvidence `json:"bindings,omitempty"`
}

type RiskWitnessResolvedBinding struct {
	Name             string `json:"name"`
	Value            string `json:"value"`
	FirstMilestoneID string `json:"first_milestone_id"`
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
		Status:               riskWitnessProgressStatus(result.Status),
		SourceResultDigest:   result.Digest,
	}
	if len(result.MissingMilestones) > 0 {
		progress.FirstMissingMilestone = result.MissingMilestones[0]
	}
	progress.MilestoneEvidence, progress.ResolvedBindings = riskWitnessProgressEvidence(result)
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
	if !validRiskWitnessProgressEvidence(progress, seen) {
		return errors.New("RISK_WITNESS_PROGRESS_EVIDENCE_INVALID")
	}
	if progress.FirstMissingMilestone != "" &&
		(!known[progress.FirstMissingMilestone] || seen[progress.FirstMissingMilestone]) {
		return errors.New("RISK_WITNESS_PROGRESS_MISSING_INVALID")
	}
	switch progress.Status {
	case RiskWitnessInstantiated:
		if len(progress.SatisfiedMilestones) != len(spec.Milestones) ||
			progress.FirstMissingMilestone != "" {
			return errors.New("RISK_WITNESS_PROGRESS_STATUS_INVALID")
		}
	case RiskWitnessNotInstantiated:
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

func riskWitnessProgressStatus(status string) string {
	if status == RiskWitnessReached {
		return RiskWitnessInstantiated
	}
	return RiskWitnessNotInstantiated
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
	progress.MilestoneEvidence = cloneRiskWitnessProgressEvidence(progress.MilestoneEvidence)
	progress.ResolvedBindings = append([]RiskWitnessResolvedBinding(nil), progress.ResolvedBindings...)
	progress.Digest = ""
	digest, err := control.CanonicalDigest(progress)
	if err != nil {
		return RiskWitnessProgress{}, err
	}
	progress.Digest = digest
	return progress, nil
}

func riskWitnessProgressEvidence(
	result RiskWitnessResult,
) ([]RiskWitnessProgressEvidence, []RiskWitnessResolvedBinding) {
	evidence := make([]RiskWitnessProgressEvidence, 0, len(result.Milestones))
	bindings := make(map[string]RiskWitnessResolvedBinding)
	for _, milestone := range result.Milestones {
		value := RiskWitnessProgressEvidence{
			MilestoneID: milestone.MilestoneID, Step: milestone.Step, Kind: milestone.Kind,
			Bindings: append([]RiskWitnessBindingEvidence(nil), milestone.Bindings...),
		}
		if milestone.Participant != nil {
			participant := *milestone.Participant
			value.Participant = &participant
		}
		if milestone.RelatedParticipant != nil {
			related := *milestone.RelatedParticipant
			value.RelatedParticipant = &related
		}
		evidence = append(evidence, value)
		for _, binding := range milestone.Bindings {
			if _, exists := bindings[binding.Name]; !exists {
				bindings[binding.Name] = RiskWitnessResolvedBinding{
					Name: binding.Name, Value: binding.Value,
					FirstMilestoneID: milestone.MilestoneID,
				}
			}
		}
	}
	resolved := make([]RiskWitnessResolvedBinding, 0, len(bindings))
	for _, binding := range bindings {
		resolved = append(resolved, binding)
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Name < resolved[j].Name })
	return evidence, resolved
}

func validRiskWitnessProgressEvidence(progress RiskWitnessProgress, satisfied map[string]bool) bool {
	resolved := make(map[string]RiskWitnessResolvedBinding, len(progress.ResolvedBindings))
	for index, binding := range progress.ResolvedBindings {
		if !validRiskWitnessToken(binding.Name) || binding.Value == "" ||
			!satisfied[binding.FirstMilestoneID] || index > 0 && progress.ResolvedBindings[index-1].Name >= binding.Name {
			return false
		}
		resolved[binding.Name] = binding
	}
	seenMilestones := make(map[string]bool, len(progress.MilestoneEvidence))
	for _, evidence := range progress.MilestoneEvidence {
		if !satisfied[evidence.MilestoneID] || seenMilestones[evidence.MilestoneID] || evidence.Step == 0 ||
			!validRiskWitnessToken(evidence.Kind) {
			return false
		}
		seenMilestones[evidence.MilestoneID] = true
		if evidence.Participant != nil && evidence.Participant.Validate() != nil ||
			evidence.RelatedParticipant != nil && evidence.RelatedParticipant.Validate() != nil {
			return false
		}
		for _, binding := range evidence.Bindings {
			want, ok := resolved[binding.Name]
			if binding.validate() != nil || !ok || want.Value != binding.Value {
				return false
			}
		}
	}
	return len(seenMilestones) == len(satisfied)
}

func cloneRiskWitnessProgressEvidence(values []RiskWitnessProgressEvidence) []RiskWitnessProgressEvidence {
	result := make([]RiskWitnessProgressEvidence, len(values))
	copy(result, values)
	for index := range result {
		if values[index].Participant != nil {
			participant := *values[index].Participant
			result[index].Participant = &participant
		}
		if values[index].RelatedParticipant != nil {
			related := *values[index].RelatedParticipant
			result[index].RelatedParticipant = &related
		}
		result[index].Bindings = append([]RiskWitnessBindingEvidence(nil), values[index].Bindings...)
	}
	return result
}

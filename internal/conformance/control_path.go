package conformance

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const ControlPathAssessmentSchemaVersion = "consensus-atlas/control-path-assessment/v1"

// ControlPathWitness records trusted facts without accepting a caller-chosen grade.
type ControlPathWitness struct {
	ID                    string    `json:"id"`
	TargetID              string    `json:"target_id"`
	Surface               SurfaceID `json:"surface"`
	Granularity           string    `json:"granularity"`
	EvidenceDigest        string    `json:"evidence_digest"`
	Observed              bool      `json:"observed"`
	Intercepted           bool      `json:"intercepted"`
	RuntimeAction         bool      `json:"runtime_action"`
	SelectionChecked      bool      `json:"selection_checked"`
	StableItemID          bool      `json:"stable_item_id"`
	RuntimeOwnsTerminal   bool      `json:"runtime_owns_terminal"`
	AtomicControllerState bool      `json:"atomic_controller_state"`
	AtomicExternalEffect  bool      `json:"atomic_external_effect"`
	StrictReplay          bool      `json:"strict_replay"`
}
type ControlPathAssessment struct {
	SchemaVersion         string       `json:"schema_version"`
	ID                    string       `json:"id"`
	TargetID              string       `json:"target_id"`
	Surface               SurfaceID    `json:"surface"`
	Granularity           string       `json:"granularity"`
	Grade                 ControlGrade `json:"grade"`
	EvidenceDigest        string       `json:"evidence_digest"`
	StableItemID          bool         `json:"stable_item_id"`
	AtomicControllerState bool         `json:"atomic_controller_state"`
	AtomicExternalEffect  bool         `json:"atomic_external_effect"`
	StrictReplay          bool         `json:"strict_replay"`
	Digest                string       `json:"digest"`
}

func AssessControlPath(witness ControlPathWitness) (ControlPathAssessment, error) {
	if witness.ID == "" || witness.TargetID == "" || !isSurfaceID(witness.Surface) || witness.Granularity == "" || len(witness.EvidenceDigest) != 64 {
		return ControlPathAssessment{}, errors.New("CONTROL_PATH_WITNESS_IDENTITY_REQUIRED")
	}
	if (witness.Intercepted && !witness.Observed) || (witness.RuntimeAction && !witness.Intercepted) ||
		(witness.SelectionChecked && !witness.RuntimeAction) ||
		(witness.RuntimeOwnsTerminal && (!witness.StableItemID || !witness.SelectionChecked)) ||
		(witness.AtomicControllerState && !witness.RuntimeAction) ||
		(witness.AtomicExternalEffect && !witness.AtomicControllerState) {
		return ControlPathAssessment{}, errors.New("CONTROL_PATH_WITNESS_FACTS_INCONSISTENT")
	}
	grade := ControlOpaque
	if witness.Observed {
		grade = ControlObservable
	}
	if witness.Intercepted {
		grade = ControlInterceptable
	}
	if witness.RuntimeAction && witness.SelectionChecked {
		grade = ControlSchedulerActuated
	}
	if witness.RuntimeOwnsTerminal {
		grade = ControlSchedulerOwned
	}
	assessment := ControlPathAssessment{
		SchemaVersion: ControlPathAssessmentSchemaVersion, ID: witness.ID, TargetID: witness.TargetID,
		Surface: witness.Surface, Granularity: witness.Granularity, Grade: grade,
		EvidenceDigest: witness.EvidenceDigest, StableItemID: witness.StableItemID,
		AtomicControllerState: witness.AtomicControllerState, AtomicExternalEffect: witness.AtomicExternalEffect,
		StrictReplay: witness.StrictReplay,
	}
	return assessment.seal()
}

func (assessment ControlPathAssessment) seal() (ControlPathAssessment, error) {
	assessment.Digest = ""
	digest, err := control.CanonicalDigest(assessment)
	if err != nil {
		return ControlPathAssessment{}, err
	}
	assessment.Digest = digest
	return assessment, nil
}

func (assessment ControlPathAssessment) Validate() error {
	if assessment.SchemaVersion != ControlPathAssessmentSchemaVersion || assessment.ID == "" || assessment.TargetID == "" ||
		!isSurfaceID(assessment.Surface) || assessment.Granularity == "" || !validGrade(assessment.Grade) || len(assessment.EvidenceDigest) != 64 {
		return errors.New("CONTROL_PATH_ASSESSMENT_INVALID")
	}
	sealed, err := assessment.seal()
	if err != nil || sealed.Digest != assessment.Digest {
		return errors.New("CONTROL_PATH_ASSESSMENT_DIGEST_MISMATCH")
	}
	return nil
}

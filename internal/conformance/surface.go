package conformance

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	ControlSurfaceReportSchemaVersion = "consensus-atlas/control-surface-report/v1"
	CommonDistributedSurfaceProfile   = "common-distributed-surfaces/v1"
)

type SurfaceID string

const (
	SurfaceExternalInput SurfaceID = "external-input"
	SurfaceMessage       SurfaceID = "message"
	SurfaceLifecycle     SurfaceID = "lifecycle"
	SurfaceTemporal      SurfaceID = "temporal"
	SurfaceDurability    SurfaceID = "durability"
)

type SurfacePresence string

const (
	PresencePresent SurfacePresence = "present"
	PresenceAbsent  SurfacePresence = "absent"
	PresenceUnknown SurfacePresence = "unknown"
)

type ControlGrade string

const (
	ControlUnavailable    ControlGrade = "unavailable"
	ControlOpaque         ControlGrade = "opaque"
	ControlObservable     ControlGrade = "observable"
	ControlInterceptable  ControlGrade = "interceptable"
	ControlSchedulerOwned ControlGrade = "scheduler-owned"
)

type SurfaceDeclaration struct {
	ID            SurfaceID       `json:"id"`
	Presence      SurfacePresence `json:"presence"`
	NativeControl ControlGrade    `json:"native_control"`
	EvidenceCode  string          `json:"evidence_code"`
}

type SurfaceAssessment struct {
	ID                      SurfaceID        `json:"id"`
	Presence                SurfacePresence  `json:"presence"`
	PresenceEvidenceCode    string           `json:"presence_evidence_code"`
	DeclaredControl         ControlGrade     `json:"declared_control"`
	ValidatedControl        ControlGrade     `json:"validated_control"`
	QualificationCapability string           `json:"qualification_capability,omitempty"`
	QualificationStatus     CapabilityStatus `json:"qualification_status"`
}

type GuaranteeAssessment struct {
	ID                      string           `json:"id"`
	QualificationCapability string           `json:"qualification_capability"`
	Required                bool             `json:"required"`
	Status                  CapabilityStatus `json:"status"`
}

type ControlSurfaceSummary struct {
	Surfaces                int `json:"surfaces"`
	Present                 int `json:"present"`
	DeclaredSchedulerOwned  int `json:"declared_scheduler_owned"`
	ValidatedSchedulerOwned int `json:"validated_scheduler_owned"`
	Guarantees              int `json:"guarantees"`
	RequiredGuarantees      int `json:"required_guarantees"`
	ValidatedGuarantees     int `json:"validated_guarantees"`
}

// ControlSurfaceReport separates common runtime phenomena from deterministic
// control guarantees. Presence is trusted onboarding knowledge; validated
// control credit is derived from the Adapter Manifest and QualificationReport.
type ControlSurfaceReport struct {
	SchemaVersion       string                `json:"schema_version"`
	ProfileID           string                `json:"profile_id"`
	AdapterID           string                `json:"adapter_id"`
	ImplementationID    string                `json:"implementation_id"`
	ManifestDigest      string                `json:"manifest_digest"`
	QualificationDigest string                `json:"qualification_digest"`
	Surfaces            []SurfaceAssessment   `json:"surfaces"`
	Guarantees          []GuaranteeAssessment `json:"guarantees"`
	Summary             ControlSurfaceSummary `json:"summary"`
	Digest              string                `json:"digest"`
}

var surfaceCapability = map[SurfaceID]string{
	SurfaceExternalInput: "opaque-invoke-boundary",
	SurfaceMessage:       "runtime-owned-message",
	SurfaceLifecycle:     "crash-restart-incarnation",
	SurfaceTemporal:      "natural-temporal-progress",
}

var guaranteeCapabilities = []struct {
	id, capability string
}{
	{"audited-entropy", "audited-entropy-replay"},
	{"process-isolation", "formal-process-isolation"},
	{"pure-enabled-set", "pure-enabled-check"},
	{"stable-yield-evidence", "strict-yield-evidence"},
	{"strict-decision-replay", "strict-decision-replay"},
}

func EvaluateControlSurfaces(manifest control.AdapterManifest, qualification QualificationReport, declarations []SurfaceDeclaration) (ControlSurfaceReport, error) {
	if err := manifest.Validate(); err != nil {
		return ControlSurfaceReport{}, err
	}
	if err := qualification.Validate(); err != nil {
		return ControlSurfaceReport{}, err
	}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		return ControlSurfaceReport{}, err
	}
	if qualification.ManifestDigest != manifestDigest || qualification.AdapterID != manifest.AdapterID || qualification.ImplementationID != manifest.ImplementationID {
		return ControlSurfaceReport{}, errors.New("CONTROL_SURFACE_QUALIFICATION_IDENTITY_MISMATCH")
	}
	declared, err := normalizeSurfaceDeclarations(declarations)
	if err != nil {
		return ControlSurfaceReport{}, err
	}
	capabilities := make(map[string]CapabilityQualification, len(qualification.Capabilities))
	for _, capability := range qualification.Capabilities {
		capabilities[capability.ID] = capability
	}

	report := ControlSurfaceReport{
		ProfileID: CommonDistributedSurfaceProfile, AdapterID: manifest.AdapterID,
		ImplementationID: manifest.ImplementationID, ManifestDigest: manifestDigest,
		QualificationDigest: qualification.Digest,
	}
	for _, id := range commonSurfaceIDs() {
		declaration := declared[id]
		declaredControl := declaredGrade(manifest, declaration)
		capabilityID := surfaceCapability[id]
		capability, found := capabilities[capabilityID]
		status := CapabilityUnvalidated
		if found {
			status = capability.Status
		}
		validatedControl := baselineGrade(declaration.Presence)
		if status == CapabilityValidated {
			validatedControl = declaredControl
		}
		report.Surfaces = append(report.Surfaces, SurfaceAssessment{
			ID: id, Presence: declaration.Presence, PresenceEvidenceCode: declaration.EvidenceCode,
			DeclaredControl: declaredControl, ValidatedControl: validatedControl,
			QualificationCapability: capabilityID, QualificationStatus: status,
		})
	}
	for _, definition := range guaranteeCapabilities {
		capability, found := capabilities[definition.capability]
		assessment := GuaranteeAssessment{
			ID: definition.id, QualificationCapability: definition.capability, Status: CapabilityUnvalidated,
		}
		if found {
			assessment.Required, assessment.Status = capability.Required, capability.Status
		}
		report.Guarantees = append(report.Guarantees, assessment)
	}
	return report.Seal()
}

func (report ControlSurfaceReport) Seal() (ControlSurfaceReport, error) {
	report.SchemaVersion = ControlSurfaceReportSchemaVersion
	sort.Slice(report.Surfaces, func(i, j int) bool { return report.Surfaces[i].ID < report.Surfaces[j].ID })
	sort.Slice(report.Guarantees, func(i, j int) bool { return report.Guarantees[i].ID < report.Guarantees[j].ID })
	report.recount()
	report.Digest = ""
	if err := report.validateContent(); err != nil {
		return ControlSurfaceReport{}, err
	}
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		return ControlSurfaceReport{}, err
	}
	report.Digest = digest
	return report, nil
}

func (report ControlSurfaceReport) Validate() error {
	if report.SchemaVersion != ControlSurfaceReportSchemaVersion {
		return errors.New("CONTROL_SURFACE_REPORT_SCHEMA_MISMATCH")
	}
	sealed, err := report.Seal()
	if err != nil {
		return err
	}
	if sealed.Digest != report.Digest {
		return errors.New("CONTROL_SURFACE_REPORT_DIGEST_MISMATCH")
	}
	return nil
}

func (report ControlSurfaceReport) validateContent() error {
	if report.ProfileID != CommonDistributedSurfaceProfile || report.AdapterID == "" || report.ImplementationID == "" || report.ManifestDigest == "" || report.QualificationDigest == "" {
		return errors.New("CONTROL_SURFACE_REPORT_IDENTITY_REQUIRED")
	}
	if len(report.Surfaces) != len(commonSurfaceIDs()) || len(report.Guarantees) != len(guaranteeCapabilities) {
		return errors.New("CONTROL_SURFACE_REPORT_DIMENSION_MISMATCH")
	}
	seen := make(map[SurfaceID]struct{}, len(report.Surfaces))
	for _, surface := range report.Surfaces {
		if _, ok := seen[surface.ID]; ok || !isSurfaceID(surface.ID) || surface.PresenceEvidenceCode == "" {
			return errors.New("CONTROL_SURFACE_REPORT_SURFACE_INVALID")
		}
		seen[surface.ID] = struct{}{}
		if !validPresence(surface.Presence) || !validGrade(surface.DeclaredControl) || !validGrade(surface.ValidatedControl) || !validStatus(surface.QualificationStatus) {
			return errors.New("CONTROL_SURFACE_REPORT_VALUE_INVALID")
		}
		if surface.QualificationCapability != surfaceCapability[surface.ID] ||
			(surface.Presence != PresencePresent && (surface.DeclaredControl != ControlUnavailable || surface.ValidatedControl != ControlUnavailable)) ||
			(surface.Presence == PresencePresent && gradeRank(surface.DeclaredControl) < gradeRank(ControlOpaque)) {
			return errors.New("CONTROL_SURFACE_REPORT_CONTROL_INVALID")
		}
		if gradeRank(surface.ValidatedControl) > gradeRank(surface.DeclaredControl) || (surface.QualificationStatus != CapabilityValidated && gradeRank(surface.ValidatedControl) > gradeRank(ControlOpaque)) {
			return errors.New("CONTROL_SURFACE_REPORT_UNVALIDATED_CREDIT")
		}
	}
	knownGuarantees := make(map[string]string, len(guaranteeCapabilities))
	for _, definition := range guaranteeCapabilities {
		knownGuarantees[definition.id] = definition.capability
	}
	seenGuarantees := make(map[string]struct{}, len(report.Guarantees))
	for _, guarantee := range report.Guarantees {
		expected, known := knownGuarantees[guarantee.ID]
		if !known || expected != guarantee.QualificationCapability || !validStatus(guarantee.Status) {
			return errors.New("CONTROL_SURFACE_REPORT_GUARANTEE_INVALID")
		}
		if _, duplicate := seenGuarantees[guarantee.ID]; duplicate {
			return errors.New("CONTROL_SURFACE_REPORT_GUARANTEE_DUPLICATE")
		}
		seenGuarantees[guarantee.ID] = struct{}{}
	}
	want := report
	want.recount()
	if want.Summary != report.Summary {
		return errors.New("CONTROL_SURFACE_REPORT_SUMMARY_MISMATCH")
	}
	return nil
}

func (report *ControlSurfaceReport) recount() {
	report.Summary = ControlSurfaceSummary{Surfaces: len(report.Surfaces), Guarantees: len(report.Guarantees)}
	for _, surface := range report.Surfaces {
		if surface.Presence == PresencePresent {
			report.Summary.Present++
		}
		if surface.DeclaredControl == ControlSchedulerOwned {
			report.Summary.DeclaredSchedulerOwned++
		}
		if surface.ValidatedControl == ControlSchedulerOwned {
			report.Summary.ValidatedSchedulerOwned++
		}
	}
	for _, guarantee := range report.Guarantees {
		if guarantee.Required {
			report.Summary.RequiredGuarantees++
		}
		if guarantee.Status == CapabilityValidated {
			report.Summary.ValidatedGuarantees++
		}
	}
}

func normalizeSurfaceDeclarations(declarations []SurfaceDeclaration) (map[SurfaceID]SurfaceDeclaration, error) {
	if len(declarations) != len(commonSurfaceIDs()) {
		return nil, errors.New("CONTROL_SURFACE_DECLARATIONS_INCOMPLETE")
	}
	result := make(map[SurfaceID]SurfaceDeclaration, len(declarations))
	for _, declaration := range declarations {
		if !isSurfaceID(declaration.ID) || !validPresence(declaration.Presence) || !validGrade(declaration.NativeControl) || declaration.EvidenceCode == "" {
			return nil, errors.New("CONTROL_SURFACE_DECLARATION_INVALID")
		}
		if gradeRank(declaration.NativeControl) > gradeRank(ControlInterceptable) {
			return nil, errors.New("CONTROL_SURFACE_NATIVE_SELF_CREDIT_FORBIDDEN")
		}
		if declaration.Presence != PresencePresent && declaration.NativeControl != ControlUnavailable {
			return nil, errors.New("CONTROL_SURFACE_ABSENT_CONTROL_FORBIDDEN")
		}
		if declaration.Presence == PresencePresent && declaration.NativeControl == ControlUnavailable {
			return nil, errors.New("CONTROL_SURFACE_PRESENT_GRADE_REQUIRED")
		}
		if _, ok := result[declaration.ID]; ok {
			return nil, errors.New("CONTROL_SURFACE_DECLARATION_DUPLICATE")
		}
		result[declaration.ID] = declaration
	}
	return result, nil
}

func declaredGrade(manifest control.AdapterManifest, declaration SurfaceDeclaration) ControlGrade {
	if declaration.Presence != PresencePresent {
		return ControlUnavailable
	}
	grade := declaration.NativeControl
	actions, items := stringSet(actionStrings(manifest.Capabilities.Actions)), stringSet(itemStrings(manifest.Capabilities.Items))
	hasAction := func(kind control.ActionKind) bool { _, ok := actions[string(kind)]; return ok }
	hasItem := func(kind control.ItemKind) bool { _, ok := items[string(kind)]; return ok }
	switch declaration.ID {
	case SurfaceExternalInput:
		if hasAction(control.ActionInvoke) {
			grade = maxGrade(grade, ControlSchedulerOwned)
		}
	case SurfaceMessage:
		if hasItem(control.ItemMessage) {
			grade = maxGrade(grade, ControlInterceptable)
			if hasAction(control.ActionDeliverMessage) && hasAction(control.ActionDropMessage) {
				grade = ControlSchedulerOwned
			}
		}
	case SurfaceLifecycle:
		if hasAction(control.ActionCrash) || hasAction(control.ActionRestart) {
			grade = maxGrade(grade, ControlInterceptable)
		}
		if hasAction(control.ActionCrash) && hasAction(control.ActionRestart) {
			grade = ControlSchedulerOwned
		}
	case SurfaceTemporal:
		if hasItem(control.ItemTemporal) {
			grade = maxGrade(grade, ControlInterceptable)
			if hasAction(control.ActionFireTemporal) {
				grade = ControlSchedulerOwned
			}
		}
	case SurfaceDurability:
		if hasItem(control.ItemEffect) {
			grade = maxGrade(grade, ControlInterceptable)
			if hasAction(control.ActionCompleteEffect) && manifest.Capabilities.DurableCheckpoints {
				grade = ControlSchedulerOwned
			}
		}
	}
	return grade
}

func commonSurfaceIDs() []SurfaceID {
	return []SurfaceID{SurfaceDurability, SurfaceExternalInput, SurfaceLifecycle, SurfaceMessage, SurfaceTemporal}
}

func baselineGrade(presence SurfacePresence) ControlGrade {
	if presence == PresencePresent {
		return ControlOpaque
	}
	return ControlUnavailable
}

func maxGrade(left, right ControlGrade) ControlGrade {
	if gradeRank(right) > gradeRank(left) {
		return right
	}
	return left
}

func gradeRank(grade ControlGrade) int {
	switch grade {
	case ControlUnavailable:
		return 0
	case ControlOpaque:
		return 1
	case ControlObservable:
		return 2
	case ControlInterceptable:
		return 3
	case ControlSchedulerOwned:
		return 4
	default:
		return -1
	}
}

func validGrade(grade ControlGrade) bool { return gradeRank(grade) >= 0 }
func validPresence(presence SurfacePresence) bool {
	return presence == PresencePresent || presence == PresenceAbsent || presence == PresenceUnknown
}
func isSurfaceID(id SurfaceID) bool {
	for _, candidate := range commonSurfaceIDs() {
		if candidate == id {
			return true
		}
	}
	return false
}
func validStatus(status CapabilityStatus) bool {
	switch status {
	case CapabilityValidated, CapabilityUnsupported, CapabilityUndeclared, CapabilityUnvalidated, CapabilityFailed:
		return true
	default:
		return false
	}
}

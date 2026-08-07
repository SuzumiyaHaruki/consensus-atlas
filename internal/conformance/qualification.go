package conformance

import (
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	QualificationProfileSchemaVersion = "consensus-atlas/adapter-qualification-profile/v1"
	QualificationReportSchemaVersion  = "consensus-atlas/adapter-qualification-report/v1"

	CapabilityValidated   CapabilityStatus = "validated"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityUndeclared  CapabilityStatus = "undeclared"
	CapabilityUnvalidated CapabilityStatus = "unvalidated"
	CapabilityFailed      CapabilityStatus = "failed"

	ReasonManifestNodeCount         = "QUALIFICATION_MANIFEST_NODE_COUNT_MISSING"
	ReasonManifestAction            = "QUALIFICATION_MANIFEST_ACTION_MISSING"
	ReasonManifestItem              = "QUALIFICATION_MANIFEST_ITEM_MISSING"
	ReasonManifestTemporal          = "QUALIFICATION_MANIFEST_TEMPORAL_KIND_MISSING"
	ReasonManifestCrashMode         = "QUALIFICATION_MANIFEST_CRASH_MODE_MISSING"
	ReasonManifestStrictYield       = "QUALIFICATION_MANIFEST_STRICT_YIELD_MISSING"
	ReasonManifestStrictReplay      = "QUALIFICATION_MANIFEST_STRICT_REPLAY_MISSING"
	ReasonManifestEntropyReplay     = "QUALIFICATION_MANIFEST_ENTROPY_REPLAY_MISSING"
	ReasonManifestDurableCheckpoint = "QUALIFICATION_MANIFEST_DURABLE_CHECKPOINT_MISSING"
	ReasonManifestEvidenceSchema    = "QUALIFICATION_MANIFEST_EVIDENCE_SCHEMA_MISSING"
	ReasonWitnessMissing            = "QUALIFICATION_WITNESS_MISSING"
	ReasonWitnessFailed             = "QUALIFICATION_WITNESS_FAILED"
	ReasonExplicitUnsupported       = "QUALIFICATION_EXPLICIT_UNSUPPORTED"

	UnsupportedControlSurface      = "ADAPTER_CONTROL_SURFACE_UNAVAILABLE"
	UnsupportedClockControl        = "ADAPTER_CLOCK_CONTROL_UNAVAILABLE"
	UnsupportedEntropyControl      = "ADAPTER_ENTROPY_CONTROL_UNAVAILABLE"
	UnsupportedProcessIsolation    = "ADAPTER_PROCESS_ISOLATION_REQUIRED"
	UnsupportedDurabilityBoundary  = "ADAPTER_DURABILITY_BOUNDARY_UNAVAILABLE"
	UnsupportedObservationBoundary = "ADAPTER_OBSERVATION_BOUNDARY_UNAVAILABLE"
	UnsupportedOperation           = "ADAPTER_OPERATION_UNAVAILABLE"
	UnsupportedConformanceWitness  = "ADAPTER_CONFORMANCE_WITNESS_UNAVAILABLE"
)

var allowedUnsupportedReasons = map[string]struct{}{
	UnsupportedControlSurface: {}, UnsupportedClockControl: {},
	UnsupportedEntropyControl: {}, UnsupportedProcessIsolation: {},
	UnsupportedDurabilityBoundary: {}, UnsupportedObservationBoundary: {},
	UnsupportedOperation: {}, UnsupportedConformanceWitness: {},
}

type CapabilityStatus string

// ManifestRequirements describes only protocol-neutral facts that can be
// checked against control.AdapterManifest. Empty slices and false booleans do
// not impose a requirement.
type ManifestRequirements struct {
	MinNodes            int                    `json:"min_nodes,omitempty"`
	Actions             []control.ActionKind   `json:"actions,omitempty"`
	Items               []control.ItemKind     `json:"items,omitempty"`
	TemporalKinds       []control.TemporalKind `json:"temporal_kinds,omitempty"`
	AnyTemporalKinds    []control.TemporalKind `json:"any_temporal_kinds,omitempty"`
	CrashModes          []string               `json:"crash_modes,omitempty"`
	StrictYield         bool                   `json:"strict_yield,omitempty"`
	StrictReplay        bool                   `json:"strict_replay,omitempty"`
	EntropyStrictReplay bool                   `json:"entropy_strict_replay,omitempty"`
	DurableCheckpoints  bool                   `json:"durable_checkpoints,omitempty"`
	EvidenceSchema      bool                   `json:"evidence_schema,omitempty"`
}

// CapabilityRequirement binds one manifest declaration to external case IDs.
// Qualification never accepts a capability from the manifest alone.
type CapabilityRequirement struct {
	ID               string               `json:"id"`
	Required         bool                 `json:"required"`
	Manifest         ManifestRequirements `json:"manifest"`
	ConformanceCases []string             `json:"conformance_cases"`
}

// QualificationProfile is the reusable Adapter onboarding template. It is
// protocol-neutral and versioned independently from a concrete Adapter.
type QualificationProfile struct {
	SchemaVersion string                  `json:"schema_version"`
	ID            string                  `json:"id"`
	Capabilities  []CapabilityRequirement `json:"capabilities"`
	Digest        string                  `json:"digest"`
}

type UnsupportedDeclaration struct {
	CapabilityID string `json:"capability_id"`
	ReasonCode   string `json:"reason_code"`
}

type QualificationFinding struct {
	Code    string `json:"code"`
	Kind    string `json:"kind,omitempty"`
	Subject string `json:"subject,omitempty"`
}

type CapabilityQualification struct {
	ID                    string                 `json:"id"`
	Required              bool                   `json:"required"`
	Declared              bool                   `json:"declared"`
	Status                CapabilityStatus       `json:"status"`
	ConformanceCases      []string               `json:"conformance_cases"`
	UnsupportedReasonCode string                 `json:"unsupported_reason_code,omitempty"`
	Findings              []QualificationFinding `json:"findings,omitempty"`
}

type QualificationSummary struct {
	Total       int `json:"total"`
	Required    int `json:"required"`
	Validated   int `json:"validated"`
	Unsupported int `json:"unsupported"`
	Undeclared  int `json:"undeclared"`
	Unvalidated int `json:"unvalidated"`
	Failed      int `json:"failed"`
}

// QualificationReport contains no human-written qualification state. Every
// status is derived from the sealed profile, Adapter manifest, explicit
// unsupported declarations, and trusted conformance case results.
type QualificationReport struct {
	SchemaVersion            string                    `json:"schema_version"`
	ProfileID                string                    `json:"profile_id"`
	ProfileDigest            string                    `json:"profile_digest"`
	AdapterID                string                    `json:"adapter_id"`
	ImplementationID         string                    `json:"implementation_id"`
	BuildID                  string                    `json:"build_id"`
	ConfigurationDigest      string                    `json:"configuration_digest"`
	ManifestDigest           string                    `json:"manifest_digest"`
	ConformanceReportDigests []string                  `json:"conformance_report_digests"`
	Unsupported              []UnsupportedDeclaration  `json:"unsupported,omitempty"`
	Capabilities             []CapabilityQualification `json:"capabilities"`
	Summary                  QualificationSummary      `json:"summary"`
	Qualified                bool                      `json:"qualified"`
	Digest                   string                    `json:"digest"`
}

func (profile QualificationProfile) Seal() (QualificationProfile, error) {
	profile.SchemaVersion = QualificationProfileSchemaVersion
	profile = normalizeProfile(profile)
	profile.Digest = ""
	if err := profile.validateContent(); err != nil {
		return QualificationProfile{}, err
	}
	digest, err := control.CanonicalDigest(profile)
	if err != nil {
		return QualificationProfile{}, err
	}
	profile.Digest = digest
	return profile, nil
}

func (profile QualificationProfile) Validate() error {
	if profile.SchemaVersion != QualificationProfileSchemaVersion {
		return errors.New("QUALIFICATION_PROFILE_SCHEMA_MISMATCH")
	}
	sealed, err := profile.Seal()
	if err != nil {
		return err
	}
	if sealed.Digest != profile.Digest {
		return errors.New("QUALIFICATION_PROFILE_DIGEST_MISMATCH")
	}
	return nil
}

func (profile QualificationProfile) validateContent() error {
	if profile.ID == "" {
		return errors.New("QUALIFICATION_PROFILE_ID_REQUIRED")
	}
	if len(profile.Capabilities) == 0 {
		return errors.New("QUALIFICATION_PROFILE_CAPABILITIES_REQUIRED")
	}
	seen := make(map[string]struct{}, len(profile.Capabilities))
	required := 0
	for _, capability := range profile.Capabilities {
		if capability.ID == "" {
			return errors.New("QUALIFICATION_CAPABILITY_ID_REQUIRED")
		}
		if _, ok := seen[capability.ID]; ok {
			return errors.New("QUALIFICATION_CAPABILITY_DUPLICATE")
		}
		seen[capability.ID] = struct{}{}
		if capability.Required {
			required++
		}
		if len(capability.ConformanceCases) == 0 {
			return fmt.Errorf("QUALIFICATION_CAPABILITY_CASES_REQUIRED: %s", capability.ID)
		}
		if err := capability.Manifest.validate(); err != nil {
			return fmt.Errorf("capability %s: %w", capability.ID, err)
		}
		if err := uniqueNonEmpty(capability.ConformanceCases); err != nil {
			return fmt.Errorf("capability %s cases: %w", capability.ID, err)
		}
	}
	if required == 0 {
		return errors.New("QUALIFICATION_PROFILE_REQUIRED_CAPABILITY_MISSING")
	}
	return nil
}

func (requirements ManifestRequirements) validate() error {
	if requirements.MinNodes < 0 {
		return errors.New("QUALIFICATION_MIN_NODES_INVALID")
	}
	for _, action := range requirements.Actions {
		if err := action.Validate(); err != nil {
			return err
		}
	}
	for _, item := range requirements.Items {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	for _, temporal := range requirements.TemporalKinds {
		if err := temporal.Validate(); err != nil {
			return err
		}
	}
	for _, temporal := range requirements.AnyTemporalKinds {
		if err := temporal.Validate(); err != nil {
			return err
		}
	}
	if err := uniqueNonEmpty(actionStrings(requirements.Actions)); err != nil {
		return err
	}
	if err := uniqueNonEmpty(itemStrings(requirements.Items)); err != nil {
		return err
	}
	if err := uniqueNonEmpty(temporalStrings(requirements.TemporalKinds)); err != nil {
		return err
	}
	if err := uniqueNonEmpty(temporalStrings(requirements.AnyTemporalKinds)); err != nil {
		return err
	}
	return uniqueNonEmpty(requirements.CrashModes)
}

// Qualify mechanically compares the four trusted inputs. Conformance reports
// must be freshly produced by the trusted runner in the same workflow; their
// digest is an integrity binding, not an authenticity mechanism.
func Qualify(
	manifest control.AdapterManifest,
	profile QualificationProfile,
	unsupported []UnsupportedDeclaration,
	reports []Report,
) (QualificationReport, error) {
	if err := manifest.Validate(); err != nil {
		return QualificationReport{}, err
	}
	if err := profile.Validate(); err != nil {
		return QualificationReport{}, err
	}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		return QualificationReport{}, err
	}
	unsupportedByID, normalizedUnsupported, err := validateUnsupported(profile, unsupported)
	if err != nil {
		return QualificationReport{}, err
	}
	cases, reportDigests, err := validateReports(manifestDigest, reports)
	if err != nil {
		return QualificationReport{}, err
	}

	report := QualificationReport{
		SchemaVersion: QualificationReportSchemaVersion,
		ProfileID:     profile.ID, ProfileDigest: profile.Digest,
		AdapterID: manifest.AdapterID, ImplementationID: manifest.ImplementationID,
		BuildID: manifest.BuildID, ConfigurationDigest: manifest.ConfigurationDigest,
		ManifestDigest: manifestDigest, ConformanceReportDigests: reportDigests,
		Unsupported: normalizedUnsupported, Qualified: true,
	}
	for _, capability := range profile.Capabilities {
		result := qualifyCapability(manifest, capability, unsupportedByID, cases)
		report.Capabilities = append(report.Capabilities, result)
		if result.Required && result.Status != CapabilityValidated {
			report.Qualified = false
		}
	}
	report.recount()
	return report.Seal()
}

func qualifyCapability(
	manifest control.AdapterManifest,
	capability CapabilityRequirement,
	unsupported map[string]UnsupportedDeclaration,
	cases map[string]CaseResult,
) CapabilityQualification {
	result := CapabilityQualification{
		ID: capability.ID, Required: capability.Required,
		ConformanceCases: append([]string(nil), capability.ConformanceCases...),
	}
	result.Findings = missingManifestRequirements(manifest, capability.Manifest)
	result.Declared = len(result.Findings) == 0
	declaration, explicitlyUnsupported := unsupported[capability.ID]
	if explicitlyUnsupported {
		result.Status = CapabilityUnsupported
		result.UnsupportedReasonCode = declaration.ReasonCode
		result.Findings = append(result.Findings, QualificationFinding{
			Code: ReasonExplicitUnsupported, Kind: "unsupported", Subject: declaration.ReasonCode,
		})
		return result
	}
	if !result.Declared {
		result.Status = CapabilityUndeclared
		return result
	}
	for _, witnessID := range capability.ConformanceCases {
		witness, ok := cases[witnessID]
		if !ok {
			result.Findings = append(result.Findings, QualificationFinding{
				Code: ReasonWitnessMissing, Kind: "conformance-case", Subject: witnessID,
			})
			continue
		}
		if !witness.Passed {
			result.Findings = append(result.Findings, QualificationFinding{
				Code: ReasonWitnessFailed, Kind: "conformance-case", Subject: witnessID + ":" + witness.ReasonCode,
			})
		}
	}
	if len(result.Findings) == 0 {
		result.Status = CapabilityValidated
		return result
	}
	result.Status = CapabilityUnvalidated
	for _, finding := range result.Findings {
		if finding.Code == ReasonWitnessFailed {
			result.Status = CapabilityFailed
		}
	}
	return result
}

func missingManifestRequirements(
	manifest control.AdapterManifest,
	requirements ManifestRequirements,
) []QualificationFinding {
	var findings []QualificationFinding
	if len(manifest.Nodes) < requirements.MinNodes {
		findings = append(findings, QualificationFinding{
			Code: ReasonManifestNodeCount, Kind: "min-nodes", Subject: fmt.Sprint(requirements.MinNodes),
		})
	}
	actions := make(map[control.ActionKind]struct{}, len(manifest.Capabilities.Actions))
	for _, action := range manifest.Capabilities.Actions {
		actions[action] = struct{}{}
	}
	for _, action := range requirements.Actions {
		if _, ok := actions[action]; !ok {
			findings = append(findings, QualificationFinding{Code: ReasonManifestAction, Kind: "action", Subject: string(action)})
		}
	}
	items := make(map[control.ItemKind]struct{}, len(manifest.Capabilities.Items))
	for _, item := range manifest.Capabilities.Items {
		items[item] = struct{}{}
	}
	for _, item := range requirements.Items {
		if _, ok := items[item]; !ok {
			findings = append(findings, QualificationFinding{Code: ReasonManifestItem, Kind: "item", Subject: string(item)})
		}
	}
	temporal := make(map[control.TemporalKind]struct{}, len(manifest.Capabilities.Temporal.Kinds))
	for _, kind := range manifest.Capabilities.Temporal.Kinds {
		temporal[kind] = struct{}{}
	}
	for _, kind := range requirements.TemporalKinds {
		if _, ok := temporal[kind]; !ok {
			findings = append(findings, QualificationFinding{Code: ReasonManifestTemporal, Kind: "temporal-kind", Subject: string(kind)})
		}
	}
	if len(requirements.AnyTemporalKinds) > 0 {
		found := false
		for _, kind := range requirements.AnyTemporalKinds {
			if _, ok := temporal[kind]; ok {
				found = true
				break
			}
		}
		if !found {
			findings = append(findings, QualificationFinding{
				Code: ReasonManifestTemporal, Kind: "any-temporal-kind",
				Subject: joinTemporalKinds(requirements.AnyTemporalKinds),
			})
		}
	}
	crashModes := stringSet(manifest.Capabilities.CrashModes)
	for _, mode := range requirements.CrashModes {
		if _, ok := crashModes[mode]; !ok {
			findings = append(findings, QualificationFinding{Code: ReasonManifestCrashMode, Kind: "crash-mode", Subject: mode})
		}
	}
	if requirements.StrictYield && !manifest.Capabilities.StrictYield {
		findings = append(findings, QualificationFinding{Code: ReasonManifestStrictYield, Kind: "boolean", Subject: "strict-yield"})
	}
	if requirements.StrictReplay && !manifest.Capabilities.StrictReplay {
		findings = append(findings, QualificationFinding{Code: ReasonManifestStrictReplay, Kind: "boolean", Subject: "strict-replay"})
	}
	if requirements.EntropyStrictReplay && !manifest.Capabilities.Entropy.StrictReplay {
		findings = append(findings, QualificationFinding{Code: ReasonManifestEntropyReplay, Kind: "boolean", Subject: "entropy-strict-replay"})
	}
	if requirements.DurableCheckpoints && !manifest.Capabilities.DurableCheckpoints {
		findings = append(findings, QualificationFinding{Code: ReasonManifestDurableCheckpoint, Kind: "boolean", Subject: "durable-checkpoints"})
	}
	if requirements.EvidenceSchema && len(manifest.EvidenceSchemas) == 0 {
		findings = append(findings, QualificationFinding{Code: ReasonManifestEvidenceSchema, Kind: "boolean", Subject: "evidence-schema"})
	}
	sortFindings(findings)
	return findings
}

func validateUnsupported(
	profile QualificationProfile,
	declarations []UnsupportedDeclaration,
) (map[string]UnsupportedDeclaration, []UnsupportedDeclaration, error) {
	known := make(map[string]struct{}, len(profile.Capabilities))
	for _, capability := range profile.Capabilities {
		known[capability.ID] = struct{}{}
	}
	result := make(map[string]UnsupportedDeclaration, len(declarations))
	for _, declaration := range declarations {
		if _, ok := known[declaration.CapabilityID]; !ok {
			return nil, nil, errors.New("QUALIFICATION_UNSUPPORTED_CAPABILITY_UNKNOWN")
		}
		if _, ok := allowedUnsupportedReasons[declaration.ReasonCode]; !ok {
			return nil, nil, errors.New("QUALIFICATION_UNSUPPORTED_REASON_UNKNOWN")
		}
		if _, ok := result[declaration.CapabilityID]; ok {
			return nil, nil, errors.New("QUALIFICATION_UNSUPPORTED_CAPABILITY_DUPLICATE")
		}
		result[declaration.CapabilityID] = declaration
	}
	normalized := append([]UnsupportedDeclaration(nil), declarations...)
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].CapabilityID < normalized[j].CapabilityID })
	return result, normalized, nil
}

func validateReports(manifestDigest string, reports []Report) (map[string]CaseResult, []string, error) {
	cases := make(map[string]CaseResult)
	digests := make([]string, 0, len(reports))
	seenDigests := make(map[string]struct{}, len(reports))
	for _, report := range reports {
		if err := report.Validate(); err != nil {
			return nil, nil, err
		}
		if report.ManifestDigest != manifestDigest {
			return nil, nil, errors.New("QUALIFICATION_CONFORMANCE_MANIFEST_MISMATCH")
		}
		if _, ok := seenDigests[report.Digest]; ok {
			return nil, nil, errors.New("QUALIFICATION_CONFORMANCE_REPORT_DUPLICATE")
		}
		seenDigests[report.Digest] = struct{}{}
		digests = append(digests, report.Digest)
		for _, result := range report.Cases {
			if _, ok := cases[result.ID]; ok {
				return nil, nil, errors.New("QUALIFICATION_CONFORMANCE_CASE_DUPLICATE")
			}
			cases[result.ID] = result
		}
	}
	sort.Strings(digests)
	return cases, digests, nil
}

func (report QualificationReport) Seal() (QualificationReport, error) {
	report.SchemaVersion = QualificationReportSchemaVersion
	sort.Strings(report.ConformanceReportDigests)
	sort.Slice(report.Unsupported, func(i, j int) bool {
		return report.Unsupported[i].CapabilityID < report.Unsupported[j].CapabilityID
	})
	sort.Slice(report.Capabilities, func(i, j int) bool {
		return report.Capabilities[i].ID < report.Capabilities[j].ID
	})
	for index := range report.Capabilities {
		sort.Strings(report.Capabilities[index].ConformanceCases)
		sortFindings(report.Capabilities[index].Findings)
	}
	report.recount()
	report.Digest = ""
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		return QualificationReport{}, err
	}
	report.Digest = digest
	return report, nil
}

func (report QualificationReport) Validate() error {
	if report.SchemaVersion != QualificationReportSchemaVersion {
		return errors.New("QUALIFICATION_REPORT_SCHEMA_MISMATCH")
	}
	if report.ProfileID == "" || report.ProfileDigest == "" || report.AdapterID == "" ||
		report.ImplementationID == "" || report.BuildID == "" || report.ConfigurationDigest == "" ||
		report.ManifestDigest == "" {
		return errors.New("QUALIFICATION_REPORT_IDENTITY_REQUIRED")
	}
	if len(report.Capabilities) == 0 {
		return errors.New("QUALIFICATION_REPORT_CAPABILITIES_REQUIRED")
	}
	unsupported := make(map[string]string, len(report.Unsupported))
	for _, declaration := range report.Unsupported {
		if _, ok := allowedUnsupportedReasons[declaration.ReasonCode]; !ok {
			return errors.New("QUALIFICATION_REPORT_UNSUPPORTED_REASON_UNKNOWN")
		}
		if _, ok := unsupported[declaration.CapabilityID]; ok || declaration.CapabilityID == "" {
			return errors.New("QUALIFICATION_REPORT_UNSUPPORTED_DUPLICATE")
		}
		unsupported[declaration.CapabilityID] = declaration.ReasonCode
	}
	seenCapabilities := make(map[string]struct{}, len(report.Capabilities))
	for _, capability := range report.Capabilities {
		if capability.ID == "" {
			return errors.New("QUALIFICATION_REPORT_CAPABILITY_ID_REQUIRED")
		}
		if _, ok := seenCapabilities[capability.ID]; ok {
			return errors.New("QUALIFICATION_REPORT_CAPABILITY_DUPLICATE")
		}
		seenCapabilities[capability.ID] = struct{}{}
		if len(capability.ConformanceCases) == 0 {
			return errors.New("QUALIFICATION_REPORT_CAPABILITY_CASES_REQUIRED")
		}
		switch capability.Status {
		case CapabilityValidated, CapabilityUnvalidated, CapabilityFailed:
			if !capability.Declared {
				return errors.New("QUALIFICATION_REPORT_DECLARED_STATUS_MISMATCH")
			}
			if capability.UnsupportedReasonCode != "" {
				return errors.New("QUALIFICATION_REPORT_UNSUPPORTED_REASON_UNEXPECTED")
			}
		case CapabilityUndeclared:
			if capability.Declared {
				return errors.New("QUALIFICATION_REPORT_UNDECLARED_STATUS_MISMATCH")
			}
			if capability.UnsupportedReasonCode != "" {
				return errors.New("QUALIFICATION_REPORT_UNSUPPORTED_REASON_UNEXPECTED")
			}
		case CapabilityUnsupported:
			if _, ok := allowedUnsupportedReasons[capability.UnsupportedReasonCode]; !ok {
				return errors.New("QUALIFICATION_REPORT_UNSUPPORTED_REASON_UNKNOWN")
			}
			if unsupported[capability.ID] != capability.UnsupportedReasonCode {
				return errors.New("QUALIFICATION_REPORT_UNSUPPORTED_DECLARATION_MISMATCH")
			}
		default:
			return errors.New("QUALIFICATION_REPORT_CAPABILITY_STATUS_UNKNOWN")
		}
	}
	for capabilityID := range unsupported {
		if _, ok := seenCapabilities[capabilityID]; !ok {
			return errors.New("QUALIFICATION_REPORT_UNSUPPORTED_CAPABILITY_UNKNOWN")
		}
	}
	seenDigests := make(map[string]struct{}, len(report.ConformanceReportDigests))
	for _, digest := range report.ConformanceReportDigests {
		if digest == "" {
			return errors.New("QUALIFICATION_REPORT_CONFORMANCE_DIGEST_REQUIRED")
		}
		if _, ok := seenDigests[digest]; ok {
			return errors.New("QUALIFICATION_REPORT_CONFORMANCE_DIGEST_DUPLICATE")
		}
		seenDigests[digest] = struct{}{}
	}
	sealed, err := report.Seal()
	if err != nil {
		return err
	}
	if sealed.Digest != report.Digest {
		return errors.New("QUALIFICATION_REPORT_DIGEST_MISMATCH")
	}
	return nil
}

func (report *QualificationReport) recount() {
	report.Summary = QualificationSummary{Total: len(report.Capabilities)}
	report.Qualified = true
	for _, capability := range report.Capabilities {
		if capability.Required {
			report.Summary.Required++
			if capability.Status != CapabilityValidated {
				report.Qualified = false
			}
		}
		switch capability.Status {
		case CapabilityValidated:
			report.Summary.Validated++
		case CapabilityUnsupported:
			report.Summary.Unsupported++
		case CapabilityUndeclared:
			report.Summary.Undeclared++
		case CapabilityUnvalidated:
			report.Summary.Unvalidated++
		case CapabilityFailed:
			report.Summary.Failed++
		}
	}
}

func normalizeProfile(profile QualificationProfile) QualificationProfile {
	profile.Capabilities = append([]CapabilityRequirement(nil), profile.Capabilities...)
	for index := range profile.Capabilities {
		capability := &profile.Capabilities[index]
		capability.Manifest.Actions = append([]control.ActionKind(nil), capability.Manifest.Actions...)
		sort.Slice(capability.Manifest.Actions, func(i, j int) bool {
			return capability.Manifest.Actions[i] < capability.Manifest.Actions[j]
		})
		capability.Manifest.Items = append([]control.ItemKind(nil), capability.Manifest.Items...)
		sort.Slice(capability.Manifest.Items, func(i, j int) bool {
			return capability.Manifest.Items[i] < capability.Manifest.Items[j]
		})
		capability.Manifest.TemporalKinds = append([]control.TemporalKind(nil), capability.Manifest.TemporalKinds...)
		sort.Slice(capability.Manifest.TemporalKinds, func(i, j int) bool {
			return capability.Manifest.TemporalKinds[i] < capability.Manifest.TemporalKinds[j]
		})
		capability.Manifest.AnyTemporalKinds = append([]control.TemporalKind(nil), capability.Manifest.AnyTemporalKinds...)
		sort.Slice(capability.Manifest.AnyTemporalKinds, func(i, j int) bool {
			return capability.Manifest.AnyTemporalKinds[i] < capability.Manifest.AnyTemporalKinds[j]
		})
		capability.Manifest.CrashModes = append([]string(nil), capability.Manifest.CrashModes...)
		sort.Strings(capability.Manifest.CrashModes)
		capability.ConformanceCases = append([]string(nil), capability.ConformanceCases...)
		sort.Strings(capability.ConformanceCases)
	}
	sort.Slice(profile.Capabilities, func(i, j int) bool {
		return profile.Capabilities[i].ID < profile.Capabilities[j].ID
	})
	return profile
}

func sortFindings(findings []QualificationFinding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Subject < findings[j].Subject
	})
}

func uniqueNonEmpty(values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return errors.New("QUALIFICATION_VALUE_REQUIRED")
		}
		if _, ok := seen[value]; ok {
			return errors.New("QUALIFICATION_VALUE_DUPLICATE")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func actionStrings(values []control.ActionKind) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func itemStrings(values []control.ItemKind) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func temporalStrings(values []control.TemporalKind) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func joinTemporalKinds(values []control.TemporalKind) string {
	normalized := temporalStrings(values)
	sort.Strings(normalized)
	result := ""
	for index, value := range normalized {
		if index > 0 {
			result += ","
		}
		result += value
	}
	return result
}

package defectbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	CandidateCatalogVersion    = 1
	CapabilitySnapshotVersion  = 2
	QualificationReportVersion = 2

	QualificationQualified = "qualified"
	QualificationDeferred  = "deferred"

	RequirementControllableInput = "controllable-input"
	RequirementObservableEvent   = "observable-event"
	RequirementDriverCapability  = "driver-capability"
	RequirementTrustedMonitor    = "trusted-monitor"
	RequirementExecutionOutcome  = "execution-outcome"
	RequirementProfileBound      = "profile-bound"
)

// CandidateCatalog contains curator facts and typed requirements only. In
// particular, Candidate has no qualification status or reason field.
type CandidateCatalog struct {
	Version    int         `json:"version"`
	ID         string      `json:"id"`
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	ID              string                `json:"id"`
	Protocol        string                `json:"protocol"`
	Family          string                `json:"family"`
	Provenance      CandidateProvenance   `json:"provenance"`
	SourceReference CandidateSource       `json:"source_reference"`
	RootCauseGroup  string                `json:"root_cause_group"`
	Requirements    CandidateRequirements `json:"requirements"`
}

type CandidateProvenance struct {
	Kind       string `json:"kind"`
	Repository string `json:"repository"`
}

type CandidateSource struct {
	Revision string `json:"revision"`
	URL      string `json:"url"`
}

type CandidateRequirements struct {
	ControllableInputs []string `json:"controllable_inputs"`
	ObservableEvents   []string `json:"observable_events"`
	DriverCapabilities []string `json:"driver_capabilities"`
	TrustedMonitors    []string `json:"trusted_monitors"`
	ExecutionOutcomes  []string `json:"execution_outcomes"`
	ProfileBounds      []string `json:"profile_bounds"`
}

// CapabilitySnapshot is trusted evaluator input assembled by a concrete
// composition layer. Profile obligations may contribute only to
// ObservableEvents and ProfileBounds; they cannot manufacture a controllable
// input.
type CapabilitySnapshot struct {
	Version            int      `json:"version"`
	ID                 string   `json:"id"`
	Protocol           string   `json:"protocol"`
	Family             string   `json:"family"`
	ProfileID          string   `json:"profile_id"`
	ProfileDigest      string   `json:"profile_digest"`
	DriverManifestHash string   `json:"driver_manifest_digest"`
	ControllableInputs []string `json:"controllable_inputs"`
	ObservableEvents   []string `json:"observable_events"`
	DriverCapabilities []string `json:"driver_capabilities"`
	TrustedMonitors    []string `json:"trusted_monitors"`
	ExecutionOutcomes  []string `json:"execution_outcomes"`
	ProfileBounds      []string `json:"profile_bounds"`
}

// QualificationReport is the only model that carries qualified/deferred
// state. Every result is a pure typed set-inclusion decision.
type QualificationReport struct {
	Version                  int                   `json:"version"`
	ID                       string                `json:"id"`
	CandidateCatalogDigest   string                `json:"candidate_catalog_digest"`
	CapabilitySnapshotDigest string                `json:"capability_snapshot_digest"`
	Snapshot                 CapabilitySnapshot    `json:"capability_snapshot"`
	Results                  []QualificationResult `json:"results"`
}

type QualificationResult struct {
	CandidateID string               `json:"candidate_id"`
	Status      string               `json:"status"`
	Missing     []MissingRequirement `json:"missing_requirements"`
	ReasonCodes []string             `json:"reason_codes"`
}

type MissingRequirement struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	ReasonCode string `json:"reason_code"`
}

func (catalog CandidateCatalog) Validate() error {
	if catalog.Version != CandidateCatalogVersion || catalog.ID == "" || len(catalog.Candidates) == 0 {
		return errors.New("candidate catalog requires version 1, an id, and candidates")
	}
	seen := make(map[string]bool, len(catalog.Candidates))
	for index, candidate := range catalog.Candidates {
		if candidate.ID == "" || candidate.Protocol == "" || candidate.Family == "" ||
			candidate.Provenance.Kind == "" || candidate.Provenance.Repository == "" ||
			candidate.SourceReference.Revision == "" || candidate.SourceReference.URL == "" ||
			candidate.RootCauseGroup == "" {
			return fmt.Errorf("candidate[%d] is missing identity, provenance, source, or root-cause metadata", index)
		}
		if seen[candidate.ID] {
			return fmt.Errorf("duplicate candidate id %q", candidate.ID)
		}
		seen[candidate.ID] = true
		if err := candidate.Requirements.validate(); err != nil {
			return fmt.Errorf("candidate %s requirements: %w", candidate.ID, err)
		}
	}
	return nil
}

func (requirements CandidateRequirements) validate() error {
	for kind, values := range requirements.sets() {
		if err := validateSet(values); err != nil {
			return fmt.Errorf("%s: %w", kind, err)
		}
	}
	return nil
}

func (snapshot CapabilitySnapshot) Validate() error {
	if snapshot.Version != CapabilitySnapshotVersion || snapshot.ID == "" || snapshot.Protocol == "" || snapshot.Family == "" ||
		snapshot.ProfileID == "" || !candidateDigest(snapshot.ProfileDigest) || !candidateDigest(snapshot.DriverManifestHash) {
		return errors.New("capability snapshot has an invalid version or identity")
	}
	for kind, values := range snapshot.sets() {
		if err := validateSet(values); err != nil {
			return fmt.Errorf("snapshot %s: %w", kind, err)
		}
	}
	return nil
}

func QualifyCandidates(catalog CandidateCatalog, snapshot CapabilitySnapshot) (QualificationReport, error) {
	if err := catalog.Validate(); err != nil {
		return QualificationReport{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return QualificationReport{}, err
	}
	for _, candidate := range catalog.Candidates {
		if candidate.Protocol != snapshot.Protocol {
			return QualificationReport{}, fmt.Errorf("candidate %s protocol %q does not match snapshot protocol %q", candidate.ID, candidate.Protocol, snapshot.Protocol)
		}
		if candidate.Family != snapshot.Family {
			return QualificationReport{}, fmt.Errorf("candidate %s family %q does not match snapshot family %q", candidate.ID, candidate.Family, snapshot.Family)
		}
	}
	canonicalCatalog := canonicalCandidateCatalog(catalog)
	canonicalSnapshot := canonicalCapabilitySnapshot(snapshot)
	catalogDigest, err := candidateDigestJSON(canonicalCatalog)
	if err != nil {
		return QualificationReport{}, err
	}
	snapshotDigest, err := candidateDigestJSON(canonicalSnapshot)
	if err != nil {
		return QualificationReport{}, err
	}
	report := QualificationReport{
		Version: QualificationReportVersion, ID: catalog.ID + "-qualification",
		CandidateCatalogDigest: catalogDigest, CapabilitySnapshotDigest: snapshotDigest,
		Snapshot: canonicalSnapshot,
	}
	available := canonicalSnapshot.sets()
	for _, candidate := range canonicalCatalog.Candidates {
		result := QualificationResult{
			CandidateID: candidate.ID, Status: QualificationQualified,
			Missing: make([]MissingRequirement, 0), ReasonCodes: make([]string, 0),
		}
		for _, kind := range requirementOrder() {
			set := make(map[string]bool, len(available[kind]))
			for _, id := range available[kind] {
				set[id] = true
			}
			required := append([]string(nil), candidate.Requirements.sets()[kind]...)
			sort.Strings(required)
			for _, id := range required {
				if set[id] {
					continue
				}
				reason := "missing." + kind + "." + id
				result.Missing = append(result.Missing, MissingRequirement{Type: kind, ID: id, ReasonCode: reason})
				result.ReasonCodes = append(result.ReasonCodes, reason)
			}
		}
		if len(result.Missing) != 0 {
			result.Status = QualificationDeferred
		}
		report.Results = append(report.Results, result)
	}
	sort.Slice(report.Results, func(i, j int) bool { return report.Results[i].CandidateID < report.Results[j].CandidateID })
	return report, nil
}

func canonicalCandidateCatalog(catalog CandidateCatalog) CandidateCatalog {
	result := catalog
	result.Candidates = append([]Candidate(nil), catalog.Candidates...)
	for index := range result.Candidates {
		result.Candidates[index].Requirements = canonicalRequirements(result.Candidates[index].Requirements)
	}
	sort.Slice(result.Candidates, func(i, j int) bool { return result.Candidates[i].ID < result.Candidates[j].ID })
	return result
}

func canonicalCapabilitySnapshot(snapshot CapabilitySnapshot) CapabilitySnapshot {
	result := snapshot
	result.ControllableInputs = sortedCopy(snapshot.ControllableInputs)
	result.ObservableEvents = sortedCopy(snapshot.ObservableEvents)
	result.DriverCapabilities = sortedCopy(snapshot.DriverCapabilities)
	result.TrustedMonitors = sortedCopy(snapshot.TrustedMonitors)
	result.ExecutionOutcomes = sortedCopy(snapshot.ExecutionOutcomes)
	result.ProfileBounds = sortedCopy(snapshot.ProfileBounds)
	return result
}

func canonicalRequirements(requirements CandidateRequirements) CandidateRequirements {
	result := requirements
	result.ControllableInputs = sortedCopy(requirements.ControllableInputs)
	result.ObservableEvents = sortedCopy(requirements.ObservableEvents)
	result.DriverCapabilities = sortedCopy(requirements.DriverCapabilities)
	result.TrustedMonitors = sortedCopy(requirements.TrustedMonitors)
	result.ExecutionOutcomes = sortedCopy(requirements.ExecutionOutcomes)
	result.ProfileBounds = sortedCopy(requirements.ProfileBounds)
	return result
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func (requirements CandidateRequirements) sets() map[string][]string {
	return map[string][]string{
		RequirementControllableInput: requirements.ControllableInputs,
		RequirementObservableEvent:   requirements.ObservableEvents,
		RequirementDriverCapability:  requirements.DriverCapabilities,
		RequirementTrustedMonitor:    requirements.TrustedMonitors,
		RequirementExecutionOutcome:  requirements.ExecutionOutcomes,
		RequirementProfileBound:      requirements.ProfileBounds,
	}
}

func (snapshot CapabilitySnapshot) sets() map[string][]string {
	return map[string][]string{
		RequirementControllableInput: snapshot.ControllableInputs,
		RequirementObservableEvent:   snapshot.ObservableEvents,
		RequirementDriverCapability:  snapshot.DriverCapabilities,
		RequirementTrustedMonitor:    snapshot.TrustedMonitors,
		RequirementExecutionOutcome:  snapshot.ExecutionOutcomes,
		RequirementProfileBound:      snapshot.ProfileBounds,
	}
}

func requirementOrder() []string {
	return []string{
		RequirementControllableInput,
		RequirementObservableEvent,
		RequirementDriverCapability,
		RequirementTrustedMonitor,
		RequirementExecutionOutcome,
		RequirementProfileBound,
	}
}

func validateSet(values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			return errors.New("set members must be non-empty and unique")
		}
		seen[value] = true
	}
	return nil
}

func candidateDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func candidateDigestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// Package coverage owns the trusted, fixed-denominator coverage model.
// Agents may target obligations and propose tests, but only this package may
// turn execution evidence into covered ledger entries and scores.
package coverage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

const (
	LegacyProfileVersion = 1
	ProfileVersion       = 2

	StatusSupported   = "supported"
	StatusUnsupported = "unsupported"

	RiskLow      = "low"
	RiskStandard = "standard"
	RiskHigh     = "high"
	RiskCritical = "critical"
)

type Profile struct {
	Version  int    `json:"version"`
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	PSSID    string `json:"pss_id,omitempty"`
	// RuntimeProfile is a curator-selected, registry-resolved execution
	// binding. It is part of the Profile digest so an evaluator never relies on
	// an environment variable or an unrecorded Driver switch. An empty value is
	// the binding's conservative default and keeps existing frozen profiles
	// byte-for-byte and digest-for-digest compatible.
	RuntimeProfile       string             `json:"runtime_profile,omitempty"`
	Nodes                []string           `json:"nodes"`
	RequiredCapabilities []string           `json:"required_capabilities,omitempty"`
	Coverage             CoverageDefinition `json:"coverage"`
	Threshold            Threshold          `json:"threshold"`
}

// CoverageDefinition supports legacy v1 profiles while v2 is rolled out.
// A profile must use exactly one denominator field. New code must emit
// Obligations; Atoms exists only so historical profiles remain replayable.
type CoverageDefinition struct {
	Weights     map[string]float64 `json:"weights"`
	Obligations []Obligation       `json:"obligations,omitempty"`
	Atoms       []Obligation       `json:"atoms,omitempty"`
}

type Threshold struct {
	Score       float64 `json:"score"`
	MinCategory float64 `json:"min_category"`
}

// Obligation is one immutable member of the fixed coverage denominator.
// WitnessLabel is accepted only for legacy v1 profiles. V2 profiles use the
// structured Evidence specification, which prevents an Agent from satisfying
// an event or ordering requirement merely by inventing a similarly named goal.
type Obligation struct {
	ID           string              `json:"id"`
	Category     string              `json:"category"`
	Description  string              `json:"description"`
	Evidence     EvidenceRequirement `json:"evidence,omitempty"`
	Monitors     []string            `json:"monitors,omitempty"`
	Requires     []string            `json:"requires,omitempty"`
	Weight       float64             `json:"weight,omitempty"`
	Risk         string              `json:"risk,omitempty"`
	Status       string              `json:"status"`
	WitnessLabel string              `json:"witness_label,omitempty"`
	Monitor      string              `json:"monitor,omitempty"`
}

// Atom remains a source-compatible name for code constructing a legacy v1
// profile. It is deliberately an alias, not a second coverage representation.
type Atom = Obligation

// EvidenceRequirement describes trace facts that the trusted matcher must
// find. All Reach and Observe predicates must match, and every Ordering must
// have a matching before step strictly earlier than a matching after step.
type EvidenceRequirement struct {
	Reach     []TracePredicate     `json:"reach"`
	Observe   []TracePredicate     `json:"observe"`
	Orderings []OrderingConstraint `json:"orderings,omitempty"`
	Counts    []CountConstraint    `json:"counts,omitempty"`
}

// TracePredicate is intentionally protocol-neutral and does not expose raw
// event IDs. Empty fields are wildcards; at least one event or observation
// selector must be present.
type TracePredicate struct {
	EventKind           core.EventKind     `json:"event_kind,omitempty"`
	EventSource         string             `json:"event_source,omitempty"`
	EventTarget         string             `json:"event_target,omitempty"`
	MessageType         string             `json:"message_type,omitempty"`
	Outcome             string             `json:"outcome,omitempty"`
	HostCutpoint        string             `json:"host_cutpoint,omitempty"`
	ObservationKind     string             `json:"observation_kind,omitempty"`
	ObservationLabel    string             `json:"observation_label,omitempty"`
	ObservationNode     string             `json:"observation_node,omitempty"`
	ObservationValue    string             `json:"observation_value,omitempty"`
	ObservationEvidence map[string]string  `json:"observation_evidence,omitempty"`
	Semantic            *SemanticPredicate `json:"semantic,omitempty"`
}

type OrderingConstraint struct {
	Before      TracePredicate   `json:"before"`
	After       TracePredicate   `json:"after"`
	Without     []TracePredicate `json:"without,omitempty"`
	SameGroup   bool             `json:"same_group,omitempty"`
	SameTarget  bool             `json:"same_target,omitempty"`
	SameMessage bool             `json:"same_message,omitempty"`
}

type CountConstraint struct {
	Predicate  TracePredicate `json:"predicate"`
	AtLeast    int            `json:"at_least"`
	AtMost     int            `json:"at_most,omitempty"`
	DistinctBy string         `json:"distinct_by,omitempty"`
}

type SemanticPredicate struct {
	Domain     string            `json:"domain"`
	Relation   string            `json:"relation"`
	Value      string            `json:"value,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// SemanticMatcher is a versioned Family Pack plugin. Generic coverage code
// owns evidence composition; the plugin may only answer whether a frozen
// semantic relation holds at one trace record.
type SemanticMatcher interface {
	ID() string
	Match(SemanticPredicate, core.TraceRecord) bool
}

type Evidence struct {
	Reach   bool `json:"reach"`
	Observe bool `json:"observe"`
	Check   bool `json:"check"`
	Replay  bool `json:"replay"`
	Conform bool `json:"conform"`
}

func (e Evidence) Strong() bool {
	return e.Reach && e.Observe && e.Check && e.Replay && e.Conform
}

type StepPair struct {
	Before int `json:"before"`
	After  int `json:"after"`
}

type EvidenceReference struct {
	RunID            string             `json:"run_id"`
	Scenario         string             `json:"scenario,omitempty"`
	Artifact         string             `json:"artifact,omitempty"`
	TraceDigest      string             `json:"trace_digest"`
	ReachSteps       []int              `json:"reach_steps,omitempty"`
	ObserveSteps     []int              `json:"observe_steps,omitempty"`
	OrderingSteps    []StepPair         `json:"ordering_steps,omitempty"`
	CountResults     []int              `json:"count_results,omitempty"`
	CheckedMonitors  []string           `json:"checked_monitors,omitempty"`
	OracleViolations []oracle.Violation `json:"oracle_violations,omitempty"`
	Evidence         Evidence           `json:"evidence"`
}

type ObligationResult struct {
	Obligation Obligation        `json:"obligation"`
	Evidence   Evidence          `json:"evidence"`
	Covered    bool              `json:"covered"`
	Reference  EvidenceReference `json:"reference"`
}

// AtomResult is retained as a Go alias for callers compiled against the v1
// name. JSON reports use the v2 "obligations" field.
type AtomResult = ObligationResult

type CategoryResult struct {
	Category      string  `json:"category"`
	Covered       int     `json:"covered"`
	Total         int     `json:"total"`
	CoveredWeight float64 `json:"covered_weight"`
	TotalWeight   float64 `json:"total_weight"`
	Ratio         float64 `json:"ratio"`
	Weight        float64 `json:"weight"`
}

type Summary struct {
	ProfileID         string             `json:"profile_id"`
	ProfileDigest     string             `json:"profile_digest"`
	Score             float64            `json:"score"`
	High              bool               `json:"high"`
	Covered           int                `json:"covered"`
	Total             int                `json:"total"`
	Unsupported       int                `json:"unsupported"`
	CapabilitySupport float64            `json:"capability_support"`
	Capabilities      []CapabilityResult `json:"capabilities,omitempty"`
	Categories        []CategoryResult   `json:"categories"`
	Obligations       []ObligationResult `json:"obligations"`
}

type CapabilityResult struct {
	ID        string `json:"id"`
	Supported bool   `json:"supported"`
	Detail    string `json:"detail,omitempty"`
}

func (p Profile) Validate() error {
	if p.Version != LegacyProfileVersion && p.Version != ProfileVersion {
		return fmt.Errorf("unsupported profile version %d", p.Version)
	}
	if p.ID == "" || p.Protocol == "" || len(p.Nodes) == 0 {
		return errors.New("profile id, protocol, and nodes are required")
	}
	if !uniqueNonEmpty(p.Nodes) {
		return errors.New("profile nodes must be non-empty and unique")
	}
	if p.RuntimeProfile != "" && !validRuntimeProfile(p.RuntimeProfile) {
		return fmt.Errorf("runtime profile %q is not a stable identifier", p.RuntimeProfile)
	}
	weightSum := 0.0
	for _, category := range sortedCategories(p.Coverage.Weights) {
		weight := p.Coverage.Weights[category]
		if category == "" || weight < 0 {
			return errors.New("coverage categories must be named and weights cannot be negative")
		}
		weightSum += weight
	}
	if weightSum < 0.999 || weightSum > 1.001 {
		return fmt.Errorf("coverage weights must sum to 1, got %.4f", weightSum)
	}
	if p.Threshold.Score < 0 || p.Threshold.Score > 100 ||
		p.Threshold.MinCategory < 0 || p.Threshold.MinCategory > 1 {
		return errors.New("coverage threshold is outside its valid range")
	}

	items, err := p.denominator()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("coverage denominator cannot be empty")
	}
	seen := make(map[string]bool)
	for _, capability := range p.RequiredCapabilities {
		if capability == "" {
			return errors.New("required capability IDs cannot be empty")
		}
		if seen["capability:"+capability] {
			return fmt.Errorf("duplicate required capability %s", capability)
		}
		seen["capability:"+capability] = true
	}
	for index, obligation := range items {
		if err := validateObligation(p.Version, obligation); err != nil {
			return fmt.Errorf("coverage obligation[%d]: %w", index, err)
		}
		if seen[obligation.ID] {
			return fmt.Errorf("duplicate coverage obligation %s", obligation.ID)
		}
		seen[obligation.ID] = true
		categoryWeight, ok := p.Coverage.Weights[obligation.Category]
		if !ok {
			return fmt.Errorf("obligation %s uses category without weight: %s", obligation.ID, obligation.Category)
		}
		if categoryWeight <= 0 {
			return fmt.Errorf("obligation %s uses category with non-positive weight: %s", obligation.ID, obligation.Category)
		}
	}
	return nil
}

func validRuntimeProfile(value string) bool {
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			if index != 0 || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				continue
			}
		}
		return false
	}
	return value != ""
}

// Digest is the immutable identity of the complete denominator and its scope.
func Digest(profile Profile) (string, error) {
	if err := profile.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// Obligations returns a defensive copy of the active fixed denominator.
func (p Profile) Obligations() ([]Obligation, error) {
	items, err := p.denominator()
	if err != nil {
		return nil, err
	}
	return append([]Obligation(nil), items...), nil
}

func (p Profile) denominator() ([]Obligation, error) {
	switch p.Version {
	case LegacyProfileVersion:
		if len(p.Coverage.Obligations) != 0 {
			return nil, errors.New("legacy profile v1 must use coverage.atoms")
		}
		return p.Coverage.Atoms, nil
	case ProfileVersion:
		if len(p.Coverage.Atoms) != 0 {
			return nil, errors.New("profile v2 must use coverage.obligations")
		}
		return p.Coverage.Obligations, nil
	default:
		return nil, fmt.Errorf("unsupported profile version %d", p.Version)
	}
}

func validateObligation(version int, obligation Obligation) error {
	if obligation.ID == "" || obligation.Category == "" || obligation.Description == "" {
		return errors.New("id, category, and description are required")
	}
	if obligation.Status != StatusSupported && obligation.Status != StatusUnsupported {
		return fmt.Errorf("%s has invalid status %q", obligation.ID, obligation.Status)
	}
	if obligation.Weight < 0 {
		return fmt.Errorf("%s has a negative weight", obligation.ID)
	}
	if !uniqueNonEmpty(obligation.Requires) || !uniqueNonEmpty(obligation.Monitors) {
		return fmt.Errorf("%s has empty or duplicate requirements/monitors", obligation.ID)
	}
	if obligation.Risk != "" && obligation.Risk != RiskLow && obligation.Risk != RiskStandard &&
		obligation.Risk != RiskHigh && obligation.Risk != RiskCritical {
		return fmt.Errorf("%s has invalid risk %q", obligation.ID, obligation.Risk)
	}
	if version == LegacyProfileVersion {
		if obligation.WitnessLabel == "" {
			return fmt.Errorf("legacy obligation %s requires witness_label", obligation.ID)
		}
		return nil
	}
	if obligation.WitnessLabel != "" || obligation.Monitor != "" {
		return fmt.Errorf("v2 obligation %s cannot use legacy witness_label/monitor", obligation.ID)
	}
	if len(obligation.Monitors) == 0 {
		return fmt.Errorf("v2 obligation %s requires at least one monitor", obligation.ID)
	}
	if len(obligation.Evidence.Reach) == 0 || len(obligation.Evidence.Observe) == 0 {
		return fmt.Errorf("v2 obligation %s requires explicit reach and observe predicates", obligation.ID)
	}
	for _, predicate := range append(append([]TracePredicate(nil), obligation.Evidence.Reach...), obligation.Evidence.Observe...) {
		if err := validatePredicate(predicate); err != nil {
			return fmt.Errorf("v2 obligation %s: %w", obligation.ID, err)
		}
	}
	for _, ordering := range obligation.Evidence.Orderings {
		if err := validatePredicate(ordering.Before); err != nil {
			return fmt.Errorf("v2 obligation %s ordering before: %w", obligation.ID, err)
		}
		if err := validatePredicate(ordering.After); err != nil {
			return fmt.Errorf("v2 obligation %s ordering after: %w", obligation.ID, err)
		}
		for _, excluded := range ordering.Without {
			if err := validatePredicate(excluded); err != nil {
				return fmt.Errorf("v2 obligation %s ordering without: %w", obligation.ID, err)
			}
		}
	}
	for _, count := range obligation.Evidence.Counts {
		if err := validatePredicate(count.Predicate); err != nil {
			return fmt.Errorf("v2 obligation %s count: %w", obligation.ID, err)
		}
		if count.AtLeast < 1 || count.AtMost < 0 || (count.AtMost > 0 && count.AtMost < count.AtLeast) {
			return fmt.Errorf("v2 obligation %s has invalid count bounds", obligation.ID)
		}
		if !validDistinctBy(count.DistinctBy) {
			return fmt.Errorf("v2 obligation %s has invalid distinct_by %q", obligation.ID, count.DistinctBy)
		}
	}
	return nil
}

func validatePredicate(predicate TracePredicate) error {
	if predicate.EventKind == "" && predicate.EventSource == "" && predicate.EventTarget == "" &&
		predicate.MessageType == "" && predicate.Outcome == "" && predicate.HostCutpoint == "" &&
		predicate.ObservationKind == "" && predicate.ObservationLabel == "" &&
		predicate.ObservationNode == "" && predicate.ObservationValue == "" && len(predicate.ObservationEvidence) == 0 {
		if predicate.Semantic == nil {
			return errors.New("trace predicate cannot be empty")
		}
	}
	if predicate.EventKind != "" && !validEventKind(predicate.EventKind) {
		return fmt.Errorf("trace predicate has invalid event kind %q", predicate.EventKind)
	}
	for key := range predicate.ObservationEvidence {
		if key == "" {
			return errors.New("observation evidence keys cannot be empty")
		}
	}
	if predicate.HostCutpoint != "" && !validHostCutpoint(predicate.HostCutpoint) {
		return fmt.Errorf("trace predicate has invalid host cutpoint %q", predicate.HostCutpoint)
	}
	if predicate.Semantic != nil {
		if predicate.Semantic.Domain == "" || predicate.Semantic.Relation == "" {
			return errors.New("semantic predicate requires domain and relation")
		}
		for key := range predicate.Semantic.Parameters {
			if key == "" {
				return errors.New("semantic predicate parameter keys cannot be empty")
			}
		}
	}
	return nil
}

func validDistinctBy(value string) bool {
	if value == "" || value == "event_source" || value == "event_target" || value == "observation_node" {
		return true
	}
	const prefix = "observation_evidence:"
	return len(value) > len(prefix) && value[:len(prefix)] == prefix
}

func validHostCutpoint(value string) bool {
	switch value {
	case "before-persist", "persist-before-sync", "sync-before-release", "release-before-ack", "apply-before-ack":
		return true
	default:
		return false
	}
}

func obligationWeight(obligation Obligation) float64 {
	if obligation.Weight == 0 {
		return 1
	}
	return obligation.Weight
}

func obligationMonitors(version int, obligation Obligation) []string {
	if version == LegacyProfileVersion {
		if obligation.Monitor == "" {
			return nil
		}
		return []string{obligation.Monitor}
	}
	return obligation.Monitors
}

func uniqueNonEmpty(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validEventKind(kind core.EventKind) bool {
	switch kind {
	case core.EventStart, core.EventCampaign, core.EventPropose, core.EventQuery, core.EventMessage,
		core.EventTimeout, core.EventPersist, core.EventSync, core.EventEmit,
		core.EventApply, core.EventAcknowledge, core.EventCrash, core.EventRestart,
		core.EventDuplicate, core.EventPartition, core.EventHeal, core.EventClockAdvance:
		return true
	default:
		return false
	}
}

func Evaluate(profile Profile, trace []core.TraceRecord, checked oracle.Result, replay, conform bool) Summary {
	return EvaluateWithCapabilities(profile, trace, checked, replay, conform, driver.Manifest{})
}

func EvaluateWithCapabilities(
	profile Profile,
	trace []core.TraceRecord,
	checked oracle.Result,
	replay, conform bool,
	manifest driver.Manifest,
) Summary {
	return EvaluateWithSemanticMatcher(profile, trace, checked, replay, conform, manifest, nil)
}

func EvaluateWithSemanticMatcher(
	profile Profile,
	trace []core.TraceRecord,
	checked oracle.Result,
	replay, conform bool,
	manifest driver.Manifest,
	matcher SemanticMatcher,
) Summary {
	digest, err := Digest(profile)
	if err != nil {
		return Summary{}
	}
	items, _ := profile.denominator()
	results := make([]ObligationResult, 0, len(items))
	traceDigest, _ := core.CanonicalTraceDigest(trace)
	for _, obligation := range items {
		matched := matchEvidence(profile.Version, obligation, trace, matcher)
		monitorChecked := monitorsChecked(obligationMonitors(profile.Version, obligation), checked.Checked)
		evidence := Evidence{
			Reach: matched.Reach, Observe: matched.Observe, Check: monitorChecked,
			Replay: replay, Conform: conform,
		}
		results = append(results, ObligationResult{
			Obligation: obligation,
			Evidence:   evidence,
			Covered:    obligation.Status == StatusSupported && evidence.Strong(),
			Reference: EvidenceReference{
				TraceDigest: traceDigest, ReachSteps: matched.ReachSteps,
				ObserveSteps: matched.ObserveSteps, OrderingSteps: matched.OrderingSteps,
				CountResults:     matched.CountResults,
				CheckedMonitors:  append([]string(nil), checked.Checked...),
				OracleViolations: append([]oracle.Violation(nil), checked.Violations...), Evidence: evidence,
			},
		})
	}
	return summarize(profile, digest, results, manifest)
}

func summarize(profile Profile, profileDigest string, results []ObligationResult, manifest driver.Manifest) Summary {
	counts := make(map[string]*CategoryResult)
	capabilitySupport, capabilityResults := capabilitySupport(profile.RequiredCapabilities, manifest)
	summary := Summary{
		ProfileID: profile.ID, ProfileDigest: profileDigest, Total: len(results),
		CapabilitySupport: capabilitySupport, Capabilities: capabilityResults,
		Obligations: append([]ObligationResult(nil), results...),
	}
	for _, result := range results {
		obligation := result.Obligation
		category := counts[obligation.Category]
		if category == nil {
			category = &CategoryResult{Category: obligation.Category, Weight: profile.Coverage.Weights[obligation.Category]}
			counts[obligation.Category] = category
		}
		weight := obligationWeight(obligation)
		category.Total++
		category.TotalWeight += weight
		if obligation.Status == StatusUnsupported {
			summary.Unsupported++
		}
		if result.Covered {
			summary.Covered++
			category.Covered++
			category.CoveredWeight += weight
		}
	}

	minimum := 1.0
	for _, name := range sortedCategories(counts) {
		category := counts[name]
		if category.TotalWeight > 0 {
			category.Ratio = category.CoveredWeight / category.TotalWeight
		}
		summary.Score += 100 * category.Weight * category.Ratio
		if category.Ratio < minimum {
			minimum = category.Ratio
		}
		summary.Categories = append(summary.Categories, *category)
	}
	sort.Slice(summary.Categories, func(i, j int) bool {
		return summary.Categories[i].Category < summary.Categories[j].Category
	})
	summary.High = summary.Score >= profile.Threshold.Score && minimum >= profile.Threshold.MinCategory &&
		summary.CapabilitySupport >= 0.90
	return summary
}

func sortedCategories[T any](categories map[string]T) []string {
	result := make([]string, 0, len(categories))
	for category := range categories {
		result = append(result, category)
	}
	sort.Strings(result)
	return result
}

func capabilitySupport(required []string, manifest driver.Manifest) (float64, []CapabilityResult) {
	if len(required) == 0 {
		return 1, nil
	}
	available := make(map[string]driver.Capability, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		available[capability.ID] = capability
	}
	supported := 0
	results := make([]CapabilityResult, 0, len(required))
	for _, id := range required {
		capability, exists := available[id]
		result := CapabilityResult{ID: id}
		if exists {
			result.Supported = capability.Supported
			result.Detail = capability.Detail
		}
		if result.Supported {
			supported++
		}
		results = append(results, result)
	}
	return float64(supported) / float64(len(required)), results
}

func monitorsChecked(required, checked []string) bool {
	available := make(map[string]bool, len(checked))
	for _, monitor := range checked {
		available[monitor] = true
	}
	for _, monitor := range required {
		if !available[monitor] {
			return false
		}
	}
	return true
}

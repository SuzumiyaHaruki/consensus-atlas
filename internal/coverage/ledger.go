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
	LedgerVersion = 1

	LedgerUncovered   = "uncovered"
	LedgerAttempted   = "attempted"
	LedgerCovered     = "covered"
	LedgerUnsupported = "unsupported"
)

// RunEvidence is the only mutation input accepted by a Ledger. Targets are
// Agent intent, never proof: a run can cover an untargeted obligation when its
// trace supplies strong evidence, but merely targeting an obligation cannot
// cover it.
type RunEvidence struct {
	ID           string
	Scenario     string
	Artifact     string
	Targets      []string
	Trace        []core.TraceRecord
	Oracle       oracle.Result
	ReplayStable bool
	Conformant   bool
	Manifest     driver.Manifest
}

type RunReference struct {
	ID           string   `json:"id"`
	Scenario     string   `json:"scenario,omitempty"`
	Artifact     string   `json:"artifact,omitempty"`
	Targets      []string `json:"targets,omitempty"`
	TraceDigest  string   `json:"trace_digest"`
	ReplayStable bool     `json:"replay_stable"`
	Conformant   bool     `json:"conformant"`
}

type LedgerEntry struct {
	ObligationID string             `json:"obligation_id"`
	Category     string             `json:"category"`
	Description  string             `json:"description"`
	Risk         string             `json:"risk"`
	Status       string             `json:"status"`
	Attempts     int                `json:"attempts"`
	CoveredBy    *EvidenceReference `json:"covered_by,omitempty"`
	LastEvidence *EvidenceReference `json:"last_evidence,omitempty"`
}

type Debt struct {
	Obligation   Obligation         `json:"obligation"`
	Status       string             `json:"status"`
	Attempts     int                `json:"attempts"`
	LastEvidence *EvidenceReference `json:"last_evidence,omitempty"`
}

type LedgerReport struct {
	Version        int            `json:"version"`
	ProfileID      string         `json:"profile_id"`
	ProfileDigest  string         `json:"profile_digest"`
	ManifestDigest string         `json:"manifest_digest"`
	Runs           []RunReference `json:"runs"`
	Entries        []LedgerEntry  `json:"entries"`
	Summary        Summary        `json:"summary"`
}

// Ledger keeps all mutable campaign state private. Agent-facing code receives
// only Report and Debts snapshots; the only state transition is AddRun.
type Ledger struct {
	version        int
	profileID      string
	profileDigest  string
	manifestDigest string
	runs           []RunReference
	entries        []LedgerEntry
	summary        Summary
	profile        Profile
	manifest       driver.Manifest
	matcher        SemanticMatcher
	runIDs         map[string]bool
}

type LedgerOption func(*ledgerOptions) error

type ledgerOptions struct {
	matcher SemanticMatcher
}

func WithSemanticMatcher(matcher SemanticMatcher) LedgerOption {
	return func(options *ledgerOptions) error {
		if matcher == nil || matcher.ID() == "" {
			return errors.New("semantic matcher requires a non-empty identity")
		}
		options.matcher = matcher
		return nil
	}
}

func NewLedger(profile Profile, manifest driver.Manifest, options ...LedgerOption) (*Ledger, error) {
	digest, err := Digest(profile)
	if err != nil {
		return nil, err
	}
	configured := ledgerOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("coverage ledger option is nil")
		}
		if err := option(&configured); err != nil {
			return nil, err
		}
	}
	if err := validateSemanticMatcher(profile, configured.matcher); err != nil {
		return nil, err
	}
	manifestDigest, err := digestManifest(manifest)
	if err != nil {
		return nil, err
	}
	frozenProfile, err := cloneProfile(profile)
	if err != nil {
		return nil, err
	}
	frozenManifest, err := cloneManifest(manifest)
	if err != nil {
		return nil, err
	}
	ledger := &Ledger{
		version: LedgerVersion, profileID: profile.ID, profileDigest: digest,
		manifestDigest: manifestDigest, profile: frozenProfile, manifest: frozenManifest,
		matcher: configured.matcher, runIDs: make(map[string]bool),
	}
	items, _ := frozenProfile.denominator()
	results := make([]ObligationResult, 0, len(items))
	for _, obligation := range items {
		status := LedgerUncovered
		if obligation.Status == StatusUnsupported {
			status = LedgerUnsupported
		}
		ledger.entries = append(ledger.entries, LedgerEntry{
			ObligationID: obligation.ID, Category: obligation.Category, Description: obligation.Description,
			Risk: normalizedRisk(obligation.Risk), Status: status,
		})
		results = append(results, ObligationResult{Obligation: obligation})
	}
	ledger.summary = summarize(frozenProfile, digest, results, frozenManifest)
	return ledger, nil
}

func (ledger *Ledger) AddRun(profile Profile, run RunEvidence) error {
	if ledger == nil {
		return errors.New("coverage ledger is nil")
	}
	if run.ID == "" {
		return errors.New("coverage run ID is required")
	}
	if ledger.runIDs == nil {
		return errors.New("coverage ledger must be created with NewLedger")
	}
	if ledger.runIDs[run.ID] {
		return fmt.Errorf("duplicate coverage run %q", run.ID)
	}
	digest, err := Digest(profile)
	if err != nil {
		return err
	}
	if digest != ledger.profileDigest || profile.ID != ledger.profileID {
		return fmt.Errorf("coverage run profile mismatch: got %s/%s, want %s/%s",
			profile.ID, digest, ledger.profileID, ledger.profileDigest)
	}
	runManifestDigest, err := digestManifest(run.Manifest)
	if err != nil {
		return err
	}
	if runManifestDigest != ledger.manifestDigest {
		return fmt.Errorf("coverage run %q manifest mismatch: got %s, want %s",
			run.ID, runManifestDigest, ledger.manifestDigest)
	}
	items, _ := profile.denominator()
	byID := make(map[string]int, len(items))
	for index, obligation := range items {
		byID[obligation.ID] = index
	}
	targeted := make(map[string]bool, len(run.Targets))
	for _, target := range run.Targets {
		if _, ok := byID[target]; !ok {
			return fmt.Errorf("coverage run %q targets unknown obligation %q", run.ID, target)
		}
		if targeted[target] {
			return fmt.Errorf("coverage run %q repeats target %q", run.ID, target)
		}
		targeted[target] = true
	}

	evaluated := EvaluateWithSemanticMatcher(
		profile, run.Trace, run.Oracle, run.ReplayStable, run.Conformant, ledger.manifest, ledger.matcher,
	)
	if len(evaluated.Obligations) != len(ledger.entries) {
		return errors.New("coverage evaluator changed the frozen denominator")
	}
	traceDigest := digestTrace(run.Trace)
	ledger.runs = append(ledger.runs, RunReference{
		ID: run.ID, Scenario: run.Scenario, Artifact: run.Artifact,
		Targets: append([]string(nil), run.Targets...), TraceDigest: traceDigest,
		ReplayStable: run.ReplayStable, Conformant: run.Conformant,
	})
	ledger.runIDs[run.ID] = true

	for index, result := range evaluated.Obligations {
		entry := &ledger.entries[index]
		if entry.ObligationID != result.Obligation.ID {
			return errors.New("coverage evaluator reordered the frozen denominator")
		}
		reference := result.Reference
		reference.RunID = run.ID
		reference.Scenario = run.Scenario
		reference.Artifact = run.Artifact
		hasPartialEvidence := result.Evidence.Reach || result.Evidence.Observe
		if targeted[result.Obligation.ID] || result.Covered || hasPartialEvidence {
			entry.LastEvidence = cloneReference(&reference)
		}
		if targeted[result.Obligation.ID] {
			entry.Attempts++
			if entry.Status == LedgerUncovered {
				entry.Status = LedgerAttempted
			}
		}
		if result.Covered && entry.Status != LedgerUnsupported && entry.CoveredBy == nil {
			entry.Status = LedgerCovered
			entry.CoveredBy = cloneReference(&reference)
		}
	}
	ledger.refreshSummary()
	return nil
}

// Debts returns actionable uncovered/attempted obligations in a deterministic
// risk-first order. Unsupported obligations remain in the score but are not
// sent to a test-planning Agent as if more scheduling could implement them.
func (ledger *Ledger) Debts() []Debt {
	if ledger == nil {
		return nil
	}
	items, _ := ledger.profile.denominator()
	byID := make(map[string]Obligation, len(items))
	for _, obligation := range items {
		byID[obligation.ID] = obligation
	}
	debts := make([]Debt, 0)
	for _, entry := range ledger.entries {
		if entry.Status != LedgerUncovered && entry.Status != LedgerAttempted {
			continue
		}
		debts = append(debts, Debt{
			Obligation: cloneObligation(byID[entry.ObligationID]), Status: entry.Status, Attempts: entry.Attempts,
			LastEvidence: cloneReference(entry.LastEvidence),
		})
	}
	sort.Slice(debts, func(i, j int) bool {
		left, right := riskRank(debts[i].Obligation.Risk), riskRank(debts[j].Obligation.Risk)
		if left != right {
			return left > right
		}
		if debts[i].Attempts != debts[j].Attempts {
			return debts[i].Attempts < debts[j].Attempts
		}
		return debts[i].Obligation.ID < debts[j].Obligation.ID
	})
	return debts
}

func (ledger *Ledger) refreshSummary() {
	items, _ := ledger.profile.denominator()
	results := make([]ObligationResult, 0, len(items))
	for index, obligation := range items {
		entry := ledger.entries[index]
		result := ObligationResult{Obligation: obligation, Covered: entry.Status == LedgerCovered}
		if entry.CoveredBy != nil {
			result.Evidence = entry.CoveredBy.Evidence
			result.Reference = *cloneReference(entry.CoveredBy)
		}
		results = append(results, result)
	}
	ledger.summary = summarize(ledger.profile, ledger.profileDigest, results, ledger.manifest)
}

// Report returns a detached, read-only snapshot suitable for persistence or
// Agent context. Mutating it cannot affect the trusted ledger.
func (ledger *Ledger) Report() LedgerReport {
	if ledger == nil {
		return LedgerReport{}
	}
	report := LedgerReport{
		Version: ledger.version, ProfileID: ledger.profileID, ProfileDigest: ledger.profileDigest,
		ManifestDigest: ledger.manifestDigest, Runs: ledger.runs, Entries: ledger.entries, Summary: ledger.summary,
	}
	return cloneReport(report)
}

func (ledger *Ledger) Summary() Summary {
	return ledger.Report().Summary
}

// MarshalJSON serializes the detached public report, never private mutable
// state or the complete frozen Profile.
func (ledger *Ledger) MarshalJSON() ([]byte, error) {
	return json.Marshal(ledger.Report())
}

func cloneReference(reference *EvidenceReference) *EvidenceReference {
	if reference == nil {
		return nil
	}
	copy := *reference
	copy.ReachSteps = append([]int(nil), reference.ReachSteps...)
	copy.ObserveSteps = append([]int(nil), reference.ObserveSteps...)
	copy.OrderingSteps = append([]StepPair(nil), reference.OrderingSteps...)
	copy.CountResults = append([]int(nil), reference.CountResults...)
	copy.CheckedMonitors = append([]string(nil), reference.CheckedMonitors...)
	copy.OracleViolations = append([]oracle.Violation(nil), reference.OracleViolations...)
	return &copy
}

func normalizedRisk(risk string) string {
	if risk == "" {
		return RiskStandard
	}
	return risk
}

func riskRank(risk string) int {
	switch normalizedRisk(risk) {
	case RiskCritical:
		return 4
	case RiskHigh:
		return 3
	case RiskStandard:
		return 2
	case RiskLow:
		return 1
	default:
		return 0
	}
}

func digestManifest(manifest driver.Manifest) (string, error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func cloneProfile(profile Profile) (Profile, error) {
	encoded, err := json.Marshal(profile)
	if err != nil {
		return Profile{}, err
	}
	var cloned Profile
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return Profile{}, err
	}
	return cloned, nil
}

func cloneManifest(manifest driver.Manifest) (driver.Manifest, error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return driver.Manifest{}, err
	}
	var cloned driver.Manifest
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return driver.Manifest{}, err
	}
	return cloned, nil
}

func cloneReport(report LedgerReport) LedgerReport {
	encoded, err := json.Marshal(report)
	if err != nil {
		return LedgerReport{}
	}
	var cloned LedgerReport
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return LedgerReport{}
	}
	return cloned
}

func cloneObligation(obligation Obligation) Obligation {
	encoded, err := json.Marshal(obligation)
	if err != nil {
		return Obligation{}
	}
	var cloned Obligation
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return Obligation{}
	}
	return cloned
}

func validateSemanticMatcher(profile Profile, matcher SemanticMatcher) error {
	domains := semanticDomains(profile)
	if len(domains) == 0 {
		return nil
	}
	if matcher == nil {
		return fmt.Errorf("profile requires semantic matcher for domains %v", domains)
	}
	for _, domain := range domains {
		if domain != matcher.ID() {
			return fmt.Errorf("semantic matcher %q cannot evaluate domain %q", matcher.ID(), domain)
		}
	}
	return nil
}

func semanticDomains(profile Profile) []string {
	items, _ := profile.denominator()
	seen := make(map[string]bool)
	add := func(predicate TracePredicate) {
		if predicate.Semantic != nil {
			seen[predicate.Semantic.Domain] = true
		}
	}
	for _, obligation := range items {
		for _, predicate := range obligation.Evidence.Reach {
			add(predicate)
		}
		for _, predicate := range obligation.Evidence.Observe {
			add(predicate)
		}
		for _, ordering := range obligation.Evidence.Orderings {
			add(ordering.Before)
			add(ordering.After)
			for _, predicate := range ordering.Without {
				add(predicate)
			}
		}
		for _, count := range obligation.Evidence.Counts {
			add(count.Predicate)
		}
	}
	domains := make([]string, 0, len(seen))
	for domain := range seen {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

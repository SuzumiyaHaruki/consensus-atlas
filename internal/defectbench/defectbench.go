// Package defectbench evaluates trusted Campaign reports against a private,
// versioned defect manifest. Coverage and protocol-state metrics are retained
// as explanatory data, but never create defect-kill credit.
package defectbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

const (
	Version                     = 1
	ManifestVersion2            = 2
	DetectionGranularityPlanEnd = "plan-end"

	KindControl = "control"
	KindDefect  = "defect"

	ProvenanceHistorical     = "historical"
	ProvenanceSemanticMutant = "semantic-mutant"
	ProvenanceFixture        = "fixture"

	StatusKilled        = "killed"
	StatusSurvived      = "survived"
	StatusControlPass   = "control-pass"
	StatusFalsePositive = "false-positive"
	StatusInvalid       = "invalid"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type Budget struct {
	MaxRuns             int `json:"max_runs"`
	MaxDecisions        int `json:"max_decisions"`
	MaxPrimaryWorkUnits int `json:"max_primary_work_units"`
}

type KillPolicy struct {
	AllowedMonitors     []string `json:"allowed_monitors"`
	RequireReplayStable bool     `json:"require_replay_stable"`
	RequireConformant   bool     `json:"require_conformant"`
}

// Variant is private evaluator input. ID, kind, provenance, root cause, source
// and expected SUT identity must not be included in Agent prompts.
type Variant struct {
	ID                string `json:"id"`
	TrialID           string `json:"trial_id"`
	Kind              string `json:"kind"`
	RootCauseID       string `json:"root_cause_id,omitempty"`
	Category          string `json:"category"`
	Provenance        string `json:"provenance"`
	SourceDigest      string `json:"source_digest"`
	SUTManifestDigest string `json:"sut_manifest_digest"`
	BuildAuditDigest  string `json:"build_audit_digest,omitempty"`
	BinaryDigest      string `json:"binary_digest,omitempty"`
}

type Manifest struct {
	Version       int    `json:"version"`
	ID            string `json:"id"`
	BlindingNonce string `json:"blinding_nonce"`
	Protocol      string `json:"protocol"`
	ProfileID     string `json:"profile_id"`
	ProfileDigest string `json:"profile_digest"`
	// PSSID is required by v2 manifests. It binds trusted evaluator monitor
	// selection to the frozen Profile rather than a curator CLI argument.
	PSSID      string     `json:"pss_id,omitempty"`
	Budget     Budget     `json:"budget"`
	KillPolicy KillPolicy `json:"kill_policy"`
	Variants   []Variant  `json:"variants"`
}

// BlindManifest is the complete Agent-facing benchmark view. Opaque trial IDs
// are the only per-variant information released before evaluation.
type BlindManifest struct {
	Version         int          `json:"version"`
	BenchmarkID     string       `json:"benchmark_id"`
	BenchmarkDigest string       `json:"benchmark_digest"`
	Protocol        string       `json:"protocol"`
	ProfileID       string       `json:"profile_id"`
	ProfileDigest   string       `json:"profile_digest"`
	PSSID           string       `json:"pss_id,omitempty"`
	Budget          Budget       `json:"budget"`
	Trials          []BlindTrial `json:"trials"`
}

type BlindTrial struct {
	TrialID string `json:"trial_id"`
}

func (manifest Manifest) Validate() error {
	if manifest.Version != Version && manifest.Version != ManifestVersion2 {
		return fmt.Errorf("unsupported defect benchmark version %d", manifest.Version)
	}
	if !validID(manifest.ID) || manifest.Protocol == "" || manifest.ProfileID == "" {
		return errors.New("benchmark id, protocol, and profile id are required")
	}
	if !isDigest(manifest.BlindingNonce) {
		return errors.New("blinding nonce must be 64 lowercase hexadecimal characters")
	}
	if !isDigest(manifest.ProfileDigest) {
		return errors.New("profile digest must be 64 lowercase hexadecimal characters")
	}
	if manifest.Version == ManifestVersion2 && !validID(manifest.PSSID) {
		return errors.New("version-2 benchmark requires a valid frozen pss_id")
	}
	if manifest.Budget.MaxRuns < 1 || manifest.Budget.MaxDecisions < 1 ||
		manifest.Budget.MaxPrimaryWorkUnits < 1 {
		return errors.New("all benchmark budgets must be positive")
	}
	if !manifest.KillPolicy.RequireReplayStable || !manifest.KillPolicy.RequireConformant {
		return errors.New("trusted kill policy must require stable replay and conformance")
	}
	if len(manifest.KillPolicy.AllowedMonitors) == 0 {
		return errors.New("at least one defect-kill monitor is required")
	}
	monitors := make(map[string]bool, len(manifest.KillPolicy.AllowedMonitors))
	for _, name := range manifest.KillPolicy.AllowedMonitors {
		if name == "" || name == "trace-integrity" {
			return errors.New("trace-integrity is a harness check, not a defect-kill monitor")
		}
		if monitors[name] {
			return fmt.Errorf("duplicate allowed monitor %q", name)
		}
		monitors[name] = true
	}
	if len(manifest.Variants) < 2 {
		return errors.New("benchmark requires at least one defect and one control")
	}
	ids := make(map[string]bool, len(manifest.Variants))
	trials := make(map[string]bool, len(manifest.Variants))
	controls, defects := 0, 0
	for index, variant := range manifest.Variants {
		if !validID(variant.ID) || !validID(variant.TrialID) || variant.Category == "" {
			return fmt.Errorf("variant[%d] requires valid id, trial_id, and category", index)
		}
		if ids[variant.ID] || trials[variant.TrialID] {
			return fmt.Errorf("duplicate variant or trial identity at variant[%d]", index)
		}
		if variant.ID == variant.TrialID || ids[variant.TrialID] || trials[variant.ID] {
			return fmt.Errorf("variant %s reuses a private variant identity as an opaque trial id", variant.ID)
		}
		ids[variant.ID], trials[variant.TrialID] = true, true
		if !isDigest(variant.SourceDigest) || !isDigest(variant.SUTManifestDigest) {
			return fmt.Errorf("variant %s has an invalid source or SUT manifest digest", variant.ID)
		}
		if (variant.BuildAuditDigest == "") != (variant.BinaryDigest == "") ||
			(variant.BuildAuditDigest != "" && (!isDigest(variant.BuildAuditDigest) || !isDigest(variant.BinaryDigest))) {
			return fmt.Errorf("variant %s must declare valid build audit and binary digests together", variant.ID)
		}
		switch variant.Provenance {
		case ProvenanceHistorical, ProvenanceSemanticMutant, ProvenanceFixture:
		default:
			return fmt.Errorf("variant %s has unsupported provenance %q", variant.ID, variant.Provenance)
		}
		switch variant.Kind {
		case KindControl:
			controls++
			if variant.RootCauseID != "" {
				return fmt.Errorf("control %s cannot declare a root cause", variant.ID)
			}
		case KindDefect:
			defects++
			if !validID(variant.RootCauseID) {
				return fmt.Errorf("defect %s requires a valid root cause id", variant.ID)
			}
		default:
			return fmt.Errorf("variant %s has unsupported kind %q", variant.ID, variant.Kind)
		}
	}
	if controls == 0 || defects == 0 {
		return errors.New("benchmark requires at least one defect and one control")
	}
	return nil
}

func Digest(manifest Manifest) (string, error) {
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	canonical := manifest
	canonical.KillPolicy.AllowedMonitors = append([]string(nil), manifest.KillPolicy.AllowedMonitors...)
	sort.Strings(canonical.KillPolicy.AllowedMonitors)
	canonical.Variants = append([]Variant(nil), manifest.Variants...)
	sort.Slice(canonical.Variants, func(i, j int) bool { return canonical.Variants[i].ID < canonical.Variants[j].ID })
	return digestJSON(canonical)
}

func DriverManifestDigest(manifest driver.Manifest) (string, error) {
	return digestJSON(manifest)
}

func (manifest Manifest) Blind() (BlindManifest, error) {
	digest, err := Digest(manifest)
	if err != nil {
		return BlindManifest{}, err
	}
	result := BlindManifest{
		Version: manifest.Version, BenchmarkID: manifest.ID, BenchmarkDigest: digest,
		Protocol: manifest.Protocol, ProfileID: manifest.ProfileID,
		ProfileDigest: manifest.ProfileDigest, PSSID: manifest.PSSID, Budget: manifest.Budget,
	}
	for _, variant := range manifest.Variants {
		result.Trials = append(result.Trials, BlindTrial{TrialID: variant.TrialID})
	}
	sort.Slice(result.Trials, func(i, j int) bool { return result.Trials[i].TrialID < result.Trials[j].TrialID })
	return result, nil
}

type FindingRef struct {
	PlanID               string `json:"plan_id"`
	RunID                string `json:"run_id"`
	Monitor              string `json:"monitor"`
	Step                 int    `json:"step,omitempty"`
	Message              string `json:"message"`
	TraceDigest          string `json:"trace_digest"`
	DetectionGranularity string `json:"detection_granularity"`
	// FirstKillPrimaryWork is charged at the point at which the trusted
	// evaluator actually observes the violation. The current evaluator runs
	// monitors after a complete Campaign plan, so the value is intentionally
	// plan-end granular rather than an inferred trace-prefix cost.
	FirstKillPrimaryWork int `json:"first_kill_primary_work"`
}

type TrialResult struct {
	TrialID           string         `json:"trial_id"`
	VariantID         string         `json:"variant_id"`
	Kind              string         `json:"kind"`
	RootCauseID       string         `json:"root_cause_id,omitempty"`
	Status            string         `json:"status"`
	InvalidReason     string         `json:"invalid_reason,omitempty"`
	Runs              int            `json:"runs"`
	Decisions         int            `json:"decisions"`
	PrimaryWorkUnits  int            `json:"primary_work_units"`
	ReplayWorkUnits   int            `json:"replay_work_units"`
	CoverageScore     float64        `json:"coverage_score"`
	Finding           *FindingRef    `json:"finding,omitempty"`
	ExecutionEvidence *TrialEvidence `json:"execution_evidence,omitempty"`
}

// TrialEvidence binds evaluator-read artifacts to one blind trial. It is
// accepted only after the trusted CLI validates the BuildAudit and binary.
type TrialEvidence struct {
	BuildAuditDigest     string `json:"build_audit_digest"`
	BinaryDigest         string `json:"binary_digest"`
	BuildSpecDigest      string `json:"build_spec_digest"`
	SourceDigest         string `json:"source_digest"`
	SUTBuildIdentity     string `json:"sut_build_identity"`
	CampaignReportDigest string `json:"campaign_report_digest"`
}

type RootCauseResult struct {
	RootCauseID string `json:"root_cause_id"`
	Variants    int    `json:"variants"`
	Killed      bool   `json:"killed"`
}

type Summary struct {
	DefectVariants    int     `json:"defect_variants"`
	KilledVariants    int     `json:"killed_variants"`
	RootCauses        int     `json:"root_causes"`
	KilledRootCauses  int     `json:"killed_root_causes"`
	RootCauseKillRate float64 `json:"root_cause_kill_rate"`
	Controls          int     `json:"controls"`
	FalsePositives    int     `json:"false_positives"`
	FalsePositiveRate float64 `json:"false_positive_rate"`
	InvalidTrials     int     `json:"invalid_trials"`
	TotalRuns         int     `json:"total_runs"`
	TotalDecisions    int     `json:"total_decisions"`
	PrimaryWorkUnits  int     `json:"primary_work_units"`
	ReplayWorkUnits   int     `json:"replay_work_units"`
}

type Report struct {
	Version         int               `json:"version"`
	BenchmarkID     string            `json:"benchmark_id"`
	BenchmarkDigest string            `json:"benchmark_digest"`
	Trials          []TrialResult     `json:"trials"`
	RootCauses      []RootCauseResult `json:"root_causes"`
	Summary         Summary           `json:"summary"`
}

type Ledger struct {
	manifest Manifest
	digest   string
	variants map[string]Variant
	monitors []oracle.Monitor
	results  map[string]TrialResult
}

func NewLedger(manifest Manifest, trustedMonitors ...oracle.Monitor) (*Ledger, error) {
	digest, err := Digest(manifest)
	if err != nil {
		return nil, err
	}
	available := make(map[string]oracle.Monitor, len(trustedMonitors))
	for _, monitor := range trustedMonitors {
		if monitor == nil || monitor.Name() == "" || monitor.Name() == "trace-integrity" {
			return nil, errors.New("trusted defect monitor must have a non-reserved name")
		}
		if _, exists := available[monitor.Name()]; exists {
			return nil, fmt.Errorf("duplicate trusted monitor %q", monitor.Name())
		}
		available[monitor.Name()] = monitor
	}
	selected := make([]oracle.Monitor, 0, len(manifest.KillPolicy.AllowedMonitors))
	for _, name := range manifest.KillPolicy.AllowedMonitors {
		monitor, ok := available[name]
		if !ok {
			return nil, fmt.Errorf("allowed monitor %q has no trusted implementation", name)
		}
		selected = append(selected, monitor)
	}
	variants := make(map[string]Variant, len(manifest.Variants))
	for _, variant := range manifest.Variants {
		variants[variant.TrialID] = variant
	}
	return &Ledger{
		manifest: manifest, digest: digest, variants: variants,
		monitors: selected, results: make(map[string]TrialResult),
	}, nil
}

// AddTrial evaluates a private Campaign report selected only by its opaque
// trial ID. Identity, budget, and report-integrity failures are recorded as an
// invalid trial and cannot receive kill credit.
func (ledger *Ledger) AddTrial(trialID string, report campaign.Report, supplied ...TrialEvidence) (TrialResult, error) {
	if ledger == nil {
		return TrialResult{}, errors.New("defect benchmark ledger is nil")
	}
	variant, ok := ledger.variants[trialID]
	if !ok {
		return TrialResult{}, fmt.Errorf("unknown blind trial %q", trialID)
	}
	if _, exists := ledger.results[trialID]; exists {
		return TrialResult{}, fmt.Errorf("blind trial %q was already evaluated", trialID)
	}
	result := TrialResult{
		TrialID: trialID, VariantID: variant.ID, Kind: variant.Kind,
		RootCauseID: variant.RootCauseID, Status: StatusInvalid,
		Runs: report.ChargedRuns, Decisions: report.ChargedDecisions,
		PrimaryWorkUnits: report.Cost.Primary.WorkUnits,
		ReplayWorkUnits:  report.Cost.Replay.WorkUnits, CoverageScore: report.Final.Score,
	}
	if len(supplied) > 1 {
		return TrialResult{}, errors.New("at most one trial evidence record is allowed")
	}
	var evidence *TrialEvidence
	if len(supplied) == 1 {
		copy := supplied[0]
		evidence = &copy
		result.ExecutionEvidence = evidence
	}
	if reason := ledger.invalidReason(variant, report, evidence); reason != "" {
		result.InvalidReason = reason
		ledger.results[trialID] = result
		return result, nil
	}

	result.Status = StatusControlPass
	if variant.Kind == KindDefect {
		result.Status = StatusSurvived
	}
	primaryWork := 0
	for _, plan := range report.Plans {
		primaryWork += plan.Cost.Primary.WorkUnits
		for _, run := range plan.Runs {
			trace := append([]core.TraceRecord(nil), plan.SetupTrace...)
			trace = append(trace, run.Explorer.Trace...)
			if err := oracle.ValidateEvidence(trace, ledger.monitors...); err != nil {
				result.Status = StatusInvalid
				result.InvalidReason = "campaign monitor evidence failed trusted validation: " + err.Error()
				ledger.results[trialID] = result
				return result, nil
			}
			if !run.ReplayStable || !run.Explorer.Conform || run.Explorer.ExecutionError != "" {
				continue
			}
			checked := oracle.Check(trace, ledger.monitors...)
			if len(checked.Violations) == 0 {
				continue
			}
			violation := checked.Violations[0]
			traceDigest, err := core.CanonicalTraceDigest(trace)
			if err != nil {
				result.Status = StatusInvalid
				result.InvalidReason = "campaign trace cannot be assigned a canonical identity"
				ledger.results[trialID] = result
				return result, nil
			}
			result.Finding = &FindingRef{
				PlanID: plan.ID, RunID: run.ID, Monitor: violation.Monitor,
				Step: violation.Step, Message: violation.Message, TraceDigest: traceDigest,
				DetectionGranularity: DetectionGranularityPlanEnd,
				FirstKillPrimaryWork: primaryWork,
			}
			if variant.Kind == KindDefect {
				result.Status = StatusKilled
			} else {
				result.Status = StatusFalsePositive
			}
			ledger.results[trialID] = result
			return result, nil
		}
	}
	ledger.results[trialID] = result
	return result, nil
}

func (ledger *Ledger) invalidReason(variant Variant, report campaign.Report, evidence *TrialEvidence) string {
	if report.Version != campaign.ReportVersion {
		return fmt.Sprintf("campaign report version %d does not provide trusted full cost", report.Version)
	}
	if report.Protocol != ledger.manifest.Protocol || report.ProfileID != ledger.manifest.ProfileID ||
		report.ProfileDigest != ledger.manifest.ProfileDigest {
		return "campaign protocol/profile identity does not match benchmark"
	}
	if ledger.manifest.PSSID != "" && report.PSSID != ledger.manifest.PSSID {
		return "campaign PSS identity does not match benchmark"
	}
	if reason := validateTrialEvidence(variant, report, evidence); reason != "" {
		return reason
	}
	manifestDigest, err := DriverManifestDigest(report.Manifest)
	if err != nil || manifestDigest != variant.SUTManifestDigest {
		return "campaign SUT manifest identity does not match private variant"
	}
	if reason := validateCoverageLedger(report, manifestDigest); reason != "" {
		return reason
	}
	if report.ChargedRuns < 0 || report.ChargedDecisions < 0 ||
		!validPhaseCost(report.Cost.Primary) || !validPhaseCost(report.Cost.Replay) {
		return "campaign costs cannot be negative"
	}
	if report.ChargedRuns > ledger.manifest.Budget.MaxRuns ||
		report.ChargedDecisions > ledger.manifest.Budget.MaxDecisions ||
		report.Cost.Primary.WorkUnits > ledger.manifest.Budget.MaxPrimaryWorkUnits {
		return "campaign exceeded frozen benchmark budget"
	}
	runs, decisions := 0, 0
	var total campaign.ExecutionCost
	for _, plan := range report.Plans {
		runs += len(plan.Runs)
		planDecisions := 0
		for _, run := range plan.Runs {
			planDecisions += len(run.Explorer.Decisions)
			if len(run.Explorer.Trace) != len(run.Explorer.Decisions) {
				return "campaign measurement trace and decision log lengths differ"
			}
			trace := append([]core.TraceRecord(nil), plan.SetupTrace...)
			trace = append(trace, run.Explorer.Trace...)
			if len(oracle.Check(trace, oracle.TraceIntegrity{}).Violations) != 0 {
				return "campaign trace failed trusted integrity validation"
			}
		}
		if planDecisions != plan.ChargedDecisions ||
			plan.Cost.Primary.MeasurementEvents != plan.ChargedDecisions {
			return "campaign plan decision or measurement totals are inconsistent"
		}
		if !validPhaseCost(plan.Cost.Primary) || !validPhaseCost(plan.Cost.Replay) ||
			!validWorkTotal(plan.Cost.Primary) || !validWorkTotal(plan.Cost.Replay) {
			return "campaign plan work total is internally inconsistent"
		}
		decisions += planDecisions
		addPhase(&total.Primary, plan.Cost.Primary)
		addPhase(&total.Replay, plan.Cost.Replay)
	}
	if runs != report.ChargedRuns || decisions != report.ChargedDecisions ||
		report.Cost.Primary.MeasurementEvents != report.ChargedDecisions || total != report.Cost {
		return "campaign run, decision, or measurement totals are inconsistent"
	}
	if !validWorkTotal(report.Cost.Primary) || !validWorkTotal(report.Cost.Replay) {
		return "campaign primary work total is internally inconsistent"
	}
	return ""
}

func CampaignReportDigest(report campaign.Report) (string, error) {
	return digestJSON(report)
}

func validateTrialEvidence(variant Variant, report campaign.Report, evidence *TrialEvidence) string {
	required := variant.BuildAuditDigest != ""
	if !required && evidence == nil {
		return ""
	}
	if !required || evidence == nil {
		return "campaign trial is missing or unexpectedly supplies bound build evidence"
	}
	if !isDigest(evidence.BuildAuditDigest) || !isDigest(evidence.BinaryDigest) ||
		!isDigest(evidence.BuildSpecDigest) || !isDigest(evidence.SourceDigest) ||
		!isDigest(evidence.CampaignReportDigest) || evidence.SUTBuildIdentity == "" {
		return "campaign trial build evidence has an invalid identity"
	}
	if evidence.BuildAuditDigest != variant.BuildAuditDigest || evidence.BinaryDigest != variant.BinaryDigest ||
		evidence.SourceDigest != variant.SourceDigest || evidence.SUTBuildIdentity != report.Manifest.SUTVersion {
		return "campaign trial build evidence does not match the private variant"
	}
	digest, err := CampaignReportDigest(report)
	if err != nil || digest != evidence.CampaignReportDigest {
		return "campaign report digest does not match submitted evidence"
	}
	return ""
}

func validateCoverageLedger(report campaign.Report, manifestDigest string) string {
	ledger := report.Ledger
	if ledger.Version != coverage.LedgerVersion || ledger.ProfileID != report.ProfileID ||
		ledger.ProfileDigest != report.ProfileDigest || ledger.ManifestDigest != manifestDigest {
		return "campaign Coverage Ledger identity is inconsistent"
	}
	type expectedRun struct {
		id           string
		targets      []string
		digest       string
		replayStable bool
		conformant   bool
	}
	expected := make([]expectedRun, 0, report.ChargedRuns)
	for _, plan := range report.Plans {
		for _, run := range plan.Runs {
			trace := append([]core.TraceRecord(nil), plan.SetupTrace...)
			trace = append(trace, run.Explorer.Trace...)
			digest, err := core.CanonicalTraceDigest(trace)
			if err != nil {
				return "campaign trace cannot be assigned a canonical identity"
			}
			expected = append(expected, expectedRun{
				id: run.ID, targets: run.ActiveTargets, digest: digest,
				replayStable: run.ReplayStable,
				conformant: run.Explorer.Conform && run.Explorer.ExecutionError == "" &&
					run.MonitorEvidenceError == "",
			})
		}
	}
	if len(ledger.Runs) != len(expected) {
		return "campaign Coverage Ledger run count is inconsistent"
	}
	knownRuns := make(map[string]string, len(expected))
	for index, want := range expected {
		got := ledger.Runs[index]
		if got.ID != want.id || got.TraceDigest != want.digest ||
			got.ReplayStable != want.replayStable || got.Conformant != want.conformant ||
			!equalStrings(got.Targets, want.targets) {
			return "campaign Coverage Ledger run witness is inconsistent"
		}
		if _, exists := knownRuns[got.ID]; exists {
			return "campaign Coverage Ledger contains a duplicate run"
		}
		knownRuns[got.ID] = got.TraceDigest
	}
	entries := make(map[string]bool, len(ledger.Entries))
	for _, entry := range ledger.Entries {
		if entry.ObligationID == "" || entries[entry.ObligationID] {
			return "campaign Coverage Ledger obligation identity is inconsistent"
		}
		entries[entry.ObligationID] = true
		for _, reference := range []*coverage.EvidenceReference{entry.CoveredBy, entry.LastEvidence} {
			if reference == nil {
				continue
			}
			if knownRuns[reference.RunID] != reference.TraceDigest || reference.TraceDigest == "" {
				return "campaign Coverage Ledger evidence does not reference a persisted run"
			}
		}
	}
	if report.Final.Score != ledger.Summary.Score || report.Final.Covered != ledger.Summary.Covered ||
		report.Final.Total != ledger.Summary.Total || report.Final.Unsupported != ledger.Summary.Unsupported {
		return "campaign final coverage and Coverage Ledger summary are inconsistent"
	}
	return ""
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validPhaseCost(cost campaign.PhaseCost) bool {
	return cost.SetupAttempts >= 0 && cost.SetupSteps >= 0 && cost.SetupRuntimeEvents >= 0 &&
		cost.MeasurementEvents >= 0 && cost.WorkUnits >= 0
}

func validWorkTotal(cost campaign.PhaseCost) bool {
	minimum := cost.SetupAttempts + cost.MeasurementEvents
	if cost.SetupSteps > cost.SetupRuntimeEvents {
		minimum += cost.SetupSteps
	} else {
		minimum += cost.SetupRuntimeEvents
	}
	return cost.WorkUnits >= minimum
}

func addPhase(total *campaign.PhaseCost, delta campaign.PhaseCost) {
	total.SetupAttempts += delta.SetupAttempts
	total.SetupSteps += delta.SetupSteps
	total.SetupRuntimeEvents += delta.SetupRuntimeEvents
	total.MeasurementEvents += delta.MeasurementEvents
	total.WorkUnits += delta.WorkUnits
}

func (ledger *Ledger) Report() Report {
	if ledger == nil {
		return Report{}
	}
	report := Report{Version: Version, BenchmarkID: ledger.manifest.ID, BenchmarkDigest: ledger.digest}
	trialIDs := make([]string, 0, len(ledger.results))
	for id := range ledger.results {
		trialIDs = append(trialIDs, id)
	}
	sort.Strings(trialIDs)
	roots := make(map[string]RootCauseResult)
	for _, id := range trialIDs {
		result := ledger.results[id]
		report.Trials = append(report.Trials, result)
		report.Summary.TotalRuns += result.Runs
		report.Summary.TotalDecisions += result.Decisions
		report.Summary.PrimaryWorkUnits += result.PrimaryWorkUnits
		report.Summary.ReplayWorkUnits += result.ReplayWorkUnits
		if result.Status == StatusInvalid {
			report.Summary.InvalidTrials++
			continue
		}
		if result.Kind == KindControl {
			report.Summary.Controls++
			if result.Status == StatusFalsePositive {
				report.Summary.FalsePositives++
			}
			continue
		}
		report.Summary.DefectVariants++
		if result.Status == StatusKilled {
			report.Summary.KilledVariants++
		}
		root := roots[result.RootCauseID]
		root.RootCauseID = result.RootCauseID
		root.Variants++
		root.Killed = root.Killed || result.Status == StatusKilled
		roots[result.RootCauseID] = root
	}
	rootIDs := make([]string, 0, len(roots))
	for id := range roots {
		rootIDs = append(rootIDs, id)
	}
	sort.Strings(rootIDs)
	for _, id := range rootIDs {
		root := roots[id]
		report.RootCauses = append(report.RootCauses, root)
		if root.Killed {
			report.Summary.KilledRootCauses++
		}
	}
	report.Summary.RootCauses = len(report.RootCauses)
	if report.Summary.RootCauses > 0 {
		report.Summary.RootCauseKillRate = 100 * float64(report.Summary.KilledRootCauses) / float64(report.Summary.RootCauses)
	}
	if report.Summary.Controls > 0 {
		report.Summary.FalsePositiveRate = 100 * float64(report.Summary.FalsePositives) / float64(report.Summary.Controls)
	}
	return report
}

func validID(value string) bool { return idPattern.MatchString(value) }

func isDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

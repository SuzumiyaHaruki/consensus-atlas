package defectbench_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/explore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

func TestBlindManifestHidesDefectMetadata(t *testing.T) {
	manifest, _, _ := fixtureManifest(t)
	blind, err := manifest.Blind()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(blind)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"root.fixture", "mutant-a", "semantic-mutant", "control-a"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("blind manifest leaked private metadata %q: %s", secret, encoded)
		}
	}
	if len(blind.Trials) != 3 || blind.BenchmarkDigest == "" {
		t.Fatalf("blind manifest is incomplete: %+v", blind)
	}
}

func TestVersion2ManifestBindsPSSIdentityToBlindAndCampaignReports(t *testing.T) {
	manifest, controlDriver, _ := fixtureManifest(t)
	manifest.Version = defectbench.ManifestVersion2
	manifest.PSSID = "fixture-pss-v1"
	blind, err := manifest.Blind()
	if err != nil {
		t.Fatal(err)
	}
	if blind.Version != defectbench.ManifestVersion2 || blind.PSSID != manifest.PSSID {
		t.Fatalf("blind manifest lost frozen PSS identity: %+v", blind)
	}
	ledger, err := defectbench.NewLedger(manifest, oracle.Agreement{})
	if err != nil {
		t.Fatal(err)
	}
	report := runFixtureCampaign(t, controlDriver, false)
	report.PSSID = "other-pss-v1"
	result, err := ledger.AddTrial("trial-01", report)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != defectbench.StatusInvalid || !strings.Contains(result.InvalidReason, "PSS identity") {
		t.Fatalf("mismatched PSS identity entered benchmark: %+v", result)
	}
	manifest.PSSID = ""
	if err := manifest.Validate(); err == nil {
		t.Fatal("version-2 manifest without PSS identity was accepted")
	}
}

func TestLedgerSeparatesCoverageFromRootCauseKillAndFalsePositive(t *testing.T) {
	manifest, controlDriver, mutantDriver := fixtureManifest(t)
	controlReport := runFixtureCampaign(t, controlDriver, false)
	mutantReport := runFixtureCampaign(t, mutantDriver, true)
	if controlReport.Final.Score != mutantReport.Final.Score || controlReport.Final.Score != 100 {
		t.Fatalf("fixture must hold internal coverage constant: control=%f mutant=%f", controlReport.Final.Score, mutantReport.Final.Score)
	}
	// Stored Oracle output is not kill authority; the evaluator recomputes it
	// from the trusted trace using its registered monitor.
	mutantReport.Plans[0].Runs[0].Oracle = oracle.Result{}

	ledger, err := defectbench.NewLedger(manifest, oracle.Agreement{})
	if err != nil {
		t.Fatal(err)
	}
	control, err := ledger.AddTrial("trial-01", controlReport)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ledger.AddTrial("trial-02", mutantReport)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ledger.AddTrial("trial-03", mutantReport)
	if err != nil {
		t.Fatal(err)
	}
	if control.Status != defectbench.StatusControlPass || control.Finding != nil {
		t.Fatalf("correct control produced a false positive: %+v", control)
	}
	for _, killed := range []defectbench.TrialResult{first, second} {
		if killed.Status != defectbench.StatusKilled || killed.Finding == nil ||
			killed.Finding.Monitor != "agreement" || killed.Finding.FirstKillPrimaryWork <= 0 ||
			killed.Finding.DetectionGranularity != defectbench.DetectionGranularityPlanEnd {
			t.Fatalf("defect lacked trusted kill evidence: %+v", killed)
		}
	}

	report := ledger.Report()
	if report.Summary.DefectVariants != 2 || report.Summary.KilledVariants != 2 ||
		report.Summary.RootCauses != 1 || report.Summary.KilledRootCauses != 1 ||
		report.Summary.RootCauseKillRate != 100 || report.Summary.Controls != 1 ||
		report.Summary.FalsePositiveRate != 0 {
		t.Fatalf("root-cause aggregation is wrong: %+v", report.Summary)
	}
}

func TestLedgerRejectsUnaccountedOrOverBudgetCampaigns(t *testing.T) {
	manifest, controlDriver, _ := fixtureManifest(t)
	ledger, err := defectbench.NewLedger(manifest, oracle.Agreement{})
	if err != nil {
		t.Fatal(err)
	}
	report := runFixtureCampaign(t, controlDriver, false)
	report.Version = 1
	result, err := ledger.AddTrial("trial-01", report)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != defectbench.StatusInvalid || !strings.Contains(result.InvalidReason, "full cost") {
		t.Fatalf("legacy report unexpectedly entered the benchmark: %+v", result)
	}

	manifest, controlDriver, _ = fixtureManifest(t)
	manifest.Budget.MaxPrimaryWorkUnits = 1
	ledger, err = defectbench.NewLedger(manifest, oracle.Agreement{})
	if err != nil {
		t.Fatal(err)
	}
	result, err = ledger.AddTrial("trial-01", runFixtureCampaign(t, controlDriver, false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != defectbench.StatusInvalid || !strings.Contains(result.InvalidReason, "budget") {
		t.Fatalf("over-budget report unexpectedly entered the benchmark: %+v", result)
	}
}

func TestLedgerMakesMalformedMonitorEvidenceInvalidInsteadOfKilled(t *testing.T) {
	manifest, controlDriver, _ := fixtureManifest(t)
	manifest.KillPolicy.AllowedMonitors = []string{"evidence-fixture"}
	ledger, err := defectbench.NewLedger(manifest, rejectingEvidenceMonitor{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ledger.AddTrial("trial-01", runFixtureCampaign(t, controlDriver, false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != defectbench.StatusInvalid || result.Finding != nil ||
		!strings.Contains(result.InvalidReason, "monitor evidence") {
		t.Fatalf("malformed evidence was not isolated from kill credit: %+v", result)
	}
}

type rejectingEvidenceMonitor struct{}

func (rejectingEvidenceMonitor) Name() string { return "evidence-fixture" }
func (rejectingEvidenceMonitor) ValidateEvidence([]core.TraceRecord) error {
	return errors.New("fixture evidence is malformed")
}
func (rejectingEvidenceMonitor) Check([]core.TraceRecord) []oracle.Violation {
	return []oracle.Violation{{Monitor: "evidence-fixture", Message: "must not receive kill credit"}}
}

func TestLedgerRejectsTamperedPersistedWitness(t *testing.T) {
	manifest, controlDriver, _ := fixtureManifest(t)
	tests := []struct {
		name   string
		tamper func(*campaign.Report)
		want   string
	}{
		{
			name: "trace digest",
			tamper: func(report *campaign.Report) {
				report.Ledger.Runs[0].TraceDigest = strings.Repeat("0", 64)
			},
			want: "run witness",
		},
		{
			name: "evidence reference",
			tamper: func(report *campaign.Report) {
				report.Ledger.Entries[0].CoveredBy.TraceDigest = strings.Repeat("0", 64)
			},
			want: "persisted run",
		},
		{
			name: "reported score",
			tamper: func(report *campaign.Report) {
				report.Final.Score = 99
			},
			want: "final coverage",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ledger, err := defectbench.NewLedger(manifest, oracle.Agreement{})
			if err != nil {
				t.Fatal(err)
			}
			report := runFixtureCampaign(t, controlDriver, false)
			test.tamper(&report)
			result, err := ledger.AddTrial("trial-01", report)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != defectbench.StatusInvalid || !strings.Contains(result.InvalidReason, test.want) {
				t.Fatalf("tampered witness unexpectedly entered evaluation: %+v", result)
			}
		})
	}
}

func TestArtifactBoundTrialRejectsMissingOrSwappedEvidence(t *testing.T) {
	manifest, controlDriver, _ := fixtureManifest(t)
	manifest.Variants[0].BuildAuditDigest = strings.Repeat("d", 64)
	manifest.Variants[0].BinaryDigest = strings.Repeat("e", 64)
	report := runFixtureCampaign(t, controlDriver, false)
	reportDigest, err := defectbench.CampaignReportDigest(report)
	if err != nil {
		t.Fatal(err)
	}
	evidence := defectbench.TrialEvidence{
		BuildAuditDigest: manifest.Variants[0].BuildAuditDigest,
		BinaryDigest:     manifest.Variants[0].BinaryDigest,
		BuildSpecDigest:  strings.Repeat("f", 64), SourceDigest: manifest.Variants[0].SourceDigest,
		SUTBuildIdentity: controlDriver.SUTVersion, CampaignReportDigest: reportDigest,
	}
	for _, test := range []struct {
		name     string
		supplied []defectbench.TrialEvidence
		status   string
	}{
		{name: "missing", status: defectbench.StatusInvalid},
		{name: "swapped binary", supplied: []defectbench.TrialEvidence{func() defectbench.TrialEvidence {
			changed := evidence
			changed.BinaryDigest = strings.Repeat("0", 64)
			return changed
		}()}, status: defectbench.StatusInvalid},
		{name: "bound", supplied: []defectbench.TrialEvidence{evidence}, status: defectbench.StatusControlPass},
	} {
		t.Run(test.name, func(t *testing.T) {
			ledger, err := defectbench.NewLedger(manifest, oracle.Agreement{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := ledger.AddTrial("trial-01", report, test.supplied...)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.status {
				t.Fatalf("status = %s, want %s: %+v", result.Status, test.status, result)
			}
		})
	}
}

func TestSubmissionV2RequiresBuildArtifacts(t *testing.T) {
	manifest, _, _ := fixtureManifest(t)
	blind, err := manifest.Blind()
	if err != nil {
		t.Fatal(err)
	}
	submission := defectbench.Submission{
		Version: defectbench.SubmissionVersion, BenchmarkID: blind.BenchmarkID, BenchmarkDigest: blind.BenchmarkDigest,
	}
	for _, trial := range blind.Trials {
		submission.Trials = append(submission.Trials, defectbench.TrialSubmission{
			TrialID: trial.TrialID, CampaignReport: trial.TrialID + ".json",
			BuildAudit: trial.TrialID + "-audit.json", Binary: trial.TrialID,
			Profile: "profile.json", Plans: "plans.json",
		})
	}
	if err := submission.Validate(blind); err != nil {
		t.Fatal(err)
	}
	submission.Trials[0].BuildAudit = ""
	if err := submission.Validate(blind); err == nil {
		t.Fatal("submission without a build audit was accepted")
	}
}

func fixtureManifest(t *testing.T) (defectbench.Manifest, driver.Manifest, driver.Manifest) {
	t.Helper()
	control := driver.Manifest{Driver: "fixture", SUT: "fixture-consensus", SUTVersion: "control-v1"}
	mutant := driver.Manifest{Driver: "fixture", SUT: "fixture-consensus", SUTVersion: "mutant-v1"}
	controlDigest, err := defectbench.DriverManifestDigest(control)
	if err != nil {
		t.Fatal(err)
	}
	mutantDigest, err := defectbench.DriverManifestDigest(mutant)
	if err != nil {
		t.Fatal(err)
	}
	profileDigest, err := coverage.Digest(fixtureProfile())
	if err != nil {
		t.Fatal(err)
	}
	manifest := defectbench.Manifest{
		Version: defectbench.Version, ID: "fixture-hidden-v1", BlindingNonce: strings.Repeat("d", 64), Protocol: "fixture",
		ProfileID: "fixture-profile", ProfileDigest: profileDigest,
		Budget: defectbench.Budget{MaxRuns: 2, MaxDecisions: 4, MaxPrimaryWorkUnits: 100},
		KillPolicy: defectbench.KillPolicy{
			AllowedMonitors: []string{"agreement"}, RequireReplayStable: true, RequireConformant: true,
		},
		Variants: []defectbench.Variant{
			{ID: "control-a", TrialID: "trial-01", Kind: defectbench.KindControl, Category: "correct-control", Provenance: defectbench.ProvenanceFixture, SourceDigest: strings.Repeat("a", 64), SUTManifestDigest: controlDigest},
			{ID: "mutant-a", TrialID: "trial-02", Kind: defectbench.KindDefect, RootCauseID: "root.fixture", Category: "agreement", Provenance: defectbench.ProvenanceSemanticMutant, SourceDigest: strings.Repeat("b", 64), SUTManifestDigest: mutantDigest},
			{ID: "mutant-b", TrialID: "trial-03", Kind: defectbench.KindDefect, RootCauseID: "root.fixture", Category: "agreement", Provenance: defectbench.ProvenanceSemanticMutant, SourceDigest: strings.Repeat("c", 64), SUTManifestDigest: mutantDigest},
		},
	}
	return manifest, control, mutant
}

func runFixtureCampaign(t *testing.T, manifest driver.Manifest, mutant bool) campaign.Report {
	t.Helper()
	profile := fixtureProfile()
	profileDigest, err := coverage.Digest(profile)
	if err != nil {
		t.Fatal(err)
	}
	suite := testplan.Suite{
		Version: testplan.Version, ID: "fixture-suite", ProfileID: profile.ID, ProfileDigest: profileDigest,
		MaxTotalRuns: 1, MaxTotalDecisions: 2,
		Plans: []testplan.Plan{{
			ID: "two-commits", Targets: []string{"transition.commit"},
			Stimuli: []testplan.Input{
				{Kind: core.EventCampaign, Target: "n1"},
				{Kind: core.EventCampaign, Target: "n1"},
			},
			Search: testplan.Search{Strategy: explore.StrategyDFS, Config: explore.Config{
				Runs: 1, BudgetPerRun: 2, DecisionBudget: 2,
			}},
		}},
	}
	report, err := campaign.Run(context.Background(), profile, manifest, nil, suite,
		func() (adapter.Adapter, error) { return &fixtureAdapter{mutant: mutant}, nil }, campaign.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

type fixtureAdapter struct {
	mutant  bool
	commits int
}

func (*fixtureAdapter) Protocol() string                  { return "fixture" }
func (*fixtureAdapter) Nodes() []string                   { return []string{"n1"} }
func (current *fixtureAdapter) Snapshot() any             { return map[string]int{"commits": current.commits} }
func (*fixtureAdapter) CheckConformance() error           { return nil }
func (*fixtureAdapter) Enabled(core.Event) (bool, string) { return true, "" }
func (current *fixtureAdapter) Apply(_ context.Context, event core.Event) (core.ApplyResult, error) {
	result := core.ApplyResult{Status: core.StatusApplied}
	if event.Kind != core.EventCampaign {
		return result, nil
	}
	current.commits++
	value := "v1"
	if current.mutant && current.commits > 1 {
		value = "v2"
	}
	result.Observations = []core.Observation{{
		Kind: "commit", Label: "commit-observed", Node: "n1", Value: value,
		Evidence: map[string]string{"index": "1"},
	}}
	return result, nil
}

func fixtureProfile() coverage.Profile {
	predicate := coverage.TracePredicate{
		EventKind: core.EventCampaign, ObservationKind: "commit", ObservationLabel: "commit-observed",
	}
	return coverage.Profile{
		Version: coverage.ProfileVersion, ID: "fixture-profile", Protocol: "fixture", Nodes: []string{"n1"},
		Coverage: coverage.CoverageDefinition{
			Weights: map[string]float64{"transition": 1},
			Obligations: []coverage.Obligation{{
				ID: "transition.commit", Category: "transition", Description: "observe a commit",
				Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}},
				Monitors: []string{"agreement"}, Risk: coverage.RiskHigh, Status: coverage.StatusSupported,
			}},
		},
		Threshold: coverage.Threshold{Score: 80, MinCategory: 0.5},
	}
}

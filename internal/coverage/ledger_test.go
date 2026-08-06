package coverage_test

import (
	"math"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

func TestLedgerAccumulatesStrongEvidenceAcrossRuns(t *testing.T) {
	first := obligation("first", "first", coverage.StatusSupported)
	first.Risk = coverage.RiskLow
	second := obligation("second", "second", coverage.StatusSupported)
	second.Risk = coverage.RiskHigh
	unsupported := obligation("unsupported", "never", coverage.StatusUnsupported)
	profile := v2Profile(first, second, unsupported)
	manifest := driver.Manifest{Driver: "fixture", SUT: "fixture", SUTVersion: "v1"}
	ledger, err := coverage.NewLedger(profile, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Summary().Total != 3 || ledger.Summary().Score != 0 || len(ledger.Debts()) != 2 {
		t.Fatalf("unexpected initial ledger: %+v", ledger)
	}
	if got := ledger.Debts()[0].Obligation.ID; got != "second" {
		t.Fatalf("risk-first debt = %q, want second", got)
	}

	checked := oracle.Result{Checked: []string{"trace-integrity"}}
	if err := ledger.AddRun(profile, coverage.RunEvidence{
		ID: "run-1", Scenario: "first-only", Targets: []string{"first"},
		Trace:  []core.TraceRecord{{Step: 1, Observations: []core.Observation{{Label: "first"}}}},
		Oracle: checked, ReplayStable: true, Conformant: true,
		Manifest: manifest,
	}); err != nil {
		t.Fatal(err)
	}
	if ledger.Summary().Covered != 1 || math.Abs(ledger.Summary().Score-100.0/3.0) > 1e-9 {
		t.Fatalf("first run did not update fixed denominator: %+v", ledger.Summary())
	}
	firstReport := ledger.Report()
	if firstReport.Entries[0].Status != coverage.LedgerCovered || firstReport.Entries[0].CoveredBy.RunID != "run-1" {
		t.Fatalf("missing first witness reference: %+v", firstReport.Entries[0])
	}

	if err := ledger.AddRun(profile, coverage.RunEvidence{
		ID: "run-2", Scenario: "second-only", Targets: []string{"second"},
		Trace:  []core.TraceRecord{{Step: 1, Observations: []core.Observation{{Label: "second"}}}},
		Oracle: checked, ReplayStable: true, Conformant: true,
		Manifest: manifest,
	}); err != nil {
		t.Fatal(err)
	}
	if ledger.Summary().Covered != 2 || ledger.Summary().Total != 3 || ledger.Summary().Unsupported != 1 ||
		math.Abs(ledger.Summary().Score-200.0/3.0) > 1e-9 {
		t.Fatalf("suite coverage was not accumulated: %+v", ledger.Summary())
	}
	if len(ledger.Debts()) != 0 {
		t.Fatalf("covered obligations remained actionable debt: %+v", ledger.Debts())
	}
}

func TestLedgerRejectsProfileMutationUnknownTargetsAndDuplicateRuns(t *testing.T) {
	profile := v2Profile(obligation("first", "first", coverage.StatusSupported))
	ledger, err := coverage.NewLedger(profile, driver.Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	base := coverage.RunEvidence{ID: "run", Targets: []string{"unknown"}}
	if err := ledger.AddRun(profile, base); err == nil {
		t.Fatal("ledger accepted an unknown target")
	}
	base.Targets = []string{"first"}
	base.Oracle = oracle.Result{Checked: []string{"trace-integrity"}}
	if err := ledger.AddRun(profile, base); err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddRun(profile, base); err == nil {
		t.Fatal("ledger accepted a duplicate run ID")
	}

	mutated := profile
	mutated.Coverage.Obligations = append([]coverage.Obligation(nil), profile.Coverage.Obligations...)
	mutated.Coverage.Obligations[0].Description = "agent changed the denominator"
	if err := ledger.AddRun(mutated, coverage.RunEvidence{ID: "mutated"}); err == nil {
		t.Fatal("ledger accepted evidence against a mutated profile")
	}
	if err := ledger.AddRun(profile, coverage.RunEvidence{
		ID: "wrong-driver", Manifest: driver.Manifest{Driver: "different"},
	}); err == nil {
		t.Fatal("ledger accepted evidence from a different driver manifest")
	}
}

func TestLedgerReportsAndDebtsAreDetachedSnapshots(t *testing.T) {
	item := obligation("first", "first", coverage.StatusSupported)
	item.Evidence.Reach[0].ObservationEvidence = map[string]string{"source": "trusted"}
	profile := v2Profile(item)
	ledger, err := coverage.NewLedger(profile, driver.Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	report := ledger.Report()
	report.Entries[0].Status = coverage.LedgerCovered
	report.Summary.Score = 100
	debts := ledger.Debts()
	debts[0].Obligation.ID = "agent-replaced-id"
	debts[0].Obligation.Evidence.Reach[0].ObservationEvidence["source"] = "agent"

	trusted := ledger.Report()
	if trusted.Entries[0].Status != coverage.LedgerUncovered || trusted.Summary.Score != 0 {
		t.Fatalf("mutating a report changed the trusted ledger: %+v", trusted)
	}
	if got := ledger.Debts()[0].Obligation.ID; got != "first" {
		t.Fatalf("mutating debt changed the trusted ledger: %q", got)
	}
	if got := ledger.Debts()[0].Obligation.Evidence.Reach[0].ObservationEvidence["source"]; got != "trusted" {
		t.Fatalf("mutating nested debt evidence changed trusted value to %q", got)
	}
}

func TestProfileDigestIsDeterministicAcrossMapInsertionOrder(t *testing.T) {
	left := v2Profile(obligation("one", "seen", coverage.StatusSupported))
	right := left
	left.Coverage.Weights = map[string]float64{"transition": 1}
	right.Coverage.Weights = make(map[string]float64)
	right.Coverage.Weights["transition"] = 1
	leftDigest, err := coverage.Digest(left)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := coverage.Digest(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("canonical profile digests differ: %s != %s", leftDigest, rightDigest)
	}
}

func TestLedgerRequiresMatchingFamilySemanticMatcher(t *testing.T) {
	item := obligation("semantic", "seen", coverage.StatusSupported)
	item.Evidence.Reach[0].Semantic = &coverage.SemanticPredicate{Domain: "family-v1", Relation: "role"}
	item.Evidence.Observe[0].Semantic = &coverage.SemanticPredicate{Domain: "family-v1", Relation: "role"}
	profile := v2Profile(item)
	if _, err := coverage.NewLedger(profile, driver.Manifest{}); err == nil {
		t.Fatal("ledger accepted semantic obligations without a Family matcher")
	}
	if _, err := coverage.NewLedger(profile, driver.Manifest{}, coverage.WithSemanticMatcher(fixtureSemanticMatcher{id: "other"})); err == nil {
		t.Fatal("ledger accepted a matcher for a different semantic domain")
	}
	if _, err := coverage.NewLedger(profile, driver.Manifest{}, coverage.WithSemanticMatcher(fixtureSemanticMatcher{id: "family-v1"})); err != nil {
		t.Fatalf("ledger rejected matching Family matcher: %v", err)
	}
}

type fixtureSemanticMatcher struct{ id string }

func (matcher fixtureSemanticMatcher) ID() string                                      { return matcher.id }
func (fixtureSemanticMatcher) Match(coverage.SemanticPredicate, core.TraceRecord) bool { return true }

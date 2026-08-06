package campaign_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/explore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

func TestCampaignReplaysRunsUpdatesLedgerAndSkipsSatisfiedPlan(t *testing.T) {
	profile := campaignProfile()
	digest, err := coverage.Digest(profile)
	if err != nil {
		t.Fatal(err)
	}
	plan := func(id string) testplan.Plan {
		return testplan.Plan{
			ID: id, Targets: []string{"transition.campaign"},
			Stimuli: []testplan.Input{{Kind: core.EventCampaign, Target: "n1"}},
			Search: testplan.Search{
				Strategy: explore.StrategyDFS,
				Config:   explore.Config{Runs: 1, BudgetPerRun: 1, DecisionBudget: 1},
			},
		}
	}
	suite := testplan.Suite{
		Version: testplan.Version, ID: "campaign-suite", ProfileID: profile.ID, ProfileDigest: digest,
		MaxTotalRuns: 2, MaxTotalDecisions: 2,
		Plans: []testplan.Plan{plan("first"), plan("redundant")},
	}
	manifest := driver.Manifest{Driver: "fixture", SUT: "fixture", SUTVersion: "v1"}
	report, err := campaign.Run(
		context.Background(), profile, manifest, nil, suite,
		func() (adapter.Adapter, error) { return &campaignAdapter{}, nil }, campaign.Options{Artifact: "report.json"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Initial.Covered != 0 || report.Final.Covered != 1 || report.Final.Score != 100 || report.Final.Debt != 0 {
		t.Fatalf("campaign did not accumulate strong evidence: %+v", report.Final)
	}
	if report.ChargedRuns != 1 || report.ChargedDecisions != 1 || len(report.Ledger.Runs) != 1 {
		t.Fatalf("campaign charges are wrong: runs=%d decisions=%d ledger=%d", report.ChargedRuns, report.ChargedDecisions, len(report.Ledger.Runs))
	}
	if report.Version != campaign.ReportVersion || report.Cost.Primary.SetupAttempts != 1 ||
		report.Cost.Primary.MeasurementEvents != 1 || report.Cost.Primary.WorkUnits <= report.ChargedDecisions {
		t.Fatalf("primary execution cost did not include repeated setup work: %+v", report.Cost.Primary)
	}
	if report.Cost.Replay.SetupAttempts != 1 || report.Cost.Replay.MeasurementEvents != 1 ||
		report.Cost.Replay.WorkUnits <= 1 {
		t.Fatalf("replay cost was not reported separately: %+v", report.Cost.Replay)
	}
	first := report.Plans[0].Runs[0]
	if !first.ReplayStable || !first.AcceptedEvidence || len(first.NewlyCovered) != 1 || first.NewlyCovered[0] != "transition.campaign" {
		t.Fatalf("first run lacks replayed coverage evidence: %+v", first)
	}
	if !report.Plans[1].Skipped || report.Plans[1].SkipReason == "" {
		t.Fatalf("satisfied redundant plan was not skipped: %+v", report.Plans[1])
	}
	entry := report.Ledger.Entries[0]
	if entry.CoveredBy == nil || entry.CoveredBy.RunID != "campaign-suite/first/run-1" || entry.Attempts != 1 {
		t.Fatalf("ledger witness/attempt audit is incomplete: %+v", entry)
	}
	reconstructed := append([]core.TraceRecord(nil), report.Plans[0].SetupTrace...)
	reconstructed = append(reconstructed, first.Explorer.Trace...)
	got, err := core.CanonicalTraceDigest(reconstructed)
	if err != nil {
		t.Fatal(err)
	}
	if got != entry.CoveredBy.TraceDigest {
		t.Fatalf("stored setup + measurement traces digest to %s, Ledger references %s", got, entry.CoveredBy.TraceDigest)
	}
}

func TestCampaignRecordsRuntimePlanFailureWithoutChangingLedger(t *testing.T) {
	profile := campaignProfile()
	digest, _ := coverage.Digest(profile)
	suite := testplan.Suite{
		Version: testplan.Version, ID: "failed-suite", ProfileID: profile.ID, ProfileDigest: digest,
		MaxTotalRuns: 1, MaxTotalDecisions: 1,
		Plans: []testplan.Plan{{
			ID: "missing-message", Targets: []string{"transition.campaign"},
			Prepare: []testplan.Action{{
				Op: testplan.OpExecute, Match: &scenario.Selector{Kind: core.EventMessage},
			}},
			Stimuli: []testplan.Input{{Kind: core.EventCampaign, Target: "n1"}},
			Search: testplan.Search{
				Strategy: explore.StrategyDFS,
				Config:   explore.Config{Runs: 1, BudgetPerRun: 1, DecisionBudget: 1},
			},
		}},
	}
	report, err := campaign.Run(
		context.Background(), profile, driver.Manifest{}, nil, suite,
		func() (adapter.Adapter, error) { return &campaignAdapter{}, nil }, campaign.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Plans[0].ExecutionError == "" || report.Final.Covered != 0 || report.Final.Debt != 1 ||
		report.ChargedRuns != 0 || len(report.Ledger.Runs) != 0 {
		t.Fatalf("runtime-invalid proposal affected trusted coverage state: %+v", report)
	}
	if report.Cost.Primary.SetupAttempts != 1 || report.Cost.Primary.SetupSteps == 0 ||
		report.Cost.Primary.WorkUnits == 0 || report.Cost.Primary.MeasurementEvents != 0 {
		t.Fatalf("failed setup was treated as free work: %+v", report.Cost.Primary)
	}
}

type campaignAdapter struct{ applied int }

func (*campaignAdapter) Protocol() string                  { return "fixture" }
func (*campaignAdapter) Nodes() []string                   { return []string{"n1"} }
func (current *campaignAdapter) Snapshot() any             { return map[string]int{"applied": current.applied} }
func (*campaignAdapter) CheckConformance() error           { return nil }
func (*campaignAdapter) Enabled(core.Event) (bool, string) { return true, "" }
func (current *campaignAdapter) Apply(_ context.Context, event core.Event) (core.ApplyResult, error) {
	current.applied++
	result := core.ApplyResult{Status: core.StatusApplied}
	if event.Kind == core.EventCampaign {
		result.Observations = []core.Observation{{Kind: "transition", Label: "covered", Node: event.Target}}
	}
	return result, nil
}

func campaignProfile() coverage.Profile {
	predicate := coverage.TracePredicate{EventKind: core.EventCampaign, ObservationLabel: "covered"}
	return coverage.Profile{
		Version: coverage.ProfileVersion, ID: "campaign-profile", Protocol: "fixture", Nodes: []string{"n1"},
		Coverage: coverage.CoverageDefinition{
			Weights: map[string]float64{"transition": 1},
			Obligations: []coverage.Obligation{{
				ID: "transition.campaign", Category: "transition", Description: "campaign",
				Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}},
				Monitors: []string{"trace-integrity"}, Risk: coverage.RiskHigh, Status: coverage.StatusSupported,
			}},
		},
		Threshold: coverage.Threshold{Score: 80, MinCategory: 0.5},
	}
}

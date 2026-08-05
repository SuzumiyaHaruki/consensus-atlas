package agentcampaign_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/agentcampaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/explore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

func TestCoordinatorRejectsInvalidProposalThenClosesCoverageLoop(t *testing.T) {
	profile := fixtureProfile("covered")
	invalid := proposal("invalid", "unknown")
	valid := proposal("valid", "target")
	planner := agentcampaign.ScriptedPlanner{Proposals: []*agentcampaign.Proposal{invalid, valid}}
	report, err := coordinate(t, profile, planner, agentcampaign.Config{
		ID: "fixture-agent", MaxAttempts: 3, MaxNoProgress: 2,
		MaxTotalRuns: 2, MaxTotalDecisions: 2, MaxTotalTokens: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != agentcampaign.StatusComplete || report.Campaign.Final.Covered != 1 || report.Campaign.Final.Debt != 0 {
		t.Fatalf("coordinator did not close the coverage loop: %+v", report)
	}
	if len(report.Rounds) != 2 || report.Rounds[0].Finding.Code != agentcampaign.FindingProposalRejected ||
		report.Rounds[1].Finding.Code != agentcampaign.FindingProgress {
		t.Fatalf("unexpected mechanical feedback sequence: %+v", report.Rounds)
	}
	if report.Rounds[1].Execution == nil || report.Rounds[1].Execution.Runs != 1 ||
		report.Rounds[1].Execution.Decisions != 1 {
		t.Fatalf("valid proposal did not execute through campaign Session: %+v", report.Rounds[1])
	}
	if err := agentcampaign.VerifyBlackboard(report.Blackboard); err != nil {
		t.Fatal(err)
	}
	if len(report.Blackboard.Records) != 9 {
		t.Fatalf("blackboard records = %d, want 9 append-only request/audit/proposal/finding/execution records", len(report.Blackboard.Records))
	}
	tampered := report.Blackboard
	tampered.Records = append([]agentcampaign.BlackboardRecord(nil), report.Blackboard.Records...)
	tampered.Records[0].Payload = append([]byte(nil), tampered.Records[0].Payload...)
	tampered.Records[0].Payload[0] ^= 1
	if err := agentcampaign.VerifyBlackboard(tampered); err == nil {
		t.Fatal("tampered append-only blackboard verified")
	}
}

func TestCoordinatorDetectsRenamedDuplicateAndStopsAfterNoProgress(t *testing.T) {
	profile := fixtureProfile("never")
	first := proposal("first", "target")
	second := proposal("second", "target")
	first.Plan.Stimuli[0] = testplan.Input{Kind: core.EventPropose, Target: "n1", Payload: []byte(`{"value":"first"}`)}
	second.Plan.Stimuli[0] = testplan.Input{Kind: core.EventPropose, Target: "n1", Payload: []byte(`{"value":"renamed-opaque-data"}`)}
	second.Plan.Search.Config.Seed = 99
	planner := agentcampaign.ScriptedPlanner{Proposals: []*agentcampaign.Proposal{first, second}}
	report, err := coordinate(t, profile, planner, agentcampaign.Config{
		ID: "duplicate-agent", MaxAttempts: 4, MaxNoProgress: 2,
		MaxTotalRuns: 4, MaxTotalDecisions: 4, MaxTotalTokens: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != agentcampaign.StatusNoProgress || len(report.Rounds) != 2 ||
		report.Rounds[0].Finding.Code != agentcampaign.FindingNoProgress ||
		report.Rounds[1].Finding.Code != agentcampaign.FindingDuplicateProposal {
		t.Fatalf("duplicate/no-progress policy failed: status=%s rounds=%+v", report.Status, report.Rounds)
	}
	if report.Campaign.ChargedRuns != 1 || report.Campaign.Ledger.Entries[0].Attempts != 1 {
		t.Fatalf("duplicate proposal consumed runtime or attempt credit: %+v", report.Campaign)
	}
}

func TestCoordinatorRejectsProposalBeyondRemainingBudget(t *testing.T) {
	profile := fixtureProfile("never")
	over := proposal("over-budget", "target")
	over.Plan.Search.Config = explore.Config{Runs: 2, BudgetPerRun: 1, DecisionBudget: 2}
	report, err := coordinate(t, profile, agentcampaign.ScriptedPlanner{Proposals: []*agentcampaign.Proposal{over}}, agentcampaign.Config{
		ID: "budget-agent", MaxAttempts: 1, MaxNoProgress: 1,
		MaxTotalRuns: 1, MaxTotalDecisions: 1, MaxTotalTokens: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Rounds[0].Finding.Code != agentcampaign.FindingBudgetRejected || report.Campaign.ChargedRuns != 0 {
		t.Fatalf("over-budget proposal reached runtime: %+v", report)
	}
}

func TestCoordinatorRejectsReusedPlanIDWithoutAbortingSession(t *testing.T) {
	profile := fixtureProfile("never")
	first := proposal("first", "target")
	second := proposal("second", "target")
	second.Plan.ID = first.Plan.ID
	second.Plan.Search.Config.Seed = 99
	second.Plan.Prepare = []testplan.Action{{Op: testplan.OpAdvance, Ticks: 1}}
	report, err := coordinate(t, profile, agentcampaign.ScriptedPlanner{Proposals: []*agentcampaign.Proposal{first, second}}, agentcampaign.Config{
		ID: "plan-id-agent", MaxAttempts: 2, MaxNoProgress: 2,
		MaxTotalRuns: 2, MaxTotalDecisions: 2, MaxTotalTokens: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rounds) != 2 || report.Rounds[1].Finding.Code != agentcampaign.FindingProposalRejected {
		t.Fatalf("reused plan id was not mechanically rejected: %+v", report.Rounds)
	}
	if report.Campaign.ChargedRuns != 1 || len(report.Campaign.Plans) != 1 {
		t.Fatalf("reused plan id reached or aborted the campaign session: %+v", report.Campaign)
	}
}

func coordinate(t *testing.T, profile coverage.Profile, planner agentcampaign.Planner, config agentcampaign.Config) (agentcampaign.Report, error) {
	t.Helper()
	return agentcampaign.Coordinate(
		context.Background(), profile, driver.Manifest{Driver: "fixture", SUT: "fixture", SUTVersion: "v1"}, nil,
		config, planner, func() (adapter.Adapter, error) { return &fixtureAdapter{}, nil }, campaign.Options{},
	)
}

func proposal(id, target string) *agentcampaign.Proposal {
	return &agentcampaign.Proposal{
		Version: agentcampaign.Version, ID: id,
		Plan: testplan.Plan{
			ID: id, Targets: []string{target},
			Stimuli: []testplan.Input{{Kind: core.EventCampaign, Target: "n1"}},
			Search: testplan.Search{
				Strategy: explore.StrategyDFS,
				Config:   explore.Config{Runs: 1, BudgetPerRun: 1, DecisionBudget: 1},
			},
		},
	}
}

type fixtureAdapter struct{}

func (*fixtureAdapter) Protocol() string                  { return "fixture" }
func (*fixtureAdapter) Nodes() []string                   { return []string{"n1"} }
func (*fixtureAdapter) Snapshot() any                     { return map[string]bool{"stable": true} }
func (*fixtureAdapter) CheckConformance() error           { return nil }
func (*fixtureAdapter) Enabled(core.Event) (bool, string) { return true, "" }
func (*fixtureAdapter) Apply(_ context.Context, event core.Event) (core.ApplyResult, error) {
	result := core.ApplyResult{Status: core.StatusApplied}
	if event.Kind == core.EventCampaign {
		result.Observations = []core.Observation{{Kind: "transition", Label: "covered", Node: event.Target}}
	}
	return result, nil
}

func fixtureProfile(label string) coverage.Profile {
	predicate := coverage.TracePredicate{EventKind: core.EventCampaign, ObservationLabel: label}
	return coverage.Profile{
		Version: coverage.ProfileVersion, ID: "agent-profile", Protocol: "fixture", Nodes: []string{"n1"},
		Coverage: coverage.CoverageDefinition{
			Weights: map[string]float64{"transition": 1},
			Obligations: []coverage.Obligation{{
				ID: "target", Category: "transition", Description: "target",
				Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}},
				Monitors: []string{"trace-integrity"}, Risk: coverage.RiskHigh, Status: coverage.StatusSupported,
			}},
		},
		Threshold: coverage.Threshold{Score: 80, MinCategory: 0.5},
	}
}

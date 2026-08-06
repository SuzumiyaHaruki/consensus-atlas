package agentcampaign_test

import (
	"context"
	"encoding/json"
	"strings"
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

func TestBlindCoordinatorHidesCandidateAndObligationDetails(t *testing.T) {
	profile := fixtureProfile("covered")
	profile.Coverage.Obligations[0].ID = "private-trigger-id"
	profile.Coverage.Obligations[0].Description = "PRIVATE_TRIGGER_TEXT must never reach planner"
	profile.Coverage.Obligations[0].Requires = []string{"PRIVATE_EVIDENCE_VALUE"}
	planner := &blindRecordingPlanner{reply: func(request agentcampaign.BlindGenerationRequest) *agentcampaign.BlindProposal {
		return blindProposal("blind-good", request.Debt[0].Ref)
	}}
	report, err := coordinateBlind(t, profile, planner, agentcampaign.Config{
		ID: "blind-fixture", MaxAttempts: 1, MaxNoProgress: 1, MaxTotalRuns: 1, MaxTotalDecisions: 1, MaxTotalTokens: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != agentcampaign.StatusComplete || report.Final.Covered != 1 || report.TrustedCampaign.Final.Covered != 1 {
		t.Fatalf("blind coordinator did not close trusted loop: %+v", report)
	}
	if len(planner.requests) != 1 || !strings.HasPrefix(planner.requests[0].Debt[0].Ref, "debt-") {
		t.Fatalf("planner did not receive exactly one opaque debt: %+v", planner.requests)
	}
	if len(planner.requests[0].Capabilities.Capabilities) != 1 || planner.requests[0].Capabilities.Capabilities[0] != "fixture-capability" {
		t.Fatalf("capability projection is not minimal: %+v", planner.requests[0].Capabilities)
	}
	encoded, err := json.Marshal(planner.requests[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-trigger-id", "PRIVATE_TRIGGER_TEXT", "PRIVATE_EVIDENCE_VALUE", "private-sut-version", "private-driver"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("blind request leaked %q: %s", forbidden, encoded)
		}
	}
	transcript, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-trigger-id", "PRIVATE_TRIGGER_TEXT", "PRIVATE_EVIDENCE_VALUE", "private-sut-version", "private-driver"} {
		if strings.Contains(string(transcript), forbidden) {
			t.Fatalf("blind transcript leaked %q: %s", forbidden, transcript)
		}
	}
	if err := agentcampaign.VerifyBlackboard(report.Blackboard); err != nil {
		t.Fatal(err)
	}
}

func TestBlindCoordinatorRejectsRawAndUnknownTargetsBeforeRuntime(t *testing.T) {
	profile := fixtureProfile("covered")
	profile.Coverage.Obligations[0].ID = "raw-obligation-id"
	planner := &blindRecordingPlanner{reply: func(request agentcampaign.BlindGenerationRequest) *agentcampaign.BlindProposal {
		if request.Attempt == 1 {
			return blindProposal("raw", "raw-obligation-id")
		}
		return blindProposal("opaque", request.Debt[0].Ref)
	}}
	report, err := coordinateBlind(t, profile, planner, agentcampaign.Config{
		ID: "blind-reject", MaxAttempts: 2, MaxNoProgress: 2, MaxTotalRuns: 1, MaxTotalDecisions: 1, MaxTotalTokens: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rounds) != 2 || report.Rounds[0].Finding.Code != agentcampaign.FindingProposalRejected || report.Rounds[1].Finding.Code != agentcampaign.FindingProgress {
		t.Fatalf("raw target did not receive bounded mechanical feedback: %+v", report.Rounds)
	}
	if report.TrustedCampaign.ChargedRuns != 1 || report.TrustedCampaign.Ledger.Entries[0].Attempts != 1 {
		t.Fatalf("raw target proposal reached trusted runtime: %+v", report.TrustedCampaign)
	}
}

func TestBlindScopeRejectsNonOpaqueIdentity(t *testing.T) {
	profile := fixtureProfile("never")
	_, err := agentcampaign.CoordinateBlind(context.Background(), profile, driver.Manifest{}, nil, agentcampaign.Config{
		ID: "blind-invalid", MaxAttempts: 1, MaxNoProgress: 1, MaxTotalRuns: 1, MaxTotalDecisions: 1, MaxTotalTokens: 1,
	}, agentcampaign.BlindScope{BenchmarkID: "bad/id", BenchmarkDigest: strings.Repeat("a", 64), TrialID: "trial"}, agentcampaign.ScriptedBlindPlanner{}, func() (adapter.Adapter, error) { return &fixtureAdapter{}, nil }, campaign.Options{})
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("unsafe blind scope accepted: %v", err)
	}
}

func TestTrustedReplayBundleReproducesBlindCampaignWithoutPlanner(t *testing.T) {
	profile := fixtureProfile("covered")
	planner := &blindRecordingPlanner{reply: func(request agentcampaign.BlindGenerationRequest) *agentcampaign.BlindProposal {
		return blindProposal("replay-plan", request.Debt[0].Ref)
	}}
	config := agentcampaign.Config{ID: "blind-replay", MaxAttempts: 1, MaxNoProgress: 1, MaxTotalRuns: 1, MaxTotalDecisions: 1, MaxTotalTokens: 1000}
	report, err := coordinateBlind(t, profile, planner, config)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := report.TrustedReplayBundle()
	if err != nil {
		t.Fatal(err)
	}
	manifest := driver.Manifest{Driver: "private-driver", SUT: "private-sut-version", SUTVersion: "private-sut-version", Capabilities: []driver.Capability{{ID: "fixture-capability", Supported: true, Detail: "PRIVATE_CAPABILITY_DETAIL"}}}
	replayed, err := agentcampaign.ReplayTrusted(context.Background(), profile, manifest, nil, bundle, func() (adapter.Adapter, error) { return &fixtureAdapter{}, nil }, campaign.Options{})
	if err != nil {
		t.Fatal(err)
	}
	left, err := json.Marshal(report.TrustedCampaign)
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(replayed)
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(right) {
		t.Fatalf("trusted replay changed campaign report\nwant %s\ngot  %s", left, right)
	}
}

func coordinateBlind(t *testing.T, profile coverage.Profile, planner agentcampaign.BlindPlanner, config agentcampaign.Config) (agentcampaign.BlindReport, error) {
	t.Helper()
	return agentcampaign.CoordinateBlind(context.Background(), profile, driver.Manifest{
		Driver: "private-driver", SUT: "private-sut-version", SUTVersion: "private-sut-version",
		Capabilities: []driver.Capability{{ID: "fixture-capability", Supported: true, Detail: "PRIVATE_CAPABILITY_DETAIL"}},
	}, nil, config, agentcampaign.BlindScope{BenchmarkID: "blind-dev", BenchmarkDigest: strings.Repeat("a", 64), TrialID: "trial-001"}, planner, func() (adapter.Adapter, error) { return &fixtureAdapter{}, nil }, campaign.Options{})
}

type blindRecordingPlanner struct {
	requests []agentcampaign.BlindGenerationRequest
	reply    func(agentcampaign.BlindGenerationRequest) *agentcampaign.BlindProposal
}

func (planner *blindRecordingPlanner) GenerateBlind(_ context.Context, request agentcampaign.BlindGenerationRequest) (*agentcampaign.BlindProposal, error) {
	planner.requests = append(planner.requests, request)
	return planner.reply(request), nil
}

func (*blindRecordingPlanner) LastBlindGenerationAudit() agentcampaign.GenerationAudit {
	return agentcampaign.GenerationAudit{Provider: "fixture", Model: "blind-recording"}
}

func blindProposal(id, ref string) *agentcampaign.BlindProposal {
	return &agentcampaign.BlindProposal{Version: agentcampaign.BlindVersion, ID: id, Plan: testplan.Plan{
		ID: id, Targets: []string{ref}, Stimuli: []testplan.Input{{Kind: core.EventCampaign, Target: "n1"}},
		Search: testplan.Search{Strategy: explore.StrategyDFS, Config: explore.Config{Runs: 1, BudgetPerRun: 1, DecisionBudget: 1}},
	}}
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
	return coverage.Profile{Version: coverage.ProfileVersion, ID: "agent-profile", Protocol: "fixture", Nodes: []string{"n1"}, Coverage: coverage.CoverageDefinition{Weights: map[string]float64{"transition": 1}, Obligations: []coverage.Obligation{{ID: "target", Category: "transition", Description: "target", Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}}, Monitors: []string{"trace-integrity"}, Risk: coverage.RiskHigh, Status: coverage.StatusSupported}}}, Threshold: coverage.Threshold{Score: 80, MinCategory: 0.5}}
}

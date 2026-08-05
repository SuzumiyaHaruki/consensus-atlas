package coverage_test

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

func TestUnsupportedObligationRemainsInDenominator(t *testing.T) {
	profile := v2Profile(
		obligation("covered", "seen", coverage.StatusSupported),
		obligation("unsupported", "seen", coverage.StatusUnsupported),
	)
	trace := []core.TraceRecord{{Step: 1, Observations: []core.Observation{{Label: "seen"}}}}
	summary := coverage.Evaluate(profile, trace, oracle.Result{Checked: []string{"trace-integrity"}}, true, true)
	if summary.Score != 50 {
		t.Fatalf("score = %v, want 50", summary.Score)
	}
	if summary.Covered != 1 || summary.Total != 2 || summary.Unsupported != 1 {
		t.Fatalf("unexpected denominator summary: %+v", summary)
	}
}

func TestRequiredCapabilitiesGateHighCoverage(t *testing.T) {
	profile := v2Profile(obligation("covered", "seen", coverage.StatusSupported))
	profile.RequiredCapabilities = []string{"messages", "natural-timeout"}
	trace := []core.TraceRecord{{Step: 1, Observations: []core.Observation{{Label: "seen"}}}}
	manifest := driver.Manifest{Capabilities: []driver.Capability{
		{ID: "messages", Supported: true},
		{ID: "natural-timeout", Supported: false},
	}}
	summary := coverage.EvaluateWithCapabilities(
		profile, trace, oracle.Result{Checked: []string{"trace-integrity"}}, true, true, manifest,
	)
	if summary.CapabilitySupport != 0.5 {
		t.Fatalf("capability support = %v, want 0.5", summary.CapabilitySupport)
	}
	if summary.High {
		t.Fatal("coverage is high despite an unsupported required capability")
	}
}

func TestStructuredEvidenceRequiresRealEventsAndOrdering(t *testing.T) {
	item := coverage.Obligation{
		ID: "persist-before-crash", Category: "transition", Description: "persist before crash",
		Evidence: coverage.EvidenceRequirement{
			Reach:   []coverage.TracePredicate{{EventKind: core.EventPersist}},
			Observe: []coverage.TracePredicate{{EventKind: core.EventCrash}},
			Orderings: []coverage.OrderingConstraint{{
				Before: coverage.TracePredicate{EventKind: core.EventPersist},
				After:  coverage.TracePredicate{EventKind: core.EventCrash},
			}},
		},
		Monitors: []string{"trace-integrity"}, Risk: coverage.RiskHigh, Status: coverage.StatusSupported,
	}
	profile := v2Profile(item)
	checked := oracle.Result{Checked: []string{"trace-integrity"}}

	forged := []core.TraceRecord{{
		Step: 1, Event: core.Event{Kind: core.EventPropose},
		Observations: []core.Observation{{Label: "persist-before-crash"}},
	}}
	if result := coverage.Evaluate(profile, forged, checked, true, true); result.Covered != 0 {
		t.Fatalf("an invented label satisfied structured evidence: %+v", result.Obligations[0])
	}

	reversed := []core.TraceRecord{
		{Step: 1, Event: core.Event{Kind: core.EventCrash}},
		{Step: 2, Event: core.Event{Kind: core.EventPersist}},
	}
	if result := coverage.Evaluate(profile, reversed, checked, true, true); result.Covered != 0 {
		t.Fatalf("reversed events satisfied ordering evidence: %+v", result.Obligations[0])
	}

	ordered := []core.TraceRecord{
		{Step: 1, Event: core.Event{Kind: core.EventPersist}},
		{Step: 2, Event: core.Event{Kind: core.EventCrash}},
	}
	result := coverage.Evaluate(profile, ordered, checked, true, true)
	if result.Covered != 1 || len(result.Obligations[0].Reference.OrderingSteps) != 1 {
		t.Fatalf("ordered evidence was not covered and audited: %+v", result.Obligations[0])
	}
}

func TestLegacyV1ProfileRemainsReplayable(t *testing.T) {
	profile := coverage.Profile{
		Version: coverage.LegacyProfileVersion, ID: "legacy", Protocol: "fixture", Nodes: []string{"n1"},
		Coverage: coverage.CoverageDefinition{
			Weights: map[string]float64{"transition": 1},
			Atoms: []coverage.Obligation{{
				ID: "legacy", Category: "transition", Description: "legacy label",
				WitnessLabel: "seen", Monitor: "trace-integrity", Status: coverage.StatusSupported,
			}},
		},
		Threshold: coverage.Threshold{Score: 80, MinCategory: 0.7},
	}
	trace := []core.TraceRecord{{Step: 1, Observations: []core.Observation{{Label: "seen"}}}}
	result := coverage.Evaluate(profile, trace, oracle.Result{Checked: []string{"trace-integrity"}}, true, true)
	if result.Covered != 1 || result.Score != 100 {
		t.Fatalf("legacy profile no longer evaluates: %+v", result)
	}
}

func TestV2RejectsLegacyAndStructuredDenominatorsTogether(t *testing.T) {
	profile := v2Profile(obligation("one", "seen", coverage.StatusSupported))
	profile.Coverage.Atoms = append([]coverage.Obligation(nil), profile.Coverage.Obligations...)
	if err := profile.Validate(); err == nil {
		t.Fatal("v2 accepted both obligations and legacy atoms")
	}
}

func TestCountAndCorrelatedOrderingEvidence(t *testing.T) {
	vote := coverage.TracePredicate{
		ObservationLabel: "vote", ObservationEvidence: map[string]string{"kind": "granted"},
	}
	before := coverage.TracePredicate{EventKind: core.EventCrash}
	after := coverage.TracePredicate{EventKind: core.EventRestart}
	item := coverage.Obligation{
		ID: "count-order", Category: "transition", Description: "count and order",
		Evidence: coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{vote}, Observe: []coverage.TracePredicate{vote},
			Counts: []coverage.CountConstraint{{Predicate: vote, AtLeast: 2, DistinctBy: "observation_evidence:term"}},
			Orderings: []coverage.OrderingConstraint{{
				Before: before, After: after, SameTarget: true,
				Without: []coverage.TracePredicate{{EventKind: core.EventCampaign}},
			}},
		},
		Monitors: []string{"trace-integrity"}, Risk: coverage.RiskHigh, Status: coverage.StatusSupported,
	}
	profile := v2Profile(item)
	checked := oracle.Result{Checked: []string{"trace-integrity"}}
	trace := []core.TraceRecord{
		{Step: 1, Observations: []core.Observation{{Label: "vote", Evidence: map[string]string{"kind": "granted", "term": "7"}}}},
		{Step: 2, Observations: []core.Observation{{Label: "vote", Evidence: map[string]string{"kind": "granted", "term": "7"}}}},
		{Step: 3, Observations: []core.Observation{{Label: "vote", Evidence: map[string]string{"kind": "granted", "term": "8"}}}},
		{Step: 4, Event: core.Event{Kind: core.EventCrash, Target: "n1"}},
		{Step: 5, Event: core.Event{Kind: core.EventRestart, Target: "n2"}},
		{Step: 6, Event: core.Event{Kind: core.EventRestart, Target: "n1"}},
	}
	result := coverage.Evaluate(profile, trace, checked, true, true)
	if result.Covered != 1 || result.Obligations[0].Reference.CountResults[0] != 2 {
		t.Fatalf("correlated count/order evidence did not cover: %+v", result.Obligations[0])
	}
	trace = append(trace[:5], core.TraceRecord{Step: 6, Event: core.Event{Kind: core.EventCampaign}}, trace[5])
	if result := coverage.Evaluate(profile, trace, checked, true, true); result.Covered != 0 {
		t.Fatalf("ordering crossed an excluded event: %+v", result.Obligations[0])
	}
}

func TestOrderingCanCorrelateTheSameFrozenMessage(t *testing.T) {
	message := func(digest string) *core.MessageEnvelope {
		return &core.MessageEnvelope{From: "n1", To: "n2", PayloadDigest: digest}
	}
	before := coverage.TracePredicate{EventKind: core.EventEmit}
	after := coverage.TracePredicate{EventKind: core.EventMessage}
	item := coverage.Obligation{
		ID: "wire", Category: "transition", Description: "same wire message",
		Evidence: coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{before}, Observe: []coverage.TracePredicate{after},
			Orderings: []coverage.OrderingConstraint{{Before: before, After: after, SameMessage: true}},
		},
		Monitors: []string{"trace-integrity"}, Risk: coverage.RiskHigh, Status: coverage.StatusSupported,
	}
	profile := v2Profile(item)
	checked := oracle.Result{Checked: []string{"trace-integrity"}}
	trace := []core.TraceRecord{
		{Step: 1, Event: core.Event{Kind: core.EventEmit, Message: message("a")}},
		{Step: 2, Event: core.Event{Kind: core.EventMessage, Message: message("b")}},
	}
	if result := coverage.Evaluate(profile, trace, checked, true, true); result.Covered != 0 {
		t.Fatal("different messages satisfied a same-message ordering")
	}
	trace = append(trace, core.TraceRecord{Step: 3, Event: core.Event{Kind: core.EventMessage, Message: message("a")}})
	if result := coverage.Evaluate(profile, trace, checked, true, true); result.Covered != 1 {
		t.Fatal("identical frozen message did not satisfy same-message ordering")
	}
}

func TestHostCutpointRequiresDeclaredOperations(t *testing.T) {
	cutpoint := coverage.TracePredicate{EventKind: core.EventCrash, HostCutpoint: "persist-before-sync"}
	item := coverage.Obligation{
		ID: "cutpoint", Category: "transition", Description: "cutpoint",
		Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{cutpoint}, Observe: []coverage.TracePredicate{cutpoint}},
		Monitors: []string{"trace-integrity"}, Risk: coverage.RiskHigh, Status: coverage.StatusSupported,
	}
	profile := v2Profile(item)
	checked := oracle.Result{Checked: []string{"trace-integrity"}}
	snapshot := func(operations ...core.EventKind) any {
		return map[string]any{"runtime": map[string]any{"nodes": map[string]any{
			"n1": map[string]any{"batch": "b1", "operations": operations},
		}}}
	}
	trace := []core.TraceRecord{
		{Step: 1, Event: core.Event{Kind: core.EventPersist, Group: "b1"}, Outcome: string(core.StatusApplied)},
		{Step: 2, Event: core.Event{Kind: core.EventCrash, Target: "n1"}, Before: snapshot(core.EventPersist)},
	}
	if result := coverage.Evaluate(profile, trace, checked, true, true); result.Covered != 0 {
		t.Fatal("cutpoint matched although the output batch declared no sync operation")
	}
	trace[1].Before = snapshot(core.EventPersist, core.EventSync)
	if result := coverage.Evaluate(profile, trace, checked, true, true); result.Covered != 1 {
		t.Fatal("declared persist-before-sync cutpoint was not matched")
	}
}

func v2Profile(items ...coverage.Obligation) coverage.Profile {
	return coverage.Profile{
		Version: coverage.ProfileVersion, ID: "test", Protocol: "test", Nodes: []string{"n1"},
		Coverage: coverage.CoverageDefinition{
			Weights: map[string]float64{"transition": 1}, Obligations: items,
		},
		Threshold: coverage.Threshold{Score: 80, MinCategory: 0.7},
	}
}

func obligation(id, label, status string) coverage.Obligation {
	predicate := coverage.TracePredicate{ObservationLabel: label}
	return coverage.Obligation{
		ID: id, Category: "transition", Description: id,
		Evidence: coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate},
		},
		Monitors: []string{"trace-integrity"}, Risk: coverage.RiskStandard, Status: status,
	}
}

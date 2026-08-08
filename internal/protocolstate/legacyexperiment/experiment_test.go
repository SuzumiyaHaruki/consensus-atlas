package legacyexperiment_test

import (
	"errors"
	"math"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	legacyexperiment "github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate/legacyexperiment"
)

type fixtureProjector struct{}

func (fixtureProjector) ID() string { return "fixture-v1" }

func (fixtureProjector) IsSample(record core.TraceRecord) bool {
	return record.Event.Kind == core.EventAcknowledge
}

func (fixtureProjector) Project(snapshot any) (any, string, error) {
	key, ok := snapshot.(string)
	if !ok {
		return nil, "", errors.New("snapshot is not a string")
	}
	return map[string]string{"key": key}, key, nil
}

func TestAggregateUsesSharedRootAndDecisionBudget(t *testing.T) {
	runs := []legacyexperiment.MeasuredRun{
		{Run: 1, DecisionCount: 2, InitialSnapshot: "a", Trace: []core.TraceRecord{
			{Step: 11, Event: core.Event{Kind: core.EventAcknowledge}, After: "b"},
			{Step: 12, Event: core.Event{Kind: core.EventPersist}, After: "ignored"},
		}},
		{Run: 2, DecisionCount: 1, InitialSnapshot: "a", Trace: []core.TraceRecord{
			{Step: 11, Event: core.Event{Kind: core.EventAcknowledge}, After: "c"},
		}},
	}
	summary, err := legacyexperiment.Aggregate(runs, fixtureProjector{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.InitialStateKey != "a" || summary.TotalDecisions != 3 ||
		summary.ProtocolSamples != 2 || summary.UniqueStates != 3 {
		t.Fatalf("unexpected aggregate: %#v", summary)
	}
	if summary.PrefixArea != 7 {
		t.Fatalf("prefix area = %d, want 2+2+3=7", summary.PrefixArea)
	}
	if want := 7.0 / 9.0; math.Abs(summary.SelfNormalizedArea-want) > 1e-12 {
		t.Fatalf("self-normalized area = %f, want %f", summary.SelfNormalizedArea, want)
	}
	if len(summary.Curve) != 3 || !summary.Curve[0].NewState ||
		summary.Curve[1].NewState || !summary.Curve[2].NewState {
		t.Fatalf("unexpected curve: %#v", summary.Curve)
	}
	if len(summary.States) != 3 || summary.States[0].FirstGlobalDecision != 0 ||
		summary.States[2].FirstGlobalDecision != 3 {
		t.Fatalf("unexpected witnesses: %#v", summary.States)
	}
}

func TestAggregateRejectsDifferentMeasurementRoots(t *testing.T) {
	_, err := legacyexperiment.Aggregate([]legacyexperiment.MeasuredRun{
		{Run: 1, InitialSnapshot: "a"},
		{Run: 2, InitialSnapshot: "b"},
	}, fixtureProjector{})
	if err == nil {
		t.Fatal("different measurement roots were accepted")
	}
}

func TestAggregateRejectsUnchargedTraceRecords(t *testing.T) {
	_, err := legacyexperiment.Aggregate([]legacyexperiment.MeasuredRun{{
		Run: 1, DecisionCount: 0, InitialSnapshot: "a",
		Trace: []core.TraceRecord{{Step: 1, Event: core.Event{Kind: core.EventPersist}}},
	}}, fixtureProjector{})
	if err == nil {
		t.Fatal("trace record without a charged decision was accepted")
	}
}

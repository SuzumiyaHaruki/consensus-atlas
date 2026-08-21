package protocolstate_test

import (
	"math"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

func TestAggregateUsesChargedDecisionsAndOptionalSamples(t *testing.T) {
	a := protocolstate.Sample{Key: "a", State: "state-a"}
	b := protocolstate.Sample{Step: 11, Key: "b", State: "state-b"}
	c := protocolstate.Sample{Step: 11, Key: "c", State: "state-c"}
	runs := []protocolstate.MeasuredRun{
		{Run: 1, Initial: a, Decisions: []protocolstate.MeasuredDecision{
			{Step: 11, Sample: &b}, {Step: 12},
		}},
		{Run: 2, Initial: a, Decisions: []protocolstate.MeasuredDecision{{Step: 11, Sample: &c}}},
	}
	summary, err := protocolstate.Aggregate("fixture-v1", runs)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalDecisions != 3 || summary.ProtocolSamples != 2 || summary.UniqueStates != 3 {
		t.Fatalf("unexpected aggregate: %#v", summary)
	}
	if summary.PrefixArea != 7 || math.Abs(summary.SelfNormalizedArea-7.0/9.0) > 1e-12 {
		t.Fatalf("unexpected areas: %#v", summary)
	}
	if len(summary.Curve) != 3 || !summary.Curve[0].NewState ||
		summary.Curve[1].NewState || !summary.Curve[2].NewState {
		t.Fatalf("unexpected curve: %#v", summary.Curve)
	}
}

func TestAggregateRejectsUnalignedSample(t *testing.T) {
	sample := protocolstate.Sample{Step: 2, Key: "state"}
	_, err := protocolstate.Aggregate("fixture-v1", []protocolstate.MeasuredRun{{
		Run: 1, Initial: protocolstate.Sample{Key: "initial"},
		Decisions: []protocolstate.MeasuredDecision{{Step: 1, Sample: &sample}},
	}})
	if err == nil {
		t.Fatal("sample from a different decision step was accepted")
	}
}

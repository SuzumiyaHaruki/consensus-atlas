package coverage_test

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

func TestUnsupportedAtomRemainsInDenominator(t *testing.T) {
	profile := coverage.Profile{
		Version: 1, ID: "test", Protocol: "test", Nodes: []string{"n1"},
		Coverage: coverage.CoverageDefinition{
			Weights: map[string]float64{"transition": 1},
			Atoms: []coverage.Atom{
				{ID: "covered", Category: "transition", WitnessLabel: "seen", Status: "supported"},
				{ID: "unsupported", Category: "transition", WitnessLabel: "seen", Status: "unsupported"},
			},
		},
		Threshold: coverage.Threshold{Score: 80, MinCategory: 0.7},
	}
	trace := []core.TraceRecord{{Step: 1, Observations: []core.Observation{{Label: "seen"}}}}
	summary := coverage.Evaluate(profile, trace, oracle.Result{}, true, true)
	if summary.Score != 50 {
		t.Fatalf("score = %v, want 50", summary.Score)
	}
}

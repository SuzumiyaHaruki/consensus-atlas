package coverage

import (
	"math"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
)

func TestSummaryScoreHasStableFloatingPointOrder(t *testing.T) {
	weights := map[string]float64{"delta": 0.4, "beta": 0.2, "alpha": 0.1, "gamma": 0.3}
	results := []ObligationResult{
		{Obligation: Obligation{ID: "alpha", Category: "alpha", Status: StatusSupported}, Covered: true},
		{Obligation: Obligation{ID: "beta-1", Category: "beta", Status: StatusSupported}, Covered: true},
		{Obligation: Obligation{ID: "beta-2", Category: "beta", Status: StatusSupported}, Covered: true},
		{Obligation: Obligation{ID: "beta-3", Category: "beta", Status: StatusSupported}},
		{Obligation: Obligation{ID: "gamma-1", Category: "gamma", Status: StatusSupported}, Covered: true},
		{Obligation: Obligation{ID: "gamma-2", Category: "gamma", Status: StatusSupported}},
		{Obligation: Obligation{ID: "gamma-3", Category: "gamma", Status: StatusSupported}},
		{Obligation: Obligation{ID: "delta-1", Category: "delta", Status: StatusSupported}, Covered: true},
		{Obligation: Obligation{ID: "delta-2", Category: "delta", Status: StatusSupported}},
		{Obligation: Obligation{ID: "delta-3", Category: "delta", Status: StatusSupported}},
	}
	profile := Profile{Coverage: CoverageDefinition{Weights: weights}, Threshold: Threshold{Score: 100, MinCategory: 1}}
	baseline := math.Float64bits(summarize(profile, "fixture", results, driver.Manifest{}).Score)
	for iteration := 0; iteration < 256; iteration++ {
		got := math.Float64bits(summarize(profile, "fixture", results, driver.Manifest{}).Score)
		if got != baseline {
			t.Fatalf("score bits changed at iteration %d: got %x, want %x", iteration, got, baseline)
		}
	}
}

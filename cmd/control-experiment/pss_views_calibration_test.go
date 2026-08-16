package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

func TestA9e3bExistingOmnipaxosArtifactSeparatesProtocolAndControlViews(t *testing.T) {
	encoded, err := os.ReadFile(
		"../../benchmarks/experiments/agentic-episode-a9d6/omnipaxos-v2-r2/bundle.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	var bundle controlexperiment.ExecutionBundle
	if err := json.Unmarshal(encoded, &bundle); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	states := make([]psscore.State, len(bundle.CorePSS))
	for index, sample := range bundle.CorePSS {
		states[index] = sample.State
	}
	views, err := psscore.SummarizeStates(states)
	if err != nil {
		t.Fatal(err)
	}
	if views != (psscore.ViewSummary{
		Samples: 31, ProtocolStates: 4, ControlStates: 28, JointStates: 29,
	}) {
		t.Fatalf("A9d6 PSS view calibration drifted: %#v", views)
	}
}

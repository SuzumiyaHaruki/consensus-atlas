package main

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func TestPairTrialsRequiresOnePublicPair(t *testing.T) {
	manifest, err := (defectbench.BundleBenchmark{
		ID: "pair", Classification: "public-calibration-only", ProjectorID: "projector",
		Budget: defectbench.BundleBudget{MaxDecisions: 1, MaxPrimaryWorkUnits: 1},
		Variants: []defectbench.BundleVariant{
			{TrialID: "trial-control", VariantID: "official-control", Kind: defectbench.BundleKindControl, ExpectedBuildID: "official"},
			{TrialID: "trial-candidate", VariantID: "public-candidate", Kind: defectbench.BundleKindCalibration, RootCauseID: "root", ExpectedBuildID: "candidate"},
		},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	control, candidate, err := pairTrials(manifest)
	if err != nil || control != "trial-control" || candidate != "trial-candidate" {
		t.Fatalf("pair = %s/%s, %v", control, candidate, err)
	}
}

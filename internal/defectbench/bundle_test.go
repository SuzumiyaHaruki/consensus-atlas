package defectbench

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCheckedPublicCalibrationLedgerIsSelfValidating(t *testing.T) {
	manifestBytes, err := os.ReadFile("../../benchmarks/pilots/etcdraft-v2-calibration-m5.16/evaluator/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest BundleBenchmark
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	reportBytes, err := os.ReadFile("../../benchmarks/pilots/etcdraft-v2-calibration-m5.16/evaluator/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var report BundleEvaluation
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	want := BundleEvaluationSummary{
		Controls: 1, Candidates: 1, KilledCandidates: 1,
		RootCauses: 1, KilledRootCauses: 1,
	}
	if report.BenchmarkDigest != manifest.Digest || report.Summary != want ||
		report.Digest != "fd4ecc9b0ca719a0dfacefd37c5e0067885a1aa5f0f1fb6b0f6da0ae4d71e664" {
		t.Fatalf("public calibration ledger = %#v", report)
	}
}

func TestCheckedActionClassCalibrationRecordsSurvivingCandidate(t *testing.T) {
	manifestBytes, err := os.ReadFile("../../benchmarks/pilots/etcdraft-v2-action-class-random-m5.17a/evaluator/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest BundleBenchmark
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	reportBytes, err := os.ReadFile("../../benchmarks/pilots/etcdraft-v2-action-class-random-m5.17a/evaluator/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var report BundleEvaluation
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	want := BundleEvaluationSummary{Controls: 1, Candidates: 1, RootCauses: 1}
	if report.BenchmarkDigest != manifest.Digest || report.Summary != want ||
		report.Digest != "3886388d346ad436684c4bffda6198f0585427336642b12b264c052c8478cb68" {
		t.Fatalf("action-class calibration ledger = %#v", report)
	}
	if report.Results[0].Status != BundleStatusSurvived || report.Results[1].Status != BundleStatusControlPass {
		t.Fatalf("action-class calibration results = %#v", report.Results)
	}
}

func TestBundleBenchmarkOptionallyBindsCompleteConfigIdentity(t *testing.T) {
	digest := strings.Repeat("a", 64)
	manifest, err := (BundleBenchmark{
		ID: "config-bound", Classification: "public-calibration-only", ProjectorID: "projector",
		Budget: BundleBudget{MaxDecisions: 1, MaxPrimaryWorkUnits: 1},
		Variants: []BundleVariant{
			{TrialID: "control", VariantID: "official", Kind: BundleKindControl, ExpectedBuildID: "official", ExpectedConfigDigest: digest},
			{TrialID: "candidate", VariantID: "changed", Kind: BundleKindCalibration, RootCauseID: "cause", ExpectedBuildID: "changed", ExpectedConfigDigest: digest},
		},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	manifest.Variants[0].ExpectedConfigDigest = "invalid"
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "CONFIG_DIGEST_INVALID") {
		t.Fatalf("invalid config digest error = %v", err)
	}
}

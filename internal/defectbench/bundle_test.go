package defectbench

import (
	"strings"
	"testing"
)

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

func TestFreshBenchmarkRequiresMethodAndBuildEvidence(t *testing.T) {
	digest := strings.Repeat("a", 64)
	base := BundleBenchmark{
		SchemaVersion: BundleBenchmarkSchemaVersionV2,
		ID:            "fresh-boundary", Classification: "public-calibration-only", ProjectorID: "projector",
		MethodSpecDigest: digest, RequiredBundleSchema: "consensus-atlas/execution-bundle/v3",
		Budget: BundleBudget{MaxDecisions: 1, MaxPrimaryWorkUnits: 1},
		Variants: []BundleVariant{
			{TrialID: "control", VariantID: "official", Kind: BundleKindControl, ExpectedBuildID: "official"},
			{TrialID: "candidate", VariantID: "changed", Kind: BundleKindCalibration, RootCauseID: "cause", ExpectedBuildID: "changed"},
		},
	}
	if _, err := base.Seal(); err == nil || !strings.Contains(err.Error(), "BUILD_EVIDENCE_INVALID") {
		t.Fatalf("fresh manifest without build evidence error = %v", err)
	}
	base.Variants[0].ExpectedBuildAuditDigest = digest
	base.Variants[0].ExpectedBinaryDigest = digest
	base.Variants[1].ExpectedBuildAuditDigest = digest
	base.Variants[1].ExpectedBinaryDigest = digest
	sealed, err := base.Seal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateBundles(sealed, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "FRESH_EXECUTION_REQUIRED") {
		t.Fatalf("v2 manifest entered submitted-bundle evaluator: %v", err)
	}
	base.SchemaVersion = "unsupported"
	if _, err := base.Seal(); err == nil || !strings.Contains(err.Error(), "SCHEMA_MISMATCH") {
		t.Fatalf("unsupported benchmark schema error = %v", err)
	}
}

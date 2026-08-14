package defectbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

func TestCheckedBuildAuditsMatchFrozenManifest(t *testing.T) {
	for _, fixture := range []struct {
		path   string
		digest string
		binary string
	}{
		{
			path:   "../../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-audit/candidate.json",
			digest: "439d4d7b44b3786f5444d1946df1b2d7609f157087fd5323d293d33ebed4df21",
			binary: "b21039b751474c8449dd5b2a35445e0e327eb53f6b8fcb71433cda1b53793b04",
		},
		{
			path:   "../../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-audit/control.json",
			digest: "b4c2ddbe2b0a0afc34ee981eac99d36c1494f5329fc6f8a748e29d7dd447846a",
			binary: "2f707ed1be64157a20c6f41353454d3dc63700d7fcef25be792ae77f8871fb5b",
		},
	} {
		encoded, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		var audit sutbuild.Audit
		if err := json.Unmarshal(encoded, &audit); err != nil {
			t.Fatal(err)
		}
		if err := audit.Validate(); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(encoded)
		if hex.EncodeToString(digest[:]) != fixture.digest || audit.BinaryDigest != fixture.binary {
			t.Fatalf("M5.18a build audit drifted: %s", fixture.path)
		}
	}
}

func TestFreshEvaluationManifestIdentity(t *testing.T) {
	manifest, err := (BundleBenchmark{
		SchemaVersion:        BundleBenchmarkSchemaVersionV2,
		ID:                   "public-etcdraft-v2-method-evaluation-m5.18a",
		Classification:       "public-calibration-only",
		ProjectorID:          "official-etcdraft-v2/applied-prefix-digest-v1",
		MethodSpecDigest:     "ee856fbf62ff99a82cfc0c2753d3021e6ca879e6490242074b08ea28d48fdbcf",
		RequiredBundleSchema: "consensus-atlas/execution-bundle/v3",
		Budget:               BundleBudget{MaxDecisions: 96, MaxPrimaryWorkUnits: 98},
		Variants: []BundleVariant{
			{
				TrialID: "trial-calibration", VariantID: "public-command-divergence",
				Kind: BundleKindCalibration, RootCauseID: "calibration-command-data-divergence",
				ExpectedBuildID:          "sut-c9811ab0ed8e2f39",
				ExpectedBuildAuditDigest: "439d4d7b44b3786f5444d1946df1b2d7609f157087fd5323d293d33ebed4df21",
				ExpectedBinaryDigest:     "b21039b751474c8449dd5b2a35445e0e327eb53f6b8fcb71433cda1b53793b04",
			},
			{
				TrialID: "trial-control", VariantID: "official-control", Kind: BundleKindControl,
				ExpectedBuildID:          "sut-5826353327bce116",
				ExpectedBuildAuditDigest: "b4c2ddbe2b0a0afc34ee981eac99d36c1494f5329fc6f8a748e29d7dd447846a",
				ExpectedBinaryDigest:     "2f707ed1be64157a20c6f41353454d3dc63700d7fcef25be792ae77f8871fb5b",
			},
		},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	t.Logf("M5.18a manifest digest=%s", manifest.Digest)
	encoded, err := os.ReadFile("../../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/evaluator/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var frozen BundleBenchmark
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		t.Fatal(err)
	}
	if err := frozen.Validate(); err != nil {
		t.Fatal(err)
	}
	if frozen.Digest != manifest.Digest || frozen.Digest !=
		"0bf5a38fcf1e61d108860910aaa0a10f10c08503f0ed4f88280bfe04446bbce9" {
		t.Fatalf("M5.18a manifest drifted: frozen=%s fresh=%s", frozen.Digest, manifest.Digest)
	}
}

func TestCheckedFreshEvaluationReportIsSelfValidating(t *testing.T) {
	manifestBytes, err := os.ReadFile("../../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/evaluator/manifest.json")
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
	reportBytes, err := os.ReadFile("../../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/evaluator/report.json")
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
	if report.BenchmarkDigest != manifest.Digest || report.MethodSpecDigest != manifest.MethodSpecDigest ||
		report.Summary != want || report.Digest !=
		"4cac6cd0731bdb9d28b2c1d927d634e3ce0dfdc2ccfac271f55df0b5bd7ef4ee" {
		t.Fatalf("M5.18a evaluation drifted: %#v", report)
	}
	if len(report.Results) != 2 || report.Results[0].Status != BundleStatusKilled ||
		report.Results[0].Finding == nil || report.Results[0].Finding.Monitor != "agreement" ||
		report.Results[0].Finding.Step != 55 || report.Results[1].Status != BundleStatusControlPass ||
		report.Results[0].OperationHistoryDigest != report.Results[1].OperationHistoryDigest ||
		report.Results[0].MethodConfigProjectionDigest != report.Results[1].MethodConfigProjectionDigest {
		t.Fatalf("M5.18a trial evidence drifted: %#v", report.Results)
	}
}

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

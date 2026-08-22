package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func TestParseInputPreservesEqualsInPath(t *testing.T) {
	label, path, err := parseInput("agentic=/tmp/run=one.json")
	if err != nil || label != "agentic" || path != "/tmp/run=one.json" {
		t.Fatalf("parseInput = %q, %q, %v", label, path, err)
	}
}

func TestRunRequiresInput(t *testing.T) {
	if err := run(nil, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadEvidenceRejectsUnknownSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"unknown/v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadEvidence("method", path)
	if err == nil || !strings.Contains(err.Error(), "unsupported evaluator schema") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeStrictRejectsTrailingJSON(t *testing.T) {
	var value map[string]string
	err := decodeStrict([]byte(`{"a":"b"} {"c":"d"}`), &value)
	if err == nil || !strings.Contains(err.Error(), "unknown field") && !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRendersValidatedFormalEvaluation(t *testing.T) {
	report := validFormalEvaluation(t)
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "formal.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"-input", "bounded-random=" + path}, &output); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"bounded-random", "0/3", "formal-fresh", "Benchmark: `benchmark-a`"} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("output missing %q:\n%s", required, output.String())
		}
	}
}

func validFormalEvaluation(t *testing.T) defectbench.FormalFreshEvaluation {
	t.Helper()
	digest := strings.Repeat("a", 64)
	report := defectbench.FormalFreshEvaluation{
		BenchmarkID: "benchmark-a", ContractDigest: digest,
		ExposureAuditDigest: digest, MethodSpecDigest: digest,
	}
	for index, suffix := range []string{"a", "b", "c"} {
		controlTrial, candidateTrial := "control-"+suffix, "candidate-"+suffix
		report.Pairs = append(report.Pairs, defectbench.FormalPairEvaluation{
			PairID: "pair-" + suffix, RootCauseID: "root-" + suffix,
			ControlTrialID: controlTrial, CandidateTrialID: candidateTrial,
		})
		for _, result := range []defectbench.BundleTrialResult{
			{
				TrialID: controlTrial, VariantID: "control-variant-" + suffix,
				Kind: defectbench.BundleKindControl, Status: defectbench.BundleStatusControlPass,
			},
			{
				TrialID: candidateTrial, VariantID: "candidate-variant-" + suffix,
				Kind: defectbench.FormalBundleKindCandidate, RootCauseID: "root-" + suffix,
				Status: defectbench.BundleStatusSurvived,
			},
		} {
			result.BundleDigest, result.BuildID = digest, "build-"+suffix
			result.BuildAuditDigest, result.BinaryDigest = digest, digest
			result.MethodConfigProjectionDigest, result.OperationHistoryDigest = digest, digest
			result.Decisions, result.PrimaryWork, result.ReplayWork = index+1, index+2, 1
			report.Results = append(report.Results, result)
		}
	}
	sealed, err := report.Seal()
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

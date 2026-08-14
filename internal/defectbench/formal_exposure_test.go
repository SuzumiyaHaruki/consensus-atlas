package defectbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestFormalExposureAuditAcceptsExactOpaqueArtifacts(t *testing.T) {
	contract := sealFixtureContract(t)
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	viewBytes, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := AuditFormalExposure(contract, view, []FormalPublicArtifact{
		{Bytes: viewBytes},
		{Bytes: []byte(`{"trial_id":"opaque-01","strategy":"uniform","attempt":1}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Validate(); err != nil {
		t.Fatal(err)
	}
	if !audit.Passed || len(audit.Findings) != 0 || len(audit.PublicArtifacts) != 2 {
		t.Fatalf("clean audit = %#v", audit)
	}
	copyAudit := audit
	copyAudit.PublicArtifacts[0].Digest = testFormalDigest("tamper")
	if err := copyAudit.Validate(); err == nil {
		t.Fatal("tampered audit was accepted")
	}
}

func TestFormalExposureAuditRejectsPrivateAtomsWithoutEchoingThem(t *testing.T) {
	contract := sealFixtureContract(t)
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := contract.Pairs[0].PairID
	privateValue := contract.Pairs[1].Candidate.ExpectedBuildAuditDigest
	leak, err := json.Marshal(map[string]any{
		privateKey: privateValue,
		"nested":   []any{contract.Pairs[2].RootCauseID, contract.Composition.MonitorIDs[0]},
	})
	if err != nil {
		t.Fatal(err)
	}
	audit, err := AuditFormalExposure(contract, view, []FormalPublicArtifact{{Bytes: leak}})
	if err != nil {
		t.Fatal(err)
	}
	if audit.Passed || len(audit.Findings) != 1 ||
		audit.Findings[0] != (FormalExposureFinding{ArtifactIndex: 1, Code: FormalExposurePrivateAtom}) {
		t.Fatalf("leak audit = %#v", audit)
	}
	if err := audit.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	for atom := range formalContractPrivateAtoms(contract) {
		if bytes.Contains(encoded, []byte(atom)) {
			t.Fatalf("audit echoed private atom %q", atom)
		}
	}
}

func TestFormalExposureAuditClassifiesMalformedJSONAndViewMismatch(t *testing.T) {
	contract := sealFixtureContract(t)
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	view.Budget.MaxDecisions++
	view, err = view.Seal()
	if err != nil {
		t.Fatal(err)
	}
	audit, err := AuditFormalExposure(contract, view, []FormalPublicArtifact{
		{Bytes: []byte(`{"valid":true} trailing`)},
		{Bytes: []byte(`{"one":1} {"two":2}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []FormalExposureFinding{
		{Code: FormalExposureOpaqueViewMismatch},
		{ArtifactIndex: 1, Code: FormalExposureInvalidPublicJSON},
		{ArtifactIndex: 2, Code: FormalExposureInvalidPublicJSON},
	}
	if audit.Passed || !reflect.DeepEqual(audit.Findings, want) {
		t.Fatalf("invalid audit findings = %#v", audit.Findings)
	}
	if err := audit.Validate(); err != nil {
		t.Fatal(err)
	}
}

type formalExposureStageSummary struct {
	SchemaVersion            string `json:"schema_version"`
	ID                       string `json:"id"`
	ContractDigest           string `json:"contract_digest"`
	OpaqueViewDigest         string `json:"opaque_view_digest"`
	CleanAuditDigest         string `json:"clean_audit_digest"`
	LeakAuditDigest          string `json:"leak_audit_digest"`
	CleanAuditPassed         bool   `json:"clean_audit_passed"`
	LeakAuditPassed          bool   `json:"leak_audit_passed"`
	LeakFindings             int    `json:"leak_findings"`
	ExposureAuditImplemented bool   `json:"exposure_audit_implemented"`
	FreshEvaluatorWired      bool   `json:"fresh_evaluator_wired"`
	DatasetClassification    string `json:"dataset_classification"`
	FormalReady              bool   `json:"formal_ready"`
	NewModelCalls            int    `json:"new_model_calls"`
	NewSUTExecutions         int    `json:"new_sut_executions"`
	Digest                   string `json:"digest"`
}

func TestFormalExposureSummaryIsRecomputed(t *testing.T) {
	contract := sealFixtureContract(t)
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	clean, err := AuditFormalExposure(contract, view, []FormalPublicArtifact{
		{Bytes: []byte(`{"trial_id":"opaque-01","attempt":1}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	leakBytes, err := json.Marshal(map[string]string{"target": contract.Pairs[0].RootCauseID})
	if err != nil {
		t.Fatal(err)
	}
	leak, err := AuditFormalExposure(contract, view, []FormalPublicArtifact{{Bytes: leakBytes}})
	if err != nil {
		t.Fatal(err)
	}
	recomputed := formalExposureStageSummary{
		SchemaVersion: "consensus-atlas/formal-exposure-stage-summary/v1",
		ID:            "formal-exposure-audit-m5-21m", ContractDigest: contract.Digest,
		OpaqueViewDigest: view.Digest, CleanAuditDigest: clean.Digest, LeakAuditDigest: leak.Digest,
		CleanAuditPassed: clean.Passed, LeakAuditPassed: leak.Passed, LeakFindings: len(leak.Findings),
		ExposureAuditImplemented: true, FreshEvaluatorWired: false,
		DatasetClassification: "public-synthetic-contract-fixture-only",
		FormalReady:           false, NewModelCalls: 0, NewSUTExecutions: 0,
	}
	digest, err := control.CanonicalDigest(recomputed)
	if err != nil {
		t.Fatal(err)
	}
	recomputed.Digest = digest

	data, err := os.ReadFile("../../benchmarks/experiments/formal-exposure-audit-m5.21m/summary.json")
	if err != nil {
		encoded, _ := json.MarshalIndent(recomputed, "", "  ")
		t.Fatalf("read stage summary: %v\n%s", err, append(encoded, '\n'))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var archived formalExposureStageSummary
	if err := decoder.Decode(&archived); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatal("stage summary contains trailing JSON")
	}
	if !reflect.DeepEqual(archived, recomputed) {
		encoded, _ := json.MarshalIndent(recomputed, "", "  ")
		t.Fatalf("stage summary is stale; recomputed:\n%s", append(encoded, '\n'))
	}
}

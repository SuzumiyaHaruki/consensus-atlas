package defectbench

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
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

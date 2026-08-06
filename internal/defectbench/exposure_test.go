package defectbench_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func TestExposureAuditAcceptsExactBlindProjection(t *testing.T) {
	manifest, _, _ := fixtureManifest(t)
	blind, err := manifest.Blind()
	if err != nil {
		t.Fatal(err)
	}
	blindBytes, err := json.Marshal(blind)
	if err != nil {
		t.Fatal(err)
	}
	transcript := []byte(`{"version":1,"blind_scope":{"trial_id":"trial-01"},"coverage_debt":[{"ref":"debt-opaque"}]}`)
	report, err := defectbench.AuditExposure(manifest, blind, []defectbench.PublicArtifact{{Bytes: blindBytes}, {Bytes: transcript}})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Findings) != 0 || len(report.PublicArtifacts) != 2 {
		t.Fatalf("exact blind projection did not pass: %+v", report)
	}
}

func TestExposureAuditReportsPrivateLeakWithoutRepeatingIt(t *testing.T) {
	manifest, _, _ := fixtureManifest(t)
	blind, err := manifest.Blind()
	if err != nil {
		t.Fatal(err)
	}
	report, err := defectbench.AuditExposure(manifest, blind, []defectbench.PublicArtifact{{Bytes: []byte(`{"mutant-a":"also-private-as-a-key"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || len(report.Findings) != 1 || report.Findings[0].Code != defectbench.ExposurePrivateString {
		t.Fatalf("private leak was not detected: %+v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "mutant-a") {
		t.Fatalf("exposure report repeated the private value: %s", encoded)
	}
}

func TestExposureAuditRejectsAlteredBlindAndInvalidArtifact(t *testing.T) {
	manifest, _, _ := fixtureManifest(t)
	blind, err := manifest.Blind()
	if err != nil {
		t.Fatal(err)
	}
	blind.Budget.MaxRuns++
	report, err := defectbench.AuditExposure(manifest, blind, []defectbench.PublicArtifact{{Bytes: []byte(`{`)}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || len(report.Findings) != 2 ||
		report.Findings[0].Code != defectbench.ExposureBlindManifestMismatch ||
		report.Findings[1].Code != defectbench.ExposureInvalidPublicJSON {
		t.Fatalf("altered blind or invalid artifact was accepted: %+v", report)
	}
}

func TestManifestRejectsPrivateVariantIdentityAsTrialID(t *testing.T) {
	manifest, _, _ := fixtureManifest(t)
	manifest.Variants[1].TrialID = manifest.Variants[0].ID
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "private variant identity") {
		t.Fatalf("private variant identity was accepted as trial id: %v", err)
	}
}

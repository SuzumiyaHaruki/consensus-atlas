package defectbench_test

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func TestPreflightReadinessSeparatesFormalPrerequisitesFromManifestValidity(t *testing.T) {
	manifest, _, _ := fixtureManifest(t)
	policy := defectbench.ReadinessPolicy{
		MinDistinctRootCauses: 2, MinControls: 2, RequireHistorical: true, RequireArtifactBinds: true,
	}
	report, err := defectbench.PreflightReadiness(manifest, policy)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.DefectVariants != 2 || report.Controls != 1 || report.DistinctRootCauses != 1 {
		t.Fatalf("development fixture incorrectly passed formal preflight: %+v", report)
	}
	for _, want := range []string{
		defectbench.ReadinessManifestNotV2,
		defectbench.ReadinessInsufficientRoots,
		defectbench.ReadinessInsufficientControl,
		defectbench.ReadinessNonHistorical,
		defectbench.ReadinessMissingArtifacts,
	} {
		if !strings.Contains(strings.Join(report.Findings, ","), want) {
			t.Fatalf("readiness report omitted %q: %+v", want, report)
		}
	}
}

func TestPreflightReadinessAcceptsMechanicallyCompleteFormalSet(t *testing.T) {
	manifest, _, _ := fixtureManifest(t)
	manifest.Version = defectbench.ManifestVersion2
	manifest.PSSID = "fixture-pss-v1"
	manifest.Variants[0].Provenance = defectbench.ProvenanceHistorical
	manifest.Variants[1].Provenance = defectbench.ProvenanceHistorical
	manifest.Variants[2].Provenance = defectbench.ProvenanceHistorical
	manifest.Variants[1].RootCauseID = "root-a"
	manifest.Variants[2].RootCauseID = "root-b"
	manifest.Variants = append(manifest.Variants, defectbench.Variant{
		ID: "control-b", TrialID: "trial-04", Kind: defectbench.KindControl,
		Category: "correct-control", Provenance: defectbench.ProvenanceHistorical,
		SourceDigest: strings.Repeat("e", 64), SUTManifestDigest: manifest.Variants[0].SUTManifestDigest,
	})
	for index := range manifest.Variants {
		manifest.Variants[index].BuildAuditDigest = strings.Repeat("a", 64)
		manifest.Variants[index].BinaryDigest = strings.Repeat("b", 64)
	}
	report, err := defectbench.PreflightReadiness(manifest, defectbench.ReadinessPolicy{
		MinDistinctRootCauses: 2, MinControls: 2, RequireHistorical: true, RequireArtifactBinds: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.DistinctRootCauses != 2 || report.Controls != 2 || report.ArtifactBoundTrials != 4 || len(report.Findings) != 0 {
		t.Fatalf("mechanically complete formal set rejected: %+v", report)
	}
}

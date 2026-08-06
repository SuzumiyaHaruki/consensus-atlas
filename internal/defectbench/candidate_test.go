package defectbench_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func TestCandidateQualificationIsTypedSetInclusion(t *testing.T) {
	catalog := defectbench.CandidateCatalog{Version: 1, ID: "fixture-catalog", Candidates: []defectbench.Candidate{
		fixtureCandidate("complete", defectbench.CandidateRequirements{ControllableInputs: []string{"propose"}}),
		fixtureCandidate("missing", defectbench.CandidateRequirements{
			ControllableInputs: []string{"read-index"}, DriverCapabilities: []string{"read-state"},
			TrustedMonitors: []string{"linearizable-read"}, ExecutionOutcomes: []string{"process-exit-classified"},
		}),
	}}
	snapshot := defectbench.CapabilitySnapshot{
		Version: defectbench.CapabilitySnapshotVersion, ID: "fixture-snapshot", Protocol: "fixture", Family: "fixture", ProfileID: "fixture-profile",
		ProfileDigest: strings.Repeat("a", 64), DriverManifestHash: strings.Repeat("b", 64),
		ControllableInputs: []string{"propose"}, TrustedMonitors: []string{"agreement"},
	}
	report, err := defectbench.QualifyCandidates(catalog, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].Status != defectbench.QualificationQualified || len(report.Results[0].ReasonCodes) != 0 {
		t.Fatalf("complete candidate was not qualified: %+v", report.Results[0])
	}
	encoded, err := json.Marshal(report.Results[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "null") || !strings.Contains(string(encoded), `"missing_requirements":[]`) ||
		!strings.Contains(string(encoded), `"reason_codes":[]`) {
		t.Fatalf("qualified result does not satisfy its required-array JSON shape: %s", encoded)
	}
	missing := report.Results[1]
	want := []string{
		"missing.controllable-input.read-index",
		"missing.driver-capability.read-state",
		"missing.trusted-monitor.linearizable-read",
		"missing.execution-outcome.process-exit-classified",
	}
	if missing.Status != defectbench.QualificationDeferred || strings.Join(missing.ReasonCodes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("deferred result = %+v, want reasons %v", missing, want)
	}
}

func TestCandidateQualificationRejectsScopeMismatch(t *testing.T) {
	catalog := defectbench.CandidateCatalog{Version: 1, ID: "fixture-catalog", Candidates: []defectbench.Candidate{
		fixtureCandidate("candidate", defectbench.CandidateRequirements{}),
	}}
	snapshot := defectbench.CapabilitySnapshot{
		Version: defectbench.CapabilitySnapshotVersion, ID: "fixture-snapshot", Protocol: "other", Family: "fixture", ProfileID: "fixture-profile",
		ProfileDigest: strings.Repeat("a", 64), DriverManifestHash: strings.Repeat("b", 64),
	}
	if _, err := defectbench.QualifyCandidates(catalog, snapshot); err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("protocol mismatch was not rejected: %v", err)
	}
	snapshot.Protocol = "fixture"
	snapshot.Family = "other"
	if _, err := defectbench.QualifyCandidates(catalog, snapshot); err == nil || !strings.Contains(err.Error(), "family") {
		t.Fatalf("family mismatch was not rejected: %v", err)
	}
}

func TestCandidateQualificationDigestUsesSetSemantics(t *testing.T) {
	first := fixtureCandidate("a", defectbench.CandidateRequirements{ControllableInputs: []string{"z", "a"}})
	second := fixtureCandidate("b", defectbench.CandidateRequirements{TrustedMonitors: []string{"z", "a"}})
	catalog := defectbench.CandidateCatalog{Version: 1, ID: "fixture-catalog", Candidates: []defectbench.Candidate{first, second}}
	snapshot := defectbench.CapabilitySnapshot{
		Version: defectbench.CapabilitySnapshotVersion, ID: "fixture-snapshot", Protocol: "fixture", Family: "fixture", ProfileID: "fixture-profile",
		ProfileDigest: strings.Repeat("a", 64), DriverManifestHash: strings.Repeat("b", 64),
		ControllableInputs: []string{"z", "a"}, TrustedMonitors: []string{"z", "a"},
	}
	want, err := defectbench.QualifyCandidates(catalog, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Candidates[0], catalog.Candidates[1] = catalog.Candidates[1], catalog.Candidates[0]
	catalog.Candidates[1].Requirements.ControllableInputs = []string{"a", "z"}
	snapshot.ControllableInputs = []string{"a", "z"}
	snapshot.TrustedMonitors = []string{"a", "z"}
	got, err := defectbench.QualifyCandidates(catalog, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got.CandidateCatalogDigest != want.CandidateCatalogDigest ||
		got.CapabilitySnapshotDigest != want.CapabilitySnapshotDigest {
		t.Fatalf("set reordering changed qualification identity: got %+v, want %+v", got, want)
	}
}

func fixtureCandidate(id string, requirements defectbench.CandidateRequirements) defectbench.Candidate {
	return defectbench.Candidate{
		ID: id, Protocol: "fixture", Family: "fixture", RootCauseGroup: "root." + id,
		Provenance:      defectbench.CandidateProvenance{Kind: "historical", Repository: "https://example.invalid/repo"},
		SourceReference: defectbench.CandidateSource{Revision: "abc", URL: "https://example.invalid/commit/abc"},
		Requirements:    requirements,
	}
}

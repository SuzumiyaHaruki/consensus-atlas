package etcdraft_test

import (
	"testing"

	catalog "github.com/SuzumiyaHaruki/consensus-atlas/catalogs/etcdraft"
	driveretcdraft "github.com/SuzumiyaHaruki/consensus-atlas/drivers/etcdraft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
)

func TestObservableProfileEvidenceDoesNotBecomeControllableInput(t *testing.T) {
	predicate := coverage.TracePredicate{EventKind: core.EventApply, ObservationKind: "application"}
	profile := coverage.Profile{
		Version: coverage.ProfileVersion, ID: "raft-campaign/raft-v1", Protocol: driveretcdraft.Protocol,
		PSSID: "raft-family-pss-v1", Nodes: []string{"n1", "n2", "n3"},
		Coverage: coverage.CoverageDefinition{Weights: map[string]float64{"property": 1}, Obligations: []coverage.Obligation{{
			ID: "property.apply", Category: "property", Description: "apply", Status: coverage.StatusSupported,
			Risk: coverage.RiskHigh, Monitors: []string{"agreement"},
			Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}},
		}, {
			ID: "boundary.election-rounds.1", Category: "property", Description: "one election", Status: coverage.StatusSupported,
			Risk: coverage.RiskHigh, Monitors: []string{"agreement"},
			Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}},
		}, {
			ID: "boundary.proposals.1", Category: "property", Description: "one proposal", Status: coverage.StatusSupported,
			Risk: coverage.RiskHigh, Monitors: []string{"agreement"},
			Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}},
		}}}, Threshold: coverage.Threshold{Score: 80, MinCategory: 0.5},
	}
	manifest := driver.Manifest{Driver: "fixture", SUT: "go.etcd.io/raft/v3", SUTVersion: "v3.6.0"}
	spec := catalog.CampaignSpec{Version: 1, ID: "raft-v1", Family: "raft", PSSID: profile.PSSID, FaultModel: "cft", Bounds: catalog.CampaignBounds{Nodes: 3, ElectionRounds: 1, Proposals: 1}}
	snapshot, err := catalog.BuildCapabilitySnapshot(profile, manifest, []string{"agreement"}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(snapshot.ObservableEvents, "event:apply") {
		t.Fatalf("apply evidence was not observable: %v", snapshot.ObservableEvents)
	}
	if contains(snapshot.ControllableInputs, "apply") {
		t.Fatalf("Profile evidence incorrectly created a controllable input: %v", snapshot.ControllableInputs)
	}
}

func TestCampaignBoundsMustMatchCompiledProfile(t *testing.T) {
	profile := minimalProfile()
	manifest := driver.Manifest{Driver: "fixture", SUT: "go.etcd.io/raft/v3", SUTVersion: "v3.6.0"}
	spec := catalog.CampaignSpec{Version: 1, ID: "raft-v1", Family: "raft", PSSID: profile.PSSID, FaultModel: "cft", Bounds: catalog.CampaignBounds{Nodes: 3, ElectionRounds: 2, Proposals: 1}}
	if _, err := catalog.BuildCapabilitySnapshot(profile, manifest, []string{"agreement"}, spec); err == nil {
		t.Fatal("a spec bound absent from the compiled Profile was accepted")
	}
}

func TestReadIndexFactsRequireDeclaredDriverCapabilities(t *testing.T) {
	profile := minimalProfile()
	spec := catalog.CampaignSpec{
		Version: 1, ID: "raft-v1", Family: "raft", PSSID: profile.PSSID, FaultModel: "cft",
		Bounds: catalog.CampaignBounds{Nodes: 3, ElectionRounds: 1, Proposals: 1},
	}
	without := driver.Manifest{Driver: "fixture", SUT: "go.etcd.io/raft/v3", SUTVersion: "v3.6.0"}
	snapshot, err := catalog.BuildCapabilitySnapshot(profile, without, []string{"agreement"}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if contains(snapshot.ControllableInputs, "read-index") || contains(snapshot.ControllableInputs, "query") ||
		contains(snapshot.ObservableEvents, "read-state") {
		t.Fatalf("undeclared ReadIndex facts entered snapshot: %#v", snapshot)
	}

	with := without
	with.Capabilities = []driver.Capability{
		{ID: "read-index-input", Supported: true},
		{ID: "read-state-observation", Supported: true},
	}
	snapshot, err = catalog.BuildCapabilitySnapshot(profile, with, []string{"agreement"}, spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"query", "read-index"} {
		if !contains(snapshot.ControllableInputs, fact) {
			t.Fatalf("missing controllable fact %q: %v", fact, snapshot.ControllableInputs)
		}
	}
	if !contains(snapshot.ObservableEvents, "read-state") {
		t.Fatalf("missing read-state observation: %v", snapshot.ObservableEvents)
	}
}

func TestReadyMustSyncObservationRequiresOptInWithoutManufacturingReachability(t *testing.T) {
	profile := minimalProfile()
	spec := catalog.CampaignSpec{
		Version: 1, ID: "raft-v1", Family: "raft", PSSID: profile.PSSID, FaultModel: "cft",
		Bounds: catalog.CampaignBounds{Nodes: 3, ElectionRounds: 1, Proposals: 1},
	}
	base := driver.Manifest{Driver: "fixture", SUT: "go.etcd.io/raft/v3", SUTVersion: "v3.6.0"}
	snapshot, err := catalog.BuildCapabilitySnapshot(profile, base, []string{"agreement", "ready-must-sync"}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if contains(snapshot.ControllableInputs, "ready-with-unchanged-hard-state") ||
		contains(snapshot.ObservableEvents, "ready-must-sync") {
		t.Fatalf("default Driver manufactured Ready.MustSync facts: %#v", snapshot)
	}

	base.Capabilities = []driver.Capability{
		{ID: "conditional-ready-sync", Supported: true},
		{ID: "ready-must-sync-observation", Supported: true},
	}
	snapshot, err = catalog.BuildCapabilitySnapshot(profile, base, []string{"agreement", "ready-must-sync"}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if contains(snapshot.ControllableInputs, "ready-with-unchanged-hard-state") {
		t.Fatalf("execution precondition was misclassified as a controllable input: %#v", snapshot)
	}
	if !contains(snapshot.ObservableEvents, "ready-must-sync") {
		t.Fatalf("opt-in Driver observation missing from snapshot: %#v", snapshot)
	}
}

func TestReadIndexCandidateQualificationIsMechanical(t *testing.T) {
	profile := minimalProfile()
	profile.Coverage.Obligations = append(profile.Coverage.Obligations,
		boundaryObligation("boundary.election-rounds.2"),
		boundaryObligation("boundary.partitions.1"),
		readIndexObservableObligation(),
	)
	spec := catalog.CampaignSpec{
		Version: 1, ID: "raft-v1", Family: "raft", PSSID: profile.PSSID, FaultModel: "cft",
		Bounds: catalog.CampaignBounds{Nodes: 3, ElectionRounds: 2, Proposals: 1, Partitions: 1},
	}
	protocolDriver, err := driveretcdraft.New([]string{"n1", "n2", "n3"})
	if err != nil {
		t.Fatal(err)
	}
	candidateCatalog := defectbench.CandidateCatalog{
		Version: defectbench.CandidateCatalogVersion, ID: "read-index-fixture",
		Candidates: []defectbench.Candidate{{
			ID: "read-index", Protocol: driveretcdraft.Protocol, Family: "raft",
			Provenance:      defectbench.CandidateProvenance{Kind: "fixture", Repository: "local"},
			SourceReference: defectbench.CandidateSource{Revision: "fixture", URL: "local"},
			RootCauseGroup:  "read-index-context",
			Requirements: defectbench.CandidateRequirements{
				ControllableInputs: []string{"campaign", "duplicate", "heal", "message", "partition", "propose", "read-index"},
				ObservableEvents:   []string{"event:message", "observation-kind:commit", "read-state"},
				DriverCapabilities: []string{"explicit-campaign", "message-drop-duplicate-partition", "message-release-control", "read-index-input", "read-state-observation"},
				TrustedMonitors:    []string{"linearizable-read"},
				ExecutionOutcomes:  []string{"campaign-report-v2", "deterministic-trace-replay", "oracle-from-saved-trace"},
				ProfileBounds:      []string{"election-rounds>=2", "partitions>=1", "proposals>=1"},
			},
		}},
	}

	snapshot, err := catalog.BuildCapabilitySnapshot(profile, protocolDriver.Capabilities(), []string{"agreement", "linearizable-read"}, spec)
	if err != nil {
		t.Fatal(err)
	}
	report, err := defectbench.QualifyCandidates(candidateCatalog, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].Status != defectbench.QualificationQualified {
		t.Fatalf("candidate was not qualified: %#v", report.Results[0])
	}

	snapshot, err = catalog.BuildCapabilitySnapshot(profile, protocolDriver.Capabilities(), []string{"agreement"}, spec)
	if err != nil {
		t.Fatal(err)
	}
	report, err = defectbench.QualifyCandidates(candidateCatalog, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].Status != defectbench.QualificationDeferred ||
		!contains(report.Results[0].ReasonCodes, "missing.trusted-monitor.linearizable-read") {
		t.Fatalf("missing monitor was not mechanically deferred: %#v", report.Results[0])
	}
}

func minimalProfile() coverage.Profile {
	predicate := coverage.TracePredicate{EventKind: core.EventCampaign, ObservationKind: "transition"}
	return coverage.Profile{
		Version: coverage.ProfileVersion, ID: "raft-campaign/raft-v1", Protocol: driveretcdraft.Protocol,
		PSSID: "raft-family-pss-v1", Nodes: []string{"n1", "n2", "n3"},
		Coverage: coverage.CoverageDefinition{Weights: map[string]float64{"boundary": 1}, Obligations: []coverage.Obligation{
			{ID: "boundary.election-rounds.1", Category: "boundary", Description: "one election", Risk: coverage.RiskHigh, Status: coverage.StatusSupported, Monitors: []string{"agreement"}, Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}}},
			{ID: "boundary.proposals.1", Category: "boundary", Description: "one proposal", Risk: coverage.RiskHigh, Status: coverage.StatusSupported, Monitors: []string{"agreement"}, Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}}},
		}}, Threshold: coverage.Threshold{Score: 80, MinCategory: 0.5},
	}
}

func boundaryObligation(id string) coverage.Obligation {
	predicate := coverage.TracePredicate{EventKind: core.EventCampaign, ObservationKind: "transition"}
	return coverage.Obligation{
		ID: id, Category: "boundary", Description: id, Risk: coverage.RiskHigh,
		Status: coverage.StatusSupported, Monitors: []string{"agreement"},
		Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}},
	}
}

func readIndexObservableObligation() coverage.Obligation {
	message := coverage.TracePredicate{EventKind: core.EventMessage}
	commit := coverage.TracePredicate{ObservationKind: "commit"}
	return coverage.Obligation{
		ID: "property.read-index-observable", Category: "boundary", Description: "read-index observations",
		Risk: coverage.RiskHigh, Status: coverage.StatusSupported, Monitors: []string{"agreement"},
		Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{message}, Observe: []coverage.TracePredicate{commit}},
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

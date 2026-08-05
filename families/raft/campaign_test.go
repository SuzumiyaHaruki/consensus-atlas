package raft_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/families/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
)

func TestCompileCampaignProducesDeterministicBoundedDenominator(t *testing.T) {
	spec := loadCampaignSpec(t)
	profile, err := raftfamily.CompileCampaign(integrationProfile(), campaignManifest(), spec)
	if err != nil {
		t.Fatal(err)
	}
	items, err := profile.Obligations()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 55 {
		t.Fatalf("campaign obligations = %d, want 55", len(items))
	}
	categories := make(map[string]int)
	unsupported := make(map[string]bool)
	for index, item := range items {
		categories[item.Category]++
		if item.Status == coverage.StatusUnsupported {
			unsupported[item.ID] = true
		}
		if index > 0 && items[index-1].ID >= item.ID {
			t.Fatalf("compiled obligations are not sorted at %q", item.ID)
		}
	}
	wantCategories := map[string]int{"transition": 5, "ordering": 12, "fault": 13, "boundary": 15, "property": 10}
	if encoded, _ := json.Marshal(categories); string(encoded) != mustJSON(wantCategories) {
		t.Fatalf("category denominator = %s, want %s", encoded, mustJSON(wantCategories))
	}
	if len(unsupported) != 2 || !unsupported["transition.natural-election-timeout"] ||
		!unsupported["ordering.ready-release-before-sync"] {
		t.Fatalf("unsupported obligations = %#v", unsupported)
	}
	encoded, _ := json.Marshal(items)
	if strings.Contains(string(encoded), `"n1"`) || strings.Contains(string(encoded), `"n2"`) {
		t.Fatalf("node identities leaked into fundamental obligation denominator: %s", encoded)
	}

	reordered := spec
	reordered.MessageTypes = reverse(reordered.MessageTypes)
	reordered.FaultMessageTypes = reverse(reordered.FaultMessageTypes)
	other, err := raftfamily.CompileCampaign(integrationProfile(), campaignManifest(), reordered)
	if err != nil {
		t.Fatal(err)
	}
	firstDigest, _ := coverage.Digest(profile)
	secondDigest, _ := coverage.Digest(other)
	if firstDigest != secondDigest {
		t.Fatalf("dimension order changed profile digest: %s != %s", firstDigest, secondDigest)
	}
}

func TestCompileCampaignBoundsMechanicallyExpandDenominator(t *testing.T) {
	spec := loadCampaignSpec(t)
	base, err := raftfamily.CompileCampaign(integrationProfile(), campaignManifest(), spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Bounds.Proposals++
	expanded, err := raftfamily.CompileCampaign(integrationProfile(), campaignManifest(), spec)
	if err != nil {
		t.Fatal(err)
	}
	baseItems, _ := base.Obligations()
	expandedItems, _ := expanded.Obligations()
	if len(expandedItems)-len(baseItems) != 3 {
		t.Fatalf("one proposal bound added %d obligations, want 3 (proposal + two log relations)", len(expandedItems)-len(baseItems))
	}
}

func TestCampaignRejectsUnsafeOutcomeAsCoverageShape(t *testing.T) {
	spec := loadCampaignSpec(t)
	spec.LogRelations = append(spec.LogRelations, raftfamily.LogConflict)
	if err := spec.Validate(); err == nil {
		t.Fatal("campaign accepted an actual log conflict as a required reachable coverage shape")
	}
}

func loadCampaignSpec(t *testing.T) raftfamily.CampaignSpec {
	t.Helper()
	data, err := os.ReadFile("../../profiles/raft/three-node-cft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec raftfamily.CampaignSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	return spec
}

func integrationProfile() coverage.Profile {
	predicate := coverage.TracePredicate{ObservationLabel: "integration"}
	return coverage.Profile{
		Version: coverage.ProfileVersion, ID: "integration", Protocol: "etcd-raft-v3.6", PSSID: raftfamily.PSSID,
		Nodes: []string{"n1", "n2", "n3"},
		RequiredCapabilities: []string{
			"explicit-campaign", "message-release-control", "message-drop-duplicate-partition",
			"visible-write-durable-sync", "power-loss-restart", "ready-crash-cutpoints",
			"application-apply", "exact-ready-send-barriers", "natural-election-timeout-replay",
		},
		Coverage: coverage.CoverageDefinition{
			Weights: map[string]float64{"transition": 1},
			Obligations: []coverage.Obligation{{
				ID: "integration", Category: "transition", Description: "integration",
				Evidence: coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate}},
				Monitors: []string{"trace-integrity"}, Risk: coverage.RiskStandard, Status: coverage.StatusSupported,
			}},
		},
		Threshold: coverage.Threshold{Score: 80, MinCategory: 0.5},
	}
}

func campaignManifest() driver.Manifest {
	ids := []string{
		"explicit-campaign", "message-release-control", "message-drop-duplicate-partition",
		"visible-write-durable-sync", "power-loss-restart", "ready-crash-cutpoints", "application-apply",
		"exact-ready-send-barriers", "natural-election-timeout-replay",
	}
	manifest := driver.Manifest{Driver: "fixture", SUT: "fixture", SUTVersion: "v1"}
	for _, id := range ids {
		manifest.Capabilities = append(manifest.Capabilities, driver.Capability{
			ID: id, Supported: id != "exact-ready-send-barriers" && id != "natural-election-timeout-replay",
		})
	}
	return manifest
}

func reverse(values []string) []string {
	result := append([]string(nil), values...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func mustJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

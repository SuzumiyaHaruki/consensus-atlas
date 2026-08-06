package bindings

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/drivers/etcdraft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
)

func TestRuntimeProfileSelectsOnlyRegisteredEtcdDriverMode(t *testing.T) {
	profile := coverage.Profile{
		Protocol:       etcdraft.Protocol,
		Nodes:          []string{"n1", "n2", "n3"},
		RuntimeProfile: etcdraftReadyMustSyncRuntimeProfile,
	}
	_, manifest, err := New(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCapability(manifest, "conditional-ready-sync") || !hasCapability(manifest, "ready-must-sync-observation") {
		t.Fatalf("runtime profile did not bind MustSync Driver capabilities: %#v", manifest)
	}

	profile.RuntimeProfile = "agent-selected-mode"
	if _, _, err := New(profile); err == nil {
		t.Fatal("unregistered runtime profile was accepted")
	}
}

func TestDefaultRuntimeProfileKeepsConservativeDriverCapabilities(t *testing.T) {
	_, manifest, err := New(coverage.Profile{Protocol: etcdraft.Protocol, Nodes: []string{"n1", "n2", "n3"}})
	if err != nil {
		t.Fatal(err)
	}
	if hasCapability(manifest, "conditional-ready-sync") || hasCapability(manifest, "ready-must-sync-observation") {
		t.Fatalf("default binding exposed opt-in capabilities: %#v", manifest)
	}
}

func hasCapability(manifest driver.Manifest, id string) bool {
	for _, capability := range manifest.Capabilities {
		if capability.ID == id && capability.Supported {
			return true
		}
	}
	return false
}

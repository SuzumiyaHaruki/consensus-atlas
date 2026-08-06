package families_test

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/families"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/families/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

func TestRegisteredMonitorsDoNotSilentlyChangeDefaultCampaignSet(t *testing.T) {
	trusted, err := families.TrustedMonitors(raftfamily.PSSID)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := families.RegisteredMonitors(raftfamily.PSSID)
	if err != nil {
		t.Fatal(err)
	}
	if containsMonitor(trusted, "ready-must-sync") {
		t.Fatalf("default campaign monitor set changed: %#v", trusted)
	}
	if !containsMonitor(registered, "ready-must-sync") {
		t.Fatalf("reviewed monitor was not registered: %#v", registered)
	}
}

func TestCampaignMonitorsRequireDriverObservationCapabilities(t *testing.T) {
	without, err := families.CampaignMonitors(raftfamily.PSSID, driver.Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	if containsMonitor(without, "ready-must-sync") {
		t.Fatalf("default Campaign set changed: %#v", without)
	}
	with, err := families.CampaignMonitors(raftfamily.PSSID, driver.Manifest{Capabilities: []driver.Capability{
		{ID: "conditional-ready-sync", Supported: true},
		{ID: "ready-must-sync-observation", Supported: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsMonitor(with, "ready-must-sync") {
		t.Fatalf("capability-bound monitor missing: %#v", with)
	}
}

func containsMonitor(monitors []oracle.Monitor, name string) bool {
	for _, monitor := range monitors {
		if monitor.Name() == name {
			return true
		}
	}
	return false
}

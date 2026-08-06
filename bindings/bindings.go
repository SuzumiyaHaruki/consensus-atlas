// Package bindings is the composition root for concrete protocol systems.
// Generic runtime, exploration, coverage, and state-discovery packages must not
// import it.
package bindings

import (
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/drivers/etcdraft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/host"
)

const etcdraftReadyMustSyncRuntimeProfile = "etcdraft-ready-must-sync-v1"

func New(profile coverage.Profile) (adapter.Adapter, driver.Manifest, error) {
	switch profile.Protocol {
	case etcdraft.Protocol:
		config := etcdraft.Config{Nodes: profile.Nodes}
		switch profile.RuntimeProfile {
		case "":
			// The Driver's default conservative mode remains the ordinary
			// Campaign binding.
		case etcdraftReadyMustSyncRuntimeProfile:
			config.ReadySyncPolicy = etcdraft.ReadySyncMustSync
		default:
			return nil, driver.Manifest{}, fmt.Errorf("runtime profile %q is not registered for protocol %q", profile.RuntimeProfile, profile.Protocol)
		}
		protocolDriver, err := etcdraft.NewWithConfig(config)
		if err != nil {
			return nil, driver.Manifest{}, err
		}
		hostAdapter, err := host.New(protocolDriver)
		if err != nil {
			return nil, driver.Manifest{}, err
		}
		return hostAdapter, hostAdapter.Capabilities(), nil
	default:
		if profile.RuntimeProfile != "" {
			return nil, driver.Manifest{}, fmt.Errorf("runtime profile requires a registered protocol binding")
		}
		return nil, driver.Manifest{}, fmt.Errorf("no driver registered for protocol %q", profile.Protocol)
	}
}

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
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/toy"
)

func New(profile coverage.Profile) (adapter.Adapter, driver.Manifest, error) {
	switch profile.Protocol {
	case toy.Protocol:
		return toy.New(profile.Nodes), driver.Manifest{
			Driver: "toy", SUT: toy.Protocol, SUTVersion: "v1",
			Capabilities: []driver.Capability{{ID: "toy-deterministic-runtime", Supported: true}},
		}, nil
	case etcdraft.Protocol:
		protocolDriver, err := etcdraft.New(profile.Nodes)
		if err != nil {
			return nil, driver.Manifest{}, err
		}
		hostAdapter, err := host.New(protocolDriver)
		if err != nil {
			return nil, driver.Manifest{}, err
		}
		return hostAdapter, hostAdapter.Capabilities(), nil
	default:
		return nil, driver.Manifest{}, fmt.Errorf("no driver registered for protocol %q", profile.Protocol)
	}
}

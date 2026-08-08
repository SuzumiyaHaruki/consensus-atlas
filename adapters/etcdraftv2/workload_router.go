package etcdraftv2

import (
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const WorkloadRouterID = "official-etcdraft-v2/workload-router-v1"

// WorkloadRouter owns the etcd/raft-specific interpretation required to route
// an opaque client input. Core PSS is not used as a routing API.
type WorkloadRouter struct{}

func (WorkloadRouter) ID() string { return WorkloadRouterID }

func (WorkloadRouter) Route(
	selector string,
	envelope control.EvidenceEnvelope,
) (controlexperiment.WorkloadRoute, error) {
	if selector != controlexperiment.TargetSingleCoordinatingMember {
		return controlexperiment.WorkloadRoute{}, fmt.Errorf(
			"ETCDRAFT_V2_WORKLOAD_SELECTOR_UNSUPPORTED: %s", selector,
		)
	}
	evidence, err := ProjectEvidence(envelope)
	if err != nil {
		return controlexperiment.WorkloadRoute{}, err
	}
	route := controlexperiment.WorkloadRoute{LogicalTime: evidence.LogicalTime}
	for _, node := range evidence.Nodes {
		if node.Running && node.Role == "StateLeader" {
			route.Candidates = append(route.Candidates, node.Node)
		}
	}
	return route, nil
}

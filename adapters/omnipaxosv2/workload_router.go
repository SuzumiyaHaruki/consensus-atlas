package omnipaxosv2

import (
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const WorkloadRouterID = "omnipaxos-v2/single-coordinator-router-v1"

type WorkloadRouter struct{}

func (WorkloadRouter) ID() string { return WorkloadRouterID }

func (WorkloadRouter) Route(
	selector string,
	envelope control.EvidenceEnvelope,
) (controlexperiment.WorkloadRoute, error) {
	if selector != controlexperiment.TargetSingleCoordinatingMember {
		return controlexperiment.WorkloadRoute{}, fmt.Errorf(
			"OMNIPAXOS_WORKLOAD_SELECTOR_UNSUPPORTED: %s", selector,
		)
	}
	snapshot, err := decodeEvidence(envelope)
	if err != nil {
		return controlexperiment.WorkloadRoute{}, err
	}
	route := controlexperiment.WorkloadRoute{LogicalTime: snapshot.LogicalTime}
	for _, node := range snapshot.Nodes {
		if node.Leader == node.ID {
			route.Candidates = append(route.Candidates, nodeName(node.ID))
		}
	}
	return route, nil
}

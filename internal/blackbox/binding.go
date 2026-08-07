package blackbox

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type GatewayControl interface {
	Identity() string
	Partition() error
	Heal() error
	Snapshot() GatewaySnapshot
}

type GatewayLink struct {
	ID      string
	Source  control.NodeID
	Target  control.NodeID
	Gateway GatewayControl
}

type GatewayResolution struct {
	PartitionID string
	Links       []GatewayLink
}

// GatewayBinding resolves a typed Partition/Heal Action against an explicit
// target topology. It does not infer missing peer links or protocol messages.
type GatewayBinding struct {
	nodes map[control.NodeID]struct{}
	links []GatewayLink
}

func NewGatewayBinding(nodes []control.NodeID, links []GatewayLink) (*GatewayBinding, error) {
	if len(nodes) == 0 || len(links) == 0 {
		return nil, errors.New("BLACKBOX_GATEWAY_TOPOLOGY_REQUIRED")
	}
	binding := &GatewayBinding{nodes: make(map[control.NodeID]struct{}, len(nodes))}
	for _, node := range nodes {
		if node == "" {
			return nil, errors.New("BLACKBOX_GATEWAY_NODE_INVALID")
		}
		if _, duplicate := binding.nodes[node]; duplicate {
			return nil, errors.New("BLACKBOX_GATEWAY_NODE_DUPLICATE")
		}
		binding.nodes[node] = struct{}{}
	}
	ids := make(map[string]struct{}, len(links))
	edges := make(map[struct{ source, target control.NodeID }]struct{}, len(links))
	gateways := make(map[string]struct{}, len(links))
	for _, link := range links {
		edge := struct{ source, target control.NodeID }{link.Source, link.Target}
		_, sourceKnown := binding.nodes[link.Source]
		_, targetKnown := binding.nodes[link.Target]
		if link.ID == "" || link.Gateway == nil || link.Gateway.Identity() == "" || !sourceKnown || !targetKnown || link.Source == link.Target {
			return nil, errors.New("BLACKBOX_GATEWAY_LINK_INVALID")
		}
		if _, duplicate := ids[link.ID]; duplicate {
			return nil, errors.New("BLACKBOX_GATEWAY_LINK_ID_DUPLICATE")
		}
		if _, duplicate := edges[edge]; duplicate {
			return nil, errors.New("BLACKBOX_GATEWAY_LINK_EDGE_DUPLICATE")
		}
		if _, duplicate := gateways[link.Gateway.Identity()]; duplicate {
			return nil, errors.New("BLACKBOX_GATEWAY_INSTANCE_DUPLICATE")
		}
		ids[link.ID], edges[edge], gateways[link.Gateway.Identity()] = struct{}{}, struct{}{}, struct{}{}
		binding.links = append(binding.links, link)
	}
	sort.Slice(binding.links, func(i, j int) bool { return binding.links[i].ID < binding.links[j].ID })
	return binding, nil
}

func (binding *GatewayBinding) Resolve(action control.Action) (GatewayResolution, error) {
	if binding == nil || (action.Kind != control.ActionPartition && action.Kind != control.ActionHeal) {
		return GatewayResolution{}, errors.New("BLACKBOX_GATEWAY_ACTION_UNSUPPORTED")
	}
	parameters, err := control.DecodePartitionParameters(action.Parameters)
	if err != nil {
		return GatewayResolution{}, err
	}
	for _, node := range append(append([]control.NodeID(nil), parameters.Left...), parameters.Right...) {
		if _, known := binding.nodes[node]; !known {
			return GatewayResolution{}, errors.New("BLACKBOX_GATEWAY_PARTITION_NODE_UNKNOWN")
		}
	}
	left, right := nodeMembership(parameters.Left), nodeMembership(parameters.Right)
	resolution := GatewayResolution{PartitionID: parameters.ID}
	for _, link := range binding.links {
		_, sourceLeft := left[link.Source]
		_, sourceRight := right[link.Source]
		_, targetLeft := left[link.Target]
		_, targetRight := right[link.Target]
		if (sourceLeft && targetRight) || (sourceRight && targetLeft) {
			resolution.Links = append(resolution.Links, link)
		}
	}
	if len(resolution.Links) == 0 {
		return GatewayResolution{}, errors.New("BLACKBOX_GATEWAY_PARTITION_LINK_MISSING")
	}
	return resolution, nil
}

func nodeMembership(nodes []control.NodeID) map[control.NodeID]struct{} {
	result := make(map[control.NodeID]struct{}, len(nodes))
	for _, node := range nodes {
		result[node] = struct{}{}
	}
	return result
}

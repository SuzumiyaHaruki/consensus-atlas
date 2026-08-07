package blackbox_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/blackbox"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestGatewayBindingResolvesExactPartitionCut(t *testing.T) {
	root := t.TempDir()
	g12, g13, g23 := bindingGateway(t, root, "g12"), bindingGateway(t, root, "g13"), bindingGateway(t, root, "g23")
	binding, err := blackbox.NewGatewayBinding(
		[]control.NodeID{"n1", "n2", "n3"},
		[]blackbox.GatewayLink{
			{ID: "n2-n3", Source: "n2", Target: "n3", Gateway: g23},
			{ID: "n1-n2", Source: "n1", Target: "n2", Gateway: g12},
			{ID: "n3-n1", Source: "n3", Target: "n1", Gateway: g13},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	partition := typedPartitionAction(t, control.ActionPartition, []control.NodeID{"n2", "n1"}, []control.NodeID{"n3"})
	resolution, err := binding.Resolve(partition)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.PartitionID == "" || !reflect.DeepEqual(linkIDs(resolution), []string{"n2-n3", "n3-n1"}) {
		t.Fatalf("partition resolution = %+v", resolution)
	}
	heal := partition
	heal.Kind = control.ActionHeal
	healed, err := binding.Resolve(heal)
	if err != nil || !reflect.DeepEqual(linkIDs(healed), linkIDs(resolution)) {
		t.Fatalf("heal resolution = %+v, err = %v", healed, err)
	}
}

func TestGatewayBindingRejectsAmbiguousOrMissingTopology(t *testing.T) {
	root := t.TempDir()
	g12, g13 := bindingGateway(t, root, "g12"), bindingGateway(t, root, "g13")
	nodes := []control.NodeID{"n1", "n2", "n3"}
	invalid := [][]blackbox.GatewayLink{
		{{ID: "same", Source: "n1", Target: "n2", Gateway: g12}, {ID: "same", Source: "n1", Target: "n3", Gateway: g13}},
		{{ID: "a", Source: "n1", Target: "n2", Gateway: g12}, {ID: "b", Source: "n1", Target: "n2", Gateway: g13}},
		{{ID: "a", Source: "n1", Target: "n2", Gateway: g12}, {ID: "b", Source: "n2", Target: "n3", Gateway: g12}},
		{{ID: "unknown", Source: "n1", Target: "n4", Gateway: g12}},
		{{ID: "nil", Source: "n1", Target: "n2"}},
	}
	for index, links := range invalid {
		if _, err := blackbox.NewGatewayBinding(nodes, links); err == nil {
			t.Fatalf("invalid topology %d was accepted", index)
		}
	}

	binding, err := blackbox.NewGatewayBinding(nodes, []blackbox.GatewayLink{{ID: "n1-n2", Source: "n1", Target: "n2", Gateway: g12}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binding.Resolve(typedPartitionAction(t, control.ActionPartition, []control.NodeID{"n1", "n2"}, []control.NodeID{"n3"})); err == nil {
		t.Fatal("partition with no bound crossing link was accepted")
	}
	if _, err := binding.Resolve(typedPartitionAction(t, control.ActionPartition, []control.NodeID{"n1"}, []control.NodeID{"n4"})); err == nil {
		t.Fatal("partition containing an unknown node was accepted")
	}
	unsupported := typedPartitionAction(t, control.ActionPartition, []control.NodeID{"n1"}, []control.NodeID{"n2"})
	unsupported.Kind = control.ActionInvoke
	if _, err := binding.Resolve(unsupported); err == nil {
		t.Fatal("non-partition action was accepted")
	}
}

func bindingGateway(t *testing.T, root, id string) *blackbox.Gateway {
	t.Helper()
	gateway, err := blackbox.NewGateway(blackbox.GatewaySpec{
		ID:      id,
		Listen:  blackbox.Endpoint{ID: id + "-in", Network: "unix", Address: filepath.Join(root, id+"-in.sock")},
		Forward: blackbox.Endpoint{ID: id + "-out", Network: "unix", Address: filepath.Join(root, id+"-out.sock")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func typedPartitionAction(t *testing.T, kind control.ActionKind, left, right []control.NodeID) control.Action {
	t.Helper()
	parameters, err := control.NewPartitionParameters(left, right)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(parameters)
	if err != nil {
		t.Fatal(err)
	}
	return control.Action{ID: control.ActionID("test-action"), Kind: kind, Parameters: encoded}
}

func linkIDs(resolution blackbox.GatewayResolution) []string {
	result := make([]string, len(resolution.Links))
	for index, link := range resolution.Links {
		result[index] = link.ID
	}
	return result
}

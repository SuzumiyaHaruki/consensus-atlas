package blackbox_test

import (
	"errors"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/blackbox"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type controlledGateway struct {
	id                        string
	partitioned               bool
	failPartition, failHeal   bool
	partitionCalls, healCalls int
}

func (gateway *controlledGateway) Identity() string { return gateway.id }
func (gateway *controlledGateway) Snapshot() blackbox.GatewaySnapshot {
	return blackbox.GatewaySnapshot{Started: true, Partitioned: gateway.partitioned}
}
func (gateway *controlledGateway) Partition() error {
	gateway.partitionCalls++
	if gateway.failPartition {
		return errors.New("TEST_PARTITION_FAILURE")
	}
	if gateway.partitioned {
		return errors.New("TEST_ALREADY_PARTITIONED")
	}
	gateway.partitioned = true
	return nil
}
func (gateway *controlledGateway) Heal() error {
	gateway.healCalls++
	if gateway.failHeal {
		return errors.New("TEST_HEAL_FAILURE")
	}
	if !gateway.partitioned {
		return errors.New("TEST_NOT_PARTITIONED")
	}
	gateway.partitioned = false
	return nil
}

func TestGatewayActuatorPreservesOverlappingPartitionReferences(t *testing.T) {
	g13, g23 := &controlledGateway{id: "g13"}, &controlledGateway{id: "g23"}
	actuator := newControlledActuator(t, g13, g23)
	p1 := typedPartitionAction(t, control.ActionPartition, []control.NodeID{"n1"}, []control.NodeID{"n3"})
	p2 := typedPartitionAction(t, control.ActionPartition, []control.NodeID{"n1", "n2"}, []control.NodeID{"n3"})
	if err := actuator.Apply(p1); err != nil {
		t.Fatal(err)
	}
	if err := actuator.Apply(p2); err != nil {
		t.Fatal(err)
	}
	if !g13.partitioned || !g23.partitioned || g13.partitionCalls != 1 || g23.partitionCalls != 1 {
		t.Fatalf("overlap apply: g13=%+v g23=%+v", g13, g23)
	}
	p1.Kind = control.ActionHeal
	if err := actuator.Apply(p1); err != nil {
		t.Fatal(err)
	}
	if !g13.partitioned || !g23.partitioned || g13.healCalls != 0 {
		t.Fatal("first Heal prematurely reopened an overlapping Gateway")
	}
	p2.Kind = control.ActionHeal
	if err := actuator.Apply(p2); err != nil {
		t.Fatal(err)
	}
	if g13.partitioned || g23.partitioned || g13.healCalls != 1 || g23.healCalls != 1 {
		t.Fatalf("final heal: g13=%+v g23=%+v", g13, g23)
	}
}

func TestGatewayActuatorRollsBackFailedMultiLinkChanges(t *testing.T) {
	g13, g23 := &controlledGateway{id: "g13"}, &controlledGateway{id: "g23", failPartition: true}
	actuator := newControlledActuator(t, g13, g23)
	partition := typedPartitionAction(t, control.ActionPartition, []control.NodeID{"n1", "n2"}, []control.NodeID{"n3"})
	if err := actuator.Apply(partition); err == nil {
		t.Fatal("multi-link partition unexpectedly succeeded")
	}
	if g13.partitioned || g23.partitioned || g13.healCalls != 1 {
		t.Fatalf("partition rollback: g13=%+v g23=%+v", g13, g23)
	}
	g23.failPartition = false
	if err := actuator.Apply(partition); err != nil {
		t.Fatal(err)
	}
	g23.failHeal = true
	heal := partition
	heal.Kind = control.ActionHeal
	if err := actuator.Apply(heal); err == nil {
		t.Fatal("multi-link heal unexpectedly succeeded")
	}
	if !g13.partitioned || !g23.partitioned || g13.partitionCalls != 3 {
		t.Fatalf("heal rollback: g13=%+v g23=%+v", g13, g23)
	}
	g23.failHeal = false
	if err := actuator.Apply(heal); err != nil {
		t.Fatal(err)
	}
}

func newControlledActuator(t *testing.T, g13, g23 blackbox.GatewayControl) *blackbox.GatewayActuator {
	t.Helper()
	binding, err := blackbox.NewGatewayBinding(
		[]control.NodeID{"n1", "n2", "n3"},
		[]blackbox.GatewayLink{
			{ID: "a-n1-n3", Source: "n1", Target: "n3", Gateway: g13},
			{ID: "b-n2-n3", Source: "n2", Target: "n3", Gateway: g23},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	actuator, err := blackbox.NewGatewayActuator(binding)
	if err != nil {
		t.Fatal(err)
	}
	return actuator
}

package blackbox_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/blackbox"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestOverlappingPartitionActionsControlThreeProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	n1Dir, n2Dir, n3Dir := filepath.Join(root, "n1"), filepath.Join(root, "n2"), filepath.Join(root, "n3")
	n1Socket, n2Socket, n3Socket := filepath.Join(n1Dir, "client.sock"), filepath.Join(n2Dir, "client.sock"), filepath.Join(n3Dir, "client.sock")

	n3 := newFixtureTarget(t, "n3", n3Dir, n3Socket, "")
	if _, err := n3.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer n3.Close()

	g13 := newProcessGateway(t, "n1-to-n3", filepath.Join(root, "g13.sock"), n3Socket)
	defer g13.Close()
	g23 := newProcessGateway(t, "n2-to-n3", filepath.Join(root, "g23.sock"), n3Socket)
	defer g23.Close()

	n1 := newFixtureTarget(t, "n1", n1Dir, n1Socket, filepath.Join(root, "g13.sock"))
	if _, err := n1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer n1.Close()
	n2 := newFixtureTarget(t, "n2", n2Dir, n2Socket, filepath.Join(root, "g23.sock"))
	if _, err := n2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer n2.Close()
	assertPeerResponse(t, ctx, n1, "open-n1", "ACK")
	assertPeerResponse(t, ctx, n2, "open-n2", "ACK")

	binding, err := blackbox.NewGatewayBinding(
		[]control.NodeID{"n1", "n2", "n3"},
		[]blackbox.GatewayLink{
			{ID: "n1-to-n3", Source: "n1", Target: "n3", Gateway: g13},
			{ID: "n2-to-n3", Source: "n2", Target: "n3", Gateway: g23},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	actuator, err := blackbox.NewGatewayActuator(binding)
	if err != nil {
		t.Fatal(err)
	}
	base, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, &gatewayActuatedAdapter{Adapter: base, actuator: actuator}, controlruntime.Config{Seed: []byte("three-process-overlap-v1")})
	if err != nil {
		t.Fatal(err)
	}

	p1 := offerPartitionGroups(t, ctx, runtime, []control.NodeID{"n1"}, []control.NodeID{"n3"})
	selectAction(t, ctx, runtime, p1)
	assertPeerResponse(t, ctx, n1, "p1-n1", "PEER-ERROR")
	assertPeerResponse(t, ctx, n2, "p1-n2", "ACK")

	p2 := offerPartitionGroups(t, ctx, runtime, []control.NodeID{"n1", "n2"}, []control.NodeID{"n3"})
	selectAction(t, ctx, runtime, p2)
	assertPeerResponse(t, ctx, n1, "p2-n1", "PEER-ERROR")
	assertPeerResponse(t, ctx, n2, "p2-n2", "PEER-ERROR")

	selectAction(t, ctx, runtime, healForPartition(t, ctx, runtime, partitionID(t, p1)))
	assertPeerResponse(t, ctx, n1, "p1-healed-n1", "PEER-ERROR")
	assertPeerResponse(t, ctx, n2, "p1-healed-n2", "PEER-ERROR")

	selectAction(t, ctx, runtime, healForPartition(t, ctx, runtime, partitionID(t, p2)))
	assertPeerResponse(t, ctx, n1, "all-healed-n1", "ACK")
	assertPeerResponse(t, ctx, n2, "all-healed-n2", "ACK")
	if len(runtime.Snapshot().Partitions) != 0 || g13.Snapshot().Partitioned || g23.Snapshot().Partitioned {
		t.Fatal("final Heal did not restore Runtime and both Gateway states")
	}
}

func newProcessGateway(t *testing.T, id, listen, forward string) *blackbox.Gateway {
	t.Helper()
	gateway, err := blackbox.NewGateway(blackbox.GatewaySpec{
		ID:      id,
		Listen:  blackbox.Endpoint{ID: id + "-proxy", Network: "unix", Address: listen},
		Forward: blackbox.Endpoint{ID: "n3", Network: "unix", Address: forward},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	return gateway
}

func offerPartitionGroups(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime, left, right []control.NodeID) control.Action {
	t.Helper()
	id, err := runtime.OfferPartition(left, right)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range mustActions(t, ctx, runtime) {
		if action.ID == id {
			return action
		}
	}
	t.Fatalf("offered partition %s is not enabled", id)
	return control.Action{}
}

func healForPartition(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime, id string) control.Action {
	t.Helper()
	for _, action := range mustActions(t, ctx, runtime) {
		if action.Kind == control.ActionHeal && partitionID(t, action) == id {
			return action
		}
	}
	t.Fatalf("heal for partition %s is not enabled", id)
	return control.Action{}
}

func partitionID(t *testing.T, action control.Action) string {
	t.Helper()
	parameters, err := control.DecodePartitionParameters(action.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	return parameters.ID
}

func selectAction(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime, action control.Action) {
	t.Helper()
	if _, err := runtime.Select(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
}

func assertPeerResponse(t *testing.T, ctx context.Context, target *blackbox.Envelope, value, want string) {
	t.Helper()
	if got := deliver(t, ctx, target, "SEND "+value); got != want {
		t.Fatalf("SEND %s response = %q, want %q", value, got, want)
	}
}

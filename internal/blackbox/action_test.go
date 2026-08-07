package blackbox_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/blackbox"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

type gatewayActuatedAdapter struct {
	control.Adapter
	actuator *blackbox.GatewayActuator
	seen     []control.Action
}

func (adapter *gatewayActuatedAdapter) CheckRuntimeAction(ctx context.Context, action control.Action) (control.CommandEligibility, error) {
	if action.Kind == control.ActionPartition || action.Kind == control.ActionHeal {
		return adapter.actuator.Check(action)
	}
	return adapter.Adapter.CheckRuntimeAction(ctx, action)
}

func (adapter *gatewayActuatedAdapter) ApplyRuntimeAction(ctx context.Context, action control.Action) error {
	adapter.seen = append(adapter.seen, action)
	if action.Kind != control.ActionPartition && action.Kind != control.ActionHeal {
		return adapter.Adapter.ApplyRuntimeAction(ctx, action)
	}
	return adapter.actuator.Apply(action)
}

func TestSamePartitionActionsDriveEtcdMailboxAndBlackboxGateway(t *testing.T) {
	ctx := context.Background()
	config := controlruntime.Config{Seed: []byte("unified-partition-action-v1"), MaxClones: 2}

	etcdAdapter, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	if err != nil {
		t.Fatal(err)
	}
	etcdRuntime, err := controlruntime.New(ctx, etcdAdapter, config)
	if err != nil {
		t.Fatal(err)
	}
	etcdPartition := offerPartitionAction(t, ctx, etcdRuntime)
	if _, err := etcdRuntime.Select(ctx, etcdPartition.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(etcdRuntime.Snapshot().Partitions); got != 1 {
		t.Fatalf("etcd Runtime partitions = %d, want 1", got)
	}
	etcdHeal := oneActionOfKind(t, ctx, etcdRuntime, control.ActionHeal)

	root := t.TempDir()
	gateway, err := blackbox.NewGateway(blackbox.GatewaySpec{
		ID:      "n1-to-n2",
		Listen:  blackbox.Endpoint{ID: "proxy", Network: "unix", Address: filepath.Join(root, "gateway.sock")},
		Forward: blackbox.Endpoint{ID: "n2", Network: "unix", Address: filepath.Join(root, "n2.sock")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()

	binding, err := blackbox.NewGatewayBinding(
		[]control.NodeID{"n1", "n2"},
		[]blackbox.GatewayLink{{ID: "n1-to-n2", Source: "n1", Target: "n2", Gateway: gateway}},
	)
	if err != nil {
		t.Fatal(err)
	}
	actuator, err := blackbox.NewGatewayActuator(binding)
	if err != nil {
		t.Fatal(err)
	}
	gatewayAdapter := &gatewayActuatedAdapter{Adapter: fixture.New(), actuator: actuator}
	gatewayRuntime, err := controlruntime.New(ctx, gatewayAdapter, config)
	if err != nil {
		t.Fatal(err)
	}
	gatewayPartition := offerPartitionAction(t, ctx, gatewayRuntime)
	assertSameAction(t, etcdPartition, gatewayPartition)
	if _, err := gatewayRuntime.Select(ctx, gatewayPartition.ID); err != nil {
		t.Fatal(err)
	}
	if !gateway.Snapshot().Partitioned || len(gatewayRuntime.Snapshot().Partitions) != 1 {
		t.Fatal("one Partition Action did not update both Runtime and Gateway")
	}
	gatewayHeal := oneActionOfKind(t, ctx, gatewayRuntime, control.ActionHeal)
	assertSameAction(t, etcdHeal, gatewayHeal)
	if _, err := gatewayRuntime.Select(ctx, gatewayHeal.ID); err != nil {
		t.Fatal(err)
	}
	if gateway.Snapshot().Partitioned || len(gatewayRuntime.Snapshot().Partitions) != 0 {
		t.Fatal("one Heal Action did not update both Runtime and Gateway")
	}
	if _, err := etcdRuntime.Select(ctx, etcdHeal.ID); err != nil {
		t.Fatal(err)
	}
	if len(etcdRuntime.Snapshot().Partitions) != 0 {
		t.Fatal("etcd Runtime remained partitioned after the shared Heal Action")
	}
	if len(gatewayAdapter.seen) != 2 || gatewayAdapter.seen[0].Kind != control.ActionPartition || gatewayAdapter.seen[1].Kind != control.ActionHeal {
		t.Fatalf("Gateway actuation log = %+v", gatewayAdapter.seen)
	}
}

func TestGatewayEligibilityFiltersMismatchedGateState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	gateway, err := blackbox.NewGateway(blackbox.GatewaySpec{
		ID:      "n1-to-n2",
		Listen:  blackbox.Endpoint{ID: "proxy", Network: "unix", Address: filepath.Join(root, "gateway.sock")},
		Forward: blackbox.Endpoint{ID: "n2", Network: "unix", Address: filepath.Join(root, "n2.sock")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	if err := gateway.Partition(); err != nil {
		t.Fatal(err)
	}
	binding, err := blackbox.NewGatewayBinding(
		[]control.NodeID{"n1", "n2"},
		[]blackbox.GatewayLink{{ID: "n1-to-n2", Source: "n1", Target: "n2", Gateway: gateway}},
	)
	if err != nil {
		t.Fatal(err)
	}
	actuator, err := blackbox.NewGatewayActuator(binding)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, &gatewayActuatedAdapter{Adapter: fixture.New(), actuator: actuator}, controlruntime.Config{Seed: []byte("eligibility-v1")})
	if err != nil {
		t.Fatal(err)
	}
	id, err := runtime.OfferPartition([]control.NodeID{"n1"}, []control.NodeID{"n2"})
	if err != nil {
		t.Fatal(err)
	}
	if hasActionID(mustActions(t, ctx, runtime), id) {
		t.Fatal("partition with mismatched external gate state was enabled")
	}
	if err := gateway.Heal(); err != nil {
		t.Fatal(err)
	}
	if !hasActionID(mustActions(t, ctx, runtime), id) {
		t.Fatal("partition remained disabled after external gate state was restored")
	}
}

func offerPartitionAction(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime) control.Action {
	t.Helper()
	id, err := runtime.OfferPartition([]control.NodeID{"n1"}, []control.NodeID{"n2"})
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

func oneActionOfKind(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime, kind control.ActionKind) control.Action {
	t.Helper()
	var found []control.Action
	for _, action := range mustActions(t, ctx, runtime) {
		if action.Kind == kind {
			found = append(found, action)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s actions = %d, want 1", kind, len(found))
	}
	return found[0]
}

func mustActions(t *testing.T, ctx context.Context, runtime *controlruntime.Runtime) []control.Action {
	t.Helper()
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return actions
}

func assertSameAction(t *testing.T, left, right control.Action) {
	t.Helper()
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("Action mismatch:\nleft:  %+v\nright: %+v", left, right)
	}
}

func hasActionID(actions []control.Action, id control.ActionID) bool {
	for _, action := range actions {
		if action.ID == id {
			return true
		}
	}
	return false
}

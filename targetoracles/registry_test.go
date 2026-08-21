package targetoracles

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

type mismatchedMonitor struct{}

func (mismatchedMonitor) Name() string { return "executed-monitor" }
func (mismatchedMonitor) CheckBundle(controlexperiment.ExecutionBundle) []oracle.Violation {
	return nil
}

func TestRegistryResolvesExactTargetAndProjector(t *testing.T) {
	projector, registry, err := Resolve(EtcdraftV2TargetID, etcdraftv2.DecisionProjectionID)
	if err != nil || projector.ID() != etcdraftv2.DecisionProjectionID ||
		!registry.Matches(EtcdraftV2TargetID, etcdraftv2.DecisionProjectionID) {
		t.Fatalf("etcd/raft registry resolution = %#v/%#v/%v", projector, registry, err)
	}
	monitors := registry.EvaluationMonitors()
	want := []string{"agreement", ClientApplicationBindingMonitorID, ElectionSafetyMonitorID, LogProgressMonitorID}
	if len(monitors) != len(want) {
		t.Fatalf("evaluation monitors = %#v", monitors)
	}
	for index := range want {
		if monitors[index].Name() != want[index] {
			t.Fatalf("evaluation monitor %d = %q, want %q", index, monitors[index].Name(), want[index])
		}
	}
	if _, _, err := Resolve(OmnipaxosV2TargetID, etcdraftv2.DecisionProjectionID); err == nil {
		t.Fatal("target/projector mismatch was accepted")
	}
}

func TestResolveTargetBindsRegisteredProjector(t *testing.T) {
	projector, registry, err := ResolveTarget(EtcdraftV2TargetID)
	if err != nil {
		t.Fatal(err)
	}
	if projector.ID() != registry.ProjectorID() || registry.TargetID() != EtcdraftV2TargetID {
		t.Fatalf("resolved Target composition drifted: %s/%s/%s",
			projector.ID(), registry.ProjectorID(), registry.TargetID())
	}
	if _, _, err := ResolveTarget("unknown-target"); err == nil {
		t.Fatal("unknown Target resolved an Oracle composition")
	}
}

func TestOmnipaxosRegistryIncludesClientDecisionBinding(t *testing.T) {
	_, registry, err := ResolveTarget(OmnipaxosV2TargetID)
	if err != nil {
		t.Fatal(err)
	}
	monitors := registry.EvaluationMonitors()
	want := []string{"agreement", OmnipaxosClientDecisionBindingMonitorID}
	if len(monitors) != len(want) {
		t.Fatalf("OmniPaxos evaluation monitors = %#v", monitors)
	}
	for index := range want {
		if monitors[index].Name() != want[index] {
			t.Fatalf("OmniPaxos monitor %d = %q, want %q", index, monitors[index].Name(), want[index])
		}
	}
}

func TestRegistryRejectsDeclaredAndExecutableMonitorIDDrift(t *testing.T) {
	registry := newRegistry(
		"fixture-target", "fixture-projector",
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "trace-integrity", Scope: controlexperiment.AgentOracleScopeGeneric,
			},
			Monitor: oracle.BundleTraceIntegrity{},
		},
		Registration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "declared-monitor", Scope: controlexperiment.AgentOracleScopeTarget,
			},
			Monitor: mismatchedMonitor{},
		},
	)
	if registry.Validate() == nil {
		t.Fatal("declared monitor ID drifted from executable monitor")
	}
}

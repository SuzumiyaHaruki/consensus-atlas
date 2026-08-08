package blackbox_test

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const (
	checkedSurfaceComparisonPath = "../../benchmarks/qualifications/control-surfaces-m5.5a/report.json"
	checkedControlPathMatrixPath = "../../benchmarks/qualifications/control-paths-m5.6d/report.json"
)

type checkedSurfaceComparison struct {
	Implementations []conformance.ControlSurfaceReport `json:"implementations"`
}

type controlPathPolicy struct {
	GradeRule     string `json:"grade_rule"`
	GuaranteeRule string `json:"guarantee_rule"`
	GatewayScope  string `json:"gateway_scope"`
}

type controlPathMatrix struct {
	SchemaVersion string                              `json:"schema_version"`
	ReportID      string                              `json:"report_id"`
	Paths         []conformance.ControlPathAssessment `json:"paths"`
	Policy        controlPathPolicy                   `json:"policy"`
	Digest        string                              `json:"digest"`
}

type checkingGatewayAdapter struct {
	*gatewayActuatedAdapter
	checks int
}

func (adapter *checkingGatewayAdapter) CheckRuntimeAction(ctx context.Context, action control.Action) (control.CommandEligibility, error) {
	if action.Kind == control.ActionPartition || action.Kind == control.ActionHeal {
		adapter.checks++
	}
	return adapter.gatewayActuatedAdapter.CheckRuntimeAction(ctx, action)
}

func TestFrozenControlPathMatrixSeparatesGradeFromGuarantees(t *testing.T) {
	paths := checkedAdapterMessagePaths(t)
	paths = append(paths, exercisedGatewayPath(t))
	matrix, err := sealControlPathMatrix(controlPathMatrix{
		SchemaVersion: "consensus-atlas/control-path-matrix/v1", ReportID: "control-paths-m5.6d",
		Paths: paths,
		Policy: controlPathPolicy{
			GradeRule: "derived-from-witness-facts", GuaranteeRule: "independent-from-control-grade",
			GatewayScope: "path-witness-only-not-adapter-qualification",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertControlPathResults(t, matrix.Paths)
	encoded, err := os.ReadFile(checkedControlPathMatrixPath)
	if err != nil {
		pretty, _ := json.MarshalIndent(matrix, "", "  ")
		t.Fatalf("%v; fresh report:\n%s", err, pretty)
	}
	var checked controlPathMatrix
	if err := json.Unmarshal(encoded, &checked); err != nil {
		t.Fatal(err)
	}
	freshJSON, _ := json.Marshal(matrix)
	checkedJSON, _ := json.Marshal(checked)
	if string(freshJSON) != string(checkedJSON) {
		pretty, _ := json.MarshalIndent(matrix, "", "  ")
		t.Fatalf("frozen control path matrix is stale; fresh report:\n%s", pretty)
	}
}

func checkedAdapterMessagePaths(t *testing.T) []conformance.ControlPathAssessment {
	t.Helper()
	encoded, err := os.ReadFile(checkedSurfaceComparisonPath)
	if err != nil {
		t.Fatal(err)
	}
	var checked checkedSurfaceComparison
	if err := json.Unmarshal(encoded, &checked); err != nil {
		t.Fatal(err)
	}
	paths := make([]conformance.ControlPathAssessment, 0, len(checked.Implementations))
	for _, report := range checked.Implementations {
		if err := report.Validate(); err != nil {
			t.Fatal(err)
		}
		owned := false
		for _, surface := range report.Surfaces {
			if surface.ID == conformance.SurfaceMessage {
				owned = surface.ValidatedControl == conformance.ControlSchedulerOwned
			}
		}
		if !owned {
			t.Fatalf("%s lacks validated message ownership", report.AdapterID)
		}
		strict := false
		for _, guarantee := range report.Guarantees {
			if guarantee.ID == "strict-decision-replay" {
				strict = guarantee.Status == conformance.CapabilityValidated
			}
		}
		path, err := conformance.AssessControlPath(conformance.ControlPathWitness{
			ID: report.AdapterID + "/message", TargetID: report.ImplementationID,
			Surface: conformance.SurfaceMessage, Granularity: "message", EvidenceDigest: report.Digest,
			Observed: true, Intercepted: true, RuntimeAction: true, SelectionChecked: true,
			StableItemID: true, RuntimeOwnsTerminal: true, StrictReplay: strict,
		})
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

func exercisedGatewayPath(t *testing.T) conformance.ControlPathAssessment {
	t.Helper()
	ctx := context.Background()
	g13, g23 := &controlledGateway{id: "g13"}, &controlledGateway{id: "g23"}
	actuator := newControlledActuator(t, g13, g23)
	base, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	if err != nil {
		t.Fatal(err)
	}
	adapter := &checkingGatewayAdapter{gatewayActuatedAdapter: &gatewayActuatedAdapter{Adapter: base, actuator: actuator}}
	runtime, err := controlruntime.New(ctx, adapter, controlruntime.Config{Seed: []byte("control-path-m5.6d")})
	if err != nil {
		t.Fatal(err)
	}
	p1 := offerPartitionGroups(t, ctx, runtime, []control.NodeID{"n1"}, []control.NodeID{"n3"})
	selectAction(t, ctx, runtime, p1)
	p2 := offerPartitionGroups(t, ctx, runtime, []control.NodeID{"n1", "n2"}, []control.NodeID{"n3"})
	selectAction(t, ctx, runtime, p2)
	selectAction(t, ctx, runtime, healForPartition(t, ctx, runtime, partitionID(t, p1)))
	if !g13.partitioned || !g23.partitioned {
		t.Fatal("overlap reference did not preserve Gateway partition")
	}
	selectAction(t, ctx, runtime, healForPartition(t, ctx, runtime, partitionID(t, p2)))
	if g13.partitioned || g23.partitioned || adapter.checks == 0 {
		t.Fatal("Gateway actuation or selection check was not observed")
	}

	f13, f23 := &controlledGateway{id: "f13"}, &controlledGateway{id: "f23", failPartition: true}
	failing := newControlledActuator(t, f13, f23)
	failureAction := typedPartitionAction(t, control.ActionPartition, []control.NodeID{"n1", "n2"}, []control.NodeID{"n3"})
	if err := failing.Apply(failureAction); err == nil || f13.partitioned || f23.partitioned || f13.healCalls != 1 {
		t.Fatalf("controller rollback evidence missing: f13=%+v f23=%+v err=%v", f13, f23, err)
	}
	evidenceDigest, err := control.CanonicalDigest(struct {
		Checks, Actions, G13Partitions, G13Heals, G23Partitions, G23Heals, RollbackHeals int
	}{adapter.checks, len(adapter.seen), g13.partitionCalls, g13.healCalls, g23.partitionCalls, g23.healCalls, f13.healCalls})
	if err != nil {
		t.Fatal(err)
	}
	path, err := conformance.AssessControlPath(conformance.ControlPathWitness{
		ID: "blackbox-gateway/partition", TargetID: "consensus-atlas/connection-gateway-v1",
		Surface: conformance.SurfaceMessage, Granularity: "connection", EvidenceDigest: evidenceDigest,
		Observed: true, Intercepted: true, RuntimeAction: true, SelectionChecked: true,
		AtomicControllerState: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func assertControlPathResults(t *testing.T, paths []conformance.ControlPathAssessment) {
	t.Helper()
	if len(paths) != 3 {
		t.Fatalf("control paths = %d, want 3", len(paths))
	}
	for _, path := range paths {
		if err := path.Validate(); err != nil {
			t.Fatal(err)
		}
		switch path.Granularity {
		case "connection":
			if path.Grade != conformance.ControlSchedulerActuated || path.StableItemID || !path.AtomicControllerState || path.AtomicExternalEffect || path.StrictReplay {
				t.Fatalf("Gateway path overcredited: %+v", path)
			}
		case "message":
			if path.Grade != conformance.ControlSchedulerOwned || !path.StableItemID {
				t.Fatalf("message path undercredited: %+v", path)
			}
		}
	}
}

func sealControlPathMatrix(matrix controlPathMatrix) (controlPathMatrix, error) {
	sort.Slice(matrix.Paths, func(i, j int) bool { return matrix.Paths[i].TargetID < matrix.Paths[j].TargetID })
	matrix.Digest = ""
	digest, err := control.CanonicalDigest(matrix)
	if err != nil {
		return controlPathMatrix{}, err
	}
	matrix.Digest = digest
	return matrix, nil
}

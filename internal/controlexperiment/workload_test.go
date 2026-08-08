package controlexperiment

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestWorkloadPlanAndFaultEnvelopeAreBounded(t *testing.T) {
	payload, err := control.NewJSONPayload("test/input/v1", map[string]string{"request": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	plan := WorkloadPlan{
		SchemaVersion: WorkloadPlanVersion, ID: "writes",
		TargetSelector: TargetSingleCoordinatingMember,
		Invocations:    []WorkloadInvocation{{ID: "r1", Input: payload, ExpectedStatus: "committed"}},
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	duplicate := plan
	duplicate.Invocations = append(duplicate.Invocations, duplicate.Invocations[0])
	if err := duplicate.Validate(); err == nil || !strings.Contains(err.Error(), "INVOCATION_INVALID") {
		t.Fatalf("duplicate invocation error = %v", err)
	}

	valid := FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxPartitions: 1, MaxActivePartitions: 1}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.MaxConcurrentCrashes = 2
	if err := invalid.Validate(); err == nil || err.Error() != "EXPERIMENT_FAULT_ENVELOPE_CONCURRENCY_INVALID" {
		t.Fatalf("invalid envelope error = %v", err)
	}
	config := Config{
		SchemaVersion: SchemaVersion, ID: "unqualified-workload", PSSID: "pss",
		Runtime: RuntimeConfig{SeedHex: "01"}, DecisionsPerRun: 1, RequireReplay: true,
		Runs: []RunPlan{{Run: 1, Policy: Policy{
			Version: PolicyVersion, ID: "invoke", Priority: []control.ActionKind{control.ActionInvoke},
		}, Workload: &plan}},
	}
	if err := config.Validate(); err == nil || err.Error() != "EXPERIMENT_ADMISSION_REQUIRED" {
		t.Fatalf("unqualified workload error = %v", err)
	}
}

func TestFaultEnvelopeChecksSelectedActionsWithoutChangingEnabledSet(t *testing.T) {
	envelope := FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 1,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	usage := FaultUsage{}
	snapshot := controlruntime.Snapshot{Nodes: []controlruntime.NodeSnapshot{
		{Ref: control.NodeRef{Node: "n1", Incarnation: 1}, Lifecycle: control.NodeRunning},
		{Ref: control.NodeRef{Node: "n2", Incarnation: 1}, Lifecycle: control.NodeRunning},
	}}
	crash := control.Action{Kind: control.ActionCrash}
	if err := usage.check(envelope, crash, snapshot); err != nil {
		t.Fatal(err)
	}
	usage.record(crash)
	if err := usage.check(envelope, crash, snapshot); err == nil ||
		err.Error() != "EXPERIMENT_FAULT_ENVELOPE_EXCEEDED: crash" {
		t.Fatalf("second crash error = %v", err)
	}
	if usage != (FaultUsage{Crashes: 1}) {
		t.Fatalf("fault usage = %#v", usage)
	}
}

func TestFaultEnvelopeConstrainsLocalBaselineWithoutEditingRuntimeFrontier(t *testing.T) {
	envelope := FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}
	usage := FaultUsage{Crashes: 1}
	snapshot := controlruntime.Snapshot{Nodes: []controlruntime.NodeSnapshot{{
		Ref: control.NodeRef{Node: "n1", Incarnation: 1}, Lifecycle: control.NodeRunning,
	}}}
	enabled := []control.Action{
		{ID: "crash", Kind: control.ActionCrash},
		{ID: "deliver", Kind: control.ActionDeliverMessage},
	}
	selectable := usage.constrain(envelope, enabled, snapshot)
	if len(selectable) != 1 || selectable[0].ID != "deliver" {
		t.Fatalf("selectable = %#v", selectable)
	}
	if len(enabled) != 2 || enabled[0].ID != "crash" {
		t.Fatalf("Runtime frontier was edited: %#v", enabled)
	}
}

package etcdraftv2_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	adapterv2 "github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

func TestQualificationBundleIsStableAndMechanicallyQualified(t *testing.T) {
	left, err := qualification.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	right, err := qualification.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("bundle digest changed: %s != %s", left.Digest, right.Digest)
	}
	if err := left.Validate(); err != nil {
		t.Fatal(err)
	}
	if !left.Qualification.Qualified {
		t.Fatalf("required portable profile did not qualify: %+v", left.Qualification.Capabilities)
	}
	want := conformance.QualificationSummary{
		Total: 9, Required: 8, Validated: 8, Unsupported: 1,
	}
	if left.Qualification.Summary != want {
		t.Fatalf("qualification summary = %+v, want %+v", left.Qualification.Summary, want)
	}
	if len(left.ConformanceReports) != 3 {
		t.Fatalf("conformance reports = %d, want 3", len(left.ConformanceReports))
	}
}

func TestPortableCFTProfileV2RemainsFullyQualified(t *testing.T) {
	report := portableV2Qualification(t)
	want := conformance.QualificationSummary{Total: 9, Required: 8, Validated: 8, Unsupported: 1}
	if !report.Qualified || report.Summary != want {
		t.Fatalf("portable v2 qualification = qualified:%t summary:%+v, want true/%+v", report.Qualified, report.Summary, want)
	}
}

func portableV2Qualification(t *testing.T) conformance.QualificationReport {
	t.Helper()
	ctx := context.Background()
	factory := func() control.Adapter {
		adapter, err := adapterv2.NewWithConfig(adapterv2.ThreeNodeConfig())
		if err != nil {
			t.Fatal(err)
		}
		return adapter
	}
	manifest, err := factory().Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := conformance.PortableCFTProfileV2()
	if err != nil {
		t.Fatal(err)
	}
	core, err := conformance.EvaluateCore(ctx, factory, conformance.CorePlan{
		Seed: []byte("portable-v2-etcd-core"), ExpectedEntropyNodes: []control.NodeID{"n1", "n2", "n3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := conformance.NaturalLifecyclePlan{Seed: []byte("portable-v2-etcd-lifecycle"), DecisionBound: 192}
	natural, err := conformance.EvaluateNaturalLifecycle(ctx, factory, plan)
	if err != nil {
		t.Fatal(err)
	}
	released, err := conformance.EvaluateReleasedMessageLifecycle(ctx, factory, plan)
	if err != nil {
		t.Fatal(err)
	}
	input, err := adapterv2.InputPayload(adapterv2.Input{
		Operation: adapterv2.OperationPropose, RequestID: "portable-v2", Value: []byte("value"),
	})
	if err != nil {
		t.Fatal(err)
	}
	invokePlan := conformance.OpaqueInvokePlan{Seed: []byte("portable-v2-etcd-invoke"), Node: "n1", Input: input, DecisionBound: 256}
	invokeReplay, err := conformance.EvaluateOpaqueInvoke(ctx, factory, invokePlan)
	if err != nil {
		t.Fatal(err)
	}
	invokeAccepted, err := conformance.EvaluateOpaqueInvokeAccepted(ctx, factory, invokePlan)
	if err != nil {
		t.Fatal(err)
	}
	report, err := conformance.Qualify(manifest, profile, []conformance.UnsupportedDeclaration{{
		CapabilityID: "formal-process-isolation", ReasonCode: conformance.UnsupportedProcessIsolation,
	}}, []conformance.Report{core, natural, released, invokeReplay, invokeAccepted})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestCheckedQualificationBundleMatchesFreshRun(t *testing.T) {
	encoded, err := os.ReadFile("../../benchmarks/qualifications/etcdraft-v2-m5.3/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var checked qualification.Bundle
	if err := json.Unmarshal(encoded, &checked); err != nil {
		t.Fatal(err)
	}
	if err := checked.Validate(); err != nil {
		t.Fatal(err)
	}
	fresh, err := qualification.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if checked.Digest != fresh.Digest {
		t.Fatalf("checked bundle is stale: %s != %s", checked.Digest, fresh.Digest)
	}
}

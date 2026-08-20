package etcdraftv2_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	adapterv2 "github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
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
	if left.Profile.ID != "portable-cft-control-v2" || len(left.ConformanceReports) != 5 {
		t.Fatalf("profile/reports = %s/%d, want portable v2/5", left.Profile.ID, len(left.ConformanceReports))
	}
	if left.Work == nil || left.Work.Validate() != nil || left.Work.WorkUnits == 0 ||
		left.Work.SchedulerDecisions == 0 {
		t.Fatalf("qualification work missing: %+v", left.Work)
	}
}

func TestQualificationUsesConfiguredFiveNodeMembership(t *testing.T) {
	bundle, err := qualification.RunWithConfig(context.Background(), adapterv2.Config{
		NodeCount: 5, ElectionTick: 7, HeartbeatTick: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []control.NodeID{"n1", "n2", "n3", "n4", "n5"}
	if !reflect.DeepEqual(bundle.Manifest.Nodes, want) || !bundle.Qualification.Qualified {
		t.Fatalf("five-node qualification = nodes:%v qualified:%t, want %v/true",
			bundle.Manifest.Nodes, bundle.Qualification.Qualified, want)
	}
	for _, report := range bundle.ConformanceReports {
		if report.ManifestDigest != bundle.Qualification.ManifestDigest {
			t.Fatalf("report %s qualified a different membership", report.Digest)
		}
	}
}

func TestPortableCFTProfileRemainsFullyQualified(t *testing.T) {
	report := portableV2Qualification(t)
	want := conformance.QualificationSummary{Total: 9, Required: 8, Validated: 8, Unsupported: 1}
	if !report.Qualified || report.Summary != want {
		t.Fatalf("portable v2 qualification = qualified:%t summary:%+v, want true/%+v", report.Qualified, report.Summary, want)
	}
}

func TestStrictAdmissionAcceptsEtcdAndRejectsHashicorp(t *testing.T) {
	etcd, err := qualification.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	requirements := controlexperiment.ExecutionRequirements{
		Capabilities: etcd.Profile.RequiredCapabilityIDs(),
	}
	if len(requirements.Capabilities) != 8 {
		t.Fatalf("strict requirements = %d, want 8", len(requirements.Capabilities))
	}
	if _, err := controlexperiment.BindExecutionAdmission(etcd.Qualification, requirements); err != nil {
		t.Fatalf("etcd strict admission: %v", err)
	}

	encoded, err := os.ReadFile("../../benchmarks/qualifications/hashicorp-raft-v2-m5.4c/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var hash conformance.QualificationBundle
	if err := json.Unmarshal(encoded, &hash); err != nil {
		t.Fatal(err)
	}
	if err := hash.Validate(); err != nil {
		t.Fatal(err)
	}
	_, err = controlexperiment.BindExecutionAdmission(hash.Qualification, requirements)
	if err == nil || !strings.HasPrefix(err.Error(), "EXPERIMENT_ADMISSION_CAPABILITY_NOT_VALIDATED:") {
		t.Fatalf("HashiCorp strict admission error = %v", err)
	}
}

func portableV2Qualification(t *testing.T) conformance.QualificationReport {
	t.Helper()
	bundle, err := qualification.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return bundle.Qualification
}

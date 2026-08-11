package hashicorpraftv2_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/hashicorpraftv2"
)

func TestPartialQualificationIsStableAndMechanical(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	left, err := qualification.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	right, err := qualification.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("bundle digest changed: %s != %s", left.Digest, right.Digest)
	}
	if err := left.Validate(); err != nil {
		t.Fatal(err)
	}
	want := conformance.QualificationSummary{Total: 9, Required: 8, Validated: 3, Unsupported: 6}
	if left.Qualification.Qualified || left.Qualification.Summary != want {
		t.Fatalf("qualification = qualified:%t summary:%+v, want false/%+v",
			left.Qualification.Qualified, left.Qualification.Summary, want)
	}
	for _, id := range []string{"runtime-owned-message", "crash-restart-incarnation", "opaque-invoke-boundary"} {
		if status := capabilityStatus(left, id); status != conformance.CapabilityValidated {
			t.Fatalf("capability %s status = %s", id, status)
		}
	}
}

func TestCheckedBundleMatchesFreshRun(t *testing.T) {
	encoded, err := os.ReadFile("../../benchmarks/qualifications/hashicorp-raft-v2-m5.4c/report.json")
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fresh, err := qualification.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Digest != checked.Digest {
		t.Fatalf("checked bundle is stale: %s != %s", checked.Digest, fresh.Digest)
	}
}

func capabilityStatus(bundle qualification.Bundle, id string) conformance.CapabilityStatus {
	for _, capability := range bundle.Qualification.Capabilities {
		if capability.ID == id {
			return capability.Status
		}
	}
	return ""
}

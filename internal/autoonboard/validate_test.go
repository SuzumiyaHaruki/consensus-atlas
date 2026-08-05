package autoonboard_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/bindings"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

func TestEtcdRaftBindingIsMechanicallyValidated(t *testing.T) {
	root := filepath.Join("..", "..")
	contract, err := protocolcontract.Load(filepath.Join(root, "contracts", "etcdraft-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := autoonboard.Load(filepath.Join(root, "onboarding", "etcdraft-binding-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := autoonboard.Validate(context.Background(), root, contract, binding, bindings.New)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "validated" {
		t.Fatalf("status = %q, findings = %+v", report.Status, report.Findings)
	}
	if report.FullySupported {
		t.Fatal("v1 must expose known etcd/raft integration limits")
	}
	if got := len(report.ValidatedObligations); got != 10 {
		t.Fatalf("validated obligations = %d, want 10", got)
	}
	if got := len(report.UnsupportedObligations); got != 2 {
		t.Fatalf("unsupported obligations = %d, want 2", got)
	}
	if got := len(report.Profile.Coverage.Obligations); got != 12 {
		t.Fatalf("compiled denominator = %d, want 12", got)
	}
	if len(report.Witnesses) != 3 {
		t.Fatalf("witness was not strongly validated: %+v", report.Witnesses)
	}
	for _, witness := range report.Witnesses {
		if !witness.Passed || !witness.ReplayStable || !witness.Conformant {
			t.Fatalf("witness was not strongly validated: %+v", witness)
		}
	}
}

func TestBindingCannotForgeContractDigest(t *testing.T) {
	root, contract, binding := loadFixtures(t)
	binding.ContractDigest = "forged"
	report, err := autoonboard.Validate(context.Background(), root, contract, binding, bindings.New)
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, report, "binding.contract-mismatch")
}

func TestBindingCannotInventRuntimeCapability(t *testing.T) {
	root, contract, binding := loadFixtures(t)
	binding.Capabilities[0].RuntimeID = "agent-invented-capability"
	report, err := autoonboard.Validate(context.Background(), root, contract, binding, bindings.New)
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, report, "runtime.capability-unknown")
}

func TestSupportedObligationCannotOmitWitness(t *testing.T) {
	root, contract, binding := loadFixtures(t)
	binding.Witnesses[0].Covers = binding.Witnesses[0].Covers[1:]
	report, err := autoonboard.Validate(context.Background(), root, contract, binding, bindings.New)
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, report, "witness.missing")
	if got := len(report.Profile.Coverage.Obligations); got != 12 {
		t.Fatalf("missing witness changed denominator to %d", got)
	}
}

func TestManifestSupportCannotReplaceCapabilityWitness(t *testing.T) {
	root, contract, binding := loadFixtures(t)
	binding.Witnesses = binding.Witnesses[:1]
	report, err := autoonboard.Validate(context.Background(), root, contract, binding, bindings.New)
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, report, "capability.witness-missing")
	for _, capability := range report.ValidatedCapabilities {
		if capability == "message-drop-duplicate-partition" {
			t.Fatal("manifest self-declaration was accepted without contract evidence")
		}
	}
}

func loadFixtures(t *testing.T) (string, *protocolcontract.Contract, *autoonboard.Binding) {
	t.Helper()
	root := filepath.Join("..", "..")
	contract, err := protocolcontract.Load(filepath.Join(root, "contracts", "etcdraft-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := autoonboard.Load(filepath.Join(root, "onboarding", "etcdraft-binding-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return root, contract, binding
}

func assertFinding(t *testing.T, report autoonboard.Report, code string) {
	t.Helper()
	if report.Status != "invalid" {
		t.Fatalf("status = %q, want invalid", report.Status)
	}
	for _, finding := range report.Findings {
		if finding.Code == code && finding.Actionable {
			return
		}
	}
	t.Fatalf("missing actionable finding %q: %+v", code, report.Findings)
}

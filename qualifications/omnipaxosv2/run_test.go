package omnipaxosv2_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adapterv2 "github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/omnipaxosv2"
)

func TestV3PartialQualificationCreditsIndependentControlPaths(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	left, err := qualification.Run(ctx, workerPath)
	if err != nil {
		t.Fatal(err)
	}
	right, err := qualification.Run(ctx, workerPath)
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("bundle digest changed: %s != %s", left.Digest, right.Digest)
	}
	if err := left.Validate(); err != nil {
		t.Fatal(err)
	}
	want := conformance.QualificationSummary{
		Total: 9, Required: 8, Validated: 6, Unsupported: 3,
	}
	if left.Qualification.Qualified || left.Qualification.Summary != want {
		t.Fatalf("qualification = qualified:%t summary:%+v, want false/%+v",
			left.Qualification.Qualified, left.Qualification.Summary, want)
	}
	if left.Profile.ID != "portable-cft-control-v3" || len(left.Profile.RequiredCapabilityIDs()) != 8 {
		t.Fatalf("portable profile was narrowed: %s/%d", left.Profile.ID, len(left.Profile.RequiredCapabilityIDs()))
	}
	for _, id := range []string{
		conformance.CapabilityStrictYieldEvidence, conformance.CapabilityPureEnabledCheck,
		conformance.CapabilityNaturalTemporal, conformance.CapabilityRuntimeOwnedMessage,
		conformance.CapabilityStrictDecisionReplay, conformance.CapabilityOpaqueInvokeBoundary,
	} {
		if status := capabilityStatus(left, id); status != conformance.CapabilityValidated {
			t.Fatalf("capability %s status=%s, want validated", id, status)
		}
	}
}

func TestAdmissionAcceptsOnlyNamedValidatedSubset(t *testing.T) {
	bundle, err := qualification.Run(context.Background(), buildWorker(t))
	if err != nil {
		t.Fatal(err)
	}
	subset := controlexperiment.ExecutionRequirements{Capabilities: []string{
		conformance.CapabilityStrictYieldEvidence, conformance.CapabilityPureEnabledCheck,
		conformance.CapabilityNaturalTemporal, conformance.CapabilityRuntimeOwnedMessage,
		conformance.CapabilityStrictDecisionReplay, conformance.CapabilityOpaqueInvokeBoundary,
	}}
	admission, err := controlexperiment.BindExecutionAdmission(bundle.Qualification, subset)
	if err != nil {
		t.Fatal(err)
	}
	if err := admission.VerifyQualification(bundle.Qualification); err != nil {
		t.Fatal(err)
	}
	_, err = controlexperiment.BindExecutionAdmission(bundle.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: bundle.Profile.RequiredCapabilityIDs()})
	if err == nil || !strings.HasPrefix(err.Error(), "EXPERIMENT_ADMISSION_CAPABILITY_NOT_VALIDATED:") {
		t.Fatalf("strict Portable admission error=%v", err)
	}
}

func TestV3SubsetAdmitsOpaqueWorkloadWithoutLifecycleOrDurability(t *testing.T) {
	workerPath := buildWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bundle, err := qualification.Run(ctx, workerPath)
	if err != nil {
		t.Fatal(err)
	}
	required := []string{
		conformance.CapabilityStrictYieldEvidence, conformance.CapabilityPureEnabledCheck,
		conformance.CapabilityNaturalTemporal, conformance.CapabilityRuntimeOwnedMessage,
		conformance.CapabilityStrictDecisionReplay, conformance.CapabilityOpaqueInvokeBoundary,
	}
	admission, err := controlexperiment.BindExecutionAdmission(bundle.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: required})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := adapterv2.InputPayload(adapterv2.Input{
		RequestID: "m5.21zr-request-1", Value: []byte("m5.21zr-admitted"),
	})
	if err != nil {
		t.Fatal(err)
	}
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersionV2, ID: "omnipaxos-m5.21zr-admitted-smoke",
		PSSID: adapterv2.CorePSSMappingID, WorkloadRouterID: adapterv2.WorkloadRouterID,
		Admission: &admission, Runtime: controlexperiment.RuntimeConfig{
			SeedHex: hex.EncodeToString([]byte("omnipaxos-m5.21zr-smoke")), MaxClones: 1,
		},
		DecisionsPerRun: 96, RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, StopAfterWorkload: true,
			Policy: controlexperiment.Policy{
				Version: controlexperiment.PolicyVersion, ID: "omnipaxos-progress",
				Priority: []control.ActionKind{
					control.ActionInvoke, control.ActionDeliverMessage, control.ActionFireTemporal,
				},
			},
			Workload: &controlexperiment.WorkloadPlan{
				SchemaVersion: controlexperiment.WorkloadPlanVersion, ID: "single-omnipaxos-write",
				TargetSelector: controlexperiment.TargetSingleCoordinatingMember,
				Invocations: []controlexperiment.WorkloadInvocation{{
					ID: "m5.21zr-request-1", Input: payload, ExpectedStatus: "decided",
				}},
			},
		}},
	}
	factory := func() (control.Adapter, error) {
		return adapterv2.New(adapterv2.Config{WorkerPath: workerPath})
	}
	report, err := controlexperiment.ExecuteQualified(
		ctx, config, bundle.Qualification, factory, adapterv2.CorePSSMapper{}, adapterv2.WorkloadRouter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.ValidateWithQualification(bundle.Qualification); err != nil {
		t.Fatal(err)
	}
	run := report.Runs[0]
	if run.Workload == nil || run.Workload.Completed != 1 || !run.Replay.Stable ||
		run.Termination != controlexperiment.RunTerminationConfigured {
		t.Fatalf("admitted smoke=%+v", run)
	}
	t.Logf("qualification=%s admission=%s report=%s trace=%s decisions=%d core_states=%d",
		bundle.Qualification.Digest, admission.Digest, report.Digest, run.TraceDigest,
		run.ChargedDecisions, report.StateDiscovery.UniqueStates)
}

func TestFrozenM521zBundleIsInternallyValid(t *testing.T) {
	encoded, err := os.ReadFile("../../benchmarks/qualifications/omnipaxos-v2-m5.21z/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle qualification.Bundle
	if err := json.Unmarshal(encoded, &bundle); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	if bundle.Digest != "43f9e40340d7f8f76ab8d795bce779195a6635818597b9cedc00b3c106bb370c" {
		t.Fatalf("frozen bundle digest=%s", bundle.Digest)
	}
}

func TestFrozenM521zRBundleIsInternallyValid(t *testing.T) {
	encoded, err := os.ReadFile("../../benchmarks/qualifications/omnipaxos-v2-m5.21zr/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle qualification.Bundle
	if err := json.Unmarshal(encoded, &bundle); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	if bundle.Digest != "1750d6c32976d71c590a15776e6e09cf4cd15685ea8cf7e3307f819bb0840e66" {
		t.Fatalf("frozen zR bundle digest=%s", bundle.Digest)
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

func buildWorker(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required for the OmniPaxos qualification test")
	}
	manifest := filepath.Join("..", "..", "adapters", "omnipaxosv2", "worker", "Cargo.toml")
	command := exec.Command("cargo", "build", "--locked", "--quiet", "--manifest-path", manifest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build OmniPaxos worker: %v\n%s", err, output)
	}
	path, err := filepath.Abs(filepath.Join("..", "..", "adapters", "omnipaxosv2", "worker", "target", "debug",
		"consensus-atlas-omnipaxos-worker"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

package controlexperiment

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestAdmissionAcceptsValidatedSubsetOfPartialQualification(t *testing.T) {
	report := admissionQualification(t)
	if report.Qualified {
		t.Fatal("fixture must remain partially qualified")
	}
	admission, err := BindExecutionAdmission(report, ExecutionRequirements{
		Capabilities: []string{"strict-replay"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := admission.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := admission.VerifyQualification(report); err != nil {
		t.Fatal(err)
	}
	if admission.RequiredCapabilities[0] != "strict-replay" || admission.QualificationDigest != report.Digest {
		t.Fatalf("unexpected admission: %#v", admission)
	}

	_, err = BindExecutionAdmission(report, ExecutionRequirements{Capabilities: []string{"message-control"}})
	if err == nil || err.Error() != "EXPERIMENT_ADMISSION_CAPABILITY_NOT_VALIDATED: message-control/unsupported" {
		t.Fatalf("unsupported capability error = %v", err)
	}
	_, err = BindExecutionAdmission(report, ExecutionRequirements{Capabilities: []string{"unknown"}})
	if err == nil || err.Error() != "EXPERIMENT_ADMISSION_CAPABILITY_UNKNOWN: unknown" {
		t.Fatalf("unknown capability error = %v", err)
	}
}

func TestAdmissionCannotBeBypassedOrRebound(t *testing.T) {
	report := admissionQualification(t)
	admission, err := BindExecutionAdmission(report, ExecutionRequirements{Capabilities: []string{"strict-replay"}})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{
		SchemaVersion: SchemaVersion, ID: "admitted", PSSID: "pss", Admission: &admission,
		Runtime: RuntimeConfig{SeedHex: "01"}, DecisionsPerRun: 1, RequireReplay: true,
		Runs: []RunPlan{{Run: 1, Policy: Policy{
			Version: PolicyVersion, ID: "progress", Priority: []control.ActionKind{control.ActionCrash},
		}}},
	}
	if _, err := ExecuteLegacy(t.Context(), config, nil, nil); err == nil || err.Error() != "EXPERIMENT_QUALIFICATION_REPORT_REQUIRED" {
		t.Fatalf("unqualified ExecuteLegacy() error = %v", err)
	}

	rebound := report
	rebound.BuildID = "other-build"
	rebound, err = rebound.Seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := admission.VerifyQualification(rebound); err == nil || err.Error() != "EXPERIMENT_ADMISSION_QUALIFICATION_MISMATCH" {
		t.Fatalf("rebound qualification error = %v", err)
	}

	tampered := admission
	tampered.RequiredCapabilities = []string{"message-control"}
	if err := tampered.Validate(); err == nil || err.Error() != "EXPERIMENT_ADMISSION_DIGEST_MISMATCH" {
		t.Fatalf("tampered admission error = %v", err)
	}
	_, err = BindExecutionAdmission(report, ExecutionRequirements{Capabilities: []string{"strict-replay", "strict-replay"}})
	if err == nil || !strings.Contains(err.Error(), "EXPERIMENT_ADMISSION_CAPABILITY_DUPLICATE") {
		t.Fatalf("duplicate capability error = %v", err)
	}
}

func admissionQualification(t *testing.T) conformance.QualificationReport {
	t.Helper()
	digest := strings.Repeat("a", 64)
	report, err := (conformance.QualificationReport{
		ProfileID: "profile", ProfileDigest: digest, AdapterID: "adapter",
		ImplementationID: "implementation", BuildID: "build", ConfigurationDigest: digest,
		ManifestDigest: digest,
		Unsupported: []conformance.UnsupportedDeclaration{{
			CapabilityID: "message-control", ReasonCode: conformance.UnsupportedControlSurface,
		}},
		Capabilities: []conformance.CapabilityQualification{
			{ID: "strict-replay", Required: true, Declared: true, Status: conformance.CapabilityValidated,
				ConformanceCases: []string{"strict-replay-case"}},
			{ID: "message-control", Required: true, Declared: false, Status: conformance.CapabilityUnsupported,
				UnsupportedReasonCode: conformance.UnsupportedControlSurface,
				ConformanceCases:      []string{"message-control-case"}},
		},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	return report
}

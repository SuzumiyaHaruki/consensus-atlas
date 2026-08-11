package controlexperiment

import (
	"reflect"
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

func TestExecuteQualifiedRejectsConfigCapabilityUnderDeclarationBeforeFactory(t *testing.T) {
	report := portableAdmissionQualification(t)
	admission, err := BindExecutionAdmission(report, ExecutionRequirements{Capabilities: []string{
		conformance.CapabilityStrictYieldEvidence,
		conformance.CapabilityPureEnabledCheck,
		conformance.CapabilityStrictDecisionReplay,
	}})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{
		SchemaVersion: SchemaVersion, ID: "underdeclared", PSSID: "pss", Admission: &admission,
		Runtime: RuntimeConfig{SeedHex: "01"}, DecisionsPerRun: 1, RequireReplay: true,
		Runs: []RunPlan{{Run: 1, Policy: Policy{
			Version: PolicyVersion, ID: "temporal", Priority: []control.ActionKind{control.ActionFireTemporal},
		}}},
	}
	factoryCalls := 0
	_, err = ExecuteQualified(t.Context(), config, report, func() (control.Adapter, error) {
		factoryCalls++
		return nil, nil
	}, nil, nil)
	if err == nil || err.Error() != "EXPERIMENT_ADMISSION_CAPABILITY_UNDERDECLARED: natural-temporal-progress" {
		t.Fatalf("underdeclared execution error=%v", err)
	}
	if factoryCalls != 0 {
		t.Fatalf("factory called %d times before admission rejection", factoryCalls)
	}
}

func TestConfigCapabilityLowerBoundIsExactOrConservative(t *testing.T) {
	report := portableAdmissionQualification(t)
	fixed := Config{Runs: []RunPlan{{Policy: Policy{
		Version: PolicyVersion,
		Priority: []control.ActionKind{
			control.ActionInvoke, control.ActionDeliverMessage, control.ActionFireTemporal,
		},
	}, Workload: &WorkloadPlan{}}}}
	want := []string{
		conformance.CapabilityNaturalTemporal,
		conformance.CapabilityOpaqueInvokeBoundary,
		conformance.CapabilityPureEnabledCheck,
		conformance.CapabilityRuntimeOwnedMessage,
		conformance.CapabilityStrictDecisionReplay,
		conformance.CapabilityStrictYieldEvidence,
	}
	if got := minimumConfigCapabilities(fixed, report); !reflect.DeepEqual(got, want) {
		t.Fatalf("fixed lower bound=%v, want %v", got, want)
	}
	bounded := Config{Runs: []RunPlan{{Policy: Policy{
		Version: BoundedActionClassPolicyVersion,
		SelectableActions: []control.ActionKind{
			control.ActionDeliverMessage, control.ActionDropMessage,
			control.ActionFireTemporal, control.ActionInvoke,
		},
	}, Workload: &WorkloadPlan{}}}}
	if got := minimumConfigCapabilities(bounded, report); !reflect.DeepEqual(got, want) {
		t.Fatalf("bounded lower bound=%v, want %v", got, want)
	}

	open := Config{Runs: []RunPlan{{Policy: Policy{Version: RandomPolicyVersion}}}}
	got := minimumConfigCapabilities(open, report)
	if len(got) != 8 {
		t.Fatalf("open-policy lower bound=%v, want all 8 required capabilities", got)
	}
	unknownEffect := Config{Runs: []RunPlan{{Policy: Policy{
		Version: PolicyVersion, Priority: []control.ActionKind{control.ActionCompleteEffect},
	}}}}
	if got := minimumConfigCapabilities(unknownEffect, report); len(got) != 8 {
		t.Fatalf("effect lower bound=%v, want conservative full set", got)
	}
	duplicate := Config{Runs: []RunPlan{{Policy: Policy{
		Version: BoundedActionClassPolicyVersion,
		SelectableActions: []control.ActionKind{
			control.ActionDeliverMessage, control.ActionDuplicateMessage,
		},
	}}}}
	if got := minimumConfigCapabilities(duplicate, report); len(got) != 8 {
		t.Fatalf("unwitnessed duplicate lower bound=%v, want conservative full set", got)
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
	unbound := config
	unbound.Admission = nil
	if _, err := ExecuteQualified(t.Context(), unbound, report, nil, nil, nil); err == nil ||
		err.Error() != "EXPERIMENT_ADMISSION_REQUIRED" {
		t.Fatalf("unbound ExecuteQualified() error = %v", err)
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

func portableAdmissionQualification(t *testing.T) conformance.QualificationReport {
	t.Helper()
	digest := strings.Repeat("b", 64)
	ids := []string{
		conformance.CapabilityStrictYieldEvidence,
		conformance.CapabilityPureEnabledCheck,
		conformance.CapabilityNaturalTemporal,
		conformance.CapabilityRuntimeOwnedMessage,
		conformance.CapabilityCrashRestart,
		conformance.CapabilityStrictDecisionReplay,
		conformance.CapabilityAuditedEntropyReplay,
		conformance.CapabilityOpaqueInvokeBoundary,
	}
	capabilities := make([]conformance.CapabilityQualification, 0, len(ids))
	for _, id := range ids {
		capabilities = append(capabilities, conformance.CapabilityQualification{
			ID: id, Required: true, Declared: true, Status: conformance.CapabilityValidated,
			ConformanceCases: []string{id + "-case"},
		})
	}
	report, err := (conformance.QualificationReport{
		ProfileID: "portable-profile", ProfileDigest: digest, AdapterID: "adapter",
		ImplementationID: "implementation", BuildID: "build", ConfigurationDigest: digest,
		ManifestDigest: digest, Capabilities: capabilities,
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	return report
}

package controlexperiment

import (
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const ExecutionAdmissionSchemaVersion = "consensus-atlas/execution-admission/v1"

// ExecutionRequirements names the externally validated capabilities required
// by one experiment. It does not trust the Adapter Manifest on its own.
type ExecutionRequirements struct {
	Capabilities []string `json:"capabilities"`
}

// ExecutionAdmission is a canonical binding from an Experiment to the exact
// QualificationReport that validated all requested capabilities. A report may
// be partially qualified as long as the experiment's required subset is fully
// validated.
type ExecutionAdmission struct {
	SchemaVersion        string   `json:"schema_version"`
	ProfileID            string   `json:"profile_id"`
	ProfileDigest        string   `json:"profile_digest"`
	QualificationDigest  string   `json:"qualification_digest"`
	AdapterID            string   `json:"adapter_id"`
	ImplementationID     string   `json:"implementation_id"`
	BuildID              string   `json:"build_id"`
	ConfigurationDigest  string   `json:"configuration_digest"`
	ManifestDigest       string   `json:"manifest_digest"`
	RequiredCapabilities []string `json:"required_capabilities"`
	Digest               string   `json:"digest"`
}

func BindExecutionAdmission(
	report conformance.QualificationReport,
	requirements ExecutionRequirements,
) (ExecutionAdmission, error) {
	if err := report.Validate(); err != nil {
		return ExecutionAdmission{}, fmt.Errorf("EXPERIMENT_ADMISSION_QUALIFICATION_INVALID: %w", err)
	}
	required, err := normalizeRequiredCapabilities(requirements.Capabilities)
	if err != nil {
		return ExecutionAdmission{}, err
	}
	if err := requireValidatedCapabilities(report, required); err != nil {
		return ExecutionAdmission{}, err
	}
	return (ExecutionAdmission{
		SchemaVersion: ExecutionAdmissionSchemaVersion,
		ProfileID:     report.ProfileID, ProfileDigest: report.ProfileDigest,
		QualificationDigest: report.Digest, AdapterID: report.AdapterID,
		ImplementationID: report.ImplementationID, BuildID: report.BuildID,
		ConfigurationDigest: report.ConfigurationDigest, ManifestDigest: report.ManifestDigest,
		RequiredCapabilities: required,
	}).seal()
}

func (admission ExecutionAdmission) Validate() error {
	if admission.SchemaVersion != ExecutionAdmissionSchemaVersion || admission.ProfileID == "" ||
		admission.AdapterID == "" || admission.ImplementationID == "" || admission.BuildID == "" ||
		!validSHA256(admission.ProfileDigest) || !validSHA256(admission.QualificationDigest) ||
		!validSHA256(admission.ConfigurationDigest) || !validSHA256(admission.ManifestDigest) {
		return errors.New("EXPERIMENT_ADMISSION_IDENTITY_INVALID")
	}
	required, err := normalizeRequiredCapabilities(admission.RequiredCapabilities)
	if err != nil {
		return err
	}
	for index := range required {
		if required[index] != admission.RequiredCapabilities[index] {
			return errors.New("EXPERIMENT_ADMISSION_CAPABILITIES_NOT_CANONICAL")
		}
	}
	sealed, err := admission.seal()
	if err != nil {
		return err
	}
	if admission.Digest == "" || sealed.Digest != admission.Digest {
		return errors.New("EXPERIMENT_ADMISSION_DIGEST_MISMATCH")
	}
	return nil
}

func (admission ExecutionAdmission) VerifyQualification(report conformance.QualificationReport) error {
	if err := admission.Validate(); err != nil {
		return err
	}
	if err := report.Validate(); err != nil {
		return fmt.Errorf("EXPERIMENT_ADMISSION_QUALIFICATION_INVALID: %w", err)
	}
	if admission.ProfileID != report.ProfileID || admission.ProfileDigest != report.ProfileDigest ||
		admission.QualificationDigest != report.Digest || admission.AdapterID != report.AdapterID ||
		admission.ImplementationID != report.ImplementationID || admission.BuildID != report.BuildID ||
		admission.ConfigurationDigest != report.ConfigurationDigest ||
		admission.ManifestDigest != report.ManifestDigest {
		return errors.New("EXPERIMENT_ADMISSION_QUALIFICATION_MISMATCH")
	}
	return requireValidatedCapabilities(report, admission.RequiredCapabilities)
}

// VerifyConfigAdmission checks the caller-declared admission against a
// conservative capability lower bound derived from the exact Config. It runs
// before any Adapter factory or SUT process is invoked.
func VerifyConfigAdmission(config Config, report conformance.QualificationReport) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if config.Admission == nil {
		return errors.New("EXPERIMENT_ADMISSION_REQUIRED")
	}
	if err := config.Admission.VerifyQualification(report); err != nil {
		return err
	}
	declared := stringSet(config.Admission.RequiredCapabilities)
	for _, capability := range minimumConfigCapabilities(config, report) {
		if _, ok := declared[capability]; !ok {
			return fmt.Errorf("EXPERIMENT_ADMISSION_CAPABILITY_UNDERDECLARED: %s", capability)
		}
	}
	return nil
}

func minimumConfigCapabilities(config Config, report conformance.QualificationReport) []string {
	required := map[string]struct{}{
		conformance.CapabilityStrictYieldEvidence:  {},
		conformance.CapabilityPureEnabledCheck:     {},
		conformance.CapabilityStrictDecisionReplay: {},
	}
	conservativeFull := false
	for _, run := range config.Runs {
		if run.Workload != nil {
			required[conformance.CapabilityOpaqueInvokeBoundary] = struct{}{}
		}
		var kinds []control.ActionKind
		switch {
		case run.Policy.Version == PolicyVersion:
			kinds = append(kinds, run.Policy.Priority...)
			for _, rule := range run.Policy.Rules {
				kinds = append(kinds, rule.Kind)
			}
		case run.Policy.bounded():
			kinds = append(kinds, run.Policy.SelectableActions...)
		default:
			conservativeFull = true
			continue
		}
		for _, kind := range kinds {
			if !addActionCapability(required, kind) {
				conservativeFull = true
			}
		}
	}
	if config.FaultEnvelope != nil {
		envelope := *config.FaultEnvelope
		if envelope.MaxCrashes > 0 || envelope.MaxConcurrentCrashes > 0 {
			required[conformance.CapabilityCrashRestart] = struct{}{}
		}
		if envelope.MaxMessageDrops > 0 || envelope.MaxMessageDuplicates > 0 ||
			envelope.MaxPartitions > 0 || envelope.MaxActivePartitions > 0 {
			required[conformance.CapabilityRuntimeOwnedMessage] = struct{}{}
		}
	}
	if conservativeFull {
		for _, capability := range report.Capabilities {
			if capability.Required {
				required[capability.ID] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(required))
	for capability := range required {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

func addActionCapability(required map[string]struct{}, kind control.ActionKind) bool {
	switch kind {
	case control.ActionInvoke:
		required[conformance.CapabilityOpaqueInvokeBoundary] = struct{}{}
	case control.ActionFireTemporal:
		required[conformance.CapabilityNaturalTemporal] = struct{}{}
	case control.ActionDeliverMessage, control.ActionDropMessage:
		required[conformance.CapabilityRuntimeOwnedMessage] = struct{}{}
	case control.ActionCrash, control.ActionRestart:
		required[conformance.CapabilityCrashRestart] = struct{}{}
	default:
		return false
	}
	return true
}

func (admission ExecutionAdmission) seal() (ExecutionAdmission, error) {
	admission.RequiredCapabilities = append([]string(nil), admission.RequiredCapabilities...)
	sort.Strings(admission.RequiredCapabilities)
	admission.Digest = ""
	digest, err := control.CanonicalDigest(admission)
	if err != nil {
		return ExecutionAdmission{}, err
	}
	admission.Digest = digest
	return admission, nil
}

func normalizeRequiredCapabilities(capabilities []string) ([]string, error) {
	if len(capabilities) == 0 {
		return nil, errors.New("EXPERIMENT_ADMISSION_CAPABILITIES_REQUIRED")
	}
	result := append([]string(nil), capabilities...)
	sort.Strings(result)
	for index, capability := range result {
		if capability == "" {
			return nil, errors.New("EXPERIMENT_ADMISSION_CAPABILITY_ID_REQUIRED")
		}
		if index > 0 && result[index-1] == capability {
			return nil, fmt.Errorf("EXPERIMENT_ADMISSION_CAPABILITY_DUPLICATE: %s", capability)
		}
	}
	return result, nil
}

func requireValidatedCapabilities(report conformance.QualificationReport, required []string) error {
	available := make(map[string]conformance.CapabilityStatus, len(report.Capabilities))
	for _, capability := range report.Capabilities {
		available[capability.ID] = capability.Status
	}
	for _, capability := range required {
		status, ok := available[capability]
		if !ok {
			return fmt.Errorf("EXPERIMENT_ADMISSION_CAPABILITY_UNKNOWN: %s", capability)
		}
		if status != conformance.CapabilityValidated {
			return fmt.Errorf("EXPERIMENT_ADMISSION_CAPABILITY_NOT_VALIDATED: %s/%s", capability, status)
		}
	}
	return nil
}

func (report Report) ValidateWithQualification(qualification conformance.QualificationReport) error {
	if err := report.Validate(); err != nil {
		return err
	}
	if report.Config.Admission == nil {
		return errors.New("EXPERIMENT_ADMISSION_REQUIRED")
	}
	if report.ManifestDigest != report.Config.Admission.ManifestDigest {
		return errors.New("EXPERIMENT_ADMISSION_MANIFEST_MISMATCH")
	}
	return VerifyConfigAdmission(report.Config, qualification)
}

package controlexperiment

import (
	"errors"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	MethodSpecSchemaVersion = "consensus-atlas/method-spec/v1"
	MethodExecutorCLI       = "consensus-atlas/control-experiment-cli/v1"
)

// MethodSpec is the frozen, target-independent invocation of one testing
// method. It binds the exact non-SUT Config projection expected from every
// evaluated build; build and qualification identities are checked separately.
type MethodSpec struct {
	SchemaVersion           string       `json:"schema_version"`
	ID                      string       `json:"id"`
	ExecutorID              string       `json:"executor_id"`
	Strategy                string       `json:"strategy"`
	Decisions               int          `json:"decisions"`
	PolicySeed              uint64       `json:"policy_seed"`
	Budget                  MethodBudget `json:"budget"`
	PSSID                   string       `json:"pss_id"`
	ProjectorID             string       `json:"projector_id"`
	ConfigProjectionDigest  string       `json:"config_projection_digest"`
	RequiredBundleSchema    string       `json:"required_bundle_schema"`
	RequiredOperationSchema string       `json:"required_operation_schema"`
	TimeoutMillis           int          `json:"timeout_millis"`
	Digest                  string       `json:"digest"`
}

func NewMethodSpec(spec MethodSpec) (MethodSpec, error) {
	spec.SchemaVersion = MethodSpecSchemaVersion
	spec.ExecutorID = MethodExecutorCLI
	spec.RequiredBundleSchema = ExecutionBundleSchemaVersionV3
	spec.RequiredOperationSchema = OperationHistorySchemaVersion
	sealed, err := spec.seal()
	if err != nil {
		return MethodSpec{}, err
	}
	if err := sealed.Validate(); err != nil {
		return MethodSpec{}, err
	}
	return sealed, nil
}

func (spec MethodSpec) Validate() error {
	if spec.SchemaVersion != MethodSpecSchemaVersion || spec.ID == "" ||
		spec.ExecutorID != MethodExecutorCLI || !validMethodToken(spec.Strategy) ||
		spec.Decisions <= 0 || spec.PSSID == "" || spec.ProjectorID == "" ||
		!validSHA256(spec.ConfigProjectionDigest) ||
		spec.RequiredBundleSchema != ExecutionBundleSchemaVersionV3 ||
		spec.RequiredOperationSchema != OperationHistorySchemaVersion ||
		spec.TimeoutMillis <= 0 || spec.TimeoutMillis > 600_000 {
		return errors.New("EXPERIMENT_METHOD_SPEC_INVALID")
	}
	if err := spec.Budget.validate(); err != nil {
		return err
	}
	if spec.Budget.MaxExecutionAttempts != 1 ||
		spec.Budget.MaxPrimaryWorkUnits < spec.Decisions ||
		spec.Budget.MaxReplayWorkUnits < spec.Decisions {
		return errors.New("EXPERIMENT_METHOD_SPEC_BUDGET_INVALID")
	}
	sealed, err := spec.seal()
	if err != nil || !validSHA256(spec.Digest) || sealed.Digest != spec.Digest {
		return errors.New("EXPERIMENT_METHOD_SPEC_DIGEST_MISMATCH")
	}
	return nil
}

func (spec MethodSpec) ValidateExecution(report Report, bundle ExecutionBundle) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := report.Validate(); err != nil {
		return err
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	projection, err := MethodConfigProjectionDigest(report.Config)
	if err != nil {
		return err
	}
	if projection != spec.ConfigProjectionDigest || report.PSSID != spec.PSSID ||
		report.Config.DecisionsPerRun != spec.Decisions || len(report.Runs) != 1 ||
		bundle.SchemaVersion != spec.RequiredBundleSchema || bundle.OperationHistory == nil ||
		bundle.OperationHistory.SchemaVersion != spec.RequiredOperationSchema ||
		bundle.Identity.MethodSpecDigest != spec.Digest ||
		bundle.Identity.ReportDigest != report.Digest ||
		bundle.Identity.ConfigDigest != report.ConfigDigest {
		return errors.New("EXPERIMENT_METHOD_SPEC_EXECUTION_MISMATCH")
	}
	if report.Work.Primary.WorkUnits > spec.Budget.MaxPrimaryWorkUnits ||
		report.Work.Replay.WorkUnits > spec.Budget.MaxReplayWorkUnits {
		return errors.New("EXPERIMENT_METHOD_SPEC_BUDGET_EXCEEDED")
	}
	return nil
}

func (spec MethodSpec) seal() (MethodSpec, error) {
	spec.Digest = ""
	digest, err := portableJSONDigest(spec)
	if err != nil {
		return MethodSpec{}, err
	}
	spec.Digest = digest
	return spec, nil
}

type methodAdmissionProjection struct {
	SchemaVersion        string   `json:"schema_version"`
	ProfileID            string   `json:"profile_id"`
	ProfileDigest        string   `json:"profile_digest"`
	AdapterID            string   `json:"adapter_id"`
	ImplementationID     string   `json:"implementation_id"`
	ConfigurationDigest  string   `json:"configuration_digest"`
	RequiredCapabilities []string `json:"required_capabilities"`
}

type methodConfigProjection struct {
	SchemaVersion    string                    `json:"schema_version"`
	ID               string                    `json:"id"`
	PSSID            string                    `json:"pss_id"`
	Runtime          RuntimeConfig             `json:"runtime"`
	Admission        methodAdmissionProjection `json:"admission"`
	FaultEnvelope    *FaultEnvelope            `json:"fault_envelope,omitempty"`
	WorkloadRouterID string                    `json:"workload_router_id,omitempty"`
	DecisionsPerRun  int                       `json:"decisions_per_run"`
	RequireReplay    bool                      `json:"require_replay"`
	Runs             []RunPlan                 `json:"runs"`
}

// MethodConfigProjectionDigest removes only per-build proof identities. The
// protocol-neutral execution semantics, required capabilities, workload,
// fault envelope, policy, seeds, and termination configuration remain bound.
func MethodConfigProjectionDigest(config Config) (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}
	if config.Admission == nil {
		return "", errors.New("EXPERIMENT_METHOD_CONFIG_ADMISSION_REQUIRED")
	}
	projection := methodConfigProjection{
		SchemaVersion: config.SchemaVersion, ID: config.ID, PSSID: config.PSSID,
		Runtime: config.Runtime, FaultEnvelope: config.FaultEnvelope,
		WorkloadRouterID: config.WorkloadRouterID, DecisionsPerRun: config.DecisionsPerRun,
		RequireReplay: config.RequireReplay, Runs: config.Runs,
		Admission: methodAdmissionProjection{
			SchemaVersion: config.Admission.SchemaVersion,
			ProfileID:     config.Admission.ProfileID, ProfileDigest: config.Admission.ProfileDigest,
			AdapterID: config.Admission.AdapterID, ImplementationID: config.Admission.ImplementationID,
			ConfigurationDigest:  config.Admission.ConfigurationDigest,
			RequiredCapabilities: append([]string(nil), config.Admission.RequiredCapabilities...),
		},
	}
	return control.CanonicalDigest(projection)
}

func validMethodToken(value string) bool {
	if value == "" || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}

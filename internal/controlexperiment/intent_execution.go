package controlexperiment

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	CompiledIntentPlanVersionV2    = "consensus-atlas/compiled-test-intent-plan/v2"
	IntentExecutionInstanceVersion = "consensus-atlas/intent-execution-instance/v1"
	IntentOutcomeVersion           = "consensus-atlas/intent-outcome/v1"

	IntentExecutionValid              = "valid"
	IntentReachabilityReached         = "reached"
	IntentReachabilityNotReached      = "not-reached"
	IntentReasonRequiredActionMissing = "INTENT_REQUIRED_ACTION_NOT_REACHED"
	IntentOracleNotEvaluated          = "not-evaluated"
)

// CompiledIntentPlanV2 is a reusable macro plan. Unlike the historical v1
// plan, it does not bind the catalog's default policy seed. A trusted
// IntentExecutionInstance supplies the seed for each concrete execution.
type CompiledIntentPlanV2 struct {
	SchemaVersion        string                 `json:"schema_version"`
	ID                   string                 `json:"id"`
	ViewDigest           string                 `json:"view_digest"`
	IntentDigest         string                 `json:"intent_digest"`
	CatalogDigest        string                 `json:"catalog_digest"`
	RiskID               string                 `json:"risk_id"`
	BackendID            string                 `json:"backend_id"`
	Strategy             string                 `json:"strategy"`
	Decisions            int                    `json:"decisions"`
	FaultEnvelope        FaultEnvelope          `json:"fault_envelope"`
	RequiredCapabilities []string               `json:"required_capabilities"`
	RequiredActions      []control.ActionKind   `json:"required_actions"`
	PreferenceMisses     []IntentPreferenceMiss `json:"preference_misses,omitempty"`
	FallbackUsed         bool                   `json:"fallback_used"`
	CompilerWork         IntentCompilerWork     `json:"compiler_work"`
	Digest               string                 `json:"digest"`
}

func CompileGuardedTestIntentV2(
	view AgentSemanticView,
	pack ProtocolKnowledgePack,
	catalog IntentCompilerCatalog,
	manifest control.AdapterManifest,
	qualification conformance.QualificationReport,
	intent GuardedTestIntent,
) (CompiledIntentPlanV2, error) {
	for _, template := range catalog.Templates {
		if template.PolicySeed != 0 {
			return CompiledIntentPlanV2{}, errors.New("EXPERIMENT_COMPILED_INTENT_V2_CATALOG_SEED_BOUND")
		}
	}
	legacy, err := CompileGuardedTestIntent(view, pack, catalog, manifest, qualification, intent)
	if err != nil {
		return CompiledIntentPlanV2{}, err
	}
	plan := CompiledIntentPlanV2{
		SchemaVersion: CompiledIntentPlanVersionV2,
		ID:            legacy.ID, ViewDigest: legacy.ViewDigest, IntentDigest: legacy.IntentDigest,
		CatalogDigest: legacy.CatalogDigest, RiskID: legacy.RiskID,
		BackendID: legacy.BackendID, Strategy: legacy.Strategy, Decisions: legacy.Decisions,
		FaultEnvelope:        legacy.FaultEnvelope,
		RequiredCapabilities: append([]string(nil), legacy.RequiredCapabilities...),
		RequiredActions:      append([]control.ActionKind(nil), legacy.RequiredActions...),
		PreferenceMisses:     append([]IntentPreferenceMiss(nil), legacy.PreferenceMisses...),
		FallbackUsed:         legacy.FallbackUsed, CompilerWork: legacy.CompilerWork,
	}
	plan, err = plan.seal()
	if err != nil {
		return CompiledIntentPlanV2{}, err
	}
	if err := plan.Validate(); err != nil {
		return CompiledIntentPlanV2{}, err
	}
	return plan, nil
}

func (plan CompiledIntentPlanV2) Validate() error {
	if plan.SchemaVersion != CompiledIntentPlanVersionV2 || !validMethodToken(plan.ID) ||
		!validSHA256(plan.ViewDigest) || !validSHA256(plan.IntentDigest) ||
		!validSHA256(plan.CatalogDigest) || !validMethodToken(plan.RiskID) ||
		!validMethodToken(plan.BackendID) || !validMethodToken(plan.Strategy) || plan.Decisions <= 0 ||
		plan.FaultEnvelope.Validate() != nil || !canonicalStrings(plan.RequiredCapabilities, true) ||
		!canonicalActionKinds(plan.RequiredActions, true) || plan.CompilerWork.CandidatesEvaluated <= 0 ||
		plan.CompilerWork.PreferenceChecks < 0 ||
		plan.CompilerWork.WorkUnits != plan.CompilerWork.CandidatesEvaluated+plan.CompilerWork.PreferenceChecks {
		return errors.New("EXPERIMENT_COMPILED_INTENT_V2_INVALID")
	}
	for _, miss := range plan.PreferenceMisses {
		if miss.Code != "INTENT_PREFERENCE_UNAVAILABLE" ||
			(miss.Kind != "backend" && miss.Kind != "action") || miss.Value == "" {
			return errors.New("EXPERIMENT_COMPILED_INTENT_V2_PREFERENCE_MISS_INVALID")
		}
	}
	sealed, err := plan.seal()
	if err != nil || !validSHA256(plan.Digest) || sealed.Digest != plan.Digest {
		return errors.New("EXPERIMENT_COMPILED_INTENT_V2_DIGEST_MISMATCH")
	}
	return nil
}

func (plan CompiledIntentPlanV2) ValidateInputs(
	view AgentSemanticView,
	pack ProtocolKnowledgePack,
	catalog IntentCompilerCatalog,
	manifest control.AdapterManifest,
	qualification conformance.QualificationReport,
	intent GuardedTestIntent,
) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	want, err := CompileGuardedTestIntentV2(view, pack, catalog, manifest, qualification, intent)
	if err != nil {
		return err
	}
	if want.Digest != plan.Digest {
		return errors.New("EXPERIMENT_COMPILED_INTENT_V2_INPUT_MISMATCH")
	}
	return nil
}

func (plan CompiledIntentPlanV2) seal() (CompiledIntentPlanV2, error) {
	plan.RequiredCapabilities = append([]string(nil), plan.RequiredCapabilities...)
	plan.RequiredActions = append([]control.ActionKind(nil), plan.RequiredActions...)
	plan.PreferenceMisses = append([]IntentPreferenceMiss(nil), plan.PreferenceMisses...)
	plan.Digest = ""
	digest, err := portableJSONDigest(plan)
	if err != nil {
		return CompiledIntentPlanV2{}, err
	}
	plan.Digest = digest
	return plan, nil
}

type IntentExecutionInstance struct {
	SchemaVersion string       `json:"schema_version"`
	ID            string       `json:"id"`
	PlanDigest    string       `json:"plan_digest"`
	PolicySeed    uint64       `json:"policy_seed"`
	Budget        MethodBudget `json:"budget"`
	Digest        string       `json:"digest"`
}

func NewIntentExecutionInstance(
	id string,
	plan CompiledIntentPlanV2,
	policySeed uint64,
	budget MethodBudget,
) (IntentExecutionInstance, error) {
	if err := plan.Validate(); err != nil {
		return IntentExecutionInstance{}, err
	}
	instance := IntentExecutionInstance{
		SchemaVersion: IntentExecutionInstanceVersion, ID: id,
		PlanDigest: plan.Digest, PolicySeed: policySeed, Budget: budget,
	}
	instance, err := instance.seal()
	if err != nil {
		return IntentExecutionInstance{}, err
	}
	if err := instance.ValidatePlan(plan); err != nil {
		return IntentExecutionInstance{}, err
	}
	return instance, nil
}

func (instance IntentExecutionInstance) Validate() error {
	if instance.SchemaVersion != IntentExecutionInstanceVersion || !validMethodToken(instance.ID) ||
		!validSHA256(instance.PlanDigest) || instance.Budget.validate() != nil ||
		instance.Budget.MaxExecutionAttempts != 1 {
		return errors.New("EXPERIMENT_INTENT_EXECUTION_INSTANCE_INVALID")
	}
	sealed, err := instance.seal()
	if err != nil || !validSHA256(instance.Digest) || sealed.Digest != instance.Digest {
		return errors.New("EXPERIMENT_INTENT_EXECUTION_INSTANCE_DIGEST_MISMATCH")
	}
	return nil
}

func (instance IntentExecutionInstance) ValidatePlan(plan CompiledIntentPlanV2) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if instance.PlanDigest != plan.Digest ||
		instance.Budget.MaxPrimaryWorkUnits < plan.Decisions ||
		instance.Budget.MaxReplayWorkUnits < plan.Decisions {
		return errors.New("EXPERIMENT_INTENT_EXECUTION_INSTANCE_PLAN_MISMATCH")
	}
	return nil
}

func (instance IntentExecutionInstance) seal() (IntentExecutionInstance, error) {
	instance.Digest = ""
	digest, err := portableJSONDigest(instance)
	if err != nil {
		return IntentExecutionInstance{}, err
	}
	instance.Digest = digest
	return instance, nil
}

type IntentOutcome struct {
	SchemaVersion           string               `json:"schema_version"`
	ID                      string               `json:"id"`
	ExecutionInstanceDigest string               `json:"execution_instance_digest"`
	PlanDigest              string               `json:"plan_digest"`
	ReportDigest            string               `json:"report_digest"`
	BundleDigest            string               `json:"bundle_digest"`
	ExecutionStatus         string               `json:"execution_status"`
	IntentStatus            string               `json:"intent_status"`
	ReasonCode              string               `json:"reason_code,omitempty"`
	MissingActions          []control.ActionKind `json:"missing_actions,omitempty"`
	OracleStatus            string               `json:"oracle_status"`
	Digest                  string               `json:"digest"`
}

func NewIntentOutcome(
	id string,
	plan CompiledIntentPlanV2,
	instance IntentExecutionInstance,
	report Report,
	bundle ExecutionBundle,
) (IntentOutcome, error) {
	if !validMethodToken(id) {
		return IntentOutcome{}, errors.New("EXPERIMENT_INTENT_OUTCOME_ID_INVALID")
	}
	if err := validateIntentExecutionInputs(plan, instance, report, bundle); err != nil {
		return IntentOutcome{}, err
	}
	observed := make(map[control.ActionKind]bool)
	for _, record := range bundle.Trace.Records {
		observed[record.Action.Kind] = true
	}
	missing := make([]control.ActionKind, 0)
	for _, action := range plan.RequiredActions {
		if !observed[action] {
			missing = append(missing, action)
		}
	}
	outcome := IntentOutcome{
		SchemaVersion: IntentOutcomeVersion, ID: id,
		ExecutionInstanceDigest: instance.Digest, PlanDigest: plan.Digest,
		ReportDigest: report.Digest, BundleDigest: bundle.Digest,
		ExecutionStatus: IntentExecutionValid, IntentStatus: IntentReachabilityReached,
		OracleStatus: IntentOracleNotEvaluated,
	}
	if len(missing) > 0 {
		outcome.IntentStatus = IntentReachabilityNotReached
		outcome.ReasonCode = IntentReasonRequiredActionMissing
		outcome.MissingActions = missing
	}
	outcome, err := outcome.seal()
	if err != nil {
		return IntentOutcome{}, err
	}
	if err := outcome.Validate(); err != nil {
		return IntentOutcome{}, err
	}
	return outcome, nil
}

func (outcome IntentOutcome) Validate() error {
	if outcome.SchemaVersion != IntentOutcomeVersion || !validMethodToken(outcome.ID) ||
		!validSHA256(outcome.ExecutionInstanceDigest) || !validSHA256(outcome.PlanDigest) ||
		!validSHA256(outcome.ReportDigest) || !validSHA256(outcome.BundleDigest) ||
		outcome.ExecutionStatus != IntentExecutionValid ||
		(outcome.IntentStatus != IntentReachabilityReached &&
			outcome.IntentStatus != IntentReachabilityNotReached) ||
		outcome.OracleStatus != IntentOracleNotEvaluated ||
		!canonicalActionKinds(outcome.MissingActions, false) {
		return errors.New("EXPERIMENT_INTENT_OUTCOME_INVALID")
	}
	if outcome.IntentStatus == IntentReachabilityReached &&
		(outcome.ReasonCode != "" || len(outcome.MissingActions) != 0) {
		return errors.New("EXPERIMENT_INTENT_OUTCOME_REACHABILITY_INVALID")
	}
	if outcome.IntentStatus == IntentReachabilityNotReached &&
		(outcome.ReasonCode != IntentReasonRequiredActionMissing || len(outcome.MissingActions) == 0) {
		return errors.New("EXPERIMENT_INTENT_OUTCOME_REACHABILITY_INVALID")
	}
	sealed, err := outcome.seal()
	if err != nil || !validSHA256(outcome.Digest) || sealed.Digest != outcome.Digest {
		return errors.New("EXPERIMENT_INTENT_OUTCOME_DIGEST_MISMATCH")
	}
	return nil
}

func (outcome IntentOutcome) ValidateInputs(
	plan CompiledIntentPlanV2,
	instance IntentExecutionInstance,
	report Report,
	bundle ExecutionBundle,
) error {
	if err := outcome.Validate(); err != nil {
		return err
	}
	want, err := NewIntentOutcome(outcome.ID, plan, instance, report, bundle)
	if err != nil {
		return err
	}
	if want.Digest != outcome.Digest {
		return errors.New("EXPERIMENT_INTENT_OUTCOME_INPUT_MISMATCH")
	}
	return nil
}

func (outcome IntentOutcome) seal() (IntentOutcome, error) {
	outcome.MissingActions = append([]control.ActionKind(nil), outcome.MissingActions...)
	sort.Slice(outcome.MissingActions, func(i, j int) bool {
		return outcome.MissingActions[i] < outcome.MissingActions[j]
	})
	outcome.Digest = ""
	digest, err := portableJSONDigest(outcome)
	if err != nil {
		return IntentOutcome{}, err
	}
	outcome.Digest = digest
	return outcome, nil
}

func validateIntentExecutionInputs(
	plan CompiledIntentPlanV2,
	instance IntentExecutionInstance,
	report Report,
	bundle ExecutionBundle,
) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := instance.ValidatePlan(plan); err != nil {
		return err
	}
	if err := report.Validate(); err != nil {
		return err
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	if bundle.Identity.ReportDigest != report.Digest || report.Config.DecisionsPerRun != plan.Decisions ||
		report.Config.FaultEnvelope == nil || *report.Config.FaultEnvelope != plan.FaultEnvelope ||
		report.Config.Admission == nil || len(report.Runs) != 1 ||
		!stringSubset(plan.RequiredCapabilities, stringSet(report.Config.Admission.RequiredCapabilities)) ||
		report.Work.Primary.WorkUnits > instance.Budget.MaxPrimaryWorkUnits ||
		report.Work.Replay.WorkUnits > instance.Budget.MaxReplayWorkUnits {
		return errors.New("EXPERIMENT_INTENT_EXECUTION_INVALID")
	}
	return nil
}

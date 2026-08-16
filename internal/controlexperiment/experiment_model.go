// Package controlexperiment executes bounded, replay-checked measurements on
// Control Runtime v2. It records state discovery but does not decide protocol
// correctness, Coverage, or Oracle results.
package controlexperiment

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

const (
	SchemaVersion                   = "consensus-atlas/control-experiment/v1"
	SchemaVersionV2                 = "consensus-atlas/control-experiment/v2"
	PolicyVersion                   = "consensus-atlas/action-priority-policy/v1"
	RandomPolicyVersion             = "consensus-atlas/uniform-random-policy/v1"
	AdmissibleUniformPolicyVersion  = "consensus-atlas/admissible-uniform-random-policy/v1"
	BoundedUniformPolicyVersion     = "consensus-atlas/admissible-uniform-random-policy/v2"
	ActionClassPolicyVersion        = "consensus-atlas/action-class-random-policy/v1"
	BoundedActionClassPolicyVersion = "consensus-atlas/action-class-random-policy/v2"
	StatusMeasurementComplete       = "measurement-complete"
	ResourceNotCollected            = "not-collected"
	RunTerminationBudget            = "budget-exhausted"
	RunTerminationQuiescent         = "quiescent"
	RunTerminationPolicySurface     = "policy-surface-exhausted"
	RunTerminationConfigured        = "configured-stop"
)

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func canonicalStrings(values []string, require bool) bool {
	if require && len(values) == 0 {
		return false
	}
	for index, value := range values {
		if value == "" || (index > 0 && values[index-1] >= value) {
			return false
		}
	}
	return true
}

func canonicalActionKinds(values []control.ActionKind, require bool) bool {
	if require && len(values) == 0 {
		return false
	}
	for index, value := range values {
		if value.Validate() != nil || (index > 0 && values[index-1] >= value) {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func actionKindSet(values []control.ActionKind) map[control.ActionKind]bool {
	result := make(map[control.ActionKind]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

type RuntimeConfig struct {
	SeedHex    string `json:"seed_hex"`
	ClockError uint64 `json:"clock_error"`
	MaxClones  uint64 `json:"max_clones"`
}

func (config RuntimeConfig) runtimeConfig() (controlruntime.Config, error) {
	seed, err := hex.DecodeString(config.SeedHex)
	if err != nil || len(seed) == 0 {
		return controlruntime.Config{}, errors.New("EXPERIMENT_RUNTIME_SEED_INVALID")
	}
	return controlruntime.Config{Seed: seed, ClockError: config.ClockError, MaxClones: config.MaxClones}, nil
}

type DecisionRule struct {
	Decision int                `json:"decision"`
	Kind     control.ActionKind `json:"kind"`
	Node     control.NodeID     `json:"node,omitempty"`
	ActionID control.ActionID   `json:"action_id,omitempty"`
	// Parameters are retained only for author-supplied Actions that must be
	// reconstructed through an Offer API before exact policy selection.
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

// Policy is intentionally limited to common Action fields. Rules override the
// fallback priority at exact one-based decision numbers. SelectableActions is
// only valid for bounded stochastic policies and is part of their identity.
type Policy struct {
	Version           string               `json:"version"`
	ID                string               `json:"id"`
	SeedHex           string               `json:"seed_hex,omitempty"`
	Rules             []DecisionRule       `json:"rules,omitempty"`
	Priority          []control.ActionKind `json:"priority,omitempty"`
	SelectableActions []control.ActionKind `json:"selectable_actions,omitempty"`
}

type policySelectionError struct {
	code   string
	detail string
}

func (failure *policySelectionError) Error() string {
	if failure.detail == "" {
		return failure.code
	}
	return failure.code + ": " + failure.detail
}

func (failure *policySelectionError) failureCode() string {
	return failure.code
}

func (policy Policy) Validate(decisionBudget int) error {
	if policy.ID == "" {
		return errors.New("EXPERIMENT_POLICY_IDENTITY_INVALID")
	}
	if policy.Version == RandomPolicyVersion {
		seed, err := hex.DecodeString(policy.SeedHex)
		if err != nil || len(seed) == 0 || len(policy.Rules) != 0 || len(policy.Priority) != 0 ||
			len(policy.SelectableActions) != 0 {
			return errors.New("EXPERIMENT_RANDOM_POLICY_INVALID")
		}
		return nil
	}
	if policy.Version == AdmissibleUniformPolicyVersion {
		seed, err := hex.DecodeString(policy.SeedHex)
		if err != nil || len(seed) == 0 || len(policy.Rules) != 0 || len(policy.SelectableActions) != 0 {
			return errors.New("EXPERIMENT_ADMISSIBLE_UNIFORM_POLICY_INVALID")
		}
		return validatePriority(policy.Priority)
	}
	if policy.Version == BoundedUniformPolicyVersion {
		return policy.validateBoundedStochastic("EXPERIMENT_BOUNDED_UNIFORM_POLICY_INVALID")
	}
	if policy.Version == ActionClassPolicyVersion {
		seed, err := hex.DecodeString(policy.SeedHex)
		if err != nil || len(seed) == 0 || len(policy.Rules) != 0 || len(policy.SelectableActions) != 0 {
			return errors.New("EXPERIMENT_ACTION_CLASS_POLICY_INVALID")
		}
		return validatePriority(policy.Priority)
	}
	if policy.Version == BoundedActionClassPolicyVersion {
		return policy.validateBoundedStochastic("EXPERIMENT_BOUNDED_ACTION_CLASS_POLICY_INVALID")
	}
	if policy.Version != PolicyVersion || policy.SeedHex != "" || len(policy.SelectableActions) != 0 {
		return errors.New("EXPERIMENT_POLICY_IDENTITY_INVALID")
	}
	if len(policy.Priority) == 0 {
		return errors.New("EXPERIMENT_POLICY_PRIORITY_REQUIRED")
	}
	if err := validatePriority(policy.Priority); err != nil {
		return err
	}
	seenDecisions := make(map[int]bool, len(policy.Rules))
	for _, rule := range policy.Rules {
		if rule.Decision <= 0 || rule.Decision > decisionBudget || seenDecisions[rule.Decision] {
			return fmt.Errorf("EXPERIMENT_POLICY_RULE_DECISION_INVALID: %d", rule.Decision)
		}
		if err := rule.Kind.Validate(); err != nil {
			return err
		}
		if rule.Kind == control.ActionPartition {
			parameters, err := control.DecodePartitionParameters(rule.Parameters)
			if err != nil || rule.ActionID == "" || parameters.ID == "" {
				return errors.New("EXPERIMENT_POLICY_PARTITION_PREPARATION_INVALID")
			}
		} else if len(rule.Parameters) != 0 {
			return errors.New("EXPERIMENT_POLICY_PREPARATION_PARAMETERS_UNEXPECTED")
		}
		seenDecisions[rule.Decision] = true
	}
	return nil
}

func (policy Policy) validateBoundedStochastic(reason string) error {
	seed, err := hex.DecodeString(policy.SeedHex)
	if err != nil || len(seed) == 0 || len(policy.Rules) != 0 ||
		!canonicalActionKinds(policy.SelectableActions, true) {
		return errors.New(reason)
	}
	if err := validatePriority(policy.Priority); err != nil {
		return err
	}
	allowed := actionKindSet(policy.SelectableActions)
	for _, kind := range policy.Priority {
		if !allowed[kind] {
			return errors.New("EXPERIMENT_BOUNDED_POLICY_PRIORITY_OUTSIDE_SURFACE")
		}
	}
	return nil
}

func validatePriority(priority []control.ActionKind) error {
	seenKinds := make(map[control.ActionKind]bool, len(priority))
	for _, kind := range priority {
		if err := kind.Validate(); err != nil {
			return err
		}
		if seenKinds[kind] {
			return fmt.Errorf("EXPERIMENT_POLICY_PRIORITY_DUPLICATE: %s", kind)
		}
		seenKinds[kind] = true
	}
	return nil
}

func (policy Policy) Digest() (string, error) {
	return control.CanonicalDigest(policy)
}

func (policy Policy) selectAction(decision int, enabled []control.Action) (control.Action, error) {
	enabled = policy.constrainSelectableActions(enabled)
	if len(enabled) == 0 {
		return control.Action{}, &policySelectionError{
			code: "EXPERIMENT_POLICY_NO_ACTION", detail: fmt.Sprintf("decision=%d", decision),
		}
	}
	if policy.Version == RandomPolicyVersion {
		index, err := policy.randomIndex(decision, enabled)
		if err != nil {
			return control.Action{}, err
		}
		return enabled[index], nil
	}
	if policy.Version == AdmissibleUniformPolicyVersion || policy.Version == BoundedUniformPolicyVersion {
		for _, kind := range policy.Priority {
			for _, action := range enabled {
				if action.Kind == kind {
					return action, nil
				}
			}
		}
		return policy.admissibleUniform(decision, enabled)
	}
	if policy.Version == ActionClassPolicyVersion || policy.Version == BoundedActionClassPolicyVersion {
		for _, kind := range policy.Priority {
			for _, action := range enabled {
				if action.Kind == kind {
					return action, nil
				}
			}
		}
		return policy.actionClassRandom(decision, enabled)
	}
	for _, rule := range policy.Rules {
		if rule.Decision != decision {
			continue
		}
		for _, action := range enabled {
			if (rule.ActionID == "" || action.ID == rule.ActionID) &&
				action.Kind == rule.Kind && (rule.Node == "" || action.Node.Node == rule.Node) {
				return action, nil
			}
		}
		return control.Action{}, &policySelectionError{
			code:   "EXPERIMENT_POLICY_RULE_NOT_ENABLED",
			detail: fmt.Sprintf("decision=%d kind=%s node=%s action=%s", decision, rule.Kind, rule.Node, rule.ActionID),
		}
	}
	for _, kind := range policy.Priority {
		for _, action := range enabled {
			if action.Kind == kind {
				return action, nil
			}
		}
	}
	return control.Action{}, &policySelectionError{
		code: "EXPERIMENT_POLICY_NO_ACTION", detail: fmt.Sprintf("decision=%d", decision),
	}
}

func (policy Policy) bounded() bool {
	return policy.Version == BoundedUniformPolicyVersion || policy.Version == BoundedActionClassPolicyVersion
}

func (policy Policy) constrainSelectableActions(enabled []control.Action) []control.Action {
	if !policy.bounded() {
		return append([]control.Action(nil), enabled...)
	}
	allowed := actionKindSet(policy.SelectableActions)
	result := make([]control.Action, 0, len(enabled))
	for _, action := range enabled {
		if allowed[action.Kind] {
			result = append(result, action)
		}
	}
	return result
}

func (policy Policy) validateTraceSurface(trace controlruntime.Trace) error {
	if !policy.bounded() {
		return nil
	}
	allowed := actionKindSet(policy.SelectableActions)
	for index, record := range trace.Records {
		if !allowed[record.Action.Kind] {
			return fmt.Errorf(
				"EXPERIMENT_BOUNDED_POLICY_TRACE_ACTION_OUTSIDE_SURFACE: decision=%d kind=%s",
				index+1, record.Action.Kind,
			)
		}
	}
	return nil
}

// admissibleUniform samples each Action in the common admissible frontier
// with equal probability. Canonical ActionID order makes the result independent
// of Adapter/Runtime slice order; Priority is reserved for frozen preparation
// actions such as Invoke and is applied before random search.
func (policy Policy) admissibleUniform(decision int, enabled []control.Action) (control.Action, error) {
	seed, err := hex.DecodeString(policy.SeedHex)
	if err != nil || len(seed) == 0 {
		return control.Action{}, errors.New("EXPERIMENT_ADMISSIBLE_UNIFORM_POLICY_SEED_INVALID")
	}
	canonical := append([]control.Action(nil), enabled...)
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].ID < canonical[j].ID })
	digest, err := control.CanonicalDigest(canonical)
	if err != nil {
		return control.Action{}, err
	}
	index := domainSeparatedRandomIndex(
		seed, decision, "consensus-atlas/admissible-uniform-random/v1\x00", digest, len(canonical),
	)
	return canonical[index], nil
}

func domainSeparatedRandomIndex(seed []byte, decision int, domain string, context string, size int) int {
	count := uint64(size)
	threshold := -count % count
	for counter := uint64(0); ; counter++ {
		var integers [16]byte
		binary.BigEndian.PutUint64(integers[:8], uint64(decision))
		binary.BigEndian.PutUint64(integers[8:], counter)
		hash := sha256.New()
		_, _ = hash.Write(seed)
		_, _ = hash.Write([]byte(domain))
		_, _ = hash.Write(integers[:])
		_, _ = hash.Write([]byte(context))
		value := binary.BigEndian.Uint64(hash.Sum(nil)[:8])
		if value >= threshold {
			return int(value % count)
		}
	}
}

// actionClassRandom first samples uniformly from the distinct enabled Action
// kinds, then uniformly within that kind. This prevents a frontier containing
// many messages/effects of one kind from silently weighting the policy toward
// that class. It only ranks Runtime-provided enabled Actions.
func (policy Policy) actionClassRandom(decision int, enabled []control.Action) (control.Action, error) {
	seed, err := hex.DecodeString(policy.SeedHex)
	if err != nil || len(seed) == 0 {
		return control.Action{}, errors.New("EXPERIMENT_ACTION_CLASS_POLICY_SEED_INVALID")
	}
	byKind := make(map[control.ActionKind][]control.Action)
	for _, action := range enabled {
		byKind[action.Kind] = append(byKind[action.Kind], action)
	}
	kinds := make([]control.ActionKind, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	canonicalEnabled := append([]control.Action(nil), enabled...)
	sort.Slice(canonicalEnabled, func(i, j int) bool { return canonicalEnabled[i].ID < canonicalEnabled[j].ID })
	enabledDigest, err := control.CanonicalDigest(canonicalEnabled)
	if err != nil {
		return control.Action{}, err
	}
	classIndex := actionClassIndex(seed, decision, "class", enabledDigest, len(kinds))
	selectedKind := kinds[classIndex]
	members := append([]control.Action(nil), byKind[selectedKind]...)
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	memberDigest, err := control.CanonicalDigest(members)
	if err != nil {
		return control.Action{}, err
	}
	memberIndex := actionClassIndex(seed, decision, "member/"+string(selectedKind), memberDigest, len(members))
	return members[memberIndex], nil
}

func actionClassIndex(seed []byte, decision int, domain string, context string, size int) int {
	count := uint64(size)
	threshold := -count % count
	for counter := uint64(0); ; counter++ {
		var integers [16]byte
		binary.BigEndian.PutUint64(integers[:8], uint64(decision))
		binary.BigEndian.PutUint64(integers[8:], counter)
		hash := sha256.New()
		_, _ = hash.Write(seed)
		_, _ = hash.Write([]byte("consensus-atlas/action-class-random/v1\x00"))
		_, _ = hash.Write([]byte(domain))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(integers[:])
		_, _ = hash.Write([]byte(context))
		value := binary.BigEndian.Uint64(hash.Sum(nil)[:8])
		if value >= threshold {
			return int(value % count)
		}
	}
}

func (policy Policy) randomIndex(decision int, enabled []control.Action) (int, error) {
	seed, err := hex.DecodeString(policy.SeedHex)
	if err != nil || len(seed) == 0 {
		return 0, errors.New("EXPERIMENT_RANDOM_POLICY_SEED_INVALID")
	}
	enabledDigest, err := control.CanonicalDigest(enabled)
	if err != nil {
		return 0, err
	}
	count := uint64(len(enabled))
	threshold := -count % count
	for counter := uint64(0); ; counter++ {
		var integers [16]byte
		binary.BigEndian.PutUint64(integers[:8], uint64(decision))
		binary.BigEndian.PutUint64(integers[8:], counter)
		hash := sha256.New()
		_, _ = hash.Write(seed)
		_, _ = hash.Write(integers[:])
		_, _ = hash.Write([]byte(enabledDigest))
		value := binary.BigEndian.Uint64(hash.Sum(nil)[:8])
		if value >= threshold {
			return int(value % count), nil
		}
	}
}

type RunPlan struct {
	Run               int           `json:"run"`
	Policy            Policy        `json:"policy"`
	Workload          *WorkloadPlan `json:"workload,omitempty"`
	StopAfterWorkload bool          `json:"stop_after_workload,omitempty"`
}

type Config struct {
	SchemaVersion    string              `json:"schema_version"`
	ID               string              `json:"id"`
	PSSID            string              `json:"pss_id"`
	Runtime          RuntimeConfig       `json:"runtime"`
	Admission        *ExecutionAdmission `json:"admission,omitempty"`
	FaultEnvelope    *FaultEnvelope      `json:"fault_envelope,omitempty"`
	WorkloadRouterID string              `json:"workload_router_id,omitempty"`
	DecisionsPerRun  int                 `json:"decisions_per_run"`
	RequireReplay    bool                `json:"require_replay"`
	Runs             []RunPlan           `json:"runs"`
}

func (config Config) Validate() error {
	if (config.SchemaVersion != SchemaVersion && config.SchemaVersion != SchemaVersionV2) ||
		config.ID == "" || config.PSSID == "" {
		return errors.New("EXPERIMENT_CONFIG_IDENTITY_INVALID")
	}
	if config.DecisionsPerRun <= 0 || len(config.Runs) == 0 {
		return errors.New("EXPERIMENT_BUDGET_INVALID")
	}
	if !config.RequireReplay {
		return errors.New("EXPERIMENT_REPLAY_REQUIRED")
	}
	if _, err := config.Runtime.runtimeConfig(); err != nil {
		return err
	}
	if config.Admission != nil {
		if err := config.Admission.Validate(); err != nil {
			return err
		}
	}
	if config.Admission == nil && config.requiresQualification() {
		return errors.New("EXPERIMENT_ADMISSION_REQUIRED")
	}
	if config.FaultEnvelope != nil {
		if err := config.FaultEnvelope.Validate(); err != nil {
			return err
		}
	}
	seenRuns := make(map[int]bool, len(config.Runs))
	hasWorkload := false
	for _, run := range config.Runs {
		if run.Run <= 0 || seenRuns[run.Run] {
			return fmt.Errorf("EXPERIMENT_RUN_INVALID: %d", run.Run)
		}
		seenRuns[run.Run] = true
		if err := run.Policy.Validate(config.DecisionsPerRun); err != nil {
			return fmt.Errorf("run %d: %w", run.Run, err)
		}
		if run.Policy.bounded() && config.SchemaVersion != SchemaVersionV2 {
			return fmt.Errorf("run %d: EXPERIMENT_BOUNDED_POLICY_REQUIRES_V2", run.Run)
		}
		if run.Workload != nil {
			hasWorkload = true
			if err := run.Workload.Validate(); err != nil {
				return fmt.Errorf("run %d: %w", run.Run, err)
			}
			if run.Policy.bounded() && !actionKindSet(run.Policy.SelectableActions)[control.ActionInvoke] {
				return fmt.Errorf("run %d: EXPERIMENT_BOUNDED_POLICY_INVOKE_REQUIRED", run.Run)
			}
		}
		if run.StopAfterWorkload && (config.SchemaVersion != SchemaVersionV2 || run.Workload == nil) {
			return fmt.Errorf("EXPERIMENT_RUN_STOP_INVALID: %d", run.Run)
		}
	}
	if config.SchemaVersion == SchemaVersion {
		if config.WorkloadRouterID != "" {
			return errors.New("EXPERIMENT_WORKLOAD_ROUTER_UNEXPECTED")
		}
	} else if hasWorkload && config.WorkloadRouterID == "" {
		return errors.New("EXPERIMENT_WORKLOAD_ROUTER_REQUIRED")
	}
	return nil
}

func (config Config) requiresQualification() bool {
	if config.FaultEnvelope != nil {
		return true
	}
	for _, run := range config.Runs {
		if run.Workload != nil {
			return true
		}
	}
	return false
}

func (config Config) requiresWorkloadRouter() bool {
	for _, run := range config.Runs {
		if run.Workload != nil {
			return true
		}
	}
	return false
}

func (config Config) Digest() (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}
	return control.CanonicalDigest(config)
}

type Budget struct {
	Runs                         int `json:"runs"`
	DecisionsPerRun              int `json:"decisions_per_run"`
	PrimarySetupAttempts         int `json:"primary_setup_attempts"`
	TargetPrimaryDecisions       int `json:"target_primary_decisions"`
	RequiredReplayAttempts       int `json:"required_replay_attempts"`
	RequiredReplayDecisions      int `json:"required_replay_decisions"`
	TargetPrimaryPrepareActions  int `json:"target_primary_prepare_actions,omitempty"`
	RequiredReplayPrepareActions int `json:"required_replay_prepare_actions,omitempty"`
}

type PhaseWork struct {
	SetupAttempts          int `json:"setup_attempts"`
	RuntimeInitializations int `json:"runtime_initializations"`
	PrepareActions         int `json:"prepare_actions"`
	SchedulerDecisions     int `json:"scheduler_decisions"`
	WorkUnits              int `json:"work_units"`
}

type ModelWork struct {
	Calls        int `json:"calls"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// AgenticLogicalBudget is editable investigation input. The active episode
// converts it to its own bounded execution budget; it is not a durable
// Campaign coordinator contract.
type AgenticLogicalBudget struct {
	MaxAttempts                  int `json:"max_attempts"`
	MaxPrimarySchedulerDecisions int `json:"max_primary_scheduler_decisions"`
	MaxPrimaryWorkUnits          int `json:"max_primary_work_units"`
	MaxReplayWorkUnits           int `json:"max_replay_work_units"`
	MaxModelCalls                int `json:"max_model_calls"`
	MaxModelTokens               int `json:"max_model_tokens"`
}

func (budget AgenticLogicalBudget) Validate() error {
	if budget.MaxAttempts <= 0 || budget.MaxPrimarySchedulerDecisions <= 0 ||
		budget.MaxPrimaryWorkUnits <= 0 || budget.MaxReplayWorkUnits <= 0 ||
		budget.MaxModelCalls <= 0 || budget.MaxModelTokens <= 0 {
		return errors.New("EXPERIMENT_AGENTIC_LOGICAL_BUDGET_INVALID")
	}
	return nil
}

type ResourceAccounting struct {
	WallTime string `json:"wall_time"`
	CPUTime  string `json:"cpu_time"`
	PeakRSS  string `json:"peak_rss"`
}

type WorkLedger struct {
	Primary   PhaseWork          `json:"primary"`
	Replay    PhaseWork          `json:"replay"`
	Model     ModelWork          `json:"model"`
	Resources ResourceAccounting `json:"resources"`
}

type ReplayResult struct {
	Required    bool   `json:"required"`
	Stable      bool   `json:"stable"`
	Decisions   int    `json:"decisions"`
	TraceDigest string `json:"trace_digest"`
}

// SelectionAudit binds the Runtime-enabled frontier to the common admissible
// frontier actually shown to the policy. It is an Experiment record and does
// not alter Runtime Trace identity.
type SelectionAudit struct {
	Decision             int              `json:"decision"`
	RuntimeEnabledDigest string           `json:"runtime_enabled_digest"`
	AdmissibleDigest     string           `json:"admissible_digest"`
	SelectedAction       control.ActionID `json:"selected_action"`
}

func (audit SelectionAudit) validate(decision int) error {
	if audit.Decision != decision || !validSHA256(audit.RuntimeEnabledDigest) ||
		!validSHA256(audit.AdmissibleDigest) || audit.SelectedAction == "" {
		return fmt.Errorf("EXPERIMENT_SELECTION_AUDIT_INVALID: %d", decision)
	}
	return nil
}

type RunReport struct {
	Run                  int                `json:"run"`
	PolicyID             string             `json:"policy_id"`
	PolicyDigest         string             `json:"policy_digest"`
	TargetDecisions      int                `json:"target_decisions"`
	ChargedDecisions     int                `json:"charged_decisions"`
	BudgetReached        bool               `json:"budget_reached"`
	Termination          string             `json:"termination,omitempty"`
	Selections           []SelectionAudit   `json:"selections,omitempty"`
	ManifestDigest       string             `json:"manifest_digest"`
	TraceSchemaVersion   string             `json:"trace_schema_version"`
	TraceDigest          string             `json:"trace_digest"`
	SeedDigest           string             `json:"seed_digest"`
	InitialStateDigest   string             `json:"initial_state_digest"`
	FinalStateDigest     string             `json:"final_state_digest"`
	CorePSSSamples       int                `json:"core_pss_samples"`
	CorePSSSamplesDigest string             `json:"core_pss_samples_digest"`
	UniqueCoreStates     int                `json:"unique_core_states"`
	Workload             *WorkloadRunReport `json:"workload,omitempty"`
	Faults               *FaultUsage        `json:"faults,omitempty"`
	Replay               ReplayResult       `json:"replay"`
}

type Report struct {
	SchemaVersion  string                          `json:"schema_version"`
	Status         string                          `json:"status"`
	ExperimentID   string                          `json:"experiment_id"`
	PSSID          string                          `json:"pss_id"`
	Config         Config                          `json:"config"`
	ConfigDigest   string                          `json:"config_digest"`
	ManifestDigest string                          `json:"manifest_digest"`
	Budget         Budget                          `json:"budget"`
	Work           WorkLedger                      `json:"work"`
	Runs           []RunReport                     `json:"runs"`
	StateDiscovery protocolstate.ExperimentSummary `json:"state_discovery"`
	Digest         string                          `json:"digest"`
}

func expectedBudget(config Config) Budget {
	total := len(config.Runs) * config.DecisionsPerRun
	prepare := expectedPrepareActions(config)
	return Budget{
		Runs: len(config.Runs), DecisionsPerRun: config.DecisionsPerRun,
		PrimarySetupAttempts: len(config.Runs), TargetPrimaryDecisions: total,
		RequiredReplayAttempts: len(config.Runs), RequiredReplayDecisions: total,
		TargetPrimaryPrepareActions: prepare, RequiredReplayPrepareActions: prepare,
	}
}

func expectedWork(config Config) WorkLedger {
	total := len(config.Runs) * config.DecisionsPerRun
	prepare := expectedPrepareActions(config)
	phase := PhaseWork{
		SetupAttempts: len(config.Runs), RuntimeInitializations: len(config.Runs),
		PrepareActions: prepare, SchedulerDecisions: total,
		WorkUnits: len(config.Runs) + prepare + total,
	}
	return WorkLedger{
		Primary: phase, Replay: phase,
		Resources: ResourceAccounting{
			WallTime: ResourceNotCollected, CPUTime: ResourceNotCollected, PeakRSS: ResourceNotCollected,
		},
	}
}

func measuredWork(report Report) WorkLedger {
	work := emptyWork()
	for _, run := range report.Runs {
		chargeSetup(&work.Primary)
		chargeRuntimeInitialization(&work.Primary)
		chargeSetup(&work.Replay)
		chargeRuntimeInitialization(&work.Replay)
		chargeDecisions(&work.Primary, run.ChargedDecisions)
		chargeDecisions(&work.Replay, run.Replay.Decisions)
		if run.Workload != nil {
			chargePrepareActions(&work.Primary, run.Workload.Offered)
			chargePrepareActions(&work.Replay, run.Workload.Offered)
		}
		if run.Faults != nil {
			chargePrepareActions(&work.Primary, run.Faults.Partitions)
			chargePrepareActions(&work.Replay, run.Faults.Partitions)
		}
	}
	return work
}

func expectedPrepareActions(config Config) int {
	total := 0
	for _, run := range config.Runs {
		if run.Workload != nil {
			total += len(run.Workload.Invocations)
		}
		for _, rule := range run.Policy.Rules {
			if rule.Kind == control.ActionPartition {
				total++
			}
		}
	}
	return total
}

func emptyWork() WorkLedger {
	return WorkLedger{Resources: ResourceAccounting{
		WallTime: ResourceNotCollected, CPUTime: ResourceNotCollected, PeakRSS: ResourceNotCollected,
	}}
}

func chargeSetup(phase *PhaseWork) {
	phase.SetupAttempts++
	updateWorkUnits(phase)
}

func chargeRuntimeInitialization(phase *PhaseWork) {
	phase.RuntimeInitializations++
	updateWorkUnits(phase)
}

func chargePrepareActions(phase *PhaseWork, actions int) {
	phase.PrepareActions += actions
	updateWorkUnits(phase)
}

func chargeDecisions(phase *PhaseWork, decisions int) {
	phase.SchedulerDecisions += decisions
	updateWorkUnits(phase)
}

func updateWorkUnits(phase *PhaseWork) {
	phase.WorkUnits = phase.SetupAttempts + phase.PrepareActions + phase.SchedulerDecisions
}

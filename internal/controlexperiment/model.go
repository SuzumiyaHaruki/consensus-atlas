// Package controlexperiment executes bounded, replay-checked measurements on
// Control Runtime v2. It records state discovery but does not decide protocol
// correctness, Coverage, or Oracle results.
package controlexperiment

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

const (
	SchemaVersion             = "consensus-atlas/control-experiment/v1"
	PolicyVersion             = "consensus-atlas/action-priority-policy/v1"
	RandomPolicyVersion       = "consensus-atlas/uniform-random-policy/v1"
	StatusMeasurementComplete = "measurement-complete"
	ResourceNotCollected      = "not-collected"
)

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
}

// Policy is intentionally limited to common Action fields. Rules override the
// fallback priority at exact one-based decision numbers.
type Policy struct {
	Version  string               `json:"version"`
	ID       string               `json:"id"`
	SeedHex  string               `json:"seed_hex,omitempty"`
	Rules    []DecisionRule       `json:"rules,omitempty"`
	Priority []control.ActionKind `json:"priority,omitempty"`
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
		if err != nil || len(seed) == 0 || len(policy.Rules) != 0 || len(policy.Priority) != 0 {
			return errors.New("EXPERIMENT_RANDOM_POLICY_INVALID")
		}
		return nil
	}
	if policy.Version != PolicyVersion || policy.SeedHex != "" {
		return errors.New("EXPERIMENT_POLICY_IDENTITY_INVALID")
	}
	if len(policy.Priority) == 0 {
		return errors.New("EXPERIMENT_POLICY_PRIORITY_REQUIRED")
	}
	seenKinds := make(map[control.ActionKind]bool, len(policy.Priority))
	for _, kind := range policy.Priority {
		if err := kind.Validate(); err != nil {
			return err
		}
		if seenKinds[kind] {
			return fmt.Errorf("EXPERIMENT_POLICY_PRIORITY_DUPLICATE: %s", kind)
		}
		seenKinds[kind] = true
	}
	seenDecisions := make(map[int]bool, len(policy.Rules))
	for _, rule := range policy.Rules {
		if rule.Decision <= 0 || rule.Decision > decisionBudget || seenDecisions[rule.Decision] {
			return fmt.Errorf("EXPERIMENT_POLICY_RULE_DECISION_INVALID: %d", rule.Decision)
		}
		if err := rule.Kind.Validate(); err != nil {
			return err
		}
		seenDecisions[rule.Decision] = true
	}
	return nil
}

func (policy Policy) Digest() (string, error) {
	return control.CanonicalDigest(policy)
}

func (policy Policy) selectAction(decision int, enabled []control.Action) (control.Action, error) {
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
	for _, rule := range policy.Rules {
		if rule.Decision != decision {
			continue
		}
		for _, action := range enabled {
			if action.Kind == rule.Kind && (rule.Node == "" || action.Node.Node == rule.Node) {
				return action, nil
			}
		}
		return control.Action{}, &policySelectionError{
			code:   "EXPERIMENT_POLICY_RULE_NOT_ENABLED",
			detail: fmt.Sprintf("decision=%d kind=%s node=%s", decision, rule.Kind, rule.Node),
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
	Run    int    `json:"run"`
	Policy Policy `json:"policy"`
}

type Config struct {
	SchemaVersion   string        `json:"schema_version"`
	ID              string        `json:"id"`
	PSSID           string        `json:"pss_id"`
	Runtime         RuntimeConfig `json:"runtime"`
	DecisionsPerRun int           `json:"decisions_per_run"`
	RequireReplay   bool          `json:"require_replay"`
	Runs            []RunPlan     `json:"runs"`
}

func (config Config) Validate() error {
	if config.SchemaVersion != SchemaVersion || config.ID == "" || config.PSSID == "" {
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
	seenRuns := make(map[int]bool, len(config.Runs))
	for _, run := range config.Runs {
		if run.Run <= 0 || seenRuns[run.Run] {
			return fmt.Errorf("EXPERIMENT_RUN_INVALID: %d", run.Run)
		}
		seenRuns[run.Run] = true
		if err := run.Policy.Validate(config.DecisionsPerRun); err != nil {
			return fmt.Errorf("run %d: %w", run.Run, err)
		}
	}
	return nil
}

func (config Config) Digest() (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}
	return control.CanonicalDigest(config)
}

type Budget struct {
	Runs                    int `json:"runs"`
	DecisionsPerRun         int `json:"decisions_per_run"`
	PrimarySetupAttempts    int `json:"primary_setup_attempts"`
	TargetPrimaryDecisions  int `json:"target_primary_decisions"`
	RequiredReplayAttempts  int `json:"required_replay_attempts"`
	RequiredReplayDecisions int `json:"required_replay_decisions"`
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

type RunReport struct {
	Run                  int          `json:"run"`
	PolicyID             string       `json:"policy_id"`
	PolicyDigest         string       `json:"policy_digest"`
	TargetDecisions      int          `json:"target_decisions"`
	ChargedDecisions     int          `json:"charged_decisions"`
	BudgetReached        bool         `json:"budget_reached"`
	ManifestDigest       string       `json:"manifest_digest"`
	TraceSchemaVersion   string       `json:"trace_schema_version"`
	TraceDigest          string       `json:"trace_digest"`
	SeedDigest           string       `json:"seed_digest"`
	InitialStateDigest   string       `json:"initial_state_digest"`
	FinalStateDigest     string       `json:"final_state_digest"`
	CorePSSSamples       int          `json:"core_pss_samples"`
	CorePSSSamplesDigest string       `json:"core_pss_samples_digest"`
	UniqueCoreStates     int          `json:"unique_core_states"`
	Replay               ReplayResult `json:"replay"`
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
	return Budget{
		Runs: len(config.Runs), DecisionsPerRun: config.DecisionsPerRun,
		PrimarySetupAttempts: len(config.Runs), TargetPrimaryDecisions: total,
		RequiredReplayAttempts: len(config.Runs), RequiredReplayDecisions: total,
	}
}

func expectedWork(config Config) WorkLedger {
	total := len(config.Runs) * config.DecisionsPerRun
	phase := PhaseWork{
		SetupAttempts: len(config.Runs), RuntimeInitializations: len(config.Runs),
		SchedulerDecisions: total, WorkUnits: len(config.Runs) + total,
	}
	return WorkLedger{
		Primary: phase, Replay: phase,
		Resources: ResourceAccounting{
			WallTime: ResourceNotCollected, CPUTime: ResourceNotCollected, PeakRSS: ResourceNotCollected,
		},
	}
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

func chargeDecisions(phase *PhaseWork, decisions int) {
	phase.SchedulerDecisions += decisions
	updateWorkUnits(phase)
}

func updateWorkUnits(phase *PhaseWork) {
	phase.WorkUnits = phase.SetupAttempts + phase.PrepareActions + phase.SchedulerDecisions
}

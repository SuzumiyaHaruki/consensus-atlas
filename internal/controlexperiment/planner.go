package controlexperiment

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const (
	PlannerProposalVersion = "consensus-atlas/planner-proposal/v1"
	PlannerAttemptVersion  = "consensus-atlas/planner-attempt/v1"

	PlannerStatusComplete        = "measurement-complete"
	PlannerStatusRejected        = "proposal-rejected"
	PlannerStatusExecutionFailed = "execution-failed"

	ProposalSchemaInvalid = "PLANNER_PROPOSAL_SCHEMA_INVALID"
	ProposalRunSetInvalid = "PLANNER_PROPOSAL_RUN_SET_INVALID"
	ProposalPolicyInvalid = "PLANNER_PROPOSAL_POLICY_INVALID"
)

// PlannerScope is trusted input. The Planner cannot propose an experiment
// identity, Runtime entropy, PSS, run set, budget, or replay policy.
type PlannerScope struct {
	ExperimentID    string        `json:"experiment_id"`
	PSSID           string        `json:"pss_id"`
	Runtime         RuntimeConfig `json:"runtime"`
	DecisionsPerRun int           `json:"decisions_per_run"`
	RequireReplay   bool          `json:"require_replay"`
	Runs            []int         `json:"runs"`
}

func (scope PlannerScope) Validate() error {
	if scope.ExperimentID == "" || scope.PSSID == "" || scope.DecisionsPerRun <= 0 ||
		!scope.RequireReplay || len(scope.Runs) == 0 {
		return errors.New("PLANNER_SCOPE_INVALID")
	}
	if _, err := scope.Runtime.runtimeConfig(); err != nil {
		return err
	}
	seen := make(map[int]bool, len(scope.Runs))
	for _, run := range scope.Runs {
		if run <= 0 || seen[run] {
			return errors.New("PLANNER_SCOPE_RUNS_INVALID")
		}
		seen[run] = true
	}
	return nil
}

func (scope PlannerScope) Digest() (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	return portableJSONDigest(scope)
}

// ProposedPolicy contains the entire untrusted strategy surface. A public seed
// selects the random policy; otherwise priority/rules select the fixed policy.
type ProposedPolicy struct {
	Run      int                  `json:"run"`
	SeedHex  string               `json:"seed_hex,omitempty"`
	Rules    []DecisionRule       `json:"rules,omitempty"`
	Priority []control.ActionKind `json:"priority,omitempty"`
}

type PlannerProposal struct {
	SchemaVersion string           `json:"schema_version"`
	Policies      []ProposedPolicy `json:"policies"`
}

// DecodePlannerProposal is the only model-output decoder. Unknown fields and
// trailing JSON are rejected instead of being silently discarded.
func DecodePlannerProposal(encoded []byte) (PlannerProposal, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var proposal PlannerProposal
	if err := decoder.Decode(&proposal); err != nil {
		return PlannerProposal{}, fmt.Errorf("PLANNER_PROPOSAL_DECODE_FAILED: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return PlannerProposal{}, errors.New("PLANNER_PROPOSAL_TRAILING_JSON")
	}
	return proposal, nil
}

type ProposalCompilation struct {
	Accepted       bool   `json:"accepted"`
	ReasonCode     string `json:"reason_code,omitempty"`
	FailedRun      int    `json:"failed_run,omitempty"`
	ProposalDigest string `json:"proposal_digest"`
	ConfigDigest   string `json:"config_digest,omitempty"`
}

func CompileProposal(scope PlannerScope, proposal PlannerProposal) (Config, ProposalCompilation, error) {
	if err := scope.Validate(); err != nil {
		return Config{}, ProposalCompilation{}, err
	}
	proposalDigest, err := portableJSONDigest(proposal)
	if err != nil {
		return Config{}, ProposalCompilation{}, err
	}
	rejected := func(code string, run int) (Config, ProposalCompilation, error) {
		return Config{}, ProposalCompilation{
			Accepted: false, ReasonCode: code, FailedRun: run, ProposalDigest: proposalDigest,
		}, nil
	}
	if proposal.SchemaVersion != PlannerProposalVersion {
		return rejected(ProposalSchemaInvalid, 0)
	}
	if len(proposal.Policies) != len(scope.Runs) {
		return rejected(ProposalRunSetInvalid, 0)
	}
	proposed := make(map[int]ProposedPolicy, len(proposal.Policies))
	allowed := make(map[int]bool, len(scope.Runs))
	for _, run := range scope.Runs {
		allowed[run] = true
	}
	for _, policy := range proposal.Policies {
		if !allowed[policy.Run] || policy.Run <= 0 {
			return rejected(ProposalRunSetInvalid, policy.Run)
		}
		if _, exists := proposed[policy.Run]; exists {
			return rejected(ProposalRunSetInvalid, policy.Run)
		}
		proposed[policy.Run] = policy
	}
	config := Config{
		SchemaVersion: SchemaVersion, ID: scope.ExperimentID, PSSID: scope.PSSID,
		Runtime: scope.Runtime, DecisionsPerRun: scope.DecisionsPerRun,
		RequireReplay: scope.RequireReplay,
	}
	for _, run := range scope.Runs {
		candidate, exists := proposed[run]
		if !exists {
			return rejected(ProposalRunSetInvalid, run)
		}
		policy := Policy{
			Version: PolicyVersion, ID: fmt.Sprintf("compiled-priority/run-%d", run),
			Rules: candidate.Rules, Priority: candidate.Priority,
		}
		if candidate.SeedHex != "" {
			policy = Policy{
				Version: RandomPolicyVersion, ID: fmt.Sprintf("compiled-random/run-%d", run),
				SeedHex: candidate.SeedHex, Rules: candidate.Rules, Priority: candidate.Priority,
			}
		}
		if err := policy.Validate(scope.DecisionsPerRun); err != nil {
			return rejected(ProposalPolicyInvalid, run)
		}
		config.Runs = append(config.Runs, RunPlan{Run: run, Policy: policy})
	}
	configDigest, err := config.Digest()
	if err != nil {
		return Config{}, ProposalCompilation{}, err
	}
	return config, ProposalCompilation{
		Accepted: true, ProposalDigest: proposalDigest, ConfigDigest: configDigest,
	}, nil
}

type PlannerWork struct {
	ProposalAttempts int        `json:"proposal_attempts"`
	Model            ModelWork  `json:"model"`
	Execution        WorkLedger `json:"execution"`
}

type PlannerModelAudit struct {
	Provider              string  `json:"provider"`
	RequestedModel        string  `json:"requested_model"`
	ResponseModel         string  `json:"response_model"`
	Endpoint              string  `json:"endpoint"`
	PromptDigest          string  `json:"prompt_digest"`
	RequestDigest         string  `json:"request_digest"`
	ResponseDigest        string  `json:"response_digest"`
	ResponseID            string  `json:"response_id,omitempty"`
	SystemFingerprint     string  `json:"system_fingerprint,omitempty"`
	FinishReason          string  `json:"finish_reason"`
	ThinkingMode          string  `json:"thinking_mode"`
	Temperature           float64 `json:"temperature"`
	MaxTokens             int     `json:"max_tokens"`
	PromptTokens          int     `json:"prompt_tokens"`
	PromptCacheHitTokens  int     `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int     `json:"prompt_cache_miss_tokens"`
	CompletionTokens      int     `json:"completion_tokens"`
	ReasoningTokens       int     `json:"reasoning_tokens"`
	TotalTokens           int     `json:"total_tokens"`
	DurationMillis        int64   `json:"duration_millis"`
}

func (audit PlannerModelAudit) modelWork() ModelWork {
	return ModelWork{
		Calls: 1, InputTokens: audit.PromptTokens,
		OutputTokens: audit.CompletionTokens, TotalTokens: audit.TotalTokens,
	}
}

func (audit PlannerModelAudit) Validate() error {
	if audit.Provider == "" || audit.RequestedModel == "" || audit.ResponseModel == "" ||
		audit.Endpoint == "" || audit.FinishReason != "stop" || audit.ThinkingMode != "disabled" ||
		audit.Temperature != 0 || audit.MaxTokens <= 0 || audit.DurationMillis < 0 ||
		!validSHA256(audit.PromptDigest) || !validSHA256(audit.RequestDigest) ||
		!validSHA256(audit.ResponseDigest) {
		return errors.New("PLANNER_MODEL_AUDIT_INVALID")
	}
	work := audit.modelWork()
	if validateModelWork(work) != nil || audit.PromptCacheHitTokens < 0 ||
		audit.PromptCacheMissTokens < 0 || audit.ReasoningTokens < 0 ||
		audit.PromptCacheHitTokens+audit.PromptCacheMissTokens > audit.PromptTokens ||
		audit.ReasoningTokens > audit.CompletionTokens {
		return errors.New("PLANNER_MODEL_USAGE_INVALID")
	}
	return nil
}

type PlannerFailure struct {
	Phase      string `json:"phase"`
	ReasonCode string `json:"reason_code"`
	Run        int    `json:"run,omitempty"`
	Decision   int    `json:"decision,omitempty"`
}

type PlannerAttempt struct {
	SchemaVersion string              `json:"schema_version"`
	Status        string              `json:"status"`
	PlannerID     string              `json:"planner_id"`
	Scope         PlannerScope        `json:"scope"`
	ScopeDigest   string              `json:"scope_digest"`
	Proposal      PlannerProposal     `json:"proposal"`
	Compilation   ProposalCompilation `json:"compilation"`
	Work          PlannerWork         `json:"work"`
	ModelAudit    *PlannerModelAudit  `json:"model_audit,omitempty"`
	Experiment    *Report             `json:"experiment,omitempty"`
	Failure       *PlannerFailure     `json:"failure,omitempty"`
	Digest        string              `json:"digest"`
}

func RunPlannerAttempt(
	ctx context.Context,
	plannerID string,
	scope PlannerScope,
	proposal PlannerProposal,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
) (PlannerAttempt, error) {
	return runPlannerAttempt(ctx, plannerID, scope, proposal, nil, newAdapter, mapper)
}

func RunAuditedPlannerAttempt(
	ctx context.Context,
	plannerID string,
	scope PlannerScope,
	proposal PlannerProposal,
	audit PlannerModelAudit,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
) (PlannerAttempt, error) {
	if err := audit.Validate(); err != nil {
		return PlannerAttempt{}, err
	}
	return runPlannerAttempt(ctx, plannerID, scope, proposal, &audit, newAdapter, mapper)
}

func runPlannerAttempt(
	ctx context.Context,
	plannerID string,
	scope PlannerScope,
	proposal PlannerProposal,
	audit *PlannerModelAudit,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
) (PlannerAttempt, error) {
	if plannerID == "" {
		return PlannerAttempt{}, errors.New("PLANNER_COMPOSITION_INVALID")
	}
	modelWork := ModelWork{}
	if audit != nil {
		modelWork = audit.modelWork()
	}
	scopeDigest, err := scope.Digest()
	if err != nil {
		return PlannerAttempt{}, err
	}
	config, compilation, err := CompileProposal(scope, proposal)
	if err != nil {
		return PlannerAttempt{}, err
	}
	attempt := PlannerAttempt{
		SchemaVersion: PlannerAttemptVersion, PlannerID: plannerID,
		Scope: scope, ScopeDigest: scopeDigest, Proposal: proposal, Compilation: compilation,
		Work:       PlannerWork{ProposalAttempts: 1, Model: modelWork, Execution: emptyWork()},
		ModelAudit: audit,
	}
	if !compilation.Accepted {
		attempt.Status = PlannerStatusRejected
		attempt.Failure = &PlannerFailure{Phase: "compile", ReasonCode: compilation.ReasonCode,
			Run: compilation.FailedRun}
		return sealAndValidateAttempt(attempt)
	}
	report, err := Execute(ctx, config, newAdapter, mapper)
	if err != nil {
		var failure *ExecutionFailure
		if !errors.As(err, &failure) {
			return PlannerAttempt{}, err
		}
		attempt.Status = PlannerStatusExecutionFailed
		attempt.Work.Execution = failure.Work
		attempt.Failure = &PlannerFailure{
			Phase: failure.Phase, ReasonCode: failure.Code, Run: failure.Run, Decision: failure.Decision,
		}
		return sealAndValidateAttempt(attempt)
	}
	attempt.Status = PlannerStatusComplete
	attempt.Work.Execution = report.Work
	attempt.Experiment = &report
	return sealAndValidateAttempt(attempt)
}

func sealAndValidateAttempt(attempt PlannerAttempt) (PlannerAttempt, error) {
	sealed, err := attempt.Seal()
	if err != nil {
		return PlannerAttempt{}, err
	}
	if err := sealed.Validate(); err != nil {
		return PlannerAttempt{}, err
	}
	return sealed, nil
}

func (attempt PlannerAttempt) Seal() (PlannerAttempt, error) {
	attempt.Digest = ""
	digest, err := portableJSONDigest(attempt)
	if err != nil {
		return PlannerAttempt{}, err
	}
	attempt.Digest = digest
	return attempt, nil
}

func (attempt PlannerAttempt) Validate() error {
	if attempt.SchemaVersion != PlannerAttemptVersion || attempt.PlannerID == "" ||
		attempt.Work.ProposalAttempts != 1 || validateModelWork(attempt.Work.Model) != nil {
		return errors.New("PLANNER_ATTEMPT_IDENTITY_INVALID")
	}
	if attempt.ModelAudit == nil {
		if !reflect.DeepEqual(attempt.Work.Model, ModelWork{}) {
			return errors.New("PLANNER_ATTEMPT_MODEL_AUDIT_MISMATCH")
		}
	} else if attempt.ModelAudit.Validate() != nil ||
		!reflect.DeepEqual(attempt.Work.Model, attempt.ModelAudit.modelWork()) {
		return errors.New("PLANNER_ATTEMPT_MODEL_AUDIT_MISMATCH")
	}
	scopeDigest, err := attempt.Scope.Digest()
	if err != nil || scopeDigest != attempt.ScopeDigest {
		return errors.New("PLANNER_ATTEMPT_SCOPE_MISMATCH")
	}
	config, compilation, err := CompileProposal(attempt.Scope, attempt.Proposal)
	if err != nil || !reflect.DeepEqual(compilation, attempt.Compilation) {
		return errors.New("PLANNER_ATTEMPT_COMPILATION_MISMATCH")
	}
	switch attempt.Status {
	case PlannerStatusRejected:
		if compilation.Accepted || attempt.Experiment != nil || attempt.Failure == nil ||
			attempt.Failure.Phase != "compile" || attempt.Failure.ReasonCode != compilation.ReasonCode ||
			attempt.Failure.Run != compilation.FailedRun ||
			!reflect.DeepEqual(attempt.Work.Execution, emptyWork()) {
			return errors.New("PLANNER_ATTEMPT_REJECTION_MISMATCH")
		}
	case PlannerStatusExecutionFailed:
		if !compilation.Accepted || attempt.Experiment != nil || attempt.Failure == nil ||
			attempt.Failure.Phase == "" || attempt.Failure.Phase == "compile" ||
			attempt.Failure.ReasonCode == "" || validatePartialWork(attempt.Scope, attempt.Work.Execution) != nil ||
			attempt.Failure.Decision < 0 || attempt.Failure.Decision > attempt.Scope.DecisionsPerRun {
			return errors.New("PLANNER_ATTEMPT_EXECUTION_FAILURE_MISMATCH")
		}
		if attempt.Failure.Run != 0 && !containsRun(attempt.Scope.Runs, attempt.Failure.Run) {
			return errors.New("PLANNER_ATTEMPT_EXECUTION_FAILURE_MISMATCH")
		}
	case PlannerStatusComplete:
		if !compilation.Accepted || attempt.Experiment == nil || attempt.Failure != nil {
			return errors.New("PLANNER_ATTEMPT_SUCCESS_MISMATCH")
		}
		if err := attempt.Experiment.Validate(); err != nil {
			return err
		}
		if !reflect.DeepEqual(attempt.Experiment.Config, config) ||
			!reflect.DeepEqual(attempt.Work.Execution, attempt.Experiment.Work) {
			return errors.New("PLANNER_ATTEMPT_EXPERIMENT_MISMATCH")
		}
	default:
		return errors.New("PLANNER_ATTEMPT_STATUS_INVALID")
	}
	sealed, err := attempt.Seal()
	if err != nil || sealed.Digest != attempt.Digest {
		return errors.New("PLANNER_ATTEMPT_DIGEST_MISMATCH")
	}
	return nil
}

func validateModelWork(work ModelWork) error {
	if work.Calls < 0 || work.InputTokens < 0 || work.OutputTokens < 0 || work.TotalTokens < 0 ||
		work.TotalTokens != work.InputTokens+work.OutputTokens {
		return errors.New("PLANNER_MODEL_WORK_INVALID")
	}
	return nil
}

func validatePartialWork(scope PlannerScope, work WorkLedger) error {
	if !reflect.DeepEqual(work.Resources, emptyWork().Resources) ||
		!reflect.DeepEqual(work.Model, ModelWork{}) || work.Primary.SetupAttempts <= 0 ||
		work.Primary.SetupAttempts > len(scope.Runs) || work.Replay.SetupAttempts > work.Primary.SetupAttempts {
		return errors.New("PLANNER_EXECUTION_WORK_INVALID")
	}
	for _, phase := range []*PhaseWork{&work.Primary, &work.Replay} {
		if phase.SetupAttempts < 0 || phase.RuntimeInitializations < 0 || phase.PrepareActions < 0 ||
			phase.SchedulerDecisions < 0 || phase.RuntimeInitializations > phase.SetupAttempts ||
			phase.SchedulerDecisions > len(scope.Runs)*scope.DecisionsPerRun ||
			phase.WorkUnits != phase.SetupAttempts+phase.PrepareActions+phase.SchedulerDecisions {
			return errors.New("PLANNER_EXECUTION_WORK_INVALID")
		}
	}
	return nil
}

func containsRun(runs []int, target int) bool {
	for _, run := range runs {
		if run == target {
			return true
		}
	}
	return false
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}

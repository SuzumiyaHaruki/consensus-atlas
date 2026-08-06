// Package agentcampaign coordinates untrusted blind test-plan proposals
// against a trusted incremental Campaign Session.
package agentcampaign

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

const (
	StatusComplete       = "complete"
	StatusAttemptLimit   = "attempt_limit"
	StatusNoProgress     = "no_progress_limit"
	StatusRunBudget      = "run_budget"
	StatusDecisionBudget = "decision_budget"
	StatusTokenBudget    = "token_budget"
)

const (
	FindingProgress          = "progress"
	FindingNoProgress        = "no_progress"
	FindingProposalRejected  = "proposal_rejected"
	FindingDuplicateProposal = "duplicate_proposal"
	FindingPlanError         = "plan_execution_error"
	FindingGeneratorError    = "generator_error"
	FindingBudgetRejected    = "budget_rejected"
)

type Config struct {
	ID                string `json:"id"`
	MaxAttempts       int    `json:"max_attempts"`
	MaxNoProgress     int    `json:"max_no_progress"`
	MaxTotalRuns      int    `json:"max_total_runs"`
	MaxTotalDecisions int    `json:"max_total_decisions"`
	MaxTotalTokens    int    `json:"max_total_tokens"`
}

func (config Config) Validate() error {
	if config.ID == "" {
		return errors.New("agent campaign id is required")
	}
	if config.MaxAttempts < 1 || config.MaxAttempts > 20 {
		return errors.New("max attempts must be between 1 and 20")
	}
	if config.MaxNoProgress < 1 || config.MaxNoProgress > config.MaxAttempts {
		return errors.New("max no-progress attempts must be positive and no greater than max attempts")
	}
	if config.MaxTotalRuns < 1 || config.MaxTotalRuns > testplan.MaxSuiteRuns {
		return errors.New("agent campaign run budget is outside trusted limits")
	}
	if config.MaxTotalDecisions < 1 || config.MaxTotalDecisions > testplan.MaxSuiteDecisions {
		return errors.New("agent campaign decision budget is outside trusted limits")
	}
	if config.MaxTotalTokens < 1 || config.MaxTotalTokens > 1_000_000 {
		return errors.New("agent campaign token budget must be between 1 and 1000000")
	}
	return nil
}

type ProfileScope struct {
	ID       string   `json:"id"`
	Digest   string   `json:"digest"`
	Protocol string   `json:"protocol"`
	PSSID    string   `json:"pss_id,omitempty"`
	Nodes    []string `json:"nodes"`
}

type RemainingBudget struct {
	Attempts         int `json:"attempts"`
	Runs             int `json:"runs"`
	Decisions        int `json:"decisions"`
	Tokens           int `json:"tokens"`
	MaxPlanRuns      int `json:"max_plan_runs"`
	MaxPlanDecisions int `json:"max_plan_decisions"`
}

type DSLPolicy struct {
	AllowedInputs       []string     `json:"allowed_inputs"`
	ProtocolInputs      []BlindInput `json:"protocol_inputs,omitempty"`
	AllowedPrepareOps   []string     `json:"allowed_prepare_ops"`
	AllowedStrategies   []string     `json:"allowed_strategies"`
	DirectMessageInject bool         `json:"direct_message_inject"`
	TransientSelectors  bool         `json:"transient_selectors"`
	MaxTargets          int          `json:"max_targets"`
	MaxPrepareActions   int          `json:"max_prepare_actions"`
	MaxStimuli          int          `json:"max_stimuli"`
	MaxDuplicatesPerRun int          `json:"max_duplicates_per_run"`
}

type GenerationAudit struct {
	Provider              string   `json:"provider"`
	Model                 string   `json:"model"`
	Endpoint              string   `json:"endpoint,omitempty"`
	PromptDigest          string   `json:"prompt_digest,omitempty"`
	ThinkingMode          string   `json:"thinking_mode,omitempty"`
	Temperature           *float64 `json:"temperature,omitempty"`
	MaxTokens             int      `json:"max_tokens,omitempty"`
	ResponseID            string   `json:"response_id,omitempty"`
	SystemFingerprint     string   `json:"system_fingerprint,omitempty"`
	FinishReason          string   `json:"finish_reason,omitempty"`
	PromptTokens          int      `json:"prompt_tokens,omitempty"`
	PromptCacheHitTokens  int      `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int      `json:"prompt_cache_miss_tokens,omitempty"`
	CompletionTokens      int      `json:"completion_tokens,omitempty"`
	ReasoningTokens       int      `json:"reasoning_tokens,omitempty"`
	TotalTokens           int      `json:"total_tokens,omitempty"`
	DurationMillis        int64    `json:"duration_millis,omitempty"`
	RequestDigest         string   `json:"request_digest,omitempty"`
	ResponseDigest        string   `json:"response_digest,omitempty"`
}

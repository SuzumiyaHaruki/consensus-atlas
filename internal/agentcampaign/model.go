// Package agentcampaign coordinates untrusted Test Plan proposals against a
// trusted incremental campaign Session.
package agentcampaign

import (
	"context"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

const Version = 1

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

type DebtView struct {
	Obligation coverage.Obligation `json:"obligation"`
	Status     string              `json:"status"`
	Attempts   int                 `json:"attempts"`
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
	AllowedInputs       []string `json:"allowed_inputs"`
	AllowedPrepareOps   []string `json:"allowed_prepare_ops"`
	AllowedStrategies   []string `json:"allowed_strategies"`
	DirectMessageInject bool     `json:"direct_message_inject"`
	TransientSelectors  bool     `json:"transient_selectors"`
	MaxTargets          int      `json:"max_targets"`
	MaxPrepareActions   int      `json:"max_prepare_actions"`
	MaxStimuli          int      `json:"max_stimuli"`
	MaxDuplicatesPerRun int      `json:"max_duplicates_per_run"`
}

type GenerationRequest struct {
	Version           int               `json:"version"`
	CampaignID        string            `json:"campaign_id"`
	Attempt           int               `json:"attempt"`
	Profile           ProfileScope      `json:"profile"`
	Manifest          driver.Manifest   `json:"manifest"`
	Progress          campaign.Progress `json:"progress"`
	Debt              []DebtView        `json:"coverage_debt"`
	PreviousProposals []Proposal        `json:"previous_proposals,omitempty"`
	PreviousFindings  []Finding         `json:"previous_findings,omitempty"`
	Remaining         RemainingBudget   `json:"remaining_budget"`
	Policy            DSLPolicy         `json:"dsl_policy"`
}

type Proposal struct {
	Version int           `json:"version"`
	ID      string        `json:"id"`
	Plan    testplan.Plan `json:"plan"`
}

func (proposal Proposal) Validate() error {
	if proposal.Version != Version || proposal.ID == "" || proposal.Plan.ID == "" {
		return errors.New("proposal version, id, and plan id are required")
	}
	return nil
}

type Finding struct {
	Code             string   `json:"code"`
	Attempt          int      `json:"attempt"`
	ProposalID       string   `json:"proposal_id,omitempty"`
	PlanID           string   `json:"plan_id,omitempty"`
	Message          string   `json:"message"`
	Targeted         []string `json:"targeted,omitempty"`
	NewlyCovered     []string `json:"newly_covered,omitempty"`
	RemainingTargets []string `json:"remaining_targets,omitempty"`
	ScoreBefore      float64  `json:"score_before,omitempty"`
	ScoreAfter       float64  `json:"score_after,omitempty"`
	CoveredBefore    int      `json:"covered_before,omitempty"`
	CoveredAfter     int      `json:"covered_after,omitempty"`
	Runs             int      `json:"runs,omitempty"`
	Decisions        int      `json:"decisions,omitempty"`
	ReplayFailures   int      `json:"replay_failures,omitempty"`
	ExecutionErrors  int      `json:"execution_errors,omitempty"`
	OracleViolations int      `json:"oracle_violations,omitempty"`
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

type Planner interface {
	Generate(context.Context, GenerationRequest) (*Proposal, error)
}

type AuditProvider interface {
	LastGenerationAudit() GenerationAudit
}

type ExecutionSummary struct {
	PlanID           string            `json:"plan_id"`
	Before           campaign.Progress `json:"before"`
	After            campaign.Progress `json:"after"`
	Runs             int               `json:"runs"`
	Decisions        int               `json:"decisions"`
	ExecutionError   string            `json:"execution_error,omitempty"`
	NewlyCovered     []string          `json:"newly_covered,omitempty"`
	ReplayFailures   int               `json:"replay_failures"`
	ExecutionErrors  int               `json:"execution_errors"`
	OracleViolations int               `json:"oracle_violations"`
}

type Round struct {
	Attempt   int               `json:"attempt"`
	Proposal  *Proposal         `json:"proposal,omitempty"`
	Audit     GenerationAudit   `json:"generation"`
	Execution *ExecutionSummary `json:"execution,omitempty"`
	Finding   Finding           `json:"finding"`
}

type Report struct {
	Version               int              `json:"version"`
	Status                string           `json:"status"`
	StopReason            string           `json:"stop_reason"`
	Config                Config           `json:"config"`
	Profile               ProfileScope     `json:"profile"`
	TotalTokens           int              `json:"total_tokens"`
	ConsecutiveNoProgress int              `json:"consecutive_no_progress"`
	Rounds                []Round          `json:"rounds"`
	Blackboard            BlackboardReport `json:"blackboard"`
	Campaign              campaign.Report  `json:"campaign"`
}

type proposalBudgetError struct {
	kind                 string
	requested, remaining int
}

func (failure proposalBudgetError) Error() string {
	return fmt.Sprintf("proposal %s budget %d exceeds remaining %d", failure.kind, failure.requested, failure.remaining)
}

func budgetError(kind string, requested, remaining int) error {
	return proposalBudgetError{kind: kind, requested: requested, remaining: remaining}
}

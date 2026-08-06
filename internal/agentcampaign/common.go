package agentcampaign

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

var safeAgentID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type blindBudgetError struct{ kind, requested, remaining string }

func (failure blindBudgetError) Error() string {
	return fmt.Sprintf("proposal %s budget %s exceeds remaining %s", failure.kind, failure.requested, failure.remaining)
}

func budgetError(kind string, requested, remaining int) error {
	return blindBudgetError{kind: kind, requested: fmt.Sprint(requested), remaining: fmt.Sprint(remaining)}
}

type ExecutionSummary struct {
	PlanID           string            `json:"plan_id"`
	Before           campaign.Progress `json:"before"`
	After            campaign.Progress `json:"after"`
	Runs             int               `json:"runs"`
	Decisions        int               `json:"decisions"`
	WorkUnits        int               `json:"work_units"`
	ExecutionError   string            `json:"execution_error,omitempty"`
	NewlyCovered     []string          `json:"newly_covered,omitempty"`
	ReplayFailures   int               `json:"replay_failures"`
	ExecutionErrors  int               `json:"execution_errors"`
	OracleViolations int               `json:"oracle_violations"`
}

func summarizeExecution(report campaign.PlanReport) ExecutionSummary {
	summary := ExecutionSummary{PlanID: report.ID, Before: report.Before, After: report.After, Runs: len(report.Runs), Decisions: report.ChargedDecisions, WorkUnits: report.Cost.Primary.WorkUnits, ExecutionError: report.ExecutionError}
	seen := make(map[string]bool)
	for _, run := range report.Runs {
		if !run.ReplayStable {
			summary.ReplayFailures++
		}
		if run.Explorer.ExecutionError != "" {
			summary.ExecutionErrors++
		}
		summary.OracleViolations += len(run.Oracle.Violations)
		for _, id := range run.NewlyCovered {
			if !seen[id] {
				seen[id] = true
				summary.NewlyCovered = append(summary.NewlyCovered, id)
			}
		}
	}
	sort.Strings(summary.NewlyCovered)
	return summary
}

func stopStatus(report campaign.Report, tokens, noProgress int, config Config) (string, string) {
	if report.Final.Debt == 0 {
		return StatusComplete, "all actionable coverage debt was covered"
	}
	if noProgress >= config.MaxNoProgress {
		return StatusNoProgress, "consecutive no-progress limit reached"
	}
	if report.ChargedRuns >= config.MaxTotalRuns {
		return StatusRunBudget, "run budget exhausted"
	}
	if report.ChargedDecisions >= config.MaxTotalDecisions {
		return StatusDecisionBudget, "decision budget exhausted"
	}
	if tokens >= config.MaxTotalTokens {
		return StatusTokenBudget, "generation token budget exhausted"
	}
	return "", ""
}

func planBehaviorDigest(plan testplan.Plan) (string, error) {
	copy := plan
	copy.ID = ""
	copy.Targets = append([]string(nil), plan.Targets...)
	sort.Strings(copy.Targets)
	copy.Search.Config.Seed = 0
	for index := range copy.Prepare {
		if opaquePayload(copy.Prepare[index].Kind) {
			copy.Prepare[index].Payload = json.RawMessage(`{"opaque":true}`)
		}
	}
	for index := range copy.Stimuli {
		if opaquePayload(copy.Stimuli[index].Kind) {
			copy.Stimuli[index].Payload = json.RawMessage(`{"opaque":true}`)
		}
	}
	encoded, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func opaquePayload(kind core.EventKind) bool {
	return kind == core.EventProtocolInput || kind == core.EventPropose
}

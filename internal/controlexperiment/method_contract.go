package controlexperiment

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

// MethodFailure is the shared terminal failure projection used by Campaign
// and semantic episode artifacts. It is not an Oracle verdict.
type MethodFailure struct {
	Phase    string                          `json:"phase"`
	Code     string                          `json:"code"`
	Decision int                             `json:"decision,omitempty"`
	Terminal *controlruntime.TerminalOutcome `json:"terminal_outcome,omitempty"`
}

// MethodBudget binds an evaluator-owned execution to common work ceilings.
// The retired batch-method ledger used the same wire shape.
type MethodBudget struct {
	MaxExecutionAttempts int `json:"max_execution_attempts"`
	MaxPrimaryWorkUnits  int `json:"max_primary_work_units"`
	MaxReplayWorkUnits   int `json:"max_replay_work_units"`
}

func (budget MethodBudget) validate() error {
	if budget.MaxExecutionAttempts <= 0 || budget.MaxPrimaryWorkUnits <= 0 ||
		budget.MaxReplayWorkUnits <= 0 {
		return errors.New("EXPERIMENT_METHOD_BUDGET_INVALID")
	}
	return nil
}

func validStatelessPlannerWork(work ModelWork) bool {
	return (work.Calls == 0 && work.InputTokens == 0 && work.OutputTokens == 0 && work.TotalTokens == 0) ||
		(work.Calls == 1 && work.InputTokens >= 0 && work.OutputTokens >= 0 &&
			work.TotalTokens > 0 && work.TotalTokens == work.InputTokens+work.OutputTokens)
}

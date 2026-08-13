package controlexperiment

import "errors"

// MethodFailure is the shared terminal failure projection used by Campaign
// and semantic episode artifacts. It is not an Oracle verdict.
type MethodFailure struct {
	Phase    string `json:"phase"`
	Code     string `json:"code"`
	Decision int    `json:"decision,omitempty"`
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

func validateMethodWork(work WorkLedger) error {
	for _, phase := range []PhaseWork{work.Primary, work.Replay} {
		if phase.SetupAttempts < 0 || phase.RuntimeInitializations < 0 || phase.PrepareActions < 0 ||
			phase.SchedulerDecisions < 0 ||
			phase.WorkUnits != phase.SetupAttempts+phase.PrepareActions+phase.SchedulerDecisions {
			return errors.New("EXPERIMENT_METHOD_WORK_INVALID")
		}
	}
	if work.Model.Calls < 0 || work.Model.InputTokens < 0 || work.Model.OutputTokens < 0 ||
		work.Model.TotalTokens != work.Model.InputTokens+work.Model.OutputTokens ||
		work.Resources.WallTime == "" || work.Resources.CPUTime == "" || work.Resources.PeakRSS == "" {
		return errors.New("EXPERIMENT_METHOD_WORK_INVALID")
	}
	return nil
}

func addWorkLedgers(left WorkLedger, right WorkLedger) WorkLedger {
	addPhaseWork := func(target *PhaseWork, source PhaseWork) {
		target.SetupAttempts += source.SetupAttempts
		target.RuntimeInitializations += source.RuntimeInitializations
		target.PrepareActions += source.PrepareActions
		target.SchedulerDecisions += source.SchedulerDecisions
		updateWorkUnits(target)
	}
	addPhaseWork(&left.Primary, right.Primary)
	addPhaseWork(&left.Replay, right.Replay)
	left.Model.Calls += right.Model.Calls
	left.Model.InputTokens += right.Model.InputTokens
	left.Model.OutputTokens += right.Model.OutputTokens
	left.Model.TotalTokens = left.Model.InputTokens + left.Model.OutputTokens
	return left
}

func validStatelessPlannerWork(work ModelWork) bool {
	return (work.Calls == 0 && work.InputTokens == 0 && work.OutputTokens == 0 && work.TotalTokens == 0) ||
		(work.Calls == 1 && work.InputTokens >= 0 && work.OutputTokens >= 0 &&
			work.TotalTokens > 0 && work.TotalTokens == work.InputTokens+work.OutputTokens)
}

func validStatelessAggregateModelWork(work ModelWork) bool {
	return work.Calls >= 0 && work.InputTokens >= 0 && work.OutputTokens >= 0 &&
		work.TotalTokens == work.InputTokens+work.OutputTokens &&
		((work.Calls == 0 && work.TotalTokens == 0) || (work.Calls > 0 && work.TotalTokens > 0))
}

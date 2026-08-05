// Package explore provides protocol-neutral, equal-budget schedule explorers.
// It operates only on Engine decisions and never interprets protocol state.
package explore

import (
	"context"
	"errors"
	"fmt"
	"math/rand"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type Strategy string

const (
	StrategyRandom Strategy = "random"
	StrategyDFS    Strategy = "dfs"
)

type DecisionAction string

const (
	ActionExecute   DecisionAction = "execute"
	ActionDrop      DecisionAction = "drop"
	ActionDuplicate DecisionAction = "duplicate"
)

type ActionPolicy struct {
	DropMessages      bool `json:"drop_messages"`
	DuplicateMessages bool `json:"duplicate_messages"`
	MaxDuplicates     int  `json:"max_duplicates_per_run"`
}

type Config struct {
	Runs           int          `json:"max_runs"`
	BudgetPerRun   int          `json:"budget_per_run"`
	DecisionBudget int          `json:"decision_budget"`
	Seed           int64        `json:"seed"`
	Actions        ActionPolicy `json:"actions"`
}

func (c Config) Validate() error {
	if c.Runs <= 0 {
		return errors.New("runs must be positive")
	}
	if c.BudgetPerRun <= 0 {
		return errors.New("budget per run must be positive")
	}
	if c.DecisionBudget < 0 {
		return errors.New("decision budget cannot be negative")
	}
	if c.DecisionBudget > c.Runs*c.BudgetPerRun {
		return fmt.Errorf("decision budget %d exceeds max runs times per-run budget %d",
			c.DecisionBudget, c.Runs*c.BudgetPerRun)
	}
	if c.Actions.MaxDuplicates < 0 {
		return errors.New("max duplicates per run cannot be negative")
	}
	if c.Actions.DuplicateMessages && c.Actions.MaxDuplicates == 0 {
		return errors.New("duplicate messages requires a positive max duplicates per run")
	}
	return nil
}

func (c Config) TargetDecisionBudget() int {
	if c.DecisionBudget > 0 {
		return c.DecisionBudget
	}
	return c.Runs * c.BudgetPerRun
}

// Factory must return a fresh Engine after deterministic setup. The first
// explorer decision starts the measurement window.
type Factory func(context.Context) (*engine.Engine, error)

type Explorer interface {
	Name() Strategy
	Explore(context.Context, Factory, Config) (Result, error)
}

type Result struct {
	Runs                 []RunResult `json:"runs"`
	TargetDecisionBudget int         `json:"target_decision_budget"`
	ChargedDecisions     int         `json:"charged_decisions"`
	BudgetReached        bool        `json:"budget_reached"`
	StopReason           string      `json:"stop_reason"`
}

func New(strategy Strategy) (Explorer, error) {
	switch strategy {
	case StrategyRandom:
		return Random{}, nil
	case StrategyDFS:
		return DFS{}, nil
	default:
		return nil, fmt.Errorf("unknown exploration strategy %q", strategy)
	}
}

type Decision struct {
	Ordinal        int            `json:"ordinal"`
	Action         DecisionAction `json:"action"`
	EventID        string         `json:"event_id"`
	EventKind      core.EventKind `json:"event_kind"`
	Source         string         `json:"source,omitempty"`
	Target         string         `json:"target,omitempty"`
	TypeHint       string         `json:"type_hint,omitempty"`
	PayloadDigest  string         `json:"payload_digest,omitempty"`
	ChoiceIndex    int            `json:"choice_index"`
	CandidateCount int            `json:"candidate_count"`
	CreatedEventID string         `json:"created_event_id,omitempty"`
}

type RunResult struct {
	Run                    int                `json:"run"`
	Seed                   *int64             `json:"seed,omitempty"`
	Termination            string             `json:"termination"`
	ExecutionError         string             `json:"execution_error,omitempty"`
	Conform                bool               `json:"conform"`
	ConformanceError       string             `json:"conformance_error,omitempty"`
	SetupFingerprint       string             `json:"setup_fingerprint"`
	ExecutionFingerprint   string             `json:"execution_fingerprint"`
	MeasurementFingerprint string             `json:"measurement_fingerprint"`
	Decisions              []Decision         `json:"decisions"`
	Trace                  []core.TraceRecord `json:"trace"`
	PendingAtEnd           []core.Event       `json:"pending_at_end,omitempty"`

	InitialSnapshot any                `json:"-"`
	SetupTrace      []core.TraceRecord `json:"-"`
	FullTrace       []core.TraceRecord `json:"-"`
}

type candidate struct {
	action DecisionAction
	event  core.Event
}

type attempt struct {
	result  RunResult
	choices []int
	arities []int
}

type chooser func(depth int, candidates []candidate) (int, error)

func executeRun(
	ctx context.Context,
	factory Factory,
	config Config,
	run int,
	seed *int64,
	choose chooser,
) (attempt, error) {
	e, setupTrace, result, err := beginRun(ctx, factory, run, seed)
	if err != nil {
		return attempt{}, err
	}
	var choices, arities []int
	duplicates := 0
	for depth := 0; depth < config.BudgetPerRun; depth++ {
		if err := ctx.Err(); err != nil {
			return attempt{}, err
		}
		candidates := enumerate(e, config.Actions, duplicates)
		if len(candidates) == 0 {
			result.Termination = "quiescent"
			break
		}
		choice, err := choose(depth, candidates)
		if err != nil {
			return attempt{}, err
		}
		if choice < 0 || choice >= len(candidates) {
			return attempt{}, fmt.Errorf("run %d decision %d chose %d of %d candidates",
				run, depth+1, choice, len(candidates))
		}
		current := candidates[choice]
		beforeRecords := len(e.Trace())
		created, actionErr := apply(ctx, e, current)
		if got := len(e.Trace()); got != beforeRecords+1 {
			return attempt{}, fmt.Errorf("run %d decision %d produced %d trace records, want exactly one",
				run, depth+1, got-beforeRecords)
		}
		decision := decisionFrom(current, depth+1, choice, len(candidates))
		decision.CreatedEventID = created
		result.Decisions = append(result.Decisions, decision)
		choices = append(choices, choice)
		arities = append(arities, len(candidates))
		if current.action == ActionDuplicate {
			duplicates++
		}
		if actionErr != nil {
			result.Termination = "execution_error"
			result.ExecutionError = actionErr.Error()
			break
		}
	}
	if result.Termination == "" {
		result.Termination = "budget_exhausted"
	}
	result, err = finishRun(e, setupTrace, result)
	if err != nil {
		return attempt{}, err
	}
	return attempt{result: result, choices: choices, arities: arities}, nil
}

func beginRun(
	ctx context.Context, factory Factory, run int, seed *int64,
) (*engine.Engine, []core.TraceRecord, RunResult, error) {
	e, err := factory(ctx)
	if err != nil {
		return nil, nil, RunResult{}, fmt.Errorf("create run %d: %w", run, err)
	}
	setupTrace := e.Trace()
	setupFingerprint, err := semantic.ExecutionFingerprint(setupTrace)
	if err != nil {
		return nil, nil, RunResult{}, err
	}
	result := RunResult{
		Run: run, Seed: seed, SetupFingerprint: setupFingerprint,
		InitialSnapshot: e.Snapshot(), SetupTrace: setupTrace,
	}
	return e, setupTrace, result, nil
}

func finishRun(e *engine.Engine, setupTrace []core.TraceRecord, result RunResult) (RunResult, error) {
	if conformErr := e.CheckConformance(); conformErr != nil {
		result.ConformanceError = conformErr.Error()
	} else {
		result.Conform = true
	}
	result.FullTrace = e.Trace()
	if len(result.FullTrace) < len(setupTrace) {
		return RunResult{}, fmt.Errorf("run %d full trace is shorter than setup trace", result.Run)
	}
	result.Trace = append([]core.TraceRecord(nil), result.FullTrace[len(setupTrace):]...)
	if len(result.Trace) != len(result.Decisions) {
		return RunResult{}, fmt.Errorf("run %d has %d decisions but %d measurement records",
			result.Run, len(result.Decisions), len(result.Trace))
	}
	result.PendingAtEnd = e.Pending()
	var err error
	result.ExecutionFingerprint, err = semantic.ExecutionFingerprint(result.FullTrace)
	if err != nil {
		return RunResult{}, err
	}
	result.MeasurementFingerprint, err = semantic.ExecutionFingerprint(result.Trace)
	if err != nil {
		return RunResult{}, err
	}
	return result, nil
}

// ReplayRun forces a recorded decision log against a fresh setup root. It does
// not ask the original Explorer to choose again.
func ReplayRun(
	ctx context.Context, factory Factory, policy ActionPolicy, expected RunResult,
) (RunResult, error) {
	e, setupTrace, result, err := beginRun(ctx, factory, expected.Run, expected.Seed)
	if err != nil {
		return RunResult{}, err
	}
	if result.SetupFingerprint != expected.SetupFingerprint {
		return RunResult{}, fmt.Errorf("run %d setup fingerprint changed: %s != %s",
			expected.Run, result.SetupFingerprint, expected.SetupFingerprint)
	}
	duplicates := 0
	for depth, wanted := range expected.Decisions {
		if err := ctx.Err(); err != nil {
			return RunResult{}, err
		}
		candidates := enumerate(e, policy, duplicates)
		choice := -1
		for index, current := range candidates {
			if current.action == wanted.Action && current.event.ID == wanted.EventID {
				choice = index
				break
			}
		}
		if choice < 0 {
			return RunResult{}, fmt.Errorf("run %d decision %d is no longer a candidate: %s %s",
				expected.Run, depth+1, wanted.Action, wanted.EventID)
		}
		if choice != wanted.ChoiceIndex || len(candidates) != wanted.CandidateCount {
			return RunResult{}, fmt.Errorf(
				"run %d decision %d candidate set changed: choice %d/%d, want %d/%d",
				expected.Run, depth+1, choice, len(candidates), wanted.ChoiceIndex, wanted.CandidateCount,
			)
		}
		current := candidates[choice]
		beforeRecords := len(e.Trace())
		created, actionErr := apply(ctx, e, current)
		if got := len(e.Trace()); got != beforeRecords+1 {
			return RunResult{}, fmt.Errorf("run %d replay decision %d produced %d trace records, want exactly one",
				expected.Run, depth+1, got-beforeRecords)
		}
		decision := decisionFrom(current, depth+1, choice, len(candidates))
		decision.CreatedEventID = created
		if decision != wanted {
			return RunResult{}, fmt.Errorf("run %d replay decision %d metadata changed", expected.Run, depth+1)
		}
		result.Decisions = append(result.Decisions, decision)
		if current.action == ActionDuplicate {
			duplicates++
		}
		if actionErr != nil {
			result.Termination = "execution_error"
			result.ExecutionError = actionErr.Error()
			if depth != len(expected.Decisions)-1 || expected.Termination != result.Termination ||
				expected.ExecutionError != result.ExecutionError {
				return RunResult{}, fmt.Errorf("run %d decision %d execution error changed: %q",
					expected.Run, depth+1, result.ExecutionError)
			}
			break
		}
	}
	if result.Termination == "" {
		switch expected.Termination {
		case "budget_exhausted":
			result.Termination = expected.Termination
		case "quiescent":
			if candidates := enumerate(e, policy, duplicates); len(candidates) != 0 {
				return RunResult{}, fmt.Errorf("run %d is no longer quiescent; %d candidates remain",
					expected.Run, len(candidates))
			}
			result.Termination = expected.Termination
		case "execution_error":
			return RunResult{}, fmt.Errorf("run %d expected an execution error but replay succeeded", expected.Run)
		default:
			return RunResult{}, fmt.Errorf("run %d has unknown termination %q", expected.Run, expected.Termination)
		}
	}
	result, err = finishRun(e, setupTrace, result)
	if err != nil {
		return RunResult{}, err
	}
	if result.ExecutionFingerprint != expected.ExecutionFingerprint ||
		result.MeasurementFingerprint != expected.MeasurementFingerprint ||
		result.Conform != expected.Conform || result.ConformanceError != expected.ConformanceError {
		return result, fmt.Errorf("run %d strict replay fingerprint or conformance changed", expected.Run)
	}
	return result, nil
}

func enumerate(e *engine.Engine, policy ActionPolicy, duplicates int) []candidate {
	enabled := e.Enabled()
	pending := e.Pending()
	candidates := make([]candidate, 0, len(enabled)+2*len(pending))
	for _, event := range enabled {
		candidates = append(candidates, candidate{action: ActionExecute, event: event})
	}
	if policy.DropMessages {
		for _, event := range pending {
			if event.Kind == core.EventMessage {
				candidates = append(candidates, candidate{action: ActionDrop, event: event})
			}
		}
	}
	if policy.DuplicateMessages && duplicates < policy.MaxDuplicates {
		for _, event := range pending {
			if event.Kind == core.EventMessage {
				candidates = append(candidates, candidate{action: ActionDuplicate, event: event})
			}
		}
	}
	return candidates
}

func apply(ctx context.Context, e *engine.Engine, current candidate) (string, error) {
	switch current.action {
	case ActionExecute:
		_, err := e.Execute(ctx, current.event.ID)
		return "", err
	case ActionDrop:
		_, err := e.Drop(current.event.ID)
		return "", err
	case ActionDuplicate:
		return e.Duplicate(current.event.ID)
	default:
		return "", fmt.Errorf("unknown decision action %q", current.action)
	}
}

func decisionFrom(current candidate, ordinal, choice, count int) Decision {
	decision := Decision{
		Ordinal: ordinal, Action: current.action, EventID: current.event.ID,
		EventKind: current.event.Kind, Source: current.event.Source, Target: current.event.Target,
		ChoiceIndex: choice, CandidateCount: count,
	}
	if current.event.Message != nil {
		decision.TypeHint = current.event.Message.TypeHint
		decision.PayloadDigest = current.event.Message.PayloadDigest
	}
	return decision
}

type Random struct{}

func (Random) Name() Strategy { return StrategyRandom }

func (Random) Explore(ctx context.Context, factory Factory, config Config) (Result, error) {
	if err := config.Validate(); err != nil {
		return Result{}, err
	}
	target := config.TargetDecisionBudget()
	result := Result{TargetDecisionBudget: target, Runs: make([]RunResult, 0, config.Runs)}
	for index := 0; index < config.Runs && result.ChargedDecisions < target; index++ {
		seed := derivedSeed(config.Seed, index)
		rng := rand.New(rand.NewSource(seed))
		runConfig := withRemainingBudget(config, target-result.ChargedDecisions)
		current, err := executeRun(ctx, factory, runConfig, index+1, &seed,
			func(_ int, candidates []candidate) (int, error) {
				return rng.Intn(len(candidates)), nil
			})
		if err != nil {
			return Result{}, err
		}
		result.Runs = append(result.Runs, current.result)
		result.ChargedDecisions += len(current.result.Decisions)
	}
	result.BudgetReached = result.ChargedDecisions == target
	if result.BudgetReached {
		result.StopReason = "decision_budget_reached"
	} else {
		result.StopReason = "run_limit_reached"
	}
	return result, nil
}

func derivedSeed(base int64, run int) int64 {
	return int64(uint64(base) + uint64(run)*0x9e3779b97f4a7c15)
}

type DFS struct{}

func (DFS) Name() Strategy { return StrategyDFS }

func (DFS) Explore(ctx context.Context, factory Factory, config Config) (Result, error) {
	if err := config.Validate(); err != nil {
		return Result{}, err
	}
	target := config.TargetDecisionBudget()
	result := Result{TargetDecisionBudget: target, Runs: make([]RunResult, 0, config.Runs)}
	var plan []int
	searchExhausted := false
	for len(result.Runs) < config.Runs && result.ChargedDecisions < target {
		currentPlan := append([]int(nil), plan...)
		runConfig := withRemainingBudget(config, target-result.ChargedDecisions)
		current, err := executeRun(ctx, factory, runConfig, len(result.Runs)+1, nil,
			func(depth int, candidates []candidate) (int, error) {
				if depth < len(currentPlan) {
					return currentPlan[depth], nil
				}
				return 0, nil
			})
		if err != nil {
			return Result{}, err
		}
		result.Runs = append(result.Runs, current.result)
		result.ChargedDecisions += len(current.result.Decisions)
		if result.ChargedDecisions == target {
			break
		}
		next, ok := nextDFSPlan(current.choices, current.arities)
		if !ok {
			searchExhausted = true
			break
		}
		plan = next
	}
	result.BudgetReached = result.ChargedDecisions == target
	switch {
	case result.BudgetReached:
		result.StopReason = "decision_budget_reached"
	case searchExhausted:
		result.StopReason = "search_space_exhausted"
	default:
		result.StopReason = "run_limit_reached"
	}
	return result, nil
}

func withRemainingBudget(config Config, remaining int) Config {
	if remaining < config.BudgetPerRun {
		config.BudgetPerRun = remaining
	}
	return config
}

func nextDFSPlan(choices, arities []int) ([]int, bool) {
	for depth := len(choices) - 1; depth >= 0; depth-- {
		if choices[depth]+1 < arities[depth] {
			next := append([]int(nil), choices[:depth+1]...)
			next[depth]++
			return next, true
		}
	}
	return nil, false
}

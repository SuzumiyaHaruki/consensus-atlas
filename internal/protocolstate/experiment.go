package protocolstate

import (
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

// MeasuredRun contains only the explicit measurement window. Setup is
// represented by InitialSnapshot and does not consume scheduler-decision
// budget.
type MeasuredRun struct {
	Run             int
	DecisionCount   int
	InitialSnapshot any
	Trace           []core.TraceRecord
}

type ExperimentWitness struct {
	Key                 string `json:"key"`
	FirstRun            int    `json:"first_run"`
	FirstRunDecision    int    `json:"first_run_decision"`
	FirstGlobalDecision int    `json:"first_global_decision"`
	TraceStep           int    `json:"trace_step,omitempty"`
	State               any    `json:"state"`
}

type ExperimentPoint struct {
	Decisions    int  `json:"decisions"`
	Run          int  `json:"run"`
	RunDecision  int  `json:"run_decision"`
	UniqueStates int  `json:"unique_states"`
	NewState     bool `json:"new_state"`
}

type ExperimentSummary struct {
	PSSID              string              `json:"pss_id"`
	Runs               int                 `json:"runs"`
	TotalDecisions     int                 `json:"total_decisions"`
	ProtocolSamples    int                 `json:"protocol_samples"`
	InitialStateKey    string              `json:"initial_state_key"`
	UniqueStates       int                 `json:"unique_states"`
	PrefixArea         int64               `json:"prefix_area"`
	SelfNormalizedArea float64             `json:"self_normalized_area"`
	Curve              []ExperimentPoint   `json:"curve"`
	States             []ExperimentWitness `json:"states"`
}

// Aggregate computes a denominator-free union over runs. Each trace record is
// exactly one charged scheduler decision. The area is the discrete prefix sum
// Σ D(b); SelfNormalizedArea divides it by B*D(B) and therefore reports early
// discovery timing, not absolute state yield.
func Aggregate(runs []MeasuredRun, projector Projector) (ExperimentSummary, error) {
	if projector == nil {
		return ExperimentSummary{}, errors.New("protocol state projector is nil")
	}
	if len(runs) == 0 {
		return ExperimentSummary{}, errors.New("at least one measured run is required")
	}
	summary := ExperimentSummary{PSSID: projector.ID(), Runs: len(runs)}
	seen := make(map[string]bool)
	for index, run := range runs {
		if run.Run <= 0 {
			return ExperimentSummary{}, fmt.Errorf("measured run %d has invalid run number %d", index+1, run.Run)
		}
		if run.DecisionCount != len(run.Trace) {
			return ExperimentSummary{}, fmt.Errorf("run %d has %d decisions but %d trace records",
				run.Run, run.DecisionCount, len(run.Trace))
		}
		initialState, initialKey, err := projector.Project(run.InitialSnapshot)
		if err != nil {
			return ExperimentSummary{}, fmt.Errorf("project run %d initial state: %w", run.Run, err)
		}
		if initialKey == "" {
			return ExperimentSummary{}, fmt.Errorf("project run %d initial state: empty key", run.Run)
		}
		if summary.InitialStateKey == "" {
			summary.InitialStateKey = initialKey
		} else if summary.InitialStateKey != initialKey {
			return ExperimentSummary{}, fmt.Errorf(
				"run %d starts from %s, want shared measurement root %s",
				run.Run, initialKey, summary.InitialStateKey,
			)
		}
		if !seen[initialKey] {
			seen[initialKey] = true
			summary.States = append(summary.States, ExperimentWitness{
				Key: initialKey, FirstRun: run.Run, State: initialState,
			})
		}

		for decision, record := range run.Trace {
			summary.TotalDecisions++
			isNew := false
			if projector.IsSample(record) {
				summary.ProtocolSamples++
				state, key, err := projector.Project(record.After)
				if err != nil {
					return ExperimentSummary{}, fmt.Errorf(
						"project run %d decision %d at trace step %d: %w",
						run.Run, decision+1, record.Step, err,
					)
				}
				if key == "" {
					return ExperimentSummary{}, fmt.Errorf(
						"project run %d decision %d at trace step %d: empty key",
						run.Run, decision+1, record.Step,
					)
				}
				if !seen[key] {
					seen[key] = true
					isNew = true
					summary.States = append(summary.States, ExperimentWitness{
						Key: key, FirstRun: run.Run, FirstRunDecision: decision + 1,
						FirstGlobalDecision: summary.TotalDecisions, TraceStep: record.Step, State: state,
					})
				}
			}
			summary.Curve = append(summary.Curve, ExperimentPoint{
				Decisions: summary.TotalDecisions, Run: run.Run, RunDecision: decision + 1,
				UniqueStates: len(seen), NewState: isNew,
			})
			summary.PrefixArea += int64(len(seen))
		}
	}
	summary.UniqueStates = len(seen)
	if summary.TotalDecisions > 0 && summary.UniqueStates > 0 {
		summary.SelfNormalizedArea = float64(summary.PrefixArea) /
			float64(summary.TotalDecisions*summary.UniqueStates)
	}
	return summary, nil
}

package protocolstate

import (
	"errors"
	"fmt"
)

type MeasuredDecision struct {
	Step   int
	Sample *Sample
}

type MeasuredRun struct {
	Run       int
	Initial   Sample
	Decisions []MeasuredDecision
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

// Aggregate computes a denominator-free state union over charged decisions.
func Aggregate(pssID string, runs []MeasuredRun) (ExperimentSummary, error) {
	if pssID == "" {
		return ExperimentSummary{}, errors.New("protocol state PSS ID is empty")
	}
	if len(runs) == 0 {
		return ExperimentSummary{}, errors.New("at least one measured run is required")
	}
	summary := ExperimentSummary{PSSID: pssID, Runs: len(runs)}
	seenStates := make(map[string]bool)
	seenRuns := make(map[int]bool)
	for index, run := range runs {
		if run.Run <= 0 || seenRuns[run.Run] {
			return ExperimentSummary{}, fmt.Errorf("measured run %d has invalid or duplicate run number %d", index+1, run.Run)
		}
		seenRuns[run.Run] = true
		if run.Initial.Key == "" {
			return ExperimentSummary{}, fmt.Errorf("run %d initial state has empty key", run.Run)
		}
		if summary.InitialStateKey == "" {
			summary.InitialStateKey = run.Initial.Key
		} else if summary.InitialStateKey != run.Initial.Key {
			return ExperimentSummary{}, fmt.Errorf("run %d starts from %s, want shared measurement root %s",
				run.Run, run.Initial.Key, summary.InitialStateKey)
		}
		if !seenStates[run.Initial.Key] {
			seenStates[run.Initial.Key] = true
			summary.States = append(summary.States, ExperimentWitness{
				Key: run.Initial.Key, FirstRun: run.Run, State: run.Initial.State,
			})
		}
		lastStep := 0
		for ordinal, decision := range run.Decisions {
			if decision.Step <= lastStep {
				return ExperimentSummary{}, fmt.Errorf("run %d decision %d has non-increasing step %d",
					run.Run, ordinal+1, decision.Step)
			}
			lastStep = decision.Step
			summary.TotalDecisions++
			isNew := false
			if decision.Sample != nil {
				if decision.Sample.Step != decision.Step || decision.Sample.Key == "" {
					return ExperimentSummary{}, fmt.Errorf("run %d decision %d has invalid sample", run.Run, ordinal+1)
				}
				summary.ProtocolSamples++
				if !seenStates[decision.Sample.Key] {
					seenStates[decision.Sample.Key] = true
					isNew = true
					summary.States = append(summary.States, ExperimentWitness{
						Key: decision.Sample.Key, FirstRun: run.Run, FirstRunDecision: ordinal + 1,
						FirstGlobalDecision: summary.TotalDecisions, TraceStep: decision.Step,
						State: decision.Sample.State,
					})
				}
			}
			summary.Curve = append(summary.Curve, ExperimentPoint{
				Decisions: summary.TotalDecisions, Run: run.Run, RunDecision: ordinal + 1,
				UniqueStates: len(seenStates), NewState: isNew,
			})
			summary.PrefixArea += int64(len(seenStates))
		}
	}
	summary.UniqueStates = len(seenStates)
	if summary.TotalDecisions > 0 && summary.UniqueStates > 0 {
		summary.SelfNormalizedArea = float64(summary.PrefixArea) /
			float64(summary.TotalDecisions*summary.UniqueStates)
	}
	return summary, nil
}

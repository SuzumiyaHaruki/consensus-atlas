// Package legacyexperiment contains the v1 core.TraceRecord cross-run ledger.
package legacyexperiment

import (
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

// Projector is the legacy v1 cross-run experiment boundary.
type Projector interface {
	ID() string
	IsSample(core.TraceRecord) bool
	Project(snapshot any) (state any, key string, err error)
}

// MeasuredRun contains only the explicit measurement window. Setup is
// represented by InitialSnapshot and does not consume scheduler-decision
// budget.
type MeasuredRun struct {
	Run             int
	DecisionCount   int
	InitialSnapshot any
	Trace           []core.TraceRecord
}

type ExperimentWitness = protocolstate.ExperimentWitness
type ExperimentPoint = protocolstate.ExperimentPoint
type ExperimentSummary = protocolstate.ExperimentSummary

// Aggregate adapts a legacy v1 trace into the protocol-neutral ledger.
func Aggregate(runs []MeasuredRun, projector Projector) (ExperimentSummary, error) {
	if projector == nil {
		return ExperimentSummary{}, errors.New("protocol state projector is nil")
	}
	converted := make([]protocolstate.MeasuredRun, 0, len(runs))
	for _, run := range runs {
		if run.DecisionCount != len(run.Trace) {
			return ExperimentSummary{}, fmt.Errorf("run %d has %d decisions but %d trace records",
				run.Run, run.DecisionCount, len(run.Trace))
		}
		initialState, initialKey, err := projector.Project(run.InitialSnapshot)
		if err != nil {
			return ExperimentSummary{}, fmt.Errorf("project run %d initial state: %w", run.Run, err)
		}
		current := protocolstate.MeasuredRun{
			Run: run.Run, Initial: protocolstate.Sample{Key: initialKey, State: initialState},
			Decisions: make([]protocolstate.MeasuredDecision, 0, len(run.Trace)),
		}
		for ordinal, record := range run.Trace {
			decision := protocolstate.MeasuredDecision{Step: record.Step}
			if projector.IsSample(record) {
				state, key, err := projector.Project(record.After)
				if err != nil {
					return ExperimentSummary{}, fmt.Errorf(
						"project run %d decision %d at trace step %d: %w",
						run.Run, ordinal+1, record.Step, err,
					)
				}
				decision.Sample = &protocolstate.Sample{Step: record.Step, Key: key, State: state}
			}
			current.Decisions = append(current.Decisions, decision)
		}
		converted = append(converted, current)
	}
	return protocolstate.Aggregate(projector.ID(), converted)
}

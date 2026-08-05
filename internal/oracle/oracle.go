package oracle

import (
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

type Violation struct {
	Monitor string `json:"monitor"`
	Step    int    `json:"step,omitempty"`
	Message string `json:"message"`
}

type Result struct {
	Checked    []string    `json:"checked"`
	Violations []Violation `json:"violations"`
}

type Monitor interface {
	Name() string
	Check([]core.TraceRecord) []Violation
}

func Check(trace []core.TraceRecord, monitors ...Monitor) Result {
	result := Result{}
	for _, monitor := range monitors {
		result.Checked = append(result.Checked, monitor.Name())
		result.Violations = append(result.Violations, monitor.Check(trace)...)
	}
	return result
}

type Agreement struct{}

func (Agreement) Name() string { return "agreement" }

func (Agreement) Check(trace []core.TraceRecord) []Violation {
	committed := ""
	for _, record := range trace {
		for _, observation := range record.Observations {
			if observation.Kind != "commit" {
				continue
			}
			if committed == "" {
				committed = observation.Value
				continue
			}
			if committed != observation.Value {
				return []Violation{{
					Monitor: "agreement",
					Step:    record.Step,
					Message: fmt.Sprintf("observed commits for %q and %q", committed, observation.Value),
				}}
			}
		}
	}
	return nil
}

type TraceIntegrity struct{}

func (TraceIntegrity) Name() string { return "trace-integrity" }

func (TraceIntegrity) Check(trace []core.TraceRecord) []Violation {
	succeeded := make(map[string]bool, len(trace))
	var violations []Violation
	for index, record := range trace {
		if record.Step != index+1 {
			violations = append(violations, Violation{
				Monitor: "trace-integrity", Step: record.Step, Message: "non-contiguous trace step",
			})
		}
		if _, seen := succeeded[record.Event.ID]; seen {
			violations = append(violations, Violation{
				Monitor: "trace-integrity", Step: record.Step, Message: "event executed more than once",
			})
		}
		for _, dependency := range record.Event.Dependencies {
			if !succeeded[dependency] {
				violations = append(violations, Violation{
					Monitor: "trace-integrity", Step: record.Step,
					Message: fmt.Sprintf("dependency %s did not succeed first", dependency),
				})
			}
		}
		succeeded[record.Event.ID] = record.Outcome == string(core.StatusApplied)
	}
	return violations
}

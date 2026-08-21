package oracle

import (
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type BundleMonitor interface {
	Name() string
	CheckBundle(controlexperiment.ExecutionBundle) []Violation
}

func CheckBundle(bundle controlexperiment.ExecutionBundle, monitors ...BundleMonitor) Result {
	result := Result{}
	for _, monitor := range monitors {
		result.Checked = append(result.Checked, monitor.Name())
		result.Violations = append(result.Violations, monitor.CheckBundle(bundle)...)
	}
	return result
}

// BundleAgreement checks exact, projector-owned decision positions and value
// digests. PSS discovery states and Coverage never enter this verdict.
type BundleAgreement struct{}

func (BundleAgreement) Name() string { return "agreement" }

func (BundleAgreement) CheckBundle(bundle controlexperiment.ExecutionBundle) []Violation {
	type accepted struct {
		participant string
		digest      string
	}
	values := make(map[string]accepted)
	for _, observation := range bundle.Decisions.Observations {
		previous, exists := values[observation.Position]
		if !exists {
			values[observation.Position] = accepted{
				participant: string(observation.Participant), digest: observation.ValueDigest,
			}
			continue
		}
		if previous.digest != observation.ValueDigest {
			return []Violation{{
				Monitor: "agreement", Step: observation.Step,
				Message: fmt.Sprintf(
					"conflicting applied values at position %s: %s/%s and %s/%s",
					observation.Position, previous.participant, previous.digest,
					observation.Participant, observation.ValueDigest,
				),
			}}
		}
	}
	return nil
}

// BundleTraceIntegrity checks the v2 state-digest chain and every embedded
// Evidence/entropy binding. A failure invalidates a trial and never earns a
// defect kill.
type BundleTraceIntegrity struct{}

func (BundleTraceIntegrity) Name() string { return "trace-integrity" }

func (BundleTraceIntegrity) CheckBundle(bundle controlexperiment.ExecutionBundle) []Violation {
	if err := bundle.ValidateTraceIntegrity(); err != nil {
		return traceViolation(0, err.Error())
	}
	return nil
}

func traceViolation(step int, message string) []Violation {
	return []Violation{{Monitor: "trace-integrity", Step: step, Message: message}}
}

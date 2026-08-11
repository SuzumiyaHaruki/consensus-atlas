package oracle

import (
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
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
	trace := bundle.Trace
	if err := trace.Validate(); err != nil {
		return traceViolation(0, err.Error())
	}
	initialEvidence, err := control.CanonicalDigest(trace.InitialEvidence)
	if err != nil || initialEvidence != trace.InitialEvidenceDigest {
		return traceViolation(0, "initial evidence digest mismatch")
	}
	if err := trace.InitialEntropy.Tape.Validate(); err != nil ||
		trace.InitialEntropy.TapeDigest != trace.InitialEntropyDigest {
		return traceViolation(0, "initial entropy digest mismatch")
	}
	previous := trace.InitialStateDigest
	prepareIndex := 0
	for index, record := range trace.Records {
		step := index + 1
		for prepareIndex < len(bundle.Preparations) && bundle.Preparations[prepareIndex].BeforeDecision == step {
			prepare := bundle.Preparations[prepareIndex]
			if prepare.BeforeStateDigest != previous {
				return traceViolation(step, "broken state digest chain before preparation")
			}
			previous = prepare.AfterStateDigest
			prepareIndex++
		}
		if record.Step != uint64(step) {
			return traceViolation(step, "non-contiguous trace step")
		}
		if record.BeforeStateDigest != previous || record.AfterStateDigest == "" {
			return traceViolation(step, "broken state digest chain")
		}
		if prepareIndex > 0 && bundle.Preparations[prepareIndex-1].BeforeDecision == step &&
			bundle.Preparations[prepareIndex-1].Action.ID != record.Action.ID {
			return traceViolation(step, "prepared Action was not selected immediately")
		}
		if record.Outcome != "applied" {
			return traceViolation(step, "non-applied Runtime outcome")
		}
		if err := record.Action.Kind.Validate(); err != nil {
			return traceViolation(step, err.Error())
		}
		if record.Command != nil && (record.Command.Action != record.Action.ID ||
			record.Command.Kind != record.Action.Kind || record.Command.Node != record.Action.Node ||
			record.Command.Item != record.Action.Item) {
			return traceViolation(step, "Adapter command does not bind selected Action")
		}
		if record.Evidence == nil {
			if record.EvidenceDigest != "" {
				return traceViolation(step, "evidence digest without evidence")
			}
		} else {
			digest, digestErr := control.CanonicalDigest(*record.Evidence)
			if digestErr != nil || digest != record.EvidenceDigest {
				return traceViolation(step, "evidence digest mismatch")
			}
		}
		if record.Entropy == nil {
			if record.EntropyTapeDigest != "" {
				return traceViolation(step, "entropy digest without audit envelope")
			}
		} else if err := record.Entropy.Tape.Validate(); err != nil ||
			record.EntropyTapeDigest != record.Entropy.TapeDigest {
			return traceViolation(step, "entropy tape digest mismatch")
		}
		previous = record.AfterStateDigest
	}
	if prepareIndex != len(bundle.Preparations) {
		return traceViolation(len(trace.Records), "unconsumed preparation record")
	}
	if previous != trace.FinalStateDigest {
		return traceViolation(len(trace.Records), "final state digest mismatch")
	}
	finalDigest, err := bundle.FinalSnapshot.Digest()
	if err != nil || finalDigest != trace.FinalStateDigest {
		return traceViolation(len(trace.Records), "final snapshot is not trace-bound")
	}
	return nil
}

func traceViolation(step int, message string) []Violation {
	return []Violation{{Monitor: "trace-integrity", Step: step, Message: message}}
}

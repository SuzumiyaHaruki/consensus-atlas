package raft

import (
	"fmt"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

// ReadyMustSync checks the native Ready contract at a Driver observation
// boundary. A synchronous write is justified by new entries or a non-empty
// HardState carrying a term/vote update. An empty HardState and no entries
// therefore cannot justify MustSync=true; this is the historical condition
// fixed by etcd/raft commit 0675f3d.
//
// The monitor deliberately does not infer storage behavior from a plan or a
// host sync operation. It checks frozen Ready evidence emitted by the Driver.
type ReadyMustSync struct{}

func (ReadyMustSync) Name() string { return "ready-must-sync" }

func (ReadyMustSync) ValidateEvidence(trace []core.TraceRecord) error {
	for _, record := range trace {
		for _, observation := range record.Observations {
			if observation.Kind != "ready" || observation.Label != "ready:must-sync" {
				continue
			}
			if _, _, _, err := decodeMustSyncObservation(observation); err != nil {
				return fmt.Errorf("%w at trace step %d: %s", oracle.ErrMalformedEvidence, record.Step, err)
			}
		}
	}
	return nil
}

func (ReadyMustSync) Check(trace []core.TraceRecord) []oracle.Violation {
	var violations []oracle.Violation
	for _, record := range trace {
		for _, observation := range record.Observations {
			if observation.Kind != "ready" || observation.Label != "ready:must-sync" {
				continue
			}
			mustSync, entries, hardStateEmpty, err := decodeMustSyncObservation(observation)
			if err != nil {
				continue
			}
			if mustSync && entries == 0 && hardStateEmpty {
				violations = append(violations, readyMustSyncViolation(record.Step,
					"Ready.MustSync is true with no Entries and an empty HardState"))
			}
		}
	}
	return violations
}

func decodeMustSyncObservation(observation core.Observation) (bool, int, bool, error) {
	if observation.Evidence == nil {
		return false, 0, false, fmt.Errorf("ready MustSync observation has no evidence")
	}
	mustSync, err := strconv.ParseBool(observation.Evidence["must_sync"])
	if err != nil {
		return false, 0, false, fmt.Errorf("ready MustSync observation has invalid must_sync")
	}
	if observation.Value != "" && observation.Value != strconv.FormatBool(mustSync) {
		return false, 0, false, fmt.Errorf("ready MustSync observation value disagrees with evidence")
	}
	entries, err := strconv.Atoi(observation.Evidence["entries"])
	if err != nil || entries < 0 {
		return false, 0, false, fmt.Errorf("ready MustSync observation has invalid entry count")
	}
	hardStateEmpty, err := strconv.ParseBool(observation.Evidence["hard_state_empty"])
	if err != nil {
		return false, 0, false, fmt.Errorf("ready MustSync observation has invalid hard-state marker")
	}
	return mustSync, entries, hardStateEmpty, nil
}

func readyMustSyncViolation(step int, message string) oracle.Violation {
	return oracle.Violation{Monitor: "ready-must-sync", Step: step, Message: message}
}

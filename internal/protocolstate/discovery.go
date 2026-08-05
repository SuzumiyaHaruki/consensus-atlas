package protocolstate

import (
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

// Projector is the protocol-family boundary for state-discovery metrics. The
// ledger owns sampling and witness accounting; a Family Pack owns the semantic
// projection and decides which implementation boundaries are meaningful.
type Projector interface {
	ID() string
	IsSample(core.TraceRecord) bool
	Project(snapshot any) (state any, key string, err error)
}

type StateWitness struct {
	Key       string `json:"key"`
	FirstStep int    `json:"first_step"`
	State     any    `json:"state"`
}

type DiscoveryPoint struct {
	Step         int  `json:"step"`
	Samples      int  `json:"samples"`
	UniqueStates int  `json:"unique_states"`
	NewState     bool `json:"new_state"`
}

type DiscoverySummary struct {
	PSSID        string           `json:"pss_id"`
	Samples      int              `json:"samples"`
	UniqueStates int              `json:"unique_states"`
	Curve        []DiscoveryPoint `json:"curve"`
	States       []StateWitness   `json:"states"`
}

func Discover(trace []core.TraceRecord, projector Projector) (DiscoverySummary, error) {
	if projector == nil {
		return DiscoverySummary{}, fmt.Errorf("protocol state projector is nil")
	}
	summary := DiscoverySummary{PSSID: projector.ID()}
	seen := make(map[string]bool)
	for _, record := range trace {
		if !projector.IsSample(record) {
			continue
		}
		state, key, err := projector.Project(record.After)
		if err != nil {
			return DiscoverySummary{}, fmt.Errorf("project protocol state at step %d: %w", record.Step, err)
		}
		if key == "" {
			return DiscoverySummary{}, fmt.Errorf("project protocol state at step %d: empty key", record.Step)
		}
		summary.Samples++
		isNew := !seen[key]
		if isNew {
			seen[key] = true
			summary.States = append(summary.States, StateWitness{Key: key, FirstStep: record.Step, State: state})
		}
		summary.Curve = append(summary.Curve, DiscoveryPoint{
			Step: record.Step, Samples: summary.Samples, UniqueStates: len(seen), NewState: isNew,
		})
	}
	summary.UniqueStates = len(seen)
	return summary, nil
}

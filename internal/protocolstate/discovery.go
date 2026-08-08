package protocolstate

import "fmt"

// Sample is a protocol-neutral ledger input. Semantic mappings own boundary
// selection and projection; the ledger owns stable keys and first witnesses.
type Sample struct {
	Step  int    `json:"step"`
	Key   string `json:"key"`
	State any    `json:"state"`
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

func Discover(pssID string, samples []Sample) (DiscoverySummary, error) {
	if pssID == "" {
		return DiscoverySummary{}, fmt.Errorf("protocol state PSS ID is empty")
	}
	summary := DiscoverySummary{PSSID: pssID}
	seen := make(map[string]bool)
	lastStep := 0
	for index, sample := range samples {
		if sample.Step < 0 || index > 0 && sample.Step <= lastStep {
			return DiscoverySummary{}, fmt.Errorf("protocol state step %d is not strictly increasing", sample.Step)
		}
		lastStep = sample.Step
		if sample.Key == "" {
			return DiscoverySummary{}, fmt.Errorf("record protocol state at step %d: empty key", sample.Step)
		}
		summary.Samples++
		isNew := !seen[sample.Key]
		if isNew {
			seen[sample.Key] = true
			summary.States = append(summary.States, StateWitness{Key: sample.Key, FirstStep: sample.Step, State: sample.State})
		}
		summary.Curve = append(summary.Curve, DiscoveryPoint{
			Step: sample.Step, Samples: summary.Samples, UniqueStates: len(seen), NewState: isNew,
		})
	}
	summary.UniqueStates = len(seen)
	return summary, nil
}

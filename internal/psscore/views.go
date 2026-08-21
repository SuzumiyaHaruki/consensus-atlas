package psscore

import (
	"encoding/json"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

// ViewKeys separates protocol progress from scheduler/control diversity while
// retaining the original joint Core PSS identity for compatibility.
type ViewKeys struct {
	Protocol string
	Control  string
	Joint    string
}

// ViewSummary is a compact, derived measurement. The underlying State remains
// the evidence; these counts do not participate in scheduling or verdicts.
type ViewSummary struct {
	Samples        int `json:"samples"`
	ProtocolStates int `json:"protocol_states"`
	ControlStates  int `json:"control_states"`
	JointStates    int `json:"joint_states"`
}

type protocolView struct {
	MappingID string        `json:"mapping_id"`
	Semantic  SemanticGraph `json:"semantic"`
}

// Keys independently canonicalizes the semantic graph and Control Context.
// This prevents a pending queue shape from choosing participant aliases for
// the protocol view (and vice versa).
func Keys(state State) (ViewKeys, error) {
	if err := state.Validate(); err != nil {
		return ViewKeys{}, err
	}
	ids := make([]string, len(state.Control.Participants))
	for index, participant := range state.Control.Participants {
		ids[index] = participant.ID
	}
	var bestProtocol protocolView
	var bestControl ControlContext
	var bestProtocolJSON, bestControlJSON []byte
	visitPermutations(ids, func(order []string) {
		aliases := make(map[string]string, len(order))
		for index, id := range order {
			aliases[id] = fmt.Sprintf("n%d", index+1)
		}
		candidate := normalizeValues(remapParticipants(state, aliases))
		sortState(&candidate)
		protocol := protocolView{MappingID: candidate.MappingID, Semantic: candidate.Semantic}
		protocolJSON, protocolErr := json.Marshal(protocol)
		if protocolErr == nil && (bestProtocolJSON == nil || string(protocolJSON) < string(bestProtocolJSON)) {
			bestProtocol, bestProtocolJSON = protocol, protocolJSON
		}
		controlJSON, controlErr := json.Marshal(candidate.Control)
		if controlErr == nil && (bestControlJSON == nil || string(controlJSON) < string(bestControlJSON)) {
			bestControl, bestControlJSON = candidate.Control, controlJSON
		}
	})
	if bestProtocolJSON == nil || bestControlJSON == nil {
		return ViewKeys{}, fmt.Errorf("CORE_PSS_VIEW_CANONICALIZATION_FAILED")
	}
	protocolKey, err := control.CanonicalDigest(bestProtocol)
	if err != nil {
		return ViewKeys{}, err
	}
	controlKey, err := control.CanonicalDigest(bestControl)
	if err != nil {
		return ViewKeys{}, err
	}
	return ViewKeys{Protocol: protocolKey, Control: controlKey, Joint: state.Digest}, nil
}

func Summarize(samples []protocolstate.Sample) (ViewSummary, error) {
	states := make([]State, 0, len(samples))
	lastStep := -1
	for _, sample := range samples {
		state, ok := sample.State.(State)
		if !ok || sample.Step < 0 || sample.Step <= lastStep || sample.Key != state.Digest {
			return ViewSummary{}, fmt.Errorf("CORE_PSS_VIEW_SAMPLE_INVALID: %d", sample.Step)
		}
		states = append(states, state)
		lastStep = sample.Step
	}
	return SummarizeStates(states)
}

func SummarizeStates(states []State) (ViewSummary, error) {
	summary := ViewSummary{Samples: len(states)}
	protocolStates := make(map[string]bool, len(states))
	controlStates := make(map[string]bool, len(states))
	jointStates := make(map[string]bool, len(states))
	for _, state := range states {
		keys, err := Keys(state)
		if err != nil {
			return ViewSummary{}, err
		}
		protocolStates[keys.Protocol] = true
		controlStates[keys.Control] = true
		jointStates[keys.Joint] = true
	}
	summary.ProtocolStates = len(protocolStates)
	summary.ControlStates = len(controlStates)
	summary.JointStates = len(jointStates)
	return summary, nil
}

func (summary ViewSummary) Validate() error {
	if summary.Samples <= 0 || summary.ProtocolStates <= 0 || summary.ControlStates <= 0 ||
		summary.JointStates <= 0 || summary.ProtocolStates > summary.Samples ||
		summary.ControlStates > summary.Samples || summary.JointStates > summary.Samples {
		return fmt.Errorf("CORE_PSS_VIEW_SUMMARY_INVALID")
	}
	return nil
}

package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

// DecisionObservation is the smallest protocol-neutral input accepted by the
// Agreement monitor. Position and ValueDigest retain exact target semantics;
// the trusted target Binding owns their projection from opaque Evidence.
type DecisionObservation struct {
	Step        int            `json:"step"`
	Participant control.NodeID `json:"participant"`
	Position    string         `json:"position"`
	ValueDigest string         `json:"value_digest"`
}

func (observation DecisionObservation) Validate() error {
	decoded, err := hex.DecodeString(observation.ValueDigest)
	if observation.Step < 0 || observation.Participant == "" || observation.Position == "" ||
		err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != observation.ValueDigest {
		return fmt.Errorf("DECISION_OBSERVATION_INVALID: %d/%s/%s", observation.Step,
			observation.Participant, observation.Position)
	}
	return nil
}

// DecisionProjector is trusted target composition. Generic execution and
// Oracle packages never decode an Adapter-specific Evidence payload.
type DecisionProjector interface {
	ID() string
	Project(control.EvidenceEnvelope) ([]DecisionObservation, error)
}

func NormalizeDecisionObservations(values []DecisionObservation) ([]DecisionObservation, error) {
	result := append([]DecisionObservation(nil), values...)
	for _, observation := range result {
		if err := observation.Validate(); err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Step != result[j].Step {
			return result[i].Step < result[j].Step
		}
		if result[i].Position != result[j].Position {
			return result[i].Position < result[j].Position
		}
		if result[i].Participant != result[j].Participant {
			return result[i].Participant < result[j].Participant
		}
		return result[i].ValueDigest < result[j].ValueDigest
	})
	for index := 1; index < len(result); index++ {
		if result[index] == result[index-1] {
			return nil, fmt.Errorf("DECISION_OBSERVATION_DUPLICATE: %d/%s/%s", result[index].Step,
				result[index].Participant, result[index].Position)
		}
	}
	return result, nil
}

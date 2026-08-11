package omnipaxosv2

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const DecisionProjectionID = "omnipaxos-v2/decided-prefix-digest-v1"

type DecisionProjector struct{}

func (DecisionProjector) ID() string { return DecisionProjectionID }

// Project exposes only the exact decided frontier and cumulative prefix
// digest owned by this Binding. The generic Agreement monitor remains unaware
// of OmniPaxos ballots, log entries, and storage types.
func (DecisionProjector) Project(envelope control.EvidenceEnvelope) ([]semantic.DecisionObservation, error) {
	snapshot, err := decodeEvidence(envelope)
	if err != nil {
		return nil, err
	}
	result := make([]semantic.DecisionObservation, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if !validDigest(node.DecidedPrefixDigest) {
			return nil, errors.New("OMNIPAXOS_DECIDED_PREFIX_DIGEST_INVALID")
		}
		if node.DecidedIndex == 0 {
			continue
		}
		observation := semantic.DecisionObservation{
			Participant: nodeNames[node.ID],
			Position:    strconv.FormatUint(node.DecidedIndex, 10),
			ValueDigest: node.DecidedPrefixDigest,
		}
		if err := observation.Validate(); err != nil {
			return nil, err
		}
		result = append(result, observation)
	}
	return result, nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

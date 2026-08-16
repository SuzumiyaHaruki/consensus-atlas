package omnipaxosv2

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const DecisionProjectionID = "omnipaxos-v2/decided-prefix-chain-v2"

type DecisionProjector struct{}

func (DecisionProjector) ID() string { return DecisionProjectionID }

// Project exposes one cumulative commitment for every decided position. This
// lets the generic Agreement monitor compare a shared position even when two
// participants currently have different decided frontiers.
func (DecisionProjector) Project(envelope control.EvidenceEnvelope) ([]semantic.DecisionObservation, error) {
	snapshot, err := decodeEvidence(envelope)
	if err != nil {
		return nil, err
	}
	result := make([]semantic.DecisionObservation, 0)
	for _, node := range snapshot.Nodes {
		if !validDigest(node.DecidedPrefixDigest) {
			return nil, errors.New("OMNIPAXOS_DECIDED_PREFIX_DIGEST_INVALID")
		}
		if len(node.DecidedPrefixes) != int(node.DecidedIndex) {
			return nil, errors.New("OMNIPAXOS_DECIDED_PREFIX_COUNT_INVALID")
		}
		for index, prefix := range node.DecidedPrefixes {
			if prefix.Index != uint64(index+1) || !validDigest(prefix.Digest) {
				return nil, errors.New("OMNIPAXOS_DECIDED_PREFIX_INVALID")
			}
			observation := semantic.DecisionObservation{
				Participant: nodeNames[node.ID],
				Position:    strconv.FormatUint(prefix.Index, 10),
				ValueDigest: prefix.Digest,
			}
			if err := observation.Validate(); err != nil {
				return nil, err
			}
			result = append(result, observation)
		}
		if len(node.DecidedPrefixes) > 0 &&
			node.DecidedPrefixes[len(node.DecidedPrefixes)-1].Digest != node.DecidedPrefixDigest {
			return nil, errors.New("OMNIPAXOS_DECIDED_PREFIX_FINAL_MISMATCH")
		}
	}
	return result, nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

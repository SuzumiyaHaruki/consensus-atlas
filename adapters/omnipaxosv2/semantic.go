package omnipaxosv2

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const CorePSSMappingID = "omnipaxos-v2/core-pss-v1"

type CorePSSMapper struct{}

func (CorePSSMapper) ID() string { return CorePSSMappingID }

func (CorePSSMapper) Map(envelope control.EvidenceEnvelope) (psscore.SemanticObservation, error) {
	snapshot, err := decodeEvidence(envelope)
	if err != nil {
		return psscore.SemanticObservation{}, err
	}
	promises := make([]ballotKey, 0, len(snapshot.Nodes))
	decisions := make([]uint64, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		promises = append(promises, ballotOf(node))
		if node.DecidedIndex > 0 {
			decisions = append(decisions, node.DecidedIndex)
		}
	}
	promiseRanks, orderedPromises := rankBallots(promises)
	decisionRanks, orderedDecisions := rankUint64(decisions)
	graph := psscore.SemanticGraph{}
	for _, node := range snapshot.Nodes {
		mode := psscore.ModePassive
		if node.Leader == node.ID {
			mode = psscore.ModeCoordinating
		}
		graph.Entities = append(graph.Entities, psscore.Entity{
			ID: string(nodeNames[node.ID]), Kind: psscore.EntityParticipant, Mode: mode,
		})
	}
	for index := range orderedPromises {
		graph.Entities = append(graph.Entities, psscore.Entity{
			ID: epochID(index), Kind: psscore.EntityEpoch, Stage: psscore.StageUnknown,
		})
		if index > 0 {
			graph.Relations = append(graph.Relations, psscore.Relation{
				Kind: psscore.RelationPrecedes, From: epochID(index - 1), To: epochID(index),
			})
		}
	}
	for index := range orderedDecisions {
		graph.Entities = append(graph.Entities, psscore.Entity{
			ID: decisionID(index), Kind: psscore.EntityDecision, Stage: psscore.StageDecided,
		})
		if index > 0 {
			graph.Relations = append(graph.Relations, psscore.Relation{
				Kind: psscore.RelationPrecedes, From: decisionID(index - 1), To: decisionID(index),
			})
		}
	}
	for _, node := range snapshot.Nodes {
		participant := string(nodeNames[node.ID])
		graph.Relations = append(graph.Relations, psscore.Relation{
			Kind: psscore.RelationBelongsTo, From: participant,
			To: epochID(promiseRanks[ballotOf(node)]),
		})
		if node.DecidedIndex > 0 {
			graph.Relations = append(graph.Relations, psscore.Relation{
				Kind: psscore.RelationDecides, From: participant,
				To: decisionID(decisionRanks[node.DecidedIndex]),
			})
		}
	}
	return psscore.SemanticObservation{LogicalTime: snapshot.LogicalTime, Graph: graph}, nil
}

type ballotKey struct {
	Number   uint32
	Priority uint32
	PID      uint64
}

func ballotOf(node workerNode) ballotKey {
	return ballotKey{node.PromiseNumber, node.PromisePriority, node.PromisePID}
}

func rankBallots(values []ballotKey) (map[ballotKey]int, []ballotKey) {
	ordered := append([]ballotKey(nil), values...)
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.Number != right.Number {
			return left.Number < right.Number
		}
		if left.Priority != right.Priority {
			return left.Priority < right.Priority
		}
		return left.PID < right.PID
	})
	unique := ordered[:0]
	for _, value := range ordered {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	ranks := make(map[ballotKey]int, len(unique))
	for index, value := range unique {
		ranks[value] = index
	}
	return ranks, unique
}

func decodeEvidence(envelope control.EvidenceEnvelope) (adapterSnapshot, error) {
	if err := envelope.Payload.Validate(); err != nil {
		return adapterSnapshot{}, err
	}
	if envelope.Payload.SchemaVersion != evidenceSchema || envelope.Payload.Encoding != "json" {
		return adapterSnapshot{}, errors.New("OMNIPAXOS_EVIDENCE_SCHEMA_MISMATCH")
	}
	var snapshot adapterSnapshot
	if err := json.Unmarshal(envelope.Payload.Bytes, &snapshot); err != nil {
		return adapterSnapshot{}, err
	}
	if len(snapshot.Nodes) != 3 {
		return adapterSnapshot{}, errors.New("OMNIPAXOS_EVIDENCE_NODE_SET_INVALID")
	}
	for index, node := range snapshot.Nodes {
		if node.ID != uint64(index+1) || nodeNames[node.ID] == "" || node.Leader > 3 || node.PromisePID > 3 {
			return adapterSnapshot{}, fmt.Errorf("OMNIPAXOS_EVIDENCE_NODE_INVALID:%d", node.ID)
		}
	}
	return snapshot, nil
}

func rankUint64(values []uint64) (map[uint64]int, []uint64) {
	ordered := append([]uint64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	unique := ordered[:0]
	for _, value := range ordered {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	ranks := make(map[uint64]int, len(unique))
	for index, value := range unique {
		ranks[value] = index
	}
	return ranks, unique
}

func epochID(rank int) string    { return fmt.Sprintf("e%d", rank+1) }
func decisionID(rank int) string { return fmt.Sprintf("d%d", rank+1) }

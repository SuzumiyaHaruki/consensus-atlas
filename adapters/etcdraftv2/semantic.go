package etcdraftv2

import (
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const CorePSSMappingID = "official-etcdraft-v2/core-pss-v1"

type CorePSSMapper struct{}

func (CorePSSMapper) ID() string { return CorePSSMappingID }

func (CorePSSMapper) Map(envelope control.EvidenceEnvelope) (psscore.SemanticObservation, error) {
	return MapCoreEvidence(envelope)
}

// MapCoreEvidence translates the stable public Evidence projection into the
// fixed Core PSS vocabulary. Absolute terms and log indexes are used only to
// derive relative ranks. Aggregate application digests are deliberately not
// presented as per-DecisionUnit Values.
func MapCoreEvidence(envelope control.EvidenceEnvelope) (psscore.SemanticObservation, error) {
	evidence, err := ProjectEvidence(envelope)
	if err != nil {
		return psscore.SemanticObservation{}, err
	}
	if len(evidence.Nodes) == 0 {
		return psscore.SemanticObservation{}, fmt.Errorf("ETCDRAFT_V2_CORE_PSS_NODES_REQUIRED")
	}
	terms := make([]uint64, 0, len(evidence.Nodes))
	frontiers := make([]uint64, 0, len(evidence.Nodes)*2)
	var maxApplied uint64
	for _, node := range evidence.Nodes {
		if node.Applied > node.Commit {
			return psscore.SemanticObservation{}, fmt.Errorf("ETCDRAFT_V2_CORE_PSS_FRONTIER_INVALID: %s", node.Node)
		}
		if _, err := coreParticipantMode(node); err != nil {
			return psscore.SemanticObservation{}, err
		}
		terms = append(terms, node.Term)
		if node.Commit > 0 {
			frontiers = append(frontiers, node.Commit)
		}
		if node.Applied > 0 {
			frontiers = append(frontiers, node.Applied)
		}
		if node.Applied > maxApplied {
			maxApplied = node.Applied
		}
	}
	termRank, orderedTerms := rankUint64(terms)
	frontierRank, orderedFrontiers := rankUint64(frontiers)
	graph := psscore.SemanticGraph{}
	for _, node := range evidence.Nodes {
		mode, _ := coreParticipantMode(node)
		graph.Entities = append(graph.Entities, psscore.Entity{
			ID: string(node.Node), Kind: psscore.EntityParticipant, Mode: mode,
		})
	}
	for index := range orderedTerms {
		graph.Entities = append(graph.Entities, psscore.Entity{
			ID: epochID(index), Kind: psscore.EntityEpoch, Stage: psscore.StageUnknown,
		})
		if index > 0 {
			graph.Relations = append(graph.Relations, psscore.Relation{
				Kind: psscore.RelationPrecedes, From: epochID(index - 1), To: epochID(index),
			})
		}
	}
	for index, frontier := range orderedFrontiers {
		stage := psscore.StageDecided
		if frontier <= maxApplied {
			stage = psscore.StageApplied
		}
		graph.Entities = append(graph.Entities, psscore.Entity{
			ID: decisionID(index), Kind: psscore.EntityDecision, Stage: stage,
		})
		if index > 0 {
			graph.Relations = append(graph.Relations, psscore.Relation{
				Kind: psscore.RelationPrecedes, From: decisionID(index - 1), To: decisionID(index),
			})
		}
	}
	relations := make(map[string]bool)
	addRelation := func(kind psscore.RelationKind, from, to string) {
		key := string(kind) + "\x00" + from + "\x00" + to
		if !relations[key] {
			graph.Relations = append(graph.Relations, psscore.Relation{Kind: kind, From: from, To: to})
			relations[key] = true
		}
	}
	for _, relation := range graph.Relations {
		relations[string(relation.Kind)+"\x00"+relation.From+"\x00"+relation.To] = true
	}
	for _, node := range evidence.Nodes {
		participant := string(node.Node)
		addRelation(psscore.RelationBelongsTo, participant, epochID(termRank[node.Term]))
		if node.Commit > 0 {
			addRelation(psscore.RelationDecides, participant, decisionID(frontierRank[node.Commit]))
		}
		if node.Applied == 0 {
			continue
		}
		unit := decisionID(frontierRank[node.Applied])
		addRelation(psscore.RelationApplies, participant, unit)
	}
	return psscore.SemanticObservation{LogicalTime: evidence.LogicalTime, Graph: graph}, nil
}

func coreParticipantMode(node NodeEvidence) (psscore.ParticipantMode, error) {
	var mode psscore.ParticipantMode
	switch node.Role {
	case "StateStopped":
		mode = psscore.ModeInactive
	case "StateFollower":
		mode = psscore.ModePassive
	case "StatePreCandidate", "StateCandidate":
		mode = psscore.ModeContending
	case "StateLeader":
		mode = psscore.ModeCoordinating
	default:
		return "", fmt.Errorf("ETCDRAFT_V2_CORE_PSS_ROLE_UNSUPPORTED: %s", node.Role)
	}
	if node.Running == (mode == psscore.ModeInactive) {
		return "", fmt.Errorf("ETCDRAFT_V2_CORE_PSS_RUNNING_ROLE_MISMATCH: %s", node.Node)
	}
	return mode, nil
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

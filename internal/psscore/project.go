package psscore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

// Project combines trusted Runtime state with an Adapter-owned semantic graph.
// It canonicalizes participant names and Value identities before sealing the
// state. Epoch and DecisionUnit IDs must already be relative ranks supplied by
// the Semantic Mapping.
func Project(snapshot controlruntime.Snapshot, mappingID string, observation SemanticObservation) (State, error) {
	if snapshot.LogicalTime != observation.LogicalTime {
		return State{}, fmt.Errorf(
			"CORE_PSS_OBSERVATION_TIME_MISMATCH: runtime=%d observation=%d",
			snapshot.LogicalTime, observation.LogicalTime,
		)
	}
	context, err := projectControl(snapshot)
	if err != nil {
		return State{}, err
	}
	raw := State{SchemaVersion: SchemaVersion, MappingID: mappingID, Control: context, Semantic: observation.Graph}
	if err := validateState(raw); err != nil {
		return State{}, err
	}
	ids := make([]string, len(context.Participants))
	for index, participant := range context.Participants {
		ids[index] = participant.ID
	}
	var best State
	var bestBytes []byte
	visitPermutations(ids, func(order []string) {
		aliases := make(map[string]string, len(order))
		for index, id := range order {
			aliases[id] = fmt.Sprintf("n%d", index+1)
		}
		candidate := remapParticipants(raw, aliases)
		candidate = normalizeValues(candidate)
		sortState(&candidate)
		candidate.Digest = ""
		encoded, marshalErr := json.Marshal(candidate)
		if marshalErr == nil && (bestBytes == nil || string(encoded) < string(bestBytes)) {
			best, bestBytes = candidate, encoded
		}
	})
	if bestBytes == nil {
		return State{}, fmt.Errorf("CORE_PSS_CANONICALIZATION_FAILED")
	}
	digest, err := control.CanonicalDigest(best)
	if err != nil {
		return State{}, err
	}
	best.Digest = digest
	if err := validateState(best); err != nil {
		return State{}, err
	}
	return best, nil
}

// Validate checks the closed vocabulary and the digest. Project is the normal
// constructor; Validate does not accept an unsealed or reordered state.
func (state State) Validate() error {
	if err := validateState(state); err != nil {
		return err
	}
	got := state.Digest
	state.Digest = ""
	want, err := control.CanonicalDigest(state)
	if err != nil {
		return err
	}
	if got == "" || got != want {
		return fmt.Errorf("CORE_PSS_DIGEST_MISMATCH")
	}
	return nil
}

func projectControl(snapshot controlruntime.Snapshot) (ControlContext, error) {
	result := ControlContext{}
	nodes := make(map[control.NodeID]controlruntime.NodeSnapshot, len(snapshot.Nodes))
	incarnations := make([]uint64, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if err := node.Ref.Validate(); err != nil {
			return ControlContext{}, err
		}
		if _, exists := nodes[node.Ref.Node]; exists {
			return ControlContext{}, fmt.Errorf("CORE_PSS_RUNTIME_NODE_DUPLICATE: %s", node.Ref.Node)
		}
		nodes[node.Ref.Node] = node
		incarnations = append(incarnations, node.Ref.Incarnation)
	}
	ranks := relativeRanks(incarnations)
	for _, node := range snapshot.Nodes {
		result.Participants = append(result.Participants, Participant{
			ID: string(node.Ref.Node), Lifecycle: node.Lifecycle,
			IncarnationRank: ranks[node.Ref.Incarnation],
		})
	}
	blocked := make(map[string]BlockedLink)
	for _, partition := range snapshot.Partitions {
		for _, left := range partition.Left {
			for _, right := range partition.Right {
				if nodes[left].Ref.Node == "" || nodes[right].Ref.Node == "" || left == right {
					return ControlContext{}, fmt.Errorf("CORE_PSS_RUNTIME_PARTITION_INVALID: %s/%s", left, right)
				}
				for _, link := range []BlockedLink{{From: string(left), To: string(right)}, {From: string(right), To: string(left)}} {
					blocked[link.From+"\x00"+link.To] = link
				}
			}
		}
	}
	for _, link := range blocked {
		result.BlockedLinks = append(result.BlockedLinks, link)
	}
	items := make(map[control.ItemID]controlruntime.ItemSnapshot, len(snapshot.Items))
	var earliest *uint64
	for _, item := range snapshot.Items {
		items[item.ID] = item
		if pendingState(item.State) && item.Kind == control.ItemTemporal && item.Value.Temporal != nil {
			deadline := item.Value.Temporal.Deadline
			if earliest == nil || deadline < *earliest {
				earliest = &deadline
			}
		}
	}
	for _, item := range snapshot.Items {
		if !pendingState(item.State) {
			continue
		}
		if nodes[item.Owner.Node].Ref.Node == "" {
			return ControlContext{}, fmt.Errorf("CORE_PSS_RUNTIME_ITEM_OWNER_UNKNOWN: %s", item.Owner.Node)
		}
		currentIncarnation := nodes[item.Owner.Node].Ref.Incarnation
		if item.Owner.Incarnation > currentIncarnation {
			return ControlContext{}, fmt.Errorf("CORE_PSS_RUNTIME_ITEM_OWNER_FROM_FUTURE: %s", item.ID)
		}
		pending := PendingItem{
			Kind: item.Kind, State: item.State, Owner: string(item.Owner.Node),
			OwnerPrior: item.Owner.Incarnation < currentIncarnation,
		}
		switch item.Kind {
		case control.ItemMessage:
			if item.Value.Message == nil || nodes[item.Value.Message.Target].Ref.Node == "" {
				return ControlContext{}, fmt.Errorf("CORE_PSS_RUNTIME_MESSAGE_INVALID: %s", item.ID)
			}
			pending.Target = string(item.Value.Message.Target)
		case control.ItemTemporal:
			if item.Value.Temporal == nil {
				return ControlContext{}, fmt.Errorf("CORE_PSS_RUNTIME_TEMPORAL_INVALID: %s", item.ID)
			}
			pending.TemporalKind = item.Value.Temporal.Kind
			pending.Earliest = earliest != nil && item.Value.Temporal.Deadline == *earliest
		case control.ItemEffect:
			if item.Value.Effect == nil {
				return ControlContext{}, fmt.Errorf("CORE_PSS_RUNTIME_EFFECT_INVALID: %s", item.ID)
			}
			pending.Durability = item.Value.Effect.Durability
		}
		for _, dependency := range item.Value.Dependencies {
			entry, ok := items[dependency]
			if !ok {
				return ControlContext{}, fmt.Errorf("CORE_PSS_RUNTIME_DEPENDENCY_UNKNOWN: %s", dependency)
			}
			pending.DependencyKinds = append(pending.DependencyKinds, entry.Kind)
		}
		sort.Slice(pending.DependencyKinds, func(i, j int) bool {
			return pending.DependencyKinds[i] < pending.DependencyKinds[j]
		})
		result.Pending = append(result.Pending, pending)
	}
	return result, nil
}

func pendingState(state control.ItemState) bool {
	switch state {
	case control.ItemProduced, control.ItemBlocked, control.ItemEnabled, control.ItemSelected:
		return true
	default:
		return false
	}
}

func relativeRanks(values []uint64) map[uint64]uint64 {
	sorted := append([]uint64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	result := make(map[uint64]uint64, len(sorted))
	for _, value := range sorted {
		if _, exists := result[value]; !exists {
			result[value] = uint64(len(result))
		}
	}
	return result
}

func visitPermutations(values []string, visit func([]string)) {
	values = append([]string(nil), values...)
	sort.Strings(values)
	var generate func(int)
	generate = func(index int) {
		if index == len(values) {
			visit(append([]string(nil), values...))
			return
		}
		for next := index; next < len(values); next++ {
			values[index], values[next] = values[next], values[index]
			generate(index + 1)
			values[index], values[next] = values[next], values[index]
		}
	}
	generate(0)
}

func remapParticipants(state State, aliases map[string]string) State {
	result := state
	result.Control.Participants = append([]Participant(nil), state.Control.Participants...)
	for index := range result.Control.Participants {
		result.Control.Participants[index].ID = aliases[result.Control.Participants[index].ID]
	}
	result.Control.BlockedLinks = append([]BlockedLink(nil), state.Control.BlockedLinks...)
	for index := range result.Control.BlockedLinks {
		result.Control.BlockedLinks[index].From = aliases[result.Control.BlockedLinks[index].From]
		result.Control.BlockedLinks[index].To = aliases[result.Control.BlockedLinks[index].To]
	}
	result.Control.Pending = append([]PendingItem(nil), state.Control.Pending...)
	for index := range result.Control.Pending {
		result.Control.Pending[index].Owner = aliases[result.Control.Pending[index].Owner]
		if result.Control.Pending[index].Target != "" {
			result.Control.Pending[index].Target = aliases[result.Control.Pending[index].Target]
		}
		result.Control.Pending[index].DependencyKinds = append([]control.ItemKind(nil), state.Control.Pending[index].DependencyKinds...)
	}
	result.Semantic.Entities = append([]Entity(nil), state.Semantic.Entities...)
	participantIDs := make(map[string]bool, len(aliases))
	for index := range result.Semantic.Entities {
		if result.Semantic.Entities[index].Kind == EntityParticipant {
			participantIDs[result.Semantic.Entities[index].ID] = true
			result.Semantic.Entities[index].ID = aliases[result.Semantic.Entities[index].ID]
		}
	}
	result.Semantic.Relations = append([]Relation(nil), state.Semantic.Relations...)
	for index := range result.Semantic.Relations {
		if participantIDs[result.Semantic.Relations[index].From] {
			result.Semantic.Relations[index].From = aliases[result.Semantic.Relations[index].From]
		}
		if participantIDs[result.Semantic.Relations[index].To] {
			result.Semantic.Relations[index].To = aliases[result.Semantic.Relations[index].To]
		}
	}
	return result
}

func normalizeValues(state State) State {
	valueIDs := make(map[string]Entity)
	for _, entity := range state.Semantic.Entities {
		if entity.Kind == EntityValue {
			valueIDs[entity.ID] = entity
		}
	}
	type valueSignature struct{ id, signature string }
	values := make([]valueSignature, 0, len(valueIDs))
	for id, entity := range valueIDs {
		parts := []string{string(entity.Stage)}
		for _, relation := range state.Semantic.Relations {
			switch {
			case relation.From == id:
				parts = append(parts, ">"+string(relation.Kind)+":"+relation.To)
			case relation.To == id:
				parts = append(parts, "<"+string(relation.Kind)+":"+relation.From)
			}
		}
		sort.Strings(parts)
		values = append(values, valueSignature{id: id, signature: strings.Join(parts, "|")})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].signature != values[j].signature {
			return values[i].signature < values[j].signature
		}
		return values[i].id < values[j].id
	})
	aliases := make(map[string]string, len(values))
	for index, value := range values {
		aliases[value.id] = fmt.Sprintf("v%d", index+1)
	}
	for index := range state.Semantic.Entities {
		if alias := aliases[state.Semantic.Entities[index].ID]; alias != "" {
			state.Semantic.Entities[index].ID = alias
		}
	}
	for index := range state.Semantic.Relations {
		if alias := aliases[state.Semantic.Relations[index].From]; alias != "" {
			state.Semantic.Relations[index].From = alias
		}
		if alias := aliases[state.Semantic.Relations[index].To]; alias != "" {
			state.Semantic.Relations[index].To = alias
		}
	}
	return state
}

func sortState(state *State) {
	sort.Slice(state.Control.Participants, func(i, j int) bool { return state.Control.Participants[i].ID < state.Control.Participants[j].ID })
	sort.Slice(state.Control.BlockedLinks, func(i, j int) bool {
		left, right := state.Control.BlockedLinks[i], state.Control.BlockedLinks[j]
		return left.From+"\x00"+left.To < right.From+"\x00"+right.To
	})
	sort.Slice(state.Control.Pending, func(i, j int) bool {
		left, _ := json.Marshal(state.Control.Pending[i])
		right, _ := json.Marshal(state.Control.Pending[j])
		return string(left) < string(right)
	})
	sort.Slice(state.Semantic.Entities, func(i, j int) bool {
		left, right := state.Semantic.Entities[i], state.Semantic.Entities[j]
		return string(left.Kind)+"\x00"+left.ID < string(right.Kind)+"\x00"+right.ID
	})
	sort.Slice(state.Semantic.Relations, func(i, j int) bool {
		left, right := state.Semantic.Relations[i], state.Semantic.Relations[j]
		return string(left.Kind)+"\x00"+left.From+"\x00"+left.To < string(right.Kind)+"\x00"+right.From+"\x00"+right.To
	})
}

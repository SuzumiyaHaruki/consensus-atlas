package controlruntime

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type NodeSnapshot struct {
	Ref            control.NodeRef       `json:"ref"`
	Lifecycle      control.NodeLifecycle `json:"lifecycle"`
	DurableEffects uint64                `json:"durable_effects"`
}

type ItemSnapshot struct {
	ID    control.ItemID       `json:"id"`
	Kind  control.ItemKind     `json:"kind"`
	Owner control.NodeRef      `json:"owner"`
	State control.ItemState    `json:"state"`
	Value control.ProducedItem `json:"value"`
}

type PartitionSnapshot struct {
	ID    string           `json:"id"`
	Left  []control.NodeID `json:"left"`
	Right []control.NodeID `json:"right"`
}

type CloneCounter struct {
	Item  control.ItemID `json:"item_id"`
	Count uint64         `json:"count"`
}

type Snapshot struct {
	Step               uint64              `json:"step"`
	LogicalTime        uint64              `json:"logical_time"`
	AdapterStateDigest string              `json:"adapter_state_digest"`
	EntropyDrawCount   uint64              `json:"entropy_draw_count"`
	EntropyTapeDigest  string              `json:"entropy_tape_digest"`
	Nodes              []NodeSnapshot      `json:"nodes"`
	Items              []ItemSnapshot      `json:"items"`
	Partitions         []PartitionSnapshot `json:"partitions"`
	CloneCounts        []CloneCounter      `json:"clone_counts,omitempty"`
	Offered            []control.Action    `json:"offered,omitempty"`
}

func (snapshot Snapshot) Digest() (string, error) {
	return control.CanonicalDigest(snapshot)
}

type itemEntry struct {
	item  control.ProducedItem
	state control.ItemState
}

type partition struct {
	id    string
	left  []control.NodeID
	right []control.NodeID
}

func (runtime *Runtime) Snapshot() Snapshot {
	snapshot := Snapshot{
		Step: runtime.step, LogicalTime: runtime.now, AdapterStateDigest: runtime.adapterState,
		EntropyDrawCount: runtime.lastEntropy, EntropyTapeDigest: runtime.entropyTape,
	}
	for _, node := range runtime.nodes {
		snapshot.Nodes = append(snapshot.Nodes, node)
	}
	sort.Slice(snapshot.Nodes, func(i, j int) bool {
		return snapshot.Nodes[i].Ref.Node < snapshot.Nodes[j].Ref.Node
	})
	for _, entry := range runtime.items {
		snapshot.Items = append(snapshot.Items, ItemSnapshot{
			ID: entry.item.ID, Kind: entry.item.Kind, Owner: entry.item.Owner,
			State: entry.state, Value: cloneItem(entry.item),
		})
	}
	sort.Slice(snapshot.Items, func(i, j int) bool { return snapshot.Items[i].ID < snapshot.Items[j].ID })
	for _, value := range runtime.partitions {
		snapshot.Partitions = append(snapshot.Partitions, PartitionSnapshot{
			ID:    value.id,
			Left:  append([]control.NodeID(nil), value.left...),
			Right: append([]control.NodeID(nil), value.right...),
		})
	}
	sort.Slice(snapshot.Partitions, func(i, j int) bool {
		return snapshot.Partitions[i].ID < snapshot.Partitions[j].ID
	})
	for item, count := range runtime.cloneCounts {
		snapshot.CloneCounts = append(snapshot.CloneCounts, CloneCounter{Item: item, Count: count})
	}
	sort.Slice(snapshot.CloneCounts, func(i, j int) bool {
		return snapshot.CloneCounts[i].Item < snapshot.CloneCounts[j].Item
	})
	for _, action := range runtime.offered {
		snapshot.Offered = append(snapshot.Offered, action)
	}
	sort.Slice(snapshot.Offered, func(i, j int) bool { return snapshot.Offered[i].ID < snapshot.Offered[j].ID })
	return snapshot
}

func (runtime *Runtime) stateDigest() (string, error) {
	return runtime.Snapshot().Digest()
}

func (runtime *Runtime) validateEmission(emission control.Emission, futureOwner *control.NodeRef) error {
	if err := emission.Validate(); err != nil {
		return err
	}
	newItems := make(map[control.ItemID]control.ProducedItem, len(emission.Items))
	for _, item := range emission.Items {
		if _, ok := runtime.items[item.ID]; ok {
			return fmt.Errorf("EMISSION_ITEM_ALREADY_EXISTS: %s", item.ID)
		}
		if _, ok := newItems[item.ID]; ok {
			return fmt.Errorf("EMISSION_ITEM_DUPLICATE: %s", item.ID)
		}
		if err := runtime.validateOwner(item.Owner, futureOwner); err != nil {
			return fmt.Errorf("item %s: %w", item.ID, err)
		}
		if !runtime.supportsItem(item.Kind) {
			return fmt.Errorf("EMISSION_ITEM_CAPABILITY_MISSING: %s", item.Kind)
		}
		if item.Temporal != nil {
			if item.Temporal.Deadline < runtime.now {
				return fmt.Errorf("TEMPORAL_DEADLINE_IN_PAST: %s", item.Temporal.ID)
			}
			if !runtime.supportsTemporal(item.Temporal.Kind) {
				return fmt.Errorf("TEMPORAL_CAPABILITY_MISSING: %s", item.Temporal.Kind)
			}
		}
		if item.Effect != nil && !runtime.supportsEffect(item.Effect.Kind) {
			return fmt.Errorf("EFFECT_CAPABILITY_MISSING: %s", item.Effect.Kind)
		}
		newItems[item.ID] = item
	}
	for _, item := range emission.Items {
		for _, dependency := range item.Dependencies {
			if _, ok := runtime.items[dependency]; ok {
				continue
			}
			if _, ok := newItems[dependency]; !ok {
				return fmt.Errorf("ITEM_DEPENDENCY_UNKNOWN: %s -> %s", item.ID, dependency)
			}
		}
	}
	visiting := make(map[control.ItemID]bool)
	visited := make(map[control.ItemID]bool)
	var visit func(control.ItemID) error
	visit = func(id control.ItemID) error {
		if visiting[id] {
			return fmt.Errorf("ITEM_DEPENDENCY_CYCLE: %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range newItems[id].Dependencies {
			if _, ok := newItems[dependency]; ok {
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range newItems {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *Runtime) validateOwner(owner control.NodeRef, futureOwner *control.NodeRef) error {
	if futureOwner != nil && owner.Node == futureOwner.Node {
		if owner == *futureOwner {
			return nil
		}
		return fmt.Errorf("ITEM_OWNER_INCARNATION_MISMATCH: got %d, future %d", owner.Incarnation, futureOwner.Incarnation)
	}
	node, ok := runtime.nodes[owner.Node]
	if !ok {
		return fmt.Errorf("ITEM_OWNER_UNKNOWN: %s", owner.Node)
	}
	if owner != node.Ref {
		return fmt.Errorf("ITEM_OWNER_INCARNATION_MISMATCH: got %d, current %d", owner.Incarnation, node.Ref.Incarnation)
	}
	return nil
}

func (runtime *Runtime) commitEmission(emission control.Emission) {
	for _, item := range emission.Items {
		runtime.items[item.ID] = &itemEntry{item: cloneItem(item), state: control.ItemProduced}
	}
	runtime.refreshItemStates()
}

func (runtime *Runtime) refreshItemStates() {
	changed := true
	for changed {
		changed = false
		for _, entry := range runtime.items {
			if terminal(entry.state) {
				continue
			}
			if entry.item.Kind == control.ItemClientResult || entry.item.Kind == control.ItemObservation {
				if entry.state != control.ItemCompleted {
					entry.state = control.ItemCompleted
					changed = true
				}
				continue
			}
			if runtime.ownerExpired(entry) {
				entry.state = control.ItemCanceled
				changed = true
				continue
			}
			ready, impossible := runtime.dependenciesReady(entry.item.Dependencies)
			if impossible {
				entry.state = control.ItemCanceled
				changed = true
				continue
			}
			var next control.ItemState
			switch entry.item.Kind {
			case control.ItemTemporal:
				next = control.ItemBlocked
			default:
				if ready {
					next = control.ItemEnabled
				} else {
					next = control.ItemBlocked
				}
			}
			if entry.state != next {
				entry.state = next
				changed = true
			}
		}
	}

	earliest := uint64(math.MaxUint64)
	for _, entry := range runtime.items {
		if entry.item.Kind != control.ItemTemporal || terminal(entry.state) {
			continue
		}
		ready, impossible := runtime.dependenciesReady(entry.item.Dependencies)
		if !ready || impossible || !runtime.ownerRunning(entry.item.Owner) {
			continue
		}
		if deadline := entry.item.Temporal.Deadline; deadline < earliest {
			earliest = deadline
		}
	}
	if earliest == math.MaxUint64 {
		return
	}
	limit := earliest
	if math.MaxUint64-earliest < runtime.clockError {
		limit = math.MaxUint64
	} else {
		limit += runtime.clockError
	}
	for _, entry := range runtime.items {
		if entry.item.Kind != control.ItemTemporal || terminal(entry.state) {
			continue
		}
		ready, impossible := runtime.dependenciesReady(entry.item.Dependencies)
		if ready && !impossible && runtime.ownerRunning(entry.item.Owner) && entry.item.Temporal.Deadline <= limit {
			entry.state = control.ItemEnabled
		} else {
			entry.state = control.ItemBlocked
		}
	}
}

func (runtime *Runtime) ownerExpired(entry *itemEntry) bool {
	node, ok := runtime.nodes[entry.item.Owner.Node]
	if !ok || node.Ref.Incarnation != entry.item.Owner.Incarnation {
		return entry.item.Kind != control.ItemMessage || entry.state != control.ItemEnabled
	}
	if node.Lifecycle == control.NodeStopped {
		return entry.item.Kind != control.ItemMessage || entry.state != control.ItemEnabled
	}
	return false
}

func (runtime *Runtime) dependenciesReady(dependencies []control.ItemID) (ready bool, impossible bool) {
	for _, dependency := range dependencies {
		entry := runtime.items[dependency]
		if entry == nil {
			return false, true
		}
		switch entry.state {
		case control.ItemCompleted:
			continue
		case control.ItemCanceled, control.ItemFailed, control.ItemDropped:
			return false, true
		default:
			return false, false
		}
	}
	return true, false
}

func (runtime *Runtime) ownerRunning(owner control.NodeRef) bool {
	node, ok := runtime.nodes[owner.Node]
	return ok && node.Ref == owner && node.Lifecycle == control.NodeRunning
}

func terminal(state control.ItemState) bool {
	switch state {
	case control.ItemCompleted, control.ItemCanceled, control.ItemFailed, control.ItemDropped:
		return true
	default:
		return false
	}
}

func cloneItem(item control.ProducedItem) control.ProducedItem {
	encoded, err := json.Marshal(item)
	if err != nil {
		panic(err)
	}
	var result control.ProducedItem
	if err := json.Unmarshal(encoded, &result); err != nil {
		panic(err)
	}
	return result
}

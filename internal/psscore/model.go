// Package psscore defines the protocol-independent state shape used for
// state-discovery measurements. It is downstream of Control Runtime and is
// never used to decide whether an action or an Oracle result is valid.
package psscore

import (
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	SchemaVersion            = "consensus-atlas/core-pss/v1"
	MaxCanonicalParticipants = 8
)

type EntityKind string

const (
	EntityParticipant EntityKind = "participant"
	EntityEpoch       EntityKind = "epoch"
	EntityDecision    EntityKind = "decision-unit"
	EntityValue       EntityKind = "value"
	EntityEvidence    EntityKind = "evidence"
)

type RelationKind string

const (
	RelationBelongsTo RelationKind = "belongs-to"
	RelationProposes  RelationKind = "proposes"
	RelationSupports  RelationKind = "supports"
	RelationDependsOn RelationKind = "depends-on"
	RelationConflicts RelationKind = "conflicts-with"
	RelationPrecedes  RelationKind = "precedes"
	RelationDecides   RelationKind = "decides"
	RelationPersists  RelationKind = "persists"
	RelationApplies   RelationKind = "applies"
)

type Stage string

const (
	StageUnknown   Stage = "unknown"
	StageProposed  Stage = "proposed"
	StageSupported Stage = "supported"
	StageAccepted  Stage = "accepted"
	StageDecided   Stage = "decided"
	StageApplied   Stage = "applied"
)

// ParticipantMode deliberately abstracts protocol names such as leader and
// candidate. Protocol-specific role details belong in Extended PSS.
type ParticipantMode string

const (
	ModeInactive     ParticipantMode = "inactive"
	ModePassive      ParticipantMode = "passive"
	ModeContending   ParticipantMode = "contending"
	ModeCoordinating ParticipantMode = "coordinating"
)

type Participant struct {
	ID              string                `json:"id"`
	Lifecycle       control.NodeLifecycle `json:"lifecycle"`
	IncarnationRank uint64                `json:"incarnation_rank"`
}

type BlockedLink struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// PendingItem intentionally excludes native IDs, absolute deadlines and
// payloads. DependencyKinds is a conservative causal-shape summary, not an
// assertion that two native dependency graphs are equivalent.
type PendingItem struct {
	Kind            control.ItemKind        `json:"kind"`
	State           control.ItemState       `json:"state"`
	Owner           string                  `json:"owner"`
	OwnerPrior      bool                    `json:"owner_prior_incarnation,omitempty"`
	Target          string                  `json:"target,omitempty"`
	TemporalKind    control.TemporalKind    `json:"temporal_kind,omitempty"`
	Durability      control.DurabilityClass `json:"durability,omitempty"`
	DependencyKinds []control.ItemKind      `json:"dependency_kinds,omitempty"`
	Earliest        bool                    `json:"earliest_temporal,omitempty"`
}

type ControlContext struct {
	Participants []Participant `json:"participants"`
	BlockedLinks []BlockedLink `json:"blocked_links,omitempty"`
	Pending      []PendingItem `json:"pending,omitempty"`
}

type Entity struct {
	ID    string          `json:"id"`
	Kind  EntityKind      `json:"kind"`
	Stage Stage           `json:"stage,omitempty"`
	Mode  ParticipantMode `json:"mode,omitempty"`
}

type Relation struct {
	Kind RelationKind `json:"kind"`
	From string       `json:"from"`
	To   string       `json:"to"`
}

type SemanticGraph struct {
	Entities  []Entity   `json:"entities"`
	Relations []Relation `json:"relations,omitempty"`
}

// SemanticObservation binds Adapter evidence to the Runtime logical instant
// from which it was sampled. LogicalTime is checked for freshness and then
// excluded from the canonical state.
type SemanticObservation struct {
	LogicalTime uint64
	Graph       SemanticGraph
}

type State struct {
	SchemaVersion string         `json:"schema_version"`
	MappingID     string         `json:"mapping_id"`
	Control       ControlContext `json:"control"`
	Semantic      SemanticGraph  `json:"semantic"`
	Digest        string         `json:"digest"`
}

var validEntityKinds = map[EntityKind]bool{
	EntityParticipant: true, EntityEpoch: true, EntityDecision: true,
	EntityValue: true, EntityEvidence: true,
}

var validRelationKinds = map[RelationKind]bool{
	RelationBelongsTo: true, RelationProposes: true, RelationSupports: true,
	RelationDependsOn: true, RelationConflicts: true, RelationPrecedes: true,
	RelationDecides: true, RelationPersists: true, RelationApplies: true,
}

var validStages = map[Stage]bool{
	StageUnknown: true, StageProposed: true, StageSupported: true,
	StageAccepted: true, StageDecided: true, StageApplied: true,
}

var validModes = map[ParticipantMode]bool{
	ModeInactive: true, ModePassive: true, ModeContending: true, ModeCoordinating: true,
}

func validateState(state State) error {
	if state.SchemaVersion != SchemaVersion {
		return fmt.Errorf("CORE_PSS_SCHEMA_MISMATCH: %s", state.SchemaVersion)
	}
	if state.MappingID == "" {
		return fmt.Errorf("CORE_PSS_MAPPING_REQUIRED")
	}
	if len(state.Control.Participants) == 0 || len(state.Control.Participants) > MaxCanonicalParticipants {
		return fmt.Errorf("CORE_PSS_PARTICIPANT_COUNT_UNSUPPORTED: %d", len(state.Control.Participants))
	}
	participants := make(map[string]bool, len(state.Control.Participants))
	for _, participant := range state.Control.Participants {
		if participant.ID == "" || participants[participant.ID] {
			return fmt.Errorf("CORE_PSS_PARTICIPANT_ID_INVALID: %s", participant.ID)
		}
		switch participant.Lifecycle {
		case control.NodeStopped, control.NodeRunning:
		default:
			return fmt.Errorf("CORE_PSS_LIFECYCLE_UNSUPPORTED: %s", participant.Lifecycle)
		}
		participants[participant.ID] = true
	}
	for _, link := range state.Control.BlockedLinks {
		if !participants[link.From] || !participants[link.To] || link.From == link.To {
			return fmt.Errorf("CORE_PSS_BLOCKED_LINK_INVALID: %s->%s", link.From, link.To)
		}
	}
	for _, item := range state.Control.Pending {
		if err := item.Kind.Validate(); err != nil {
			return err
		}
		switch item.State {
		case control.ItemProduced, control.ItemBlocked, control.ItemEnabled:
		default:
			return fmt.Errorf("CORE_PSS_PENDING_STATE_INVALID: %s", item.State)
		}
		if !participants[item.Owner] || (item.Target != "" && !participants[item.Target]) {
			return fmt.Errorf("CORE_PSS_PENDING_ROUTE_INVALID: %s->%s", item.Owner, item.Target)
		}
		if item.TemporalKind != "" {
			if item.Kind != control.ItemTemporal {
				return fmt.Errorf("CORE_PSS_TEMPORAL_KIND_UNEXPECTED")
			}
			if err := item.TemporalKind.Validate(); err != nil {
				return err
			}
		}
		for _, dependency := range item.DependencyKinds {
			if err := dependency.Validate(); err != nil {
				return err
			}
		}
	}
	entities := make(map[string]Entity, len(state.Semantic.Entities))
	semanticParticipants := make(map[string]bool, len(participants))
	participantModes := make(map[string]ParticipantMode, len(participants))
	for _, entity := range state.Semantic.Entities {
		if entity.ID == "" || entities[entity.ID].ID != "" || !validEntityKinds[entity.Kind] {
			return fmt.Errorf("CORE_PSS_ENTITY_INVALID: %s/%s", entity.ID, entity.Kind)
		}
		if entity.Stage != "" && !validStages[entity.Stage] {
			return fmt.Errorf("CORE_PSS_STAGE_UNSUPPORTED: %s", entity.Stage)
		}
		if entity.Kind == EntityParticipant {
			if !participants[entity.ID] || !validModes[entity.Mode] {
				return fmt.Errorf("CORE_PSS_PARTICIPANT_ENTITY_INVALID: %s/%s", entity.ID, entity.Mode)
			}
			semanticParticipants[entity.ID] = true
			participantModes[entity.ID] = entity.Mode
		} else if entity.Mode != "" {
			return fmt.Errorf("CORE_PSS_MODE_UNEXPECTED: %s", entity.ID)
		}
		entities[entity.ID] = entity
	}
	if len(semanticParticipants) != len(participants) {
		return fmt.Errorf("CORE_PSS_PARTICIPANT_ENTITY_SET_MISMATCH")
	}
	for _, participant := range state.Control.Participants {
		mode := participantModes[participant.ID]
		if participant.Lifecycle == control.NodeStopped && mode != ModeInactive {
			return fmt.Errorf("CORE_PSS_STOPPED_PARTICIPANT_MODE_INVALID: %s/%s", participant.ID, mode)
		}
		if participant.Lifecycle == control.NodeRunning && mode == ModeInactive {
			return fmt.Errorf("CORE_PSS_RUNNING_PARTICIPANT_MODE_INVALID: %s", participant.ID)
		}
	}
	seenRelations := make(map[string]bool, len(state.Semantic.Relations))
	for _, relation := range state.Semantic.Relations {
		if !validRelationKinds[relation.Kind] || entities[relation.From].ID == "" || entities[relation.To].ID == "" {
			return fmt.Errorf("CORE_PSS_RELATION_INVALID: %s/%s/%s", relation.Kind, relation.From, relation.To)
		}
		key := string(relation.Kind) + "\x00" + relation.From + "\x00" + relation.To
		if seenRelations[key] {
			return fmt.Errorf("CORE_PSS_RELATION_DUPLICATE: %s", key)
		}
		seenRelations[key] = true
	}
	return nil
}

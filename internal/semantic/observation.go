package semantic

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

// ObservationKind is a small protocol-family vocabulary projected from a
// trusted trace. Target Adapters may add facts, but they do not change Action
// semantics or Oracle results.
type ObservationKind string

const (
	ObservationWorkloadInvoked   ObservationKind = "workload-invoked"
	ObservationMessageDropped    ObservationKind = "message-dropped"
	ObservationMessageDelivered  ObservationKind = "message-delivered"
	ObservationTemporalFired     ObservationKind = "temporal-fired"
	ObservationNodeCrashed       ObservationKind = "node-crashed"
	ObservationNodeRestarted     ObservationKind = "node-restarted"
	ObservationCoordinatorChange ObservationKind = "coordinator-changed"
	ObservationEpochAdvanced     ObservationKind = "epoch-advanced"
	ObservationDecisionAdvanced  ObservationKind = "decision-advanced"
)

var observationKinds = map[ObservationKind]struct{}{
	ObservationWorkloadInvoked: {}, ObservationMessageDropped: {},
	ObservationMessageDelivered: {}, ObservationTemporalFired: {},
	ObservationNodeCrashed: {}, ObservationNodeRestarted: {},
	ObservationCoordinatorChange: {}, ObservationEpochAdvanced: {},
	ObservationDecisionAdvanced: {},
}

// Observation is a trace-bound semantic fact. It intentionally contains only
// fields shared by the current leader/round consensus scope. Target-specific
// state remains in Adapter Evidence.
type Observation struct {
	Kind               ObservationKind        `json:"kind"`
	Step               uint64                 `json:"step"`
	SourceDigest       string                 `json:"source_digest"`
	Participant        *control.NodeRef       `json:"participant,omitempty"`
	RelatedParticipant *control.NodeRef       `json:"related_participant,omitempty"`
	RequestID          string                 `json:"request_id,omitempty"`
	ParticipantRole    string                 `json:"participant_role,omitempty"`
	MessageRole        string                 `json:"message_role,omitempty"`
	OperationStage     string                 `json:"operation_stage,omitempty"`
	Attributes         []ObservationAttribute `json:"attributes,omitempty"`
}

type ObservationValueType string

const (
	ObservationValueString ObservationValueType = "string"
	ObservationValueUint   ObservationValueType = "uint"
	ObservationValueBool   ObservationValueType = "bool"
	ObservationValueNodeID ObservationValueType = "node-id"
)

// ObservationAttribute carries one target-declared scalar. Core validates its
// shape and declared type but never interprets protocol meaning.
type ObservationAttribute struct {
	Field ObservationField     `json:"field"`
	Type  ObservationValueType `json:"type"`
	Value string               `json:"value"`
}

func (observation Observation) Validate() error {
	if !validObservationKind(observation.Kind) || observation.Step == 0 ||
		!validRiskWitnessSHA256(observation.SourceDigest) {
		return errors.New("OBSERVATION_IDENTITY_INVALID")
	}
	if observation.Participant != nil {
		if err := observation.Participant.Validate(); err != nil {
			return err
		}
	}
	if observation.RelatedParticipant != nil {
		if err := observation.RelatedParticipant.Validate(); err != nil {
			return err
		}
	}
	for _, value := range []string{
		observation.RequestID, observation.ParticipantRole,
		observation.MessageRole, observation.OperationStage,
	} {
		if strings.TrimSpace(value) != value {
			return errors.New("OBSERVATION_VALUE_INVALID")
		}
	}
	for index, attribute := range observation.Attributes {
		if !validTargetObservationField(attribute.Field) ||
			!validObservationScalar(attribute.Type, attribute.Value) ||
			(index > 0 && observation.Attributes[index-1].Field >= attribute.Field) {
			return errors.New("OBSERVATION_ATTRIBUTE_INVALID")
		}
	}
	return nil
}

// ObservationHistory is a derived view over an existing Trace, not a second
// execution log. SourceDigest mechanically binds every fact to one Trace
// record, so observations cannot invent events.
type ObservationHistory struct {
	ProjectorID string        `json:"projector_id"`
	TraceDigest string        `json:"trace_digest"`
	Events      []Observation `json:"events"`
}

func NewObservationHistory(
	projectorID string,
	trace controlruntime.Trace,
	events []Observation,
) (ObservationHistory, error) {
	return newObservationHistory(projectorID, trace, events, nil, false)
}

// NewObservationHistoryWithCapabilities admits target-local facts only after
// validating them against the projector's declared schemas.
func NewObservationHistoryWithCapabilities(
	projectorID string,
	trace controlruntime.Trace,
	events []Observation,
	capabilities []ObservationCapability,
) (ObservationHistory, error) {
	return newObservationHistory(projectorID, trace, events, capabilities, true)
}

func newObservationHistory(
	projectorID string,
	trace controlruntime.Trace,
	events []Observation,
	capabilities []ObservationCapability,
	validateCapabilities bool,
) (ObservationHistory, error) {
	if strings.TrimSpace(projectorID) != projectorID || projectorID == "" || trace.Validate() != nil {
		return ObservationHistory{}, errors.New("OBSERVATION_HISTORY_IDENTITY_INVALID")
	}
	var declared map[ObservationKind]indexedObservationCapability
	var err error
	if validateCapabilities {
		declared, err = indexObservationCapabilities(capabilities)
		if err != nil {
			return ObservationHistory{}, err
		}
	}
	recordDigests := make(map[uint64]string, len(trace.Records))
	for _, record := range trace.Records {
		digest, err := control.CanonicalDigest(record)
		if err != nil {
			return ObservationHistory{}, err
		}
		recordDigests[record.Step] = digest
	}
	result := ObservationHistory{
		ProjectorID: projectorID,
		TraceDigest: trace.Digest,
		Events:      cloneObservations(events),
	}
	for _, event := range result.Events {
		if err := event.Validate(); err != nil || recordDigests[event.Step] != event.SourceDigest ||
			(!validateCapabilities && !isCoreObservationKind(event.Kind)) ||
			(validateCapabilities && !observationMatchesCapability(event, declared[event.Kind])) {
			return ObservationHistory{}, errors.New("OBSERVATION_HISTORY_SOURCE_INVALID")
		}
	}
	sort.SliceStable(result.Events, func(i, j int) bool {
		return observationLess(result.Events[i], result.Events[j])
	})
	return result, nil
}

// ProjectRuntimeObservations derives only facts already represented by the
// generic control Action and node lifecycle. Protocol facts are added by a
// target-owned Observation projector.
func ProjectRuntimeObservations(trace controlruntime.Trace) ([]Observation, error) {
	if err := trace.Validate(); err != nil {
		return nil, err
	}
	result := make([]Observation, 0, len(trace.Records))
	for _, record := range trace.Records {
		digest, err := control.CanonicalDigest(record)
		if err != nil {
			return nil, err
		}
		kind := ObservationKind("")
		participant := record.Action.Node
		switch record.Action.Kind {
		case control.ActionInvoke:
			kind = ObservationWorkloadInvoked
		case control.ActionDropMessage:
			kind = ObservationMessageDropped
		case control.ActionDeliverMessage:
			kind = ObservationMessageDelivered
		case control.ActionFireTemporal:
			kind = ObservationTemporalFired
		case control.ActionCrash:
			kind = ObservationNodeCrashed
		case control.ActionRestart:
			kind = ObservationNodeRestarted
			for _, transition := range record.NodeTransitions {
				if transition.Node == record.Action.Node.Node &&
					transition.Before.Lifecycle == control.NodeStopped &&
					transition.After.Lifecycle == control.NodeRunning {
					participant = transition.After.Ref
					break
				}
			}
		}
		if kind == "" {
			continue
		}
		value := participant
		result = append(result, Observation{
			Kind: kind, Step: record.Step, SourceDigest: digest, Participant: &value,
		})
	}
	return result, nil
}

type ObservationField string

const (
	ObservationFieldParticipant        ObservationField = "participant"
	ObservationFieldParticipantNode    ObservationField = "participant-node"
	ObservationFieldRelatedParticipant ObservationField = "related-participant"
	ObservationFieldRelatedNode        ObservationField = "related-participant-node"
	ObservationFieldRequestID          ObservationField = "request-id"
	ObservationFieldParticipantRole    ObservationField = "participant-role"
	ObservationFieldMessageRole        ObservationField = "message-role"
	ObservationFieldOperationStage     ObservationField = "operation-stage"
)

var observationFields = map[ObservationField]struct{}{
	ObservationFieldParticipant: {}, ObservationFieldParticipantNode: {},
	ObservationFieldRelatedParticipant: {}, ObservationFieldRelatedNode: {},
	ObservationFieldRequestID: {}, ObservationFieldParticipantRole: {}, ObservationFieldMessageRole: {},
	ObservationFieldOperationStage: {},
}

// ObservationConstraint is deliberately smaller than a temporal DSL. Equals
// matches one literal; BindAs captures a value on first use and requires the
// same value on later uses.
type ObservationConstraint struct {
	Field  ObservationField `json:"field"`
	Equals string           `json:"equals,omitempty"`
	BindAs string           `json:"bind_as,omitempty"`
}

type ObservationPredicate struct {
	MilestoneID string                  `json:"milestone_id"`
	Kind        ObservationKind         `json:"kind"`
	Constraints []ObservationConstraint `json:"constraints,omitempty"`
}

// MatchLinearRiskWitness finds the earliest complete ordered match. If no
// complete chain exists, it returns the longest deterministic prefix so the
// Agent can see honest progress without controlling execution closure.
func MatchLinearRiskWitness(
	spec RiskWitnessSpec,
	predicates []ObservationPredicate,
	history ObservationHistory,
) ([]RiskWitnessMilestoneEvidence, error) {
	if err := spec.Validate(); err != nil || history.ProjectorID == "" || history.TraceDigest == "" ||
		len(predicates) != len(spec.Milestones) {
		return nil, errors.New("LINEAR_WITNESS_INPUT_INVALID")
	}
	if err := validateObservationPredicates(predicates); err != nil {
		return nil, err
	}
	for index, predicate := range predicates {
		if predicate.MilestoneID != spec.Milestones[index].ID {
			return nil, errors.New("LINEAR_WITNESS_MILESTONE_MISMATCH")
		}
	}
	for index, event := range history.Events {
		if err := event.Validate(); err != nil || (index > 0 && observationLess(event, history.Events[index-1])) {
			return nil, errors.New("LINEAR_WITNESS_HISTORY_INVALID")
		}
	}

	best := make([]RiskWitnessMilestoneEvidence, 0, len(predicates))
	var search func(int, int, map[string]string, []RiskWitnessMilestoneEvidence) bool
	search = func(predicateIndex, eventIndex int, bindings map[string]string, path []RiskWitnessMilestoneEvidence) bool {
		if len(path) > len(best) {
			best = cloneRiskWitnessEvidence(path)
		}
		if predicateIndex == len(predicates) {
			return true
		}
		predicate := predicates[predicateIndex]
		for index := eventIndex; index < len(history.Events); index++ {
			event := history.Events[index]
			if len(path) > 0 && event.Step <= path[len(path)-1].Step {
				continue
			}
			nextBindings, ok := matchObservationPredicate(predicate, event, bindings)
			if !ok {
				continue
			}
			matched := RiskWitnessMilestoneEvidence{
				MilestoneID: predicate.MilestoneID, Step: event.Step,
				Kind: string(event.Kind), EvidenceDigest: event.SourceDigest,
				Participant: event.Participant, RelatedParticipant: event.RelatedParticipant,
			}
			if search(predicateIndex+1, index+1, nextBindings, append(path, matched)) {
				return true
			}
		}
		return false
	}
	search(0, 0, map[string]string{}, nil)
	if best == nil {
		best = make([]RiskWitnessMilestoneEvidence, 0)
	}
	return best, nil
}

func validateObservationPredicates(predicates []ObservationPredicate) error {
	if len(predicates) == 0 {
		return errors.New("LINEAR_WITNESS_PREDICATES_REQUIRED")
	}
	for _, predicate := range predicates {
		if !validObservationKind(predicate.Kind) {
			return errors.New("LINEAR_WITNESS_KIND_INVALID")
		}
		for _, constraint := range predicate.Constraints {
			fieldOK := validObservationField(constraint.Field)
			if !fieldOK || (constraint.Equals == "") == (constraint.BindAs == "") ||
				(constraint.BindAs != "" && !validRiskWitnessToken(constraint.BindAs)) {
				return errors.New("LINEAR_WITNESS_CONSTRAINT_INVALID")
			}
		}
	}
	return nil
}

func matchObservationPredicate(
	predicate ObservationPredicate,
	event Observation,
	bindings map[string]string,
) (map[string]string, bool) {
	if event.Kind != predicate.Kind {
		return nil, false
	}
	result := make(map[string]string, len(bindings)+len(predicate.Constraints))
	for key, value := range bindings {
		result[key] = value
	}
	for _, constraint := range predicate.Constraints {
		value, ok := observationFieldValue(event, constraint.Field)
		if !ok {
			return nil, false
		}
		if constraint.Equals != "" && value != constraint.Equals {
			return nil, false
		}
		if constraint.BindAs != "" {
			bound, exists := result[constraint.BindAs]
			if exists && bound != value {
				return nil, false
			}
			result[constraint.BindAs] = value
		}
	}
	return result, true
}

func observationFieldValue(event Observation, field ObservationField) (string, bool) {
	switch field {
	case ObservationFieldParticipant:
		if event.Participant == nil {
			return "", false
		}
		return fmt.Sprintf("%s@%d", event.Participant.Node, event.Participant.Incarnation), true
	case ObservationFieldParticipantNode:
		if event.Participant == nil {
			return "", false
		}
		return string(event.Participant.Node), true
	case ObservationFieldRelatedParticipant:
		if event.RelatedParticipant == nil {
			return "", false
		}
		return fmt.Sprintf("%s@%d", event.RelatedParticipant.Node, event.RelatedParticipant.Incarnation), true
	case ObservationFieldRelatedNode:
		if event.RelatedParticipant == nil {
			return "", false
		}
		return string(event.RelatedParticipant.Node), true
	case ObservationFieldRequestID:
		return event.RequestID, event.RequestID != ""
	case ObservationFieldParticipantRole:
		return event.ParticipantRole, event.ParticipantRole != ""
	case ObservationFieldMessageRole:
		return event.MessageRole, event.MessageRole != ""
	case ObservationFieldOperationStage:
		return event.OperationStage, event.OperationStage != ""
	}
	for _, attribute := range event.Attributes {
		if attribute.Field == field {
			return attribute.Value, true
		}
	}
	return "", false
}

func observationLess(left, right Observation) bool {
	if left.Step != right.Step {
		return left.Step < right.Step
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.SourceDigest != right.SourceDigest {
		return left.SourceDigest < right.SourceDigest
	}
	leftNode, rightNode := observationNodeKey(left.Participant), observationNodeKey(right.Participant)
	if leftNode != rightNode {
		return leftNode < rightNode
	}
	return observationAttributeKey(left.Attributes) < observationAttributeKey(right.Attributes)
}

func observationNodeKey(value *control.NodeRef) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%s@%020d", value.Node, value.Incarnation)
}

func cloneObservations(values []Observation) []Observation {
	result := make([]Observation, len(values))
	copy(result, values)
	for index := range result {
		if values[index].Participant != nil {
			participant := *values[index].Participant
			result[index].Participant = &participant
		}
		if values[index].RelatedParticipant != nil {
			related := *values[index].RelatedParticipant
			result[index].RelatedParticipant = &related
		}
		result[index].Attributes = append([]ObservationAttribute(nil), values[index].Attributes...)
	}
	return result
}

func isCoreObservationKind(kind ObservationKind) bool {
	_, ok := observationKinds[kind]
	return ok
}

func validObservationKind(kind ObservationKind) bool {
	return isCoreObservationKind(kind) || validNamespacedObservationName(string(kind))
}

func validObservationField(field ObservationField) bool {
	if _, ok := observationFields[field]; ok {
		return true
	}
	return validTargetObservationField(field)
}

func validTargetObservationField(field ObservationField) bool {
	return validNamespacedObservationName(string(field))
}

func validNamespacedObservationName(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) < 2 || len(value) > 128 {
		return false
	}
	for _, part := range parts {
		if part == "" || part[0] < 'a' || part[0] > 'z' || part[len(part)-1] == '-' {
			return false
		}
		for _, character := range part {
			if (character < 'a' || character > 'z') &&
				(character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func validObservationValueType(value ObservationValueType) bool {
	return value == ObservationValueString || value == ObservationValueUint ||
		value == ObservationValueBool || value == ObservationValueNodeID
}

func validObservationScalar(valueType ObservationValueType, value string) bool {
	if !validObservationValueType(valueType) || value == "" || len(value) > 256 ||
		strings.TrimSpace(value) != value {
		return false
	}
	switch valueType {
	case ObservationValueString:
		return true
	case ObservationValueBool:
		return value == "true" || value == "false"
	case ObservationValueUint:
		parsed, err := strconv.ParseUint(value, 10, 64)
		return err == nil && strconv.FormatUint(parsed, 10) == value
	case ObservationValueNodeID:
		return !strings.ContainsAny(value, " /\\")
	default:
		return false
	}
}

func observationAttributeKey(attributes []ObservationAttribute) string {
	var builder strings.Builder
	for _, attribute := range attributes {
		builder.WriteString(string(attribute.Field))
		builder.WriteByte(0)
		builder.WriteString(string(attribute.Type))
		builder.WriteByte(0)
		builder.WriteString(attribute.Value)
		builder.WriteByte(0)
	}
	return builder.String()
}

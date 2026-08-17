package semantic

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	RiskIssueMissingObservationKind  = "missing-observation-kind"
	RiskIssueMissingObservationField = "missing-observation-field"
	RiskIssueMissingObservationValue = "missing-observation-value"
	RiskIssueMissingAction           = "missing-action"
)

// ObservationCapability is the small, target-declared surface that a Risk
// may reference. It describes facts the projector can produce, not protocol
// internals or runtime control semantics.
type ObservationCapability struct {
	Kind       ObservationKind                           `json:"kind"`
	Fields     []ObservationField                        `json:"fields,omitempty"`
	FieldTypes map[ObservationField]ObservationValueType `json:"field_types,omitempty"`
	Values     map[ObservationField][]string             `json:"values,omitempty"`
}

// ObservationBindingDomain describes which fields may safely reuse one
// bind_as token. It is derived from an existing capability declaration; a
// Target does not maintain a second binding schema.
type ObservationBindingDomain struct {
	Kind   ObservationKind  `json:"kind"`
	Field  ObservationField `json:"field"`
	Domain string           `json:"domain"`
}

// RiskRequirements is mechanically compiled from ordered predicates. Actions
// are implied only when an Observation kind is defined by one Runtime Action.
type RiskRequirements struct {
	Observations []ObservationCapability `json:"observations"`
	Actions      []control.ActionKind    `json:"actions,omitempty"`
}

type RiskQualificationIssue struct {
	Code   string             `json:"code"`
	Kind   ObservationKind    `json:"kind,omitempty"`
	Field  ObservationField   `json:"field,omitempty"`
	Value  string             `json:"value,omitempty"`
	Action control.ActionKind `json:"action,omitempty"`
}

type RiskQualification struct {
	Qualified    bool                     `json:"qualified"`
	Requirements RiskRequirements         `json:"requirements"`
	Issues       []RiskQualificationIssue `json:"issues,omitempty"`
}

// CompileRiskRequirements turns a Risk's declarative predicates into the
// minimum Observation and Action surface needed to execute it.
func CompileRiskRequirements(predicates []ObservationPredicate) (RiskRequirements, error) {
	if err := validateObservationPredicates(predicates); err != nil {
		return RiskRequirements{}, err
	}
	fieldsByKind := make(map[ObservationKind]map[ObservationField]struct{})
	valuesByKind := make(map[ObservationKind]map[ObservationField]map[string]struct{})
	actions := make(map[control.ActionKind]struct{})
	for _, predicate := range predicates {
		fields := fieldsByKind[predicate.Kind]
		if fields == nil {
			fields = make(map[ObservationField]struct{})
			fieldsByKind[predicate.Kind] = fields
		}
		for _, constraint := range predicate.Constraints {
			fields[constraint.Field] = struct{}{}
			if constraint.Equals != "" {
				kindValues := valuesByKind[predicate.Kind]
				if kindValues == nil {
					kindValues = make(map[ObservationField]map[string]struct{})
					valuesByKind[predicate.Kind] = kindValues
				}
				fieldValues := kindValues[constraint.Field]
				if fieldValues == nil {
					fieldValues = make(map[string]struct{})
					kindValues[constraint.Field] = fieldValues
				}
				fieldValues[constraint.Equals] = struct{}{}
			}
		}
		if action, ok := observationAction(predicate.Kind); ok {
			actions[action] = struct{}{}
		}
	}
	requirements := RiskRequirements{
		Observations: make([]ObservationCapability, 0, len(fieldsByKind)),
		Actions:      make([]control.ActionKind, 0, len(actions)),
	}
	for kind, fields := range fieldsByKind {
		capability := ObservationCapability{Kind: kind, Fields: make([]ObservationField, 0, len(fields))}
		for field := range fields {
			capability.Fields = append(capability.Fields, field)
		}
		sort.Slice(capability.Fields, func(i, j int) bool { return capability.Fields[i] < capability.Fields[j] })
		if values := valuesByKind[kind]; len(values) > 0 {
			capability.Values = make(map[ObservationField][]string, len(values))
			for field, fieldValues := range values {
				for value := range fieldValues {
					capability.Values[field] = append(capability.Values[field], value)
				}
				sort.Strings(capability.Values[field])
			}
		}
		requirements.Observations = append(requirements.Observations, capability)
	}
	sort.Slice(requirements.Observations, func(i, j int) bool {
		return requirements.Observations[i].Kind < requirements.Observations[j].Kind
	})
	for action := range actions {
		requirements.Actions = append(requirements.Actions, action)
	}
	sort.Slice(requirements.Actions, func(i, j int) bool { return requirements.Actions[i] < requirements.Actions[j] })
	return requirements, nil
}

// QualifyRisk checks the compiled requirements against one target's projector
// declaration and the Action list from its actual Adapter Manifest.
func QualifyRisk(
	predicates []ObservationPredicate,
	capabilities []ObservationCapability,
	actions []control.ActionKind,
) (RiskQualification, error) {
	requirements, err := CompileRiskRequirements(predicates)
	if err != nil {
		return RiskQualification{}, err
	}
	available, err := indexObservationCapabilities(capabilities)
	if err != nil {
		return RiskQualification{}, err
	}
	actionSet := make(map[control.ActionKind]struct{}, len(actions))
	for _, action := range actions {
		if err := action.Validate(); err != nil {
			return RiskQualification{}, err
		}
		actionSet[action] = struct{}{}
	}
	result := RiskQualification{Requirements: requirements}
	for _, required := range requirements.Observations {
		capability, ok := available[required.Kind]
		if !ok {
			result.Issues = append(result.Issues, RiskQualificationIssue{
				Code: RiskIssueMissingObservationKind, Kind: required.Kind,
			})
			continue
		}
		for _, field := range required.Fields {
			if _, ok := capability.fields[field]; !ok {
				result.Issues = append(result.Issues, RiskQualificationIssue{
					Code: RiskIssueMissingObservationField, Kind: required.Kind, Field: field,
				})
				continue
			}
			for _, value := range required.Values[field] {
				allowed := capability.values[field]
				valueType := capability.fieldTypes[field]
				_, explicitlyAllowed := allowed[value]
				if len(allowed) == 0 && valueType != "" {
					explicitlyAllowed = validObservationScalar(valueType, value)
				}
				if !explicitlyAllowed {
					result.Issues = append(result.Issues, RiskQualificationIssue{
						Code: RiskIssueMissingObservationValue, Kind: required.Kind, Field: field, Value: value,
					})
				}
			}
		}
	}
	for _, action := range requirements.Actions {
		if _, ok := actionSet[action]; !ok {
			result.Issues = append(result.Issues, RiskQualificationIssue{
				Code: RiskIssueMissingAction, Action: action,
			})
		}
	}
	sort.Slice(result.Issues, func(i, j int) bool {
		left, right := result.Issues[i], result.Issues[j]
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Field != right.Field {
			return left.Field < right.Field
		}
		if left.Value != right.Value {
			return left.Value < right.Value
		}
		return left.Action < right.Action
	})
	result.Qualified = len(result.Issues) == 0
	return result, nil
}

// ValidateObservationCapabilities lets an Agent-facing composition validate
// the target declaration before spending a model call.
func ValidateObservationCapabilities(capabilities []ObservationCapability) error {
	_, err := indexObservationCapabilities(capabilities)
	return err
}

// ObservationBindingDomains exposes the mechanically derived domains used by
// Risk validation. Node-ID target attributes intentionally share a domain
// with participant-node; other target-local fields remain separate.
func ObservationBindingDomains(capabilities []ObservationCapability) ([]ObservationBindingDomain, error) {
	indexed, err := indexObservationCapabilities(capabilities)
	if err != nil {
		return nil, err
	}
	result := make([]ObservationBindingDomain, 0)
	for _, capability := range capabilities {
		for _, field := range capability.Fields {
			domain, ok := observationBindingDomain(field, indexed[capability.Kind].fieldTypes[field])
			if !ok {
				return nil, errors.New("RISK_OBSERVATION_BINDING_DOMAIN_INVALID")
			}
			result = append(result, ObservationBindingDomain{
				Kind: capability.Kind, Field: field, Domain: domain,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Field < result[j].Field
	})
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// ValidateObservationPredicateBindings rejects a bind_as token reused across
// incompatible value domains before a Scenario is planned. Missing fields are
// left to the existing qualification report so its precise issue is retained.
func ValidateObservationPredicateBindings(
	predicates []ObservationPredicate,
	capabilities []ObservationCapability,
) error {
	if err := validateObservationPredicates(predicates); err != nil {
		return err
	}
	domains, err := ObservationBindingDomains(capabilities)
	if err != nil {
		return err
	}
	byField := make(map[ObservationKind]map[ObservationField]string)
	for _, value := range domains {
		if byField[value.Kind] == nil {
			byField[value.Kind] = make(map[ObservationField]string)
		}
		byField[value.Kind][value.Field] = value.Domain
	}
	bindings := make(map[string]string)
	for _, predicate := range predicates {
		for _, constraint := range predicate.Constraints {
			if constraint.BindAs == "" {
				continue
			}
			domain := byField[predicate.Kind][constraint.Field]
			if domain == "" {
				continue
			}
			if previous := bindings[constraint.BindAs]; previous != "" && previous != domain {
				return errors.New("RISK_OBSERVATION_BINDING_DOMAIN_MISMATCH")
			}
			bindings[constraint.BindAs] = domain
		}
	}
	return nil
}

func observationBindingDomain(field ObservationField, valueType ObservationValueType) (string, bool) {
	switch field {
	case ObservationFieldParticipant, ObservationFieldRelatedParticipant:
		return "node-incarnation", true
	case ObservationFieldParticipantNode, ObservationFieldRelatedNode:
		return "node-id", true
	case ObservationFieldRequestID:
		return "request-id", true
	case ObservationFieldParticipantRole:
		return "participant-role", true
	case ObservationFieldMessageRole:
		return "message-role", true
	case ObservationFieldOperationStage:
		return "operation-stage", true
	default:
		if !validTargetObservationField(field) || !validObservationValueType(valueType) {
			return "", false
		}
		if valueType == ObservationValueNodeID {
			return "node-id", true
		}
		return "target-field:" + string(field) + ":" + string(valueType), true
	}
}

func indexObservationCapabilities(
	capabilities []ObservationCapability,
) (map[ObservationKind]indexedObservationCapability, error) {
	result := make(map[ObservationKind]indexedObservationCapability, len(capabilities))
	for _, capability := range capabilities {
		if !validObservationKind(capability.Kind) {
			return nil, errors.New("RISK_OBSERVATION_CAPABILITY_KIND_INVALID")
		}
		if _, exists := result[capability.Kind]; exists {
			return nil, errors.New("RISK_OBSERVATION_CAPABILITY_DUPLICATE")
		}
		fields := make(map[ObservationField]struct{}, len(capability.Fields))
		for _, field := range capability.Fields {
			if !validObservationField(field) {
				return nil, errors.New("RISK_OBSERVATION_CAPABILITY_FIELD_INVALID")
			}
			if _, exists := fields[field]; exists {
				return nil, errors.New("RISK_OBSERVATION_CAPABILITY_FIELD_DUPLICATE")
			}
			fields[field] = struct{}{}
		}
		fieldTypes := make(map[ObservationField]ObservationValueType, len(capability.FieldTypes))
		for field, valueType := range capability.FieldTypes {
			if _, ok := fields[field]; !ok || !validTargetObservationField(field) ||
				!validObservationValueType(valueType) {
				return nil, errors.New("RISK_OBSERVATION_CAPABILITY_FIELD_TYPE_INVALID")
			}
			fieldTypes[field] = valueType
		}
		for field := range fields {
			if validTargetObservationField(field) && fieldTypes[field] == "" {
				return nil, errors.New("RISK_OBSERVATION_CAPABILITY_FIELD_TYPE_REQUIRED")
			}
		}
		values := make(map[ObservationField]map[string]struct{}, len(capability.Values))
		for field, allowed := range capability.Values {
			if _, ok := fields[field]; !ok || len(allowed) == 0 {
				return nil, errors.New("RISK_OBSERVATION_CAPABILITY_VALUE_FIELD_INVALID")
			}
			set := make(map[string]struct{}, len(allowed))
			for index, value := range allowed {
				if value == "" || len(value) > 128 || strings.TrimSpace(value) != value ||
					(fieldTypes[field] != "" && !validObservationScalar(fieldTypes[field], value)) ||
					(index > 0 && allowed[index-1] >= value) {
					return nil, errors.New("RISK_OBSERVATION_CAPABILITY_VALUE_INVALID")
				}
				set[value] = struct{}{}
			}
			values[field] = set
		}
		result[capability.Kind] = indexedObservationCapability{
			fields: fields, fieldTypes: fieldTypes, values: values,
		}
	}
	return result, nil
}

type indexedObservationCapability struct {
	fields     map[ObservationField]struct{}
	fieldTypes map[ObservationField]ObservationValueType
	values     map[ObservationField]map[string]struct{}
}

func observationMatchesCapability(
	event Observation,
	capability indexedObservationCapability,
) bool {
	if capability.fields == nil {
		return false
	}
	actual := make(map[ObservationField]string)
	if event.Participant != nil {
		actual[ObservationFieldParticipant] = fmt.Sprintf("%s@%d", event.Participant.Node, event.Participant.Incarnation)
		actual[ObservationFieldParticipantNode] = string(event.Participant.Node)
	}
	if event.RelatedParticipant != nil {
		actual[ObservationFieldRelatedParticipant] = fmt.Sprintf(
			"%s@%d", event.RelatedParticipant.Node, event.RelatedParticipant.Incarnation,
		)
		actual[ObservationFieldRelatedNode] = string(event.RelatedParticipant.Node)
	}
	for field, value := range map[ObservationField]string{
		ObservationFieldRequestID:       event.RequestID,
		ObservationFieldParticipantRole: event.ParticipantRole,
		ObservationFieldMessageRole:     event.MessageRole,
		ObservationFieldOperationStage:  event.OperationStage,
	} {
		if value != "" {
			actual[field] = value
		}
	}
	for _, attribute := range event.Attributes {
		if capability.fieldTypes[attribute.Field] != attribute.Type {
			return false
		}
		actual[attribute.Field] = attribute.Value
	}
	for field, value := range actual {
		if _, ok := capability.fields[field]; !ok {
			return false
		}
		if allowed := capability.values[field]; len(allowed) > 0 {
			if _, ok := allowed[value]; !ok {
				return false
			}
		}
	}
	return true
}

func observationAction(kind ObservationKind) (control.ActionKind, bool) {
	switch kind {
	case ObservationWorkloadInvoked:
		return control.ActionInvoke, true
	case ObservationMessageDropped:
		return control.ActionDropMessage, true
	case ObservationMessageDelivered:
		return control.ActionDeliverMessage, true
	case ObservationTemporalFired:
		return control.ActionFireTemporal, true
	case ObservationNodeCrashed:
		return control.ActionCrash, true
	case ObservationNodeRestarted:
		return control.ActionRestart, true
	default:
		return "", false
	}
}

package semantic

import (
	"errors"
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
	Kind   ObservationKind               `json:"kind"`
	Fields []ObservationField            `json:"fields,omitempty"`
	Values map[ObservationField][]string `json:"values,omitempty"`
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
				if _, ok := capability.values[field][value]; !ok {
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

func indexObservationCapabilities(
	capabilities []ObservationCapability,
) (map[ObservationKind]indexedObservationCapability, error) {
	result := make(map[ObservationKind]indexedObservationCapability, len(capabilities))
	for _, capability := range capabilities {
		if _, ok := observationKinds[capability.Kind]; !ok {
			return nil, errors.New("RISK_OBSERVATION_CAPABILITY_KIND_INVALID")
		}
		if _, exists := result[capability.Kind]; exists {
			return nil, errors.New("RISK_OBSERVATION_CAPABILITY_DUPLICATE")
		}
		fields := make(map[ObservationField]struct{}, len(capability.Fields))
		for _, field := range capability.Fields {
			if _, ok := observationFields[field]; !ok {
				return nil, errors.New("RISK_OBSERVATION_CAPABILITY_FIELD_INVALID")
			}
			if _, exists := fields[field]; exists {
				return nil, errors.New("RISK_OBSERVATION_CAPABILITY_FIELD_DUPLICATE")
			}
			fields[field] = struct{}{}
		}
		values := make(map[ObservationField]map[string]struct{}, len(capability.Values))
		for field, allowed := range capability.Values {
			if _, ok := fields[field]; !ok || len(allowed) == 0 {
				return nil, errors.New("RISK_OBSERVATION_CAPABILITY_VALUE_FIELD_INVALID")
			}
			set := make(map[string]struct{}, len(allowed))
			for index, value := range allowed {
				if value == "" || len(value) > 128 || strings.TrimSpace(value) != value ||
					(index > 0 && allowed[index-1] >= value) {
					return nil, errors.New("RISK_OBSERVATION_CAPABILITY_VALUE_INVALID")
				}
				set[value] = struct{}{}
			}
			values[field] = set
		}
		result[capability.Kind] = indexedObservationCapability{fields: fields, values: values}
	}
	return result, nil
}

type indexedObservationCapability struct {
	fields map[ObservationField]struct{}
	values map[ObservationField]map[string]struct{}
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

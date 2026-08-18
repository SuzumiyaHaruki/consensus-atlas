package etcdraftv2

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	ObservationProjectionID                               = "official-etcdraft-v2/observations-v3"
	ObservationRaftTermAdvanced semantic.ObservationKind  = "raft/term-advanced"
	ObservationFieldRaftTerm    semantic.ObservationField = "raft/term"
)

type ObservationProjector struct{}

func (ObservationProjector) ID() string { return ObservationProjectionID }

// Capabilities declares only facts this projector can derive from a trace
// produced by the etcd/raft Adapter.
func (ObservationProjector) Capabilities() []semantic.ObservationCapability {
	node := []semantic.ObservationField{
		semantic.ObservationFieldParticipant,
		semantic.ObservationFieldParticipantNode,
	}
	return []semantic.ObservationCapability{
		{Kind: semantic.ObservationWorkloadInvoked, Fields: append(append([]semantic.ObservationField{}, node...),
			semantic.ObservationFieldRequestID, semantic.ObservationFieldParticipantRole),
			Values: map[semantic.ObservationField][]string{
				semantic.ObservationFieldParticipantRole: {"coordinator"},
			}},
		{Kind: semantic.ObservationMessageDropped,
			Fields: append(append([]semantic.ObservationField{}, node...), semantic.ObservationFieldMessageRole),
			Values: map[semantic.ObservationField][]string{
				semantic.ObservationFieldMessageRole: append([]string(nil), messageTypeHints...),
			}},
		{Kind: semantic.ObservationMessageDelivered,
			Fields: append(append([]semantic.ObservationField{}, node...), semantic.ObservationFieldMessageRole),
			Values: map[semantic.ObservationField][]string{
				semantic.ObservationFieldMessageRole: append([]string(nil), messageTypeHints...),
			}},
		{Kind: semantic.ObservationTemporalFired, Fields: append([]semantic.ObservationField{}, node...)},
		{Kind: semantic.ObservationNodeCrashed, Fields: append([]semantic.ObservationField{}, node...)},
		{Kind: semantic.ObservationNodeRestarted, Fields: append([]semantic.ObservationField{}, node...)},
		{Kind: semantic.ObservationCoordinatorChange, Fields: []semantic.ObservationField{
			semantic.ObservationFieldParticipant, semantic.ObservationFieldParticipantNode,
			semantic.ObservationFieldRelatedParticipant, semantic.ObservationFieldRelatedNode,
			semantic.ObservationFieldOperationStage,
		}, Values: map[semantic.ObservationField][]string{
			semantic.ObservationFieldOperationStage: {"inflight"},
		}},
		{Kind: semantic.ObservationEpochAdvanced, Fields: append([]semantic.ObservationField{}, node...)},
		{Kind: semantic.ObservationDecisionAdvanced, Fields: append([]semantic.ObservationField{}, node...)},
		{Kind: ObservationRaftTermAdvanced,
			Fields: append(append([]semantic.ObservationField{}, node...), ObservationFieldRaftTerm),
			FieldTypes: map[semantic.ObservationField]semantic.ObservationValueType{
				ObservationFieldRaftTerm: semantic.ObservationValueUint,
			}},
	}
}

// Project translates etcd/raft Evidence into the compact common observation
// vocabulary. Message roles remain opaque target labels; internal Raft state
// and application counters do not escape this Adapter-owned boundary.
func (ObservationProjector) Project(trace controlruntime.Trace) (semantic.ObservationHistory, error) {
	events, err := semantic.ProjectRuntimeObservations(trace)
	if err != nil {
		return semantic.ObservationHistory{}, err
	}
	previousEvidence, err := ProjectEvidence(trace.InitialEvidence)
	if err != nil {
		return semantic.ObservationHistory{}, err
	}
	previousCoordinator, _ := evidenceCoordinator(previousEvidence)
	previousTerm := evidenceMaxTerm(previousEvidence)
	previousCommands := evidenceMaxCommands(previousEvidence)
	activeBaseline := 0
	activeWorkload := false

	for _, record := range trace.Records {
		digest, err := control.CanonicalDigest(record)
		if err != nil {
			return semantic.ObservationHistory{}, err
		}
		if record.Action.Kind == control.ActionDropMessage || record.Action.Kind == control.ActionDeliverMessage {
			role, err := etcdraftObservationMessageRole(record)
			if err != nil {
				return semantic.ObservationHistory{}, err
			}
			for index := range events {
				if events[index].Step == record.Step &&
					(events[index].Kind == semantic.ObservationMessageDropped ||
						events[index].Kind == semantic.ObservationMessageDelivered) {
					events[index].MessageRole = role
					break
				}
			}
		}
		if record.Action.Kind == control.ActionInvoke {
			input, err := etcdraftObservationInput(record.Action.Parameters)
			if err != nil {
				return semantic.ObservationHistory{}, err
			}
			for index := range events {
				if events[index].Step == record.Step && events[index].Kind == semantic.ObservationWorkloadInvoked {
					events[index].RequestID = input.RequestID
					break
				}
			}
			activeBaseline = previousCommands
			activeWorkload = true
		}
		if record.Evidence == nil {
			continue
		}
		current, err := ProjectEvidence(*record.Evidence)
		if err != nil {
			return semantic.ObservationHistory{}, err
		}
		currentCoordinator, hasCoordinator := evidenceCoordinator(current)
		currentTerm := evidenceMaxTerm(current)
		currentCommands := evidenceMaxCommands(current)
		if record.Action.Kind == control.ActionInvoke && hasCoordinator && record.Action.Node == currentCoordinator {
			for index := range events {
				if events[index].Step == record.Step && events[index].Kind == semantic.ObservationWorkloadInvoked {
					events[index].ParticipantRole = "coordinator"
					break
				}
			}
		}
		if currentTerm > previousTerm {
			participant := currentCoordinator
			events = append(events, semantic.Observation{
				Kind: semantic.ObservationEpochAdvanced, Step: record.Step,
				SourceDigest: digest, Participant: optionalNodeRef(participant, hasCoordinator),
			})
			events = append(events, semantic.Observation{
				Kind: ObservationRaftTermAdvanced, Step: record.Step,
				SourceDigest: digest, Participant: optionalNodeRef(participant, hasCoordinator),
				Attributes: []semantic.ObservationAttribute{{
					Field: ObservationFieldRaftTerm, Type: semantic.ObservationValueUint,
					Value: strconv.FormatUint(currentTerm, 10),
				}},
			})
			previousTerm = currentTerm
		}
		if hasCoordinator && previousCoordinator.Node != "" && currentCoordinator != previousCoordinator {
			participant, related := currentCoordinator, previousCoordinator
			stage := ""
			if activeWorkload && currentCommands <= activeBaseline {
				stage = "inflight"
			}
			events = append(events, semantic.Observation{
				Kind: semantic.ObservationCoordinatorChange, Step: record.Step,
				SourceDigest: digest, Participant: &participant, RelatedParticipant: &related,
				OperationStage: stage,
			})
		}
		if hasCoordinator {
			previousCoordinator = currentCoordinator
		}
		if currentCommands > previousCommands {
			participant := currentCoordinator
			events = append(events, semantic.Observation{
				Kind: semantic.ObservationDecisionAdvanced, Step: record.Step,
				SourceDigest: digest, Participant: optionalNodeRef(participant, hasCoordinator),
			})
			previousCommands = currentCommands
		}
		if activeWorkload && currentCommands > activeBaseline {
			activeWorkload = false
		}
	}
	return semantic.NewObservationHistoryWithCapabilities(
		ObservationProjectionID, trace, events, (ObservationProjector{}).Capabilities(),
	)
}

func etcdraftObservationMessageRole(record controlruntime.ActionRecord) (string, error) {
	if record.Command == nil {
		return "", fmt.Errorf("ETCDRAFT_OBSERVATION_MESSAGE_COMMAND_REQUIRED")
	}
	envelope, err := control.DecodeAdapterCommand(*record.Command)
	if err != nil {
		return "", err
	}
	if envelope.Item == nil || envelope.Item.ID != record.Action.Item ||
		envelope.Item.Kind != control.ItemMessage || envelope.Item.Message == nil ||
		envelope.Item.Message.TypeHint == "" {
		return "", fmt.Errorf("ETCDRAFT_OBSERVATION_MESSAGE_ITEM_INVALID")
	}
	return envelope.Item.Message.TypeHint, nil
}

func etcdraftObservationInput(parameters json.RawMessage) (Input, error) {
	var invoke control.AdapterInvokeParameters
	if err := json.Unmarshal(parameters, &invoke); err != nil {
		return Input{}, err
	}
	return ProjectInput(invoke.Input)
}

func evidenceCoordinator(evidence Evidence) (control.NodeRef, bool) {
	for _, node := range evidence.Nodes {
		if node.Running && node.Role == "StateLeader" {
			return control.NodeRef{Node: node.Node, Incarnation: node.Incarnation}, true
		}
	}
	return control.NodeRef{}, false
}

func evidenceMaxTerm(evidence Evidence) uint64 {
	var result uint64
	for _, node := range evidence.Nodes {
		if node.Term > result {
			result = node.Term
		}
	}
	return result
}

func evidenceMaxCommands(evidence Evidence) int {
	result := 0
	for _, node := range evidence.Nodes {
		if node.ApplicationCommands > result {
			result = node.ApplicationCommands
		}
	}
	return result
}

func optionalNodeRef(value control.NodeRef, present bool) *control.NodeRef {
	if !present {
		return nil
	}
	result := value
	return &result
}

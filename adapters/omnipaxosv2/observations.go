package omnipaxosv2

import (
	"encoding/json"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	ObservationProjectionID                                  = "omnipaxos-v2/observations-v2"
	ObservationPromiseRaised       semantic.ObservationKind  = "omnipaxos/promise-raised"
	ObservationFieldBallotNode     semantic.ObservationField = "omnipaxos/ballot-node"
	ObservationFieldBallotNumber   semantic.ObservationField = "omnipaxos/ballot-number"
	ObservationFieldBallotPriority semantic.ObservationField = "omnipaxos/ballot-priority"
)

type ObservationProjector struct{}

func (ObservationProjector) ID() string { return ObservationProjectionID }

// Capabilities declares only facts this projector can derive from traces
// admitted by the current OmniPaxos Adapter Manifest. Crash/restart are absent.
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
		{Kind: semantic.ObservationMessageDropped, Fields: append(append([]semantic.ObservationField{}, node...),
			semantic.ObservationFieldOperationStage), Values: map[semantic.ObservationField][]string{
			semantic.ObservationFieldOperationStage: {"inflight"},
		}},
		{Kind: semantic.ObservationMessageDelivered, Fields: append([]semantic.ObservationField{}, node...)},
		{Kind: semantic.ObservationTemporalFired, Fields: append([]semantic.ObservationField{}, node...)},
		{Kind: semantic.ObservationCoordinatorChange, Fields: []semantic.ObservationField{
			semantic.ObservationFieldParticipant, semantic.ObservationFieldParticipantNode,
			semantic.ObservationFieldRelatedParticipant, semantic.ObservationFieldRelatedNode,
		}},
		{Kind: semantic.ObservationEpochAdvanced, Fields: append([]semantic.ObservationField{}, node...)},
		{Kind: semantic.ObservationDecisionAdvanced, Fields: append([]semantic.ObservationField{}, node...)},
		{Kind: ObservationPromiseRaised, Fields: []semantic.ObservationField{
			semantic.ObservationFieldParticipant, semantic.ObservationFieldParticipantNode,
			ObservationFieldBallotNode, ObservationFieldBallotNumber, ObservationFieldBallotPriority,
		}, FieldTypes: map[semantic.ObservationField]semantic.ObservationValueType{
			ObservationFieldBallotNode:     semantic.ObservationValueNodeID,
			ObservationFieldBallotNumber:   semantic.ObservationValueUint,
			ObservationFieldBallotPriority: semantic.ObservationValueUint,
		}},
	}
}

// Project exposes the same common observation vocabulary as the etcd/raft
// Adapter while deriving it from OmniPaxos leader, promise and decision facts.
func (ObservationProjector) Project(trace controlruntime.Trace) (semantic.ObservationHistory, error) {
	events, err := semantic.ProjectRuntimeObservations(trace)
	if err != nil {
		return semantic.ObservationHistory{}, err
	}
	previous, err := ProjectEvidence(trace.InitialEvidence)
	if err != nil {
		return semantic.ObservationHistory{}, err
	}
	previousCoordinator, _ := omnipaxosCoordinator(previous)
	previousPromise := omnipaxosMaxPromise(previous)
	previousDecision := omnipaxosMaxDecision(previous)
	activeBaseline := uint64(0)
	activeWorkload := false

	for _, record := range trace.Records {
		digest, err := control.CanonicalDigest(record)
		if err != nil {
			return semantic.ObservationHistory{}, err
		}
		if record.Action.Kind == control.ActionInvoke {
			input, err := omnipaxosObservationInput(record.Action.Parameters)
			if err != nil {
				return semantic.ObservationHistory{}, err
			}
			for index := range events {
				if events[index].Step == record.Step && events[index].Kind == semantic.ObservationWorkloadInvoked {
					events[index].RequestID = input.RequestID
					break
				}
			}
			activeBaseline = previousDecision
			activeWorkload = true
		}
		if record.Action.Kind == control.ActionDropMessage && activeWorkload {
			for index := range events {
				if events[index].Step == record.Step && events[index].Kind == semantic.ObservationMessageDropped {
					events[index].OperationStage = "inflight"
					break
				}
			}
		}
		if record.Evidence == nil {
			continue
		}
		current, err := ProjectEvidence(*record.Evidence)
		if err != nil {
			return semantic.ObservationHistory{}, err
		}
		currentCoordinator, hasCoordinator := omnipaxosCoordinator(current)
		currentPromise := omnipaxosMaxPromise(current)
		currentDecision := omnipaxosMaxDecision(current)
		if record.Action.Kind == control.ActionInvoke && hasCoordinator && record.Action.Node.Node == currentCoordinator.Node {
			for index := range events {
				if events[index].Step == record.Step && events[index].Kind == semantic.ObservationWorkloadInvoked {
					events[index].ParticipantRole = "coordinator"
					break
				}
			}
		}
		if previousPromise.less(currentPromise) {
			events = append(events, semantic.Observation{
				Kind: semantic.ObservationEpochAdvanced, Step: record.Step, SourceDigest: digest,
				Participant: omnipaxosOptionalNode(currentCoordinator, hasCoordinator),
			})
			attributes := make([]semantic.ObservationAttribute, 0, 3)
			if currentPromise.node != "" {
				attributes = append(attributes, semantic.ObservationAttribute{
					Field: ObservationFieldBallotNode, Type: semantic.ObservationValueNodeID,
					Value: string(currentPromise.node),
				})
			}
			attributes = append(attributes,
				semantic.ObservationAttribute{
					Field: ObservationFieldBallotNumber, Type: semantic.ObservationValueUint,
					Value: strconv.FormatUint(uint64(currentPromise.number), 10),
				},
				semantic.ObservationAttribute{
					Field: ObservationFieldBallotPriority, Type: semantic.ObservationValueUint,
					Value: strconv.FormatUint(uint64(currentPromise.priority), 10),
				},
			)
			events = append(events, semantic.Observation{
				Kind: ObservationPromiseRaised, Step: record.Step, SourceDigest: digest,
				Participant: omnipaxosOptionalNode(currentCoordinator, hasCoordinator),
				Attributes:  attributes,
			})
			previousPromise = currentPromise
		}
		if hasCoordinator && previousCoordinator.Node != "" && currentCoordinator != previousCoordinator {
			participant, related := currentCoordinator, previousCoordinator
			events = append(events, semantic.Observation{
				Kind: semantic.ObservationCoordinatorChange, Step: record.Step, SourceDigest: digest,
				Participant: &participant, RelatedParticipant: &related,
			})
		}
		if hasCoordinator {
			previousCoordinator = currentCoordinator
		}
		if currentDecision > previousDecision {
			participant, participantOK := omnipaxosDecisionParticipant(current, previousDecision)
			events = append(events, semantic.Observation{
				Kind: semantic.ObservationDecisionAdvanced, Step: record.Step, SourceDigest: digest,
				Participant: omnipaxosOptionalNode(participant, participantOK),
			})
			previousDecision = currentDecision
		}
		if activeWorkload && currentDecision > activeBaseline {
			activeWorkload = false
		}
	}
	return semantic.NewObservationHistoryWithCapabilities(
		ObservationProjectionID, trace, events, (ObservationProjector{}).Capabilities(),
	)
}

func omnipaxosObservationInput(parameters json.RawMessage) (Input, error) {
	var invoke control.AdapterInvokeParameters
	if err := json.Unmarshal(parameters, &invoke); err != nil {
		return Input{}, err
	}
	return ProjectInput(invoke.Input)
}

func omnipaxosCoordinator(evidence Evidence) (control.NodeRef, bool) {
	for _, node := range evidence.Nodes {
		if node.Node != "" && node.Leader == node.Node {
			return control.NodeRef{Node: node.Node, Incarnation: 1}, true
		}
	}
	return control.NodeRef{}, false
}

type omnipaxosPromise struct {
	number   uint32
	priority uint32
	node     control.NodeID
}

func (left omnipaxosPromise) less(right omnipaxosPromise) bool {
	if left.number != right.number {
		return left.number < right.number
	}
	if left.priority != right.priority {
		return left.priority < right.priority
	}
	return left.node < right.node
}

func omnipaxosMaxPromise(evidence Evidence) omnipaxosPromise {
	result := omnipaxosPromise{}
	for _, node := range evidence.Nodes {
		current := omnipaxosPromise{node.PromiseNumber, node.PromisePriority, node.PromiseNode}
		if result.less(current) {
			result = current
		}
	}
	return result
}

func omnipaxosMaxDecision(evidence Evidence) uint64 {
	var result uint64
	for _, node := range evidence.Nodes {
		if node.DecidedIndex > result {
			result = node.DecidedIndex
		}
	}
	return result
}

func omnipaxosDecisionParticipant(evidence Evidence, previous uint64) (control.NodeRef, bool) {
	for _, node := range evidence.Nodes {
		if node.DecidedIndex > previous {
			return control.NodeRef{Node: node.Node, Incarnation: 1}, true
		}
	}
	return control.NodeRef{}, false
}

func omnipaxosOptionalNode(value control.NodeRef, present bool) *control.NodeRef {
	if !present {
		return nil
	}
	result := value
	return &result
}

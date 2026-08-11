package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
)

const inputSchema = "consensus-atlas/fixture-input/v1"

type Input struct {
	Operation string         `json:"operation"`
	Target    control.NodeID `json:"target,omitempty"`
	Value     string         `json:"value,omitempty"`
	Delay     uint64         `json:"delay,omitempty"`
}

const (
	OpEmitMessage        = "emit-message"
	OpEmitDurableMessage = "emit-durable-message"
	OpSleep              = "sleep"
	OpOneShot            = "one-shot"
	OpCallback           = "callback"
)

func InputPayload(input Input) (control.PayloadEnvelope, error) {
	return control.NewJSONPayload(inputSchema, input)
}

type nodeState struct {
	Ref          control.NodeRef `json:"ref"`
	Running      bool            `json:"running"`
	PulseCount   uint64          `json:"pulse_count"`
	DurableCount uint64          `json:"durable_count"`
	RandomOffset int             `json:"random_offset"`
}

type commandEnvelope struct {
	LogicalTime uint64                `json:"logical_time"`
	Parameters  json.RawMessage       `json:"parameters,omitempty"`
	Item        *control.ProducedItem `json:"item,omitempty"`
}

type invokeParameters struct {
	Input control.PayloadEnvelope `json:"input"`
}

type Adapter struct {
	nodes       map[control.NodeID]nodeState
	entropy     *controlentropy.Provider
	pending     *control.AdapterCommand
	bootstrap   bool
	yieldSeq    uint64
	itemSeq     uint64
	logicalTime uint64
	current     control.YieldID
	emissions   map[control.YieldID]control.Emission
}

func New() *Adapter {
	return &Adapter{}
}

func (*Adapter) Manifest(context.Context) (control.AdapterManifest, error) {
	configurationDigest, err := control.CanonicalDigest(struct {
		Nodes       []control.NodeID `json:"nodes"`
		PulsePeriod uint64           `json:"pulse_period"`
	}{Nodes: []control.NodeID{"n1", "n2"}, PulsePeriod: 1})
	if err != nil {
		return control.AdapterManifest{}, err
	}
	return control.AdapterManifest{
		SchemaVersion: control.SchemaVersion,
		AdapterID:     "control-fixture-v1", ImplementationID: "protocol-free-fixture/v1",
		BuildID: "in-tree-fixture-v1", ConfigurationDigest: configurationDigest,
		Nodes: []control.NodeID{"n1", "n2"},
		Capabilities: control.CapabilityManifest{
			Actions: []control.ActionKind{
				control.ActionDropMessage, control.ActionDuplicateMessage, control.ActionPartition,
				control.ActionHeal, control.ActionInvoke, control.ActionDeliverMessage,
				control.ActionFireTemporal, control.ActionCrash, control.ActionRestart,
				control.ActionCompleteEffect, control.ActionFailEffect, control.ActionCompleteCallback,
			},
			Items: []control.ItemKind{
				control.ItemMessage, control.ItemTemporal, control.ItemEffect, control.ItemCallback,
				control.ItemClientResult, control.ItemObservation,
			},
			Temporal: control.TemporalCapability{
				Kinds: []control.TemporalKind{
					control.TemporalOneShotTimer, control.TemporalPeriodicPulse, control.TemporalSleepWakeup,
				},
				ClockError: 0,
			},
			Entropy: control.EntropyCapability{
				Provider: "fixture-native", Algorithm: controlentropy.Algorithm,
				DomainPolicy: "node-incarnation-domain", ResetPolicy: "per-incarnation", StrictReplay: true,
			},
			CrashModes: []string{"power-loss"}, EffectKinds: []string{"persist"},
			StrictYield: true, StrictReplay: true, DurableCheckpoints: true,
		},
		EvidenceSchemas: []string{"consensus-atlas/fixture-evidence/v1"},
	}, nil
}

func (adapter *Adapter) Reset(_ context.Context, seed []byte) error {
	provider, err := controlentropy.New(seed)
	if err != nil {
		return err
	}
	adapter.nodes = map[control.NodeID]nodeState{
		"n1": {Ref: control.NodeRef{Node: "n1", Incarnation: 1}, Running: true},
		"n2": {Ref: control.NodeRef{Node: "n2", Incarnation: 1}, Running: true},
	}
	adapter.entropy = provider
	adapter.pending = nil
	adapter.bootstrap = true
	adapter.yieldSeq = 0
	adapter.itemSeq = 0
	adapter.logicalTime = 0
	adapter.current = ""
	adapter.emissions = make(map[control.YieldID]control.Emission)
	return nil
}

func (adapter *Adapter) Check(_ context.Context, command control.AdapterCommand) (control.CommandEligibility, error) {
	if err := command.Payload.Validate(); err != nil {
		return control.CommandEligibility{}, err
	}
	state, known := adapter.nodes[command.Node.Node]
	eligible := false
	reason := "FIXTURE_COMMAND_UNSUPPORTED"
	switch command.Kind {
	case control.ActionDropMessage:
		eligible = true
	case control.ActionInvoke, control.ActionDeliverMessage, control.ActionFireTemporal,
		control.ActionCompleteEffect, control.ActionFailEffect, control.ActionCompleteCallback:
		eligible = known && state.Running && state.Ref == command.Node
		reason = "FIXTURE_NODE_NOT_RUNNING"
	case control.ActionCrash:
		eligible = known && state.Running && state.Ref == command.Node
		reason = "FIXTURE_NODE_NOT_RUNNING"
	case control.ActionRestart:
		eligible = known && !state.Running && command.Node.Incarnation == state.Ref.Incarnation+1
		reason = "FIXTURE_RESTART_INELIGIBLE"
	}
	if eligible {
		return control.CommandEligibility{Eligible: true}, nil
	}
	return control.CommandEligibility{ReasonCode: reason}, nil
}

func (adapter *Adapter) Submit(_ context.Context, command control.AdapterCommand) error {
	if adapter.pending != nil {
		return errors.New("FIXTURE_COMMAND_ALREADY_PENDING")
	}
	copyCommand := command
	copyCommand.Payload.Bytes = append([]byte(nil), command.Payload.Bytes...)
	adapter.pending = &copyCommand
	return nil
}

func (*Adapter) CheckRuntimeAction(_ context.Context, action control.Action) (control.CommandEligibility, error) {
	switch action.Kind {
	case control.ActionDuplicateMessage, control.ActionPartition, control.ActionHeal:
		return control.CommandEligibility{Eligible: true}, nil
	default:
		return control.CommandEligibility{ReasonCode: "FIXTURE_RUNTIME_ACTION_UNSUPPORTED"}, nil
	}
}

func (*Adapter) ApplyRuntimeAction(_ context.Context, action control.Action) error {
	switch action.Kind {
	case control.ActionDuplicateMessage, control.ActionPartition, control.ActionHeal:
		return nil
	default:
		return fmt.Errorf("FIXTURE_RUNTIME_ACTION_UNSUPPORTED: %s", action.Kind)
	}
}

func (adapter *Adapter) RunUntilYield(_ context.Context) (control.Yield, error) {
	if adapter.bootstrap {
		adapter.bootstrap = false
		items := make([]control.ProducedItem, 0, len(adapter.nodes))
		nodes := []control.NodeID{"n1", "n2"}
		for _, node := range nodes {
			state := adapter.nodes[node]
			offset, err := adapter.entropy.Intn(adapter.domain(state.Ref), 10)
			if err != nil {
				return control.Yield{}, err
			}
			state.RandomOffset = offset
			adapter.nodes[node] = state
			item, err := adapter.periodicPulse(state.Ref, 1)
			if err != nil {
				return control.Yield{}, err
			}
			items = append(items, item)
		}
		return adapter.finishYield(control.YieldStable, "bootstrap", items)
	}
	if adapter.pending == nil {
		return control.Yield{}, errors.New("FIXTURE_COMMAND_REQUIRED")
	}
	command := *adapter.pending
	adapter.pending = nil
	envelope, err := decodeCommand(command)
	if err != nil {
		return control.Yield{}, err
	}
	adapter.logicalTime = envelope.LogicalTime
	items := make([]control.ProducedItem, 0)
	yieldKind := control.YieldStable
	switch command.Kind {
	case control.ActionDropMessage:
	case control.ActionInvoke:
		produced, err := adapter.invoke(command.Node, envelope.Parameters)
		if err != nil {
			return control.Yield{}, err
		}
		items = append(items, produced...)
	case control.ActionDeliverMessage:
		item, err := adapter.observation(command.Node, "message-delivered", string(command.Item))
		if err != nil {
			return control.Yield{}, err
		}
		items = append(items, item)
	case control.ActionFireTemporal:
		if envelope.Item == nil || envelope.Item.Temporal == nil {
			return control.Yield{}, errors.New("FIXTURE_TEMPORAL_ITEM_REQUIRED")
		}
		state := adapter.nodes[command.Node.Node]
		state.PulseCount++
		adapter.nodes[command.Node.Node] = state
		observation, err := adapter.observation(command.Node, "temporal-fired", string(envelope.Item.Temporal.Kind))
		if err != nil {
			return control.Yield{}, err
		}
		items = append(items, observation)
		if envelope.Item.Temporal.Kind == control.TemporalPeriodicPulse {
			next, err := adapter.periodicPulse(command.Node, adapter.logicalTime+envelope.Item.Temporal.Period)
			if err != nil {
				return control.Yield{}, err
			}
			items = append(items, next)
		}
	case control.ActionCrash:
		state := adapter.nodes[command.Node.Node]
		state.Running = false
		adapter.nodes[command.Node.Node] = state
		yieldKind = control.YieldTerminal
	case control.ActionRestart:
		state := adapter.nodes[command.Node.Node]
		state.Ref = command.Node
		state.Running = true
		offset, err := adapter.entropy.Intn(adapter.domain(state.Ref), 10)
		if err != nil {
			return control.Yield{}, err
		}
		state.RandomOffset = offset
		adapter.nodes[command.Node.Node] = state
		pulse, err := adapter.periodicPulse(state.Ref, adapter.logicalTime+1)
		if err != nil {
			return control.Yield{}, err
		}
		items = append(items, pulse)
	case control.ActionCompleteEffect:
		state := adapter.nodes[command.Node.Node]
		state.DurableCount++
		adapter.nodes[command.Node.Node] = state
		observation, err := adapter.observation(command.Node, "effect-completed", string(command.Item))
		if err != nil {
			return control.Yield{}, err
		}
		items = append(items, observation)
	case control.ActionFailEffect:
		observation, err := adapter.observation(command.Node, "effect-failed", string(command.Item))
		if err != nil {
			return control.Yield{}, err
		}
		items = append(items, observation)
	case control.ActionCompleteCallback:
		observation, err := adapter.observation(command.Node, "callback-completed", string(command.Item))
		if err != nil {
			return control.Yield{}, err
		}
		items = append(items, observation)
	default:
		return control.Yield{}, fmt.Errorf("FIXTURE_COMMAND_UNSUPPORTED: %s", command.Kind)
	}
	return adapter.finishYield(yieldKind, string(command.ID), items)
}

func (adapter *Adapter) Collect(_ context.Context, yieldID control.YieldID) (control.Emission, error) {
	emission, ok := adapter.emissions[yieldID]
	if !ok {
		return control.Emission{}, fmt.Errorf("FIXTURE_YIELD_UNKNOWN: %s", yieldID)
	}
	encoded, err := json.Marshal(emission)
	if err != nil {
		return control.Emission{}, err
	}
	var result control.Emission
	if err := json.Unmarshal(encoded, &result); err != nil {
		return control.Emission{}, err
	}
	return result, nil
}

func (adapter *Adapter) SnapshotEvidence(context.Context) (control.EvidenceEnvelope, error) {
	if adapter.current == "" {
		return control.EvidenceEnvelope{}, errors.New("FIXTURE_YIELD_REQUIRED")
	}
	payload, err := control.NewJSONPayload("consensus-atlas/fixture-evidence/v1", adapter.snapshot())
	if err != nil {
		return control.EvidenceEnvelope{}, err
	}
	return control.EvidenceEnvelope{Yield: adapter.current, Payload: payload}, nil
}

func (adapter *Adapter) SnapshotEntropy(context.Context) (control.EntropyAuditEnvelope, error) {
	if adapter.current == "" || adapter.entropy == nil {
		return control.EntropyAuditEnvelope{}, errors.New("FIXTURE_ENTROPY_UNAVAILABLE")
	}
	return adapter.entropy.SnapshotAudit(adapter.current)
}

func (adapter *Adapter) invoke(owner control.NodeRef, raw json.RawMessage) ([]control.ProducedItem, error) {
	var parameters invokeParameters
	if err := json.Unmarshal(raw, &parameters); err != nil {
		return nil, err
	}
	if err := parameters.Input.Validate(); err != nil {
		return nil, err
	}
	if parameters.Input.SchemaVersion != inputSchema {
		return nil, fmt.Errorf("FIXTURE_INPUT_SCHEMA_MISMATCH: %s", parameters.Input.SchemaVersion)
	}
	var input Input
	if err := json.Unmarshal(parameters.Input.Bytes, &input); err != nil {
		return nil, err
	}
	switch input.Operation {
	case OpEmitMessage:
		message, err := adapter.message(owner, input.Target, input.Value, nil)
		if err != nil {
			return nil, err
		}
		return []control.ProducedItem{message}, nil
	case OpEmitDurableMessage:
		effect, err := adapter.effect(owner, input.Value)
		if err != nil {
			return nil, err
		}
		message, err := adapter.message(owner, input.Target, input.Value, []control.ItemID{effect.ID})
		if err != nil {
			return nil, err
		}
		return []control.ProducedItem{effect, message}, nil
	case OpSleep:
		wake, err := adapter.sleep(owner, adapter.logicalTime+input.Delay)
		if err != nil {
			return nil, err
		}
		return []control.ProducedItem{wake}, nil
	case OpOneShot:
		timer, err := adapter.oneShot(owner, adapter.logicalTime+input.Delay)
		if err != nil {
			return nil, err
		}
		return []control.ProducedItem{timer}, nil
	case OpCallback:
		callback, err := adapter.callback(owner, input.Value)
		if err != nil {
			return nil, err
		}
		return []control.ProducedItem{callback}, nil
	default:
		return nil, fmt.Errorf("FIXTURE_INPUT_UNSUPPORTED: %s", input.Operation)
	}
}

func (adapter *Adapter) periodicPulse(owner control.NodeRef, deadline uint64) (control.ProducedItem, error) {
	payload, err := control.NewPayload("consensus-atlas/fixture-callback/v1", "json", []byte(`{"operation":"tick"}`))
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, temporalID, err := adapter.nextIDs("pulse")
	if err != nil {
		return control.ProducedItem{}, err
	}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemTemporal, Owner: owner,
		Temporal: &control.TemporalItem{
			ID: temporalID, Kind: control.TemporalPeriodicPulse, Owner: owner,
			ClockDomain: "fixture-global", Deadline: deadline, Period: 1, Callback: payload,
		},
	}, nil
}

func (adapter *Adapter) sleep(owner control.NodeRef, deadline uint64) (control.ProducedItem, error) {
	payload, err := control.NewPayload("consensus-atlas/fixture-callback/v1", "json", []byte(`{"operation":"wake"}`))
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, temporalID, err := adapter.nextIDs("sleep")
	if err != nil {
		return control.ProducedItem{}, err
	}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemTemporal, Owner: owner,
		Temporal: &control.TemporalItem{
			ID: temporalID, Kind: control.TemporalSleepWakeup, Owner: owner,
			ClockDomain: "fixture-global", Deadline: deadline, Callback: payload,
		},
	}, nil
}

func (adapter *Adapter) oneShot(owner control.NodeRef, deadline uint64) (control.ProducedItem, error) {
	payload, err := control.NewPayload("consensus-atlas/fixture-callback/v1", "json", []byte(`{"operation":"timer"}`))
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, temporalID, err := adapter.nextIDs("one-shot")
	if err != nil {
		return control.ProducedItem{}, err
	}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemTemporal, Owner: owner,
		Temporal: &control.TemporalItem{
			ID: temporalID, Kind: control.TemporalOneShotTimer, Owner: owner,
			ClockDomain: "fixture-global", Deadline: deadline, Callback: payload,
		},
	}, nil
}

func (adapter *Adapter) message(owner control.NodeRef, target control.NodeID, value string, dependencies []control.ItemID) (control.ProducedItem, error) {
	if _, ok := adapter.nodes[target]; !ok {
		return control.ProducedItem{}, fmt.Errorf("FIXTURE_TARGET_UNKNOWN: %s", target)
	}
	payload, err := control.NewPayload("consensus-atlas/fixture-message/v1", "bytes", []byte(value))
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, messageID, err := adapter.nextIDs("message")
	if err != nil {
		return control.ProducedItem{}, err
	}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemMessage, Owner: owner, Dependencies: dependencies,
		Message: &control.MessageEnvelope{
			ID: control.MessageID(messageID), Source: owner, Target: target, Payload: payload,
		},
	}, nil
}

func (adapter *Adapter) effect(owner control.NodeRef, value string) (control.ProducedItem, error) {
	payload, err := control.NewPayload("consensus-atlas/fixture-effect/v1", "bytes", []byte(value))
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, effectID, err := adapter.nextIDs("effect")
	if err != nil {
		return control.ProducedItem{}, err
	}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemEffect, Owner: owner,
		Effect: &control.HostEffect{
			ID: control.EffectID(effectID), Kind: "persist", Owner: owner, Request: payload,
			AllowedResults: []string{"ok"}, AllowedFailures: []string{"io-error"},
			Durability: control.DurabilityDurable,
		},
	}, nil
}

func (adapter *Adapter) callback(owner control.NodeRef, value string) (control.ProducedItem, error) {
	payload, err := control.NewPayload("consensus-atlas/fixture-callback/v1", "bytes", []byte(value))
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, callbackID, err := adapter.nextIDs("callback")
	if err != nil {
		return control.ProducedItem{}, err
	}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemCallback, Owner: owner,
		Callback: &control.HostCallback{
			ID: control.CallbackID(callbackID), Kind: "fixture-callback", Owner: owner,
			Request: payload, AllowedResults: []string{"ok"},
		},
	}, nil
}

func (adapter *Adapter) observation(owner control.NodeRef, kind, value string) (control.ProducedItem, error) {
	payload, err := control.NewPayload("consensus-atlas/fixture-observation/v1", "bytes", []byte(value))
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, observationID, err := adapter.nextIDs("observation")
	if err != nil {
		return control.ProducedItem{}, err
	}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemObservation, Owner: owner,
		Observation: &control.TypedObservation{
			ID: control.ObservationID(observationID), Owner: owner, Kind: kind, Payload: payload,
		},
	}, nil
}

func (adapter *Adapter) nextIDs(kind string) (control.ItemID, control.TemporalID, error) {
	adapter.itemSeq++
	ordinal := strconv.FormatUint(adapter.itemSeq, 10)
	item, err := control.StableID("fixture-item", kind, ordinal)
	if err != nil {
		return "", "", err
	}
	typed, err := control.StableID("fixture-typed", kind, ordinal)
	if err != nil {
		return "", "", err
	}
	return control.ItemID(item), control.TemporalID(typed), nil
}

func (adapter *Adapter) finishYield(kind control.YieldKind, cause string, items []control.ProducedItem) (control.Yield, error) {
	adapter.yieldSeq++
	id, err := control.StableID("fixture-yield", strconv.FormatUint(adapter.yieldSeq, 10), cause)
	if err != nil {
		return control.Yield{}, err
	}
	stateDigest, err := control.CanonicalDigest(adapter.snapshot())
	if err != nil {
		return control.Yield{}, err
	}
	yield := control.Yield{ID: control.YieldID(id), Kind: kind, StateDigest: stateDigest}
	emission, err := (control.Emission{Yield: yield.ID, Items: items}).Seal()
	if err != nil {
		return control.Yield{}, err
	}
	adapter.current = yield.ID
	adapter.emissions[yield.ID] = emission
	return yield, nil
}

func (adapter *Adapter) snapshot() any {
	nodes := make([]nodeState, 0, len(adapter.nodes))
	for _, node := range adapter.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Ref.Node < nodes[j].Ref.Node })
	return struct {
		LogicalTime uint64      `json:"logical_time"`
		YieldSeq    uint64      `json:"yield_seq"`
		ItemSeq     uint64      `json:"item_seq"`
		Nodes       []nodeState `json:"nodes"`
	}{adapter.logicalTime, adapter.yieldSeq, adapter.itemSeq, nodes}
}

func (adapter *Adapter) domain(owner control.NodeRef) controlentropy.Domain {
	return controlentropy.Domain{
		Namespace: "protocol-free-fixture/v1", Node: owner.Node,
		Incarnation: owner.Incarnation, ID: "election-timeout",
	}
}

func decodeCommand(command control.AdapterCommand) (commandEnvelope, error) {
	if err := command.Payload.Validate(); err != nil {
		return commandEnvelope{}, err
	}
	var envelope commandEnvelope
	if err := json.Unmarshal(command.Payload.Bytes, &envelope); err != nil {
		return commandEnvelope{}, err
	}
	return envelope, nil
}

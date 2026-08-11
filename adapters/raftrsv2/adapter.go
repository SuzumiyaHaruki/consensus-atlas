package raftrsv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
)

type Adapter struct {
	config       Config
	workerDigest string
	worker       *workerClient
	entropy      *controlentropy.Provider
	logicalTime  uint64
	yieldSeq     uint64
	itemSeq      uint64
	pending      *control.AdapterCommand
	current      control.YieldID
	emission     control.Emission
	last         workerResponse
	ready        map[control.NodeID]readyBinding
	pulses       map[control.NodeID]control.ItemID
}

func New(config Config) (*Adapter, error) {
	if config.WorkerPath == "" {
		return nil, errors.New("RAFT_RS_WORKER_PATH_REQUIRED")
	}
	info, err := os.Stat(config.WorkerPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("RAFT_RS_WORKER_NOT_EXECUTABLE")
	}
	encoded, err := os.ReadFile(config.WorkerPath)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	return &Adapter{config: config, workerDigest: hex.EncodeToString(digest[:])}, nil
}

func (adapter *Adapter) Manifest(context.Context) (control.AdapterManifest, error) {
	configurationDigest, err := control.CanonicalDigest(struct {
		WorkerDigest string   `json:"worker_digest"`
		Nodes        []string `json:"nodes"`
		Heartbeat    uint64   `json:"heartbeat_tick"`
		Election     []uint64 `json:"fixed_election_ticks"`
	}{adapter.workerDigest, []string{"n1", "n2", "n3"}, 1, []uint64{5, 12, 13}})
	if err != nil {
		return control.AdapterManifest{}, err
	}
	return control.AdapterManifest{
		SchemaVersion: control.SchemaVersion,
		AdapterID:     adapterID, ImplementationID: implementation,
		BuildID: "sha256:" + adapter.workerDigest, ConfigurationDigest: configurationDigest,
		Nodes: []control.NodeID{"n1", "n2", "n3"},
		Capabilities: control.CapabilityManifest{
			Actions: []control.ActionKind{
				control.ActionInvoke, control.ActionDropMessage, control.ActionDeliverMessage,
				control.ActionFireTemporal, control.ActionCompleteEffect,
			},
			Items: []control.ItemKind{
				control.ItemMessage, control.ItemTemporal, control.ItemEffect, control.ItemClientResult,
			},
			Temporal: control.TemporalCapability{
				Kinds: []control.TemporalKind{control.TemporalPeriodicPulse}, ClockError: 0,
			},
			Entropy: control.EntropyCapability{
				Provider:  "harness-empty-tape/sut-fixed-single-value-election-range",
				Algorithm: controlentropy.Algorithm, DomainPolicy: "no-harness-draws",
				ResetPolicy: "fresh-worker-process", StrictReplay: false,
			},
			EffectKinds: []string{effectReadyComplete}, StrictYield: true, StrictReplay: true,
		},
		EvidenceSchemas: []string{evidenceSchema},
	}, nil
}

func (adapter *Adapter) Reset(ctx context.Context, seed []byte) error {
	if adapter.worker != nil {
		if err := adapter.worker.close(); err != nil {
			return err
		}
	}
	provider, err := controlentropy.New(seed)
	if err != nil {
		return err
	}
	worker, err := startWorker(ctx, adapter.config.WorkerPath)
	if err != nil {
		return err
	}
	response, err := worker.call(workerRequest{Op: "reset"})
	if err != nil {
		_ = worker.close()
		return err
	}
	adapter.worker = worker
	adapter.entropy = provider
	adapter.logicalTime = 0
	adapter.yieldSeq = 0
	adapter.itemSeq = 0
	adapter.pending = nil
	adapter.current = ""
	adapter.emission = control.Emission{}
	adapter.ready = make(map[control.NodeID]readyBinding)
	adapter.pulses = make(map[control.NodeID]control.ItemID)
	adapter.last = response
	return adapter.validateResponse(response)
}

func (adapter *Adapter) Check(_ context.Context, command control.AdapterCommand) (control.CommandEligibility, error) {
	if adapter.worker == nil || adapter.pending != nil {
		return control.CommandEligibility{ReasonCode: "RAFT_RS_NOT_STABLE"}, nil
	}
	envelope, err := control.DecodeAdapterCommand(command)
	if err != nil {
		return control.CommandEligibility{}, err
	}
	switch command.Kind {
	case control.ActionInvoke:
		var parameters control.AdapterInvokeParameters
		if err := json.Unmarshal(envelope.Parameters, &parameters); err != nil {
			return control.CommandEligibility{}, err
		}
		_, inputErr := decodeInput(parameters.Input)
		node := adapter.node(command.Node.Node)
		if inputErr != nil || node == nil || node.Role != "Leader" || command.Node.Incarnation != 1 ||
			adapter.ready[command.Node.Node].Item != "" {
			return control.CommandEligibility{ReasonCode: "RAFT_RS_INVOKE_NOT_ELIGIBLE"}, nil
		}
	case control.ActionDropMessage:
		if !validMessageItem(command, envelope.Item) {
			return control.CommandEligibility{ReasonCode: "RAFT_RS_MESSAGE_NOT_DROPPABLE"}, nil
		}
	case control.ActionDeliverMessage:
		if !validMessageItem(command, envelope.Item) || envelope.Item.Message.Target != command.Node.Node ||
			command.Node.Incarnation != 1 || envelope.Item.Message.Payload.SchemaVersion != messageSchema ||
			envelope.Item.Message.Payload.Encoding != "protobuf" {
			return control.CommandEligibility{ReasonCode: "RAFT_RS_MESSAGE_NOT_DELIVERABLE"}, nil
		}
		if _, ok := raftID(command.Node.Node); !ok {
			return control.CommandEligibility{ReasonCode: "RAFT_RS_MESSAGE_TARGET_UNKNOWN"}, nil
		}
	case control.ActionFireTemporal:
		if envelope.Item == nil || envelope.Item.Temporal == nil || envelope.Item.ID != command.Item ||
			envelope.Item.Owner != command.Node || command.Node.Incarnation != 1 ||
			adapter.pulses[command.Node.Node] != command.Item || adapter.ready[command.Node.Node].Item != "" {
			return control.CommandEligibility{ReasonCode: "RAFT_RS_PULSE_NOT_CURRENT"}, nil
		}
	case control.ActionCompleteEffect:
		binding := adapter.ready[command.Node.Node]
		if envelope.Item == nil || envelope.Item.Effect == nil || envelope.Item.ID != command.Item ||
			envelope.Item.Owner != command.Node || command.Node.Incarnation != 1 || binding.Item != command.Item ||
			envelope.Item.Effect.Kind != effectReadyComplete {
			return control.CommandEligibility{ReasonCode: "RAFT_RS_READY_NOT_CURRENT"}, nil
		}
		var request readyRequest
		var result control.AdapterResultParameters
		if err := json.Unmarshal(envelope.Item.Effect.Request.Bytes, &request); err != nil {
			return control.CommandEligibility{}, err
		}
		if err := json.Unmarshal(envelope.Parameters, &result); err != nil {
			return control.CommandEligibility{}, err
		}
		if request.ReadyID != binding.ReadyID || request.Digest != binding.Digest ||
			result.Result != "persisted-applied-advanced" {
			return control.CommandEligibility{ReasonCode: "RAFT_RS_READY_IDENTITY_MISMATCH"}, nil
		}
	default:
		return control.CommandEligibility{ReasonCode: "RAFT_RS_ACTION_UNSUPPORTED"}, nil
	}
	return control.CommandEligibility{Eligible: true}, nil
}

func (adapter *Adapter) Submit(_ context.Context, command control.AdapterCommand) error {
	if adapter.pending != nil {
		return errors.New("RAFT_RS_COMMAND_ALREADY_PENDING")
	}
	copyCommand := command
	copyCommand.Payload.Bytes = append([]byte(nil), command.Payload.Bytes...)
	adapter.pending = &copyCommand
	return nil
}

func (*Adapter) CheckRuntimeAction(context.Context, control.Action) (control.CommandEligibility, error) {
	return control.CommandEligibility{ReasonCode: "RAFT_RS_RUNTIME_ACTION_UNSUPPORTED"}, nil
}

func (*Adapter) ApplyRuntimeAction(_ context.Context, action control.Action) error {
	return fmt.Errorf("RAFT_RS_RUNTIME_ACTION_UNSUPPORTED: %s", action.Kind)
}

func (adapter *Adapter) RunUntilYield(_ context.Context) (control.Yield, error) {
	if adapter.yieldSeq == 0 {
		items, err := adapter.capture(adapter.last, "bootstrap", "")
		if err != nil {
			return control.Yield{}, err
		}
		return adapter.finishYield("bootstrap", items)
	}
	if adapter.pending == nil {
		return control.Yield{}, errors.New("RAFT_RS_COMMAND_REQUIRED")
	}
	command := *adapter.pending
	adapter.pending = nil
	envelope, err := control.DecodeAdapterCommand(command)
	if err != nil {
		return control.Yield{}, err
	}
	adapter.logicalTime = envelope.LogicalTime
	response := adapter.last
	dependency := control.ItemID("")
	switch command.Kind {
	case control.ActionInvoke:
		var parameters control.AdapterInvokeParameters
		if err := json.Unmarshal(envelope.Parameters, &parameters); err != nil {
			return control.Yield{}, err
		}
		input, err := decodeInput(parameters.Input)
		if err != nil {
			return control.Yield{}, err
		}
		proposal, err := encodeProposal(input, command.Node)
		if err != nil {
			return control.Yield{}, err
		}
		id, _ := raftID(command.Node.Node)
		response, err = adapter.worker.call(workerRequest{Op: "propose", Node: id, Data: proposal})
	case control.ActionDropMessage:
	case control.ActionDeliverMessage:
		id, _ := raftID(command.Node.Node)
		response, err = adapter.worker.call(workerRequest{
			Op: "step", Node: id, Message: envelope.Item.Message.Payload.Bytes,
		})
	case control.ActionFireTemporal:
		id, _ := raftID(command.Node.Node)
		delete(adapter.pulses, command.Node.Node)
		response, err = adapter.worker.call(workerRequest{Op: "tick", Node: id})
	case control.ActionCompleteEffect:
		binding := adapter.ready[command.Node.Node]
		delete(adapter.ready, command.Node.Node)
		dependency = command.Item
		id, _ := raftID(command.Node.Node)
		response, err = adapter.worker.call(workerRequest{Op: "complete-ready", Node: id, ReadyID: binding.ReadyID})
	default:
		err = fmt.Errorf("RAFT_RS_COMMAND_UNSUPPORTED: %s", command.Kind)
	}
	if err != nil {
		return control.Yield{}, err
	}
	if err := adapter.validateResponse(response); err != nil {
		return control.Yield{}, err
	}
	items, err := adapter.capture(response, string(command.ID), dependency)
	if err != nil {
		return control.Yield{}, err
	}
	response.Messages = nil
	response.Commits = nil
	adapter.last = response
	return adapter.finishYield(string(command.ID), items)
}

func (adapter *Adapter) Collect(_ context.Context, yield control.YieldID) (control.Emission, error) {
	if adapter.emission.Yield != yield {
		return control.Emission{}, fmt.Errorf("RAFT_RS_YIELD_UNKNOWN: %s", yield)
	}
	encoded, err := json.Marshal(adapter.emission)
	if err != nil {
		return control.Emission{}, err
	}
	var copyEmission control.Emission
	if err := json.Unmarshal(encoded, &copyEmission); err != nil {
		return control.Emission{}, err
	}
	return copyEmission, nil
}

func (adapter *Adapter) SnapshotEvidence(context.Context) (control.EvidenceEnvelope, error) {
	if adapter.current == "" {
		return control.EvidenceEnvelope{}, errors.New("RAFT_RS_YIELD_REQUIRED")
	}
	payload, err := control.NewJSONPayload(evidenceSchema, adapter.snapshot())
	if err != nil {
		return control.EvidenceEnvelope{}, err
	}
	return control.EvidenceEnvelope{Yield: adapter.current, Payload: payload}, nil
}

func (adapter *Adapter) SnapshotEntropy(context.Context) (control.EntropyAuditEnvelope, error) {
	if adapter.current == "" || adapter.entropy == nil {
		return control.EntropyAuditEnvelope{}, errors.New("RAFT_RS_ENTROPY_UNAVAILABLE")
	}
	return adapter.entropy.SnapshotAudit(adapter.current)
}

func (adapter *Adapter) Close() error {
	if adapter.worker == nil {
		return nil
	}
	err := adapter.worker.close()
	adapter.worker = nil
	return err
}

func (adapter *Adapter) capture(response workerResponse, cause string, dependency control.ItemID) ([]control.ProducedItem, error) {
	var items []control.ProducedItem
	for _, node := range response.Nodes {
		name := nodeNames[node.ID]
		binding := adapter.ready[name]
		if node.Ready == nil {
			if binding.Item != "" {
				return nil, fmt.Errorf("RAFT_RS_READY_DISAPPEARED: %s", name)
			}
			continue
		}
		if binding.Item != "" {
			if binding.ReadyID != node.Ready.ReadyID || binding.Digest != node.Ready.Digest {
				return nil, fmt.Errorf("RAFT_RS_READY_CHANGED: %s", name)
			}
			continue
		}
		effect, next, err := adapter.readyEffect(name, *node.Ready, cause)
		if err != nil {
			return nil, err
		}
		adapter.ready[name] = next
		items = append(items, effect)
	}
	for ordinal, message := range response.Messages {
		item, err := adapter.messageItem(message, dependency, ordinal)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	for _, commit := range response.Commits {
		item, ok, err := adapter.clientResultItem(commit)
		if err != nil {
			return nil, err
		}
		if ok {
			items = append(items, item)
		}
	}
	for _, name := range []control.NodeID{"n1", "n2", "n3"} {
		if adapter.ready[name].Item != "" || adapter.pulses[name] != "" {
			continue
		}
		pulse, err := adapter.periodicPulse(name)
		if err != nil {
			return nil, err
		}
		adapter.pulses[name] = pulse.ID
		items = append(items, pulse)
	}
	return items, nil
}

func (adapter *Adapter) readyEffect(node control.NodeID, ready workerReady, cause string) (control.ProducedItem, readyBinding, error) {
	request, err := control.NewJSONPayload(readySchema, readyRequest{ReadyID: ready.ReadyID, Digest: ready.Digest})
	if err != nil {
		return control.ProducedItem{}, readyBinding{}, err
	}
	itemID, typedID, err := adapter.nextIDs(node, "ready", cause)
	if err != nil {
		return control.ProducedItem{}, readyBinding{}, err
	}
	owner := control.NodeRef{Node: node, Incarnation: 1}
	item := control.ProducedItem{
		ID: itemID, Kind: control.ItemEffect, Owner: owner,
		Effect: &control.HostEffect{
			ID: control.EffectID(typedID), Kind: effectReadyComplete, Owner: owner,
			Request: request, AllowedResults: []string{"persisted-applied-advanced"},
			Durability: control.DurabilityApplied,
		},
	}
	return item, readyBinding{Item: itemID, ReadyID: ready.ReadyID, Digest: ready.Digest}, nil
}

func (adapter *Adapter) messageItem(message workerMessage, dependency control.ItemID, ordinal int) (control.ProducedItem, error) {
	source, sourceOK := nodeNames[message.From]
	target, targetOK := nodeNames[message.To]
	if !sourceOK || !targetOK {
		return control.ProducedItem{}, errors.New("RAFT_RS_MESSAGE_ROUTE_UNKNOWN")
	}
	payload, err := control.NewPayload(messageSchema, "protobuf", message.Bytes)
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, typedID, err := adapter.nextIDs(source, "message", strconv.Itoa(ordinal))
	if err != nil {
		return control.ProducedItem{}, err
	}
	dependencies := []control.ItemID(nil)
	if dependency != "" {
		dependencies = []control.ItemID{dependency}
	}
	owner := control.NodeRef{Node: source, Incarnation: 1}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemMessage, Owner: owner, Dependencies: dependencies,
		Message: &control.MessageEnvelope{
			ID: control.MessageID(typedID), Source: owner, Target: target,
			TypeHint: message.TypeHint, Payload: payload,
			Metadata: map[string]string{
				"term": strconv.FormatUint(message.Term, 10), "index": strconv.FormatUint(message.Index, 10),
				"commit": strconv.FormatUint(message.Commit, 10),
			},
		},
	}, nil
}

func (adapter *Adapter) clientResultItem(commit workerCommit) (control.ProducedItem, bool, error) {
	node := nodeNames[commit.Node]
	if node == "" || commit.Index == 0 || commit.Term == 0 {
		return control.ProducedItem{}, false, errors.New("RAFT_RS_COMMIT_IDENTITY_INVALID")
	}
	proposal, err := decodeProposal(commit.Data)
	if err != nil {
		return control.ProducedItem{}, false, err
	}
	if proposal.Origin.Node != node {
		return control.ProducedItem{}, false, nil
	}
	payload, err := control.NewJSONPayload(resultSchema, clientResult{
		Index: commit.Index, Term: commit.Term, Value: append([]byte(nil), proposal.Value...),
	})
	if err != nil {
		return control.ProducedItem{}, false, err
	}
	itemID, _, err := adapter.nextIDs(node, "client-result", proposal.RequestID)
	if err != nil {
		return control.ProducedItem{}, false, err
	}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemClientResult, Owner: proposal.Origin,
		Response: &control.ClientResponse{
			RequestID: proposal.RequestID, Owner: proposal.Origin, Status: "committed", Payload: payload,
		},
	}, true, nil
}

func (adapter *Adapter) periodicPulse(node control.NodeID) (control.ProducedItem, error) {
	callback, err := control.NewPayload(callbackSchema, "json", []byte(`{"operation":"tick"}`))
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, typedID, err := adapter.nextIDs(node, "pulse", "")
	if err != nil {
		return control.ProducedItem{}, err
	}
	owner := control.NodeRef{Node: node, Incarnation: 1}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemTemporal, Owner: owner,
		Temporal: &control.TemporalItem{
			ID: control.TemporalID(typedID), Kind: control.TemporalPeriodicPulse, Owner: owner,
			ClockDomain: "raft-rs-host-tick", Deadline: adapter.logicalTime + 1, Period: 1, Callback: callback,
		},
	}, nil
}

func (adapter *Adapter) finishYield(cause string, items []control.ProducedItem) (control.Yield, error) {
	adapter.yieldSeq++
	id, err := control.StableID("raft-rs-v2-yield", strconv.FormatUint(adapter.yieldSeq, 10), cause)
	if err != nil {
		return control.Yield{}, err
	}
	digest, err := control.CanonicalDigest(adapter.snapshot())
	if err != nil {
		return control.Yield{}, err
	}
	yield := control.Yield{ID: control.YieldID(id), Kind: control.YieldStable, StateDigest: digest}
	emission, err := (control.Emission{Yield: yield.ID, Items: items}).Seal()
	if err != nil {
		return control.Yield{}, err
	}
	adapter.current = yield.ID
	adapter.emission = emission
	return yield, nil
}

func (adapter *Adapter) snapshot() adapterSnapshot {
	return adapterSnapshot{
		LogicalTime: adapter.logicalTime, YieldSeq: adapter.yieldSeq, ItemSeq: adapter.itemSeq,
		Nodes: append([]workerNode(nil), adapter.last.Nodes...),
		Ready: adapter.ready, Pulses: adapter.pulses,
	}
}

func (adapter *Adapter) nextIDs(node control.NodeID, kind, cause string) (control.ItemID, string, error) {
	adapter.itemSeq++
	ordinal := strconv.FormatUint(adapter.itemSeq, 10)
	item, err := control.StableID("raft-rs-v2-item", string(node), kind, ordinal, cause)
	if err != nil {
		return "", "", err
	}
	typed, err := control.StableID("raft-rs-v2-typed", string(node), kind, ordinal, cause)
	return control.ItemID(item), typed, err
}

func (adapter *Adapter) validateResponse(response workerResponse) error {
	if response.SchemaVersion != workerSchema || len(response.Nodes) != 3 {
		return errors.New("RAFT_RS_WORKER_STATE_INVALID")
	}
	for index, node := range response.Nodes {
		if node.ID != uint64(index+1) || nodeNames[node.ID] == "" {
			return errors.New("RAFT_RS_WORKER_NODE_SET_INVALID")
		}
	}
	return nil
}

func (adapter *Adapter) node(name control.NodeID) *workerNode {
	for index := range adapter.last.Nodes {
		if nodeNames[adapter.last.Nodes[index].ID] == name {
			return &adapter.last.Nodes[index]
		}
	}
	return nil
}

func validMessageItem(command control.AdapterCommand, item *control.ProducedItem) bool {
	return item != nil && item.Message != nil && item.ID == command.Item && item.Owner == item.Message.Source
}

func raftID(node control.NodeID) (uint64, bool) {
	for id, name := range nodeNames {
		if name == node {
			return id, true
		}
	}
	return 0, false
}

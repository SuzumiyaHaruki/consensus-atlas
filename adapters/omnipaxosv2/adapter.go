package omnipaxosv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
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
	pulses       map[control.NodeID]control.ItemID
}

func New(config Config) (*Adapter, error) {
	if config.WorkerPath == "" {
		return nil, errors.New("OMNIPAXOS_WORKER_PATH_REQUIRED")
	}
	if err := config.ValidateNodeConfiguration(); err != nil {
		return nil, err
	}
	info, err := os.Stat(config.WorkerPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("OMNIPAXOS_WORKER_NOT_EXECUTABLE")
	}
	encoded, err := os.ReadFile(config.WorkerPath)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	return &Adapter{config: config, workerDigest: hex.EncodeToString(digest[:])}, nil
}

func (adapter *Adapter) Manifest(context.Context) (control.AdapterManifest, error) {
	nodes := adapter.config.NodeIDs()
	configuration := expectedWorkerConfiguration(len(nodes))
	configurationDigest, err := control.CanonicalDigest(struct {
		WorkerDigest  string              `json:"worker_digest"`
		Nodes         []control.NodeID    `json:"nodes"`
		Configuration workerConfiguration `json:"configuration"`
	}{adapter.workerDigest, nodes, configuration})
	if err != nil {
		return control.AdapterManifest{}, err
	}
	return control.AdapterManifest{
		SchemaVersion: control.SchemaVersion,
		AdapterID:     adapterID, ImplementationID: implementation,
		BuildID: "sha256:" + adapter.workerDigest, ConfigurationDigest: configurationDigest,
		Nodes: nodes,
		Capabilities: control.CapabilityManifest{
			Actions: []control.ActionKind{
				control.ActionInvoke,
				control.ActionDropMessage, control.ActionDuplicateMessage,
				control.ActionDeliverMessage, control.ActionFireTemporal,
			},
			Items: []control.ItemKind{control.ItemMessage, control.ItemTemporal, control.ItemClientResult},
			Temporal: control.TemporalCapability{
				Kinds: []control.TemporalKind{control.TemporalPeriodicPulse}, ClockError: 0,
			},
			Entropy: control.EntropyCapability{
				Provider:  "harness-empty-tape/sut-no-rng-api-in-exercised-core",
				Algorithm: controlentropy.Algorithm, DomainPolicy: "no-harness-draws",
				ResetPolicy: "fresh-worker-process", StrictReplay: false,
			},
			Message: &control.MessageCapability{
				TypeHints:    append([]string(nil), messageTypeHints...),
				MetadataKeys: append([]string(nil), messageMetadataKeys...),
			},
			StrictYield: true, StrictReplay: true,
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
	response, err := worker.call(ctx, workerRequest{
		Op: "reset", NodeCount: uint64(adapter.config.ResolvedNodeCount()),
	})
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
	adapter.pulses = make(map[control.NodeID]control.ItemID)
	adapter.last = response
	return adapter.validateResponse(response)
}

func (adapter *Adapter) Check(_ context.Context, command control.AdapterCommand) (control.CommandEligibility, error) {
	if adapter.worker == nil || adapter.pending != nil {
		return control.CommandEligibility{ReasonCode: "OMNIPAXOS_NOT_STABLE"}, nil
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
		id, ok := protocolID(command.Node.Node)
		if inputErr != nil || !ok || id > uint64(adapter.config.ResolvedNodeCount()) || command.Node.Incarnation != 1 {
			return control.CommandEligibility{ReasonCode: "OMNIPAXOS_INVOKE_NOT_ELIGIBLE"}, nil
		}
	case control.ActionDropMessage:
		if !validMessageItem(command, envelope.Item) {
			return control.CommandEligibility{ReasonCode: "OMNIPAXOS_MESSAGE_NOT_DROPPABLE"}, nil
		}
	case control.ActionDeliverMessage:
		if !validMessageItem(command, envelope.Item) || envelope.Item.Message.Target != command.Node.Node ||
			command.Node.Incarnation != 1 || envelope.Item.Message.Payload.SchemaVersion != messageSchema ||
			envelope.Item.Message.Payload.Encoding != "json" {
			return control.CommandEligibility{ReasonCode: "OMNIPAXOS_MESSAGE_NOT_DELIVERABLE"}, nil
		}
		if id, ok := protocolID(command.Node.Node); !ok || id > uint64(adapter.config.ResolvedNodeCount()) {
			return control.CommandEligibility{ReasonCode: "OMNIPAXOS_MESSAGE_TARGET_UNKNOWN"}, nil
		}
	case control.ActionFireTemporal:
		if envelope.Item == nil || envelope.Item.Temporal == nil || envelope.Item.ID != command.Item ||
			envelope.Item.Owner != command.Node || command.Node.Incarnation != 1 ||
			adapter.pulses[command.Node.Node] != command.Item {
			return control.CommandEligibility{ReasonCode: "OMNIPAXOS_PULSE_NOT_CURRENT"}, nil
		}
	default:
		return control.CommandEligibility{ReasonCode: "OMNIPAXOS_ACTION_UNSUPPORTED"}, nil
	}
	return control.CommandEligibility{Eligible: true}, nil
}

func (adapter *Adapter) Submit(_ context.Context, command control.AdapterCommand) error {
	if adapter.pending != nil {
		return errors.New("OMNIPAXOS_COMMAND_ALREADY_PENDING")
	}
	copyCommand := command
	copyCommand.Payload.Bytes = append([]byte(nil), command.Payload.Bytes...)
	adapter.pending = &copyCommand
	return nil
}

func (*Adapter) CheckRuntimeAction(_ context.Context, action control.Action) (control.CommandEligibility, error) {
	if action.Kind == control.ActionDuplicateMessage {
		return control.CommandEligibility{Eligible: true}, nil
	}
	return control.CommandEligibility{ReasonCode: "OMNIPAXOS_RUNTIME_ACTION_UNSUPPORTED"}, nil
}

func (*Adapter) ApplyRuntimeAction(_ context.Context, action control.Action) error {
	if action.Kind == control.ActionDuplicateMessage {
		return nil
	}
	return fmt.Errorf("OMNIPAXOS_RUNTIME_ACTION_UNSUPPORTED: %s", action.Kind)
}

func (adapter *Adapter) RunUntilYield(ctx context.Context) (control.Yield, error) {
	if adapter.yieldSeq == 0 {
		items, err := adapter.capture(adapter.last, "")
		if err != nil {
			return control.Yield{}, err
		}
		return adapter.finishYield("bootstrap", items)
	}
	if adapter.pending == nil {
		return control.Yield{}, errors.New("OMNIPAXOS_COMMAND_REQUIRED")
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
		input, decodeErr := decodeInput(parameters.Input)
		if decodeErr != nil {
			return control.Yield{}, decodeErr
		}
		id, _ := protocolID(command.Node.Node)
		data, encodeErr := encodeEntry(input, id)
		if encodeErr != nil {
			return control.Yield{}, encodeErr
		}
		response, err = adapter.worker.call(ctx, workerRequest{Op: "append", Node: id, Payload: data})
	case control.ActionDropMessage:
	case control.ActionDeliverMessage:
		id, _ := protocolID(command.Node.Node)
		response, err = adapter.worker.call(ctx, workerRequest{
			Op: "step", Node: id, Payload: envelope.Item.Message.Payload.Bytes,
		})
		dependency = command.Item
	case control.ActionFireTemporal:
		id, _ := protocolID(command.Node.Node)
		delete(adapter.pulses, command.Node.Node)
		response, err = adapter.worker.call(ctx, workerRequest{Op: "tick", Node: id})
		dependency = command.Item
	default:
		err = fmt.Errorf("OMNIPAXOS_COMMAND_UNSUPPORTED: %s", command.Kind)
	}
	if err != nil {
		return control.Yield{}, err
	}
	if err := adapter.validateResponse(response); err != nil {
		return control.Yield{}, err
	}
	items, err := adapter.capture(response, dependency)
	if err != nil {
		return control.Yield{}, err
	}
	response.Messages = nil
	response.Decisions = nil
	adapter.last = response
	return adapter.finishYield(string(command.ID), items)
}

func (adapter *Adapter) Collect(_ context.Context, yield control.YieldID) (control.Emission, error) {
	if adapter.emission.Yield != yield {
		return control.Emission{}, fmt.Errorf("OMNIPAXOS_YIELD_UNKNOWN: %s", yield)
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
		return control.EvidenceEnvelope{}, errors.New("OMNIPAXOS_YIELD_REQUIRED")
	}
	payload, err := control.NewJSONPayload(evidenceSchema, adapter.snapshot())
	if err != nil {
		return control.EvidenceEnvelope{}, err
	}
	return control.EvidenceEnvelope{Yield: adapter.current, Payload: payload}, nil
}

func (adapter *Adapter) SnapshotEntropy(context.Context) (control.EntropyAuditEnvelope, error) {
	if adapter.current == "" || adapter.entropy == nil {
		return control.EntropyAuditEnvelope{}, errors.New("OMNIPAXOS_ENTROPY_UNAVAILABLE")
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

func (adapter *Adapter) capture(
	response workerResponse,
	dependency control.ItemID,
) ([]control.ProducedItem, error) {
	items := make([]control.ProducedItem, 0, len(response.Messages)+len(response.Decisions)+3)
	for ordinal, message := range response.Messages {
		item, err := adapter.messageItem(message, dependency, ordinal)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	for _, decision := range response.Decisions {
		item, ok, err := adapter.clientResultItem(decision)
		if err != nil {
			return nil, err
		}
		if ok {
			items = append(items, item)
		}
	}
	for _, name := range adapter.config.NodeIDs() {
		if adapter.pulses[name] != "" {
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

func (adapter *Adapter) clientResultItem(decision workerDecision) (control.ProducedItem, bool, error) {
	node, origin := nodeName(decision.Node), nodeName(decision.Origin)
	if node == "" || origin == "" || decision.Node > uint64(adapter.config.ResolvedNodeCount()) ||
		decision.Origin > uint64(adapter.config.ResolvedNodeCount()) || decision.Index == 0 || decision.RequestID == "" {
		return control.ProducedItem{}, false, errors.New("OMNIPAXOS_DECISION_IDENTITY_INVALID")
	}
	if node != origin {
		return control.ProducedItem{}, false, nil
	}
	payload, err := control.NewJSONPayload(resultSchema, decision)
	if err != nil {
		return control.ProducedItem{}, false, err
	}
	itemID, _, err := adapter.nextIDs(origin, "client-result", decision.RequestID)
	if err != nil {
		return control.ProducedItem{}, false, err
	}
	owner := control.NodeRef{Node: origin, Incarnation: 1}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemClientResult, Owner: owner,
		Response: &control.ClientResponse{
			RequestID: decision.RequestID, Owner: owner, Status: "decided", Payload: payload,
		},
	}, true, nil
}

func (adapter *Adapter) messageItem(
	message workerMessage,
	dependency control.ItemID,
	ordinal int,
) (control.ProducedItem, error) {
	source, target := nodeName(message.From), nodeName(message.To)
	if source == "" || target == "" || message.From > uint64(adapter.config.ResolvedNodeCount()) ||
		message.To > uint64(adapter.config.ResolvedNodeCount()) || len(message.Bytes) == 0 {
		return control.ProducedItem{}, errors.New("OMNIPAXOS_MESSAGE_ROUTE_INVALID")
	}
	payload, err := control.NewPayload(messageSchema, "json", message.Bytes)
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
	metadata := make(map[string]string, len(message.Metadata))
	for key, value := range message.Metadata {
		metadata[key] = value
	}
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemMessage, Owner: owner, Dependencies: dependencies,
		Message: &control.MessageEnvelope{
			ID: control.MessageID(typedID), Source: owner, Target: target,
			TypeHint: message.TypeHint, Metadata: metadata, Payload: payload,
		},
	}, nil
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
			ClockDomain: "omnipaxos-host-tick", Deadline: adapter.logicalTime + 1,
			Period: 1, Callback: callback,
		},
	}, nil
}

func (adapter *Adapter) finishYield(cause string, items []control.ProducedItem) (control.Yield, error) {
	adapter.yieldSeq++
	id, err := control.StableID("omnipaxos-v2-yield", strconv.FormatUint(adapter.yieldSeq, 10), cause)
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
	nodes := make([]workerNode, len(adapter.last.Nodes))
	for index, node := range adapter.last.Nodes {
		node.DecidedPrefixes = append([]workerDecisionPrefix(nil), node.DecidedPrefixes...)
		nodes[index] = node
	}
	return adapterSnapshot{
		LogicalTime: adapter.logicalTime, YieldSeq: adapter.yieldSeq, ItemSeq: adapter.itemSeq,
		Nodes: nodes, Pulses: adapter.pulses,
	}
}

func (adapter *Adapter) nextIDs(node control.NodeID, kind, cause string) (control.ItemID, string, error) {
	adapter.itemSeq++
	ordinal := strconv.FormatUint(adapter.itemSeq, 10)
	item, err := control.StableID("omnipaxos-v2-item", string(node), kind, ordinal, cause)
	if err != nil {
		return "", "", err
	}
	typed, err := control.StableID("omnipaxos-v2-typed", string(node), kind, ordinal, cause)
	return control.ItemID(item), typed, err
}

func (adapter *Adapter) validateResponse(response workerResponse) error {
	nodeCount := adapter.config.ResolvedNodeCount()
	expectedConfiguration := expectedWorkerConfiguration(nodeCount)
	if response.SchemaVersion != workerSchema || len(response.Nodes) != nodeCount ||
		response.Configuration == nil || !reflect.DeepEqual(*response.Configuration, expectedConfiguration) {
		return errors.New("OMNIPAXOS_WORKER_STATE_INVALID")
	}
	for index, node := range response.Nodes {
		if node.ID != uint64(index+1) || nodeName(node.ID) == "" ||
			node.Leader > uint64(nodeCount) || node.PromisePID > uint64(nodeCount) {
			return errors.New("OMNIPAXOS_WORKER_NODE_SET_INVALID")
		}
		if !validDigest(node.DecidedPrefixDigest) {
			return errors.New("OMNIPAXOS_WORKER_DECIDED_PREFIX_DIGEST_INVALID")
		}
		if len(node.DecidedPrefixes) != int(node.DecidedIndex) {
			return errors.New("OMNIPAXOS_WORKER_DECIDED_PREFIX_COUNT_INVALID")
		}
		for prefixIndex, prefix := range node.DecidedPrefixes {
			if prefix.Index != uint64(prefixIndex+1) || !validDigest(prefix.Digest) {
				return errors.New("OMNIPAXOS_WORKER_DECIDED_PREFIX_INVALID")
			}
		}
		if len(node.DecidedPrefixes) > 0 &&
			node.DecidedPrefixes[len(node.DecidedPrefixes)-1].Digest != node.DecidedPrefixDigest {
			return errors.New("OMNIPAXOS_WORKER_DECIDED_PREFIX_FINAL_MISMATCH")
		}
	}
	for _, message := range response.Messages {
		if nodeName(message.From) == "" || nodeName(message.To) == "" ||
			message.From > uint64(nodeCount) || message.To > uint64(nodeCount) || len(message.Bytes) == 0 ||
			!validWorkerMessageDescription(message) {
			return errors.New("OMNIPAXOS_WORKER_MESSAGE_INVALID")
		}
	}
	for _, decision := range response.Decisions {
		if nodeName(decision.Node) == "" || nodeName(decision.Origin) == "" ||
			decision.Node > uint64(nodeCount) || decision.Origin > uint64(nodeCount) ||
			decision.Index == 0 || decision.RequestID == "" {
			return errors.New("OMNIPAXOS_WORKER_DECISION_INVALID")
		}
	}
	return nil
}

func expectedWorkerConfiguration(nodeCount int) workerConfiguration {
	priorities := make([]uint32, nodeCount)
	for index := range priorities {
		priorities[index] = uint32(nodeCount - index)
	}
	return workerConfiguration{
		NodeCount: uint64(nodeCount), ElectionTickTimeout: 2,
		ResendMessageTickTimeout: 100, BufferSize: 1024, BatchSize: 1,
		LeaderPriorities: priorities,
	}
}

func validWorkerMessageDescription(message workerMessage) bool {
	if message.Family != "ble" && message.Family != "sequence-paxos" {
		return false
	}
	if !containsString(messageTypeHints, message.TypeHint) ||
		(message.Family == "ble") != (len(message.TypeHint) > 4 && message.TypeHint[:4] == "ble/") {
		return false
	}
	for key, value := range message.Metadata {
		if value == "" || !containsString(messageMetadataKeys, key) {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func validMessageItem(command control.AdapterCommand, item *control.ProducedItem) bool {
	return item != nil && item.Message != nil && item.ID == command.Item && item.Owner == item.Message.Source
}

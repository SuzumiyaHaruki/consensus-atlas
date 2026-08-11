package etcdraftv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
)

type nodeState struct {
	config       NodeConfig
	raw          *raft.RawNode
	storage      *raft.MemoryStorage
	durable      durableImage
	application  applicationImage
	incarnation  uint64
	confState    pb.ConfState
	applied      uint64
	tickCount    uint64
	advanceCount uint64
	outstanding  *readyRecord
	pulseItem    control.ItemID
}

type Adapter struct {
	config       Config
	order        []control.NodeID
	nodes        map[control.NodeID]*nodeState
	names        map[uint64]control.NodeID
	peers        []raft.Peer
	entropy      *nativeEntropy
	logicalTime  uint64
	yieldSeq     uint64
	itemSeq      uint64
	readySeq     uint64
	bootstrap    bool
	initializing bool
	pending      *control.AdapterCommand
	current      control.YieldID
	emissions    map[control.YieldID]control.Emission
}

func New() *Adapter {
	adapter, err := NewWithConfig(DefaultConfig())
	if err != nil {
		panic(err)
	}
	return adapter
}

func NewWithConfig(config Config) (*Adapter, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &Adapter{config: config.normalized()}, nil
}

func (adapter *Adapter) Manifest(context.Context) (control.AdapterManifest, error) {
	configurationDigest, err := adapter.config.digest()
	if err != nil {
		return control.AdapterManifest{}, err
	}
	actions := []control.ActionKind{
		control.ActionInvoke, control.ActionFireTemporal, control.ActionCompleteEffect,
	}
	actions = append(actions, control.ActionCrash, control.ActionRestart)
	items := []control.ItemKind{
		control.ItemTemporal, control.ItemEffect, control.ItemClientResult, control.ItemObservation,
	}
	if len(adapter.config.Nodes) > 1 {
		actions = append(actions,
			control.ActionDropMessage, control.ActionDuplicateMessage, control.ActionPartition,
			control.ActionHeal, control.ActionDeliverMessage,
		)
		items = append(items, control.ItemMessage)
	}
	return control.AdapterManifest{
		SchemaVersion: control.SchemaVersion,
		AdapterID:     adapterID, ImplementationID: implementation + "@" + implementationV,
		BuildID: SUTBuildIdentity, ConfigurationDigest: configurationDigest, Nodes: adapter.config.nodeIDs(),
		Capabilities: control.CapabilityManifest{
			Actions: actions,
			Items:   items,
			Temporal: control.TemporalCapability{
				Kinds: []control.TemporalKind{control.TemporalPeriodicPulse}, ClockError: 0,
			},
			Entropy: control.EntropyCapability{
				Provider: "scoped-crypto-rand-reader-v1", Algorithm: controlentropy.Algorithm,
				DomainPolicy: "node-incarnation-native-call", ResetPolicy: "per-trial",
				StrictReplay: true,
			},
			CrashModes:  []string{"power-loss"},
			EffectKinds: []string{effectReadyPersist, effectReadyAdvance},
			StrictYield: true, StrictReplay: true,
			DurableCheckpoints: true,
		},
		EvidenceSchemas: []string{evidenceSchema},
	}, nil
}

func (adapter *Adapter) Reset(_ context.Context, seed []byte) error {
	if err := adapter.config.validate(); err != nil {
		return err
	}
	entropy, err := newNativeEntropy(seed)
	if err != nil {
		return err
	}
	adapter.entropy = entropy
	adapter.order = adapter.config.nodeIDs()
	adapter.nodes = make(map[control.NodeID]*nodeState, len(adapter.config.Nodes))
	adapter.names = make(map[uint64]control.NodeID, len(adapter.config.Nodes))
	adapter.peers = make([]raft.Peer, 0, len(adapter.config.Nodes))
	for _, configured := range adapter.config.Nodes {
		adapter.names[configured.RaftID] = configured.Node
		adapter.peers = append(adapter.peers, raft.Peer{ID: configured.RaftID})
	}
	adapter.logicalTime = 0
	adapter.yieldSeq = 0
	adapter.itemSeq = 0
	adapter.readySeq = 0
	adapter.bootstrap = true
	adapter.initializing = true
	adapter.pending = nil
	adapter.current = ""
	adapter.emissions = make(map[control.YieldID]control.Emission)
	for _, configured := range adapter.config.Nodes {
		node := &nodeState{
			config: configured, storage: raft.NewMemoryStorage(), incarnation: 1,
		}
		node.application, err = newApplicationImage()
		if err != nil {
			return fmt.Errorf("initial application image %s: %w", configured.Node, err)
		}
		node.durable, err = captureDurableImage(node.storage, 0, pb.ConfState{}, node.application)
		if err != nil {
			return fmt.Errorf("initial durable image %s: %w", configured.Node, err)
		}
		if err := adapter.boot(node, true); err != nil {
			return fmt.Errorf("boot %s: %w", configured.Node, err)
		}
		adapter.nodes[configured.Node] = node
	}
	return nil
}

func (adapter *Adapter) boot(node *nodeState, bootstrap bool) error {
	return adapter.entropy.withDomain(adapter.entropyDomain(node), func() error {
		raw, err := raft.NewRawNode(&raft.Config{
			ID: node.config.RaftID, ElectionTick: adapter.config.ElectionTick,
			HeartbeatTick: adapter.config.HeartbeatTick, Storage: node.storage,
			Applied: node.applied, MaxSizePerMsg: math.MaxUint64, MaxInflightMsgs: 256,
			MaxUncommittedEntriesSize: 1 << 30, ReadOnlyOption: raft.ReadOnlySafe,
			Logger: &raft.DefaultLogger{Logger: log.New(io.Discard, "", 0)},
		})
		if err != nil {
			return err
		}
		node.raw = raw
		if bootstrap {
			return node.raw.Bootstrap(adapter.peers)
		}
		return nil
	})
}

func (adapter *Adapter) Check(_ context.Context, command control.AdapterCommand) (control.CommandEligibility, error) {
	if adapter.nodes == nil || adapter.pending != nil {
		return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_NOT_STABLE"}, nil
	}
	envelope, err := decodeCommand(command)
	if err != nil {
		return control.CommandEligibility{}, err
	}
	switch command.Kind {
	case control.ActionDropMessage:
		if envelope.Item == nil || envelope.Item.Message == nil || envelope.Item.ID != command.Item {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_MESSAGE_NOT_DROPPABLE"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionInvoke:
		node := adapter.nodes[command.Node.Node]
		if node == nil || node.raw == nil || command.Node != adapter.owner(node) || node.outstanding != nil {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_INVOKE_INELIGIBLE"}, nil
		}
		var parameters invokeParameters
		if err := json.Unmarshal(envelope.Parameters, &parameters); err != nil {
			return control.CommandEligibility{}, err
		}
		if _, err := decodeInput(parameters.Input); err != nil {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_INPUT_INVALID"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionCompleteEffect:
		node := adapter.nodes[command.Node.Node]
		if node == nil || node.raw == nil || command.Node != adapter.owner(node) || node.outstanding == nil ||
			envelope.Item == nil || envelope.Item.Effect == nil || envelope.Item.ID != command.Item ||
			envelope.Item.Owner != command.Node {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_READY_EFFECT_NOT_CURRENT"}, nil
		}
		var request readyRequest
		if err := json.Unmarshal(envelope.Item.Effect.Request.Bytes, &request); err != nil {
			return control.CommandEligibility{}, err
		}
		var result resultParameters
		if err := json.Unmarshal(envelope.Parameters, &result); err != nil {
			return control.CommandEligibility{}, err
		}
		if request.ReadyID != node.outstanding.id || request.Digest != node.outstanding.digest {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_READY_EFFECT_IDENTITY_MISMATCH"}, nil
		}
		switch envelope.Item.Effect.Kind {
		case effectReadyPersist:
			if node.outstanding.persisted || node.outstanding.persistEffect != envelope.Item.ID ||
				request.Phase != "persist" || result.Result != "persisted" {
				return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_READY_PERSIST_IDENTITY_MISMATCH"}, nil
			}
		case effectReadyAdvance:
			if !node.outstanding.persisted || node.outstanding.advanceEffect != envelope.Item.ID ||
				request.Phase != "advance" || result.Result != "applied-and-advanced" {
				return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_READY_ADVANCE_IDENTITY_MISMATCH"}, nil
			}
		default:
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_READY_EFFECT_KIND_INVALID"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionFireTemporal:
		node := adapter.nodes[command.Node.Node]
		if node == nil || node.raw == nil || command.Node != adapter.owner(node) || node.outstanding != nil ||
			node.pulseItem != command.Item || envelope.Item == nil || envelope.Item.Temporal == nil ||
			envelope.Item.ID != command.Item || envelope.Item.Temporal.Kind != control.TemporalPeriodicPulse {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_PULSE_NOT_CURRENT"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionDeliverMessage:
		target := adapter.nodes[command.Node.Node]
		if target == nil || target.raw == nil || command.Node != adapter.owner(target) || target.outstanding != nil ||
			envelope.Item == nil || envelope.Item.Message == nil || envelope.Item.ID != command.Item ||
			envelope.Item.Message.Target != target.config.Node {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_MESSAGE_NOT_DELIVERABLE"}, nil
		}
		if err := adapter.validateMessage(envelope.Item, target); err != nil {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_MESSAGE_IDENTITY_MISMATCH"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionCrash:
		node := adapter.nodes[command.Node.Node]
		var parameters modeParameters
		if err := json.Unmarshal(envelope.Parameters, &parameters); err != nil {
			return control.CommandEligibility{}, err
		}
		if node == nil || node.raw == nil || command.Node != adapter.owner(node) || parameters.Mode != "power-loss" {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_CRASH_INELIGIBLE"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionRestart:
		node := adapter.nodes[command.Node.Node]
		if node == nil || node.raw != nil || command.Node.Node != node.config.Node ||
			command.Node.Incarnation != node.incarnation+1 {
			return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_RESTART_INELIGIBLE"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	default:
		return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_ACTION_UNSUPPORTED"}, nil
	}
}

func (adapter *Adapter) Submit(_ context.Context, command control.AdapterCommand) error {
	if adapter.pending != nil {
		return errors.New("ETCDRAFT_V2_COMMAND_ALREADY_PENDING")
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
		return control.CommandEligibility{ReasonCode: "ETCDRAFT_V2_RUNTIME_ACTION_UNSUPPORTED"}, nil
	}
}

func (*Adapter) ApplyRuntimeAction(_ context.Context, action control.Action) error {
	switch action.Kind {
	case control.ActionDuplicateMessage, control.ActionPartition, control.ActionHeal:
		return nil
	default:
		return fmt.Errorf("ETCDRAFT_V2_RUNTIME_ACTION_UNSUPPORTED: %s", action.Kind)
	}
}

func (adapter *Adapter) RunUntilYield(_ context.Context) (control.Yield, error) {
	if adapter.bootstrap {
		adapter.bootstrap = false
		items, err := adapter.captureAll("bootstrap")
		if err != nil {
			return control.Yield{}, err
		}
		return adapter.finishYield(control.YieldStable, "bootstrap", items)
	}
	if adapter.pending == nil {
		return control.Yield{}, errors.New("ETCDRAFT_V2_COMMAND_REQUIRED")
	}
	command := *adapter.pending
	adapter.pending = nil
	envelope, err := decodeCommand(command)
	if err != nil {
		return control.Yield{}, err
	}
	adapter.logicalTime = envelope.LogicalTime
	var observations []control.ProducedItem
	yieldKind := control.YieldStable
	switch command.Kind {
	case control.ActionDropMessage:
	case control.ActionInvoke:
		node := adapter.nodes[command.Node.Node]
		var parameters invokeParameters
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
		proposalErr := adapter.entropy.withDomain(adapter.entropyDomain(node), func() error {
			return node.raw.Propose(proposal)
		})
		if proposalErr != nil {
			if !errors.Is(proposalErr, raft.ErrProposalDropped) {
				return control.Yield{}, proposalErr
			}
			result, err := adapter.rejectedClientResult(node, input, "proposal-dropped")
			if err != nil {
				return control.Yield{}, err
			}
			observation, err := adapter.observation(node, "proposal-rejected", input.RequestID)
			if err != nil {
				return control.Yield{}, err
			}
			observations = append(observations, result, observation)
			break
		}
		observation, err := adapter.observation(node, "proposal-submitted", input.RequestID)
		if err != nil {
			return control.Yield{}, err
		}
		observations = append(observations, observation)
	case control.ActionCompleteEffect:
		node := adapter.nodes[command.Node.Node]
		produced, err := adapter.completeReadyEffect(node, envelope.Item.Effect.Kind)
		if err != nil {
			return control.Yield{}, err
		}
		observations = append(observations, produced...)
	case control.ActionFireTemporal:
		node := adapter.nodes[command.Node.Node]
		node.pulseItem = ""
		if err := adapter.entropy.withDomain(adapter.entropyDomain(node), func() error {
			node.raw.Tick()
			return nil
		}); err != nil {
			return control.Yield{}, err
		}
		node.tickCount++
		observation, err := adapter.observation(node, "tick", strconv.FormatUint(node.tickCount, 10))
		if err != nil {
			return control.Yield{}, err
		}
		observations = append(observations, observation)
	case control.ActionDeliverMessage:
		target := adapter.nodes[command.Node.Node]
		message, err := decodeRaftMessage(envelope.Item.Message.Payload)
		if err != nil {
			return control.Yield{}, err
		}
		if err := adapter.entropy.withDomain(adapter.entropyDomain(target), func() error {
			return target.raw.Step(message)
		}); err != nil {
			return control.Yield{}, err
		}
		observation, err := adapter.observation(target, "message-stepped", string(envelope.Item.Message.ID))
		if err != nil {
			return control.Yield{}, err
		}
		observations = append(observations, observation)
	case control.ActionCrash:
		node := adapter.nodes[command.Node.Node]
		observation, err := adapter.observation(node, "node-crashed", "power-loss")
		if err != nil {
			return control.Yield{}, err
		}
		observations = append(observations, observation)
		adapter.crashNode(node)
		yieldKind = control.YieldTerminal
	case control.ActionRestart:
		node := adapter.nodes[command.Node.Node]
		if err := adapter.restartNode(node, command.Node); err != nil {
			return control.Yield{}, err
		}
		observation, err := adapter.observation(node, "node-restarted", node.durable.Digest)
		if err != nil {
			return control.Yield{}, err
		}
		observations = append(observations, observation)
	default:
		return control.Yield{}, fmt.Errorf("ETCDRAFT_V2_COMMAND_UNSUPPORTED: %s", command.Kind)
	}
	produced, err := adapter.captureAll(string(command.ID))
	if err != nil {
		return control.Yield{}, err
	}
	return adapter.finishYield(yieldKind, string(command.ID), append(observations, produced...))
}

func (adapter *Adapter) Collect(_ context.Context, yield control.YieldID) (control.Emission, error) {
	emission, ok := adapter.emissions[yield]
	if !ok {
		return control.Emission{}, fmt.Errorf("ETCDRAFT_V2_YIELD_UNKNOWN: %s", yield)
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
		return control.EvidenceEnvelope{}, errors.New("ETCDRAFT_V2_YIELD_REQUIRED")
	}
	snapshot, err := adapter.snapshot()
	if err != nil {
		return control.EvidenceEnvelope{}, err
	}
	payload, err := control.NewJSONPayload(evidenceSchema, snapshot)
	if err != nil {
		return control.EvidenceEnvelope{}, err
	}
	return control.EvidenceEnvelope{Yield: adapter.current, Payload: payload}, nil
}

func (adapter *Adapter) SnapshotEntropy(context.Context) (control.EntropyAuditEnvelope, error) {
	if adapter.current == "" || adapter.entropy == nil {
		return control.EntropyAuditEnvelope{}, errors.New("ETCDRAFT_V2_ENTROPY_UNAVAILABLE")
	}
	return adapter.entropy.provider.SnapshotAudit(adapter.current)
}

func (adapter *Adapter) captureAll(cause string) ([]control.ProducedItem, error) {
	var items []control.ProducedItem
	for _, name := range adapter.order {
		node := adapter.nodes[name]
		if node.raw == nil || node.outstanding != nil || !node.raw.HasReady() {
			continue
		}
		produced, err := adapter.captureReady(node, cause)
		if err != nil {
			return nil, err
		}
		items = append(items, produced...)
	}
	if adapter.initializing {
		for _, name := range adapter.order {
			node := adapter.nodes[name]
			if node.raw != nil && (node.outstanding != nil || node.raw.HasReady()) {
				return items, nil
			}
		}
		adapter.initializing = false
	}
	for _, name := range adapter.order {
		node := adapter.nodes[name]
		if node.raw == nil || node.outstanding != nil || node.pulseItem != "" {
			continue
		}
		pulse, err := adapter.periodicPulse(node, adapter.logicalTime+1)
		if err != nil {
			return nil, err
		}
		node.pulseItem = pulse.ID
		items = append(items, pulse)
	}
	return items, nil
}

func (adapter *Adapter) captureReady(node *nodeState, cause string) ([]control.ProducedItem, error) {
	ready, err := cloneReady(node.raw.Ready())
	if err != nil {
		return nil, err
	}
	adapter.readySeq++
	readyID, err := control.StableID("etcdraft-v2-ready", string(node.config.Node),
		strconv.FormatUint(adapter.readySeq, 10), cause)
	if err != nil {
		return nil, err
	}
	digest, err := readyDigest(ready)
	if err != nil {
		return nil, err
	}
	node.outstanding = &readyRecord{node: node.config.Node, id: readyID, digest: digest, ready: ready}
	effect, err := adapter.readyPersistEffect(node, *node.outstanding)
	if err != nil {
		return nil, err
	}
	node.outstanding.persistEffect = effect.ID
	status := node.raw.BasicStatus()
	observation, err := adapter.typedObservation(node, "ready-captured", readyObservation{
		ReadyID: readyID, Digest: digest, MustSync: ready.MustSync,
		Entries: len(ready.Entries), Committed: len(ready.CommittedEntries), Messages: len(ready.Messages),
		HardStateEmpty: raft.IsEmptyHardState(ready.HardState), SnapshotEmpty: raft.IsEmptySnap(ready.Snapshot),
		CurrentRaftTerm: status.Term,
	})
	if err != nil {
		return nil, err
	}
	items := []control.ProducedItem{effect, observation}
	for index := range ready.Messages {
		message, err := adapter.messageItem(node, ready.Messages[index], effect.ID, index)
		if err != nil {
			return nil, err
		}
		items = append(items, message)
	}
	return items, nil
}

func (adapter *Adapter) completeReadyEffect(node *nodeState, kind string) ([]control.ProducedItem, error) {
	if node == nil || node.raw == nil || node.storage == nil || node.outstanding == nil {
		return nil, errors.New("ETCDRAFT_V2_READY_REQUIRED")
	}
	switch kind {
	case effectReadyPersist:
		return adapter.persistReadyEffect(node)
	case effectReadyAdvance:
		return adapter.advanceReadyEffect(node)
	default:
		return nil, fmt.Errorf("ETCDRAFT_V2_READY_EFFECT_KIND_INVALID: %s", kind)
	}
}

func (adapter *Adapter) persistReadyEffect(node *nodeState) ([]control.ProducedItem, error) {
	record := node.outstanding
	if record.persisted {
		return nil, errors.New("ETCDRAFT_V2_READY_ALREADY_PERSISTED")
	}
	if err := adapter.entropy.withDomain(adapter.entropyDomain(node), func() error {
		if err := persistReady(node.storage, record.ready); err != nil {
			return err
		}
		image, err := captureDurableImage(node.storage, node.applied, node.confState, node.application)
		if err != nil {
			return err
		}
		node.durable = image
		return nil
	}); err != nil {
		return nil, err
	}
	record.persisted = true
	advance, err := adapter.readyAdvanceEffect(node, *record)
	if err != nil {
		return nil, err
	}
	record.advanceEffect = advance.ID
	observation, err := adapter.observation(node, "ready-persisted", record.id)
	if err != nil {
		return nil, err
	}
	return []control.ProducedItem{advance, observation}, nil
}

func (adapter *Adapter) advanceReadyEffect(node *nodeState) ([]control.ProducedItem, error) {
	record := *node.outstanding
	if !record.persisted {
		return nil, errors.New("ETCDRAFT_V2_READY_NOT_PERSISTED")
	}
	var commands []appliedCommand
	if err := adapter.entropy.withDomain(adapter.entropyDomain(node), func() error {
		var err error
		commands, err = applyCommitted(
			node.raw, record.ready, &node.applied, &node.confState, &node.application,
		)
		if err != nil {
			return err
		}
		image, err := captureDurableImage(node.storage, node.applied, node.confState, node.application)
		if err != nil {
			return err
		}
		node.durable = image
		node.raw.Advance(record.ready)
		return nil
	}); err != nil {
		return nil, err
	}
	node.outstanding = nil
	node.advanceCount++
	observation, err := adapter.observation(node, "ready-advanced", record.id)
	if err != nil {
		return nil, err
	}
	items := []control.ProducedItem{observation}
	for _, command := range commands {
		if command.Origin.Node != node.config.Node {
			continue
		}
		result, err := adapter.clientResult(node, command)
		if err != nil {
			return nil, err
		}
		items = append(items, result)
	}
	return items, nil
}

func (adapter *Adapter) crashNode(node *nodeState) {
	node.raw = nil
	node.storage = nil
	node.outstanding = nil
	node.pulseItem = ""
}

func (adapter *Adapter) restartNode(node *nodeState, next control.NodeRef) error {
	storage, applied, confState, application, err := node.durable.restore()
	if err != nil {
		return err
	}
	state, err := node.durable.decode()
	if err != nil {
		return err
	}
	node.storage = storage
	node.applied = applied
	node.confState = confState
	node.application = application
	node.incarnation = next.Incarnation
	node.outstanding = nil
	node.pulseItem = ""
	bootstrap := raft.IsEmptyHardState(state.hardState) && state.snapshot.Metadata.Index == 0 && len(state.entries) == 0
	if err := adapter.boot(node, bootstrap); err != nil {
		node.raw = nil
		node.storage = nil
		return err
	}
	return nil
}

func (adapter *Adapter) readyPersistEffect(node *nodeState, record readyRecord) (control.ProducedItem, error) {
	requestBytes, err := json.Marshal(readyRequest{
		ReadyID: record.id, Digest: record.digest, Phase: "persist", MustSync: record.ready.MustSync,
	})
	if err != nil {
		return control.ProducedItem{}, err
	}
	request, err := control.NewPayload(readySchema, "json", requestBytes)
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, typedID, err := adapter.nextIDs(node, "ready-persist-effect")
	if err != nil {
		return control.ProducedItem{}, err
	}
	owner := adapter.owner(node)
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemEffect, Owner: owner,
		Effect: &control.HostEffect{
			ID: control.EffectID(typedID), Kind: effectReadyPersist, Owner: owner, Request: request,
			AllowedResults: []string{"persisted"}, Durability: control.DurabilityDurable,
		},
	}, nil
}

func (adapter *Adapter) readyAdvanceEffect(node *nodeState, record readyRecord) (control.ProducedItem, error) {
	requestBytes, err := json.Marshal(readyRequest{
		ReadyID: record.id, Digest: record.digest, Phase: "advance", MustSync: record.ready.MustSync,
	})
	if err != nil {
		return control.ProducedItem{}, err
	}
	request, err := control.NewPayload(readySchema, "json", requestBytes)
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, typedID, err := adapter.nextIDs(node, "ready-advance-effect")
	if err != nil {
		return control.ProducedItem{}, err
	}
	owner := adapter.owner(node)
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemEffect, Owner: owner,
		Effect: &control.HostEffect{
			ID: control.EffectID(typedID), Kind: effectReadyAdvance, Owner: owner, Request: request,
			AllowedResults: []string{"applied-and-advanced"}, Durability: control.DurabilityApplied,
		},
	}, nil
}

func (adapter *Adapter) clientResult(node *nodeState, command appliedCommand) (control.ProducedItem, error) {
	payload, err := control.NewJSONPayload(clientResultSchema, clientResult{Index: command.Index, Term: command.Term, Value: command.Value})
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, _, err := adapter.nextIDs(node, "client-result")
	if err != nil {
		return control.ProducedItem{}, err
	}
	owner := adapter.owner(node)
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemClientResult, Owner: owner,
		Response: &control.ClientResponse{
			RequestID: command.RequestID, Owner: owner, Status: "committed", Payload: payload,
		},
	}, nil
}

func (adapter *Adapter) rejectedClientResult(
	node *nodeState,
	input Input,
	reasonCode string,
) (control.ProducedItem, error) {
	payload, err := control.NewJSONPayload(clientRejectionSchema, clientRejection{ReasonCode: reasonCode})
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, _, err := adapter.nextIDs(node, "client-rejection")
	if err != nil {
		return control.ProducedItem{}, err
	}
	owner := adapter.owner(node)
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemClientResult, Owner: owner,
		Response: &control.ClientResponse{
			RequestID: input.RequestID, Owner: owner, Status: "rejected", Payload: payload,
		},
	}, nil
}

func (adapter *Adapter) messageItem(node *nodeState, message pb.Message, dependency control.ItemID, ordinal int) (control.ProducedItem, error) {
	target, ok := adapter.names[message.To]
	if !ok {
		return control.ProducedItem{}, fmt.Errorf("ETCDRAFT_V2_MESSAGE_TARGET_UNKNOWN: %d", message.To)
	}
	if message.From != node.config.RaftID {
		return control.ProducedItem{}, fmt.Errorf("ETCDRAFT_V2_MESSAGE_SOURCE_MISMATCH: %d != %d", message.From, node.config.RaftID)
	}
	encoded, err := message.Marshal()
	if err != nil {
		return control.ProducedItem{}, err
	}
	payload, err := control.NewPayload(messageSchema, "protobuf", encoded)
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, typedID, err := adapter.nextIDs(node, "message-"+strconv.Itoa(ordinal))
	if err != nil {
		return control.ProducedItem{}, err
	}
	owner := adapter.owner(node)
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemMessage, Owner: owner, Dependencies: []control.ItemID{dependency},
		Message: &control.MessageEnvelope{
			ID: control.MessageID(typedID), Source: owner, Target: target,
			TypeHint: message.Type.String(), Payload: payload,
			Metadata: map[string]string{
				"term": strconv.FormatUint(message.Term, 10), "index": strconv.FormatUint(message.Index, 10),
				"commit": strconv.FormatUint(message.Commit, 10),
			},
		},
	}, nil
}

func (adapter *Adapter) validateMessage(item *control.ProducedItem, target *nodeState) error {
	message, err := decodeRaftMessage(item.Message.Payload)
	if err != nil {
		return err
	}
	source := adapter.nodes[item.Message.Source.Node]
	if source == nil || item.Message.Source.Node != source.config.Node || item.Message.Source.Incarnation == 0 ||
		message.From != source.config.RaftID || message.To != target.config.RaftID {
		return errors.New("ETCDRAFT_V2_MESSAGE_ROUTE_MISMATCH")
	}
	return nil
}

func decodeRaftMessage(payload control.PayloadEnvelope) (pb.Message, error) {
	if err := payload.Validate(); err != nil {
		return pb.Message{}, err
	}
	if payload.SchemaVersion != messageSchema || payload.Encoding != "protobuf" {
		return pb.Message{}, fmt.Errorf("ETCDRAFT_V2_MESSAGE_SCHEMA_MISMATCH: %s/%s", payload.SchemaVersion, payload.Encoding)
	}
	var message pb.Message
	if err := message.Unmarshal(payload.Bytes); err != nil {
		return pb.Message{}, err
	}
	return message, nil
}

func (adapter *Adapter) periodicPulse(node *nodeState, deadline uint64) (control.ProducedItem, error) {
	callback, err := control.NewPayload(callbackSchema, "json", []byte(`{"operation":"tick"}`))
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, typedID, err := adapter.nextIDs(node, "periodic-pulse")
	if err != nil {
		return control.ProducedItem{}, err
	}
	owner := adapter.owner(node)
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemTemporal, Owner: owner,
		Temporal: &control.TemporalItem{
			ID: control.TemporalID(typedID), Kind: control.TemporalPeriodicPulse,
			Owner: owner, ClockDomain: "etcdraft-host-tick", Deadline: deadline, Period: 1,
			Callback: callback,
		},
	}, nil
}

func (adapter *Adapter) observation(node *nodeState, kind, value string) (control.ProducedItem, error) {
	return adapter.typedObservation(node, kind, map[string]string{"value": value})
}

func (adapter *Adapter) typedObservation(node *nodeState, kind string, value any) (control.ProducedItem, error) {
	payload, err := control.NewJSONPayload(observationV1, value)
	if err != nil {
		return control.ProducedItem{}, err
	}
	itemID, typedID, err := adapter.nextIDs(node, "observation-"+kind)
	if err != nil {
		return control.ProducedItem{}, err
	}
	owner := adapter.owner(node)
	return control.ProducedItem{
		ID: itemID, Kind: control.ItemObservation, Owner: owner,
		Observation: &control.TypedObservation{
			ID: control.ObservationID(typedID), Owner: owner, Kind: kind, Payload: payload,
		},
	}, nil
}

func (adapter *Adapter) finishYield(kind control.YieldKind, cause string, items []control.ProducedItem) (control.Yield, error) {
	adapter.yieldSeq++
	id, err := control.StableID("etcdraft-v2-yield", strconv.FormatUint(adapter.yieldSeq, 10), cause)
	if err != nil {
		return control.Yield{}, err
	}
	snapshot, err := adapter.snapshot()
	if err != nil {
		return control.Yield{}, err
	}
	stateDigest, err := control.CanonicalDigest(snapshot)
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

func (adapter *Adapter) snapshot() (clusterSnapshot, error) {
	if adapter.nodes == nil {
		return clusterSnapshot{}, errors.New("ETCDRAFT_V2_NOT_INITIALIZED")
	}
	snapshot := clusterSnapshot{
		LogicalTime: adapter.logicalTime, YieldSequence: adapter.yieldSeq,
		ItemSequence: adapter.itemSeq, ReadySequence: adapter.readySeq,
		EntropyScope: "process-global-reader/scoped+mutex; isolated-process-required-for-formal-qualification",
	}
	for _, name := range adapter.order {
		node := adapter.nodes[name]
		durableState, err := node.durable.decode()
		if err != nil {
			return clusterSnapshot{}, err
		}
		lastIndex, err := node.durable.lastIndex()
		if err != nil {
			return clusterSnapshot{}, err
		}
		role := "StateStopped"
		term := durableState.hardState.Term
		vote := durableState.hardState.Vote
		commit := durableState.hardState.Commit
		lead := uint64(0)
		if node.raw != nil {
			status := node.raw.BasicStatus()
			role = status.RaftState.String()
			term = status.Term
			vote = status.Vote
			commit = status.Commit
			lead = status.Lead
		}
		current := nodeSnapshot{
			Node: node.config.Node, RaftID: node.config.RaftID, Incarnation: node.incarnation,
			Running:   node.raw != nil,
			TickCount: node.tickCount, AdvanceCount: node.advanceCount, Applied: node.applied,
			Role: role, Term: term, Vote: vote, Commit: commit, Lead: lead,
			StorageTerm: durableState.hardState.Term, StorageVote: durableState.hardState.Vote,
			StorageCommit: durableState.hardState.Commit, StorageLastIndex: lastIndex,
			ConfState: cloneConfState(node.confState), DurableImageDigest: node.durable.Digest,
			ApplicationDigest: node.application.Digest, ApplicationCommands: len(node.application.Commands),
			PulseItemID: node.pulseItem,
		}
		if node.outstanding != nil {
			current.OutstandingReadyID = node.outstanding.id
			current.OutstandingDigest = node.outstanding.digest
		}
		snapshot.Nodes = append(snapshot.Nodes, current)
	}
	return snapshot, nil
}

func (adapter *Adapter) nextIDs(node *nodeState, kind string) (control.ItemID, string, error) {
	adapter.itemSeq++
	ordinal := strconv.FormatUint(adapter.itemSeq, 10)
	item, err := control.StableID("etcdraft-v2-item", string(node.config.Node), kind, ordinal)
	if err != nil {
		return "", "", err
	}
	typed, err := control.StableID("etcdraft-v2-typed", string(node.config.Node), kind, ordinal)
	if err != nil {
		return "", "", err
	}
	return control.ItemID(item), typed, nil
}

func (adapter *Adapter) owner(node *nodeState) control.NodeRef {
	return control.NodeRef{Node: node.config.Node, Incarnation: node.incarnation}
}

func (adapter *Adapter) entropyDomain(node *nodeState) controlentropy.Domain {
	return controlentropy.Domain{
		Namespace: "etcdraft-v3.6.0", Node: node.config.Node,
		Incarnation: node.incarnation, ID: "native-election-timeout",
	}
}

func decodeCommand(command control.AdapterCommand) (commandEnvelope, error) {
	if command.Payload.SchemaVersion != commandSchema {
		return commandEnvelope{}, fmt.Errorf("ETCDRAFT_V2_COMMAND_SCHEMA_MISMATCH: %s", command.Payload.SchemaVersion)
	}
	if err := command.Payload.Validate(); err != nil {
		return commandEnvelope{}, err
	}
	var envelope commandEnvelope
	if err := json.Unmarshal(command.Payload.Bytes, &envelope); err != nil {
		return commandEnvelope{}, err
	}
	return envelope, nil
}

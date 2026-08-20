package hashicorpraftv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
	hraft "github.com/hashicorp/raft"
)

const (
	modulePath      = "github.com/hashicorp/raft"
	moduleVersion   = "v1.7.3"
	moduleRevision  = "c0dc6a0b2c7e889f31e5ab2f7ed90ceb159acffe"
	moduleSum       = "h1:DxpEqZJysHN0wK+fviai5mFcSYsCkNpFUl1xpAW8Rbo="
	clockReasonCode = "HASHICORP_RAFT_CLOCK_NOT_INJECTABLE_V1_7_3"
	rngReasonCode   = "HASHICORP_RAFT_PACKAGE_RANDOM_NOT_INJECTABLE_V1_7_3"

	runtimeRPCSchema = "consensus-atlas/hashicorp-raft-runtime-rpc/v1"
	inputSchema      = "consensus-atlas/hashicorp-raft-input/v1"
	evidenceSchema   = "consensus-atlas/hashicorp-raft-evidence/v1"
	defaultBuildID   = "local-source-unsealed:hashicorpraft"
	defaultNodeCount = 3
	maxStaticNodes   = 64
)

// SUTBuildIdentity is deliberately unsealed for ordinary local builds.  The
// pinned moduleRevision remains the upstream baseline provenance used by the
// package audits, but it must not describe an editable checkout after a local
// source change.
var SUTBuildIdentity = defaultBuildID

type Adapter struct {
	config      Config
	nodes       map[control.NodeID]*runtimeNode
	outbound    chan *rpcCall
	applied     chan appliedRecord
	applyResult chan error
	entropy     *controlentropy.Provider
	pendingRPC  map[control.ItemID]*rpcCall
	pending     *control.AdapterCommand
	emissions   map[control.YieldID]control.Emission
	current     control.YieldID
	evidence    control.EvidenceEnvelope
	yieldSeq    uint64
	applyDigest string
	applyOpen   bool
	appliedLog  []appliedRecord
}

type Config struct {
	NodeCount int `json:"node_count,omitempty"`
}

func (config Config) validate() error {
	if config.NodeCount < 0 || config.NodeCount > maxStaticNodes ||
		config.NodeCount > 0 && config.NodeCount < defaultNodeCount {
		return errors.New("HASHICORP_RAFT_NODE_COUNT_INVALID")
	}
	return nil
}

func (config Config) resolvedNodeCount() int {
	if config.NodeCount == 0 {
		return defaultNodeCount
	}
	return config.NodeCount
}

func (config Config) nodeIDs() []control.NodeID {
	if config.validate() != nil {
		return nil
	}
	result := make([]control.NodeID, config.resolvedNodeCount())
	for index := range result {
		result[index] = control.NodeID("n" + strconv.Itoa(index+1))
	}
	return result
}

type runtimeNode struct {
	raft        *hraft.Raft
	transport   *runtimeTransport
	stores      *raftStores
	cancel      chan struct{}
	incarnation uint64
}

type raftStores struct {
	logs      *hraft.InmemStore
	stable    *hraft.InmemStore
	snapshots *hraft.InmemSnapshotStore
}

type appliedRecord struct {
	Node   control.NodeID `json:"node"`
	Index  uint64         `json:"index"`
	Digest string         `json:"digest"`
}

type runtimeEvidence struct {
	YieldSequence uint64          `json:"yield_sequence"`
	Applied       []appliedRecord `json:"applied"`
}

func NewAdapter() *Adapter { return &Adapter{config: Config{}} }

func NewWithConfig(config Config) (*Adapter, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &Adapter{config: config}, nil
}

func (a *Adapter) Manifest(context.Context) (control.AdapterManifest, error) {
	if err := a.config.validate(); err != nil {
		return control.AdapterManifest{}, err
	}
	nodes := a.config.nodeIDs()
	configuration, err := control.CanonicalDigest(struct {
		Nodes []control.NodeID `json:"nodes"`
		Fast  string           `json:"n1_timeout"`
		Slow  string           `json:"other_node_timeout"`
	}{nodes, "heartbeat=500ms,election=10s", "1h"})
	if err != nil {
		return control.AdapterManifest{}, err
	}
	return control.AdapterManifest{
		SchemaVersion: control.SchemaVersion, AdapterID: "hashicorp-raft-v2",
		ImplementationID: modulePath + "@" + moduleVersion, BuildID: SUTBuildIdentity,
		ConfigurationDigest: configuration, Nodes: nodes,
		Capabilities: control.CapabilityManifest{
			Actions: []control.ActionKind{
				control.ActionInvoke, control.ActionDropMessage, control.ActionDeliverMessage,
				control.ActionCrash, control.ActionRestart,
			},
			Items: []control.ItemKind{control.ItemMessage, control.ItemObservation},
			Entropy: control.EntropyCapability{
				Provider: "adapter-audit-only", Algorithm: controlentropy.Algorithm,
				DomainPolicy: "sut-randomness-uncontrolled", ResetPolicy: "per-trial", StrictReplay: false,
			},
			CrashModes: []string{"power-loss"}, StrictYield: false, StrictReplay: false,
		},
		EvidenceSchemas: []string{evidenceSchema},
	}, nil
}

func (a *Adapter) Reset(_ context.Context, seed []byte) error {
	_ = a.Close()
	if err := a.config.validate(); err != nil {
		return err
	}
	nodeCount := a.config.resolvedNodeCount()
	entropy, err := controlentropy.New(seed)
	if err != nil {
		return err
	}
	a.entropy = entropy
	a.outbound = make(chan *rpcCall, max(32, nodeCount*nodeCount*4))
	a.applied = make(chan appliedRecord, max(8, nodeCount*4))
	a.applyResult = make(chan error, 1)
	a.nodes = make(map[control.NodeID]*runtimeNode, nodeCount)
	a.pendingRPC = make(map[control.ItemID]*rpcCall)
	a.emissions = make(map[control.YieldID]control.Emission)
	a.pending, a.current, a.evidence = nil, "", control.EvidenceEnvelope{}
	a.yieldSeq, a.applyDigest, a.applyOpen, a.appliedLog = 0, "", false, nil
	nodes := a.config.nodeIDs()
	for _, id := range append(append([]control.NodeID(nil), nodes[1:]...), nodes[0]) {
		if err := a.startNode(id); err != nil {
			_ = a.Close()
			return err
		}
	}
	return nil
}

func (a *Adapter) startNode(id control.NodeID) error {
	node := &runtimeNode{stores: newRaftStores()}
	a.nodes[id] = node
	return a.bootNode(id, node, 1, true)
}

func (a *Adapter) bootNode(id control.NodeID, node *runtimeNode, incarnation uint64, bootstrap bool) error {
	cancel := make(chan struct{})
	transport := newRuntimeTransport(control.NodeRef{Node: id, Incarnation: incarnation}, a.outbound, cancel)
	timing := raftTiming{time.Hour, time.Hour, time.Minute}
	if id == "n1" {
		timing = raftTiming{500 * time.Millisecond, 10 * time.Second, 500 * time.Millisecond}
	}
	raftNode, _, err := startOfficialNode(
		id, timing, recordingFSM{id, a.applied}, transport, node.stores, bootstrap, a.config.nodeIDs(),
	)
	if err != nil {
		close(cancel)
		return err
	}
	node.raft, node.transport, node.cancel, node.incarnation = raftNode, transport, cancel, incarnation
	return nil
}

type raftTiming struct{ heartbeat, election, lease time.Duration }

func newRaftStores() *raftStores {
	return &raftStores{hraft.NewInmemStore(), hraft.NewInmemStore(), hraft.NewInmemSnapshotStore()}
}

func startOfficialNode(
	id control.NodeID,
	timing raftTiming,
	fsm hraft.FSM,
	transport hraft.Transport,
	stores *raftStores,
	bootstrap bool,
	membership ...[]control.NodeID,
) (*hraft.Raft, *hraft.Config, error) {
	conf := hraft.DefaultConfig()
	conf.LocalID, conf.PreVoteDisabled, conf.LogOutput = hraft.ServerID(id), true, io.Discard
	conf.CommitTimeout, conf.SnapshotInterval = 5*time.Millisecond, time.Hour
	conf.HeartbeatTimeout, conf.ElectionTimeout, conf.LeaderLeaseTimeout = timing.heartbeat, timing.election, timing.lease
	nodes := Config{}.nodeIDs()
	if len(membership) == 1 && len(membership[0]) > 0 {
		nodes = membership[0]
	}
	servers := make([]hraft.Server, len(nodes))
	for index, node := range nodes {
		servers[index] = hraft.Server{
			Suffrage: hraft.Voter, ID: hraft.ServerID(node), Address: hraft.ServerAddress(node),
		}
	}
	cluster := hraft.Configuration{Servers: servers}
	if bootstrap {
		if err := hraft.BootstrapCluster(conf, stores.logs, stores.stable, stores.snapshots, transport, cluster); err != nil {
			return nil, nil, fmt.Errorf("bootstrap %s: %w", id, err)
		}
	}
	node, err := hraft.NewRaft(conf, fsm, stores.logs, stores.stable, stores.snapshots, transport)
	if err != nil {
		return nil, nil, fmt.Errorf("start %s: %w", id, err)
	}
	return node, conf, nil
}

func (a *Adapter) Close() error {
	if a.nodes == nil {
		return nil
	}
	var first error
	for _, node := range a.nodes {
		if err := stopRuntimeNode(node); err != nil && first == nil {
			first = err
		}
	}
	a.nodes = nil
	return first
}

func stopRuntimeNode(node *runtimeNode) error {
	if node == nil || node.raft == nil {
		return nil
	}
	close(node.cancel)
	err := node.raft.Shutdown().Error()
	node.raft, node.transport, node.cancel = nil, nil, nil
	return err
}

func (a *Adapter) Check(_ context.Context, command control.AdapterCommand) (control.CommandEligibility, error) {
	if a.nodes == nil || a.pending != nil {
		return control.CommandEligibility{ReasonCode: "HASHICORP_RAFT_NOT_STABLE"}, nil
	}
	envelope, err := control.DecodeAdapterCommand(command)
	if err != nil {
		return control.CommandEligibility{}, err
	}
	switch command.Kind {
	case control.ActionDropMessage:
		if call := a.pendingRPC[command.Item]; call == nil || envelope.Item == nil || envelope.Item.Message == nil {
			return control.CommandEligibility{ReasonCode: "HASHICORP_RAFT_MESSAGE_NOT_DROPPABLE"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionDeliverMessage:
		call := a.pendingRPC[command.Item]
		target := a.nodes[command.Node.Node]
		if call == nil || target == nil || target.raft == nil || command.Node.Incarnation != target.incarnation ||
			envelope.Item == nil || envelope.Item.Message == nil ||
			envelope.Item.Message.Target != command.Node.Node || call.item.Message.Payload.Digest != envelope.Item.Message.Payload.Digest {
			return control.CommandEligibility{ReasonCode: "HASHICORP_RAFT_MESSAGE_NOT_DELIVERABLE"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionInvoke:
		node := a.nodes[command.Node.Node]
		input, err := decodeInvokeInput(envelope.Parameters)
		if err != nil || node == nil || node.raft == nil || node.raft.State() != hraft.Leader ||
			command.Node.Incarnation != node.incarnation {
			return control.CommandEligibility{ReasonCode: "HASHICORP_RAFT_INVOKE_NOT_ELIGIBLE"}, nil
		}
		if err := input.Validate(); err != nil {
			return control.CommandEligibility{ReasonCode: "HASHICORP_RAFT_INPUT_INVALID"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionCrash:
		node := a.nodes[command.Node.Node]
		var parameters control.AdapterModeParameters
		if err := json.Unmarshal(envelope.Parameters, &parameters); err != nil {
			return control.CommandEligibility{}, err
		}
		if node == nil || node.raft == nil || command.Node.Incarnation != node.incarnation || parameters.Mode != "power-loss" {
			return control.CommandEligibility{ReasonCode: "HASHICORP_RAFT_CRASH_INELIGIBLE"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	case control.ActionRestart:
		node := a.nodes[command.Node.Node]
		if node == nil || node.raft != nil || command.Node.Incarnation != node.incarnation+1 {
			return control.CommandEligibility{ReasonCode: "HASHICORP_RAFT_RESTART_INELIGIBLE"}, nil
		}
		return control.CommandEligibility{Eligible: true}, nil
	default:
		return control.CommandEligibility{ReasonCode: "HASHICORP_RAFT_ACTION_UNSUPPORTED"}, nil
	}
}

func (a *Adapter) Submit(_ context.Context, command control.AdapterCommand) error {
	if a.pending != nil {
		return errors.New("HASHICORP_RAFT_COMMAND_ALREADY_PENDING")
	}
	copyCommand := command
	copyCommand.Payload.Bytes = append([]byte(nil), command.Payload.Bytes...)
	a.pending = &copyCommand
	return nil
}

func (*Adapter) CheckRuntimeAction(_ context.Context, _ control.Action) (control.CommandEligibility, error) {
	return control.CommandEligibility{ReasonCode: "HASHICORP_RAFT_RUNTIME_ACTION_UNSUPPORTED"}, nil
}

func (*Adapter) ApplyRuntimeAction(_ context.Context, action control.Action) error {
	return fmt.Errorf("HASHICORP_RAFT_RUNTIME_ACTION_UNSUPPORTED: %s", action.Kind)
}

func (a *Adapter) RunUntilYield(ctx context.Context) (control.Yield, error) {
	if a.current == "" {
		items, err := a.collectCalls(ctx, a.config.resolvedNodeCount()-1)
		if err != nil {
			return control.Yield{}, err
		}
		return a.finishYield("bootstrap-election", items)
	}
	if a.pending == nil {
		return control.Yield{}, errors.New("HASHICORP_RAFT_COMMAND_REQUIRED")
	}
	command := *a.pending
	a.pending = nil
	envelope, err := control.DecodeAdapterCommand(command)
	if err != nil {
		return control.Yield{}, err
	}
	var items []control.ProducedItem
	switch command.Kind {
	case control.ActionDropMessage:
		call := a.pendingRPC[command.Item]
		delete(a.pendingRPC, command.Item)
		call.done <- errors.New("HASHICORP_RAFT_RPC_DROPPED")
	case control.ActionDeliverMessage:
		call := a.pendingRPC[command.Item]
		delete(a.pendingRPC, command.Item)
		if err := a.deliver(ctx, call); err != nil {
			return control.Yield{}, err
		}
		if call.kind == "request-vote" {
			items, err = a.collectCalls(ctx, a.config.resolvedNodeCount()-1)
		} else if a.applyOpen {
			items, err = a.waitApplyProgress(ctx)
		}
	case control.ActionInvoke:
		input, decodeErr := decodeInvokeInput(envelope.Parameters)
		if decodeErr != nil {
			return control.Yield{}, decodeErr
		}
		a.applyDigest = input.Digest
		a.applyOpen = true
		future := a.nodes[command.Node.Node].raft.Apply(input.Bytes, 0)
		go func() { a.applyResult <- future.Error() }()
		items, err = a.waitApplyProgress(ctx)
	case control.ActionCrash:
		node := a.nodes[command.Node.Node]
		if err := stopRuntimeNode(node); err != nil {
			return control.Yield{}, err
		}
		return a.finishYieldKind(control.YieldTerminal, string(command.ID), nil)
	case control.ActionRestart:
		node := a.nodes[command.Node.Node]
		if err := a.bootNode(command.Node.Node, node, command.Node.Incarnation, false); err != nil {
			return control.Yield{}, err
		}
	default:
		return control.Yield{}, fmt.Errorf("HASHICORP_RAFT_COMMAND_UNSUPPORTED: %s", command.Kind)
	}
	if err != nil {
		return control.Yield{}, err
	}
	return a.finishYield(string(command.ID), items)
}

func (a *Adapter) collectCalls(ctx context.Context, count int) ([]control.ProducedItem, error) {
	items := make([]control.ProducedItem, 0, count)
	for len(items) < count {
		select {
		case call := <-a.outbound:
			a.pendingRPC[call.item.ID] = call
			items = append(items, call.item)
		case <-ctx.Done():
			return nil, fmt.Errorf("HASHICORP_RAFT_OUTBOUND_WAIT: %w", ctx.Err())
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (a *Adapter) waitApplyProgress(ctx context.Context) ([]control.ProducedItem, error) {
	for {
		select {
		case call := <-a.outbound:
			a.pendingRPC[call.item.ID] = call
			return []control.ProducedItem{call.item}, nil
		case record := <-a.applied:
			if record.Node != "n1" || record.Digest != a.applyDigest {
				continue
			}
			a.applyOpen = false
			a.appliedLog = append(a.appliedLog, record)
			item, err := a.observation(record)
			if err != nil {
				return nil, err
			}
			return []control.ProducedItem{item}, nil
		case err := <-a.applyResult:
			if err != nil {
				return nil, fmt.Errorf("HASHICORP_RAFT_APPLY_FAILED: %w", err)
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("HASHICORP_RAFT_APPLY_WAIT: %w", ctx.Err())
		}
	}
}

func (a *Adapter) deliver(ctx context.Context, call *rpcCall) error {
	if call == nil {
		return errors.New("HASHICORP_RAFT_RPC_REQUIRED")
	}
	target := a.nodes[call.item.Message.Target]
	responses := make(chan hraft.RPCResponse, 1)
	rpc := hraft.RPC{Command: call.request, RespChan: responses}
	select {
	case target.transport.incoming <- rpc:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case result := <-responses:
		if err := assignRPCResponse(call.response, result.Response); err != nil {
			return err
		}
		call.done <- result.Error
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Adapter) Collect(_ context.Context, yield control.YieldID) (control.Emission, error) {
	emission, ok := a.emissions[yield]
	if !ok {
		return control.Emission{}, fmt.Errorf("HASHICORP_RAFT_YIELD_UNKNOWN: %s", yield)
	}
	return emission, nil
}

func (a *Adapter) SnapshotEvidence(context.Context) (control.EvidenceEnvelope, error) {
	if a.current == "" {
		return control.EvidenceEnvelope{}, errors.New("HASHICORP_RAFT_YIELD_REQUIRED")
	}
	return a.evidence, nil
}

func (a *Adapter) SnapshotEntropy(context.Context) (control.EntropyAuditEnvelope, error) {
	if a.current == "" || a.entropy == nil {
		return control.EntropyAuditEnvelope{}, errors.New("HASHICORP_RAFT_ENTROPY_UNAVAILABLE")
	}
	return a.entropy.SnapshotAudit(a.current)
}

func (a *Adapter) finishYield(cause string, items []control.ProducedItem) (control.Yield, error) {
	return a.finishYieldKind(control.YieldStable, cause, items)
}

func (a *Adapter) finishYieldKind(kind control.YieldKind, cause string, items []control.ProducedItem) (control.Yield, error) {
	a.yieldSeq++
	evidence := runtimeEvidence{a.yieldSeq, append([]appliedRecord(nil), a.appliedLog...)}
	payload, err := control.NewJSONPayload(evidenceSchema, evidence)
	if err != nil {
		return control.Yield{}, err
	}
	id, _ := control.StableID("hashicorp-yield", strconv.FormatUint(a.yieldSeq, 10), cause)
	state, _ := control.CanonicalDigest(evidence)
	yield := control.Yield{ID: control.YieldID(id), Kind: kind, StateDigest: state}
	emission, err := (control.Emission{Yield: yield.ID, Items: items}).Seal()
	if err != nil {
		return control.Yield{}, err
	}
	a.current, a.evidence, a.emissions[yield.ID] = yield.ID, control.EvidenceEnvelope{Yield: yield.ID, Payload: payload}, emission
	return yield, nil
}

func (a *Adapter) observation(record appliedRecord) (control.ProducedItem, error) {
	payload, err := control.NewJSONPayload(evidenceSchema, record)
	if err != nil {
		return control.ProducedItem{}, err
	}
	id, _ := control.StableID("hashicorp-apply", strconv.FormatUint(record.Index, 10), record.Digest)
	owner := control.NodeRef{Node: record.Node, Incarnation: 1}
	return control.ProducedItem{ID: control.ItemID(id), Kind: control.ItemObservation, Owner: owner,
		Observation: &control.TypedObservation{ID: control.ObservationID(id), Owner: owner, Kind: "fsm-apply", Payload: payload}}, nil
}

func InputPayload(value []byte) (control.PayloadEnvelope, error) {
	return control.NewPayload(inputSchema, "bytes", value)
}

func decodeInvokeInput(parameters json.RawMessage) (control.PayloadEnvelope, error) {
	var input control.AdapterInvokeParameters
	if err := json.Unmarshal(parameters, &input); err != nil {
		return control.PayloadEnvelope{}, err
	}
	if input.Input.SchemaVersion != inputSchema || input.Input.Encoding != "bytes" {
		return control.PayloadEnvelope{}, errors.New("HASHICORP_RAFT_INPUT_SCHEMA_MISMATCH")
	}
	return input.Input, nil
}

type rpcCall struct {
	kind     string
	item     control.ProducedItem
	request  any
	response any
	done     chan error
}

type runtimeTransport struct {
	owner    control.NodeRef
	incoming chan hraft.RPC
	outbound chan<- *rpcCall
	cancel   <-chan struct{}
	mu       sync.Mutex
	sequence map[string]uint64
}

func newRuntimeTransport(owner control.NodeRef, outbound chan<- *rpcCall, cancel <-chan struct{}) *runtimeTransport {
	return &runtimeTransport{owner: owner, incoming: make(chan hraft.RPC), outbound: outbound, cancel: cancel, sequence: make(map[string]uint64)}
}

func (t *runtimeTransport) Consumer() <-chan hraft.RPC     { return t.incoming }
func (t *runtimeTransport) LocalAddr() hraft.ServerAddress { return hraft.ServerAddress(t.owner.Node) }
func (t *runtimeTransport) EncodePeer(_ hraft.ServerID, addr hraft.ServerAddress) []byte {
	return []byte(addr)
}
func (t *runtimeTransport) DecodePeer(value []byte) hraft.ServerAddress {
	return hraft.ServerAddress(value)
}
func (t *runtimeTransport) SetHeartbeatHandler(func(hraft.RPC)) {}
func (t *runtimeTransport) AppendEntriesPipeline(hraft.ServerID, hraft.ServerAddress) (hraft.AppendPipeline, error) {
	return nil, hraft.ErrPipelineReplicationNotSupported
}
func (t *runtimeTransport) RequestVote(_ hraft.ServerID, target hraft.ServerAddress, req *hraft.RequestVoteRequest, resp *hraft.RequestVoteResponse) error {
	return t.call("request-vote", target, req, resp)
}
func (t *runtimeTransport) AppendEntries(_ hraft.ServerID, target hraft.ServerAddress, req *hraft.AppendEntriesRequest, resp *hraft.AppendEntriesResponse) error {
	return t.call("append-entries", target, req, resp)
}
func (t *runtimeTransport) InstallSnapshot(hraft.ServerID, hraft.ServerAddress, *hraft.InstallSnapshotRequest, *hraft.InstallSnapshotResponse, io.Reader) error {
	return errors.New("HASHICORP_RAFT_SNAPSHOT_UNSUPPORTED")
}
func (t *runtimeTransport) TimeoutNow(hraft.ServerID, hraft.ServerAddress, *hraft.TimeoutNowRequest, *hraft.TimeoutNowResponse) error {
	return errors.New("HASHICORP_RAFT_TIMEOUT_NOW_UNSUPPORTED")
}

func (t *runtimeTransport) call(kind string, target hraft.ServerAddress, request, response any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(struct {
		Kind   string          `json:"kind"`
		Source control.NodeID  `json:"source"`
		Target string          `json:"target"`
		Body   json.RawMessage `json:"body"`
	}{kind, t.owner.Node, string(target), body})
	if err != nil {
		return err
	}
	payload, err := control.NewPayload(runtimeRPCSchema, "json", encoded)
	if err != nil {
		return err
	}
	t.mu.Lock()
	key := kind + "\x00" + string(target)
	t.sequence[key]++
	sequence := t.sequence[key]
	t.mu.Unlock()
	idParts := []string{string(t.owner.Node), string(target), kind, strconv.FormatUint(sequence, 10), payload.Digest}
	if t.owner.Incarnation > 1 {
		idParts = append(idParts, strconv.FormatUint(t.owner.Incarnation, 10))
	}
	id, _ := control.StableID("hashicorp-runtime-rpc", idParts...)
	owner := t.owner
	call := &rpcCall{kind: kind, request: request, response: response, done: make(chan error, 1),
		item: control.ProducedItem{ID: control.ItemID(id), Kind: control.ItemMessage, Owner: owner,
			Message: &control.MessageEnvelope{ID: control.MessageID(id), Source: owner, Target: control.NodeID(target), TypeHint: kind, Payload: payload}}}
	select {
	case t.outbound <- call:
	case <-t.cancel:
		return errors.New("HASHICORP_RAFT_TRANSPORT_CLOSED")
	}
	select {
	case err := <-call.done:
		return err
	case <-t.cancel:
		return errors.New("HASHICORP_RAFT_TRANSPORT_CLOSED")
	}
}

func assignRPCResponse(destination, response any) error {
	target, value := reflect.ValueOf(destination), reflect.ValueOf(response)
	if target.Kind() != reflect.Pointer || value.Kind() != reflect.Pointer || target.Type() != value.Type() {
		return errors.New("HASHICORP_RAFT_RESPONSE_UNSUPPORTED")
	}
	target.Elem().Set(value.Elem())
	return nil
}

type recordingFSM struct {
	node    control.NodeID
	applied chan<- appliedRecord
}

func (fsm recordingFSM) Apply(log *hraft.Log) interface{} {
	payload, _ := control.NewPayload(inputSchema, "bytes", log.Data)
	fsm.applied <- appliedRecord{Node: fsm.node, Index: log.Index, Digest: payload.Digest}
	return nil
}
func (recordingFSM) Snapshot() (hraft.FSMSnapshot, error) { return emptySnapshot{}, nil }
func (recordingFSM) Restore(io.ReadCloser) error          { return nil }

type emptySnapshot struct{}

func (emptySnapshot) Persist(sink hraft.SnapshotSink) error { return sink.Close() }
func (emptySnapshot) Release()                              {}

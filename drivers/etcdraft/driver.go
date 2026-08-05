package etcdraft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"sort"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
)

const (
	Protocol = "etcd-raft-v3.6"
	Version  = "v3.6.0"
)

type Config struct {
	Nodes         []string
	ElectionTick  int
	HeartbeatTick int
}

type Driver struct {
	order      []string
	ids        map[string]uint64
	names      map[uint64]string
	peers      []raft.Peer
	config     Config
	nodes      map[string]*node
	capability driver.Manifest
}

type node struct {
	name        string
	id          uint64
	running     bool
	epoch       uint64
	batchSeq    uint64
	raw         *raft.RawNode
	storage     *raftStorage
	durable     storageImage
	app         applicationImage
	outstanding *nativeBatch
}

type nativeBatch struct {
	token string
	ready raft.Ready
}

type applicationImage struct {
	Applied   uint64            `json:"applied"`
	ConfState pb.ConfState      `json:"conf_state"`
	Values    map[uint64]string `json:"values,omitempty"`
}

type logEntrySnapshot struct {
	Index       uint64 `json:"index"`
	Term        uint64 `json:"term"`
	Type        string `json:"type"`
	ValueDigest string `json:"value_digest,omitempty"`
}

func New(nodeIDs []string) (*Driver, error) {
	return NewWithConfig(Config{Nodes: nodeIDs, ElectionTick: 10, HeartbeatTick: 1})
}

func NewWithConfig(config Config) (*Driver, error) {
	order := append([]string(nil), config.Nodes...)
	sort.Strings(order)
	if len(order) < 3 {
		return nil, errors.New("etcd/raft profile requires at least three nodes")
	}
	if config.ElectionTick == 0 {
		config.ElectionTick = 10
	}
	if config.HeartbeatTick == 0 {
		config.HeartbeatTick = 1
	}
	ids := make(map[string]uint64, len(order))
	names := make(map[uint64]string, len(order))
	peers := make([]raft.Peer, 0, len(order))
	for index, name := range order {
		if name == "" {
			return nil, errors.New("node IDs cannot be empty")
		}
		if _, exists := ids[name]; exists {
			return nil, fmt.Errorf("duplicate node ID %q", name)
		}
		id := uint64(index + 1)
		ids[name] = id
		names[id] = name
		peers = append(peers, raft.Peer{ID: id})
	}
	d := &Driver{
		order: order, ids: ids, names: names, peers: peers, config: config,
		nodes: make(map[string]*node, len(order)),
		capability: driver.Manifest{
			Driver: "embedded-etcdraft", SUT: "go.etcd.io/raft/v3", SUTVersion: Version,
			Capabilities: []driver.Capability{
				{ID: "explicit-campaign", Supported: true},
				{ID: "message-release-control", Supported: true},
				{ID: "message-drop-duplicate-partition", Supported: true},
				{ID: "visible-write-durable-sync", Supported: true},
				{ID: "power-loss-restart", Supported: true},
				{ID: "ready-crash-cutpoints", Supported: true},
				{ID: "application-apply", Supported: true, Detail: "application apply is modeled as an atomic durable operation"},
				{ID: "exact-ready-send-barriers", Supported: false, Detail: "v1 conservatively syncs the current Ready before release"},
				{ID: "natural-election-timeout-replay", Supported: false, Detail: "official v3.6 does not expose its randomized election timeout"},
				{ID: "async-storage-writes", Supported: false, Detail: "v1 disables etcd/raft AsyncStorageWrites"},
				{ID: "snapshot-delivery-feedback", Supported: false, Detail: "message drop feedback is not yet routed to ReportSnapshot"},
			},
		},
	}
	for _, name := range order {
		n := &node{name: name, id: ids[name], running: true, epoch: 1, app: newApplicationImage()}
		if err := d.boot(n); err != nil {
			return nil, fmt.Errorf("boot node %s: %w", name, err)
		}
		d.nodes[name] = n
	}
	return d, nil
}

func (d *Driver) Protocol() string { return Protocol }

func (d *Driver) Nodes() []string { return append([]string(nil), d.order...) }

func (d *Driver) Capabilities() driver.Manifest { return d.capability }

func (d *Driver) EnabledInput(event core.Event) (bool, string) {
	n, ok := d.nodes[event.Target]
	if !ok {
		return false, "unknown target node"
	}
	if !n.running || n.raw == nil {
		return false, "node is stopped"
	}
	if n.outstanding != nil {
		return false, "native Ready is outstanding"
	}
	switch event.Kind {
	case core.EventCampaign, core.EventMessage:
		return true, ""
	case core.EventPropose:
		if n.raw.BasicStatus().RaftState != raft.StateLeader {
			return false, "node is not the current Raft leader"
		}
		return true, ""
	case core.EventTimeout:
		return false, "natural election timeout replay is unsupported by the official v3.6 build"
	default:
		return false, "unsupported driver input"
	}
}

func (d *Driver) Invoke(_ context.Context, event core.Event) ([]core.Observation, error) {
	n := d.nodes[event.Target]
	before := n.raw.BasicStatus()
	var err error
	switch event.Kind {
	case core.EventCampaign:
		err = n.raw.Campaign()
	case core.EventPropose:
		var request struct {
			Value string `json:"value"`
		}
		if decodeErr := json.Unmarshal(event.Payload, &request); decodeErr != nil {
			return nil, fmt.Errorf("decode proposal: %w", decodeErr)
		}
		if request.Value == "" {
			return nil, errors.New("proposal value is required")
		}
		err = n.raw.Propose([]byte(request.Value))
	case core.EventMessage:
		message, decodeErr := d.decodeMessage(event)
		if decodeErr != nil {
			return nil, decodeErr
		}
		err = n.raw.Step(message)
	case core.EventTimeout:
		return nil, errors.New("natural election timeout is unsupported in the certified v1 driver")
	default:
		return nil, fmt.Errorf("unsupported input %q", event.Kind)
	}
	if err != nil {
		return nil, err
	}
	after := n.raw.BasicStatus()
	return transitionObservations(n.name, before, after), nil
}

func (d *Driver) Poll(nodeName string) (*driver.OutputBatch, error) {
	n, ok := d.nodes[nodeName]
	if !ok {
		return nil, fmt.Errorf("unknown node %q", nodeName)
	}
	if !n.running || n.raw == nil {
		return nil, nil
	}
	if n.outstanding != nil {
		return nil, fmt.Errorf("node %s already has native batch %s", nodeName, n.outstanding.token)
	}
	if !n.raw.HasReady() {
		return nil, nil
	}
	n.batchSeq++
	token := fmt.Sprintf("%s-ready-%06d", nodeName, n.batchSeq)
	ready, err := cloneReady(n.raw.Ready())
	if err != nil {
		return nil, fmt.Errorf("freeze Ready: %w", err)
	}
	n.outstanding = &nativeBatch{token: token, ready: ready}
	operations, err := d.operationsFor(n, ready)
	if err != nil {
		return nil, err
	}
	return &driver.OutputBatch{Token: token, Node: nodeName, Operations: operations}, nil
}

func (d *Driver) ExecuteHostOp(_ context.Context, nodeName, batchToken string, operation driver.Operation) ([]core.Observation, error) {
	n, ok := d.nodes[nodeName]
	if !ok || n.outstanding == nil || n.outstanding.token != batchToken {
		return nil, fmt.Errorf("node %s has no native batch %s", nodeName, batchToken)
	}
	ready := n.outstanding.ready
	switch operation.Kind {
	case core.EventPersist:
		if err := n.storage.writeReady(ready); err != nil {
			return nil, fmt.Errorf("persist Ready: %w", err)
		}
		return []core.Observation{{
			Kind: "persistence", Label: "host:persist", Node: nodeName, Value: batchToken,
		}}, nil
	case core.EventSync:
		image, err := n.storage.capture()
		if err != nil {
			return nil, fmt.Errorf("sync Ready: %w", err)
		}
		n.durable = image
		return []core.Observation{{
			Kind: "persistence", Label: "host:sync", Node: nodeName, Value: batchToken,
		}}, nil
	case core.EventEmit:
		return []core.Observation{{
			Kind: "transport", Label: "message:released", Node: nodeName,
			Value:    operation.Message.PayloadDigest,
			Evidence: map[string]string{"type": operation.Message.TypeHint, "to": operation.Message.To},
		}}, nil
	case core.EventApply:
		return d.applyReady(n, ready)
	case core.EventAcknowledge:
		n.raw.Advance(ready)
		n.outstanding = nil
		return []core.Observation{{
			Kind: "host", Label: "host:acknowledge", Node: nodeName, Value: batchToken,
		}}, nil
	default:
		return nil, fmt.Errorf("unsupported host operation %q", operation.Kind)
	}
}

func (d *Driver) Crash(_ context.Context, nodeName string) ([]core.Observation, error) {
	n, ok := d.nodes[nodeName]
	if !ok || !n.running {
		return nil, fmt.Errorf("node %s is not running", nodeName)
	}
	n.raw = nil
	n.storage = nil
	n.outstanding = nil
	n.running = false
	return []core.Observation{{Kind: "fault", Label: "fault:crash", Node: nodeName}}, nil
}

func (d *Driver) Restart(_ context.Context, nodeName string) ([]core.Observation, error) {
	n, ok := d.nodes[nodeName]
	if !ok || n.running {
		return nil, fmt.Errorf("node %s is not stopped", nodeName)
	}
	n.epoch++
	if err := d.boot(n); err != nil {
		return nil, err
	}
	n.running = true
	return []core.Observation{{Kind: "fault", Label: "fault:restart", Node: nodeName}}, nil
}

func (d *Driver) Snapshot() any {
	type nodeSnapshot struct {
		Running              bool               `json:"running"`
		Epoch                uint64             `json:"epoch"`
		Role                 string             `json:"role"`
		Term                 uint64             `json:"term"`
		Vote                 string             `json:"vote,omitempty"`
		Lead                 string             `json:"lead,omitempty"`
		Commit               uint64             `json:"commit"`
		Applied              uint64             `json:"applied"`
		DurableTerm          uint64             `json:"durable_term"`
		DurableCommit        uint64             `json:"durable_commit"`
		DurableLastIndex     uint64             `json:"durable_last_index"`
		DurableLastTerm      uint64             `json:"durable_last_term"`
		DurableSnapshotIndex uint64             `json:"durable_snapshot_index"`
		DurableSnapshotTerm  uint64             `json:"durable_snapshot_term"`
		DurableLog           []logEntrySnapshot `json:"durable_log,omitempty"`
		Voters               []string           `json:"voters,omitempty"`
		Outstanding          string             `json:"outstanding,omitempty"`
		Values               map[uint64]string  `json:"values,omitempty"`
	}
	out := make(map[string]nodeSnapshot, len(d.nodes))
	for name, n := range d.nodes {
		current := nodeSnapshot{
			Running: n.running, Epoch: n.epoch, Applied: n.app.Applied,
			DurableTerm: n.durable.HardState.Term, DurableCommit: n.durable.HardState.Commit,
			DurableSnapshotIndex: n.durable.Snapshot.Metadata.Index,
			DurableSnapshotTerm:  n.durable.Snapshot.Metadata.Term,
			DurableLog:           summarizeEntries(n.durable.Entries),
			Voters:               d.voterNames(n.app.ConfState), Values: cloneValues(n.app.Values),
		}
		if len(n.durable.Entries) > 0 {
			current.DurableLastIndex = n.durable.Entries[len(n.durable.Entries)-1].Index
			current.DurableLastTerm = n.durable.Entries[len(n.durable.Entries)-1].Term
		} else {
			current.DurableLastIndex = n.durable.Snapshot.Metadata.Index
			current.DurableLastTerm = n.durable.Snapshot.Metadata.Term
		}
		if n.raw != nil {
			status := n.raw.BasicStatus()
			current.Role = roleName(status.RaftState)
			current.Term = status.Term
			current.Vote = d.nameFor(status.Vote)
			current.Lead = d.nameFor(status.Lead)
			current.Commit = status.Commit
		} else {
			current.Role = "stopped"
			current.Term = n.durable.HardState.Term
			current.Commit = n.durable.HardState.Commit
		}
		if n.outstanding != nil {
			current.Outstanding = n.outstanding.token
		}
		out[name] = current
	}
	return map[string]any{"nodes": out}
}

func (d *Driver) CheckConformance() error {
	if len(d.order) < 3 || len(d.order) != len(d.nodes) {
		return errors.New("etcd/raft driver requires at least three configured nodes")
	}
	for _, name := range d.order {
		n, ok := d.nodes[name]
		if !ok || n.id == 0 {
			return fmt.Errorf("invalid configured node %q", name)
		}
		if n.running && (n.raw == nil || n.storage == nil) {
			return fmt.Errorf("running node %s has no RawNode/storage", name)
		}
		if !n.running && (n.raw != nil || n.storage != nil || n.outstanding != nil) {
			return fmt.Errorf("stopped node %s retains volatile raft state", name)
		}
	}
	return nil
}

func (d *Driver) boot(n *node) error {
	storage, err := restoreRaftStorage(n.durable, n.app.ConfState)
	if err != nil {
		return err
	}
	config := &raft.Config{
		ID: n.id, ElectionTick: d.config.ElectionTick, HeartbeatTick: d.config.HeartbeatTick,
		Storage: storage, Applied: n.app.Applied, MaxSizePerMsg: math.MaxUint64,
		MaxInflightMsgs: 256, MaxUncommittedEntriesSize: 1 << 30, Logger: raftLogger(),
	}
	raw, err := raft.NewRawNode(config)
	if err != nil {
		return err
	}
	if !n.durable.Initialized {
		if err := raw.Bootstrap(d.peers); err != nil {
			return err
		}
	}
	n.storage = storage
	n.raw = raw
	n.outstanding = nil
	return nil
}

func raftLogger() raft.Logger {
	return &raft.DefaultLogger{Logger: log.New(io.Discard, "", 0)}
}

func (d *Driver) operationsFor(n *node, ready raft.Ready) ([]driver.Operation, error) {
	operations := []driver.Operation{
		{Token: "persist", Kind: core.EventPersist},
		{Token: "sync", Kind: core.EventSync, After: []string{"persist"}},
	}
	completion := []string{"sync"}
	for index, message := range ready.Messages {
		encoded, err := message.Marshal()
		if err != nil {
			return nil, err
		}
		from, fromOK := d.names[message.From]
		to, toOK := d.names[message.To]
		if !fromOK || !toOK {
			return nil, fmt.Errorf("Ready contains message with unknown route %d -> %d", message.From, message.To)
		}
		digest := sha256.Sum256(encoded)
		token := fmt.Sprintf("emit-%03d", index)
		operations = append(operations, driver.Operation{
			Token: token, Kind: core.EventEmit, After: []string{"sync"},
			Message: &core.MessageEnvelope{
				From: from, To: to, SenderEpoch: n.epoch, TypeHint: message.Type.String(),
				Payload: encoded, PayloadDigest: hex.EncodeToString(digest[:]),
				Metadata: map[string]string{
					"term":   strconv.FormatUint(message.Term, 10),
					"index":  strconv.FormatUint(message.Index, 10),
					"commit": strconv.FormatUint(message.Commit, 10),
				},
			},
		})
		completion = append(completion, token)
	}
	operations = append(operations, driver.Operation{
		Token: "apply", Kind: core.EventApply, After: []string{"sync"},
	})
	completion = append(completion, "apply")
	operations = append(operations, driver.Operation{
		Token: "ack", Kind: core.EventAcknowledge, After: completion,
	})
	return operations, nil
}

func (d *Driver) applyReady(n *node, ready raft.Ready) ([]core.Observation, error) {
	var observations []core.Observation
	if !raft.IsEmptySnap(ready.Snapshot) && ready.Snapshot.Metadata.Index > n.app.Applied {
		n.app.Applied = ready.Snapshot.Metadata.Index
		n.app.ConfState = cloneConfState(ready.Snapshot.Metadata.ConfState)
		n.storage.confState = cloneConfState(n.app.ConfState)
		observations = append(observations, core.Observation{
			Kind: "application", Label: "raft:snapshot-applied", Node: n.name,
			Value: strconv.FormatUint(n.app.Applied, 10),
		})
	}
	for _, entry := range ready.CommittedEntries {
		if entry.Index <= n.app.Applied {
			continue
		}
		switch entry.Type {
		case pb.EntryNormal:
			if len(entry.Data) > 0 {
				value := string(entry.Data)
				n.app.Values[entry.Index] = value
				observations = append(observations, core.Observation{
					Kind: "commit", Label: "commit", Node: n.name, Value: value,
					Evidence: map[string]string{
						"index": strconv.FormatUint(entry.Index, 10),
						"term":  strconv.FormatUint(entry.Term, 10),
					},
				})
			}
		case pb.EntryConfChange:
			var change pb.ConfChange
			if err := change.Unmarshal(entry.Data); err != nil {
				return nil, fmt.Errorf("decode ConfChange at index %d: %w", entry.Index, err)
			}
			n.app.ConfState = cloneConfState(*n.raw.ApplyConfChange(change))
			n.storage.confState = cloneConfState(n.app.ConfState)
			observations = append(observations, core.Observation{
				Kind: "membership", Label: "raft:conf-change-applied", Node: n.name,
				Value: strconv.FormatUint(entry.Index, 10),
			})
		case pb.EntryConfChangeV2:
			var change pb.ConfChangeV2
			if err := change.Unmarshal(entry.Data); err != nil {
				return nil, fmt.Errorf("decode ConfChangeV2 at index %d: %w", entry.Index, err)
			}
			n.app.ConfState = cloneConfState(*n.raw.ApplyConfChange(change))
			n.storage.confState = cloneConfState(n.app.ConfState)
			observations = append(observations, core.Observation{
				Kind: "membership", Label: "raft:conf-change-applied", Node: n.name,
				Value: strconv.FormatUint(entry.Index, 10),
			})
		default:
			return nil, fmt.Errorf("unsupported committed entry type %s", entry.Type)
		}
		n.app.Applied = entry.Index
	}
	// The application image is modeled as durable at the apply boundary.
	n.app.ConfState = cloneConfState(n.storage.confState)
	n.durable.ConfState = cloneConfState(n.app.ConfState)
	observations = append(observations, core.Observation{
		Kind: "application", Label: "host:apply", Node: n.name,
		Value: strconv.FormatUint(n.app.Applied, 10),
	})
	return observations, nil
}

func (d *Driver) decodeMessage(event core.Event) (pb.Message, error) {
	if event.Message == nil {
		return pb.Message{}, errors.New("message event has no wire envelope")
	}
	if event.Message.From != event.Source || event.Message.To != event.Target {
		return pb.Message{}, errors.New("message envelope route does not match event route")
	}
	digest := sha256.Sum256(event.Message.Payload)
	if hex.EncodeToString(digest[:]) != event.Message.PayloadDigest {
		return pb.Message{}, errors.New("message payload digest does not match the envelope")
	}
	var message pb.Message
	if err := message.Unmarshal(event.Message.Payload); err != nil {
		return pb.Message{}, fmt.Errorf("decode raft message: %w", err)
	}
	if d.nameFor(message.From) != event.Source || d.nameFor(message.To) != event.Target {
		return pb.Message{}, errors.New("protobuf route does not match wire envelope")
	}
	if event.Message.TypeHint != "" && message.Type.String() != event.Message.TypeHint {
		return pb.Message{}, errors.New("protobuf type does not match wire type hint")
	}
	return message, nil
}

func (d *Driver) nameFor(id uint64) string {
	if id == 0 {
		return ""
	}
	return d.names[id]
}

func (d *Driver) voterNames(state pb.ConfState) []string {
	ids := append([]uint64(nil), state.Voters...)
	ids = append(ids, state.VotersOutgoing...)
	seen := make(map[string]bool, len(ids))
	var names []string
	for _, id := range ids {
		name := d.nameFor(id)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func summarizeEntries(entries []pb.Entry) []logEntrySnapshot {
	out := make([]logEntrySnapshot, 0, len(entries))
	for _, entry := range entries {
		current := logEntrySnapshot{Index: entry.Index, Term: entry.Term, Type: entry.Type.String()}
		if len(entry.Data) > 0 {
			digest := sha256.Sum256(entry.Data)
			current.ValueDigest = hex.EncodeToString(digest[:])
		}
		out = append(out, current)
	}
	return out
}

func newApplicationImage() applicationImage {
	return applicationImage{Values: make(map[uint64]string)}
}

func cloneValues(values map[uint64]string) map[uint64]string {
	if values == nil {
		return nil
	}
	out := make(map[uint64]string, len(values))
	for index, value := range values {
		out[index] = value
	}
	return out
}

func roleName(state raft.StateType) string {
	switch state {
	case raft.StateFollower:
		return "follower"
	case raft.StateCandidate:
		return "candidate"
	case raft.StatePreCandidate:
		return "pre-candidate"
	case raft.StateLeader:
		return "leader"
	default:
		return "unknown"
	}
}

func transitionObservations(node string, before, after raft.BasicStatus) []core.Observation {
	beforeRole, afterRole := roleName(before.RaftState), roleName(after.RaftState)
	if beforeRole == afterRole {
		return nil
	}
	return []core.Observation{{
		Kind: "transition", Label: "transition:" + beforeRole + "->" + afterRole, Node: node,
		Evidence: map[string]string{"term": strconv.FormatUint(after.Term, 10)},
	}}
}

func cloneReady(ready raft.Ready) (raft.Ready, error) {
	cloned := raft.Ready{HardState: ready.HardState, MustSync: ready.MustSync}
	if ready.SoftState != nil {
		softState := *ready.SoftState
		cloned.SoftState = &softState
	}
	cloned.Entries = cloneEntries(ready.Entries)
	cloned.CommittedEntries = cloneEntries(ready.CommittedEntries)
	cloned.Snapshot = cloneSnapshot(ready.Snapshot)
	cloned.ReadStates = make([]raft.ReadState, len(ready.ReadStates))
	for index, state := range ready.ReadStates {
		cloned.ReadStates[index] = state
		cloned.ReadStates[index].RequestCtx = append([]byte(nil), state.RequestCtx...)
	}
	cloned.Messages = make([]pb.Message, len(ready.Messages))
	for index, message := range ready.Messages {
		encoded, err := message.Marshal()
		if err != nil {
			return raft.Ready{}, err
		}
		if err := cloned.Messages[index].Unmarshal(encoded); err != nil {
			return raft.Ready{}, err
		}
	}
	return cloned, nil
}

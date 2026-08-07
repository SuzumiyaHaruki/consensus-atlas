package etcdraftv2

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
)

const (
	adapterID       = "official-etcdraft-v2alpha1"
	implementation  = "go.etcd.io/raft/v3"
	implementationV = "v3.6.0"
	buildID         = "go.etcd.io/raft/v3@v3.6.0"

	evidenceSchema = "consensus-atlas/etcdraft-v2-evidence/v1"
	readySchema    = "consensus-atlas/etcdraft-v2-ready-effect/v1"
	messageSchema  = "consensus-atlas/etcdraft-v2-message/v1"
	callbackSchema = "consensus-atlas/etcdraft-v2-callback/v1"
	observationV1  = "consensus-atlas/etcdraft-v2-observation/v1"
	commandSchema  = "consensus-atlas/adapter-command/v1"

	effectReadyPersist = "raft-ready-persist"
	effectReadyAdvance = "raft-ready-advance"
)

type NodeConfig struct {
	Node   control.NodeID `json:"node"`
	RaftID uint64         `json:"raft_id"`
}

type Config struct {
	Nodes         []NodeConfig `json:"nodes"`
	ElectionTick  int          `json:"election_tick"`
	HeartbeatTick int          `json:"heartbeat_tick"`
}

func DefaultConfig() Config {
	return SingleNodeConfig()
}

func SingleNodeConfig() Config {
	return Config{
		Nodes: []NodeConfig{{Node: "n1", RaftID: 1}}, ElectionTick: 5, HeartbeatTick: 1,
	}
}

func ThreeNodeConfig() Config {
	return Config{
		Nodes: []NodeConfig{
			{Node: "n1", RaftID: 1}, {Node: "n2", RaftID: 2}, {Node: "n3", RaftID: 3},
		},
		ElectionTick: 5, HeartbeatTick: 1,
	}
}

func (config Config) validate() error {
	if len(config.Nodes) == 0 {
		return fmt.Errorf("ETCDRAFT_V2_NODES_REQUIRED")
	}
	if config.ElectionTick <= config.HeartbeatTick || config.HeartbeatTick <= 0 {
		return fmt.Errorf("ETCDRAFT_V2_TICK_CONFIG_INVALID")
	}
	names := make(map[control.NodeID]struct{}, len(config.Nodes))
	ids := make(map[uint64]struct{}, len(config.Nodes))
	for _, node := range config.Nodes {
		if node.Node == "" || node.RaftID == 0 {
			return fmt.Errorf("ETCDRAFT_V2_NODE_IDENTITY_REQUIRED")
		}
		if _, ok := names[node.Node]; ok {
			return fmt.Errorf("ETCDRAFT_V2_NODE_DUPLICATE: %s", node.Node)
		}
		if _, ok := ids[node.RaftID]; ok {
			return fmt.Errorf("ETCDRAFT_V2_RAFT_ID_DUPLICATE: %d", node.RaftID)
		}
		names[node.Node] = struct{}{}
		ids[node.RaftID] = struct{}{}
	}
	return nil
}

func (config Config) normalized() Config {
	normalized := config
	normalized.Nodes = append([]NodeConfig(nil), config.Nodes...)
	sort.Slice(normalized.Nodes, func(i, j int) bool {
		return normalized.Nodes[i].Node < normalized.Nodes[j].Node
	})
	return normalized
}

func (config Config) digest() (string, error) {
	if err := config.validate(); err != nil {
		return "", err
	}
	return control.CanonicalDigest(config.normalized())
}

func (config Config) nodeIDs() []control.NodeID {
	normalized := config.normalized()
	result := make([]control.NodeID, len(normalized.Nodes))
	for index, node := range normalized.Nodes {
		result[index] = node.Node
	}
	return result
}

type commandEnvelope struct {
	LogicalTime uint64                `json:"logical_time"`
	Parameters  json.RawMessage       `json:"parameters,omitempty"`
	Item        *control.ProducedItem `json:"item,omitempty"`
}

type resultParameters struct {
	Result string `json:"result"`
}

type modeParameters struct {
	Mode string `json:"mode"`
}

type invokeParameters struct {
	Input control.PayloadEnvelope `json:"input"`
}

type readyRequest struct {
	ReadyID  string `json:"ready_id"`
	Digest   string `json:"digest"`
	Phase    string `json:"phase"`
	MustSync bool   `json:"must_sync"`
}

type readyObservation struct {
	ReadyID         string `json:"ready_id"`
	Digest          string `json:"digest"`
	MustSync        bool   `json:"must_sync"`
	Entries         int    `json:"entries"`
	Committed       int    `json:"committed_entries"`
	Messages        int    `json:"messages"`
	HardStateEmpty  bool   `json:"hard_state_empty"`
	SnapshotEmpty   bool   `json:"snapshot_empty"`
	CurrentRaftTerm uint64 `json:"current_raft_term"`
}

type clusterSnapshot struct {
	LogicalTime   uint64         `json:"logical_time"`
	YieldSequence uint64         `json:"yield_sequence"`
	ItemSequence  uint64         `json:"item_sequence"`
	ReadySequence uint64         `json:"ready_sequence"`
	Nodes         []nodeSnapshot `json:"nodes"`
	EntropyScope  string         `json:"entropy_scope"`
}

type nodeSnapshot struct {
	Node                control.NodeID `json:"node"`
	RaftID              uint64         `json:"raft_id"`
	Incarnation         uint64         `json:"incarnation"`
	Running             bool           `json:"running"`
	TickCount           uint64         `json:"tick_count"`
	AdvanceCount        uint64         `json:"advance_count"`
	Applied             uint64         `json:"applied"`
	Role                string         `json:"role"`
	Term                uint64         `json:"term"`
	Vote                uint64         `json:"vote"`
	Commit              uint64         `json:"commit"`
	Lead                uint64         `json:"lead"`
	StorageTerm         uint64         `json:"storage_term"`
	StorageVote         uint64         `json:"storage_vote"`
	StorageCommit       uint64         `json:"storage_commit"`
	StorageLastIndex    uint64         `json:"storage_last_index"`
	ConfState           pb.ConfState   `json:"conf_state"`
	DurableImageDigest  string         `json:"durable_image_digest"`
	ApplicationDigest   string         `json:"application_digest"`
	ApplicationCommands int            `json:"application_commands"`
	OutstandingReadyID  string         `json:"outstanding_ready_id,omitempty"`
	OutstandingDigest   string         `json:"outstanding_digest,omitempty"`
	PulseItemID         control.ItemID `json:"pulse_item_id,omitempty"`
}

type readyRecord struct {
	node          control.NodeID
	id            string
	digest        string
	ready         raft.Ready
	persisted     bool
	persistEffect control.ItemID
	advanceEffect control.ItemID
}

type readyIdentity struct {
	SoftState        *softStateIdentity `json:"soft_state,omitempty"`
	HardState        []byte             `json:"hard_state,omitempty"`
	ReadStates       []readStateID      `json:"read_states,omitempty"`
	Entries          [][]byte           `json:"entries,omitempty"`
	Snapshot         []byte             `json:"snapshot,omitempty"`
	CommittedEntries [][]byte           `json:"committed_entries,omitempty"`
	Messages         [][]byte           `json:"messages,omitempty"`
	MustSync         bool               `json:"must_sync"`
}

type softStateIdentity struct {
	Lead  uint64 `json:"lead"`
	State string `json:"state"`
}

type readStateID struct {
	Index      uint64 `json:"index"`
	RequestCtx []byte `json:"request_ctx"`
}

func cloneReady(ready raft.Ready) (raft.Ready, error) {
	copyReady := ready
	if ready.SoftState != nil {
		soft := *ready.SoftState
		copyReady.SoftState = &soft
	}
	copyReady.ReadStates = make([]raft.ReadState, len(ready.ReadStates))
	for index, state := range ready.ReadStates {
		copyReady.ReadStates[index] = state
		copyReady.ReadStates[index].RequestCtx = append([]byte(nil), state.RequestCtx...)
	}
	copyReady.Entries = cloneEntries(ready.Entries)
	copyReady.CommittedEntries = cloneEntries(ready.CommittedEntries)
	copyReady.Snapshot = cloneSnapshot(ready.Snapshot)
	copyReady.Messages = make([]pb.Message, len(ready.Messages))
	for index := range ready.Messages {
		encoded, err := ready.Messages[index].Marshal()
		if err != nil {
			return raft.Ready{}, err
		}
		if err := copyReady.Messages[index].Unmarshal(encoded); err != nil {
			return raft.Ready{}, err
		}
	}
	return copyReady, nil
}

func readyDigest(ready raft.Ready) (string, error) {
	identity := readyIdentity{MustSync: ready.MustSync}
	if ready.SoftState != nil {
		identity.SoftState = &softStateIdentity{Lead: ready.SoftState.Lead, State: ready.SoftState.RaftState.String()}
	}
	hardState, err := ready.HardState.Marshal()
	if err != nil {
		return "", err
	}
	identity.HardState = hardState
	for _, state := range ready.ReadStates {
		identity.ReadStates = append(identity.ReadStates, readStateID{
			Index: state.Index, RequestCtx: append([]byte(nil), state.RequestCtx...),
		})
	}
	if identity.Entries, err = marshalEntries(ready.Entries); err != nil {
		return "", err
	}
	if identity.CommittedEntries, err = marshalEntries(ready.CommittedEntries); err != nil {
		return "", err
	}
	if identity.Snapshot, err = ready.Snapshot.Marshal(); err != nil {
		return "", err
	}
	for index := range ready.Messages {
		encoded, err := ready.Messages[index].Marshal()
		if err != nil {
			return "", err
		}
		identity.Messages = append(identity.Messages, encoded)
	}
	return control.CanonicalDigest(identity)
}

func marshalEntries(entries []pb.Entry) ([][]byte, error) {
	result := make([][]byte, 0, len(entries))
	for index := range entries {
		encoded, err := entries[index].Marshal()
		if err != nil {
			return nil, err
		}
		result = append(result, encoded)
	}
	return result, nil
}

func cloneEntries(entries []pb.Entry) []pb.Entry {
	result := make([]pb.Entry, len(entries))
	for index := range entries {
		result[index] = entries[index]
		result[index].Data = append([]byte(nil), entries[index].Data...)
	}
	return result
}

func cloneSnapshot(snapshot pb.Snapshot) pb.Snapshot {
	snapshot.Data = append([]byte(nil), snapshot.Data...)
	snapshot.Metadata.ConfState = cloneConfState(snapshot.Metadata.ConfState)
	return snapshot
}

func cloneConfState(state pb.ConfState) pb.ConfState {
	state.Voters = append([]uint64(nil), state.Voters...)
	state.Learners = append([]uint64(nil), state.Learners...)
	state.VotersOutgoing = append([]uint64(nil), state.VotersOutgoing...)
	state.LearnersNext = append([]uint64(nil), state.LearnersNext...)
	return state
}

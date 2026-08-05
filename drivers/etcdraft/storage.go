package etcdraft

import (
	"math"

	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
)

// raftStorage keeps the implementation-visible store separate from the
// durable image controlled by the host sync operation. ConfState is application
// metadata in etcd/raft and is exposed through InitialState on restart.
type raftStorage struct {
	memory    *raft.MemoryStorage
	confState pb.ConfState
}

type storageImage struct {
	Initialized bool         `json:"initialized"`
	HardState   pb.HardState `json:"hard_state"`
	ConfState   pb.ConfState `json:"conf_state"`
	Snapshot    pb.Snapshot  `json:"snapshot"`
	Entries     []pb.Entry   `json:"entries,omitempty"`
}

func newRaftStorage() *raftStorage {
	return &raftStorage{memory: raft.NewMemoryStorage()}
}

func restoreRaftStorage(image storageImage, appliedConf pb.ConfState) (*raftStorage, error) {
	storage := newRaftStorage()
	if !raft.IsEmptySnap(image.Snapshot) {
		if err := storage.memory.ApplySnapshot(cloneSnapshot(image.Snapshot)); err != nil {
			return nil, err
		}
	}
	if !raft.IsEmptyHardState(image.HardState) {
		if err := storage.memory.SetHardState(image.HardState); err != nil {
			return nil, err
		}
	}
	if len(image.Entries) > 0 {
		if err := storage.memory.Append(cloneEntries(image.Entries)); err != nil {
			return nil, err
		}
	}
	storage.confState = cloneConfState(image.ConfState)
	if hasConfState(appliedConf) {
		storage.confState = cloneConfState(appliedConf)
	}
	return storage, nil
}

func (s *raftStorage) InitialState() (pb.HardState, pb.ConfState, error) {
	hardState, _, err := s.memory.InitialState()
	return hardState, cloneConfState(s.confState), err
}

func (s *raftStorage) Entries(lo, hi, maxSize uint64) ([]pb.Entry, error) {
	return s.memory.Entries(lo, hi, maxSize)
}

func (s *raftStorage) Term(index uint64) (uint64, error) { return s.memory.Term(index) }

func (s *raftStorage) LastIndex() (uint64, error) { return s.memory.LastIndex() }

func (s *raftStorage) FirstIndex() (uint64, error) { return s.memory.FirstIndex() }

func (s *raftStorage) Snapshot() (pb.Snapshot, error) { return s.memory.Snapshot() }

func (s *raftStorage) writeReady(ready raft.Ready) error {
	if !raft.IsEmptySnap(ready.Snapshot) {
		if err := s.memory.ApplySnapshot(cloneSnapshot(ready.Snapshot)); err != nil {
			return err
		}
		s.confState = cloneConfState(ready.Snapshot.Metadata.ConfState)
	}
	if !raft.IsEmptyHardState(ready.HardState) {
		if err := s.memory.SetHardState(ready.HardState); err != nil {
			return err
		}
	}
	if len(ready.Entries) > 0 {
		if err := s.memory.Append(cloneEntries(ready.Entries)); err != nil {
			return err
		}
	}
	return nil
}

func (s *raftStorage) capture() (storageImage, error) {
	hardState, _, err := s.memory.InitialState()
	if err != nil {
		return storageImage{}, err
	}
	snapshot, err := s.memory.Snapshot()
	if err != nil {
		return storageImage{}, err
	}
	first, err := s.memory.FirstIndex()
	if err != nil {
		return storageImage{}, err
	}
	last, err := s.memory.LastIndex()
	if err != nil {
		return storageImage{}, err
	}
	var entries []pb.Entry
	if first <= last {
		entries, err = s.memory.Entries(first, last+1, math.MaxUint64)
		if err != nil {
			return storageImage{}, err
		}
	}
	return storageImage{
		Initialized: true,
		HardState:   hardState,
		ConfState:   cloneConfState(s.confState),
		Snapshot:    cloneSnapshot(snapshot),
		Entries:     cloneEntries(entries),
	}, nil
}

func hasConfState(state pb.ConfState) bool {
	return len(state.Voters)+len(state.Learners)+len(state.VotersOutgoing)+
		len(state.LearnersNext) > 0 || state.AutoLeave
}

func cloneConfState(state pb.ConfState) pb.ConfState {
	state.Voters = append([]uint64(nil), state.Voters...)
	state.Learners = append([]uint64(nil), state.Learners...)
	state.VotersOutgoing = append([]uint64(nil), state.VotersOutgoing...)
	state.LearnersNext = append([]uint64(nil), state.LearnersNext...)
	return state
}

func cloneEntries(entries []pb.Entry) []pb.Entry {
	out := make([]pb.Entry, len(entries))
	for index, entry := range entries {
		out[index] = entry
		out[index].Data = append([]byte(nil), entry.Data...)
	}
	return out
}

func cloneSnapshot(snapshot pb.Snapshot) pb.Snapshot {
	snapshot.Data = append([]byte(nil), snapshot.Data...)
	snapshot.Metadata.ConfState = cloneConfState(snapshot.Metadata.ConfState)
	return snapshot
}

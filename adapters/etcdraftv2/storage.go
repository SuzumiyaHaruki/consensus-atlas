package etcdraftv2

import (
	"bytes"
	"fmt"
	"math"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
)

const durableImageSchema = "consensus-atlas/etcdraft-v2-durable-image/v1"

// durableImage contains only state owned by the host application. It never
// serializes RawNode private memory. Entries at or below Applied are represented
// by Snapshot; later persisted entries remain explicit.
type durableImage struct {
	SchemaVersion string   `json:"schema_version"`
	HardState     []byte   `json:"hard_state"`
	Snapshot      []byte   `json:"snapshot"`
	Entries       [][]byte `json:"entries,omitempty"`
	Applied       uint64   `json:"applied"`
	ConfState     []byte   `json:"conf_state"`
	Application   []byte   `json:"application"`
	Digest        string   `json:"digest"`
}

type durableImageState struct {
	hardState   pb.HardState
	snapshot    pb.Snapshot
	entries     []pb.Entry
	applied     uint64
	confState   pb.ConfState
	application applicationImage
}

func captureDurableImage(
	storage *raft.MemoryStorage,
	applied uint64,
	confState pb.ConfState,
	application applicationImage,
) (durableImage, error) {
	if storage == nil {
		return durableImage{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_STORAGE_REQUIRED")
	}
	if err := application.validate(); err != nil {
		return durableImage{}, err
	}
	if application.Applied != applied {
		return durableImage{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_APPLICATION_APPLIED_MISMATCH")
	}
	applicationBytes, err := application.encode()
	if err != nil {
		return durableImage{}, err
	}
	hardState, _, err := storage.InitialState()
	if err != nil {
		return durableImage{}, err
	}
	storedSnapshot, err := storage.Snapshot()
	if err != nil {
		return durableImage{}, err
	}
	snapshot := cloneSnapshot(storedSnapshot)
	if applied > 0 {
		term, err := storage.Term(applied)
		if err != nil {
			return durableImage{}, fmt.Errorf("durable snapshot term at %d: %w", applied, err)
		}
		snapshot = pb.Snapshot{
			Data: append([]byte(nil), applicationBytes...),
			Metadata: pb.SnapshotMetadata{
				Index: applied, Term: term, ConfState: cloneConfState(confState),
			},
		}
	}
	lastIndex, err := storage.LastIndex()
	if err != nil {
		return durableImage{}, err
	}
	var entries []pb.Entry
	if snapshot.Metadata.Index < lastIndex {
		entries, err = storage.Entries(snapshot.Metadata.Index+1, lastIndex+1, math.MaxUint64)
		if err != nil {
			return durableImage{}, err
		}
		entries = cloneEntries(entries)
	}
	hardStateBytes, err := hardState.Marshal()
	if err != nil {
		return durableImage{}, err
	}
	snapshotBytes, err := snapshot.Marshal()
	if err != nil {
		return durableImage{}, err
	}
	confStateBytes, err := confState.Marshal()
	if err != nil {
		return durableImage{}, err
	}
	entryBytes, err := marshalEntries(entries)
	if err != nil {
		return durableImage{}, err
	}
	image := durableImage{
		SchemaVersion: durableImageSchema,
		HardState:     hardStateBytes, Snapshot: snapshotBytes, Entries: entryBytes,
		Applied: applied, ConfState: confStateBytes, Application: applicationBytes,
	}
	image.Digest, err = image.digest()
	if err != nil {
		return durableImage{}, err
	}
	return image, nil
}

func (image durableImage) digest() (string, error) {
	image.Digest = ""
	return control.CanonicalDigest(image)
}

func (image durableImage) decode() (durableImageState, error) {
	if image.SchemaVersion != durableImageSchema || image.Digest == "" {
		return durableImageState{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_IMAGE_IDENTITY_INVALID")
	}
	digest, err := image.digest()
	if err != nil {
		return durableImageState{}, err
	}
	if digest != image.Digest {
		return durableImageState{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_IMAGE_DIGEST_MISMATCH")
	}
	state := durableImageState{applied: image.Applied}
	if err := state.hardState.Unmarshal(image.HardState); err != nil {
		return durableImageState{}, err
	}
	if err := state.snapshot.Unmarshal(image.Snapshot); err != nil {
		return durableImageState{}, err
	}
	if err := state.confState.Unmarshal(image.ConfState); err != nil {
		return durableImageState{}, err
	}
	state.application, err = decodeApplicationImage(image.Application)
	if err != nil {
		return durableImageState{}, err
	}
	for _, encoded := range image.Entries {
		var entry pb.Entry
		if err := entry.Unmarshal(encoded); err != nil {
			return durableImageState{}, err
		}
		state.entries = append(state.entries, entry)
	}
	if state.applied != state.snapshot.Metadata.Index {
		return durableImageState{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_APPLIED_SNAPSHOT_MISMATCH")
	}
	if state.application.Applied != state.applied {
		return durableImageState{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_APPLICATION_APPLIED_MISMATCH")
	}
	snapshotConfState, err := state.snapshot.Metadata.ConfState.Marshal()
	if err != nil {
		return durableImageState{}, err
	}
	if state.applied > 0 && !bytes.Equal(snapshotConfState, image.ConfState) {
		return durableImageState{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_CONFSTATE_MISMATCH")
	}
	if state.applied > 0 && !bytes.Equal(state.snapshot.Data, image.Application) {
		return durableImageState{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_APPLICATION_SNAPSHOT_MISMATCH")
	}
	last := state.snapshot.Metadata.Index
	for _, entry := range state.entries {
		if entry.Index != last+1 {
			return durableImageState{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_ENTRIES_NONCONTIGUOUS")
		}
		last = entry.Index
	}
	if state.hardState.Commit > last {
		return durableImageState{}, fmt.Errorf("ETCDRAFT_V2_DURABLE_COMMIT_OUT_OF_RANGE")
	}
	return state, nil
}

func (image durableImage) restore() (*raft.MemoryStorage, uint64, pb.ConfState, applicationImage, error) {
	state, err := image.decode()
	if err != nil {
		return nil, 0, pb.ConfState{}, applicationImage{}, err
	}
	storage := raft.NewMemoryStorage()
	if !raft.IsEmptySnap(state.snapshot) {
		if err := storage.ApplySnapshot(cloneSnapshot(state.snapshot)); err != nil {
			return nil, 0, pb.ConfState{}, applicationImage{}, err
		}
	}
	if !raft.IsEmptyHardState(state.hardState) {
		if err := storage.SetHardState(state.hardState); err != nil {
			return nil, 0, pb.ConfState{}, applicationImage{}, err
		}
	}
	if len(state.entries) > 0 {
		if err := storage.Append(cloneEntries(state.entries)); err != nil {
			return nil, 0, pb.ConfState{}, applicationImage{}, err
		}
	}
	return storage, state.applied, cloneConfState(state.confState), state.application, nil
}

func (image durableImage) lastIndex() (uint64, error) {
	state, err := image.decode()
	if err != nil {
		return 0, err
	}
	last := state.snapshot.Metadata.Index
	if len(state.entries) > 0 {
		last = state.entries[len(state.entries)-1].Index
	}
	return last, nil
}

func persistReady(storage *raft.MemoryStorage, ready raft.Ready) error {
	if !raft.IsEmptySnap(ready.Snapshot) {
		if err := storage.ApplySnapshot(cloneSnapshot(ready.Snapshot)); err != nil {
			return err
		}
	}
	if !raft.IsEmptyHardState(ready.HardState) {
		if err := storage.SetHardState(ready.HardState); err != nil {
			return err
		}
	}
	if len(ready.Entries) > 0 {
		if err := storage.Append(cloneEntries(ready.Entries)); err != nil {
			return err
		}
	}
	return nil
}

func applyCommitted(
	raw *raft.RawNode,
	ready raft.Ready,
	applied *uint64,
	confState *pb.ConfState,
	application *applicationImage,
) ([]appliedCommand, error) {
	var commands []appliedCommand
	if !raft.IsEmptySnap(ready.Snapshot) && ready.Snapshot.Metadata.Index > *applied {
		if len(ready.Snapshot.Data) == 0 {
			return nil, fmt.Errorf("ETCDRAFT_V2_SNAPSHOT_APPLICATION_IMAGE_REQUIRED")
		}
		restored, err := decodeApplicationImage(ready.Snapshot.Data)
		if err != nil {
			return nil, err
		}
		if restored.Applied != ready.Snapshot.Metadata.Index {
			return nil, fmt.Errorf("ETCDRAFT_V2_SNAPSHOT_APPLICATION_APPLIED_MISMATCH")
		}
		*application = restored
		*applied = ready.Snapshot.Metadata.Index
		*confState = cloneConfState(ready.Snapshot.Metadata.ConfState)
	}
	for _, entry := range ready.CommittedEntries {
		if entry.Index <= *applied {
			continue
		}
		switch entry.Type {
		case pb.EntryNormal:
			updated, command, err := application.applyNormal(entry)
			if err != nil {
				return nil, err
			}
			*application = updated
			if command != nil {
				commands = append(commands, *command)
			}
		case pb.EntryConfChange:
			var change pb.ConfChange
			if err := change.Unmarshal(entry.Data); err != nil {
				return nil, fmt.Errorf("decode ConfChange at %d: %w", entry.Index, err)
			}
			*confState = cloneConfState(*raw.ApplyConfChange(change))
			updated, err := application.advance(entry.Index)
			if err != nil {
				return nil, err
			}
			*application = updated
		case pb.EntryConfChangeV2:
			var change pb.ConfChangeV2
			if err := change.Unmarshal(entry.Data); err != nil {
				return nil, fmt.Errorf("decode ConfChangeV2 at %d: %w", entry.Index, err)
			}
			*confState = cloneConfState(*raw.ApplyConfChange(change))
			updated, err := application.advance(entry.Index)
			if err != nil {
				return nil, err
			}
			*application = updated
		default:
			return nil, fmt.Errorf("unsupported committed entry type %s", entry.Type)
		}
		*applied = entry.Index
	}
	if application.Applied != *applied {
		return nil, fmt.Errorf("ETCDRAFT_V2_APPLICATION_APPLIED_MISMATCH")
	}
	return commands, nil
}

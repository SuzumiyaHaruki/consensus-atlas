package etcdraftv2

import (
	"testing"

	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
)

func TestDurableImageRestoresPublicStorageState(t *testing.T) {
	storage := raft.NewMemoryStorage()
	entries := []pb.Entry{
		{Index: 1, Term: 1, Type: pb.EntryConfChange, Data: []byte("configuration")},
		{Index: 2, Term: 2, Type: pb.EntryNormal, Data: []byte("uncommitted")},
	}
	if err := storage.Append(entries); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetHardState(pb.HardState{Term: 2, Vote: 1, Commit: 1}); err != nil {
		t.Fatal(err)
	}
	confState := pb.ConfState{Voters: []uint64{1, 2, 3}}
	application, err := newApplicationImage()
	if err != nil {
		t.Fatal(err)
	}
	application, err = application.advance(1)
	if err != nil {
		t.Fatal(err)
	}
	image, err := captureDurableImage(storage, 1, confState, application)
	if err != nil {
		t.Fatal(err)
	}
	restored, applied, restoredConfState, restoredApplication, err := image.restore()
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || len(restoredConfState.Voters) != 3 || restoredApplication.Applied != 1 {
		t.Fatalf("restored application state = applied %d, conf %+v", applied, restoredConfState)
	}
	hardState, initialConfState, err := restored.InitialState()
	if err != nil {
		t.Fatal(err)
	}
	if hardState.Term != 2 || hardState.Vote != 1 || hardState.Commit != 1 ||
		len(initialConfState.Voters) != 3 {
		t.Fatalf("restored initial state = hard %+v, conf %+v", hardState, initialConfState)
	}
	lastIndex, err := restored.LastIndex()
	if err != nil {
		t.Fatal(err)
	}
	if lastIndex != 2 {
		t.Fatalf("restored last index = %d, want 2", lastIndex)
	}
	restoredEntries, err := restored.Entries(2, 3, ^uint64(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(restoredEntries) != 1 || string(restoredEntries[0].Data) != "uncommitted" {
		t.Fatalf("restored post-snapshot entries = %+v", restoredEntries)
	}
}

func TestDurableImageRejectsMutation(t *testing.T) {
	application, err := newApplicationImage()
	if err != nil {
		t.Fatal(err)
	}
	image, err := captureDurableImage(raft.NewMemoryStorage(), 0, pb.ConfState{}, application)
	if err != nil {
		t.Fatal(err)
	}
	image.Applied++
	if _, _, _, _, err := image.restore(); err == nil {
		t.Fatal("mutated durable image was accepted")
	}
}

package etcdraftv2

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	pb "go.etcd.io/raft/v3/raftpb"
)

func TestApplicationImageAppliesAndRejectsMutation(t *testing.T) {
	image, err := newApplicationImage()
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Operation: OperationPropose, RequestID: "request-1", Value: []byte("alpha")}
	encoded, err := encodeProposal(input, control.NodeRef{Node: "n1", Incarnation: 1})
	if err != nil {
		t.Fatal(err)
	}
	image, command, err := image.applyNormal(pb.Entry{Index: 2, Term: 3, Type: pb.EntryNormal, Data: encoded})
	if err != nil {
		t.Fatal(err)
	}
	if image.Applied != 2 || len(image.Commands) != 1 || command == nil ||
		command.RequestID != "request-1" || string(command.Value) != "alpha" {
		t.Fatalf("application image = %+v, command = %+v", image, command)
	}
	roundTrip, err := image.encode()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := decodeApplicationImage(roundTrip)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Digest != image.Digest || len(restored.Commands) != 1 {
		t.Fatalf("restored application image = %+v", restored)
	}
	image.Commands[0].Value[0] ^= 1
	if err := image.validate(); err == nil {
		t.Fatal("mutated application image was accepted")
	}
}

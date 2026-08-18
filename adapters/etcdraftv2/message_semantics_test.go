package etcdraftv2

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	pb "go.etcd.io/raft/v3/raftpb"
)

func TestMessageCapabilityAndPayloadDescriptionStayBound(t *testing.T) {
	adapter, err := NewWithConfig(ThreeNodeConfig())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := adapter.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Capabilities.Message == nil ||
		len(manifest.Capabilities.Message.TypeHints) != len(messageTypeHints) ||
		len(manifest.Capabilities.Message.MetadataKeys) != len(messageMetadataKeys) {
		t.Fatalf("message capability is incomplete: %#v", manifest.Capabilities.Message)
	}

	message := pb.Message{Type: pb.MsgAppResp, From: 2, To: 1, Term: 4, Index: 9, Commit: 8}
	encoded, err := message.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := control.NewPayload(messageSchema, "protobuf", encoded)
	if err != nil {
		t.Fatal(err)
	}
	adapter.nodes = map[control.NodeID]*nodeState{
		"n1": {config: NodeConfig{Node: "n1", RaftID: 1}, incarnation: 1},
		"n2": {config: NodeConfig{Node: "n2", RaftID: 2}, incarnation: 1},
	}
	target := adapter.nodes["n1"]
	item := &control.ProducedItem{Message: &control.MessageEnvelope{
		Source: control.NodeRef{Node: "n2", Incarnation: 1}, Target: "n1",
		TypeHint: "MsgAppResp", Payload: payload,
		Metadata: map[string]string{"term": "4", "index": "9", "commit": "8"},
	}}
	if err := adapter.validateMessage(item, target); err != nil {
		t.Fatalf("valid message description rejected: %v", err)
	}
	item.Message.TypeHint = "MsgApp"
	if err := adapter.validateMessage(item, target); err == nil {
		t.Fatal("type label drift from protobuf payload was accepted")
	}
	item.Message.TypeHint = "MsgAppResp"
	item.Message.Metadata["index"] = "10"
	if err := adapter.validateMessage(item, target); err == nil {
		t.Fatal("metadata drift from protobuf payload was accepted")
	}
}

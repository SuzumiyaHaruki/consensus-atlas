package control_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestPartitionParametersAreCanonicalAndSelfValidating(t *testing.T) {
	left, err := control.NewPartitionParameters(
		[]control.NodeID{"n2", "n1", "n1"}, []control.NodeID{"n3"},
	)
	if err != nil {
		t.Fatal(err)
	}
	right, err := control.NewPartitionParameters(
		[]control.NodeID{"n3"}, []control.NodeID{"n1", "n2"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("canonical parameters differ: %+v != %+v", left, right)
	}
	encoded, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := control.DecodePartitionParameters(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, left) {
		t.Fatalf("decoded parameters = %+v, want %+v", decoded, left)
	}

	left.ID = "forged"
	forged, _ := json.Marshal(left)
	if _, err := control.DecodePartitionParameters(forged); err == nil {
		t.Fatal("forged partition ID was accepted")
	}
	if _, err := control.NewPartitionParameters([]control.NodeID{"n1"}, []control.NodeID{"n1"}); err == nil {
		t.Fatal("overlapping partition groups were accepted")
	}
}

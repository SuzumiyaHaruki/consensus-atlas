package controlentropy_test

import (
	"bytes"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
)

func TestSameSeedProducesSameTape(t *testing.T) {
	left := mustProvider(t, []byte("seed"))
	right := mustProvider(t, []byte("seed"))
	domain := nodeDomain("n1", 1)
	for range 5 {
		leftValue, err := left.Intn(domain, 10)
		if err != nil {
			t.Fatal(err)
		}
		rightValue, err := right.Intn(domain, 10)
		if err != nil {
			t.Fatal(err)
		}
		if leftValue != rightValue {
			t.Fatalf("draw differs: %d != %d", leftValue, rightValue)
		}
	}
	leftTape, err := left.Tape()
	if err != nil {
		t.Fatal(err)
	}
	rightTape, err := right.Tape()
	if err != nil {
		t.Fatal(err)
	}
	if leftTape.Digest != rightTape.Digest {
		t.Fatalf("tape digest differs: %s != %s", leftTape.Digest, rightTape.Digest)
	}
}

func TestNodeStreamsDoNotDependOnCrossNodeCallOrder(t *testing.T) {
	first := mustProvider(t, []byte("seed"))
	second := mustProvider(t, []byte("seed"))
	n1 := nodeDomain("n1", 1)
	n2 := nodeDomain("n2", 1)

	firstN1, err := first.Bytes(n1, 32)
	if err != nil {
		t.Fatal(err)
	}
	firstN2, err := first.Bytes(n2, 32)
	if err != nil {
		t.Fatal(err)
	}
	secondN2, err := second.Bytes(n2, 32)
	if err != nil {
		t.Fatal(err)
	}
	secondN1, err := second.Bytes(n1, 32)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstN1, secondN1) || !bytes.Equal(firstN2, secondN2) {
		t.Fatal("per-node draw changed with cross-node call order")
	}
	if bytes.Equal(firstN1, firstN2) {
		t.Fatal("different node domains produced identical 32-byte output")
	}
}

func TestIncarnationGetsIndependentStream(t *testing.T) {
	provider := mustProvider(t, []byte("seed"))
	first, err := provider.Bytes(nodeDomain("n1", 1), 32)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Bytes(nodeDomain("n1", 2), 32)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("different incarnations produced identical 32-byte output")
	}
}

func TestTapeRejectsOrdinalGap(t *testing.T) {
	provider := mustProvider(t, []byte("seed"))
	if _, err := provider.Intn(nodeDomain("n1", 1), 10); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Intn(nodeDomain("n1", 1), 10); err != nil {
		t.Fatal(err)
	}
	tape, err := provider.Tape()
	if err != nil {
		t.Fatal(err)
	}
	tape.Draws[1].Ordinal = 0
	tape.Draws[1].ID = tape.Draws[0].ID
	if _, err := tape.Seal(); err == nil {
		t.Fatal("Tape.Seal() accepted an ordinal gap")
	}
}

func mustProvider(t *testing.T, seed []byte) *controlentropy.Provider {
	t.Helper()
	provider, err := controlentropy.New(seed)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func nodeDomain(node string, incarnation uint64) controlentropy.Domain {
	return controlentropy.Domain{
		Namespace: "fixture/v1", Node: control.NodeID(node),
		Incarnation: incarnation, ID: "election-timeout",
	}
}

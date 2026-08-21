package etcdraftv2

import (
	cryptorand "crypto/rand"
	"math/big"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
)

func TestNativeEntropyScopeIsDeterministicAndRestoresReader(t *testing.T) {
	original := cryptorand.Reader
	left, err := newNativeEntropy([]byte("etcdraft-v2-seed"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := newNativeEntropy([]byte("etcdraft-v2-seed"))
	if err != nil {
		t.Fatal(err)
	}
	domain := controlentropy.Domain{
		Namespace: "etcdraft-v3.6.0", Node: control.NodeID("n1"),
		Incarnation: 1, ID: "native-election-timeout",
	}
	var leftValue, rightValue *big.Int
	if err := left.withDomain(domain, func() error {
		leftValue, err = cryptorand.Int(cryptorand.Reader, big.NewInt(10))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if cryptorand.Reader != original {
		t.Fatal("crypto/rand.Reader was not restored")
	}
	if err := right.withDomain(domain, func() error {
		rightValue, err = cryptorand.Int(cryptorand.Reader, big.NewInt(10))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if leftValue.Cmp(rightValue) != 0 {
		t.Fatalf("scoped values differ: %s != %s", leftValue, rightValue)
	}
	leftTape, err := left.provider.Tape()
	if err != nil {
		t.Fatal(err)
	}
	rightTape, err := right.provider.Tape()
	if err != nil {
		t.Fatal(err)
	}
	if leftTape.Digest != rightTape.Digest || len(leftTape.Draws) == 0 {
		t.Fatalf("scoped tapes differ or are empty: %s / %s", leftTape.Digest, rightTape.Digest)
	}
}

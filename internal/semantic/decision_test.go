package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestDecisionObservationsNormalizeWithoutProtocolKnowledge(t *testing.T) {
	value := sha256.Sum256([]byte("value"))
	digest := hex.EncodeToString(value[:])
	values := []DecisionObservation{
		{Step: 2, Participant: "n2", Position: "7", ValueDigest: digest},
		{Step: 1, Participant: "n1", Position: "7", ValueDigest: digest},
	}
	normalized, err := NormalizeDecisionObservations(values)
	if err != nil {
		t.Fatal(err)
	}
	if normalized[0].Step != 1 || normalized[1].Step != 2 {
		t.Fatalf("observations are not canonical: %#v", normalized)
	}
	if _, err := NormalizeDecisionObservations(append(values, values[0])); err == nil {
		t.Fatal("duplicate decision observation was accepted")
	}
}

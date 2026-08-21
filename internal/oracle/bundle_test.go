package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestBundleAgreementUsesExactProjectedPositionAndDigest(t *testing.T) {
	left := sha256.Sum256([]byte("left"))
	right := sha256.Sum256([]byte("right"))
	bundle := controlexperiment.ExecutionBundle{Decisions: controlexperiment.DecisionHistory{
		Observations: []semantic.DecisionObservation{
			{Step: 3, Participant: "n1", Position: "5", ValueDigest: hex.EncodeToString(left[:])},
			{Step: 8, Participant: "n3", Position: "5", ValueDigest: hex.EncodeToString(right[:])},
		},
	}}
	result := CheckBundle(bundle, BundleAgreement{})
	if len(result.Violations) != 1 || result.Violations[0].Monitor != "agreement" ||
		result.Violations[0].Step != 8 {
		t.Fatalf("agreement result = %#v", result)
	}
}

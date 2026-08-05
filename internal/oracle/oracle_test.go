package oracle_test

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

func TestAgreementDetectsConflictingCommit(t *testing.T) {
	trace := []core.TraceRecord{
		{Step: 1, Observations: []core.Observation{{Kind: "commit", Value: "a"}}},
		{Step: 2, Observations: []core.Observation{{Kind: "commit", Value: "b"}}},
	}
	violations := (oracle.Agreement{}).Check(trace)
	if len(violations) != 1 {
		t.Fatalf("violations = %#v, want one", violations)
	}
}

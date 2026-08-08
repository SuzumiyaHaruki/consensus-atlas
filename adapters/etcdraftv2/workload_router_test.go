package etcdraftv2

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestWorkloadRouterUsesNativeEvidenceWithoutCorePSS(t *testing.T) {
	if (WorkloadRouter{}).ID() != WorkloadRouterID {
		t.Fatal("workload router identity changed")
	}
	if _, err := (WorkloadRouter{}).Route("unknown-selector", control.EvidenceEnvelope{}); err == nil {
		t.Fatal("unsupported selector was accepted")
	}
	if WorkloadRouterID == CorePSSMappingID ||
		controlexperiment.TargetSingleCoordinatingMember == "" {
		t.Fatal("workload routing was not separated from Core PSS identity")
	}
}

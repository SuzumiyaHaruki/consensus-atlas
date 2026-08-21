package conformance

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

// CorePlan exercises Adapter invariants that require no protocol-specific
// input. ExpectedEntropyNodes binds the audited domains expected after Reset.
type CorePlan struct {
	Seed                 []byte
	ExpectedEntropyNodes []control.NodeID
}

func (plan CorePlan) Validate() error {
	if len(plan.Seed) == 0 {
		return errors.New("CONFORMANCE_CORE_SEED_REQUIRED")
	}
	if len(plan.ExpectedEntropyNodes) == 0 {
		return errors.New("CONFORMANCE_CORE_ENTROPY_NODES_REQUIRED")
	}
	seen := make(map[control.NodeID]struct{}, len(plan.ExpectedEntropyNodes))
	for _, node := range plan.ExpectedEntropyNodes {
		if node == "" {
			return errors.New("CONFORMANCE_CORE_ENTROPY_NODE_REQUIRED")
		}
		if _, ok := seen[node]; ok {
			return errors.New("CONFORMANCE_CORE_ENTROPY_NODE_DUPLICATE")
		}
		seen[node] = struct{}{}
	}
	return nil
}

// EvaluateCore validates stable yield/evidence, pure enabled checks, and
// deterministic audited entropy without knowing any adapted operation.
func EvaluateCore(ctx context.Context, factory Factory, plan CorePlan) (Report, error) {
	if err := plan.Validate(); err != nil {
		return Report{}, err
	}
	manifest, err := readFactoryManifest(ctx, factory)
	if err != nil {
		return Report{}, err
	}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		return Report{}, err
	}
	tests := []struct {
		id         string
		capability string
		run        func(context.Context, Factory, CorePlan) error
	}{
		{"collect-idempotent", "strict-yield", checkCollectIdempotent},
		{"enabled-check-pure", "pure-enabled-check", checkEnabledPure},
		{"entropy-audit-stable", "strict-entropy-replay", checkEntropyStable},
	}
	report := Report{SchemaVersion: ReportSchemaVersion, ManifestDigest: manifestDigest, Passed: true}
	for _, test := range tests {
		result := CaseResult{ID: test.id, Passed: true}
		if err := test.run(ctx, factory, plan); err != nil {
			result.Passed = false
			result.ReasonCode = stableReason(err)
			report.Passed = false
		} else {
			report.ValidatedCapabilities = append(report.ValidatedCapabilities, test.capability)
		}
		report.Cases = append(report.Cases, result)
	}
	return report.Seal()
}

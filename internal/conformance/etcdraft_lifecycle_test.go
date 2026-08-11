package conformance_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestEtcdRaftDeclaredNaturalLifecycleConformance(t *testing.T) {
	report, err := conformance.EvaluateNaturalLifecycle(
		context.Background(),
		func() control.Adapter {
			adapter, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
			if err != nil {
				t.Fatal(err)
			}
			return adapter
		},
		conformance.NaturalLifecyclePlan{
			Seed: []byte("etcdraft-v2-natural-lifecycle-conformance"), DecisionBound: 192,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Cases) != 2 || len(report.ValidatedCapabilities) != 2 {
		t.Fatalf("natural lifecycle conformance report = %+v", report)
	}
}

func TestEtcdRaftCoreConformanceNeedsNoProtocolInput(t *testing.T) {
	report, err := conformance.EvaluateCore(
		context.Background(),
		func() control.Adapter {
			adapter, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
			if err != nil {
				t.Fatal(err)
			}
			return adapter
		},
		conformance.CorePlan{
			Seed:                 []byte("etcdraft-v2-core-conformance"),
			ExpectedEntropyNodes: []control.NodeID{"n1", "n2", "n3"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Cases) != 3 || len(report.ValidatedCapabilities) != 3 {
		t.Fatalf("core conformance report = %+v", report)
	}
}

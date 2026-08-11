package conformance_test

import (
	"context"
	"testing"
	"time"

	etcdraftv2 "github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	hashicorpraftv2 "github.com/SuzumiyaHaruki/consensus-atlas/adapters/hashicorpraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestReleasedMessageLifecycleIntentRunsOnTwoOfficialImplementations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	plan := conformance.NaturalLifecyclePlan{Seed: []byte("portable-lifecycle-v2"), DecisionBound: 192}

	t.Run("etcd-raft", func(t *testing.T) {
		factory := func() control.Adapter {
			adapter, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
			if err != nil {
				t.Fatal(err)
			}
			return adapter
		}
		report, err := conformance.EvaluateReleasedMessageLifecycle(ctx, factory, plan)
		assertLifecycleReport(t, report, err)
	})

	t.Run("hashicorp-raft", func(t *testing.T) {
		var adapters []*hashicorpraftv2.Adapter
		factory := func() control.Adapter {
			adapter := hashicorpraftv2.NewAdapter()
			adapters = append(adapters, adapter)
			return adapter
		}
		defer func() {
			for _, adapter := range adapters {
				_ = adapter.Close()
			}
		}()
		report, err := conformance.EvaluateReleasedMessageLifecycle(ctx, factory, plan)
		assertLifecycleReport(t, report, err)
	})
}

func TestIndependentControlWitnessIsNotOmniPaxosSpecific(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	factory := func() control.Adapter {
		adapter, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
		if err != nil {
			t.Fatal(err)
		}
		return adapter
	}
	report, err := conformance.EvaluateIndependentControl(ctx, factory, conformance.NaturalLifecyclePlan{
		Seed: []byte("portable-independent-control-v3"), DecisionBound: 192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Cases) != 3 {
		t.Fatalf("independent control report=%+v", report)
	}
}

func assertLifecycleReport(t *testing.T, report conformance.Report, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Cases) != 1 || !report.Cases[0].Passed ||
		report.Cases[0].ID != "released-message-lifecycle" {
		t.Fatalf("lifecycle report failed: %+v", report)
	}
}

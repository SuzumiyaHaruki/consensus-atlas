package conformance_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type closeCountingAdapter struct {
	control.Adapter
	closed *int
}

func (adapter *closeCountingAdapter) Close() error {
	*adapter.closed = *adapter.closed + 1
	return nil
}

func TestCoreConformanceClosesEveryFactoryAdapter(t *testing.T) {
	opened := 0
	closed := 0
	factory := func() control.Adapter {
		opened++
		return &closeCountingAdapter{Adapter: fixture.New(), closed: &closed}
	}
	report, err := conformance.EvaluateCore(context.Background(), factory, conformance.CorePlan{
		Seed:                 []byte("a7h1-conformance-resource-lifecycle"),
		ExpectedEntropyNodes: []control.NodeID{"n1", "n2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("core conformance did not pass: %#v", report)
	}
	if opened == 0 || closed != opened {
		t.Fatalf("conformance adapters: opened=%d closed=%d", opened, closed)
	}
}

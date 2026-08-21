package conformance_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestQualificationWorkMeterCountsActualRuntimeControl(t *testing.T) {
	factory, meter := conformance.MeterFactory(func() control.Adapter { return fixture.New() })
	report, err := conformance.EvaluateCore(context.Background(), factory, corePlan())
	if err != nil {
		t.Fatal(err)
	}
	work := meter.Snapshot()
	if !report.Passed || work.Validate() != nil || work.SetupAttempts < len(report.Cases) ||
		work.RuntimeInitializations != work.SetupAttempts || work.WorkUnits < work.SetupAttempts ||
		work.SchedulerDecisions != work.WorkUnits-work.SetupAttempts {
		t.Fatalf("qualification work was not measured mechanically: %+v", work)
	}
}

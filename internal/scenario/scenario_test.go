package scenario_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
)

func TestRunWithCostChargesNonTraceStepsAndRuntimeEvents(t *testing.T) {
	execution := newEngine(t)
	cost, err := scenario.RunWithCost(context.Background(), execution, scenario.Spec{
		Version: 1,
		Name:    "costed",
		Steps: []scenario.Step{
			{Op: "inject", Kind: core.EventCampaign, Target: "n1"},
			{Op: "execute_next"},
			{Op: "advance", Ticks: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cost.Invocations != 1 || cost.Steps != 3 || cost.RuntimeEvents != 2 || cost.WorkUnits != 3 {
		t.Fatalf("unexpected scenario cost: %+v", cost)
	}
}

func TestRunWithCostRetainsFailedStepCost(t *testing.T) {
	execution := newEngine(t)
	cost, err := scenario.RunWithCost(context.Background(), execution, scenario.Spec{
		Version: 1,
		Name:    "failed",
		Steps: []scenario.Step{{
			Op: "execute_match", Match: &scenario.Selector{Kind: core.EventMessage},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "no event matches selector") {
		t.Fatalf("expected selector failure, got %v", err)
	}
	if cost.Invocations != 1 || cost.Steps != 1 || cost.RuntimeEvents != 0 || cost.WorkUnits != 1 {
		t.Fatalf("failed step was not charged: %+v", cost)
	}
}

func TestCaptureMessageBindsOneRuntimeOwnedEvent(t *testing.T) {
	execution := newEngine(t)
	execution.Schedule(core.Event{Kind: core.EventMessage, Source: "n1", Target: "n1", Message: &core.MessageEnvelope{TypeHint: "fixture"}})
	if _, err := scenario.RunWithCost(context.Background(), execution, scenario.Spec{
		Version: 1, Name: "captured-message", Steps: []scenario.Step{
			{Op: "capture_message", Ref: "old", Match: &scenario.Selector{Kind: core.EventMessage, TypeHint: "fixture"}},
			{Op: "execute_ref", Ref: "old"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if pending := execution.Pending(); len(pending) != 0 {
		t.Fatalf("captured message was not executed: %#v", pending)
	}
}

func TestCaptureMessageRejectsAmbiguousSelector(t *testing.T) {
	execution := newEngine(t)
	for range []int{0, 1} {
		execution.Schedule(core.Event{Kind: core.EventMessage, Source: "n1", Target: "n1"})
	}
	_, err := scenario.RunWithCost(context.Background(), execution, scenario.Spec{
		Version: 1, Name: "ambiguous-message", Steps: []scenario.Step{{
			Op: "capture_message", Ref: "old", Match: &scenario.Selector{Kind: core.EventMessage},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatalf("ambiguous capture error = %v", err)
	}
}

func TestCaptureMessageSkipsAlreadyBoundEvent(t *testing.T) {
	execution := newEngine(t)
	execution.Schedule(core.Event{Kind: core.EventMessage, Source: "n1", Target: "n1", Message: &core.MessageEnvelope{TypeHint: "old"}})
	execution.Schedule(core.Event{Kind: core.EventMessage, Source: "n1", Target: "n1", Message: &core.MessageEnvelope{TypeHint: "new"}})
	if _, err := scenario.RunWithCost(context.Background(), execution, scenario.Spec{
		Version: 1, Name: "two-captured-messages", Steps: []scenario.Step{
			{Op: "capture_message", Ref: "first", Match: &scenario.Selector{Kind: core.EventMessage, TypeHint: "old"}},
			{Op: "capture_message", Ref: "second", Match: &scenario.Selector{Kind: core.EventMessage}},
			{Op: "execute_ref", Ref: "first"},
			{Op: "execute_ref", Ref: "second"},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteOptionalAllowsOnlyAnAbsentChoice(t *testing.T) {
	execution := newEngine(t)
	cost, err := scenario.RunWithCost(context.Background(), execution, scenario.Spec{
		Version: 1, Name: "optional", Steps: []scenario.Step{{
			Op: "execute_optional", Match: &scenario.Selector{Kind: core.EventPersist},
		}},
	})
	if err != nil || cost.RuntimeEvents != 0 || cost.WorkUnits != 1 {
		t.Fatalf("optional absent step cost=%+v err=%v", cost, err)
	}
}

type fixtureAdapter struct{}

var _ adapter.Adapter = (*fixtureAdapter)(nil)

func newEngine(t *testing.T) *engine.Engine {
	t.Helper()
	execution, err := engine.New(&fixtureAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	return execution
}

func (*fixtureAdapter) Protocol() string                  { return "fixture" }
func (*fixtureAdapter) Nodes() []string                   { return []string{"n1"} }
func (*fixtureAdapter) Snapshot() any                     { return map[string]any{} }
func (*fixtureAdapter) CheckConformance() error           { return nil }
func (*fixtureAdapter) Enabled(core.Event) (bool, string) { return true, "" }
func (*fixtureAdapter) Apply(context.Context, core.Event) (core.ApplyResult, error) {
	return core.ApplyResult{Status: core.StatusApplied}, nil
}

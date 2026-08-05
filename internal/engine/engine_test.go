package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
)

type dependencyAdapter struct{}

func (dependencyAdapter) Protocol() string        { return "dependency-test" }
func (dependencyAdapter) Nodes() []string         { return []string{"n1"} }
func (dependencyAdapter) Snapshot() any           { return struct{}{} }
func (dependencyAdapter) CheckConformance() error { return nil }
func (dependencyAdapter) Apply(_ context.Context, event core.Event) (core.ApplyResult, error) {
	if event.Kind != core.EventCampaign {
		return core.ApplyResult{Status: core.StatusApplied}, nil
	}
	payload, _ := json.Marshal(map[string]string{"vote": "n1"})
	return core.ApplyResult{
		Status: core.StatusApplied,
		Effects: []core.Effect{
			{Key: "persist", Kind: core.EventPersist, Target: "n1", Payload: payload},
			{Key: "send", Kind: core.EventMessage, Source: "n1", Target: "n1", After: []string{"persist"}},
		},
	}, nil
}

func TestDependencyMustCompleteBeforeMessage(t *testing.T) {
	e := engine.New(dependencyAdapter{})
	e.Schedule(core.Event{Kind: core.EventCampaign, Target: "n1"})
	if _, err := e.ExecuteNext(context.Background()); err != nil {
		t.Fatal(err)
	}

	enabled := e.Enabled()
	if len(enabled) != 1 || enabled[0].Kind != core.EventPersist {
		t.Fatalf("enabled after campaign = %#v, want only persist", enabled)
	}
	if _, err := e.ExecuteNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	enabled = e.Enabled()
	if len(enabled) != 1 || enabled[0].Kind != core.EventMessage {
		t.Fatalf("enabled after persist = %#v, want message", enabled)
	}
}

func TestDroppedDependencyBlocksDependentEvent(t *testing.T) {
	e := engine.New(dependencyAdapter{})
	e.Schedule(core.Event{Kind: core.EventCampaign, Target: "n1"})
	if _, err := e.ExecuteNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	persist := e.Enabled()[0]
	if _, err := e.Drop(persist.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.Enabled(); len(got) != 0 {
		t.Fatalf("enabled = %#v, want no event after dropping dependency", got)
	}
}

package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
)

type dependencyAdapter struct{}

func (dependencyAdapter) Protocol() string                  { return "dependency-test" }
func (dependencyAdapter) Nodes() []string                   { return []string{"n1"} }
func (dependencyAdapter) Snapshot() any                     { return struct{}{} }
func (dependencyAdapter) CheckConformance() error           { return nil }
func (dependencyAdapter) Enabled(core.Event) (bool, string) { return true, "" }
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

func TestMessageDuplicateHasStableLineageAndPartitionBlocksDelivery(t *testing.T) {
	e := engine.New(dependencyAdapter{})
	originalID := e.Schedule(core.Event{
		Kind: core.EventMessage, Source: "n1", Target: "n2", Payload: json.RawMessage(`{"type":"vote"}`),
	})
	cloneID, err := e.Duplicate(originalID)
	if err != nil {
		t.Fatal(err)
	}
	pending := e.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending messages = %d, want 2", len(pending))
	}
	if pending[0].Message.LinkSequence != 1 || pending[1].Message.LinkSequence != 2 {
		t.Fatalf("link sequences = %d, %d, want 1, 2",
			pending[0].Message.LinkSequence, pending[1].Message.LinkSequence)
	}
	if pending[1].ID != cloneID || pending[1].Message.CloneOf != originalID {
		t.Fatalf("clone lineage = %#v, want clone_of %s", pending[1].Message, originalID)
	}
	if err := e.Partition([][]string{{"n1"}, {"n2"}}); err != nil {
		t.Fatal(err)
	}
	if enabled := e.Enabled(); len(enabled) != 0 {
		t.Fatalf("enabled across partition = %#v, want none", enabled)
	}
	e.Heal()
	if enabled := e.Enabled(); len(enabled) != 2 {
		t.Fatalf("enabled after heal = %d, want 2", len(enabled))
	}
	trace := e.Trace()
	wantKinds := []core.EventKind{core.EventDuplicate, core.EventPartition, core.EventHeal}
	if len(trace) != len(wantKinds) {
		t.Fatalf("control trace length = %d, want %d", len(trace), len(wantKinds))
	}
	for index, want := range wantKinds {
		if trace[index].Event.Kind != want {
			t.Fatalf("control trace[%d] = %s, want %s", index, trace[index].Event.Kind, want)
		}
	}
}

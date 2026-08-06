package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
)

type timerAdapter struct {
	timers []core.Timer
	seen   []core.Event
	rearm  bool
	keep   bool
}

func (*timerAdapter) Protocol() string        { return "timer-fixture" }
func (*timerAdapter) Nodes() []string         { return []string{"n1"} }
func (*timerAdapter) Snapshot() any           { return map[string]any{} }
func (*timerAdapter) CheckConformance() error { return nil }
func (*timerAdapter) Enabled(core.Event) (bool, string) {
	return true, ""
}

func (a *timerAdapter) Timers(uint64) ([]core.Timer, error) {
	result := make([]core.Timer, len(a.timers))
	copy(result, a.timers)
	return result, nil
}

func (a *timerAdapter) Apply(_ context.Context, event core.Event) (core.ApplyResult, error) {
	a.seen = append(a.seen, event)
	if event.TimerID != "" {
		if a.rearm {
			a.timers = []core.Timer{{ID: event.TimerID, Target: event.Target, Deadline: event.At + 2, Payload: event.Payload}}
		} else if !a.keep {
			a.timers = nil
		}
	}
	return core.ApplyResult{Status: core.StatusApplied}, nil
}

func TestVirtualTimerBecomesEnabledButDoesNotRunAutomatically(t *testing.T) {
	runtime := &timerAdapter{timers: []core.Timer{{
		ID: "election", Target: "n1", Deadline: 3, Payload: []byte(`{"round":1}`),
	}}}
	e, err := engine.New(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if pending := e.Pending(); len(pending) != 1 || pending[0].Kind != core.EventTimeout || pending[0].TimerID != "election" {
		t.Fatalf("initial timer queue = %#v", pending)
	}
	if err := e.Advance(2); err != nil {
		t.Fatal(err)
	}
	if got := e.Enabled(); len(got) != 0 {
		t.Fatalf("timer enabled before deadline: %#v", got)
	}
	if len(runtime.seen) != 0 {
		t.Fatalf("advance invoked adapter: %#v", runtime.seen)
	}
	if err := e.Advance(1); err != nil {
		t.Fatal(err)
	}
	enabled := e.Enabled()
	if len(enabled) != 1 || enabled[0].TimerID != "election" || enabled[0].At != 3 {
		t.Fatalf("due timer was not the sole scheduler choice: %#v", enabled)
	}
	if len(runtime.seen) != 0 {
		t.Fatalf("reaching deadline invoked adapter: %#v", runtime.seen)
	}
	if _, err := e.Execute(context.Background(), enabled[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(runtime.seen) != 1 || runtime.seen[0].TimerID != "election" {
		t.Fatalf("released timer was not delivered exactly once: %#v", runtime.seen)
	}
	if timers := e.Timers(); len(timers) != 0 {
		t.Fatalf("consumed timer was not removed: %#v", timers)
	}
	trace := e.Trace()
	if len(trace) != 3 || trace[0].Event.Kind != core.EventClockAdvance || trace[1].Event.Kind != core.EventClockAdvance {
		t.Fatalf("virtual-time trace = %#v", trace)
	}
	if trace[1].Before == nil || trace[1].After == nil {
		t.Fatalf("advance omitted replay snapshots: %#v", trace[1])
	}
}

func TestVirtualTimerRearmReplacesPendingEvent(t *testing.T) {
	runtime := &timerAdapter{timers: []core.Timer{{ID: "heartbeat", Target: "n1", Deadline: 1}}, rearm: true}
	e, err := engine.New(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Advance(1); err != nil {
		t.Fatal(err)
	}
	first := e.Enabled()
	if len(first) != 1 {
		t.Fatalf("first due timer = %#v", first)
	}
	if _, err := e.Execute(context.Background(), first[0].ID); err != nil {
		t.Fatal(err)
	}
	pending := e.Pending()
	if len(pending) != 1 || pending[0].TimerID != "heartbeat" || pending[0].At != 3 || pending[0].ID == first[0].ID {
		t.Fatalf("rearmed timer = %#v", pending)
	}
}

func TestVirtualTimerSourceCanCancelPendingTimer(t *testing.T) {
	runtime := &timerAdapter{timers: []core.Timer{{ID: "view", Target: "n1", Deadline: 5}}}
	e, err := engine.New(runtime)
	if err != nil {
		t.Fatal(err)
	}
	runtime.timers = nil
	if err := e.Advance(1); err != nil {
		t.Fatal(err)
	}
	if pending := e.Pending(); len(pending) != 0 {
		t.Fatalf("cancelled timer remains pending: %#v", pending)
	}
	trace := e.Trace()
	if len(trace) != 1 || !hasObservation(trace[0].Observations, "timer:cancelled") {
		t.Fatalf("timer cancellation was not auditable: %#v", trace)
	}
}

func TestVirtualTimerRejectsMalformedOrStaleDeclarations(t *testing.T) {
	for name, timers := range map[string][]core.Timer{
		"duplicate-id":    {{ID: "same", Target: "n1", Deadline: 1}, {ID: "same", Target: "n1", Deadline: 2}},
		"unknown-target":  {{ID: "wrong", Target: "n2", Deadline: 1}},
		"invalid-payload": {{ID: "json", Target: "n1", Deadline: 1, Payload: []byte(`{`)}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := engine.New(&timerAdapter{timers: timers})
			if err == nil {
				t.Fatal("expected timer declaration error")
			}
		})
	}

	runtime := &timerAdapter{timers: []core.Timer{{ID: "static", Target: "n1", Deadline: 0}}, keep: true}
	e, err := engine.New(runtime)
	if err != nil {
		t.Fatal(err)
	}
	due := e.Enabled()
	if len(due) != 1 {
		t.Fatalf("initial due timer = %#v", due)
	}
	if _, err := e.Drop(due[0].ID); err == nil || !strings.Contains(err.Error(), "cannot be dropped") {
		t.Fatalf("dropping declared timer error = %v", err)
	}
	if _, err := e.Execute(context.Background(), due[0].ID); err == nil || !strings.Contains(err.Error(), "remained declared") {
		t.Fatalf("unchanged consumed timer error = %v", err)
	}
}

func hasObservation(observations []core.Observation, label string) bool {
	for _, observation := range observations {
		if observation.Label == label {
			return true
		}
	}
	return false
}

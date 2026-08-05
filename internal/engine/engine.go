package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

var (
	ErrNoEnabledEvent = errors.New("no enabled event")
	ErrUnknownEvent   = errors.New("unknown pending event")
)

type scheduledEvent struct {
	event core.Event
	seq   uint64
}

type Engine struct {
	adapter   adapter.Adapter
	clock     uint64
	nextID    uint64
	nextSeq   uint64
	step      int
	pending   map[string]scheduledEvent
	succeeded map[string]bool
	trace     []core.TraceRecord
}

func New(a adapter.Adapter) *Engine {
	return &Engine{
		adapter:   a,
		pending:   make(map[string]scheduledEvent),
		succeeded: make(map[string]bool),
	}
}

func (e *Engine) Clock() uint64 { return e.clock }

func (e *Engine) Advance(ticks uint64) { e.clock += ticks }

func (e *Engine) Schedule(event core.Event) string {
	e.nextID++
	e.nextSeq++
	if event.ID == "" {
		event.ID = fmt.Sprintf("e%06d", e.nextID)
	}
	if event.At < e.clock {
		event.At = e.clock
	}
	e.pending[event.ID] = scheduledEvent{event: cloneEvent(event), seq: e.nextSeq}
	return event.ID
}

func (e *Engine) Pending() []core.Event {
	items := make([]scheduledEvent, 0, len(e.pending))
	for _, item := range e.pending {
		items = append(items, item)
	}
	sortScheduled(items)
	out := make([]core.Event, 0, len(items))
	for _, item := range items {
		out = append(out, cloneEvent(item.event))
	}
	return out
}

func (e *Engine) Enabled() []core.Event {
	items := make([]scheduledEvent, 0, len(e.pending))
	for _, item := range e.pending {
		if item.event.At <= e.clock && e.dependenciesSucceeded(item.event) {
			items = append(items, item)
		}
	}
	sortScheduled(items)
	out := make([]core.Event, 0, len(items))
	for _, item := range items {
		out = append(out, cloneEvent(item.event))
	}
	return out
}

func (e *Engine) ExecuteNext(ctx context.Context) (core.TraceRecord, error) {
	enabled := e.Enabled()
	if len(enabled) == 0 {
		return core.TraceRecord{}, ErrNoEnabledEvent
	}
	return e.Execute(ctx, enabled[0].ID)
}

func (e *Engine) Execute(ctx context.Context, id string) (core.TraceRecord, error) {
	item, ok := e.pending[id]
	if !ok {
		return core.TraceRecord{}, fmt.Errorf("%w: %s", ErrUnknownEvent, id)
	}
	if item.event.At > e.clock || !e.dependenciesSucceeded(item.event) {
		return core.TraceRecord{}, fmt.Errorf("event %s is not enabled", id)
	}

	before := e.adapter.Snapshot()
	result, applyErr := e.adapter.Apply(ctx, cloneEvent(item.event))
	after := e.adapter.Snapshot()
	delete(e.pending, id)

	status := result.Status
	if status == "" {
		status = core.StatusApplied
	}
	e.succeeded[id] = applyErr == nil && status == core.StatusApplied
	e.step++
	record := core.TraceRecord{
		Step:         e.step,
		LogicalTime:  e.clock,
		Event:        cloneEvent(item.event),
		Outcome:      string(status),
		Before:       before,
		After:        after,
		Observations: append([]core.Observation(nil), result.Observations...),
	}
	if applyErr != nil {
		record.Outcome = "error: " + applyErr.Error()
	}
	e.trace = append(e.trace, record)
	if applyErr != nil {
		return record, applyErr
	}

	e.scheduleEffects(item.event, result.Effects)
	return record, nil
}

func (e *Engine) Drop(id string) (core.TraceRecord, error) {
	item, ok := e.pending[id]
	if !ok {
		return core.TraceRecord{}, fmt.Errorf("%w: %s", ErrUnknownEvent, id)
	}
	delete(e.pending, id)
	e.succeeded[id] = false
	e.step++
	snapshot := e.adapter.Snapshot()
	record := core.TraceRecord{
		Step:        e.step,
		LogicalTime: e.clock,
		Event:       cloneEvent(item.event),
		Outcome:     "dropped",
		Before:      snapshot,
		After:       snapshot,
	}
	e.trace = append(e.trace, record)
	return record, nil
}

func (e *Engine) Run(ctx context.Context, limit int) error {
	for i := 0; i < limit; i++ {
		if len(e.Enabled()) == 0 {
			return nil
		}
		if _, err := e.ExecuteNext(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) Trace() []core.TraceRecord {
	return append([]core.TraceRecord(nil), e.trace...)
}

func (e *Engine) dependenciesSucceeded(event core.Event) bool {
	for _, id := range event.Dependencies {
		if !e.succeeded[id] {
			return false
		}
	}
	return true
}

func (e *Engine) scheduleEffects(parent core.Event, effects []core.Effect) {
	ids := make(map[string]string, len(effects))
	for i, effect := range effects {
		id := e.Schedule(core.Event{
			Kind:    effect.Kind,
			Source:  effect.Source,
			Target:  effect.Target,
			At:      e.clock + effect.Delay,
			Payload: append([]byte(nil), effect.Payload...),
		})
		key := effect.Key
		if key == "" {
			key = fmt.Sprintf("effect-%d", i)
		}
		ids[key] = id
	}

	// Add causal dependencies only after every local effect key has an ID.
	for i, effect := range effects {
		key := effect.Key
		if key == "" {
			key = fmt.Sprintf("effect-%d", i)
		}
		id := ids[key]
		item := e.pending[id]
		deps := []string{parent.ID}
		for _, after := range effect.After {
			dependencyID, ok := ids[after]
			if !ok {
				// An unknown local dependency can never become enabled. Keeping the
				// literal value makes the malformed effect visible in Pending().
				dependencyID = "unknown:" + after
			}
			deps = append(deps, dependencyID)
		}
		item.event.Dependencies = deps
		e.pending[id] = item
	}
}

func sortScheduled(items []scheduledEvent) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].event.At != items[j].event.At {
			return items[i].event.At < items[j].event.At
		}
		return items[i].seq < items[j].seq
	})
}

func cloneEvent(event core.Event) core.Event {
	event.Dependencies = append([]string(nil), event.Dependencies...)
	event.Payload = append([]byte(nil), event.Payload...)
	return event
}

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

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
	linkSeq   map[string]uint64
	partition map[string]int
}

func New(a adapter.Adapter) *Engine {
	return &Engine{
		adapter:   a,
		pending:   make(map[string]scheduledEvent),
		succeeded: make(map[string]bool),
		linkSeq:   make(map[string]uint64),
		partition: make(map[string]int),
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
	if event.Kind == core.EventMessage {
		e.prepareMessage(&event)
	}
	e.pending[event.ID] = scheduledEvent{event: cloneEvent(event), seq: e.nextSeq}
	return event.ID
}

// Duplicate creates a distinct delivery with an explicit lineage and a new
// per-link sequence number. It never aliases the original payload or metadata.
func (e *Engine) Duplicate(id string) (string, error) {
	item, ok := e.pending[id]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownEvent, id)
	}
	if item.event.Kind != core.EventMessage || item.event.Message == nil {
		return "", fmt.Errorf("event %s is not a message", id)
	}
	clone := cloneEvent(item.event)
	clone.ID = ""
	clone.Message.CloneOf = id
	clone.Message.LinkSequence = 0
	cloneID := e.Schedule(clone)
	e.recordControl(core.Event{
		Kind: core.EventDuplicate, Source: item.event.Source, Target: item.event.Target,
		Message: cloneMessage(item.event.Message),
	}, []core.Observation{{
		Kind: "transport", Label: "message:duplicated", Node: item.event.Source,
		Evidence: map[string]string{"original": id, "clone": cloneID, "type": item.event.Message.TypeHint},
	}}, nil, nil)
	return cloneID, nil
}

// Partition blocks transport between distinct groups. Nodes omitted from all
// groups remain connected. This is scheduler state, not adapter state.
func (e *Engine) Partition(groups [][]string) error {
	before := e.controlSnapshot()
	next := make(map[string]int)
	for groupIndex, group := range groups {
		for _, node := range group {
			if node == "" {
				return errors.New("partition contains an empty node")
			}
			if _, exists := next[node]; exists {
				return fmt.Errorf("node %s appears in multiple partition groups", node)
			}
			next[node] = groupIndex + 1
		}
	}
	if len(next) == 0 {
		return errors.New("partition requires at least one node")
	}
	e.partition = next
	payload, err := json.Marshal(map[string]any{"groups": groups})
	if err != nil {
		return err
	}
	e.recordControl(core.Event{Kind: core.EventPartition, Payload: payload}, []core.Observation{{
		Kind: "transport", Label: "network:partition",
	}}, before, e.controlSnapshot())
	return nil
}

func (e *Engine) Heal() {
	before := e.controlSnapshot()
	e.partition = make(map[string]int)
	e.recordControl(core.Event{Kind: core.EventHeal}, []core.Observation{{
		Kind: "transport", Label: "network:heal",
	}}, before, e.controlSnapshot())
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
		enabled, _ := e.adapter.Enabled(item.event)
		if item.event.At <= e.clock && e.dependenciesSucceeded(item.event) &&
			e.transportAllowed(item.event) && enabled {
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
	enabled, reason := e.adapter.Enabled(item.event)
	if item.event.At > e.clock || !e.dependenciesSucceeded(item.event) ||
		!e.transportAllowed(item.event) || !enabled {
		if reason != "" {
			return core.TraceRecord{}, fmt.Errorf("event %s is not enabled: %s", id, reason)
		}
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

	record.Cancelled = e.cancelGroups(result.CancelGroups)
	e.trace[len(e.trace)-1] = record
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
	if item.event.Kind == core.EventMessage && item.event.Message != nil {
		record.Observations = []core.Observation{{
			Kind: "transport", Label: "message:dropped", Node: item.event.Source,
			Value:    item.event.Message.PayloadDigest,
			Evidence: map[string]string{"to": item.event.Target, "type": item.event.Message.TypeHint},
		}}
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

// Snapshot exposes read-only evidence at an explicit measurement boundary.
// Adapters must return detached snapshot values as required by their contract.
func (e *Engine) Snapshot() any { return e.adapter.Snapshot() }

func (e *Engine) CheckConformance() error { return e.adapter.CheckConformance() }

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
			Group:   effect.Group,
			Payload: append([]byte(nil), effect.Payload...),
			Message: cloneMessage(effect.Message),
		})
		item := e.pending[id]
		if item.event.Message != nil && item.event.Message.CausationID == "" {
			item.event.Message.CausationID = parent.ID
			e.pending[id] = item
		}
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

func (e *Engine) prepareMessage(event *core.Event) {
	if event.Message == nil {
		event.Message = &core.MessageEnvelope{Payload: append([]byte(nil), event.Payload...)}
	}
	event.Message.From = event.Source
	event.Message.To = event.Target
	link := event.Source + "\x00" + event.Target + "\x00" + strconv.FormatUint(event.Message.SenderEpoch, 10)
	e.linkSeq[link]++
	event.Message.LinkSequence = e.linkSeq[link]
	if len(event.Message.Payload) == 0 && len(event.Payload) > 0 {
		event.Message.Payload = append([]byte(nil), event.Payload...)
	}
	if event.Message.PayloadDigest == "" {
		sum := sha256.Sum256(event.Message.Payload)
		event.Message.PayloadDigest = hex.EncodeToString(sum[:])
	}
}

func (e *Engine) transportAllowed(event core.Event) bool {
	if event.Kind != core.EventMessage {
		return true
	}
	from, fromPartitioned := e.partition[event.Source]
	to, toPartitioned := e.partition[event.Target]
	return !fromPartitioned || !toPartitioned || from == to
}

func (e *Engine) cancelGroups(groups []string) []string {
	if len(groups) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(groups))
	for _, group := range groups {
		if group != "" {
			wanted[group] = true
		}
	}
	var cancelled []string
	for id, item := range e.pending {
		if wanted[item.event.Group] {
			delete(e.pending, id)
			e.succeeded[id] = false
			cancelled = append(cancelled, id)
		}
	}
	sort.Strings(cancelled)
	return cancelled
}

func (e *Engine) recordControl(event core.Event, observations []core.Observation, before, after any) {
	e.nextID++
	e.nextSeq++
	event.ID = fmt.Sprintf("e%06d", e.nextID)
	event.At = e.clock
	e.step++
	e.succeeded[event.ID] = true
	e.trace = append(e.trace, core.TraceRecord{
		Step: e.step, LogicalTime: e.clock, Event: cloneEvent(event), Outcome: string(core.StatusApplied),
		Before: before, After: after, Observations: append([]core.Observation(nil), observations...),
	})
}

func (e *Engine) networkSnapshot() any {
	groups := make(map[string]int, len(e.partition))
	for node, group := range e.partition {
		groups[node] = group
	}
	return map[string]any{"partition_groups": groups}
}

func (e *Engine) controlSnapshot() any {
	return map[string]any{
		"system":  e.adapter.Snapshot(),
		"network": e.networkSnapshot(),
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
	event.Message = cloneMessage(event.Message)
	return event
}

func cloneMessage(message *core.MessageEnvelope) *core.MessageEnvelope {
	if message == nil {
		return nil
	}
	cloned := *message
	cloned.Payload = append([]byte(nil), message.Payload...)
	if message.Metadata != nil {
		cloned.Metadata = make(map[string]string, len(message.Metadata))
		for key, value := range message.Metadata {
			cloned.Metadata[key] = value
		}
	}
	return &cloned
}

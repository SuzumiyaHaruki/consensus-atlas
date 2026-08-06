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

type timerBinding struct {
	timer   core.Timer
	eventID string
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
	timers    map[string]timerBinding
}

func New(a adapter.Adapter) (*Engine, error) {
	if a == nil {
		return nil, errors.New("engine adapter is required")
	}
	e := &Engine{
		adapter:   a,
		pending:   make(map[string]scheduledEvent),
		succeeded: make(map[string]bool),
		linkSeq:   make(map[string]uint64),
		partition: make(map[string]int),
		timers:    make(map[string]timerBinding),
	}
	if _, err := e.syncTimers(); err != nil {
		return nil, fmt.Errorf("initialize virtual timers: %w", err)
	}
	return e, nil
}

func (e *Engine) Clock() uint64 { return e.clock }

// Advance moves the Engine's logical clock and records the boundary in the
// trace. It only makes due timers eligible for the normal scheduler; it never
// invokes them by itself.
func (e *Engine) Advance(ticks uint64) error {
	if ^uint64(0)-e.clock < ticks {
		return errors.New("logical clock overflow")
	}
	before := e.timeSnapshot()
	from := e.clock
	e.clock += ticks
	timerObservations, err := e.syncTimers()
	if err != nil {
		e.clock = from
		return fmt.Errorf("advance virtual time: %w", err)
	}
	observations := []core.Observation{{
		Kind: "time", Label: "clock:advanced",
		Evidence: map[string]string{
			"from":  strconv.FormatUint(from, 10),
			"to":    strconv.FormatUint(e.clock, 10),
			"ticks": strconv.FormatUint(ticks, 10),
		},
	}}
	observations = append(observations, timerObservations...)
	e.recordControl(core.Event{Kind: core.EventClockAdvance}, observations, before, e.timeSnapshot())
	return nil
}

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
	e.scheduleEffects(item.event, result.Effects)
	timerObservations, timerErr := e.syncTimers()
	if timerErr != nil {
		e.succeeded[id] = false
		record.Outcome = "error: timer reconciliation: " + timerErr.Error()
		e.trace[len(e.trace)-1] = record
		return record, fmt.Errorf("event %s timer reconciliation: %w", id, timerErr)
	}
	record.Observations = append(record.Observations, timerObservations...)
	e.trace[len(e.trace)-1] = record
	return record, nil
}

func (e *Engine) Drop(id string) (core.TraceRecord, error) {
	item, ok := e.pending[id]
	if !ok {
		return core.TraceRecord{}, fmt.Errorf("%w: %s", ErrUnknownEvent, id)
	}
	if item.event.TimerID != "" {
		return core.TraceRecord{}, fmt.Errorf("declared timer event %s cannot be dropped", id)
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

// Timers returns a detached, ID-sorted view of the Engine-owned timer queue.
// It is evidence for replay and diagnostics, not a protocol control API.
func (e *Engine) Timers() []core.Timer {
	ids := make([]string, 0, len(e.timers))
	for id := range e.timers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]core.Timer, 0, len(ids))
	for _, id := range ids {
		result = append(result, cloneTimer(e.timers[id].timer))
	}
	return result
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

// syncTimers reconciles a complete, side-effect-free timer declaration with
// Engine-owned pending events. It validates the entire next declaration before
// mutating the queue, so an invalid declaration never leaves a partial update.
func (e *Engine) syncTimers() ([]core.Observation, error) {
	source, ok := e.adapter.(adapter.TimerSource)
	if !ok {
		return nil, nil
	}
	declared, err := source.Timers(e.clock)
	if err != nil {
		return nil, fmt.Errorf("timer source: %w", err)
	}
	nodes := make(map[string]bool, len(e.adapter.Nodes()))
	for _, node := range e.adapter.Nodes() {
		nodes[node] = true
	}
	wanted := make(map[string]core.Timer, len(declared))
	for _, raw := range declared {
		timer, err := raw.Normalize()
		if err != nil {
			return nil, err
		}
		if timer.Deadline < e.clock {
			return nil, fmt.Errorf("timer %q deadline %d is before logical time %d", timer.ID, timer.Deadline, e.clock)
		}
		if !nodes[timer.Target] {
			return nil, fmt.Errorf("timer %q targets unknown node %q", timer.ID, timer.Target)
		}
		if _, exists := wanted[timer.ID]; exists {
			return nil, fmt.Errorf("duplicate timer id %q", timer.ID)
		}
		wanted[timer.ID] = timer
	}

	var removeIDs, replaceIDs, addIDs []string
	for id, binding := range e.timers {
		timer, exists := wanted[id]
		if !exists {
			removeIDs = append(removeIDs, id)
			continue
		}
		if timersEqual(binding.timer, timer) {
			if _, pending := e.pending[binding.eventID]; !pending {
				return nil, fmt.Errorf("timer %q remained declared after timeout event %s was consumed", id, binding.eventID)
			}
			continue
		}
		replaceIDs = append(replaceIDs, id)
	}
	for id := range wanted {
		if _, exists := e.timers[id]; !exists {
			addIDs = append(addIDs, id)
		}
	}
	sort.Strings(removeIDs)
	sort.Strings(replaceIDs)
	sort.Strings(addIDs)

	observations := make([]core.Observation, 0, len(removeIDs)+len(replaceIDs)+len(addIDs))
	for _, id := range removeIDs {
		binding := e.timers[id]
		_, pending := e.pending[binding.eventID]
		if pending {
			delete(e.pending, binding.eventID)
			e.succeeded[binding.eventID] = false
		}
		delete(e.timers, id)
		action := "completed"
		if pending {
			action = "cancelled"
		}
		observations = append(observations, timerObservation(action, binding.timer))
	}
	for _, id := range replaceIDs {
		binding := e.timers[id]
		if _, pending := e.pending[binding.eventID]; pending {
			delete(e.pending, binding.eventID)
			e.succeeded[binding.eventID] = false
		}
		timer := wanted[id]
		e.timers[id] = timerBinding{timer: cloneTimer(timer), eventID: e.scheduleTimer(timer)}
		observations = append(observations, timerObservation("rearmed", timer))
	}
	for _, id := range addIDs {
		timer := wanted[id]
		e.timers[id] = timerBinding{timer: cloneTimer(timer), eventID: e.scheduleTimer(timer)}
		observations = append(observations, timerObservation("scheduled", timer))
	}
	return observations, nil
}

func (e *Engine) scheduleTimer(timer core.Timer) string {
	return e.Schedule(core.Event{
		Kind: core.EventTimeout, Target: timer.Target, At: timer.Deadline,
		Payload: append([]byte(nil), timer.Payload...), TimerID: timer.ID,
	})
}

func timersEqual(left, right core.Timer) bool {
	return left.ID == right.ID && left.Target == right.Target &&
		left.Deadline == right.Deadline && string(left.Payload) == string(right.Payload)
}

func timerObservation(action string, timer core.Timer) core.Observation {
	return core.Observation{
		Kind: "time", Label: "timer:" + action, Node: timer.Target,
		Value:    strconv.FormatUint(timer.Deadline, 10),
		Evidence: map[string]string{"id": timer.ID, "deadline": strconv.FormatUint(timer.Deadline, 10)},
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

// timeSnapshot is deliberately separate from controlSnapshot. Existing
// transport controls retain their frozen snapshot shape; only an explicit
// clock-advance record carries virtual-time queue evidence.
func (e *Engine) timeSnapshot() any {
	return map[string]any{
		"logical_time": e.clock,
		"system":       e.adapter.Snapshot(),
		"network":      e.networkSnapshot(),
		"timers":       e.Timers(),
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

func cloneTimer(timer core.Timer) core.Timer {
	timer.Payload = append([]byte(nil), timer.Payload...)
	return timer
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

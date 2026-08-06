package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
)

type Spec struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	Steps   []Step `json:"steps"`
}

type Step struct {
	Op        string          `json:"op"`
	Kind      core.EventKind  `json:"kind,omitempty"`
	Operation string          `json:"operation,omitempty"`
	Source    string          `json:"source,omitempty"`
	Target    string          `json:"target,omitempty"`
	At        uint64          `json:"at,omitempty"`
	Ticks     uint64          `json:"ticks,omitempty"`
	Count     int             `json:"count,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Match     *Selector       `json:"match,omitempty"`
	Groups    [][]string      `json:"groups,omitempty"`
	Ref       string          `json:"ref,omitempty"`
}

type Selector struct {
	ID       string         `json:"id,omitempty"`
	Kind     core.EventKind `json:"kind,omitempty"`
	Operation string         `json:"operation,omitempty"`
	Source   string         `json:"source,omitempty"`
	Target   string         `json:"target,omitempty"`
	Group    string         `json:"group,omitempty"`
	TypeHint string         `json:"type_hint,omitempty"`
}

// Cost records trusted Runtime work performed while applying a scenario. A
// step costs at least one work unit even when it only schedules an input,
// advances logical time, or fails before producing a trace record. Operations
// such as run that execute several Runtime events cost one unit per event.
type Cost struct {
	Invocations   int `json:"invocations"`
	Steps         int `json:"steps"`
	RuntimeEvents int `json:"runtime_events"`
	WorkUnits     int `json:"work_units"`
}

func (s Spec) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported scenario version %d", s.Version)
	}
	if s.Name == "" {
		return errors.New("scenario name is required")
	}
	if len(s.Steps) == 0 {
		return errors.New("scenario must contain at least one step")
	}
	return nil
}

func Run(ctx context.Context, e *engine.Engine, spec Spec) error {
	_, err := RunWithCost(ctx, e, spec)
	return err
}

// RunWithCost executes spec and returns cost even when a step fails. Callers
// must retain the returned cost on error so failed setup/prepare attempts are
// not treated as free work.
func RunWithCost(ctx context.Context, e *engine.Engine, spec Spec) (Cost, error) {
	cost := Cost{Invocations: 1}
	if err := spec.Validate(); err != nil {
		return cost, err
	}
	refs := make(map[string]string)
	for index, step := range spec.Steps {
		cost.Steps++
		before := len(e.Trace())
		if err := runStep(ctx, e, step, refs); err != nil {
			chargeStep(&cost, len(e.Trace())-before)
			return cost, fmt.Errorf("scenario step %d (%s): %w", index+1, step.Op, err)
		}
		chargeStep(&cost, len(e.Trace())-before)
	}
	return cost, nil
}

func chargeStep(cost *Cost, runtimeEvents int) {
	if runtimeEvents < 0 {
		runtimeEvents = 0
	}
	cost.RuntimeEvents += runtimeEvents
	if runtimeEvents == 0 {
		cost.WorkUnits++
		return
	}
	cost.WorkUnits += runtimeEvents
}

func runStep(ctx context.Context, e *engine.Engine, step Step, refs map[string]string) error {
	switch step.Op {
	case "inject":
		if step.Kind == "" {
			return errors.New("inject requires kind")
		}
		e.Schedule(core.Event{
			Kind: step.Kind, Source: step.Source, Target: step.Target, At: step.At,
			Operation: step.Operation,
			Payload:   append([]byte(nil), step.Payload...),
		})
		return nil
	case "execute_next":
		_, err := e.ExecuteNext(ctx)
		return err
	case "execute_match":
		event, err := find(e.Enabled(), step.Match)
		if err != nil {
			return err
		}
		_, err = e.Execute(ctx, event.ID)
		return err
	case "execute_optional":
		event, found, err := findAtMostOne(e.Enabled(), step.Match)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		_, err = e.Execute(ctx, event.ID)
		return err
	case "drop_match":
		event, err := find(e.Pending(), step.Match)
		if err != nil {
			return err
		}
		_, err = e.Drop(event.ID)
		return err
	case "duplicate_match":
		event, err := find(e.Pending(), step.Match)
		if err != nil {
			return err
		}
		_, err = e.Duplicate(event.ID)
		return err
	case "capture_message":
		if step.Ref == "" {
			return errors.New("capture_message requires a ref")
		}
		if _, exists := refs[step.Ref]; exists {
			return fmt.Errorf("reference %q is already captured", step.Ref)
		}
		event, err := findExactlyOneUnbound(e.Pending(), step.Match, refs)
		if err != nil {
			return err
		}
		if event.Kind != core.EventMessage {
			return errors.New("capture_message may capture only a message")
		}
		refs[step.Ref] = event.ID
		return nil
	case "execute_ref":
		id, exists := refs[step.Ref]
		if !exists {
			return fmt.Errorf("reference %q is not available", step.Ref)
		}
		if _, err := e.Execute(ctx, id); err != nil {
			return err
		}
		delete(refs, step.Ref)
		return nil
	case "partition":
		return e.Partition(step.Groups)
	case "heal":
		e.Heal()
		return nil
	case "advance":
		return e.Advance(step.Ticks)
	case "run":
		count := step.Count
		if count <= 0 {
			count = 100
		}
		return e.Run(ctx, count)
	default:
		return fmt.Errorf("unsupported operation %q", step.Op)
	}
}

func find(events []core.Event, selector *Selector) (core.Event, error) {
	if selector == nil {
		return core.Event{}, errors.New("selector is required")
	}
	for _, event := range events {
		if matches(event, selector) {
			return event, nil
		}
	}
	return core.Event{}, errors.New("no event matches selector")
}

func findExactlyOneUnbound(events []core.Event, selector *Selector, refs map[string]string) (core.Event, error) {
	bound := make(map[string]bool, len(refs))
	for _, id := range refs {
		bound[id] = true
	}
	var found *core.Event
	for index := range events {
		event := events[index]
		if bound[event.ID] || !matches(event, selector) {
			continue
		}
		if found != nil {
			return core.Event{}, errors.New("selector matches more than one unbound event")
		}
		found = &event
	}
	if found == nil {
		return core.Event{}, errors.New("no unbound event matches selector")
	}
	return *found, nil
}

func findAtMostOne(events []core.Event, selector *Selector) (core.Event, bool, error) {
	var found *core.Event
	for index := range events {
		event := events[index]
		if !matches(event, selector) {
			continue
		}
		if found != nil {
			return core.Event{}, false, errors.New("selector matches more than one event")
		}
		found = &event
	}
	if found == nil {
		return core.Event{}, false, nil
	}
	return *found, true, nil
}

func matches(event core.Event, selector *Selector) bool {
	if selector == nil {
		return false
	}
	if selector.ID != "" && event.ID != selector.ID {
		return false
	}
	if selector.Kind != "" && event.Kind != selector.Kind {
		return false
	}
	if selector.Operation != "" && event.Operation != selector.Operation {
		return false
	}
	if selector.Source != "" && event.Source != selector.Source {
		return false
	}
	if selector.Target != "" && event.Target != selector.Target {
		return false
	}
	if selector.Group != "" && event.Group != selector.Group {
		return false
	}
	return selector.TypeHint == "" || event.Message != nil && event.Message.TypeHint == selector.TypeHint
}

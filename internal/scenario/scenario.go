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
	Op      string          `json:"op"`
	Kind    core.EventKind  `json:"kind,omitempty"`
	Source  string          `json:"source,omitempty"`
	Target  string          `json:"target,omitempty"`
	At      uint64          `json:"at,omitempty"`
	Ticks   uint64          `json:"ticks,omitempty"`
	Count   int             `json:"count,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Match   *Selector       `json:"match,omitempty"`
	Groups  [][]string      `json:"groups,omitempty"`
}

type Selector struct {
	ID       string         `json:"id,omitempty"`
	Kind     core.EventKind `json:"kind,omitempty"`
	Source   string         `json:"source,omitempty"`
	Target   string         `json:"target,omitempty"`
	Group    string         `json:"group,omitempty"`
	TypeHint string         `json:"type_hint,omitempty"`
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
	if err := spec.Validate(); err != nil {
		return err
	}
	for index, step := range spec.Steps {
		if err := runStep(ctx, e, step); err != nil {
			return fmt.Errorf("scenario step %d (%s): %w", index+1, step.Op, err)
		}
	}
	return nil
}

func runStep(ctx context.Context, e *engine.Engine, step Step) error {
	switch step.Op {
	case "inject":
		if step.Kind == "" {
			return errors.New("inject requires kind")
		}
		e.Schedule(core.Event{
			Kind: step.Kind, Source: step.Source, Target: step.Target, At: step.At,
			Payload: append([]byte(nil), step.Payload...),
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
	case "partition":
		return e.Partition(step.Groups)
	case "heal":
		e.Heal()
		return nil
	case "advance":
		e.Advance(step.Ticks)
		return nil
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
		if selector.ID != "" && event.ID != selector.ID {
			continue
		}
		if selector.Kind != "" && event.Kind != selector.Kind {
			continue
		}
		if selector.Source != "" && event.Source != selector.Source {
			continue
		}
		if selector.Target != "" && event.Target != selector.Target {
			continue
		}
		if selector.Group != "" && event.Group != selector.Group {
			continue
		}
		if selector.TypeHint != "" && (event.Message == nil || event.Message.TypeHint != selector.TypeHint) {
			continue
		}
		return event, nil
	}
	return core.Event{}, errors.New("no event matches selector")
}

package host

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
)

// Adapter is the protocol-neutral host runtime between the deterministic
// engine and a thin native driver. It owns batch scheduling and message
// release; the driver owns only native API calls and opaque host operations.
type Adapter struct {
	driver driver.ProtocolDriver
	order  []string
	nodes  map[string]*nodeState
}

type nodeState struct {
	Running bool
	Batch   *driver.OutputBatch
}

type operationRef struct {
	Batch     string `json:"batch"`
	Operation string `json:"operation"`
}

func New(protocolDriver driver.ProtocolDriver) (*Adapter, error) {
	if err := protocolDriver.CheckConformance(); err != nil {
		return nil, err
	}
	order := append([]string(nil), protocolDriver.Nodes()...)
	sort.Strings(order)
	nodes := make(map[string]*nodeState, len(order))
	for _, id := range order {
		if id == "" {
			return nil, fmt.Errorf("driver returned an empty node ID")
		}
		if _, exists := nodes[id]; exists {
			return nil, fmt.Errorf("driver returned duplicate node %q", id)
		}
		nodes[id] = &nodeState{Running: true}
	}
	return &Adapter{driver: protocolDriver, order: order, nodes: nodes}, nil
}

func (a *Adapter) Protocol() string { return a.driver.Protocol() }

func (a *Adapter) Nodes() []string { return append([]string(nil), a.order...) }

func (a *Adapter) Capabilities() driver.Manifest { return a.driver.Capabilities() }

// Timers forwards the optional declaration interface without making it a
// requirement for every native driver. The host remains a passive bridge: the
// Engine, not this adapter or the driver, owns pending timer delivery.
func (a *Adapter) Timers(now uint64) ([]core.Timer, error) {
	source, ok := a.driver.(driver.TimerSource)
	if !ok {
		return nil, nil
	}
	timers, err := source.Timers(now)
	if err != nil {
		return nil, err
	}
	result := make([]core.Timer, len(timers))
	for index, timer := range timers {
		result[index] = timer
		result[index].Payload = append([]byte(nil), timer.Payload...)
	}
	return result, nil
}

func (a *Adapter) Enabled(event core.Event) (bool, string) {
	state, ok := a.nodes[event.Target]
	if !ok {
		return false, "unknown target node"
	}
	switch event.Kind {
	case core.EventStart:
		if !state.Running {
			return false, "node is stopped"
		}
		if state.Batch != nil {
			return false, "node has an outstanding output batch"
		}
		return true, ""
	case core.EventCrash:
		if !state.Running {
			return false, "node is already stopped"
		}
		return true, ""
	case core.EventRestart:
		if state.Running {
			return false, "node is already running"
		}
		return true, ""
	case core.EventPersist, core.EventSync, core.EventEmit, core.EventApply, core.EventAcknowledge:
		if !state.Running {
			return false, "node is stopped"
		}
		_, reason := a.operationFor(state, event)
		return reason == "", reason
	case core.EventProtocolInput, core.EventCampaign, core.EventPropose, core.EventQuery, core.EventMessage, core.EventTimeout:
		if !state.Running {
			return false, "target node is stopped"
		}
		if state.Batch != nil {
			return false, "node has an outstanding output batch"
		}
		return a.driver.EnabledInput(event)
	default:
		return false, "unsupported event kind"
	}
}

func (a *Adapter) Apply(ctx context.Context, event core.Event) (core.ApplyResult, error) {
	switch event.Kind {
	case core.EventStart:
		effects, err := a.collect(event.Target)
		return core.ApplyResult{
			Status: core.StatusApplied, Effects: effects,
			Observations: []core.Observation{{Kind: "lifecycle", Label: "node:started", Node: event.Target}},
		}, err
	case core.EventProtocolInput, core.EventCampaign, core.EventPropose, core.EventQuery, core.EventMessage, core.EventTimeout:
		observations, err := a.driver.Invoke(ctx, event)
		if err != nil {
			return core.ApplyResult{}, err
		}
		if event.Kind == core.EventMessage && event.Message != nil {
			observations = append(observations, core.Observation{
				Kind: "transport", Label: "message:delivered", Node: event.Target,
				Value:    event.Message.PayloadDigest,
				Evidence: map[string]string{"from": event.Source, "type": event.Message.TypeHint},
			})
		}
		effects, err := a.collect(event.Target)
		return core.ApplyResult{Status: core.StatusApplied, Effects: effects, Observations: observations}, err
	case core.EventPersist, core.EventSync, core.EventEmit, core.EventApply, core.EventAcknowledge:
		return a.executeOperation(ctx, event)
	case core.EventCrash:
		return a.crash(ctx, event.Target)
	case core.EventRestart:
		return a.restart(ctx, event.Target)
	default:
		return core.ApplyResult{}, fmt.Errorf("unsupported event kind %q", event.Kind)
	}
}

func (a *Adapter) Snapshot() any {
	type runtimeNode struct {
		Running    bool             `json:"running"`
		Batch      string           `json:"batch,omitempty"`
		Operations []core.EventKind `json:"operations,omitempty"`
	}
	runtimeNodes := make(map[string]runtimeNode, len(a.nodes))
	for id, state := range a.nodes {
		current := runtimeNode{Running: state.Running}
		if state.Batch != nil {
			current.Batch = state.Batch.Token
			for _, operation := range state.Batch.Operations {
				current.Operations = append(current.Operations, operation.Kind)
			}
		}
		runtimeNodes[id] = current
	}
	return map[string]any{
		"runtime": map[string]any{"nodes": runtimeNodes},
		"driver":  a.driver.Snapshot(),
	}
}

func (a *Adapter) CheckConformance() error {
	if err := a.driver.CheckConformance(); err != nil {
		return err
	}
	for id, state := range a.nodes {
		if state.Batch != nil {
			if state.Batch.Node != id {
				return fmt.Errorf("node %s holds batch for node %s", id, state.Batch.Node)
			}
			if err := state.Batch.Validate(); err != nil {
				return fmt.Errorf("node %s batch: %w", id, err)
			}
		}
	}
	return nil
}

func (a *Adapter) executeOperation(ctx context.Context, event core.Event) (core.ApplyResult, error) {
	state := a.nodes[event.Target]
	operation, reason := a.operationFor(state, event)
	if reason != "" {
		return core.ApplyResult{}, fmt.Errorf("host operation is not applicable: %s", reason)
	}
	observations, err := a.driver.ExecuteHostOp(ctx, event.Target, state.Batch.Token, operation)
	if err != nil {
		return core.ApplyResult{}, err
	}
	result := core.ApplyResult{Status: core.StatusApplied, Observations: observations}
	if operation.Kind == core.EventEmit {
		message := cloneMessage(operation.Message)
		result.Effects = append(result.Effects, core.Effect{
			Kind: core.EventMessage, Source: message.From, Target: message.To, Message: message,
		})
	}
	if operation.Kind == core.EventAcknowledge {
		state.Batch = nil
		next, collectErr := a.collect(event.Target)
		if collectErr != nil {
			return core.ApplyResult{}, collectErr
		}
		result.Effects = append(result.Effects, next...)
	}
	return result, nil
}

func (a *Adapter) crash(ctx context.Context, node string) (core.ApplyResult, error) {
	state := a.nodes[node]
	var cancel []string
	if state.Batch != nil {
		cancel = append(cancel, state.Batch.Token)
	}
	observations, err := a.driver.Crash(ctx, node)
	if err != nil {
		return core.ApplyResult{}, err
	}
	state.Running = false
	state.Batch = nil
	return core.ApplyResult{
		Status: core.StatusApplied, Observations: observations, CancelGroups: cancel,
	}, nil
}

func (a *Adapter) restart(ctx context.Context, node string) (core.ApplyResult, error) {
	observations, err := a.driver.Restart(ctx, node)
	if err != nil {
		return core.ApplyResult{}, err
	}
	a.nodes[node].Running = true
	effects, err := a.collect(node)
	return core.ApplyResult{Status: core.StatusApplied, Observations: observations, Effects: effects}, err
}

func (a *Adapter) collect(node string) ([]core.Effect, error) {
	state := a.nodes[node]
	if state.Batch != nil {
		return nil, fmt.Errorf("node %s already has outstanding batch %s", node, state.Batch.Token)
	}
	batch, err := a.driver.Poll(node)
	if err != nil || batch == nil {
		return nil, err
	}
	if batch.Node != node {
		return nil, fmt.Errorf("driver returned batch for %s while polling %s", batch.Node, node)
	}
	if err := batch.Validate(); err != nil {
		return nil, fmt.Errorf("invalid output batch: %w", err)
	}
	state.Batch = cloneBatch(batch)
	effects := make([]core.Effect, 0, len(batch.Operations))
	for _, operation := range batch.Operations {
		payload, err := json.Marshal(operationRef{Batch: batch.Token, Operation: operation.Token})
		if err != nil {
			return nil, err
		}
		effects = append(effects, core.Effect{
			Key: operation.Token, Kind: operation.Kind, Target: node, Group: batch.Token,
			After: append([]string(nil), operation.After...), Payload: payload,
			Message: cloneMessage(operation.Message),
		})
	}
	return effects, nil
}

func (a *Adapter) operationFor(state *nodeState, event core.Event) (driver.Operation, string) {
	if state == nil || state.Batch == nil {
		return driver.Operation{}, "no outstanding output batch"
	}
	var ref operationRef
	if err := json.Unmarshal(event.Payload, &ref); err != nil {
		return driver.Operation{}, "invalid host operation reference"
	}
	if event.Group != state.Batch.Token || ref.Batch != state.Batch.Token {
		return driver.Operation{}, "stale output batch"
	}
	for _, operation := range state.Batch.Operations {
		if operation.Token == ref.Operation {
			if operation.Kind != event.Kind {
				return driver.Operation{}, "host operation kind mismatch"
			}
			return cloneOperation(operation), ""
		}
	}
	return driver.Operation{}, "unknown host operation"
}

func cloneBatch(batch *driver.OutputBatch) *driver.OutputBatch {
	if batch == nil {
		return nil
	}
	cloned := *batch
	cloned.Operations = make([]driver.Operation, len(batch.Operations))
	for index, operation := range batch.Operations {
		cloned.Operations[index] = cloneOperation(operation)
	}
	return &cloned
}

func cloneOperation(operation driver.Operation) driver.Operation {
	operation.After = append([]string(nil), operation.After...)
	operation.Message = cloneMessage(operation.Message)
	if operation.Metadata != nil {
		metadata := make(map[string]string, len(operation.Metadata))
		for key, value := range operation.Metadata {
			metadata[key] = value
		}
		operation.Metadata = metadata
	}
	return operation
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

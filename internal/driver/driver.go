package driver

import (
	"context"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

// Capability describes a controllable or observable integration boundary.
// Unsupported capabilities remain visible to coverage instead of being
// silently approximated.
type Capability struct {
	ID        string `json:"id"`
	Supported bool   `json:"supported"`
	Detail    string `json:"detail,omitempty"`
}

type Manifest struct {
	Driver       string       `json:"driver"`
	SUT          string       `json:"sut"`
	SUTVersion   string       `json:"sut_version"`
	Capabilities []Capability `json:"capabilities"`
}

// Operation is protocol-neutral host work frozen from one implementation
// output batch. The driver interprets Token; the host runtime interprets Kind,
// After, and Message.
type Operation struct {
	Token    string                `json:"token"`
	Kind     core.EventKind        `json:"kind"`
	After    []string              `json:"after,omitempty"`
	Message  *core.MessageEnvelope `json:"message,omitempty"`
	Metadata map[string]string     `json:"metadata,omitempty"`
}

type OutputBatch struct {
	Token      string      `json:"token"`
	Node       string      `json:"node"`
	Operations []Operation `json:"operations"`
}

func (b OutputBatch) Validate() error {
	if b.Token == "" || b.Node == "" {
		return errors.New("batch token and node are required")
	}
	seen := make(map[string]bool, len(b.Operations))
	ackToken := ""
	for _, operation := range b.Operations {
		if operation.Token == "" {
			return errors.New("host operation token is required")
		}
		if seen[operation.Token] {
			return fmt.Errorf("duplicate host operation token %q", operation.Token)
		}
		seen[operation.Token] = true
		switch operation.Kind {
		case core.EventPersist, core.EventSync, core.EventEmit, core.EventApply, core.EventAcknowledge:
		default:
			return fmt.Errorf("invalid host operation kind %q", operation.Kind)
		}
		if operation.Kind == core.EventEmit && operation.Message == nil {
			return fmt.Errorf("emit operation %q has no message", operation.Token)
		}
		if operation.Kind == core.EventEmit &&
			(operation.Message.From == "" || operation.Message.To == "" || operation.Message.PayloadDigest == "") {
			return fmt.Errorf("emit operation %q has an incomplete wire message", operation.Token)
		}
		if operation.Kind == core.EventAcknowledge {
			if ackToken != "" {
				return errors.New("output batch must contain exactly one acknowledge operation")
			}
			ackToken = operation.Token
		}
		for _, dependency := range operation.After {
			if dependency == operation.Token {
				return fmt.Errorf("host operation %q depends on itself", operation.Token)
			}
		}
	}
	if ackToken == "" {
		return errors.New("output batch must contain exactly one acknowledge operation")
	}
	indegree := make(map[string]int, len(b.Operations))
	children := make(map[string][]string, len(b.Operations))
	for _, operation := range b.Operations {
		indegree[operation.Token] = len(operation.After)
		for _, dependency := range operation.After {
			if !seen[dependency] {
				return fmt.Errorf("host operation %q has unknown dependency %q", operation.Token, dependency)
			}
			children[dependency] = append(children[dependency], operation.Token)
		}
	}
	queue := make([]string, 0, len(b.Operations))
	for token, count := range indegree {
		if count == 0 {
			queue = append(queue, token)
		}
	}
	visited := 0
	for len(queue) > 0 {
		token := queue[0]
		queue = queue[1:]
		visited++
		for _, child := range children[token] {
			indegree[child]--
			if indegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}
	if visited != len(b.Operations) {
		return errors.New("host operation dependencies contain a cycle")
	}
	operations := make(map[string]Operation, len(b.Operations))
	for _, operation := range b.Operations {
		operations[operation.Token] = operation
	}
	reachable := map[string]bool{ackToken: true}
	stack := []string{ackToken}
	for len(stack) > 0 {
		token := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, dependency := range operations[token].After {
			if !reachable[dependency] {
				reachable[dependency] = true
				stack = append(stack, dependency)
			}
		}
	}
	if len(reachable) != len(b.Operations) {
		return errors.New("acknowledge operation must depend transitively on every host operation")
	}
	return nil
}

// ProtocolDriver is the thin system-specific boundary. Its constructor must
// return the declared nodes already running and ready for an initial Poll; the
// generic host consumes EventStart itself and never passes it to Invoke. It
// calls native APIs and translates native output into host operations, but it
// never schedules, transports, drops, or duplicates messages itself.
type ProtocolDriver interface {
	Protocol() string
	Nodes() []string
	Capabilities() Manifest

	EnabledInput(core.Event) (bool, string)
	Invoke(context.Context, core.Event) ([]core.Observation, error)
	Poll(node string) (*OutputBatch, error)
	ExecuteHostOp(context.Context, string, string, Operation) ([]core.Observation, error)

	Crash(context.Context, string) ([]core.Observation, error)
	Restart(context.Context, string) ([]core.Observation, error)
	Snapshot() any
	CheckConformance() error
}

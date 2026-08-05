package toy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

const Protocol = "toy-quorum-v1"

type node struct {
	Alive       bool            `json:"alive"`
	Role        string          `json:"role"`
	VotedFor    string          `json:"voted_for,omitempty"`
	PendingVote string          `json:"pending_vote,omitempty"`
	Proposal    string          `json:"proposal,omitempty"`
	Votes       map[string]bool `json:"votes,omitempty"`
	Committed   string          `json:"committed,omitempty"`
}

type Adapter struct {
	order []string
	nodes map[string]*node
}

func New(nodeIDs []string) *Adapter {
	ids := append([]string(nil), nodeIDs...)
	sort.Strings(ids)
	nodes := make(map[string]*node, len(ids))
	for _, id := range ids {
		nodes[id] = &node{Alive: true, Role: "follower"}
	}
	return &Adapter{order: ids, nodes: nodes}
}

func (a *Adapter) Protocol() string { return Protocol }

func (a *Adapter) Nodes() []string { return append([]string(nil), a.order...) }

func (a *Adapter) Enabled(event core.Event) (bool, string) {
	n, exists := a.nodes[event.Target]
	if event.Target != "" && !exists {
		return false, "unknown target"
	}
	switch event.Kind {
	case core.EventStart:
		return true, ""
	case core.EventCrash:
		return n != nil && n.Alive, "node is already stopped"
	case core.EventRestart:
		return n != nil && !n.Alive, "node is already running"
	default:
		if n != nil && !n.Alive {
			return false, "target node is stopped"
		}
		return true, ""
	}
}

func (a *Adapter) CheckConformance() error {
	if len(a.order) < 3 {
		return fmt.Errorf("toy quorum requires at least three nodes")
	}
	for _, id := range a.order {
		if _, ok := a.nodes[id]; !ok {
			return fmt.Errorf("missing node %s", id)
		}
	}
	return nil
}

func (a *Adapter) Snapshot() any {
	copyNodes := make(map[string]node, len(a.nodes))
	for id, current := range a.nodes {
		cloned := *current
		cloned.Votes = cloneVotes(current.Votes)
		copyNodes[id] = cloned
	}
	return map[string]any{"nodes": copyNodes}
}

func (a *Adapter) Apply(_ context.Context, event core.Event) (core.ApplyResult, error) {
	switch event.Kind {
	case core.EventStart:
		return core.ApplyResult{Status: core.StatusApplied}, nil
	case core.EventCampaign:
		return a.campaign(event)
	case core.EventMessage:
		return a.message(event)
	case core.EventPersist:
		return a.persist(event)
	case core.EventCrash:
		return a.crash(event)
	case core.EventRestart:
		return a.restart(event)
	case core.EventTimeout, core.EventPropose, core.EventSync, core.EventEmit, core.EventApply, core.EventAcknowledge:
		return core.ApplyResult{Status: core.StatusIgnored}, nil
	default:
		return core.ApplyResult{}, fmt.Errorf("unsupported event kind %q", event.Kind)
	}
}

type message struct {
	Type      string `json:"type"`
	Candidate string `json:"candidate,omitempty"`
	Voter     string `json:"voter,omitempty"`
	Value     string `json:"value,omitempty"`
}

func (a *Adapter) campaign(event core.Event) (core.ApplyResult, error) {
	n, ok := a.nodes[event.Target]
	if !ok {
		return core.ApplyResult{}, fmt.Errorf("unknown target %q", event.Target)
	}
	if !n.Alive {
		return ignored(), nil
	}
	value := "value-1"
	if len(event.Payload) > 0 {
		var request struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(event.Payload, &request); err != nil {
			return core.ApplyResult{}, fmt.Errorf("decode campaign: %w", err)
		}
		if request.Value != "" {
			value = request.Value
		}
	}

	n.Role = "candidate"
	n.VotedFor = event.Target
	n.Proposal = value
	n.Votes = map[string]bool{event.Target: true}
	result := core.ApplyResult{
		Status: core.StatusApplied,
		Observations: []core.Observation{{
			Kind: "transition", Label: "transition:follower->candidate", Node: event.Target,
		}},
	}
	for _, peer := range a.order {
		if peer == event.Target {
			continue
		}
		payload, _ := json.Marshal(message{Type: "request_vote", Candidate: event.Target, Value: value})
		result.Effects = append(result.Effects, core.Effect{
			Kind: core.EventMessage, Source: event.Target, Target: peer, Payload: payload,
		})
	}
	return result, nil
}

func (a *Adapter) message(event core.Event) (core.ApplyResult, error) {
	target, ok := a.nodes[event.Target]
	if !ok {
		return core.ApplyResult{}, fmt.Errorf("unknown target %q", event.Target)
	}
	if !target.Alive {
		return ignored(), nil
	}
	var msg message
	if err := json.Unmarshal(event.Payload, &msg); err != nil {
		return core.ApplyResult{}, fmt.Errorf("decode message: %w", err)
	}
	switch msg.Type {
	case "request_vote":
		if (target.VotedFor != "" && target.VotedFor != msg.Candidate) ||
			(target.PendingVote != "" && target.PendingVote != msg.Candidate) {
			return ignored(), nil
		}
		target.PendingVote = msg.Candidate
		persistPayload, _ := json.Marshal(msg)
		responsePayload, _ := json.Marshal(message{
			Type: "vote_response", Candidate: msg.Candidate, Voter: event.Target, Value: msg.Value,
		})
		return core.ApplyResult{
			Status: core.StatusApplied,
			Effects: []core.Effect{
				{Key: "persist-vote", Kind: core.EventPersist, Target: event.Target, Payload: persistPayload},
				{
					Key: "send-vote", Kind: core.EventMessage, Source: event.Target, Target: msg.Candidate,
					After: []string{"persist-vote"}, Payload: responsePayload,
				},
			},
		}, nil
	case "vote_response":
		if target.Role != "candidate" || target.Proposal != msg.Value || msg.Candidate != event.Target {
			return ignored(), nil
		}
		if target.Votes == nil {
			target.Votes = make(map[string]bool)
		}
		target.Votes[msg.Voter] = true
		if len(target.Votes) < a.quorum() || target.Committed != "" {
			return core.ApplyResult{Status: core.StatusApplied}, nil
		}
		target.Role = "leader"
		target.Committed = msg.Value
		result := core.ApplyResult{
			Status: core.StatusApplied,
			Observations: []core.Observation{
				{Kind: "transition", Label: "transition:candidate->leader", Node: event.Target},
				{Kind: "commit", Label: "commit", Node: event.Target, Value: msg.Value},
			},
		}
		for _, peer := range a.order {
			if peer == event.Target {
				continue
			}
			payload, _ := json.Marshal(message{Type: "commit", Value: msg.Value})
			result.Effects = append(result.Effects, core.Effect{
				Kind: core.EventMessage, Source: event.Target, Target: peer, Payload: payload,
			})
		}
		return result, nil
	case "commit":
		if target.Committed != "" && target.Committed != msg.Value {
			return core.ApplyResult{}, fmt.Errorf("node %s already committed conflicting value", event.Target)
		}
		if target.Committed == msg.Value {
			return ignored(), nil
		}
		target.Committed = msg.Value
		return core.ApplyResult{
			Status:       core.StatusApplied,
			Observations: []core.Observation{{Kind: "commit", Label: "commit", Node: event.Target, Value: msg.Value}},
		}, nil
	default:
		return core.ApplyResult{}, fmt.Errorf("unsupported message type %q", msg.Type)
	}
}

func (a *Adapter) persist(event core.Event) (core.ApplyResult, error) {
	n, ok := a.nodes[event.Target]
	if !ok {
		return core.ApplyResult{}, fmt.Errorf("unknown target %q", event.Target)
	}
	if !n.Alive {
		return ignored(), nil
	}
	var msg message
	if err := json.Unmarshal(event.Payload, &msg); err != nil {
		return core.ApplyResult{}, fmt.Errorf("decode persistence record: %w", err)
	}
	if msg.Type != "request_vote" || n.PendingVote != msg.Candidate {
		return ignored(), nil
	}
	n.VotedFor = msg.Candidate
	n.PendingVote = ""
	return core.ApplyResult{
		Status: core.StatusApplied,
		Observations: []core.Observation{{
			Kind: "persistence", Label: "persist:vote", Node: event.Target, Value: msg.Candidate,
		}},
	}, nil
}

func (a *Adapter) crash(event core.Event) (core.ApplyResult, error) {
	n, ok := a.nodes[event.Target]
	if !ok {
		return core.ApplyResult{}, fmt.Errorf("unknown target %q", event.Target)
	}
	if !n.Alive {
		return ignored(), nil
	}
	n.Alive = false
	n.Role = "follower"
	n.PendingVote = ""
	n.Votes = nil
	return core.ApplyResult{
		Status:       core.StatusApplied,
		Observations: []core.Observation{{Kind: "fault", Label: "fault:crash", Node: event.Target}},
	}, nil
}

func (a *Adapter) restart(event core.Event) (core.ApplyResult, error) {
	n, ok := a.nodes[event.Target]
	if !ok {
		return core.ApplyResult{}, fmt.Errorf("unknown target %q", event.Target)
	}
	if n.Alive {
		return ignored(), nil
	}
	n.Alive = true
	n.Role = "follower"
	return core.ApplyResult{
		Status:       core.StatusApplied,
		Observations: []core.Observation{{Kind: "fault", Label: "fault:restart", Node: event.Target}},
	}, nil
}

func (a *Adapter) quorum() int { return len(a.order)/2 + 1 }

func ignored() core.ApplyResult { return core.ApplyResult{Status: core.StatusIgnored} }

func cloneVotes(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for id, voted := range in {
		out[id] = voted
	}
	return out
}

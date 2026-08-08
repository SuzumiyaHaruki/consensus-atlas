package agentcampaign

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/modelcommand"
)

const commandProtocolVersion = 1

// BlindCommandPlanner is the bounded subprocess transport for the only
// model-facing protocol. It does not inherit the parent environment.
type BlindCommandPlanner struct {
	Path  string
	Args  []string
	Dir   string
	Env   []string
	mu    sync.Mutex
	audit GenerationAudit
}

type blindCommandResponse struct {
	Version  int             `json:"version"`
	Proposal json.RawMessage `json:"proposal"`
	Audit    GenerationAudit `json:"audit"`
}

func (planner *BlindCommandPlanner) GenerateBlind(ctx context.Context, request BlindGenerationRequest) (*BlindProposal, error) {
	if planner == nil || planner.Path == "" || planner.Dir == "" {
		return nil, errors.New("blind command planner path and working directory are required")
	}
	planner.mu.Lock()
	planner.audit = GenerationAudit{}
	planner.mu.Unlock()
	encoded, err := json.Marshal(struct {
		Version int                    `json:"version"`
		Request BlindGenerationRequest `json:"request"`
	}{Version: commandProtocolVersion, Request: request})
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	stdout, err := modelcommand.Run(ctx, modelcommand.Config{
		Path: planner.Path, Args: planner.Args, Dir: planner.Dir, Env: planner.Env,
		StdoutLimit: 8 << 20, StderrLimit: 64 << 10,
	}, encoded)
	if err != nil {
		return nil, fmt.Errorf("blind model planner failed: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	decoder.DisallowUnknownFields()
	var response blindCommandResponse
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode blind model planner response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("blind model planner returned multiple JSON values")
		}
		return nil, err
	}
	if response.Version != commandProtocolVersion || len(response.Proposal) == 0 || string(response.Proposal) == "null" {
		return nil, errors.New("blind model planner returned an invalid version or empty proposal")
	}
	response.Audit.RequestDigest, response.Audit.ResponseDigest = sha256Hex(encoded), sha256Hex(stdout)
	planner.mu.Lock()
	planner.audit = response.Audit
	planner.mu.Unlock()
	proposalDecoder := json.NewDecoder(bytes.NewReader(response.Proposal))
	proposalDecoder.DisallowUnknownFields()
	var proposal BlindProposal
	if err := proposalDecoder.Decode(&proposal); err != nil {
		return nil, fmt.Errorf("decode blind model planner proposal: %w", err)
	}
	if err := proposalDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("blind model planner returned multiple proposal values")
		}
		return nil, err
	}
	return cloneBlindProposal(&proposal), nil
}

func (planner *BlindCommandPlanner) LastBlindGenerationAudit() GenerationAudit {
	if planner == nil {
		return GenerationAudit{}
	}
	planner.mu.Lock()
	defer planner.mu.Unlock()
	return planner.audit
}

type ScriptedBlindPlanner struct {
	Proposals []*BlindProposal
	Audit     GenerationAudit
}

func (planner ScriptedBlindPlanner) GenerateBlind(_ context.Context, request BlindGenerationRequest) (*BlindProposal, error) {
	index := request.Attempt - 1
	if index < 0 || index >= len(planner.Proposals) || planner.Proposals[index] == nil {
		return nil, fmt.Errorf("scripted blind planner has no proposal for attempt %d", request.Attempt)
	}
	return cloneBlindProposal(planner.Proposals[index]), nil
}
func (planner ScriptedBlindPlanner) LastBlindGenerationAudit() GenerationAudit {
	if planner.Audit.Provider == "" {
		return GenerationAudit{Provider: "fixture", Model: "scripted-blind-planner"}
	}
	return planner.Audit
}

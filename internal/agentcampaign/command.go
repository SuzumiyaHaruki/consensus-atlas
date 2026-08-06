package agentcampaign

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	command := exec.CommandContext(ctx, planner.Path, planner.Args...)
	command.Dir = planner.Dir
	command.Env = append([]string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}, planner.Env...)
	command.Stdin = bytes.NewReader(encoded)
	stdout, stderr := &limitedBuffer{limit: 8 << 20}, &limitedBuffer{limit: 64 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("blind model planner failed: %s", message)
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("blind model planner exceeded its output boundary")
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
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
	response.Audit.RequestDigest, response.Audit.ResponseDigest = sha256Hex(encoded), sha256Hex(stdout.Bytes())
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

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	original, remaining := len(data), buffer.limit-buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = true
		return original, nil
	}
	if len(data) > remaining {
		data, buffer.exceeded = data[:remaining], true
	}
	_, _ = buffer.Buffer.Write(data)
	return original, nil
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

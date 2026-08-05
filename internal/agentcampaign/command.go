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

type CommandPlanner struct {
	Path string
	Args []string
	Dir  string
	Env  []string

	mu    sync.Mutex
	audit GenerationAudit
}

type commandResponse struct {
	Version  int             `json:"version"`
	Proposal json.RawMessage `json:"proposal"`
	Audit    GenerationAudit `json:"audit"`
}

// Generate invokes an untrusted planner without a shell or inherited parent
// environment. Only explicitly configured DeepSeek variables cross the
// process boundary.
func (planner *CommandPlanner) Generate(ctx context.Context, request GenerationRequest) (*Proposal, error) {
	if planner == nil || planner.Path == "" || planner.Dir == "" {
		return nil, errors.New("command planner path and working directory are required")
	}
	planner.mu.Lock()
	planner.audit = GenerationAudit{}
	planner.mu.Unlock()
	encoded, err := json.Marshal(struct {
		Version int               `json:"version"`
		Request GenerationRequest `json:"request"`
	}{Version: commandProtocolVersion, Request: request})
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	command := exec.CommandContext(ctx, planner.Path, planner.Args...)
	command.Dir = planner.Dir
	command.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}, planner.Env...)
	command.Stdin = bytes.NewReader(encoded)
	stdout := &limitedBuffer{limit: 8 << 20}
	stderr := &limitedBuffer{limit: 64 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("model planner failed: %s", message)
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("model planner exceeded its output boundary")
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	var response commandResponse
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode model planner response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("model planner returned multiple JSON values")
		}
		return nil, err
	}
	if response.Version != commandProtocolVersion || len(response.Proposal) == 0 || string(response.Proposal) == "null" {
		return nil, errors.New("model planner returned an invalid version or empty proposal")
	}
	response.Audit.RequestDigest = sha256Hex(encoded)
	response.Audit.ResponseDigest = sha256Hex(stdout.Bytes())
	planner.mu.Lock()
	planner.audit = response.Audit
	planner.mu.Unlock()
	proposalDecoder := json.NewDecoder(bytes.NewReader(response.Proposal))
	proposalDecoder.DisallowUnknownFields()
	var proposal Proposal
	if err := proposalDecoder.Decode(&proposal); err != nil {
		return nil, fmt.Errorf("decode model planner proposal: %w", err)
	}
	if err := proposalDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("model planner returned multiple proposal values")
		}
		return nil, err
	}
	return cloneProposal(&proposal), nil
}

func (planner *CommandPlanner) LastGenerationAudit() GenerationAudit {
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
	original := len(data)
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = true
		return original, nil
	}
	if len(data) > remaining {
		data = data[:remaining]
		buffer.exceeded = true
	}
	_, _ = buffer.Buffer.Write(data)
	return original, nil
}

func cloneProposal(proposal *Proposal) *Proposal {
	if proposal == nil {
		return nil
	}
	encoded, err := json.Marshal(proposal)
	if err != nil {
		return nil
	}
	var cloned Proposal
	if json.Unmarshal(encoded, &cloned) != nil {
		return nil
	}
	return &cloned
}

type ScriptedPlanner struct {
	Proposals []*Proposal
	Audit     GenerationAudit
}

func (planner ScriptedPlanner) Generate(_ context.Context, request GenerationRequest) (*Proposal, error) {
	index := request.Attempt - 1
	if index < 0 || index >= len(planner.Proposals) || planner.Proposals[index] == nil {
		return nil, fmt.Errorf("scripted planner has no proposal for attempt %d", request.Attempt)
	}
	return cloneProposal(planner.Proposals[index]), nil
}

func (planner ScriptedPlanner) LastGenerationAudit() GenerationAudit {
	if planner.Audit.Provider == "" {
		return GenerationAudit{Provider: "fixture", Model: "scripted-planner"}
	}
	return planner.Audit
}

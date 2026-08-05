package contractonly

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
)

type CommandGenerator struct {
	Path string
	Args []string
	Dir  string
	Env  []string

	mu    sync.Mutex
	audit autoonboard.GenerationAudit
}

type commandResponse struct {
	Version  int                         `json:"version"`
	Proposal *Proposal                   `json:"proposal"`
	Audit    autoonboard.GenerationAudit `json:"audit"`
}

func (g *CommandGenerator) Generate(ctx context.Context, request GenerationRequest) (*Proposal, error) {
	if g == nil || g.Path == "" || g.Dir == "" {
		return nil, errors.New("contract-only command path and working directory are required")
	}
	encoded, err := json.Marshal(struct {
		Version int               `json:"version"`
		Request GenerationRequest `json:"request"`
	}{Version: Version, Request: request})
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if len(encoded) > 8<<20 {
		return nil, errors.New("contract-only model request exceeded 8 MiB")
	}

	command := exec.CommandContext(ctx, g.Path, g.Args...)
	command.Dir = g.Dir
	command.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}, g.Env...)
	command.Stdin = bytes.NewReader(encoded)
	stdout := &boundedBuffer{limit: 16 << 20}
	stderr := &boundedBuffer{limit: 128 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("contract-only model generator failed: %s", message)
	}
	if stdout.exceeded {
		return nil, errors.New("contract-only model output exceeded 16 MiB")
	}
	if stderr.exceeded {
		return nil, errors.New("contract-only model stderr exceeded 128 KiB")
	}

	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	var response commandResponse
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode contract-only response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("contract-only generator returned multiple JSON values")
		}
		return nil, err
	}
	if response.Version != Version || response.Proposal == nil {
		return nil, errors.New("contract-only generator returned an invalid version or empty proposal")
	}
	response.Audit.RequestDigest = digest(encoded)
	response.Audit.ResponseDigest = digest(stdout.Bytes())
	g.mu.Lock()
	g.audit = response.Audit
	g.mu.Unlock()
	return cloneProposal(response.Proposal), nil
}

func (g *CommandGenerator) LastGenerationAudit() autoonboard.GenerationAudit {
	if g == nil {
		return autoonboard.GenerationAudit{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.audit
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.exceeded = true
		return original, nil
	}
	if len(data) > remaining {
		data = data[:remaining]
		b.exceeded = true
	}
	_, _ = b.Buffer.Write(data)
	return original, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

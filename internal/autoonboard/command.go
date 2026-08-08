package autoonboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/modelcommand"
)

const commandProtocolVersion = 1

type CommandGenerator struct {
	Path string
	Args []string
	Dir  string
	Env  []string

	mu    sync.Mutex
	audit GenerationAudit
}

type commandResponse struct {
	Version int             `json:"version"`
	Binding *Binding        `json:"binding"`
	Audit   GenerationAudit `json:"audit"`
}

// Generate invokes an untrusted model process through a versioned JSON
// stdin/stdout boundary. It never invokes a shell and does not inherit the
// parent's environment, preventing unrelated credentials from reaching the
// model client.
func (g *CommandGenerator) Generate(ctx context.Context, request GenerationRequest) (*Binding, error) {
	if g == nil || g.Path == "" || g.Dir == "" {
		return nil, errors.New("command generator path and working directory are required")
	}
	encoded, err := json.Marshal(struct {
		Version int               `json:"version"`
		Request GenerationRequest `json:"request"`
	}{Version: commandProtocolVersion, Request: request})
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')

	stdout, err := modelcommand.Run(ctx, modelcommand.Config{
		Path: g.Path, Args: g.Args, Dir: g.Dir, Env: g.Env,
		StdoutLimit: 8 << 20, StderrLimit: 64 << 10,
	}, encoded)
	if err != nil {
		return nil, fmt.Errorf("model generator failed: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(stdout))
	decoder.DisallowUnknownFields()
	var response commandResponse
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode model generator response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("model generator returned multiple JSON values")
		}
		return nil, err
	}
	if response.Version != commandProtocolVersion || response.Binding == nil {
		return nil, errors.New("model generator returned an invalid protocol version or empty binding")
	}
	response.Audit.RequestDigest = digestBytes(encoded)
	response.Audit.ResponseDigest = digestBytes(stdout)
	g.mu.Lock()
	g.audit = response.Audit
	g.mu.Unlock()
	return cloneBinding(response.Binding), nil
}

func (g *CommandGenerator) LastGenerationAudit() GenerationAudit {
	if g == nil {
		return GenerationAudit{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.audit
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

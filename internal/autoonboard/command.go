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
	"os"
	"os/exec"
	"strings"
	"sync"
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

	command := exec.CommandContext(ctx, g.Path, g.Args...)
	command.Dir = g.Dir
	command.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}, g.Env...)
	command.Stdin = bytes.NewReader(encoded)
	stdout := &limitedBuffer{limit: 8 << 20}
	stderr := &limitedBuffer{limit: 64 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("model generator failed: %s", message)
	}
	if stdout.exceeded {
		return nil, errors.New("model generator output exceeded 8 MiB")
	}
	if stderr.exceeded {
		return nil, errors.New("model generator stderr exceeded 64 KiB")
	}

	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
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
	response.Audit.ResponseDigest = digestBytes(stdout.Bytes())
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

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
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

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

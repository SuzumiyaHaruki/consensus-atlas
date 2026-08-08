// Package modelcommand runs an untrusted model client behind a bounded,
// environment-isolated stdin/stdout boundary. It owns no model or protocol
// semantics.
package modelcommand

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Config struct {
	Path        string
	Args        []string
	Dir         string
	Env         []string
	StdoutLimit int
	StderrLimit int
}

func Run(ctx context.Context, config Config, stdin []byte) ([]byte, error) {
	if config.Path == "" || config.Dir == "" || config.StdoutLimit <= 0 || config.StderrLimit <= 0 {
		return nil, errors.New("MODEL_COMMAND_CONFIG_INVALID")
	}
	command := exec.CommandContext(ctx, config.Path, config.Args...)
	command.Dir = config.Dir
	command.Env = append([]string{
		"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "LC_ALL=C.UTF-8",
	}, config.Env...)
	command.Stdin = bytes.NewReader(stdin)
	stdout := &limitedBuffer{limit: config.StdoutLimit}
	stderr := &limitedBuffer{limit: config.StderrLimit}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("MODEL_COMMAND_FAILED: %s", message)
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("MODEL_COMMAND_OUTPUT_LIMIT_EXCEEDED")
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

func ValidateSecretFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("MODEL_KEY_NOT_REGULAR_FILE")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("MODEL_KEY_PERMISSIONS_TOO_OPEN")
	}
	return nil
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

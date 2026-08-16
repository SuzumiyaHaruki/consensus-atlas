package omnipaxosv2

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type workerClient struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	decode   *json.Decoder
	encode   *json.Encoder
	stderr   bytes.Buffer
	nextID   uint64
	closed   bool
	terminal bool
}

type workerFailure struct {
	class control.ExecutionFailureClass
	code  string
	cause error
}

func (failure *workerFailure) Error() string { return failure.code + ": " + failure.cause.Error() }
func (failure *workerFailure) Unwrap() error { return failure.cause }
func (failure *workerFailure) ExecutionFailureClass() control.ExecutionFailureClass {
	return failure.class
}
func (failure *workerFailure) ExecutionFailureCode() string { return failure.code }

type workerCallResult struct {
	response workerResponse
	err      error
}

func startWorker(ctx context.Context, path string) (*workerClient, error) {
	cmd := exec.CommandContext(ctx, path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	client := &workerClient{
		cmd: cmd, stdin: stdin, decode: json.NewDecoder(bufio.NewReader(stdout)),
		encode: json.NewEncoder(stdin),
	}
	cmd.Stderr = &client.stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	return client, nil
}

func (client *workerClient) call(ctx context.Context, request workerRequest) (workerResponse, error) {
	if client == nil || client.closed || client.terminal {
		return workerResponse{}, &workerFailure{
			class: control.ExecutionFailureProcessExit, code: "OMNIPAXOS_WORKER_UNAVAILABLE",
			cause: errors.New("OMNIPAXOS_WORKER_CLOSED"),
		}
	}
	if err := ctx.Err(); err != nil {
		return workerResponse{}, &workerFailure{
			class: control.ExecutionFailureDeadline, code: "OMNIPAXOS_WORKER_DEADLINE",
			cause: err,
		}
	}
	client.nextID++
	request.ID = client.nextID
	result := make(chan workerCallResult, 1)
	go func() {
		response, err := client.exchange(request)
		result <- workerCallResult{response: response, err: err}
	}()
	select {
	case completed := <-result:
		return completed.response, completed.err
	case <-ctx.Done():
		client.terminal = true
		killErr := client.cmd.Process.Kill()
		completed := <-result
		return workerResponse{}, &workerFailure{
			class: control.ExecutionFailureDeadline, code: "OMNIPAXOS_WORKER_DEADLINE",
			cause: errors.Join(ctx.Err(), killErr, completed.err),
		}
	}
}

func (client *workerClient) exchange(request workerRequest) (workerResponse, error) {
	if err := client.encode.Encode(request); err != nil {
		return workerResponse{}, &workerFailure{
			class: control.ExecutionFailureAdapterIO, code: "OMNIPAXOS_WORKER_WRITE_FAILED",
			cause: fmt.Errorf("OMNIPAXOS_WORKER_WRITE: %w", err),
		}
	}
	var response workerResponse
	if err := client.decode.Decode(&response); err != nil {
		return workerResponse{}, &workerFailure{
			class: control.ExecutionFailureAdapterIO, code: "OMNIPAXOS_WORKER_READ_FAILED",
			cause: fmt.Errorf("OMNIPAXOS_WORKER_READ: %w: %s", err, client.stderr.String()),
		}
	}
	if response.SchemaVersion != workerSchema || response.ID != request.ID {
		return workerResponse{}, errors.New("OMNIPAXOS_WORKER_RESPONSE_IDENTITY_MISMATCH")
	}
	if !response.OK {
		return workerResponse{}, fmt.Errorf("OMNIPAXOS_WORKER_REJECTED: %s", response.Error)
	}
	return response, nil
}

func (client *workerClient) close() error {
	if client == nil || client.closed {
		return nil
	}
	client.closed = true
	closeErr := client.stdin.Close()
	waitErr := client.cmd.Wait()
	if closeErr != nil {
		return closeErr
	}
	if waitErr != nil {
		return fmt.Errorf("OMNIPAXOS_WORKER_EXIT: %w: %s", waitErr, client.stderr.String())
	}
	return nil
}

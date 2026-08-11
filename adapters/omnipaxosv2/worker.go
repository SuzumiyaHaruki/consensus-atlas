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
)

type workerClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	decode *json.Decoder
	encode *json.Encoder
	stderr bytes.Buffer
	nextID uint64
	closed bool
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

func (client *workerClient) call(request workerRequest) (workerResponse, error) {
	if client == nil || client.closed {
		return workerResponse{}, errors.New("OMNIPAXOS_WORKER_CLOSED")
	}
	client.nextID++
	request.ID = client.nextID
	if err := client.encode.Encode(request); err != nil {
		return workerResponse{}, fmt.Errorf("OMNIPAXOS_WORKER_WRITE: %w", err)
	}
	var response workerResponse
	if err := client.decode.Decode(&response); err != nil {
		return workerResponse{}, fmt.Errorf("OMNIPAXOS_WORKER_READ: %w: %s", err, client.stderr.String())
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

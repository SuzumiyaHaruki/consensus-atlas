// Package blackbox controls deployable targets without importing protocol or
// implementation types. Its wall-clock readiness and opaque call boundaries
// are intentionally weaker than control.Adapter strict replay.
package blackbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	CallSchema   = "consensus-atlas/blackbox-call/v1"
	ResultSchema = "consensus-atlas/blackbox-call-result/v1"
)

type Endpoint struct {
	ID        string `json:"id"`
	Network   string `json:"network"`
	Address   string `json:"address"`
	Readiness bool   `json:"readiness"`
}

// Spec contains only local process and opaque endpoint information. Command
// is argv-based and never interpreted by a shell.
type Spec struct {
	ID                 string     `json:"id"`
	ImplementationID   string     `json:"implementation_id"`
	Executable         string     `json:"executable"`
	Args               []string   `json:"args,omitempty"`
	WorkDir            string     `json:"work_dir"`
	DataDir            string     `json:"data_dir"`
	Endpoints          []Endpoint `json:"endpoints"`
	ReadyTimeoutMillis uint64     `json:"ready_timeout_millis"`
}

func (spec Spec) Validate() error {
	if spec.ID == "" || spec.ImplementationID == "" || spec.Executable == "" {
		return errors.New("BLACKBOX_SPEC_IDENTITY_REQUIRED")
	}
	if !filepath.IsAbs(spec.Executable) || !filepath.IsAbs(spec.WorkDir) || !filepath.IsAbs(spec.DataDir) {
		return errors.New("BLACKBOX_SPEC_ABSOLUTE_PATH_REQUIRED")
	}
	if filepath.Clean(spec.DataDir) == string(filepath.Separator) || spec.ReadyTimeoutMillis == 0 || len(spec.Endpoints) == 0 {
		return errors.New("BLACKBOX_SPEC_BOUNDARY_INVALID")
	}
	seen := make(map[string]struct{}, len(spec.Endpoints))
	readiness := 0
	for _, endpoint := range spec.Endpoints {
		if endpoint.ID == "" || endpoint.Address == "" || (endpoint.Network != "tcp" && endpoint.Network != "unix") {
			return errors.New("BLACKBOX_ENDPOINT_INVALID")
		}
		if endpoint.Network == "unix" && !filepath.IsAbs(endpoint.Address) {
			return errors.New("BLACKBOX_ENDPOINT_ABSOLUTE_PATH_REQUIRED")
		}
		if _, duplicate := seen[endpoint.ID]; duplicate {
			return errors.New("BLACKBOX_ENDPOINT_DUPLICATE")
		}
		if endpoint.Readiness {
			readiness++
		}
		seen[endpoint.ID] = struct{}{}
	}
	if readiness == 0 {
		return errors.New("BLACKBOX_READINESS_ENDPOINT_REQUIRED")
	}
	return nil
}

func (spec Spec) Digest() (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	normalized := spec
	normalized.Args = append([]string(nil), spec.Args...)
	normalized.Endpoints = append([]Endpoint(nil), spec.Endpoints...)
	sort.Slice(normalized.Endpoints, func(i, j int) bool { return normalized.Endpoints[i].ID < normalized.Endpoints[j].ID })
	return control.CanonicalDigest(normalized)
}

type State string

const (
	StateNew     State = "new"
	StateRunning State = "running"
	StateStopped State = "stopped"
)

type LifecycleObservation struct {
	Kind        string `json:"kind"`
	Incarnation uint64 `json:"incarnation"`
	ExitCode    int    `json:"exit_code,omitempty"`
}

type FrozenCall struct {
	ID         string                  `json:"id"`
	TargetID   string                  `json:"target_id"`
	EndpointID string                  `json:"endpoint_id"`
	Sequence   uint64                  `json:"sequence"`
	Payload    control.PayloadEnvelope `json:"payload"`
	Digest     string                  `json:"digest"`
}

type CallResult struct {
	CallID   string                  `json:"call_id"`
	Outcome  string                  `json:"outcome"`
	Response control.PayloadEnvelope `json:"response,omitempty"`
	Digest   string                  `json:"digest"`
}

type Envelope struct {
	spec        Spec
	specDigest  string
	command     *exec.Cmd
	state       State
	incarnation uint64
	callSeq     uint64
	pending     map[string]FrozenCall
}

func New(spec Spec) (*Envelope, error) {
	digest, err := spec.Digest()
	if err != nil {
		return nil, err
	}
	return &Envelope{spec: spec, specDigest: digest, state: StateNew, pending: make(map[string]FrozenCall)}, nil
}

func (envelope *Envelope) State() State        { return envelope.state }
func (envelope *Envelope) Incarnation() uint64 { return envelope.incarnation }
func (envelope *Envelope) SpecDigest() string  { return envelope.specDigest }

func (envelope *Envelope) Start(ctx context.Context) (LifecycleObservation, error) {
	if envelope.state != StateNew {
		return LifecycleObservation{}, errors.New("BLACKBOX_START_STATE_INVALID")
	}
	return envelope.start(ctx, "start")
}

func (envelope *Envelope) Restart(ctx context.Context) (LifecycleObservation, error) {
	if envelope.state != StateStopped {
		return LifecycleObservation{}, errors.New("BLACKBOX_RESTART_STATE_INVALID")
	}
	return envelope.start(ctx, "restart")
}

func (envelope *Envelope) start(ctx context.Context, kind string) (LifecycleObservation, error) {
	if err := os.MkdirAll(envelope.spec.DataDir, 0o700); err != nil {
		return LifecycleObservation{}, err
	}
	command := exec.Command(envelope.spec.Executable, envelope.spec.Args...)
	command.Dir, command.Stdout, command.Stderr = envelope.spec.WorkDir, io.Discard, io.Discard
	if err := command.Start(); err != nil {
		return LifecycleObservation{}, err
	}
	envelope.command = command
	if err := envelope.waitReady(ctx); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		envelope.command, envelope.state = nil, StateStopped
		return LifecycleObservation{}, err
	}
	envelope.incarnation++
	envelope.state = StateRunning
	return LifecycleObservation{Kind: kind, Incarnation: envelope.incarnation}, nil
}

func (envelope *Envelope) Crash() (LifecycleObservation, error) {
	if envelope.state != StateRunning || envelope.command == nil || envelope.command.Process == nil {
		return LifecycleObservation{}, errors.New("BLACKBOX_CRASH_STATE_INVALID")
	}
	if err := envelope.command.Process.Kill(); err != nil {
		return LifecycleObservation{}, err
	}
	_ = envelope.command.Wait()
	exitCode := -1
	if envelope.command.ProcessState != nil {
		exitCode = envelope.command.ProcessState.ExitCode()
	}
	envelope.command, envelope.state = nil, StateStopped
	return LifecycleObservation{Kind: "crash", Incarnation: envelope.incarnation, ExitCode: exitCode}, nil
}

func (envelope *Envelope) Close() error {
	if envelope.state != StateRunning {
		return nil
	}
	_, err := envelope.Crash()
	return err
}

func (envelope *Envelope) FreezeCall(endpointID string, data []byte) (FrozenCall, error) {
	if _, ok := envelope.endpoint(endpointID); !ok {
		return FrozenCall{}, errors.New("BLACKBOX_CALL_ENDPOINT_UNKNOWN")
	}
	envelope.callSeq++
	payload, err := control.NewPayload(CallSchema, "bytes", data)
	if err != nil {
		return FrozenCall{}, err
	}
	id, err := control.StableID("blackbox-call", envelope.specDigest, endpointID, strconv.FormatUint(envelope.callSeq, 10), payload.Digest)
	if err != nil {
		return FrozenCall{}, err
	}
	call := FrozenCall{ID: id, TargetID: envelope.spec.ID, EndpointID: endpointID, Sequence: envelope.callSeq, Payload: payload}
	call.Digest, err = digestWithout(&call.Digest, call)
	if err == nil {
		envelope.pending[call.ID] = call
	}
	return call, err
}

func (envelope *Envelope) Deliver(ctx context.Context, callID string) (CallResult, error) {
	if envelope.state != StateRunning {
		return CallResult{}, errors.New("BLACKBOX_DELIVER_TARGET_NOT_RUNNING")
	}
	call, ok := envelope.pending[callID]
	if !ok {
		return CallResult{}, errors.New("BLACKBOX_CALL_NOT_PENDING")
	}
	endpoint, _ := envelope.endpoint(call.EndpointID)
	connection, err := (&net.Dialer{}).DialContext(ctx, endpoint.Network, endpoint.Address)
	if err != nil {
		return CallResult{}, err
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if _, err := connection.Write(call.Payload.Bytes); err != nil {
		return CallResult{}, err
	}
	if writer, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = writer.CloseWrite()
	}
	response, err := io.ReadAll(connection)
	if err != nil {
		return CallResult{}, err
	}
	delete(envelope.pending, callID)
	return sealResult(CallResult{CallID: callID, Outcome: "delivered"}, response)
}

func (envelope *Envelope) Drop(callID string) (CallResult, error) {
	if _, ok := envelope.pending[callID]; !ok {
		return CallResult{}, errors.New("BLACKBOX_CALL_NOT_PENDING")
	}
	delete(envelope.pending, callID)
	return sealResult(CallResult{CallID: callID, Outcome: "dropped"}, nil)
}

func (envelope *Envelope) waitReady(ctx context.Context) error {
	readyContext, cancel := context.WithTimeout(ctx, time.Duration(envelope.spec.ReadyTimeoutMillis)*time.Millisecond)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		allReady := true
		for _, endpoint := range envelope.spec.Endpoints {
			if !endpoint.Readiness {
				continue
			}
			connection, err := (&net.Dialer{Timeout: 10 * time.Millisecond}).DialContext(readyContext, endpoint.Network, endpoint.Address)
			if err != nil {
				allReady = false
				break
			}
			_ = connection.Close()
		}
		if allReady {
			return nil
		}
		select {
		case <-readyContext.Done():
			return fmt.Errorf("BLACKBOX_TARGET_NOT_READY: %w", readyContext.Err())
		case <-ticker.C:
		}
	}
}

func (envelope *Envelope) endpoint(id string) (Endpoint, bool) {
	for _, endpoint := range envelope.spec.Endpoints {
		if endpoint.ID == id {
			return endpoint, true
		}
	}
	return Endpoint{}, false
}

func sealResult(result CallResult, response []byte) (CallResult, error) {
	payload, err := control.NewPayload(ResultSchema, "bytes", response)
	if err != nil {
		return CallResult{}, err
	}
	result.Response = payload
	result.Digest, err = digestWithout(&result.Digest, result)
	return result, err
}

func digestWithout(field *string, value any) (string, error) {
	*field = ""
	return control.CanonicalDigest(value)
}

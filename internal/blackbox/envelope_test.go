package blackbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/blackbox"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const checkedReportPath = "../../benchmarks/qualifications/blackbox-target-m5.5b/report.json"

type boundaryResult struct {
	Surface  string `json:"surface"`
	Grade    string `json:"grade"`
	Evidence string `json:"evidence"`
}

type conformanceReport struct {
	SchemaVersion string           `json:"schema_version"`
	ReportID      string           `json:"report_id"`
	Checks        map[string]bool  `json:"checks"`
	Boundaries    []boundaryResult `json:"boundaries"`
	Limitations   []string         `json:"limitations"`
	Digest        string           `json:"digest"`
}

func TestIndependentProcessEnvelopeMatchesFrozenReport(t *testing.T) {
	temporary := t.TempDir()
	socket := filepath.Join(temporary, "service.sock")
	target := newFixtureTarget(t, "process-fixture", temporary, socket, "")
	defer target.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started, err := target.Start(ctx)
	if err != nil || started.Kind != "start" || started.Incarnation != 1 {
		t.Fatalf("start = %+v, %v", started, err)
	}

	dropped, err := target.FreezeCall("client", []byte("PUT ignored"))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := target.Drop(dropped.ID); err != nil || result.Outcome != "dropped" {
		t.Fatalf("drop = %+v, %v", result, err)
	}
	if _, err := target.Drop(dropped.ID); err == nil || err.Error() != "BLACKBOX_CALL_NOT_PENDING" {
		t.Fatalf("second drop error = %v", err)
	}
	if response := deliver(t, ctx, target, "GET"); response != "EMPTY" {
		t.Fatalf("dropped call changed target: %q", response)
	}
	if response := deliver(t, ctx, target, "PUT durable-value"); response != "OK" {
		t.Fatalf("put response = %q", response)
	}

	frozenAcrossRestart, err := target.FreezeCall("client", []byte("GET"))
	if err != nil {
		t.Fatal(err)
	}
	crashed, err := target.Crash()
	if err != nil || crashed.Kind != "crash" || target.State() != blackbox.StateStopped {
		t.Fatalf("crash = %+v, %v", crashed, err)
	}
	if _, err := target.Deliver(ctx, frozenAcrossRestart.ID); err == nil || err.Error() != "BLACKBOX_DELIVER_TARGET_NOT_RUNNING" {
		t.Fatalf("deliver while stopped error = %v", err)
	}
	restarted, err := target.Restart(ctx)
	if err != nil || restarted.Kind != "restart" || restarted.Incarnation != 2 {
		t.Fatalf("restart = %+v, %v", restarted, err)
	}
	result, err := target.Deliver(ctx, frozenAcrossRestart.ID)
	if err != nil || string(result.Response.Bytes) != "durable-value" {
		t.Fatalf("frozen call after restart = %+v, %v", result, err)
	}

	report := conformanceReport{
		SchemaVersion: "consensus-atlas/blackbox-target-conformance/v1", ReportID: "process-fixture-m5.5b",
		Checks: map[string]bool{
			"independent-process-started": true, "opaque-call-dropped-without-effect": true,
			"opaque-call-delivered": true, "process-killed-and-restarted": true,
			"data-survived-restart": true, "frozen-call-survived-restart": true,
		},
		Boundaries: []boundaryResult{
			{Surface: "durability", Grade: "observable", Evidence: "value-visible-after-process-restart"},
			{Surface: "external-input", Grade: "interceptable", Evidence: "freeze-deliver-drop"},
			{Surface: "lifecycle", Grade: "interceptable", Evidence: "process-kill-restart-incarnation"},
			{Surface: "opaque-endpoint", Grade: "observable", Evidence: "unix-readiness-and-response"},
		},
		Limitations: []string{"wall-clock-readiness", "one-call-per-connection", "direct-child-process-only", "no-peer-message-boundary", "no-strict-replay"},
	}
	report.Digest = ""
	report.Digest, err = control.CanonicalDigest(report)
	if err != nil {
		t.Fatal(err)
	}
	assertFrozenReport(t, report)
}

func TestSpecRequiresExplicitReadinessEndpoint(t *testing.T) {
	temporary := t.TempDir()
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = blackbox.New(blackbox.Spec{
		ID: "invalid", ImplementationID: "fixture", Executable: executable,
		WorkDir: temporary, DataDir: temporary, ReadyTimeoutMillis: 100,
		Endpoints: []blackbox.Endpoint{{ID: "peer", Network: "unix", Address: filepath.Join(temporary, "peer.sock")}},
	})
	if err == nil || err.Error() != "BLACKBOX_READINESS_ENDPOINT_REQUIRED" {
		t.Fatalf("missing readiness error = %v", err)
	}
}

func deliver(t *testing.T, ctx context.Context, target *blackbox.Envelope, request string) string {
	t.Helper()
	call, err := target.FreezeCall("client", []byte(request))
	if err != nil {
		t.Fatal(err)
	}
	result, err := target.Deliver(ctx, call.ID)
	if err != nil {
		t.Fatal(err)
	}
	return string(result.Response.Bytes)
}

func assertFrozenReport(t *testing.T, fresh conformanceReport) {
	t.Helper()
	encoded, err := os.ReadFile(checkedReportPath)
	if err != nil {
		t.Fatal(err)
	}
	var frozen conformanceReport
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		t.Fatal(err)
	}
	freshJSON, _ := json.Marshal(fresh)
	frozenJSON, _ := json.Marshal(frozen)
	if string(freshJSON) != string(frozenJSON) {
		pretty, _ := json.MarshalIndent(fresh, "", "  ")
		t.Fatalf("frozen blackbox report is stale; fresh report:\n%s", pretty)
	}
}

func TestBlackboxHelperProcess(t *testing.T) {
	marker := -1
	for index, argument := range os.Args {
		if argument == "--blackbox-helper" {
			marker = index
			break
		}
	}
	if marker < 0 {
		return
	}
	if marker+2 >= len(os.Args) {
		t.Fatal("helper arguments missing")
	}
	peer := ""
	if marker+3 < len(os.Args) {
		peer = os.Args[marker+3]
	}
	if err := runFixture(os.Args[marker+1], os.Args[marker+2], peer); err != nil {
		t.Fatal(err)
	}
}

func runFixture(dataDir, socket, peer string) error {
	if err := removeOwnedSocket(dataDir, socket); err != nil {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	for {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleFixtureConnection(connection, filepath.Join(dataDir, "state.bin"), peer)
	}
}

func handleFixtureConnection(connection net.Conn, statePath, peer string) {
	defer connection.Close()
	request, err := io.ReadAll(connection)
	if err != nil || len(request) == 0 {
		return
	}
	command := string(request)
	switch {
	case command == "GET":
		value, err := os.ReadFile(statePath)
		if errors.Is(err, os.ErrNotExist) {
			_, _ = connection.Write([]byte("EMPTY"))
		} else if err == nil {
			_, _ = connection.Write(value)
		}
	case strings.HasPrefix(command, "PUT "):
		if os.WriteFile(statePath, []byte(strings.TrimPrefix(command, "PUT ")), 0o600) == nil {
			_, _ = connection.Write([]byte("OK"))
		}
	case strings.HasPrefix(command, "PEER "):
		if os.WriteFile(statePath, []byte(strings.TrimPrefix(command, "PEER ")), 0o600) == nil {
			_, _ = connection.Write([]byte("ACK"))
		}
	case strings.HasPrefix(command, "SEND ") && peer != "":
		response, err := invokePeer(peer, []byte("PEER "+strings.TrimPrefix(command, "SEND ")))
		if err != nil || len(response) == 0 {
			_, _ = connection.Write([]byte("PEER-ERROR"))
		} else {
			_, _ = connection.Write(response)
		}
	}
}

func invokePeer(address string, request []byte) ([]byte, error) {
	connection, err := net.DialTimeout("unix", address, time.Second)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := connection.Write(request); err != nil {
		return nil, err
	}
	if writer, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = writer.CloseWrite()
	}
	return io.ReadAll(connection)
}

func newFixtureTarget(t *testing.T, id, dataDir, socket, peer string) *blackbox.Envelope {
	t.Helper()
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=TestBlackboxHelperProcess", "--", "--blackbox-helper", dataDir, socket}
	if peer != "" {
		args = append(args, peer)
	}
	target, err := blackbox.New(blackbox.Spec{
		ID: id, ImplementationID: "consensus-atlas/process-fixture-v1",
		Executable: executable, WorkDir: dataDir, DataDir: dataDir, Args: args,
		Endpoints:          []blackbox.Endpoint{{ID: "client", Network: "unix", Address: socket, Readiness: true}},
		ReadyTimeoutMillis: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func removeOwnedSocket(dataDir, socket string) error {
	relative, err := filepath.Rel(filepath.Clean(dataDir), filepath.Clean(socket))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("helper socket is outside data directory")
	}
	info, err := os.Lstat(socket)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("helper socket target is not a socket")
	}
	return os.Remove(socket)
}

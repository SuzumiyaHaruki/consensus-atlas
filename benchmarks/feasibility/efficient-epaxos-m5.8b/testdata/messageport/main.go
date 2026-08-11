package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"epaxosproto"
	"fmt"
	"genericsmr"
	"io"
	"os"
	"state"
	"time"
)

const commitRPC uint8 = 15

type frozenFrame struct {
	ID     string
	Bytes  []byte
	Writes int
}

type framePort struct {
	buffer     []byte
	writes     int
	pending    chan frozenFrame
	decision   chan bool
	downstream bytes.Buffer
}

func newFramePort() *framePort {
	return &framePort{pending: make(chan frozenFrame), decision: make(chan bool)}
}

func (p *framePort) Write(chunk []byte) (int, error) {
	p.buffer = append(p.buffer, chunk...)
	p.writes++
	frame, consumed, complete, err := parseCommit(p.buffer)
	if err != nil {
		return 0, err
	}
	if !complete {
		return len(chunk), nil
	}
	frozen := frozenFrame{ID: frameID(frame), Bytes: frame, Writes: p.writes}
	p.pending <- frozen
	if <-p.decision {
		_, _ = p.downstream.Write(frame)
	}
	p.buffer = append(p.buffer[:0], p.buffer[consumed:]...)
	return len(chunk), nil
}

func parseCommit(stream []byte) ([]byte, int, bool, error) {
	if len(stream) == 0 {
		return nil, 0, false, nil
	}
	if stream[0] != commitRPC {
		return nil, 0, false, fmt.Errorf("unexpected rpc code %d", stream[0])
	}
	reader := bytes.NewReader(stream[1:])
	var message epaxosproto.Commit
	if err := message.Unmarshal(reader); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, 0, false, nil
		}
		return nil, 0, false, err
	}
	consumed := len(stream) - reader.Len()
	frame := append([]byte(nil), stream[:consumed]...)
	return frame, consumed, true, nil
}

func frameID(frame []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("efficient-epaxos/0/1/1\x00"))
	_, _ = hash.Write(frame)
	return hex.EncodeToString(hash.Sum(nil))
}

type trial struct {
	FrameID         string `json:"frame_id"`
	FrameSHA256     string `json:"frame_sha256"`
	FrameBytes      int    `json:"frame_bytes"`
	UnderlyingWrite int    `json:"underlying_writes"`
	ReleasedBytes   int    `json:"released_bytes"`
}

type decision struct {
	Status                  string `json:"status"`
	FramingRequirement      string `json:"framing_requirement"`
	MessagePortWorker       bool   `json:"message_port_worker"`
	RuntimeIntegration      bool   `json:"runtime_integration"`
	FullAdapterAllowed      bool   `json:"full_adapter_allowed"`
	NextStage               string `json:"next_stage"`
	LaterBindingWorkAllowed bool   `json:"later_binding_work_allowed"`
}

func run(message *epaxosproto.Commit, release bool) (trial, error) {
	port := newFramePort()
	replica := &genericsmr.Replica{PeerWriters: make([]*bufio.Writer, 2)}
	replica.PeerWriters[1] = bufio.NewWriter(port)
	done := make(chan struct{})
	go func() {
		replica.SendMsg(1, commitRPC, message)
		close(done)
	}()

	var frozen frozenFrame
	select {
	case frozen = <-port.pending:
	case <-time.After(2 * time.Second):
		return trial{}, fmt.Errorf("frame was not frozen")
	}
	select {
	case <-done:
		return trial{}, fmt.Errorf("SendMsg returned before the frame decision")
	default:
	}
	port.decision <- release
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		return trial{}, fmt.Errorf("SendMsg did not resume")
	}

	digest := sha256.Sum256(frozen.Bytes)
	if release {
		decoded, consumed, complete, err := parseCommit(port.downstream.Bytes())
		if err != nil || !complete || consumed != port.downstream.Len() || !bytes.Equal(decoded, frozen.Bytes) {
			return trial{}, fmt.Errorf("released frame did not decode exactly: %v", err)
		}
	} else if port.downstream.Len() != 0 {
		return trial{}, fmt.Errorf("dropped frame reached the downstream")
	}
	return trial{
		FrameID: frozen.ID, FrameSHA256: hex.EncodeToString(digest[:]),
		FrameBytes: len(frozen.Bytes), UnderlyingWrite: frozen.Writes,
		ReleasedBytes: port.downstream.Len(),
	}, nil
}

func commit(commandCount int) *epaxosproto.Commit {
	commands := make([]state.Command, commandCount)
	for index := range commands {
		commands[index] = state.Command{Op: state.PUT, K: state.Key(index), V: state.Value(index + 1)}
	}
	return &epaxosproto.Commit{
		LeaderId: 0, Replica: 0, Instance: 1, Command: commands, Seq: 1,
		Deps: [5]int32{-1, -1, -1, -1, -1},
	}
}

func main() {
	small, err := run(commit(1), true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	largeRelease, err := run(commit(300), true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	largeDrop, err := run(commit(300), false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if small.UnderlyingWrite != 1 || largeRelease.UnderlyingWrite <= 1 ||
		largeRelease.FrameID != largeDrop.FrameID || largeDrop.ReleasedBytes != 0 {
		fmt.Fprintln(os.Stderr, "message-port invariants failed")
		os.Exit(1)
	}
	result := struct {
		Schema       string   `json:"schema_version"`
		SourceCommit string   `json:"source_commit"`
		SourceTree   string   `json:"source_tree"`
		SmallRelease trial    `json:"small_release"`
		LargeRelease trial    `json:"large_release"`
		LargeDrop    trial    `json:"large_drop"`
		Decision     decision `json:"decision"`
	}{
		"consensus-atlas/epaxos-message-port-probe/v1",
		"791b115669fca472d3136f6a2eda46c00b3f8251",
		"708b8e37f6b50a4e6b6fcbb95045dc53ad7d6344",
		small, largeRelease, largeDrop,
		decision{
			Status: "worker-witness-passed", FramingRequirement: "codec-aware",
			MessagePortWorker: true, RuntimeIntegration: false, FullAdapterAllowed: false,
			NextStage: "m5.9-v1-consumer-migration", LaterBindingWorkAllowed: true,
		},
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	if len(os.Args) == 1 {
		fmt.Println(string(encoded))
		return
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: messageport [frozen-result.json]")
		os.Exit(2)
	}
	expected, err := os.ReadFile(os.Args[1])
	if err != nil || !bytes.Equal(bytes.TrimSpace(expected), encoded) {
		fmt.Fprintf(os.Stderr, "frozen result mismatch: %v\nfresh result:\n%s\n", err, encoded)
		os.Exit(1)
	}
	fmt.Println("efficient/epaxos message-port witness matches frozen result")
}

package blackbox

import (
	"errors"
	"io"
	"net"
	"path/filepath"
	"sync"
	"time"
)

type GatewaySpec struct {
	ID      string   `json:"id"`
	Listen  Endpoint `json:"listen"`
	Forward Endpoint `json:"forward"`
}

func (spec GatewaySpec) Validate() error {
	if spec.ID == "" || spec.Listen.Address == "" || spec.Forward.Address == "" {
		return errors.New("BLACKBOX_GATEWAY_IDENTITY_REQUIRED")
	}
	for _, endpoint := range []Endpoint{spec.Listen, spec.Forward} {
		if endpoint.ID == "" || (endpoint.Network != "tcp" && endpoint.Network != "unix") {
			return errors.New("BLACKBOX_GATEWAY_NETWORK_INVALID")
		}
		if endpoint.Network == "unix" && !filepath.IsAbs(endpoint.Address) {
			return errors.New("BLACKBOX_GATEWAY_ABSOLUTE_PATH_REQUIRED")
		}
	}
	if spec.Listen.Network == spec.Forward.Network && spec.Listen.Address == spec.Forward.Address {
		return errors.New("BLACKBOX_GATEWAY_LOOP_FORBIDDEN")
	}
	return nil
}

type GatewaySnapshot struct {
	Started         bool   `json:"started"`
	Closed          bool   `json:"closed"`
	Partitioned     bool   `json:"partitioned"`
	Accepted        uint64 `json:"accepted"`
	Forwarded       uint64 `json:"forwarded"`
	Rejected        uint64 `json:"rejected"`
	DialFailures    uint64 `json:"dial_failures"`
	Active          int    `json:"active"`
	BytesToTarget   uint64 `json:"bytes_to_target"`
	BytesFromTarget uint64 `json:"bytes_from_target"`
}

type connectionPair struct {
	client   net.Conn
	upstream net.Conn
}

type Gateway struct {
	mu          sync.Mutex
	spec        GatewaySpec
	listener    net.Listener
	partitioned bool
	closed      bool
	next        uint64
	active      map[uint64]*connectionPair
	snapshot    GatewaySnapshot
	wait        sync.WaitGroup
}

func NewGateway(spec GatewaySpec) (*Gateway, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &Gateway{spec: spec, active: make(map[uint64]*connectionPair)}, nil
}

func (gateway *Gateway) Start() error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if gateway.listener != nil || gateway.closed {
		return errors.New("BLACKBOX_GATEWAY_START_STATE_INVALID")
	}
	listener, err := net.Listen(gateway.spec.Listen.Network, gateway.spec.Listen.Address)
	if err != nil {
		return err
	}
	gateway.listener = listener
	gateway.wait.Add(1)
	go gateway.accept()
	return nil
}

func (gateway *Gateway) Partition() error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if gateway.listener == nil || gateway.closed || gateway.partitioned {
		return errors.New("BLACKBOX_GATEWAY_PARTITION_STATE_INVALID")
	}
	gateway.partitioned = true
	for _, pair := range gateway.active {
		_ = pair.client.Close()
		if pair.upstream != nil {
			_ = pair.upstream.Close()
		}
	}
	return nil
}

func (gateway *Gateway) Heal() error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if gateway.listener == nil || gateway.closed || !gateway.partitioned {
		return errors.New("BLACKBOX_GATEWAY_HEAL_STATE_INVALID")
	}
	gateway.partitioned = false
	return nil
}

func (gateway *Gateway) Snapshot() GatewaySnapshot {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	result := gateway.snapshot
	result.Started, result.Closed = gateway.listener != nil, gateway.closed
	result.Partitioned, result.Active = gateway.partitioned, len(gateway.active)
	return result
}

func (gateway *Gateway) Identity() string { return gateway.spec.ID }

func (gateway *Gateway) Close() error {
	gateway.mu.Lock()
	if gateway.closed {
		gateway.mu.Unlock()
		return nil
	}
	gateway.closed = true
	listener := gateway.listener
	for _, pair := range gateway.active {
		_ = pair.client.Close()
		if pair.upstream != nil {
			_ = pair.upstream.Close()
		}
	}
	gateway.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	gateway.wait.Wait()
	return nil
}

func (gateway *Gateway) accept() {
	defer gateway.wait.Done()
	for {
		client, err := gateway.listener.Accept()
		if err != nil {
			gateway.mu.Lock()
			closed := gateway.closed
			gateway.mu.Unlock()
			if closed {
				return
			}
			continue
		}
		gateway.mu.Lock()
		gateway.snapshot.Accepted++
		if gateway.partitioned || gateway.closed {
			gateway.snapshot.Rejected++
			gateway.mu.Unlock()
			_ = client.Close()
			continue
		}
		gateway.next++
		id := gateway.next
		gateway.active[id] = &connectionPair{client: client}
		gateway.wait.Add(1)
		gateway.mu.Unlock()
		go gateway.forward(id)
	}
}

func (gateway *Gateway) forward(id uint64) {
	defer gateway.wait.Done()
	gateway.mu.Lock()
	pair := gateway.active[id]
	gateway.mu.Unlock()
	upstream, err := net.DialTimeout(gateway.spec.Forward.Network, gateway.spec.Forward.Address, time.Second)
	if err != nil {
		gateway.finishDialFailure(id, pair)
		return
	}
	gateway.mu.Lock()
	if gateway.partitioned || gateway.closed {
		gateway.snapshot.Rejected++
		delete(gateway.active, id)
		gateway.mu.Unlock()
		_ = pair.client.Close()
		_ = upstream.Close()
		return
	}
	pair.upstream = upstream
	gateway.snapshot.Forwarded++
	gateway.mu.Unlock()

	var toTarget, fromTarget uint64
	var pipes sync.WaitGroup
	pipes.Add(2)
	go func() {
		defer pipes.Done()
		toTarget = copyAndCloseWrite(upstream, pair.client)
	}()
	go func() {
		defer pipes.Done()
		fromTarget = copyAndCloseWrite(pair.client, upstream)
	}()
	pipes.Wait()
	_ = pair.client.Close()
	_ = upstream.Close()
	gateway.mu.Lock()
	gateway.snapshot.BytesToTarget += toTarget
	gateway.snapshot.BytesFromTarget += fromTarget
	delete(gateway.active, id)
	gateway.mu.Unlock()
}

func (gateway *Gateway) finishDialFailure(id uint64, pair *connectionPair) {
	gateway.mu.Lock()
	gateway.snapshot.DialFailures++
	delete(gateway.active, id)
	gateway.mu.Unlock()
	_ = pair.client.Close()
}

func copyAndCloseWrite(destination, source net.Conn) uint64 {
	written, _ := io.Copy(destination, source)
	if writer, ok := destination.(interface{ CloseWrite() error }); ok {
		_ = writer.CloseWrite()
	}
	return uint64(written)
}

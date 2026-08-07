package etcdraftv2

import (
	cryptorand "crypto/rand"
	"errors"
	"io"
	"sync"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
)

// crypto/rand.Reader is process-global in the official etcd/raft v3.6.0
// implementation. The mutex prevents two Adapter-owned scopes from
// overlapping. Formal qualification will additionally require running this
// Adapter in an isolated process, because unrelated goroutines do not know
// about this mutex.
var nativeEntropyScope sync.Mutex

type nativeEntropy struct {
	provider *controlentropy.Provider
}

type scopedReader struct {
	provider *controlentropy.Provider
	domain   controlentropy.Domain
	err      error
}

func newNativeEntropy(seed []byte) (*nativeEntropy, error) {
	provider, err := controlentropy.New(seed)
	if err != nil {
		return nil, err
	}
	return &nativeEntropy{provider: provider}, nil
}

func (entropy *nativeEntropy) withDomain(domain controlentropy.Domain, operation func() error) (err error) {
	if entropy == nil || entropy.provider == nil {
		return errors.New("ETCDRAFT_V2_ENTROPY_UNAVAILABLE")
	}
	if operation == nil {
		return errors.New("ETCDRAFT_V2_ENTROPY_OPERATION_REQUIRED")
	}
	reader := &scopedReader{provider: entropy.provider, domain: domain}
	nativeEntropyScope.Lock()
	original := cryptorand.Reader
	cryptorand.Reader = reader
	defer func() {
		cryptorand.Reader = original
		nativeEntropyScope.Unlock()
	}()
	if err := operation(); err != nil {
		return err
	}
	return reader.err
}

func (reader *scopedReader) Read(buffer []byte) (int, error) {
	if reader.err != nil {
		return 0, reader.err
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	data, err := reader.provider.Bytes(reader.domain, len(buffer))
	if err != nil {
		reader.err = err
		return 0, err
	}
	return copy(buffer, data), nil
}

var _ io.Reader = (*scopedReader)(nil)

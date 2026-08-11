# M5.21tA marginal Binding cost audit

This artifact records a no-capability refactor of the shared Runtime-to-Adapter command wire contract.
The extraction is accepted because the decoder is used by the protocol-free fixture and three consensus
Adapters, while the Runtime is the single producer. It does not move proposal, persistence, result, PSS,
or process semantics into the shared core.

Reproduce the functional and identity checks with:

```text
make test-raftrs-binding
go test ./...
go vet ./...
go test -race ./internal/control ./internal/controlruntime ./adapters/fixture ./adapters/etcdraftv2 ./adapters/hashicorpraftv2 ./adapters/raftrsv2
```

The checked-in `summary.json` is a line-accounting and regression summary, not a coverage, qualification,
or portability score.

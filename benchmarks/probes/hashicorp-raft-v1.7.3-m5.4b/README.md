# HashiCorp Raft M5.4b Runtime slice

The report records five public runs of the bounded integration test:

```bash
go test -v ./adapters/hashicorpraftv2 \
  -run TestThreeNodeRuntimeDeliverDropAndFSMApply -count=5
```

Each run used the official unmodified module and completed the same six
protocol-neutral selections: two drops, three deliveries, and one opaque
invoke. All five reached the recording FSM's `Apply` boundary.

Trace digests intentionally differ. HashiCorp Raft v1.7.3 owns wall-clock
timers, package-level random staggering, and operational `Log.AppendedAt`
metadata. The artifact therefore records successful outcomes but does not
claim strict replay or qualification.

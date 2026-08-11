# etcd/raft v2 fixed baselines — M5.10

This is a public development measurement, not a holdout result and not a
protocol-correctness claim.

`report.json` contains two fresh three-node etcd/raft Control Runtime v2 runs
under the same 32-decision budget and shared initial Core PSS state:

- `progress-v1` selects host-effect completion, message delivery, and natural
  periodic timeout actions in that priority order;
- `lifecycle-v1` first selects `crash(n3)` and `restart(n3)`, then uses the same
  progress priority.

Both primary traces were replayed against newly constructed Adapters and have
matching trace digests. The report stores trace and sample-sequence identities,
not the full trace or every sample. Its state witnesses are retained so that
the 45-state union remains inspectable. Re-run with:

```bash
make experiment-etcdraft-v2
```

The report status is `measurement-complete`. Oracle and Coverage evaluation
are outside this artifact, so it must not be interpreted as `pass`, `safe`, or
complete protocol coverage.

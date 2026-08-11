# etcd/raft v1/v2 M5.2.5 migration comparison

This is a deterministic migration artifact, not a defect benchmark and not an
Agent evaluation.

Run it with:

```bash
make v1v2-compare-etcdraft
```

The checked report compares externally visible command digests, applied-node
sets, fixed witnesses, Agreement/TraceIntegrity results, and replay stability.
It deliberately does not compare trace bytes, internal event kinds, decision
counts, terms, log indexes, or leaders.

Current result:

- 3 comparable cases passed;
- 0 cases mismatched;
- 1 case is deferred;
- the suite is therefore `qualified=false`.

The deferred case is `natural-leader-change`: legacy v1 explicitly declares
natural election-timeout replay unsupported, whereas v2 has controlled
periodic pulses and deterministic entropy. Replacing v1 with an explicit
`Campaign` would change the input semantics and is not accepted as an
equivalent comparison.

Replay strength also differs and remains visible in every summary: v1 runs a
fresh execution and compares its full execution fingerprint; v2 replays every
selected ActionID from the recorded decision trace. A passing case does not
upgrade the v1 replay guarantee.

Both paths use official `go.etcd.io/raft/v3 v3.6.0`, but their configuration
digests intentionally differ because legacy v1 and v2 use different election
tick settings and host representations. The comparison is against frozen
external expectations, not identical internal configuration.

This partial result does not authorize deletion of the legacy runtime. v1
still owns the current PSS/Coverage/Agent/benchmark execution path, and the
natural-time case is not comparable.

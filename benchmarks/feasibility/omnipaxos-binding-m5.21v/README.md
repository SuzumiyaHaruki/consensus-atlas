# OmniPaxos M5.21v minimal Binding spike

This checked-in result records a bounded integration feasibility test, not an
Adapter qualification.

The unmodified `omnipaxos = 0.2.2` core runs in a target-owned Rust worker. A
Go Adapter maps only periodic logical ticks and complete opaque peer messages
to the existing Control Runtime. The Runtime owns the pending-message set,
duplication, dropping, delivery order, virtual time, enabled actions, and
strict Replay; the worker executes one requested `tick` or `step` and returns.

The integration test duplicated the first produced message, dropped the
original, delivered the clone, continued a natural three-node election, and
observed n1 as leader. It then closed the worker and strictly replayed all 51
decisions in a fresh process with the same trace digest.

Run from the repository root:

```sh
make test-omnipaxos-binding
```

The binary and trace digests in `summary.json` identify the recorded local
BuildID/toolchain. A rebuild with another toolchain may change both; exact
Replay is required within the declared BuildID.

This directory freezes the pre-workload M5.21v result. The current executable
gate and current BuildID are recorded by the successor
`omnipaxos-workload-m5.21w` artifact.

This stage deliberately has no Invoke/workload, durable storage effect,
crash/restart, PSS Mapping, RiskWitness, Oracle, Agent, Campaign, or
Qualification. The synchronous in-memory Storage path must not be described as
a suspendable HostEffect. OmniPaxos is also leader-based, so this result does
not discharge the later leaderless/dependency-graph portability requirement.

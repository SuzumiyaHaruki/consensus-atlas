# OmniPaxos M5.21u deterministic-core probe

This frozen feasibility artifact answers one narrow question: can a non-Raft
consensus core expose logical time and pending messages to an external,
deterministic scheduler without changing the upstream implementation?

The probe pins `omnipaxos = 0.2.2` and `omnipaxos_storage = 0.2.2`. It builds
three fresh in-memory nodes twice in one process, elects a leader only through
logical `tick()` calls, freezes messages returned by `outgoing_messages()`, and
delivers them explicitly through `handle_incoming()`. An opaque request is
appended at non-leader node 2 and checked in the decided suffix of a quorum.

Run from the repository root:

```sh
make probe-omnipaxos-core
```

The target verifies the three probe inputs against `inputs.sha256`, runs Rust
format and lint checks, then compares fresh output byte-for-byte with
`report.json`.

## Boundary

This is not a ConsensusAtlas Adapter and does not qualify OmniPaxos as a test
target. It does not demonstrate crash/restart recovery, a suspendable durable
host-effect boundary, strict Runtime replay, PSS projection, RiskWitness,
WorkloadRouter, or Agent search. No shared Action, Runtime, or PSS code was
changed for this probe.

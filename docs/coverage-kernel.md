# Coverage Kernel v2

Coverage Kernel v2 is the trusted boundary between Agent proposals and a
reported coverage percentage. Agents may select obligation IDs and generate
tests, but cannot mark an obligation covered.

## Fixed denominator

A v2 `Profile` uses `coverage.obligations`; legacy v1 profiles use
`coverage.atoms` and remain replayable. Mixing the two representations is
rejected. The complete Profile is canonically hashed, and a `Ledger` rejects
evidence produced against a different Profile or Driver Manifest.

Each v2 obligation contains:

- explicit reach predicates;
- explicit observation predicates;
- optional strict before/after constraints, same-batch/target/message
  correlation and excluded interval events;
- optional bounded counts with stable distinct keys;
- optional versioned Family Pack semantic predicates;
- required deterministic monitors;
- capability requirements, risk, weight and support status.

The JSON schema is `profiles/schema-v2.json`. Go validation remains the trusted
validator and additionally checks weight sums, unique IDs, event kinds and
non-empty evidence.

## Strong evidence

The matcher grants coverage only when all five conditions hold:

```text
Reach + Observe + Check + Replay + Conform
```

Trace predicates inspect real Runtime event kinds, endpoints, outcomes and
trusted observations. An Agent target or an invented label cannot satisfy an
obligation that also requires a real event or ordering predicate. A stored evidence reference contains the trace
digest, matching steps, checked monitors, Oracle violations and artifact/run
identity.

An Oracle failure does not erase coverage: the scenario was reached and
checked, while the Oracle result is reported independently.

## Ledger

`coverage.NewLedger` freezes deep copies of the Profile and Driver Manifest.
Its mutable state is private; callers and Agents receive detached `Report()`
and `Debts()` snapshots. `AddRun` is the only state transition.

Ledger entry states are:

- `uncovered`: no strong witness and no targeted attempt;
- `attempted`: targeted by a test but strong evidence is incomplete;
- `covered`: at least one strong witness exists;
- `unsupported`: remains in the denominator but is not actionable scheduling
  debt.

Debts are returned in deterministic risk-first, attempt-count, ID order. A run
may cover an untargeted obligation when the trace genuinely supplies its
evidence; targeting alone only increments the attempt count.

## Integration versus campaign profiles

The etcd/raft Contract compiles to a 12-obligation integration Profile. It
answers only whether the Driver exposes the declared control and observation
boundaries. It is not used as the final test-campaign denominator.

`families/raft/campaign.go` expands the reviewed bounded spec
`profiles/raft/three-node-cft-v1.json` into a separate 55-obligation campaign
Profile. It covers role transitions, Ready/message orderings, role and storage
crash points, message faults, bounded depth/quorum/log/partition shapes and
property activations. Two obligations remain explicitly unsupported by the
current etcd/raft Driver. The compiler derives support only from the frozen
Driver Manifest, sorts the denominator and validates Profile v2 before output.

Raft state relations are evaluated by the versioned Raft Family matcher over
frozen trace snapshots. The generic matcher contains no Raft type or role.
Actual unsafe outcomes such as conflicting durable entries remain Oracle
results; the compiler rejects them as required reachable coverage shapes.

The runner writes both the summary and the single-run Ledger into its result.
`internal/campaign` now adds replayed Random/DFS runs from bounded Test Plans to
one Ledger and supplies only detached Coverage Debt to the planning boundary.
`internal/agentcampaign` now keeps that Ledger in a private incremental Session,
records a hash-chained Blackboard, and accepts bounded Test Plan proposals from
the first DeepSeek Planner. Scenario/Critic role separation and reliable repair
of failed plans remain the next milestone.

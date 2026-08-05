# Bounded Raft campaign denominator

The Raft campaign denominator is deliberately separate from the 12-item
etcd/raft onboarding result:

```text
validated integration Profile + Driver Manifest
                         +
versioned Raft Family campaign spec
                         |
             deterministic compiler
                         v
        immutable Profile v2 + digest
```

The integration Profile proves that the concrete Driver can expose declared
events and witnesses. The campaign Profile states which fundamental scenario
classes a bounded test suite should exercise. An Agent may select an existing
obligation ID, but cannot add an obligation after the campaign starts, change
its predicate, mark it supported or claim it covered.

## Frozen v1 scope

`profiles/raft/three-node-cft-v1.json` declares:

- three voters;
- up to two distinct election rounds and two proposals;
- one crash, restart and partition tier;
- five role transitions, four message classes and two faulted message classes;
- three crash roles and five explicit host crash cutpoints;
- equal/non-empty and strict-prefix durable-log relations;
- minority/quorum and isolated-leader partition shapes;
- five separately weighted categories.

The deterministic expansion contains 55 obligations:

| Category | Count | Examples |
| --- | ---: | --- |
| transition | 5 | explicit election, quorum election, step-down |
| ordering | 12 | Ready DAG, same-message release/delivery, propose/commit |
| fault | 13 | role crash, exact host cutpoint, typed drop/duplicate |
| boundary | 15 | depth tiers, quorum, log relation, partition shape |
| property | 10 | agreement activation and progress after recovery/fault |

The current embedded etcd/raft Manifest marks 53 supported and preserves two
unsupported obligations: native randomized timeout replay and a Ready
release-before-sync overlap. Unsupported entries remain in the denominator and
are not returned to a planning Agent as actionable debt.

Absolute node IDs, term values, indexes, message IDs and payloads do not appear
in the denominator. Counts use semantic depth or stable relations. For example,
two different numeric terms with the same protocol shape do not create two
state classes in the PSS; the explicit election-round bound only records that a
test exercised repeated elections as a path condition.

## Trusted evidence

The protocol-neutral matcher checks concrete event kinds, typed messages,
outcomes, trusted observations, same Ready group, same endpoint, same frozen
message, excluded interval events and bounded distinct counts. The Raft Family
matcher adds only versioned relations over immutable snapshots:

- target role immediately before/after an event;
- safe durable application-log relations;
- partition shape relative to quorum and the current leader;
- running leader count.

Host crash cutpoints are not inferred from a label. The frozen host snapshot
must identify an outstanding batch and its declared operations, and the trace
must show exactly which operations from that batch completed before the crash.

Actual agreement violations, conflicting entries or other bugs are reported
by Oracles independently. Requiring a correct implementation to reach an
unsafe state would make coverage uninterpretable, so the campaign compiler
rejects `conflict` as a required safe log-shape obligation.

## Commands and current baseline

```bash
make coverage-compile-etcdraft
make run-raft
```

The first command writes
`artifacts/profiles/etcdraft-campaign-v1.json`. With the current
`etcdraft-election-crash` scenario, the audited single-run baseline covers
25/55 obligations and scores 44.41 with no Oracle violation. The bounded expert
Test Plan Suite now accumulates multiple replayed Random/DFS runs in one Ledger
and reaches 39/55 and 66.38. Both remain deliberately non-high: the remaining
Coverage Debt is explicit input to the next Agent Coordinator rather than a
manually curated claim of completeness.

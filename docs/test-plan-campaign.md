# Test Plan DSL and deterministic campaign v1

This stage closes the first non-LLM coverage loop:

```text
frozen Profile + Coverage Debt
              |
       bounded Test Plan
              |
 deterministic concretizer
              v
Scenario setup + Random/DFS decisions
              |
 strict decision replay + Oracle + conformance
              |
       trusted Coverage Ledger
              v
     updated Coverage Debt
```

## Authority boundary

A Test Plan may provide only:

- IDs of supported obligations already present in the frozen Profile;
- bounded protocol inputs (`campaign`, `propose`, `timeout`, `crash`,
  `restart`);
- bounded prepare operations using stable selectors;
- complete network partitions, heal, logical-time advance and drain;
- Random or DFS and explicit run/decision/fault budgets.

It cannot provide or override an evidence predicate, semantic matcher, Oracle,
monitor set, capability status, coverage result or score. Direct message and
host-operation injection is rejected. Transient event-ID selectors are also
rejected, as are transient host batch-group selectors; message delivery and
host work must originate from the real Runtime.
Every partition must place every Profile node in exactly one group.

The suite carries the exact Profile ID and digest. Unknown or unsupported
targets, duplicate targets, unbounded duplication and aggregate budgets above
the suite limit fail before execution. The Go validator is the trusted check;
`plans/schema-v1.json` is the authoring schema.

## Concretization and execution

`internal/testplan` translates a valid plan without model judgment. It sorts
Profile nodes, adds one common startup input per node, drains the startup
Ready work with a fixed trusted bound, translates prepare actions, and finally
injects the declared stimuli. Targets and Explorer budgets are copied without
change.

`internal/campaign` then creates a fresh official implementation for every
run. Random/DFS chooses only among Engine candidates. The resulting decision
log is forced against another fresh implementation; an execution error,
conformance failure or unstable replay prevents strong coverage evidence.
Oracle failures remain independently visible and do not erase a genuinely
reached scenario.

The report stores one deterministic setup trace per plan and one measurement
trace/decision log per run instead of duplicating the setup prefix in every
run. Concatenating those two traces reconstructs the full trace whose digest is
recorded by the Ledger.

Only `CoverageLedger.AddRun` changes coverage state. A target increments its
attempt count while it remains debt; targeting alone never marks it covered.
Plans whose targets have all been covered by earlier plans are skipped.
Untargeted obligations can still be covered when the trace supplies complete
strong evidence.

A structurally valid plan can still fail during concretized setup—for example,
when a stable selector finds no runtime event. That failure is stored in the
plan report for a future Critic/repair loop; it adds no run and cannot mutate
the Ledger. Trusted internal errors and context cancellation still fail the
campaign.

## etcd/raft expert baseline

`plans/etcdraft-expert-v1.json` is a hand-written, digest-bound baseline with
four plans:

- minority partition and recovery using DFS;
- Ready crash/restart cutpoints using seeded Random;
- typed message drop/duplicate using seeded Random;
- contended elections and bounded depth using seeded Random.

Run it with:

```bash
make campaign-etcdraft
```

The current deterministic result is:

```text
covered             39 / 55
score                66.38
actionable debt      14
unsupported          2
charged runs         33
charged decisions    1624
unstable replay      0
Oracle violations    0
```

The suite declares maxima of 40 runs and 2304 decisions. Actual charges are
lower when an Explorer reaches its decision budget or its paths become
quiescent. Several explicitly targeted obligations remain `attempted`; this is
intentional evidence that the planner cannot award itself credit.

During this experiment, rejected proposals revealed that the etcd/raft Driver
had exposed proposal inputs on followers even though native Raft must drop
them. `EnabledInput` now exposes proposal only on the current leader. This is a
Driver precondition correction, not a search heuristic or matcher relaxation.

The first DeepSeek Planner is now connected above this DSL through
`internal/agentcampaign`. It receives detached Debt and execution findings,
while the concretizer, runtime, replay, Oracle and Ledger remain deterministic.
The v0.1 run established the boundary but did not reliably repair an invalid
leader/proposal precondition; Scenario and Critic remain separate next-stage
roles. See `docs/stage-v0.1-single-planner.md`.

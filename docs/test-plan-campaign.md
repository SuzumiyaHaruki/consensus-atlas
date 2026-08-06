# Test Plan DSL v1 and deterministic Campaign report v2

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
- bounded protocol inputs (`campaign`, `propose`, `query`, `timeout`, `crash`,
  `restart`);
- bounded prepare operations using stable selectors, including an opaque
  execution-local reference captured from exactly one Runtime-owned message;
- complete network partitions, heal, logical-time advance and drain;
- Random or DFS and explicit run/decision/fault budgets.

It cannot provide or override an evidence predicate, semantic matcher, Oracle,
monitor set, capability status, coverage result or score. Direct message and
host-operation injection is rejected. Transient event-ID selectors are also
rejected, as are transient host batch-group selectors; message delivery and
host work must originate from the real Runtime.
Every partition must place every Profile node in exactly one group.

`capture_message` can only bind a message that already exists in the Runtime
mailbox and is selected uniquely by its stable metadata. `execute_ref` can
consume that one captured message later; it cannot name an Engine event ID,
replace the payload, or be used before capture. This supports explicit
temporal plans while keeping message ownership in the Runtime.

`execute_optional` is limited to a uniquely selected host output operation.
It executes that operation when the SUT produced it, and otherwise records a
charged no-op setup step. It cannot select an input or network message. This
lets one plan compare a behavior that produces an output with a correct
control that legitimately does not produce it.

`advance` is a Runtime control operation, not a request to invoke a protocol timeout. It records a
`clock-advance` trace boundary and may make a Driver-declared timer eligible for normal scheduling. A due
timer remains pending until a subsequent ordinary `execute` choice; the plan cannot assign a native
`timer_id`, delete a declared timer, or cause a Driver to manufacture a timer declaration.

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
the Ledger. It does contribute fresh-SUT/setup work, so an Agent cannot obtain
free retries by failing before the Explorer makes a decision. Trusted internal
errors and context cancellation still fail the campaign.

Campaign report v2 adds separate primary and replay execution costs. Each
fresh-SUT attempt, repeated scenario step or batch Runtime event, and
measurement decision contributes deterministic work units. New benchmark
comparisons use primary work as the shared budget; historical v1 reports retain
their decision counts but cannot enter the trusted Defect Benchmark.

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

`Blind Planner Agent v1` receives only an opaque trial ID, Profile identity/node
projection, Driver-declared input shapes, supported capability names, opaque
debt refs, bounded DSL policy and mechanical aggregate findings. The trusted
Coordinator alone maps a `debt-<sha256>` ref back to a real obligation before
concretization; it alone retains the Driver Manifest, trace, monitor, Oracle
and Ledger. The request protocol is test-covered but has not called a model or
made an effectiveness claim. See `docs/stage-m4.12-blind-planner-v1.md`.

For a formal trial, accepted resolved plans do not return to the Planner. They
are written only to a private `TrustedReplayBundle`; `cmd/blind-replay` uses
that bundle to reproduce the same Campaign identity without a model call, so
the trusted evaluator can compare the full Campaign report digest. See
`docs/stage-m4.14-blind-trial-replay.md`.

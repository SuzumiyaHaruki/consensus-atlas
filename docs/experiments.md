# Equal-budget exploration experiments

`cmd/experiment` compares schedule-search methods above the same deterministic Runtime and below the same PSS projector. It does not let an explorer mutate protocol semantics, Oracle logic or coverage denominators.

## Measurement window

An experiment has two phases:

```text
deterministic setup                 measured exploration
start/bootstrap/drain/inject   --> scheduler decisions
not charged                         charged one-for-one
```

The setup scenario is replayed for every run. All runs must have the same setup execution fingerprint and the same canonical PSS root state. The report stores a `measurement_window.id` derived from Profile ID, PSS ID and setup fingerprint. Reports with different window IDs must not be compared directly.

The canonical root state is included at decision budget zero. Setup transitions are not included in the discovery curve.

## Budget model

Three limits are explicit:

- `decision_budget`: total charged decisions across all runs;
- `budget_per_run`: maximum path depth before starting from the root again;
- `max_runs`: upper bound preventing repeated quiescent runs from continuing forever.

Every execute, message drop or message duplicate action produces exactly one trace record and costs one scheduler decision. The experiment fails if that one-to-one relation is violated.

The report distinguishes:

- `decision_budget_reached`;
- `run_limit_reached`;
- `search_space_exhausted`.

Only reports that reached the same target decision budget, or curves truncated to their common charged prefix, should be compared as equal-budget results.

## Baselines

### Random

Each run uses a published seed derived deterministically from the base seed and run number. Candidate selection is uniform over the current action list. Repeating the experiment with the same inputs must reproduce decisions and full execution fingerprints.

### DFS

DFS chooses candidates in deterministic Runtime order, runs a path, then replays from the shared root and increments the deepest choice that still has an alternative. It never clones an in-memory SUT state. This makes it applicable to native systems that only support restart/re-execution.

The first DFS baseline is intentionally naive: it does not yet implement sleep sets, state pruning or DPOR. Its role is to provide a transparent systematic baseline.

## Action policy

Execution of enabled events is always available. Optional message actions are:

- drop a pending message;
- duplicate a pending message.

Duplication requires `max_duplicates_per_run > 0`. This prevents an unbounded action from silently creating an infinite branching factor. Crash/restart actions are injected declaratively by the setup scenario and then become enabled according to Runtime state.

## Running

```bash
make experiment-random
make experiment-dfs
```

Equivalent direct invocation:

```bash
make auto-onboard-etcdraft
go run ./cmd/experiment \
  -profile artifacts/onboarding/etcdraft-profile-v1.json \
  -setup scenarios/etcdraft-explore-setup.json \
  -strategy random \
  -runs 64 \
  -budget 64 \
  -decision-budget 128 \
  -seed 1 \
  -drop-messages=true \
  -out artifacts/etcdraft-random-experiment.json
```

`-verify-replay=true` is the default. It creates a fresh setup root and forces each recorded action/event ID without asking Random or DFS to choose again; candidate count, choice position, duplicate ID, termination, conformance and strict fingerprints must match. The report contains setup evidence, every decision, per-run trace, Oracle result, pending events, the cross-run PSS state union and first-discovery witnesses.

## Area metrics

`prefix_area` is the discrete sum:

```text
sum from b=1 to B of D(b)
```

`self_normalized_area = prefix_area / (B * D(B))` describes how early a method found its own final set. It must not be used alone to compare absolute discovery strength: a method that quickly finds five states can have a higher self-normalized area than one that gradually finds thirteen. Always report final unique states and raw prefix area at the same budget.

A future comparison tool may normalize against the union of all evaluated methods, but that post-hoc reference set is still not a completeness denominator.

## Current limitations

- DFS has no state memoization, DPOR or conservative independence rules.
- Setup replay cost is excluded from the scheduler-decision budget; wall-clock/CPU and Agent token cost still need a separate cost report.
- The current workload has one bounded campaign and crash/restart opportunity; workload-family generation is not implemented.
- A state-discovery curve does not cover different paths to the same state; transition, ordering and fault obligations remain separate.

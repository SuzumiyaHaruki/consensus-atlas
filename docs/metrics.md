# Metrics: state discovery and fixed-profile coverage

ConsensusAtlas deliberately reports two different coverage views. They answer different research questions and must not be collapsed into one score.

## 1. Protocol-state discovery (PSD)

PSD evaluates a search method without pretending that the reachable state space has a known denominator. For method `m` and budget `b`:

```text
D_m(b) = | union of PSS state keys discovered by m within budget b |
```

The curve answers: under the same budget, which method discovers semantically different protocol states faster?

This is an internal search-efficiency metric for comparing random, DFS/DPOR, greybox and Agent-guided exploration. It is neither a final completeness score nor external evidence that an Agent is effective.

The scenario runner records a single-trace prefix curve:

```json
"protocol_state_discovery": {
  "pss_id": "raft-family-pss-v1",
  "samples": 21,
  "unique_states": 21,
  "curve": [],
  "states": []
}
```

Each state witness stores its canonical key, first trace step and canonical state. This makes every count auditable.

`cmd/experiment` additionally aggregates the state-key union across independent runs from one verified measurement root. Historical curves are indexed by charged scheduler decisions. New benchmark comparisons must also index or cap them by Campaign v2 primary work so repeated setup is not free.

### Fair comparison protocol

A cross-method experiment must freeze:

- SUT build, Driver and runtime;
- PSS projector and version;
- Profile bounds, initial cluster state and workload distribution;
- Oracle set;
- measurement window and budget definition.

Use Campaign v2 `primary.work_units` as the primary common budget. It charges fresh-SUT attempts, repeated bootstrap/prepare work, Runtime events drained during setup, and measurement events. Scheduler decisions and run count remain separate explanatory axes; neither is sufficient because setup lengths and quiescent paths differ. Also report replay work, executed SUT inputs, wall-clock/CPU and model tokens. Repeat stochastic methods with fixed, published seeds and report median plus dispersion.

Bootstrap and other fixed setup transitions must be excluded from the measured search window or shown as a separate shared prefix. Otherwise every method receives the same early discoveries and area-under-curve comparisons are biased.

Recommended reports are:

- unique PSS states versus scheduler decisions;
- unique PSS states versus time/cost;
- normalized area under the discovery curve for a fixed budget;
- time or decisions to reach common state-count thresholds;
- unique Oracle failures and semantic counterexamples.

## 2. Current Core PSS v1 key

The active implementation is `internal/psscore`, with schema
`consensus-atlas/core-pss/v1` and target-owned semantic mapping IDs. It combines two components:

- `control`: participant lifecycle and relative incarnation rank, blocked links, plus pending Runtime messages,
  temporal items and host effects;
- `semantic`: target-projected participant modes, relative epoch/decision/value entities, stages and typed relations.

Native participant names are canonicalized by permutation; native item IDs, payloads and absolute deadlines are removed.
Exact participant-permutation canonicalization is bounded to eight participants. The online sampler records the initial
root and then one sample after every successfully applied Runtime action.

This definition has an important construct-validity limitation: `control.pending` makes the current unique-state count a
mixture of protocol-semantic state and scheduler/control-frontier state. In a diagnostic re-projection of the A9d6
OmniPaxos artifact, the reported 31 samples / 29 unique states become 5 unique states when only
`control.pending` is removed. This is not a new effectiveness experiment; it demonstrates that the published 29 count
must not be described as 29 distinct protocol states.

Before formal search comparisons, derive and report two views from the same Trace:

- protocol-semantic state discovery, excluding queue churn from the primary semantic key;
- scheduler/control-frontier discovery, preserving pending-item and ordering diversity.

Do not create a second Ledger or combine the two views into one score. Until these views and their invariance/separation
tests are implemented, label existing Core PSS numbers as mixed control-state diagnostics.

## 3. Fixed-profile semantic coverage

The existing Profile score answers a different question:

> How many of a frozen, reviewable set of test obligations have strong evidence?

Profile v2 uses structured Coverage Obligations rather than a single free-form witness label. An obligation requires `Reach + Observe + Check + Replay + Conform`. A digest-bound Coverage Ledger accumulates strong evidence across tests and reports uncovered, attempted, covered and unsupported entries. The fixed denominator allows a final test campaign to report what remains unsupported or uncovered. It still does not estimate the probability that the protocol is correct.

For etcd/raft, the 12 onboarding obligations measure integration quality only.
The final bounded test metric uses the independently compiled 55-obligation
Raft campaign Profile. This prevents a Driver capability catalog from being
misreported as comprehensive protocol testing.

Use PSD to explain state-discovery behavior. Use fixed Profile/SCS coverage to assess the resulting test suite against declared obligations. Evaluate method effectiveness primarily on hidden historical defects/semantic mutants and correct controls. Always report trusted Oracle results separately.

## 4. External defect effectiveness

The primary method-level result is evaluated by `internal/defectbench` under a private, versioned Manifest:

- independent root-cause kill rate on hidden historical defects and semantic mutants;
- primary work to first trusted kill and stable reproduction;
- false-positive rate on correct controls;
- invalid-trial and total primary/replay cost.

The evaluator recomputes registered Oracle monitors from stored traces and requires identity, budget, replay and conformance evidence. Coverage score and PSS discoveries are copied only as explanatory covariates; they cannot create kill credit. Multiple variants representing one root cause count once in the primary percentage. See `docs/defect-benchmark.md`.

## 5. Known limitations and safeguards

- A state key can over-merge if the PSS omits a safety-relevant relation, or over-split if it retains an irrelevant one. Invariance and separation tests are therefore part of the Family Pack.
- State coverage cannot distinguish two paths reaching the same state. Transition, ordering and causal-graph coverage remain necessary.
- A growing discovery curve does not establish completeness in an open or unbounded state space.
- Campaign v2 now records deterministic primary/replay work, but the harness does not yet report wall-clock/CPU/RSS cost or repeated-seed confidence intervals.
- PSS changes require a new ID. Results from different PSS IDs must not be combined on the same curve.

The current `self_normalized_area` uses each method's own final state count. It describes discovery timing only and must be shown beside final unique states and raw prefix area; it is not a shared completeness-normalized score.

The design is inspired by MODIST's use of protocol-level state discovery for comparing search strategies, while retaining ConsensusAtlas's separate fixed-denominator result assessment.

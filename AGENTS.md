# ConsensusAtlas Repository Instructions

This file contains durable repository constraints. Progress logs and individual
experiment narratives belong in `docs/CURRENT_STAGE.md` or the relevant result
README, not here.

## Read first

Before changing code, read:

1. `docs/ConsensusAtlas-总体规划.md`;
2. `docs/CURRENT_STAGE.md`;
3. the package documentation and tests for the affected component.

Preserve stronger trust, reproducibility, and protocol-independence constraints
when documentation disagrees. Only the plan inside `docs/` is maintained; there
is no desktop mirror.

## Objective and trust boundary

ConsensusAtlas uses Agents to investigate CFT and limited-BFT consensus
implementations. Agents own uncertain hypothesis generation, semantic planning,
source navigation, and feedback-driven repair. Trusted Go code owns:

- Action admission, concretization, scheduling, and execution;
- message and host-effect ownership;
- deterministic Trace, Replay, and conformance;
- Observation/PSS projection from real evidence;
- Oracle execution and formal result aggregation;
- method identity, budget accounting, and private evaluation data.

An Agent proposal is not evidence or a verdict. It may fail typed validation,
refer to an unavailable capability, or be revised. Agents must not write ledgers,
change an Oracle, invent an Action, or turn natural-language agreement into a
finding. Oracle-derived results remain terminal evaluator data and must not feed
private labels back into online planning.

Do not add a generic scoring DSL, compatibility contract, second execution
engine, second ledger, or new gate merely to make an intermediate artifact look
more complete. By default, do not add hashes, frozen contracts, baselines, or
gates. Add one only when a concrete failure scenario exists and ordinary types,
tests, version fields, identities, or uniqueness constraints cannot address it.

## Architecture

Packages under `internal/` stay protocol-neutral. Concrete protocol types and
semantics belong in adapters, target-owned packages, or explicit composition
roots. In particular:

- an Adapter translates an official implementation's API; it does not schedule,
  transport, discard, duplicate, or reorder messages;
- Runtime controls only Action kinds and capabilities the Target actually
  exposes;
- Target-local Observation and Oracle extensions are namespaced and registered
  without teaching the Core Raft, Paxos, or another protocol;
- unsupported behavior is reported as a capability/fidelity gap, not as a
  failed hypothesis;
- official unmodified implementations are preferred for controls.

Preserve distinctions between produced/released/terminal messages, visible and
durable storage, applied state, setup/primary/replay work, and exact versus
semantic identities. Crash/restart must not silently turn volatile state into
durable state or erase Runtime-owned released messages.

## Evaluation

Keep four result classes separate:

1. independent replay-stable Oracle findings and control false positives;
2. semantic exploration (PSS states/transitions), with no completeness claim;
3. capability and fidelity gaps;
4. complete execution, Replay, model, wall-time, CPU, and memory cost.

Coverage/PSS never creates a finding. Multiple manifestations of one root cause
count once in a primary benchmark result. Public calibration and private formal
evaluation are distinct datasets. Private variants, triggers, labels, and
root-cause mappings must not enter Agent-visible prompts or memory.

PSS is frozen as an offline exploration metric for the current phase: do not
expand its vocabulary or optimize the main search loop around it without an
effect experiment showing that the change matters.

## External services and secrets

- Never read or print credentials unless the user explicitly authorizes the
  corresponding model experiment.
- Model calls are opt-in, bounded, journaled, separately costed, and use only
  the authorized provider/material scope.
- Do not call a model API to validate deterministic code.
- Never store credentials in prompts, process arguments, artifacts, or Git.

## Editing and Git hygiene

- Preserve pre-existing user changes; inspect `git status` before editing.
- Use `rg` for discovery and `apply_patch` for source/document edits.
- Treat `/home/nitro/Desktop/raft` as read-only unless scope is changed.
- Do not commit, push, publish, or run paid services unless requested.
- Generated build caches stay outside Git and should preferably use `/tmp`.
- Historical experiment output must not become a normal unit-test dependency.
- Retain only research artifacts required to understand or reproduce a current
  conclusion; Git history holds retired stage narratives.

## Validation and handoff

Run checks in proportion to risk. A completed implementation stage normally
requires:

```text
gofmt on changed Go files
go test ./...
go vet ./...
make audit-no-v1 audit-race-shards
git diff --check
```

Use focused race tests for affected concurrent paths. Run the expensive full
race suite at release/evaluation milestones; record a stable timeout instead of
repeating it indefinitely. If a check cannot run, report the exact reason.

Every handoff states what was preserved, what changed, what is proven, what is
not proven, and the next concrete decision-producing step.

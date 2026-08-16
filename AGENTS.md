# ConsensusAtlas Repository Instructions

This file applies to the entire repository. It records durable project and
development constraints. Current progress, temporary experiments, and branch
state belong in stage documents, not here.

## Read first

Before making changes, read the relevant parts of:

1. `docs/ConsensusAtlas-总体规划.md` for project direction and frozen decisions;
2. the current `docs/stage-*.md` or `docs/CURRENT_STAGE.md`, when present;
3. the package documentation and tests for the component being changed.

If these documents disagree, preserve the stronger trust, reproducibility,
and protocol-independence constraint and report the conflict.

## Project objective

ConsensusAtlas is an Agentic testing system for CFT and limited-BFT consensus
implementations. Protocol-aware Agents should own hypothesis generation,
semantic test planning, bounded search guidance, and feedback-driven repair;
the deterministic control layer owns what can execute, Replay owns
reproducibility, and independent Oracles/evaluators own accepted conclusions.
The system must not become tied to one Raft implementation.

The long-term structure is:

```text
minimal protocol knowledge + thin Adapter
                    |
                    v
Protocol/Hypothesis Agent -> Explorer Agent
                    ^                |
                    |                v
         mechanical feedback + bounded proposal
                    ^                |
                    |                v
          deterministic Runtime / semantic projection
                    |                \
                    |                 +-> Replay / Oracle / evaluator
                    v                              |
          semantic test progress        terminal private outcome
```

Maximize the work that Agents can propose or automate, but minimize their
authority over accepted evidence and conclusions. Do not turn this principle
into minimal Agent influence: Agents may propose uncertain hypotheses, choose
semantic objectives, rank global WorkItems, select frozen search operators,
and revise plans after rejection. Invalid proposals are expected test-planning
outcomes; they are not trusted facts and must be rejected or concretized by
deterministic code.

## Trust boundary

The following are trusted deterministic components and must not delegate their
decisions to an LLM:

- Protocol/Profile identity and canonical digests;
- Test Plan validation and concretization;
- Runtime event execution and message ownership;
- Replay and conformance checking;
- evidence matching and Coverage Ledger updates;
- PSS projection and canonical state keys;
- Oracle execution;
- defect-trial identity, budget validation, and result aggregation.

Agents may propose protocol mappings, Driver code, witnesses, obligations,
bounded plans, and explanations. They may not:

- modify the active Profile denominator, weights, Oracle, or equivalence rules;
- report their own observation as trusted evidence;
- write directly to Coverage or Defect ledgers;
- weaken replay, conformance, identity, or evidence requirements;
- classify a protocol result solely through natural-language agreement among
  Agents.

For Agentic episodes, keep hypothesis, execution, and verdict authority
separate:

- `TestHypothesis` may name semantic targets, prerequisites, a fault-boundary
  reference, and rationale. It must not contain an executable Oracle, expected
  bug/verdict, trusted assertion, concrete Action sequence, or future absolute
  decision number.
- `SemanticExplorerProposal` may only order the current trusted semantic queue;
  `ScenarioInvestigationProposal` may select coordinator-owned branch checkpoints,
  and its nested `ScenarioPlan` may only use current or mechanically exposed selectors. The
  trusted search input owns the backend, root, fault envelope, total budget,
  and stop rule.
- Explorer/Scenario feedback may expose only pre-registered mechanical
  rejection, prefix-bound progress, cost, and non-verdict public activation fields.
  Candidate/control identities, private monitor evidence, Oracle verdicts,
  known triggers, and root-cause mappings are evaluator-only terminal data.
- Do not add a generic Blackboard, scoring DSL, operator catalog, budget DSL,
  stop DSL, compatibility Episode contract, or second Ledger.

## Evaluation rules

Keep these questions separate:

1. **External effectiveness**: independent root-cause detection on evaluation
   samples not shown to the planning method, plus false positives on correct
   controls. This is the primary method-level result.
2. **Fixed Profile coverage**: strong evidence obtained for a frozen,
   versioned obligation denominator. This explains what was tested.
3. **PSS discovery**: unique semantic states/transitions found under a common
   budget. This explains search behavior and has no completeness denominator.

Coverage or PSS values must never directly create a defect-kill result. A kill
requires trusted, replay-stable, conformant Oracle evidence from a real trace.
Multiple manifestations of the same cause count once in the primary result.

Do not describe any score as protocol correctness probability, remaining bug
probability, or proof of asynchronous liveness.

## Candidate qualification

A versioned candidate catalog may describe historical implementation behavior,
protocol-level calibration variants, and correct controls. Qualification must
be a pure deterministic comparison between typed requirements and a trusted
capability snapshot.

The trusted capability snapshot must distinguish at least:

- controllable protocol inputs;
- observable Runtime/semantic events;
- Driver capabilities;
- registered trusted monitors;
- classifiable execution outcomes;
- relevant Profile bounds and identities.

Important: **do not infer controllable inputs from Coverage obligation event
types**. An obligation may observe `message`, `persist`, `sync`, `emit`, or
`apply` even though none is a legal direct test input. Profile obligations
restrict evaluation scope but do not prove input availability.

Qualification rules must:

- use stable typed requirement and reason codes;
- sort missing requirements deterministically;
- produce `qualified` only when every required set is satisfied;
- otherwise produce `deferred` with explicit missing requirements;
- avoid protocol-specific conditionals in the generic evaluator;
- keep etcd/raft catalog composition outside generic Runtime packages;
- never relax a requirement merely to increase the number of usable samples.

Source references and curator notes are metadata, not executable proof of
qualification. Network access must not be required during qualification.

## Evaluation data separation

Public development/calibration samples and formal evaluation samples are
different datasets.

- Public calibration samples may be committed when they test the build and
  evaluation pipeline. They must be labelled calibration and cannot support a
  formal holdout claim.
- Formal evaluation identities, exact source transformations, known triggers,
  and root-cause mappings must not be included in Agent prompts or public
  planning inputs before the experiment is frozen and completed.
- Pre-register one source-exposure mode for the full method matrix. The primary
  hidden evaluation uses `opaque-source`: every arm sees the same official/base
  source, never the trial variant. A separate `code-aware-symmetric` study may
  expose the current SUT source to every arm, but not the paired source, diff,
  transformation metadata, label, stable variant identity, or cross-trial
  private memory. Never mix modes across methods or candidate/control.
- Formal online feedback must be variant-blind and field-allowlisted. Private
  pass/fail labels are terminal evaluator output, not planner feedback.
- Development, calibration, and holdout results must be reported separately.
- A newly found upstream issue is a bonus case study, not a required project
  outcome.

## Architecture and dependency direction

The trusted core is written in Go. Python may orchestrate experiments and
model clients but must not own Runtime execution, Oracle decisions, or scoring.

Generic packages under `internal/` must not import a concrete consensus
implementation. In particular:

- etcd/raft types stay in `adapters/etcdraftv2`, target-owned semantic
  mappings, or explicit CLI composition roots;
- `internal/control`, `internal/controlruntime`, `internal/controlexperiment`,
  `internal/semantic`, `internal/oracle`, and `internal/defectbench` remain
  protocol-neutral;
- an Adapter calls official implementation APIs and translates outputs, but does
  not schedule, transport, drop, duplicate, or reorder messages;
- unsupported behavior is explicit in the Driver Manifest and remains visible
  in evaluation.

Prefer the official unmodified implementation for controls. A calibration
build that changes implementation behavior must leave Driver, Runtime,
Profile, Test Plan, Replay, and Oracle code unchanged, and must have a distinct
SUT build identity.

## Runtime invariants

Preserve the following distinctions:

- produced, released, and delivered/dropped messages;
- visible storage, durable storage, applied state, and network mailbox;
- setup/prepare work, primary measurement work, and replay work;
- exact execution fingerprint, semantic state key, and coverage evidence.

Messages must be frozen before release and remain Runtime-owned until terminal
delivery or drop. Crash/restart must not silently delete released network
messages or restore volatile process state as durable state.

Campaign comparisons use complete primary work as the common logical budget.
Repeated fresh-SUT construction and setup/prepare work are not free. Runs,
scheduler decisions, replay work, wall time, CPU, memory, and model tokens are
reported separately.

## Source-variation build rules

When building a public calibration version of a dependency:

- do not modify the Go module cache or another checkout in place;
- bind the module path/version and original source digest;
- require each declared source transformation to match exactly once;
- verify the transformed source digest;
- use an explicit build-command allowlist;
- use readonly, locally available dependencies;
- assign an opaque, digest-bound SUT build identity;
- record input, transformed-source, binary, command, and toolchain identities;
- write generated sources and build products only to a dedicated temporary or
  artifact directory.

Runtime generation of temporary build files by the implementation under test
is allowed. Repository source edits made by Codex must still use `apply_patch`.

## Secrets and external services

- Never read, print, copy, or commit `key.txt` unless the user explicitly asks
  to run the corresponding model experiment.
- Do not call a model API merely to validate deterministic code.
- Model calls must remain opt-in, bounded, and separately costed.
- Do not put credentials in prompts, process arguments, logs, artifacts, or
  Git history.
- Prefer local repository history and official primary sources when checking
  implementation behavior.

## Editing and Git hygiene

- Preserve all pre-existing changes unless the user explicitly asks to remove
  them.
- Inspect `git status` before editing and work incrementally on the dirty tree.
- Use `rg`/`rg --files` for discovery.
- Use `apply_patch` for repository source and documentation edits.
- Do not run destructive Git or filesystem commands.
- Do not modify `/home/nitro/Desktop/raft`; treat it as read-only reference
  material unless the user explicitly changes its scope.
- Do not commit, push, open a PR, or call a paid/model service unless requested.
- Do not regenerate historical artifacts just because a report schema gained
  additive fields. Preserve their documented version identity.

## Documentation

Material changes must update the relevant design or stage document. Keep these
two files byte-identical whenever the overall plan changes:

- `/home/nitro/Desktop/ConsensusAtlas-总体规划.md`
- `docs/ConsensusAtlas-总体规划.md`

Stage summaries must explicitly separate:

- what the implementation and experiment proved;
- what remains unsupported or deferred;
- what was only a public calibration result;
- what the next minimal decision-producing experiment is.

The A0 plan admits Protocol/Hypothesis and Explorer as the first two cognitive
roles. Avoid adding a third Agent, new PSS dimensions, obligation language, or
generic abstraction layers without a concrete evaluation miss that requires
the added complexity. Always compare the two-role method with a single Agent
under the same total model and Runtime budget.

## Required validation

Run validation in proportion to the change. Before handing off a completed
stage, normally run:

```text
gofmt on changed Go files
go test ./...
go vet ./...
go test -race ./...
python3 -m unittest discover -s agents -p 'test_*.py'
JSON syntax/schema checks for changed JSON artifacts
git diff --check
```

If a check cannot run, report the exact reason. Passing tests establish the
implemented boundary; they do not establish that an Agent outperforms a
baseline or that the target protocol is correct.

## Handoff discipline

Do not store fast-changing progress in this file. A new window should inspect
the worktree and current stage document, then state:

1. which completed work it is preserving;
2. the current stage objective;
3. the next deterministic deliverable;
4. any capability or evidence boundary that prevents a broader claim.

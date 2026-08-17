# M4e DeepSeek OmniPaxos Scenario-to-Action calibration

This is the first paid single-Episode calibration of Scenario prompt v12 and
method implementation `m4d-v1`. It is a pipeline calibration, not a defect
finding or method-comparison result.

## Authorized input boundary

The run used the ProtocolKnowledgePack, Target Dossier and target surface from
`plans/agent/omnipaxos-agentic-calibration-v1.json`. Source navigation was
physically isolated to exactly these four previously authorized files:

- `adapters/omnipaxosv2/adapter.go`
- `adapters/omnipaxosv2/model.go`
- `adapters/omnipaxosv2/worker.go`
- `adapters/omnipaxosv2/worker/src/main.rs`

The Risk response returned a portfolio immediately with an empty
`knowledge_requests` array, so no source snippet was selected or sent during
this Episode. There was no retry, provider fallback or second Episode.

## Result

| Item | Result |
|---|---:|
| Provider/model | DeepSeek official / `deepseek-v4-flash` |
| Risk calls | 1 |
| Scenario calls | 3 |
| Model tokens | 105,334 |
| Scenario decisions | 8 |
| Final Trace records | 31 |
| Execution primary/replay work | 33 / 33 |
| PSS protocol/control/joint | 6 / 28 / 29 |
| PSS samples | 32 |
| Oracle findings | 0 |

The terminal artifact is `completed`, contains a V3 Bundle, and can be loaded
again with `-campaign-resume` without another provider call. This satisfies the
M4e minimum success condition: a real Scenario response crossed the typed
contract, selected Runtime Actions, produced PSS and Oracle evidence, and
completed fresh Replay.

The accepted hypothesis was `timer-symmetry-recovery-lapse`. It was not
reached. All three Scenario proposals entered trusted execution, then stopped
respectively with `milestone-unreachable`, `no-match`, and `ambiguous`. The
terminal assessment is `search-budget-exhausted`; its first missing milestone
is `client-invoked`. Therefore this run establishes no OmniPaxos issue.

## Concrete follow-up evidence

The first predicate binds both `participant` and `participant-node` under the
same name `n1`. The active observation semantics encode those fields as
`n1@1` and `n1`, respectively, so the first predicate cannot match even though
the saved Trace contains the client Invoke. This is a concrete Risk-contract
failure, not an absent Runtime event. A following stage should expose binding
domains to the Agent and reject reuse across incompatible domains before
Scenario execution.

The later `no-match` and `ambiguous` outcomes also show that valid JSON and
valid intent shape are not sufficient for robust Scenario execution. They
should drive improvement of existing selector feedback; they do not justify a
new Action DSL or protocol-specific ActionKind.

The embedded portable qualification profile records six validated and three
explicitly unsupported capabilities, with the overall profile not qualified.
This calibration only exercises the admitted OmniPaxos target surface and must
not be presented as production-process or durable-restart coverage.

# M4c DeepSeek official OmniPaxos calibration v1

This is a public capability calibration artifact, not a protocol verdict or a
formal holdout result.

## Frozen scope

- Provider: DeepSeek official API
- Model: `deepseek-v4-flash`
- Target: `omnipaxos-v2`
- Episodes requested: 1
- Model budget: at most 6 calls and 120,000 observed tokens
- Source mount: an isolated read-only view containing only:
  - `adapters/omnipaxosv2/adapter.go`
  - `adapters/omnipaxosv2/worker.go`
  - `adapters/omnipaxosv2/model.go`
  - `adapters/omnipaxosv2/worker/src/main.rs`

The Agent did not request a source excerpt in this Episode.

## Result

The first Risk call completed in 155,811 ms with 32,201 observed tokens
(11,776 input and 20,425 output). It returned three candidates. Trusted local
qualification selected `timer-symmetry-recovery-lapse`, concerning bounded
coordinator recovery after one message drop and a natural periodic pulse.

The first Scenario call completed in 138,963 ms with 24,918 observed tokens
(8,248 input and 16,670 output). Its four-step proposal was not admitted: two
semantic selector fields used structured node identities where the typed
Scenario schema requires node ID strings. The local validator recorded
`proposal-invalid` and requested a repair.

The repair call was dispatched with less than five seconds left in the
300,000 ms Episode wall-clock allowance. It was cancelled by the real Episode
deadline after 4,913 ms and is recorded as `agent-transport-ambiguous` with
unknown provider usage. It must not be treated as a zero-token call or retried
implicitly.

Observed, reconciled usage before the ambiguous repair was 57,119 tokens over
two calls. No Scenario Action was admitted or executed, so this artifact has no
PSS, Replay, Oracle, or finding result and no `summary.json`.

## Interpretation

The calibration establishes official-provider connectivity, exact durable
call evidence, local schema rejection, and real Episode cancellation. It does
not establish Agent effectiveness or an OmniPaxos defect. The 300-second
Episode allowance was too small for a successful response plus one repair;
the next MethodSpec uses a 1,200,000 ms Episode wall-clock while retaining the
same model-call and token limits.

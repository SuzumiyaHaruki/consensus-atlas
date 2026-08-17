# M4c DeepSeek official OmniPaxos calibration v2

This is a public capability calibration artifact, not a protocol verdict or a
formal holdout result.

## Scope

- DeepSeek official `deepseek-v4-flash`
- one fresh `omnipaxos-v2` Episode
- at most 6 model calls and 120,000 observed tokens
- 1,200,000 ms Episode wall-clock
- the same isolated read-only four-file source view used by v1

The Agent did not request a source excerpt in this Episode.

## Result

The Episode completed and is independently recoverable:

- Risk calls: 1
- Scenario calls: 3
- model usage: 116,912 tokens (36,737 input and 80,175 output)
- selected candidate: `client-op-loss-recovery`
- Scenario decisions used: 6
- final Trace records: 29
- qualified primary/replay decisions: 29/29
- protocol/control/joint PSS: 4/27/28
- Oracle findings: 0

The first Scenario proposal copied structured node identities into selector
fields that require node ID strings and was rejected locally. The second
proposal fixed those fields but used `continue` after stopped feedback, so it
was also rejected. The third proposal used `revise`, was admitted, executed,
and fresh-replayed.

Risk was not reached. The first missing milestone was `request-at-leader`, and
the evidence assessment is `search-budget-exhausted` because all three
Scenario calls were consumed. The final plan placed
`after_milestone=request-at-leader` on the Invoke step that would create that
milestone, making the plan causally self-blocking.

## Interpretation

This run establishes a complete official-provider path from Risk generation
through typed repair, deterministic execution, PSS projection, fresh Replay,
Oracle evaluation, artifact recovery, and cost accounting. It does not show a
defect or that the Agent reached its chosen hypothesis. The next prompt version
states node selector scalar types, stopped-feedback intent, and
`after_milestone` pre-step semantics explicitly before moving to a two-Episode
investigation.

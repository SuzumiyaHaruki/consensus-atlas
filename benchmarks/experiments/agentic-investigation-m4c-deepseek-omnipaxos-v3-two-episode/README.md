# M4c DeepSeek official OmniPaxos two-Episode attempt v3

This paid public calibration terminated during Episode 1. Episode 2 did not
start, and the directory is not a completed Investigation artifact.

## Result

- requested Episodes: 2
- completed Episodes: 0
- Risk call: 30,769 tokens
- Scenario call 1: 25,629 tokens
- Scenario call 2: 24,641 tokens
- Scenario call 3: 40,110 tokens
- total observed usage: 121,149 tokens

The first two Scenario calls returned complete responses. The third consumed
the full 32,000 output-token allowance and did not finish with `stop`; the
transport therefore recorded `agent-response-rejected`. Provider usage was
observed, so this is not a billing ambiguity. No terminal `summary.json`,
Bundle, Replay, PSS, or Oracle result was produced.

## Correction

The failure demonstrates that Risk and Scenario should not share one high
thinking transport. Risk investigation remains `thinking=high` with a 32,000
output allowance. Scenario planning now uses an independently MethodSpec-bound
transport with `thinking=disabled` and a 16,000 output allowance. Episode call
and total-token budgets, durable journal semantics, Runtime, Replay, and Oracle
are unchanged.

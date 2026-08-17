# M4h DeepSeek OmniPaxos semantic-selector calibration

This is one paid DeepSeek-official Episode using Scenario prompt v13 and
structured output v5. It compares the two concrete M4e failures after the M4f
binding-domain and M4g semantic-selector changes. It is not a defect finding or
a method-comparison result.

## Input and provider boundary

The run used `plans/agent/omnipaxos-agentic-calibration-v1.json`, provider
`deepseek`, model `deepseek-v4-flash`, high thinking, no provider fallback and
no transport retry. It had a six-call/120,000-token/64-Runtime-decision Episode
limit and ran exactly one Episode.

The read-only source mount contained exactly the four previously authorized
OmniPaxos files:

- `adapters/omnipaxosv2/adapter.go`
- `adapters/omnipaxosv2/model.go`
- `adapters/omnipaxosv2/worker.go`
- `adapters/omnipaxosv2/worker/src/main.rs`

The Risk Agent returned an accepted portfolio on its first call, so it made no
source-window request and no source content was sent in this Episode.

## Result

| Item | Result |
|---|---:|
| Risk / Scenario calls | 1 / 3 |
| Model tokens | 97,522 |
| Scenario decisions | 16 |
| Final Trace / Replay decisions | 39 / 39 |
| Execution primary / replay work | 41 / 41 |
| PSS protocol / control / joint | 4 / 37 / 38 |
| PSS samples | 40 |
| Oracle findings | 0 |

The accepted hypothesis was `recovery-after-single-drop`. Its predicates used
node-ID bindings consistently, so the M4e cross-domain binding failure did not
recur. All three Scenario proposals crossed the typed contract and entered
trusted execution. None stopped with `no-match` or `ambiguous`; each stopped
with `milestone-unreachable`. The final trace contains one Invoke, one message
drop, six natural timer firings and 31 message deliveries.

The Risk was not reached. The first missing milestone was `leader-handoff`, and
the terminal assessment was `search-budget-exhausted` because all three
Scenario calls were consumed. Therefore the Episode establishes no OmniPaxos
issue. It does provide positive calibration evidence that the binding-domain
repair and frontier selector path removed the two M4e failure classes. The next
work should improve feedback-driven progress toward the missing coordinator
transition, not add another Action DSL or enlarge the number of Episodes.

The terminal Bundle has identical primary and fresh-Replay trace digests. An
offline `-campaign-resume` recovered the same 4 calls, 97,522 tokens and 39-step
execution without another provider call.

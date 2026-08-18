# M4l2 DeepSeek OmniPaxos family-vs-leaf calibration v2-r1

## Status

This paired Scenario calibration completed successfully. It is evidence that
Target-local leaf message types can remove a concrete planning ambiguity under
the existing public message Actions. It is not evidence of Risk-Agent quality,
a protocol finding, or general effectiveness across protocols.

## Fixed method

- target: `crates.io/omnipaxos@0.2.2` through the existing worker Adapter;
- provider/model: DeepSeek official, `deepseek-v4-flash`;
- order: family, then leaf;
- one fixed AcceptedHypothesis and one fixed natural root;
- no Risk-Agent call, source navigation, route selector, or exact ActionID;
- the Scenario output selector contains only `kind + message_type_hint`;
- both arms share the same budget and differ only in
  `scenario_message_view=family|leaf`.

The root contains 23 natural decisions and no Duplicate. Its two relevant
pending messages have distinct leaf types, including the target
`sequence-paxos/prepare`, but both project to `sequence-paxos` in the family
arm. Mechanical cardinality is therefore family=2 and leaf=1.

## Result

| Metric | family | leaf |
|---|---:|---:|
| first proposal | `ambiguous` | `correct-unique` |
| Scenario calls | 3 | 1 |
| repair calls | 2 | 0 |
| ambiguous proposals | 3 | 0 |
| target Drop executed | no | yes |
| Runtime decisions | 0 | 23 |
| total tokens | 58,857 | 8,950 |
| qualified Risk reached | no | yes |
| Bundle produced | no | yes |
| fresh Replay stable | no execution | yes |
| Oracle violations | 0 | 0 |

All three family proposals used exactly `kind + message_type_hint` and matched
the same two projected Actions. Feedback could not reconstruct a leaf type that
the view did not expose. The leaf proposal used the same selector fields,
matched one Action on its first call, and completed deterministic execution.

The leaf Trace digest is:

```text
5d1f74623a295c085a2622d4957841e5a6f82b80753bf9f99aa08ddb6b59e0a1
```

The qualified path checked `trace-integrity` and `agreement`, reported no
violation, and reproduced exactly through `-campaign-resume` without another
provider dispatch. Raw model content is absent from arm/pair summaries and is
retained only by the durable provider journal.

## Interpretation boundary

This one deliberately constrained sample activates the intended independent
variable and provides positive evidence for leaf message vocabulary in the
second Agent. The token difference is descriptive: the family arm exhausted
all three attempts, while the leaf arm stopped after one. It must not be treated
as a general 85% efficiency claim from one ordered pair.

The earlier sibling directory ending in `-v2` is an excluded infrastructure
attempt made without external network permission. Both transports were recorded
as ambiguous with provider usage `unknown` and zero observed tokens; it is not
part of this paired result and must not be merged into its metrics.

Primary artifacts are `summary.json`, `ground-truth.json`,
`scripted-preflight.json`, each arm's `method-spec.json`, `arm-summary.json`,
and `provider-audits.json`. The large leaf Bundle remains reproducible from the
recorded method and is intentionally not retained in HEAD.

# A9d6 real Agentic Episode calibration

Date: 2026-08-15

This directory records the first real OpenRouter calibration of the common
`agentic-episode-v1` entry on both supported targets. The configured model was
`deepseek/deepseek-v4-flash`. Credentials are not stored in these artifacts.

## Results

| Directory | Target | Result | Model work | Interpretation |
|---|---|---|---:|---|
| `omnipaxos-v2/` | OmniPaxos | Risk candidate rejected | 1 call, 4,197 tokens | The model created one-use bindings. This exposed a prompt/feedback gap. |
| `omnipaxos-v2-r2/` | OmniPaxos | Episode completed | 2 calls, 9,863 tokens | Candidate accepted; Scenario executed; Replay stable; 31 PSS samples / 29 unique states; Risk not reached; Oracle findings 0. |
| `etcdraft-v2/` | etcd/raft | Provider response rejected | 1 logical call, 3 transport attempts, 0 observed tokens | No complete response was read within the bounded attempts. |
| `etcdraft-v2-r2/` | etcd/raft | Provider response rejected | 1 call, 3,021 tokens | The response stopped at the 2,048-token output limit. |
| `etcdraft-v2-r3/` | etcd/raft | Risk candidate rejected | 1 call, 3,911 tokens | The candidate used the invalid uppercase binding `nodeA`. This exposed a structured-output schema gap. |
| `etcdraft-v2-r4/` | etcd/raft | Provider response rejected | 1 logical call, 3 transport attempts, 0 observed tokens | The interactive process handle was lost while the call was still running. It later wrote this bounded failure. |
| `etcdraft-v2-r4-recovery/` | etcd/raft | Provider response rejected | 1 logical call, 3 transport attempts, 0 observed tokens | Operational duplicate started while the previous process appeared detached; it is not an independent calibration sample. |

The OmniPaxos retry is the positive end-to-end usability result. Its accepted
Risk was `coordinator-change-after-message-loss`; the one-action Scenario did
not reach the complete four-milestone Risk. This is a valid reachability result,
not a finding and not evidence that the Agent is better than a baseline.

The etcd/raft attempts do not provide a completed episode. They provide two
actionable interface findings:

- Risk feedback now distinguishes a single-use binding from an otherwise
  invalid candidate, and the prompt explicitly requires every binding to be
  reused;
- the structured-output schema and prompt now restrict IDs and bindings to
  lowercase letters, digits and hyphens.

No further provider retry was made after the final bounded failures. The
remaining response instability is recorded as an external calibration limit,
not hidden as a protocol or testing failure.

## Evidence boundary

- `summary.json` is compact and does not duplicate the Trace.
- A completed episode stores the full evidence once in `bundle.json`.
- Provider calls retain intent, dispatch and result journals.
- PSS and Risk are explanatory. Only an independent replay-stable Oracle may
  create a formal finding.
- This calibration does not demonstrate a new protocol defect, broad coverage,
  or Agent superiority.

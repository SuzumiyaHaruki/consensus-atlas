# M4c DeepSeek OmniPaxos two-Episode calibration (v4)

This directory records the completed two-Episode calibration that motivated
M4d. It is calibration evidence, not a defect benchmark result.

## Result

| Episode | Accepted hypothesis | Model calls | Model tokens | Runtime decisions | Outcome |
|---|---|---:|---:|---:|---|
| 0001 | `bounded-recovery-drop-coupling` | 4 | 56,373 | 0 | `scenario-agent-stopped` |
| 0002 | `aligned-timer-ballot-oscillation` | 4 | 58,821 | 0 | `scenario-agent-stopped` |

The DeepSeek official transport returned all eight responses and the recorded
usage reconciles to 115,194 tokens. Each Risk call produced three parseable
candidates and one candidate was accepted. All six Scenario calls returned a
decoded `continue` proposal that also supplied `branch_id`, which violated the
conditional Scenario contract before any Runtime Action was selected.

Consequently these artifacts establish provider delivery and substantive Risk
generation, but they establish no protocol execution result: there is no
Scenario Action, PSS sample, qualified Bundle, Replay, Oracle result, or defect
finding in either Episode.

## M4d response

M4d narrows the initial and repair phases to a minimal intent-specific output
shape, reports a stable field-level validation issue together with the allowed
intent set, preserves the decoded proposal for repair, and stores that compact
feedback in the Episode summary. The six real responses are regression inputs.
No OmniPaxos implementation, Adapter behavior, PSS mapping, or Oracle was
changed to make this calibration pass.

# A9e4c4 real OpenRouter OmniPaxos investigation

Date: 2026-08-16

This directory is a two-episode calibration of the resumable Agentic Investigation CLI against the real
OmniPaxos worker. The provider was OpenRouter with `deepseek/deepseek-v4-flash`. The credential is not
stored in these artifacts.

## Configuration change justified by the run

Earlier attempts used a 4,096-token response limit. One response ended at exactly that limit and was
rejected as incomplete. The main Agent limit was therefore aligned with Agora's 32,000-token setting;
the per-episode 50,000-token cumulative budget remains authoritative. A 120-second request timeout and
two retries remain in place.

The first successful Risk response used 5,800 output tokens and the second used 6,980. Neither came
close to the new limit, but both would have been truncated by the previous limit.

## Results

| Episode | Risk candidate | Scenario calls | Risk reached | PSS sample / joint / protocol / control | Oracle | Model work |
|---|---|---:|---|---|---:|---:|
| 1 | old-coordinator accepted-state reuse | 1 | no | 33 / 31 / 4 / 30 | 0 | 2 calls / 16,187 tokens |
| 2 | message-loss and coordinator-change coupling | 3 | no | 34 / 32 / 4 / 31 | 0 | 4 calls / 41,540 tokens |

Total model work was 6 calls and 57,727 tokens. Episode 1's Scenario request timed out once and then
succeeded, so the six successful model calls required seven transport attempts. Primary and fresh Replay
both used 32 decisions in episode 1 and 33 in episode 2.

The second Risk request contains the recovered first-episode Memory: Risk not reached, milestones `m1`
and `m2` reached, `m3` first missing, four protocol states, and the actual model/search/execution cost.
The model changed its preferred candidate and the Scenario Agent used all three repair opportunities.
This demonstrates that the real provider receives feedback and can revise both hypothesis and plan.

The revision did not expand protocol-state coverage: all four protocol PSS states in episode 2 were
already present in episode 1. It added one control-only state. Neither episode reached its Risk witness or
produced an Oracle finding.

## Important negative evidence

Episode 2's prose attributes the suspected mechanism to a dropped accept message, but its accepted
predicate sequence contains only workload invocation, coordinator change, and decision advance. It does
not contain `message-dropped`. Mechanical qualification therefore established that the predicates were
executable and observable, not that they faithfully represented the prose mechanism.

Consequently this calibration proves the multi-round data path, provider recovery, Agent repair loop,
deterministic execution, Replay, PSS accounting, and Oracle handoff. It does not show better exploration,
complete testing, or a consensus defect. Before comparative evaluation, the next calibration should make
hypothesis-to-witness consistency visible to Agent self-review without giving an Agent verdict authority.

The CLI stop reason is `runtime-decision-allowance-reached`. This is conservative accounting: it reserves
64 decisions per episode and reached 128 after two episodes; it must not be reported as 128 decisions
actually executed.

## Follow-up fix

A9e4c5 removes the independent provider-authored `suspected_mechanism` field from new portfolios. The
Agent must now provide exactly one `mechanism_step` for every predicate, in the same order and with the
same milestone ID and Observation kind. Trusted code derives the stored mechanism from those steps.
Missing or mismatched steps produce `risk-candidate-mechanism-unaligned` and return to the existing Risk
repair loop. A regression test replays the failure shape above and verifies that it is rejected before
Scenario execution, then accepted only after the message-drop milestone is added.

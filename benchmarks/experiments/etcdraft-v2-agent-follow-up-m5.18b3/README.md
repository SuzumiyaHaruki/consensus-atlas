# M5.18b3 unseen follow-up baseline

This checked-in directory contains only the small, recomputable feedback/spec/summary boundary. Full reports and
bundles remain under ignored `artifacts/experiments/etcdraft-v2-agent-follow-up-m5.18b3/`.

Both source methods use Experiment v2 semantics, seeds 1/2/3, three attempts and 294/294 primary/replay ceiling.
Each completed two of three workloads and left one pending. The deterministic rule was frozen as
`workload-completion-then-execution-canonical-v2`; the tie therefore selected `action-class-random` by canonical ID.

The unseen seed 4 run consumed 97/97 follow-up work, found 79 Core PSS states and passed strict replay, but did not
exercise every hard Action required by the intent. It is therefore preserved as a charged `execution-failed` result.
No replacement backend or seed was tried, and no model was called. This result is not a defect verdict and does not
rank the two search policies by defect effectiveness.

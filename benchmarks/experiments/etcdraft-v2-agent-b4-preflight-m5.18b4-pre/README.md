# M5.18b4-pre trust-boundary calibration

This directory contains only small, recomputable artifacts. Full report and bundle evidence remains under ignored
`artifacts/experiments/etcdraft-v2-agent-b4-preflight-m5.18b4-pre/`.

The corrected Agent view distinguishes Runtime-supported actions from the actions that current Experiment producers
can actually offer to a backend. Partition/Heal therefore remain visible in the manifest-derived action list but are
absent from backend selectable surfaces and have a zero partition envelope.

The frozen seed-4 execution is valid and strict-replay stable, but it does not reach the hard Action precondition
because `invoke` is absent. It is recorded as `intent=not-reached`, not as an execution failure. Oracle status is
`not-evaluated`; this public calibration is not a defect verdict, a RiskWitness, or an Agent-effectiveness result.

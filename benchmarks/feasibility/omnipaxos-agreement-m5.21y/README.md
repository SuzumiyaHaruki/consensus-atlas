# OmniPaxos M5.21y Agreement activation

This artifact records a bounded Oracle activation, not a real protocol defect,
coverage score, or target Qualification.

The unmodified OmniPaxos 0.2.2 worker now exposes each node's exact decided
index and a cumulative SHA-256 digest of the complete decided prefix. A
target-owned `DecisionProjector` maps only participant, exact position, and
digest into the existing protocol-neutral Agreement monitor.

One real 35-decision control run produced two independent observations at
position 1 with the same digest. Agreement reported no violation, and a fresh
worker replay reproduced the exact trace and projected observations. A clearly
labelled calibration copy changed one node's digest at the same position;
Agreement then reported exactly one violation. The calibration is not an
OmniPaxos execution and is not counted as a discovered defect.

Run from the repository root:

```sh
make test-omnipaxos-binding
```

No public Runtime, semantic observation, Agreement monitor, Core PSS schema,
or upstream OmniPaxos source changed. Restart/durability, liveness, Agent
search, coverage-quality correlation, Qualification, and a real candidate /
control defect evaluation remain absent.

# OmniPaxos M5.21w opaque workload slice

This artifact records the first input-to-result path for the current
OmniPaxos Binding. It remains a feasibility result, not Qualification.

After the common Runtime naturally elects n1, it offers one opaque request to
non-leader n2. The unmodified OmniPaxos core forwards and decides the entry
using only scheduler-controlled complete messages and logical ticks. The
target Adapter emits one ClientResult only when the entry's declared origin
n2 observes the exact decided request/value. A second fresh worker process
strictly replays all 35 decisions and the same result.

Run the current control and workload regressions from the repository root:

```sh
make test-omnipaxos-binding
```

The workload added 178 production lines over the M5.21v 770-line baseline,
after a 15-line contraction, for a 948-line total. No shared Action, Runtime,
PSS, experiment code, or upstream OmniPaxos source changed.

The synchronous `MemoryStorage` path is still internal to the worker. This
artifact provides no crash/restart, durable HostEffect, PSS Mapping,
RiskWitness, Oracle, Agent, Campaign, or Qualification evidence, and it does
not validate a leaderless/dependency-graph protocol.

# raft-rs M5.21t opaque workload slice

Run the current gate with:

```text
make test-raftrs-binding
```

The new test reaches a naturally elected Leader, offers one versioned opaque input through the common
Runtime, lets raft-rs replicate and commit it only through existing Ready/message actions, emits one
ClientResult at the original participant, and strictly replays the complete trace in a fresh worker.

This is a direct Runtime input-to-result witness. It is not a WorkloadRouter, Experiment admission,
PSS/Oracle mapping, crash/restart model, target Qualification, or cross-protocol result.

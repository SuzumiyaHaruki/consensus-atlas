# raft-rs M5.21s Binding spike

This checked-in result records a bounded integration feasibility test, not a target qualification.

The unmodified `raft = 0.7.0` core runs in a target-owned Rust worker. The Go Adapter translates only
the existing Control Runtime abstractions for periodic temporal pulses, Ready completion effects, and
peer-message delivery/drop. The worker executes exactly one requested operation and has no search or
scheduling policy.

The test drove a three-node natural election through the common Runtime, saved its complete in-memory
trace, closed the first worker, started a second worker process, and called the common strict Replay path.
This directory freezes the pre-contraction M5.21s result. The current executable gate and current BuildID
are recorded by the successor `raft-rs-binding-m5.21sr` artifact.

The trace digest in `summary.json` identifies the audited local worker binary and toolchain represented
by its manifest BuildID. A rebuild on another toolchain may legitimately produce a different binary and
therefore a different manifest/trace digest; replay within one declared BuildID must remain exact.

This result does not provide crash/restart, workload input, PSS mapping, RiskWitness, Oracle, Agent,
campaign, or Qualification evidence. It also does not audit raft-rs internal RNG draws; the tested
configuration uses single-value election ranges so those draws cannot change this trace.

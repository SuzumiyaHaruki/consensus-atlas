# raft-rs M5.21sR Binding contraction

This directory freezes the pre-workload contraction result. The current executable gate and current
BuildID are recorded by the successor `raft-rs-workload-m5.21t` artifact.

It checks Rust formatting and Clippy, runs the three-node natural-election trace, closes the first worker,
strictly replays through a fresh worker process, verifies typed rejection of an unsupported worker command,
and verifies that an abnormal worker exit is surfaced to the Go boundary.

The contraction removes duplicate target-owned state without moving it into a nominally shared package or
weakening Adapter eligibility, BuildID, evidence, Ready identity, or worker process checks. It remains a
Binding feasibility result, not Qualification or cross-protocol evidence.

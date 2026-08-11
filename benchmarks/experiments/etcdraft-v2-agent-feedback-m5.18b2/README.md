# M5.18b2 defect-blind Agent batch feedback

This public experiment mechanically reprojects two real etcd/raft backend methods under the same ceiling:

- 3 execution attempts;
- 294 primary work units;
- 294 replay work units;
- policy seeds fixed in advance to 1, 2 and 3.

`action-class-random` completed two executions and preserved one charged seed-3 workload failure. It used all 294
primary work units, replayed the two completed executions for 196 units, and exposed 194 PSS samples / 165 unique
states. No replacement seed was selected.

`admissible-uniform` completed all three executions, used 294/294 primary/replay work, and exposed 291 samples / 238
unique states.

The checked-in `feedback.json` is the exact Agent-visible projection. It intentionally omits bundle, trace, manifest,
qualification, build, state-key, Oracle, candidate and root-cause fields. The trusted constructor validates each full
ignored bundle with the bound PSS mapper before producing this view.

This is a public feedback-pipeline calibration. It does not show that an LLM uses the feedback well, that uniform is
better at finding defects, that 238 states are complete, or that either backend is correct. No model call was made.

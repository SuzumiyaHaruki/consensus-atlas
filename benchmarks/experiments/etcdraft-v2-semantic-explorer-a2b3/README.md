# etcd/raft A2b3 public semantic Explorer calibration

This directory freezes the public input boundary used by the A2b3 real-model calibration.

- Target: official etcd/raft v2 Adapter, three nodes.
- Source: the already frozen M5.23e official-source bundle and root corpus.
- Root: `invoked`, selected before planning at exactly 28 decisions.
- Risk: Raft `leader-change-with-inflight-proposal` family witness.
- Search: depth 2, 16 total WorkItems, deterministic semantic baseline versus one restricted Explorer.
- Explorer allowance: at most 2 calls, 16,000 reported tokens, temperature 0, no retry.
- Mutable output: only a complete permutation of the current semantic Candidate IDs.

`spec.json` is small, source-bound preregistration evidence. It contains no credential, model output, Trace body,
candidate/control variant, Oracle verdict, or defect label.

The explicitly authorized A2b3b call completed on 2026-08-13. DeepSeek v4 Flash returned one valid proposal on the
first call, using 3,309 reported tokens. It moved the first expansion from message delivery to crashing `n1`, while
both the baseline and Explorer reached zero complete risk candidates under the frozen depth/work-item bound. The
large exact call artifact remains under ignored `artifacts/`; the non-sensitive result, digests, resume check, and
limitations remain recoverable from Git history at the calibration commit; current boundaries are recorded in
`docs/CURRENT_STAGE.md`.

This remains a public single-sample calibration and cannot support a holdout, superiority, coverage-completeness, or
correctness claim.

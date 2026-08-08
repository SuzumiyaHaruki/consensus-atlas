# etcd/raft v2 M5.18a evaluator-owned calibration

This public calibration freezes the minimum evidence boundary required before
testing an Agent method. It is not a private holdout and does not compare
search methods.

## Reproduction

From a clean clone with the required module already available locally:

```bash
make build-etcdraft-v2-method-evaluation
make evaluate-etcdraft-v2-method-evaluation
```

The builder refuses to overwrite an existing binary. Full reports, binaries,
and approximately 1.94 MB bundles are written under ignored `artifacts/`.
This directory keeps only the small build inputs/audits, frozen MethodSpec,
benchmark manifest, and evaluator report.

## Frozen inputs

- MethodSpec: `ee856fbf62ff99a82cfc0c2753d3021e6ca879e6490242074b08ea28d48fdbcf`
- benchmark manifest: `0bf5a38fcf1e61d108860910aaa0a10f10c08503f0ed4f88280bfe04446bbce9`
- control build audit: `b4c2ddbe2b0a0afc34ee981eac99d36c1494f5329fc6f8a748e29d7dd447846a`
- candidate build audit: `439d4d7b44b3786f5444d1946df1b2d7609f157087fd5323d293d33ebed4df21`

The control uses the official unmodified `go.etcd.io/raft/v3@v3.6.0` module.
The candidate is the existing public command-data-divergence calibration, not
an undisclosed historical defect.

## Result

Both evaluator-owned runs used the same MethodSpec, non-SUT config projection,
operation history, 96 decisions, and 98/98 primary/replay work. The control
passed. Agreement found the candidate's conflicting applied value at step 55.
The report contains one killed calibration root cause, zero false positives,
and zero invalid trials.

OperationHistory alone did not distinguish the pair: both invoked at step 28,
returned `committed` at step 42, and have the same history digest. The kill
comes from the target-owned applied-prefix projection plus the generic
Agreement monitor. Coverage and PSS do not participate in the verdict.

Evaluator report digest:
`4cac6cd0731bdb9d28b2c1d927d634e3ce0dfdc2ccfac271f55df0b5bd7ef4ee`.

## What this does not prove

- no method or Agent outperforms a baseline;
- no private holdout was evaluated;
- a single-execution MethodSpec is not yet a multi-attempt search-method submission;
- no generic durability Oracle was added, because this calibration did not
  expose a durability-evidence gap;
- etcd/raft and ConsensusAtlas are not proven correct or complete.

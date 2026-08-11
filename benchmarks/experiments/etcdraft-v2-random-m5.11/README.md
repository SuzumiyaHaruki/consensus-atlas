# etcd/raft v2 deterministic Random baseline — M5.11

This is a public development baseline, not a holdout result, an Oracle result,
or evidence that one search method is generally superior.

`report.json` contains two fresh three-node etcd/raft runs. Each run has a
32-decision primary budget and a mandatory 32-decision replay against another
fresh Adapter/Runtime. The two per-run policy seeds are deterministically
derived from the public base seed `1`; they are part of the Policy digest and
are never passed to the Runtime entropy source.

At every decision, `uniform-random-policy/v1` hashes its policy seed, decision
number, and the canonical enabled Action set, then uses rejection sampling to
select an index. The policy cannot read Raft fields or Core PSS state.

Re-run with:

```bash
make experiment-etcdraft-v2-random
```

The result found 29 and 26 states in the individual runs and 53 in their union.
The M5.10 fixed policies found 45 under the same logical budget, but this single
public seed does not establish a method-level advantage. Resource time remains
explicitly uncollected in both reports.

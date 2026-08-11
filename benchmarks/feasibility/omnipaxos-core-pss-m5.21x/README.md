# OmniPaxos M5.21x Core PSS mapping probe

This artifact records a bounded mapping test, not a coverage score or target
Qualification.

The target-owned mapper reads only the existing OmniPaxos Evidence. It maps
participants conservatively to `passive` or `coordinating`, converts complete
ballot tuples and decided indexes to relative ranks, and emits no Value,
contending, applied, or durable semantics that the Evidence cannot support.

One real 35-decision workload produced 36 online samples. A fresh worker replay
produced the exact same sample sequence and discovery ledger. The run contains
35 complete Core states but only 9 semantic graphs; the difference is largely
Runtime pending-message/temporal shape. Neither number is treated as testing
quality, completeness, or protocol-state equivalence.

Run from the repository root:

```sh
make test-omnipaxos-pss
```

No shared Core PSS schema, Control Runtime, worker, or upstream OmniPaxos source
changed. Extended Sequence Paxos semantics, Oracle validation, coverage-quality
correlation, restart/durability, Agent search, and Qualification remain absent.

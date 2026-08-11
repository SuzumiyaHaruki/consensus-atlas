# OmniPaxos M5.21z Portable CFT qualification audit

This directory freezes a full mechanically derived QualificationBundle and a
small reader summary. The 446-line bundle is the validation source of truth:
it contains the unchanged Portable CFT Profile, target Manifest, three fresh
conformance reports, unsupported declarations, findings, and digests. The
summary is not accepted by admission code.

The result is intentionally partial: 3 of 8 required capabilities validate,
3 are explicitly unsupported, and 3 remain unvalidated because the v2 Profile
couples their witnesses to crash/restart. The profile denominator was not
reduced to make OmniPaxos pass.

Existing admission code accepts a named validated subset and rejects the full
Portable set. However, required capabilities are caller-declared and are not
mechanically derived from the experiment Policy/Workload/FaultEnvelope. A
meaningful OmniPaxos run therefore remains inadmissible for trusted comparison
until that under-declaration gap is closed.

Generate a current, toolchain-bound bundle under ignored `artifacts/`:

```sh
make adapter-qualify-omnipaxosv2
```

The frozen BuildID is specific to the audited Rust worker binary. A different
toolchain may produce a different valid current bundle without rewriting this
historical artifact.

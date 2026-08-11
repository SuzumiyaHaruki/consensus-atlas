# M5.18b4 request freeze

This directory contains the small, checked-in commitments created before any
M5.18b4 model call. `freeze.json` binds the exact prompt/request byte digests
for the no-feedback and with-feedback arms, the common seed 4, transport
limits, and per-arm execution budget. `follow-up-spec.json` charges the shared
source construction to each arm; `hard-baseline.json` freezes the identical
risk and `must` fields.

The exact prompt/request bodies are deterministic public data but are kept in
ignored `artifacts/experiments/etcdraft-v2-agent-b4-freeze-m5.18b4/evidence/`
to avoid duplicating roughly 31 KB of JSON. The integration test reconstructs
those bytes and checks every digest in `freeze.json`.

This is a request-freeze result with `model_calls = 0`. It is not an Agent
result, an execution result, a defect verdict, or a RiskWitness.

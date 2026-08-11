# OmniPaxos M5.21zR qualification and admission closure

This directory freezes the final M5.21 QualificationBundle and a compact
reader summary. The unchanged v2 profile and M5.21z bundle remain available in
the sibling directory; zR introduces `portable-cft-control-v3` instead of
rewriting their identities.

V3 keeps the same 8-required strict denominator but gives natural temporal,
runtime-owned message, and strict replay separate protocol-neutral witnesses.
OmniPaxos consequently validates six required capabilities while lifecycle and
audited entropy remain unsupported. The target is still not fully portable.

Experiment admission now derives a capability lower bound from the exact
Policy, Workload, FaultEnvelope, and replay setting before any Adapter factory
is called. Fixed action-priority policies receive an exact known-action map;
open random/trace-mutation policies and unclassified effect actions
conservatively require the full required set.

One real workload used the six validated capabilities, completed in 29
decisions, and reproduced the same trace in a fresh worker. This is an admitted
smoke, not a Campaign, method comparison, coverage score, or discovered defect.

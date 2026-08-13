.PHONY: fmt test test-fast test-race-core test-race-full test-race-control-shards test-race-other audit-race-shards audit-no-v1 audit-no-retired-experiment probe-raftrs-core probe-omnipaxos-core test-raftrs-binding test-omnipaxos-binding test-omnipaxos-pss adapter-qualify-etcdraftv2 adapter-qualify-hashicorpraftv2 adapter-qualify-omnipaxosv2 audit-hashicorp-determinism audit-portable-cft-matrix audit-control-surfaces experiment-etcdraft-v2-workload experiment-etcdraft-v2-semantics experiment-etcdraft-v2-bundle experiment-etcdraft-v2-stateless-canonical-campaign experiment-etcdraft-v2-stateless-uniform-campaign experiment-etcdraft-v2-stateless-agent-campaign experiment-etcdraft-v2-action-class-random experiment-etcdraft-v2-trace-mutation experiment-etcdraft-v2-corpus-mutation experiment-etcdraft-v2-uniform-method experiment-etcdraft-v2-action-class-method experiment-etcdraft-v2-pss-guided-method build-etcdraft-v2-calibration experiment-etcdraft-v2-calibration evaluate-etcdraft-v2-calibration build-etcdraft-v2-action-class-calibration experiment-etcdraft-v2-action-class-calibration evaluate-etcdraft-v2-action-class-calibration build-etcdraft-v2-method-evaluation evaluate-etcdraft-v2-method-evaluation

fmt:
	gofmt -w $$(find adapters cmd internal qualifications -type f -name '*.go')

test: audit-no-v1 audit-no-retired-experiment
	go test ./...

# Fast local feedback may skip explicitly marked high-cost real-method tests.
# It is not a release substitute for `make test` and `make test-race-full`.
test-fast: audit-no-v1 audit-no-retired-experiment
	go test -short ./...

# Developer signal for the shared control boundary. This is deliberately not
# a substitute for the mechanically exhaustive `test-race-full` gate.
test-race-core: audit-no-v1 audit-no-retired-experiment
	go test -race -count=1 -timeout 20m ./adapters/... ./internal/control ./internal/controlruntime
	go test -race -count=1 -timeout 20m ./cmd/control-experiment \
		-run '^(TestEtcdraftSemanticWorkloadIsQualifiedCommittedAndReplayStable|TestEtcdraftM518b0GuardedIntentCompilesAndUsesQualifiedExecutor)$$'

# The manifest must be an exact partition of every top-level test in the heavy
# composition package. A new, renamed, duplicated or unclassified test fails
# before any long-running race witness starts.
audit-race-shards:
	@manifest=cmd/control-experiment/race-shards.txt; \
	awk 'NF != 2 || $$1 !~ /^(method|execution|agent)$$/ || $$2 !~ /^Test[[:alnum:]_]+$$/ { \
		print "invalid race shard entry at line " NR ": " $$0 > "/dev/stderr"; bad=1 \
	} END { if (NR == 0 || bad) exit 1 }' "$$manifest"
	@duplicates="$$(awk '{print $$2}' cmd/control-experiment/race-shards.txt | sort | uniq -d)"; \
	if test -n "$$duplicates"; then \
		echo "duplicate race shard tests:" >&2; echo "$$duplicates" >&2; exit 1; \
	fi
	@for shard in method execution agent; do \
		count="$$(awk -v shard="$$shard" '$$1 == shard { count++ } END { print count + 0 }' \
			cmd/control-experiment/race-shards.txt)"; \
		test "$$count" -gt 0 || { echo "empty race shard: $$shard" >&2; exit 1; }; \
	done
	@actual_raw="$$(go test -list '^Test' ./cmd/control-experiment)" || exit 1; \
	actual="$$(printf '%s\n' "$$actual_raw" | sed -n '/^Test/p' | sort)"; \
	declared="$$(awk '{print $$2}' cmd/control-experiment/race-shards.txt | sort)"; \
	if test "$$actual" != "$$declared"; then \
		echo "race shard manifest does not exactly match go test -list" >&2; \
		echo "declared:" >&2; echo "$$declared" >&2; \
		echo "actual:" >&2; echo "$$actual" >&2; exit 1; \
	fi

test-race-control-shards: audit-race-shards
	@set -e; for shard in method execution agent; do \
		pattern="$$(awk -v shard="$$shard" '$$1 == shard { \
			if (count++) printf "|"; printf "%s", $$2 \
		} END { print "" }' cmd/control-experiment/race-shards.txt)"; \
		echo "==> race shard: $$shard"; \
		go test -race -count=1 -timeout 20m ./cmd/control-experiment -run "^($$pattern)$$"; \
	done

test-race-other:
	@packages="$$(go list ./... | sed '\|/cmd/control-experiment$$|d')"; \
	test -n "$$packages"; \
	go test -race -count=1 -timeout 20m $$packages

# Full means every top-level control-experiment test exactly once plus every
# remaining package. Independent binaries keep the 20-minute ceiling honest.
test-race-full: audit-no-v1 audit-no-retired-experiment audit-race-shards
	$(MAKE) --no-print-directory test-race-control-shards
	$(MAKE) --no-print-directory test-race-other

# M5.16R guard: archived documents and experiment artifacts may mention v1,
# but no compiled source may import or recreate the deleted implementation cone.
audit-no-v1:
	@test -z "$$(find agents bindings catalogs drivers families migrations -type f \
		\( -name '*.go' -o -name '*.py' \) -print 2>/dev/null)"
	@if rg -n 'github.com/SuzumiyaHaruki/consensus-atlas/(internal/(adapter|agentcampaign|autoonboard|blackbox|campaign|core|coverage|driver|engine|explore|host|migration|protocolcontract|scenario|testplan)|drivers/etcdraft|families/raft|migrations/etcdraftv1v2)' --glob '*.go' .; then \
		echo 'M5.16R violation: compiled source references the deleted v1 cone' >&2; \
		exit 1; \
	fi

# M5.17bR2 guard: historical artifacts may retain these identities, but the
# pre-admission Planner and model transport must not return to compiled code.
audit-no-retired-experiment:
	@if rg -n 'ExecuteLegacy|PlannerProposalVersion|PlannerAttemptVersion|deepseek_control_planner|internal/modelcommand|CrossTargetPlannerView|CrossTargetCampaignLedger|AgentBatchFeedbackView|AgentFollowUpSpec|AgentPreferenceAblationFreeze|CampaignPlannerView|CampaignPlannedAttempt|CampaignModelCall|GuardedTestIntent|CompiledIntentPlan|AgentSemanticView|IntentCompilerCatalog|AgentInvocationAudit' \
		--glob '*.go' .; then \
		echo 'M5.17bR2 violation: compiled source references a retired experiment path' >&2; \
		exit 1; \
	fi

# M5.21r is a bounded candidate probe, not Adapter qualification. Exact Cargo
# resolution and a checked-in output keep the result independently reproducible.
probe-raftrs-core:
	sha256sum -c benchmarks/feasibility/raft-rs-m5.21r/inputs.sha256
	cargo fmt --manifest-path probes/raftrs/Cargo.toml -- --check
	cargo clippy --manifest-path probes/raftrs/Cargo.toml --locked --quiet -- -D warnings
	cargo run --manifest-path probes/raftrs/Cargo.toml --locked --quiet | \
		diff -u benchmarks/feasibility/raft-rs-m5.21r/report.json -

# M5.21u is a bounded non-Raft core probe. It may not change shared Actions,
# Runtime, PSS, or upstream OmniPaxos source and does not imply qualification.
probe-omnipaxos-core:
	sha256sum -c benchmarks/feasibility/omnipaxos-m5.21u/inputs.sha256
	cargo fmt --manifest-path probes/omnipaxos/Cargo.toml -- --check
	cargo clippy --manifest-path probes/omnipaxos/Cargo.toml --locked --quiet -- -D warnings
	cargo run --manifest-path probes/omnipaxos/Cargo.toml --locked --quiet | \
		diff -u benchmarks/feasibility/omnipaxos-m5.21u/report.json -

# Current OmniPaxos gate exercises the M5.21v control boundary, M5.21w workload,
# M5.21x PSS, and M5.21y decided-prefix Agreement path. It is not Qualification.
test-omnipaxos-binding:
	cargo fmt --manifest-path adapters/omnipaxosv2/worker/Cargo.toml -- --check
	cargo clippy --manifest-path adapters/omnipaxosv2/worker/Cargo.toml --locked --quiet -- -D warnings
	go test ./adapters/omnipaxosv2 -count=1 -v

# M5.21x checks only the target-owned mapping into the existing Core PSS IR.
# Core state counts remain descriptive and are not a quality/coverage score.
test-omnipaxos-pss:
	go test ./adapters/omnipaxosv2 -run '^TestCorePSS' -count=1 -v

# Current raft-rs gate exercises target-owned Go/Rust binding, opaque workload,
# error boundaries, and fresh-process Control Runtime replay. It is not qualification.
test-raftrs-binding:
	cargo fmt --manifest-path adapters/raftrsv2/worker/Cargo.toml -- --check
	cargo clippy --manifest-path adapters/raftrsv2/worker/Cargo.toml --locked --quiet -- -D warnings
	go test ./adapters/raftrsv2 -count=1 -v

adapter-qualify-etcdraftv2:
	go run ./cmd/adapter-qualify -target etcdraftv2 \
		-out artifacts/qualifications/etcdraft-v2-current/report.json

adapter-qualify-hashicorpraftv2:
	go run ./cmd/adapter-qualify -target hashicorpraftv2 \
		-out artifacts/qualifications/hashicorp-raft-v2-current/report.json

adapter-qualify-omnipaxosv2:
	cargo build --locked --quiet --manifest-path adapters/omnipaxosv2/worker/Cargo.toml
	go run ./cmd/adapter-qualify -target omnipaxosv2 \
		-worker adapters/omnipaxosv2/worker/target/debug/consensus-atlas-omnipaxos-worker \
		-out artifacts/qualifications/omnipaxos-v2-current/report.json

audit-hashicorp-determinism:
	go test ./adapters/hashicorpraftv2 \
		-run TestOfficialDeterminismBoundaryMatchesFrozenAudit -count=1

audit-portable-cft-matrix:
	go test ./qualifications/etcdraftv2 \
		-run TestFrozenPortableCapabilityMatrixMatchesMechanicalQualification -count=1
	go test ./qualifications/hashicorpraftv2 -count=1

audit-control-surfaces:
	go test ./qualifications/etcdraftv2 \
		-run 'TestFrozenControlSurfaceComparisonMatchesFreshQualifications|TestSurfaceDeclarationCannotSelfAwardSchedulerControl' -count=1

experiment-etcdraft-v2-workload:
	go run ./cmd/control-experiment -strategy workload -decisions 96 \
		-out benchmarks/experiments/etcdraft-v2-workload-m5.15/report.json

experiment-etcdraft-v2-semantics:
	go run ./cmd/control-experiment -strategy workload-semantics-v2 -decisions 96 \
		-out artifacts/experiments/etcdraft-v2-semantics-m5.17c0/report.json \
		-bundle-out artifacts/experiments/etcdraft-v2-semantics-m5.17c0/bundle.json

experiment-etcdraft-v2-bundle:
	go run ./cmd/control-experiment -strategy workload -decisions 96 \
		-out artifacts/experiments/etcdraft-v2-bundle-m5.16/report.json \
		-bundle-out artifacts/experiments/etcdraft-v2-bundle-m5.16/bundle.json

experiment-etcdraft-v2-stateless-canonical-campaign:
	go run ./cmd/control-experiment -strategy campaign-etcdraft-stateless-canonical-v1 \
		-stateless-corpus benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json \
		-campaign-dir artifacts/experiments/etcdraft-v2-stateless-canonical-m5.23r3/campaign \
		-campaign-attempts 3 -campaign-wall-clock-ms 600000 \
		-out artifacts/experiments/etcdraft-v2-stateless-canonical-m5.23r3/summary.json \
		-campaign-observation-out artifacts/experiments/etcdraft-v2-stateless-canonical-m5.23r3/observation.json

experiment-etcdraft-v2-stateless-uniform-campaign:
	go run ./cmd/control-experiment -strategy campaign-etcdraft-stateless-uniform-v1 \
		-stateless-corpus benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json \
		-campaign-dir artifacts/experiments/etcdraft-v2-stateless-uniform-m5.23r3/campaign \
		-campaign-attempts 3 -campaign-wall-clock-ms 600000 -policy-seed 1 \
		-out artifacts/experiments/etcdraft-v2-stateless-uniform-m5.23r3/summary.json \
		-campaign-observation-out artifacts/experiments/etcdraft-v2-stateless-uniform-m5.23r3/observation.json

# Explicit opt-in durable Agent Campaign. The artifact parent has no default so
# an ordinary validation target cannot read a key or contact the provider.
experiment-etcdraft-v2-stateless-agent-campaign:
	@test -n "$(AGENT_KEY_FILE)" || (echo 'AGENT_KEY_FILE is required' >&2; exit 1)
	@test -n "$(AGENT_ARTIFACT_DIR)" || (echo 'AGENT_ARTIFACT_DIR is required' >&2; exit 1)
	go run ./cmd/control-experiment -strategy campaign-etcdraft-stateless-agent-v1 \
		-stateless-corpus benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json \
		-campaign-dir "$(AGENT_ARTIFACT_DIR)/campaign" \
		-campaign-attempts 1 -campaign-wall-clock-ms 600000 \
		-campaign-model-tokens-per-attempt 36000 -agent-key-file "$(AGENT_KEY_FILE)" \
		-out "$(AGENT_ARTIFACT_DIR)/summary.json" \
		-campaign-observation-out "$(AGENT_ARTIFACT_DIR)/observation.json"

experiment-etcdraft-v2-action-class-random:
	go run ./cmd/control-experiment -strategy workload-action-class-random \
		-policy-seed 1 -decisions 96 \
		-out artifacts/experiments/etcdraft-v2-action-class-random-m5.17a/report.json \
		-bundle-out artifacts/experiments/etcdraft-v2-action-class-random-m5.17a/bundle.json

experiment-etcdraft-v2-trace-mutation:
	go run ./cmd/control-experiment -strategy workload-trace-mutation \
		-policy-seed 1 -decisions 96 \
		-out artifacts/experiments/etcdraft-v2-trace-mutation-m5.17b/report.json \
		-bundle-out artifacts/experiments/etcdraft-v2-trace-mutation-m5.17b/bundle.json

experiment-etcdraft-v2-corpus-mutation:
	go run ./cmd/control-experiment -strategy workload-trace-mutation-corpus \
		-policy-seed 1 -decisions 96 \
		-out artifacts/experiments/etcdraft-v2-corpus-mutation-m5.17c1/report.json \
		-source-bundle-out artifacts/experiments/etcdraft-v2-corpus-mutation-m5.17c1/source-bundle.json \
		-bundle-out artifacts/experiments/etcdraft-v2-corpus-mutation-m5.17c1/bundle.json \
		-method-out artifacts/experiments/etcdraft-v2-corpus-mutation-m5.17c1/method.json

experiment-etcdraft-v2-uniform-method:
	go run ./cmd/control-experiment -strategy workload-admissible-uniform-method \
		-policy-seed 1 -decisions 96 \
		-out artifacts/experiments/etcdraft-v2-methods-m5.17c2/uniform-method.json \
		-method-artifacts artifacts/experiments/etcdraft-v2-methods-m5.17c2/evidence

experiment-etcdraft-v2-action-class-method:
	go run ./cmd/control-experiment -strategy workload-action-class-random-method \
		-policy-seed 1 -decisions 96 \
		-out artifacts/experiments/etcdraft-v2-methods-m5.18b2/action-class-method.json \
		-method-artifacts artifacts/experiments/etcdraft-v2-methods-m5.18b2/evidence

experiment-etcdraft-v2-pss-guided-method:
	go run ./cmd/control-experiment -strategy workload-pss-guided-corpus \
		-policy-seed 1 -decisions 96 \
		-out artifacts/experiments/etcdraft-v2-methods-m5.17c2/pss-guided-method.json \
		-method-artifacts artifacts/experiments/etcdraft-v2-methods-m5.17c2/evidence

build-etcdraft-v2-calibration:
	go run ./cmd/sut-build -repo . \
		-spec benchmarks/pilots/etcdraft-v2-calibration-m5.16/build-input/candidate.json \
		-audit-out benchmarks/pilots/etcdraft-v2-calibration-m5.16/build-audit/candidate.json

experiment-etcdraft-v2-calibration: build-etcdraft-v2-calibration
	artifacts/pilots/etcdraft-v2-calibration-m5.16/bin/sut-c9811ab0ed8e2f39-bundle-v1 \
		-strategy workload -decisions 96 \
		-out artifacts/pilots/etcdraft-v2-calibration-m5.16/candidate/report.json \
		-bundle-out artifacts/pilots/etcdraft-v2-calibration-m5.16/candidate/bundle.json

evaluate-etcdraft-v2-calibration:
	go run ./cmd/defect-eval \
		-manifest benchmarks/pilots/etcdraft-v2-calibration-m5.16/evaluator/manifest.json \
		-control-bundle artifacts/experiments/etcdraft-v2-bundle-m5.16/bundle.json \
		-candidate-bundle artifacts/pilots/etcdraft-v2-calibration-m5.16/candidate/bundle.json \
		-out benchmarks/pilots/etcdraft-v2-calibration-m5.16/evaluator/report.json

build-etcdraft-v2-action-class-calibration:
	go run ./cmd/sut-build -repo . \
		-spec benchmarks/pilots/etcdraft-v2-action-class-random-m5.17a/build-input/candidate.json \
		-audit-out benchmarks/pilots/etcdraft-v2-action-class-random-m5.17a/build-audit/candidate.json

experiment-etcdraft-v2-action-class-calibration: build-etcdraft-v2-action-class-calibration
	artifacts/pilots/etcdraft-v2-action-class-random-m5.17a/bin/sut-c9811ab0ed8e2f39-action-class-v1 \
		-strategy workload-action-class-random -policy-seed 1 -decisions 96 \
		-out artifacts/pilots/etcdraft-v2-action-class-random-m5.17a/candidate/report.json \
		-bundle-out artifacts/pilots/etcdraft-v2-action-class-random-m5.17a/candidate/bundle.json

evaluate-etcdraft-v2-action-class-calibration:
	go run ./cmd/defect-eval \
		-manifest benchmarks/pilots/etcdraft-v2-action-class-random-m5.17a/evaluator/manifest.json \
		-control-bundle artifacts/experiments/etcdraft-v2-action-class-random-m5.17a/bundle.json \
		-candidate-bundle artifacts/pilots/etcdraft-v2-action-class-random-m5.17a/candidate/bundle.json \
		-out benchmarks/pilots/etcdraft-v2-action-class-random-m5.17a/evaluator/report.json

build-etcdraft-v2-method-evaluation:
	go run ./cmd/sut-build -repo . \
		-spec benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-input/control.json \
		-audit-out benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-audit/control.json
	go run ./cmd/sut-build -repo . \
		-spec benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-input/candidate.json \
		-audit-out benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-audit/candidate.json

evaluate-etcdraft-v2-method-evaluation:
	go run ./cmd/defect-eval \
		-manifest benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/evaluator/manifest.json \
		-method-spec benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/method-spec.json \
		-control-build-audit benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-audit/control.json \
		-control-binary artifacts/pilots/etcdraft-v2-method-evaluation-m5.18a/bin/sut-5826353327bce116-control-v2 \
		-candidate-build-audit benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-audit/candidate.json \
		-candidate-binary artifacts/pilots/etcdraft-v2-method-evaluation-m5.18a/bin/sut-c9811ab0ed8e2f39-candidate-v2 \
		-fresh-artifacts artifacts/pilots/etcdraft-v2-method-evaluation-m5.18a/fresh \
		-out benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/evaluator/report.json

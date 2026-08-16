.PHONY: fmt test test-fast test-race-core test-race-full test-race-control-shards test-race-other audit-race-shards audit-no-v1 test-omnipaxos-binding test-omnipaxos-pss adapter-qualify-etcdraftv2 adapter-qualify-hashicorpraftv2 adapter-qualify-omnipaxosv2 audit-hashicorp-determinism audit-portable-cft-matrix audit-control-surfaces

fmt:
	gofmt -w $$(find adapters cmd internal qualifications -type f -name '*.go')

test: audit-no-v1
	go test ./...

# Fast local feedback may skip explicitly marked high-cost real-method tests.
# It is not a release substitute for `make test` and `make test-race-full`.
test-fast: audit-no-v1
	go test -short ./...

# Developer signal for the shared control boundary. This is deliberately not
# a substitute for the mechanically exhaustive `test-race-full` gate.
test-race-core: audit-no-v1
	go test -race -count=1 -timeout 20m ./adapters/... ./internal/control ./internal/controlruntime
	go test -race -count=1 -timeout 20m ./cmd/control-experiment \
		-run '^(TestEtcdraftSemanticWorkloadIsQualifiedCommittedAndReplayStable|TestEtcdraftComposableActionsAreReachableAndReplayable)$$'

# The manifest must be an exact partition of every top-level test in the heavy
# composition package. A new, renamed, duplicated or unclassified test fails
# before any long-running race witness starts.
audit-race-shards:
	@manifest=cmd/control-experiment/race-shards.txt; \
	awk 'NF != 2 || $$1 !~ /^(method|execution|agent|agent-integration)$$/ || $$2 !~ /^Test[[:alnum:]_]+$$/ { \
		print "invalid race shard entry at line " NR ": " $$0 > "/dev/stderr"; bad=1 \
	} END { if (NR == 0 || bad) exit 1 }' "$$manifest"
	@duplicates="$$(awk '{print $$2}' cmd/control-experiment/race-shards.txt | sort | uniq -d)"; \
	if test -n "$$duplicates"; then \
		echo "duplicate race shard tests:" >&2; echo "$$duplicates" >&2; exit 1; \
	fi
	@for shard in method execution agent agent-integration; do \
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
	@set -e; for shard in method execution agent agent-integration; do \
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
test-race-full: audit-no-v1 audit-race-shards
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

DEEPSEEK_KEY_FILE ?= ../key.txt

.PHONY: fmt test audit-no-v1 adapter-qualify-etcdraftv2 adapter-qualify-hashicorpraftv2 audit-hashicorp-determinism audit-portable-cft-matrix audit-control-surfaces experiment-etcdraft-v2 experiment-etcdraft-v2-random experiment-etcdraft-v2-workload experiment-etcdraft-v2-bundle experiment-etcdraft-v2-action-class-random experiment-etcdraft-v2-trace-mutation build-etcdraft-v2-calibration experiment-etcdraft-v2-calibration evaluate-etcdraft-v2-calibration build-etcdraft-v2-action-class-calibration experiment-etcdraft-v2-action-class-calibration evaluate-etcdraft-v2-action-class-calibration experiment-etcdraft-v2-stub-planner experiment-etcdraft-v2-deepseek-planner

fmt:
	gofmt -w $$(find adapters cmd internal qualifications -type f -name '*.go')

test: audit-no-v1
	go test ./...

# M5.16R guard: archived documents and experiment artifacts may mention v1,
# but no compiled source may import or recreate the deleted implementation cone.
audit-no-v1:
	@test -z "$$(find agents bindings catalogs drivers families migrations -type f \
		\( -name '*.go' -o -name '*.py' \) -print 2>/dev/null)"
	@if rg -n 'github.com/SuzumiyaHaruki/consensus-atlas/(internal/(adapter|agentcampaign|autoonboard|blackbox|campaign|core|coverage|driver|engine|explore|host|migration|protocolcontract|scenario|testplan)|drivers/etcdraft|families/raft|migrations/etcdraftv1v2)' --glob '*.go' .; then \
		echo 'M5.16R violation: compiled source references the deleted v1 cone' >&2; \
		exit 1; \
	fi

adapter-qualify-etcdraftv2:
	go run ./cmd/adapter-qualify -target etcdraftv2 \
		-out artifacts/qualifications/etcdraft-v2-current/report.json

adapter-qualify-hashicorpraftv2:
	go run ./cmd/adapter-qualify -target hashicorpraftv2 \
		-out artifacts/qualifications/hashicorp-raft-v2-current/report.json

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

experiment-etcdraft-v2:
	go run ./cmd/control-experiment -strategy fixed -decisions 32 \
		-out benchmarks/experiments/etcdraft-v2-fixed-baselines-m5.10/report.json

experiment-etcdraft-v2-random:
	go run ./cmd/control-experiment -strategy random -policy-seed 1 -decisions 32 \
		-out benchmarks/experiments/etcdraft-v2-random-m5.11/report.json

experiment-etcdraft-v2-workload:
	go run ./cmd/control-experiment -strategy workload -decisions 96 \
		-out benchmarks/experiments/etcdraft-v2-workload-m5.15/report.json

experiment-etcdraft-v2-bundle:
	go run ./cmd/control-experiment -strategy workload -decisions 96 \
		-out artifacts/experiments/etcdraft-v2-bundle-m5.16/report.json \
		-bundle-out artifacts/experiments/etcdraft-v2-bundle-m5.16/bundle.json

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

experiment-etcdraft-v2-stub-planner:
	go run ./cmd/control-experiment -strategy stub-planner -decisions 32 \
		-out benchmarks/experiments/etcdraft-v2-stub-planner-m5.12/attempt.json

# This target performs exactly one real model call and is excluded from test.
experiment-etcdraft-v2-deepseek-planner:
	go run ./cmd/control-experiment -strategy deepseek-planner \
		-key-file $(DEEPSEEK_KEY_FILE) -model deepseek-v4-flash -model-timeout 5m \
		-decisions 32 \
		-out benchmarks/experiments/etcdraft-v2-deepseek-planner-m5.13/attempt.json

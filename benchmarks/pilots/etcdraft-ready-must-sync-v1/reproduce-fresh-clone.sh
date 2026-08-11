#!/usr/bin/env bash
# Rebuild and verify the frozen M4.11 pilot without modifying checked-in
# reports. Run from a fresh clone after the repository's Go dependencies and
# go.etcd.io/raft/v3@v3.6.0 are present in the local module cache.
set -euo pipefail

pilot_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_dir=$(CDPATH= cd -- "$pilot_dir/../../.." && pwd)
scratch_dir=$(mktemp -d)

if [ "$(go env GOVERSION)" != "go1.25.8" ] || [ "$(go env GOOS)" != "linux" ] || [ "$(go env GOARCH)" != "amd64" ]; then
	printf '%s\n' 'M4.11 frozen binary identities require go1.25.8 on linux/amd64.' >&2
	exit 1
fi

module_dir="$(go env GOMODCACHE)/go.etcd.io/raft/v3@v3.6.0"
if [ ! -d "$module_dir" ]; then
	printf '%s\n' "missing readonly module cache entry: $module_dir" >&2
	printf '%s\n' 'Run go mod download once before this offline verification.' >&2
	exit 1
fi

candidate_bin="$repo_dir/artifacts/pilots/etcdraft-ready-must-sync-v1/bin/sut-a277bf530a06feb0"
control_bin="$repo_dir/artifacts/pilots/etcdraft-ready-must-sync-v1/bin/sut-5b83c9e4579067c0"
if [ -e "$candidate_bin" ] || [ -e "$control_bin" ]; then
	printf '%s\n' 'refusing to overwrite an existing pilot binary; use a fresh clone or remove only the verified artifact binaries.' >&2
	exit 1
fi

cd "$repo_dir"
GOPROXY=off GOSUMDB=off GOWORK=off go run ./cmd/candidate-qualify \
	-catalog benchmarks/candidates/etcdraft/catalog-v1.json \
	-profile profiles/raft/ready-must-sync-v1.json \
	-raft-spec benchmarks/pilots/etcdraft-ready-must-sync-v1/campaign-scope.json \
	-snapshot-out "$scratch_dir/capability-snapshot.json" \
	-out "$scratch_dir/qualification-report.json"
cmp -s "$scratch_dir/capability-snapshot.json" benchmarks/pilots/etcdraft-ready-must-sync-v1/capability-snapshot.json
cmp -s "$scratch_dir/qualification-report.json" benchmarks/pilots/etcdraft-ready-must-sync-v1/qualification-report.json

GOPROXY=off GOSUMDB=off GOWORK=off go run ./cmd/sut-build \
	-repo . \
	-spec benchmarks/pilots/etcdraft-ready-must-sync-v1/build-input/candidate.json \
	-audit-out "$scratch_dir/candidate-audit.json"
cmp -s "$scratch_dir/candidate-audit.json" benchmarks/pilots/etcdraft-ready-must-sync-v1/build-audit/trial-ready-must-sync-candidate-v1.json

GOPROXY=off GOSUMDB=off GOWORK=off go run ./cmd/sut-build \
	-repo . \
	-spec benchmarks/pilots/etcdraft-ready-must-sync-v1/build-input/control.json \
	-audit-out "$scratch_dir/control-audit.json"
cmp -s "$scratch_dir/control-audit.json" benchmarks/pilots/etcdraft-ready-must-sync-v1/build-audit/trial-ready-must-sync-control-v1.json

"$candidate_bin" \
	-profile profiles/raft/ready-must-sync-v1.json \
	-plans plans/defectbench/etcdraft-0675f3d-v1.json \
	-out "$scratch_dir/candidate-campaign.json" \
	-artifact trial://trial-ready-must-sync-candidate-v1
cmp -s "$scratch_dir/candidate-campaign.json" benchmarks/pilots/etcdraft-ready-must-sync-v1/campaign/trial-ready-must-sync-candidate-v1.json

"$control_bin" \
	-profile profiles/raft/ready-must-sync-v1.json \
	-plans plans/defectbench/etcdraft-0675f3d-v1.json \
	-out "$scratch_dir/control-campaign.json" \
	-artifact trial://trial-ready-must-sync-control-v1
cmp -s "$scratch_dir/control-campaign.json" benchmarks/pilots/etcdraft-ready-must-sync-v1/campaign/trial-ready-must-sync-control-v1.json

GOPROXY=off GOSUMDB=off GOWORK=off go run ./cmd/defect-eval \
	-manifest benchmarks/pilots/etcdraft-ready-must-sync-v1/evaluator/manifest.json \
	-submission benchmarks/pilots/etcdraft-ready-must-sync-v1/evaluator/submission.json \
	-out "$scratch_dir/evaluator-report.json"
cmp -s "$scratch_dir/evaluator-report.json" benchmarks/pilots/etcdraft-ready-must-sync-v1/evaluator/report.json

printf 'M4.11 fresh-clone verification passed; temporary files: %s\n' "$scratch_dir"

DEEPSEEK_KEY_FILE ?= key.txt
BLIND_BENCHMARK_ID ?= public-development-v1
BLIND_BENCHMARK_DIGEST ?= dd82d3a5e3b43b0a0035a3fa4044d04910c4fd840d49129a58d299b336c0df1c
BLIND_TRIAL_ID ?= public-etcdraft-v1

.PHONY: fmt test contract-compile-etcdraft auto-onboard-etcdraft auto-onboard-etcdraft-llm coverage-compile-etcdraft campaign-etcdraft campaign-baselines-etcdraft agent-campaign-etcdraft run-raft run-raft-onboarding run-raft-llm experiment-random experiment-dfs

fmt:
	gofmt -w $$(find bindings cmd drivers families internal -type f -name '*.go')

test:
	go test ./...
	python3 -m unittest discover -s agents -p 'test_*.py'

contract-compile-etcdraft:
	go run ./cmd/contract-compile \
		-contract contracts/etcdraft-v1.json

auto-onboard-etcdraft:
	go run ./cmd/auto-onboard \
		-repo . \
		-contract contracts/etcdraft-v1.json \
		-binding onboarding/etcdraft-binding-v1.json \
		-report-out artifacts/onboarding/etcdraft-v1.json \
		-profile-out artifacts/onboarding/etcdraft-profile-v1.json

auto-onboard-etcdraft-llm:
	go run ./cmd/auto-onboard-llm \
		-repo . \
		-contract contracts/etcdraft-v1.json \
		-key-file $(DEEPSEEK_KEY_FILE) \
		-model deepseek-v4-flash \
		-max-attempts 5 \
		-report-out artifacts/onboarding/deepseek-etcdraft-v1.json \
		-binding-out artifacts/onboarding/deepseek-etcdraft-binding-v1.json \
		-profile-out artifacts/onboarding/deepseek-etcdraft-profile-v1.json

coverage-compile-etcdraft: auto-onboard-etcdraft
	go run ./cmd/coverage-compile \
		-base-profile artifacts/onboarding/etcdraft-profile-v1.json \
		-spec profiles/raft/three-node-cft-v1.json \
		-out artifacts/profiles/etcdraft-campaign-v1.json

run-raft-onboarding: auto-onboard-etcdraft
	go run ./cmd/runner \
		-profile artifacts/onboarding/etcdraft-profile-v1.json \
		-scenario scenarios/etcdraft-election-crash.json \
		-out artifacts/etcdraft-onboarding-run.json

run-raft: coverage-compile-etcdraft
	go run ./cmd/runner \
		-profile artifacts/profiles/etcdraft-campaign-v1.json \
		-scenario scenarios/etcdraft-election-crash.json \
		-out artifacts/etcdraft-campaign-run.json

campaign-etcdraft: coverage-compile-etcdraft
	go run ./cmd/campaign \
		-profile artifacts/profiles/etcdraft-campaign-v1.json \
		-plans plans/etcdraft-expert-v1.json \
		-out artifacts/campaigns/etcdraft-expert-v1.json

campaign-baselines-etcdraft: coverage-compile-etcdraft
	go run ./cmd/campaign \
		-profile artifacts/profiles/etcdraft-campaign-v1.json \
		-plans plans/baselines/etcdraft-partition-random-256-v1.json \
		-out artifacts/campaigns/etcdraft-partition-random-256-v1.json
	go run ./cmd/campaign \
		-profile artifacts/profiles/etcdraft-campaign-v1.json \
		-plans plans/baselines/etcdraft-partition-dfs-256-v1.json \
		-out artifacts/campaigns/etcdraft-partition-dfs-256-v1.json

# This target performs real model calls and is intentionally excluded from test.
agent-campaign-etcdraft: coverage-compile-etcdraft
	go run ./cmd/agent-campaign \
		-repo . \
		-profile artifacts/profiles/etcdraft-campaign-v1.json \
		-blind-benchmark-id $(BLIND_BENCHMARK_ID) \
		-blind-benchmark-digest $(BLIND_BENCHMARK_DIGEST) \
		-blind-trial-id $(BLIND_TRIAL_ID) \
		-key-file $(DEEPSEEK_KEY_FILE) \
		-model deepseek-v4-flash \
		-campaign-id deepseek-etcdraft-blind-v1 \
		-max-attempts 6 \
		-max-no-progress 3 \
		-max-runs 20 \
		-max-decisions 1024 \
		-max-tokens 200000 \
		-out artifacts/agent-campaigns/deepseek-etcdraft-blind-v1.json

run-raft-llm: auto-onboard-etcdraft-llm
	go run ./cmd/runner \
		-profile artifacts/onboarding/deepseek-etcdraft-profile-v1.json \
		-scenario scenarios/etcdraft-election-crash.json \
		-out artifacts/deepseek-etcdraft-run.json

experiment-random: auto-onboard-etcdraft
	go run ./cmd/experiment \
		-profile artifacts/onboarding/etcdraft-profile-v1.json \
		-setup scenarios/etcdraft-explore-setup.json \
		-strategy random -runs 64 -budget 64 -decision-budget 128 -seed 1 \
		-out artifacts/etcdraft-random-experiment.json

experiment-dfs: auto-onboard-etcdraft
	go run ./cmd/experiment \
		-profile artifacts/onboarding/etcdraft-profile-v1.json \
		-setup scenarios/etcdraft-explore-setup.json \
		-strategy dfs -runs 64 -budget 64 -decision-budget 128 \
		-out artifacts/etcdraft-dfs-experiment.json

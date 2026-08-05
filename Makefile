.PHONY: fmt test run

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

run:
	go run ./cmd/runner \
		-profile profiles/toy-v1.json \
		-scenario scenarios/toy-election.json \
		-out artifacts/toy-run.json

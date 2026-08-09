package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

const etcdraftAgentB4PairStrategy = "workload-agent-b4-pair"

type etcdraftAgentB4FreezeBuilder func(
	context.Context, int, uint64,
) (etcdraftAgentB4RequestFreeze, error)

type agentKeyReader func(string) (string, error)

func runEtcdraftAgentB4Pair(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	keyFile string,
	artifactDirectory string,
	stdout io.Writer,
	buildFreeze etcdraftAgentB4FreezeBuilder,
	readKey agentKeyReader,
	client deepSeekIntentClient,
) error {
	if decisions <= 0 || keyFile == "" || artifactDirectory == "" || stdout == nil ||
		buildFreeze == nil || readKey == nil {
		return errors.New("ETCDRAFT_B4_PAIR_RUNNER_FLAGS_INVALID")
	}
	if _, err := os.Lstat(artifactDirectory); err == nil || !os.IsNotExist(err) {
		return errors.New("ETCDRAFT_B4_PAIR_DIRECTORY_NOT_NEW")
	}
	freeze, err := buildFreeze(ctx, decisions, baseSeed)
	if err != nil {
		return err
	}
	if err := validateEtcdraftAgentB4RequestFreeze(freeze); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := readKey(keyFile)
	if err != nil {
		return err
	}
	result, pairErr := consumeEtcdraftAgentB4Pair(ctx, key, client, freeze)
	key = ""
	if result.Ledger.SchemaVersion == "" {
		return errors.Join(errors.New("ETCDRAFT_B4_PAIR_RESULT_INVALID"), pairErr)
	}
	if err := persistEtcdraftAgentB4Pair(artifactDirectory, freeze, result); err != nil {
		return errors.Join(err, pairErr)
	}
	_, outputErr := fmt.Fprintf(
		stdout,
		"wrote %s\nno_feedback=%s with_feedback=%s model_calls=%d ledger=%s\n",
		artifactDirectory, result.NoFeedback.Audit.Status, result.WithFeedback.Audit.Status,
		result.Ledger.Totals.Model.Calls, result.Ledger.Digest,
	)
	return errors.Join(outputErr, pairErr)
}

func runEtcdraftAgentB4PairOptIn(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	keyFile string,
	artifactDirectory string,
	stdout io.Writer,
) error {
	return runEtcdraftAgentB4Pair(
		ctx, decisions, baseSeed, keyFile, artifactDirectory, stdout,
		newEtcdraftAgentB4RequestFreeze, readAgentKey, defaultDeepSeekIntentClient(),
	)
}

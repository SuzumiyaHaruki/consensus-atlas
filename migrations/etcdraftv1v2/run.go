// Package etcdraftv1v2 is an explicit composition root used only while the
// legacy etcd/raft execution path is migrated to Control Runtime v2.
package etcdraftv1v2

import (
	"context"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/migration"
)

const (
	SuiteID                  = "etcdraft-v1-v2-m5.2.5"
	ScenarioNormalCommit     = "normal-commit"
	ScenarioTransportControl = "message-drop-duplicate-commit"
	ScenarioFollowerRecovery = "committed-follower-recovery"
	ScenarioNaturalChange    = "natural-leader-change"
)

func Run(ctx context.Context) (migration.Suite, error) {
	legacy, err := runLegacyNormal(ctx)
	if err != nil {
		return migration.Suite{}, err
	}
	legacyTransport, err := runLegacyTransport(ctx)
	if err != nil {
		return migration.Suite{}, err
	}
	controlV2Transport, err := runControlV2Transport(ctx)
	if err != nil {
		return migration.Suite{}, err
	}
	transport, err := migration.CompareExpected(
		legacyTransport, controlV2Transport,
		expectedSingleCommand("after-drop-duplicate", []string{
			"commit-all-nodes", "message-drop", "message-duplicate",
		}),
	)
	if err != nil {
		return migration.Suite{}, err
	}
	legacyRecovery, err := runLegacyRecovery(ctx)
	if err != nil {
		return migration.Suite{}, err
	}
	controlV2Recovery, err := runControlV2Recovery(ctx)
	if err != nil {
		return migration.Suite{}, err
	}
	recovery, err := migration.CompareExpected(
		legacyRecovery, controlV2Recovery,
		expectedSingleCommand("alpha", []string{
			"commit-all-nodes", "node-restart-preserves-commit",
		}),
	)
	if err != nil {
		return migration.Suite{}, err
	}
	controlV2, err := runControlV2Normal(ctx)
	if err != nil {
		return migration.Suite{}, err
	}
	normal, err := migration.CompareExpected(legacy, controlV2, migration.Expectation{
		Commands: []migration.Command{{
			Ordinal: 1, ValueDigest: migration.ValueDigest([]byte("alpha")),
			AppliedNodes: []string{"n1", "n2", "n3"},
		}},
		Witnesses: []string{"commit-all-nodes"},
	})
	if err != nil {
		return migration.Suite{}, err
	}
	natural, err := migration.Defer(
		ScenarioNaturalChange,
		"V1_NATURAL_ELECTION_TIMEOUT_REPLAY_UNSUPPORTED",
	)
	if err != nil {
		return migration.Suite{}, err
	}
	return migration.SealSuite(SuiteID, []migration.Case{normal, transport, recovery, natural})
}

func expectedSingleCommand(value string, witnesses []string) migration.Expectation {
	return migration.Expectation{
		Commands: []migration.Command{{
			Ordinal: 1, ValueDigest: migration.ValueDigest([]byte(value)),
			AppliedNodes: []string{"n1", "n2", "n3"},
		}},
		Witnesses: append([]string(nil), witnesses...),
	}
}

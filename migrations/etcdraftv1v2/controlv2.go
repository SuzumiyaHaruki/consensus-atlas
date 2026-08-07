package etcdraftv1v2

import (
	"context"
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/migration"
)

const controlV2PathID = "control-runtime-v2"

func runControlV2Normal(ctx context.Context) (migration.Summary, error) {
	runtime, config, err := newControlV2Runtime(ctx, "normal-commit")
	if err != nil {
		return migration.Summary{}, err
	}
	leader, err := driveControlV2Leader(ctx, runtime, 512)
	if err != nil {
		return migration.Summary{}, err
	}
	if err := invokeControlV2(ctx, runtime, leader, "normal-request", []byte("alpha")); err != nil {
		return migration.Summary{}, err
	}
	if err := driveControlV2Applications(ctx, runtime, 1, 1024); err != nil {
		return migration.Summary{}, err
	}
	return controlV2Summary(
		ctx, ScenarioNormalCommit, runtime, config,
		[]string{"commit-all-nodes"},
	)
}

func runControlV2Transport(ctx context.Context) (migration.Summary, error) {
	runtime, config, err := newControlV2Runtime(ctx, "message-drop-duplicate")
	if err != nil {
		return migration.Summary{}, err
	}
	pair, err := driveControlV2ElectionPair(ctx, runtime, 512)
	if err != nil {
		return migration.Summary{}, err
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return migration.Summary{}, err
	}
	duplicate, ok := actionForControlV2Item(actions, control.ActionDuplicateMessage, pair[0].ID)
	if !ok {
		return migration.Summary{}, errors.New("MIGRATION_V2_DUPLICATE_ACTION_MISSING")
	}
	if _, err := runtime.Select(ctx, duplicate.ID); err != nil {
		return migration.Summary{}, err
	}
	actions, err = runtime.EnabledActions(ctx)
	if err != nil {
		return migration.Summary{}, err
	}
	drop, ok := actionForControlV2Item(actions, control.ActionDropMessage, pair[1].ID)
	if !ok {
		return migration.Summary{}, errors.New("MIGRATION_V2_DROP_ACTION_MISSING")
	}
	if _, err := runtime.Select(ctx, drop.ID); err != nil {
		return migration.Summary{}, err
	}
	leader, err := driveControlV2Leader(ctx, runtime, 1024)
	if err != nil {
		return migration.Summary{}, err
	}
	if err := invokeControlV2(
		ctx, runtime, leader, "transport-request", []byte("after-drop-duplicate"),
	); err != nil {
		return migration.Summary{}, err
	}
	if err := driveControlV2Applications(ctx, runtime, 1, 1024); err != nil {
		return migration.Summary{}, err
	}
	trace, err := runtime.Trace()
	if err != nil {
		return migration.Summary{}, err
	}
	if !controlV2TraceHasKind(trace, control.ActionDuplicateMessage) ||
		!controlV2TraceHasKind(trace, control.ActionDropMessage) {
		return migration.Summary{}, errors.New("MIGRATION_V2_TRANSPORT_WITNESS_MISSING")
	}
	return controlV2Summary(
		ctx, ScenarioTransportControl, runtime, config,
		[]string{"commit-all-nodes", "message-drop", "message-duplicate"},
	)
}

func runControlV2Recovery(ctx context.Context) (migration.Summary, error) {
	runtime, config, err := newControlV2Runtime(ctx, "follower-recovery")
	if err != nil {
		return migration.Summary{}, err
	}
	leader, err := driveControlV2Leader(ctx, runtime, 512)
	if err != nil {
		return migration.Summary{}, err
	}
	if err := invokeControlV2(ctx, runtime, leader, "recovery-request", []byte("alpha")); err != nil {
		return migration.Summary{}, err
	}
	if err := driveControlV2Applications(ctx, runtime, 1, 1024); err != nil {
		return migration.Summary{}, err
	}
	evidence, err := latestControlV2Evidence(runtime)
	if err != nil {
		return migration.Summary{}, err
	}
	var target control.NodeID
	var beforeDigest string
	for _, node := range evidence.Nodes {
		if node.Node != leader {
			target = node.Node
			beforeDigest = node.ApplicationDigest
			break
		}
	}
	if target == "" || beforeDigest == "" {
		return migration.Summary{}, errors.New("MIGRATION_V2_RECOVERY_TARGET_MISSING")
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return migration.Summary{}, err
	}
	crash, ok := actionForControlV2Node(actions, control.ActionCrash, target)
	if !ok {
		return migration.Summary{}, errors.New("MIGRATION_V2_CRASH_ACTION_MISSING")
	}
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		return migration.Summary{}, err
	}
	actions, err = runtime.EnabledActions(ctx)
	if err != nil {
		return migration.Summary{}, err
	}
	restart, ok := actionForControlV2Node(actions, control.ActionRestart, target)
	if !ok {
		return migration.Summary{}, errors.New("MIGRATION_V2_RESTART_ACTION_MISSING")
	}
	if _, err := runtime.Select(ctx, restart.ID); err != nil {
		return migration.Summary{}, err
	}
	if err := drainControlV2NodeEffects(ctx, runtime, target, 128); err != nil {
		return migration.Summary{}, err
	}
	evidence, err = latestControlV2Evidence(runtime)
	if err != nil {
		return migration.Summary{}, err
	}
	found := false
	for _, node := range evidence.Nodes {
		if node.Node == target && node.Running && node.Incarnation == 2 &&
			node.ApplicationCommands == 1 && node.ApplicationDigest == beforeDigest {
			found = true
		}
	}
	if !found {
		return migration.Summary{}, errors.New("MIGRATION_V2_RECOVERY_STATE_INVALID")
	}
	trace, err := runtime.Trace()
	if err != nil {
		return migration.Summary{}, err
	}
	if !controlV2TraceHasKind(trace, control.ActionCrash) ||
		!controlV2TraceHasKind(trace, control.ActionRestart) {
		return migration.Summary{}, errors.New("MIGRATION_V2_RECOVERY_WITNESS_MISSING")
	}
	return controlV2Summary(
		ctx, ScenarioFollowerRecovery, runtime, config,
		[]string{"commit-all-nodes", "node-restart-preserves-commit"},
	)
}

func newControlV2Runtime(
	ctx context.Context,
	scenarioID string,
) (*controlruntime.Runtime, controlruntime.Config, error) {
	adapter, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	if err != nil {
		return nil, controlruntime.Config{}, err
	}
	config := controlruntime.Config{
		Seed: []byte("migration-etcdraft-v2-" + scenarioID), ClockError: 0, MaxClones: 1,
	}
	runtime, err := controlruntime.New(ctx, adapter, config)
	return runtime, config, err
}

func driveControlV2Leader(
	ctx context.Context,
	runtime *controlruntime.Runtime,
	bound int,
) (control.NodeID, error) {
	for decision := 0; decision < bound; decision++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return "", err
		}
		evidence, err := latestControlV2Evidence(runtime)
		if err != nil {
			return "", err
		}
		for _, node := range evidence.Nodes {
			if !node.Running || node.Role != "StateLeader" || hasNodeEffect(actions, node.Node) {
				continue
			}
			return node.Node, nil
		}
		action, ok := controlV2Progress(actions)
		if !ok {
			return "", errors.New("MIGRATION_V2_LEADER_PROGRESS_MISSING")
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			return "", err
		}
	}
	return "", errors.New("MIGRATION_V2_LEADER_BOUND_EXCEEDED")
}

func driveControlV2ElectionPair(
	ctx context.Context,
	runtime *controlruntime.Runtime,
	bound int,
) ([2]controlruntime.ItemSnapshot, error) {
	for decision := 0; decision < bound; decision++ {
		items := runtime.Snapshot().Items
		for left := 0; left < len(items); left++ {
			first := items[left]
			if !controlV2ElectionMessage(first) {
				continue
			}
			for right := left + 1; right < len(items); right++ {
				second := items[right]
				if controlV2ElectionMessage(second) &&
					first.Value.Message.Source == second.Value.Message.Source &&
					first.Value.Message.TypeHint == second.Value.Message.TypeHint &&
					first.Value.Message.Target != second.Value.Message.Target {
					return [2]controlruntime.ItemSnapshot{first, second}, nil
				}
			}
		}
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return [2]controlruntime.ItemSnapshot{}, err
		}
		action, ok := controlV2ElectionProgress(actions)
		if !ok {
			return [2]controlruntime.ItemSnapshot{}, errors.New("MIGRATION_V2_ELECTION_MESSAGE_PROGRESS_MISSING")
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			return [2]controlruntime.ItemSnapshot{}, err
		}
	}
	return [2]controlruntime.ItemSnapshot{}, errors.New("MIGRATION_V2_ELECTION_MESSAGE_BOUND_EXCEEDED")
}

func invokeControlV2(
	ctx context.Context,
	runtime *controlruntime.Runtime,
	node control.NodeID,
	requestID string,
	value []byte,
) error {
	payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: requestID, Value: append([]byte(nil), value...),
	})
	if err != nil {
		return err
	}
	action, err := runtime.OfferInvoke(ctx, node, payload)
	if err != nil {
		return err
	}
	_, err = runtime.Select(ctx, action)
	return err
}

func driveControlV2Applications(
	ctx context.Context,
	runtime *controlruntime.Runtime,
	count int,
	bound int,
) error {
	for decision := 0; decision < bound; decision++ {
		evidence, err := latestControlV2Evidence(runtime)
		if err != nil {
			return err
		}
		complete := len(evidence.Nodes) == 3
		for _, node := range evidence.Nodes {
			if !node.Running || node.ApplicationCommands < count {
				complete = false
				break
			}
		}
		if complete {
			return nil
		}
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return err
		}
		action, ok := controlV2Progress(actions)
		if !ok {
			return errors.New("MIGRATION_V2_APPLICATION_PROGRESS_MISSING")
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			return err
		}
	}
	return errors.New("MIGRATION_V2_APPLICATION_BOUND_EXCEEDED")
}

func controlV2Summary(
	ctx context.Context,
	scenarioID string,
	runtime *controlruntime.Runtime,
	config controlruntime.Config,
	witnesses []string,
) (migration.Summary, error) {
	evidence, err := latestControlV2Evidence(runtime)
	if err != nil {
		return migration.Summary{}, err
	}
	results := make([]etcdraftv2.ClientResult, 0)
	for _, item := range runtime.Snapshot().Items {
		if item.Kind != control.ItemClientResult || item.Value.Response == nil ||
			item.Value.Response.Status != "committed" {
			continue
		}
		result, err := etcdraftv2.ProjectClientResult(*item.Value.Response)
		if err != nil {
			return migration.Summary{}, err
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Index < results[j].Index })
	commands := make([]migration.Command, 0, len(results))
	for ordinal, result := range results {
		nodes := make([]string, 0, len(evidence.Nodes))
		for _, node := range evidence.Nodes {
			if node.ApplicationCommands >= ordinal+1 {
				nodes = append(nodes, string(node.Node))
			}
		}
		commands = append(commands, migration.Command{
			Ordinal: ordinal + 1, ValueDigest: migration.ValueDigest(result.Value), AppliedNodes: nodes,
		})
	}
	safety := migration.Safety{}
	if applicationDigestsDiverge(evidence) {
		safety.AgreementViolations = 1
	}
	trace, err := runtime.Trace()
	if err != nil {
		return migration.Summary{}, err
	}
	if err := trace.Validate(); err != nil {
		safety.TraceIntegrityViolations = 1
	}
	adapter, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	if err != nil {
		return migration.Summary{}, err
	}
	manifest, err := adapter.Manifest(ctx)
	if err != nil {
		return migration.Summary{}, err
	}
	_, replayErr := controlruntime.Replay(ctx, adapter, config, trace)
	return migration.SealSummary(migration.Summary{
		ScenarioID: scenarioID, PathID: controlV2PathID,
		ImplementationID: manifest.BuildID, ConfigurationID: manifest.ConfigurationDigest,
		Commands: commands, Safety: safety,
		Replay:    migration.Replay{Mode: "strict-decision-trace", Stable: replayErr == nil},
		Witnesses: append([]string(nil), witnesses...),
	})
}

func latestControlV2Evidence(runtime *controlruntime.Runtime) (etcdraftv2.Evidence, error) {
	trace, err := runtime.Trace()
	if err != nil {
		return etcdraftv2.Evidence{}, err
	}
	for index := len(trace.Records) - 1; index >= 0; index-- {
		if trace.Records[index].Evidence != nil {
			return etcdraftv2.ProjectEvidence(*trace.Records[index].Evidence)
		}
	}
	return etcdraftv2.ProjectEvidence(trace.InitialEvidence)
}

func hasNodeEffect(actions []control.Action, node control.NodeID) bool {
	for _, action := range actions {
		if action.Kind == control.ActionCompleteEffect && action.Node.Node == node {
			return true
		}
	}
	return false
}

func controlV2Progress(actions []control.Action) (control.Action, bool) {
	for _, kind := range []control.ActionKind{
		control.ActionCompleteEffect,
		control.ActionDeliverMessage,
		control.ActionFireTemporal,
	} {
		for _, action := range actions {
			if action.Kind == kind {
				return action, true
			}
		}
	}
	return control.Action{}, false
}

func controlV2ElectionProgress(actions []control.Action) (control.Action, bool) {
	for _, kind := range []control.ActionKind{control.ActionCompleteEffect, control.ActionFireTemporal} {
		if action, ok := actionByKind(actions, kind); ok {
			return action, true
		}
	}
	return control.Action{}, false
}

func controlV2ElectionMessage(item controlruntime.ItemSnapshot) bool {
	if item.Kind != control.ItemMessage || item.State != control.ItemEnabled || item.Value.Message == nil {
		return false
	}
	return item.Value.Message.TypeHint == "MsgVote" || item.Value.Message.TypeHint == "MsgPreVote"
}

func actionForControlV2Item(
	actions []control.Action,
	kind control.ActionKind,
	item control.ItemID,
) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind && action.Item == item {
			return action, true
		}
	}
	return control.Action{}, false
}

func actionForControlV2Node(
	actions []control.Action,
	kind control.ActionKind,
	node control.NodeID,
) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind && action.Node.Node == node {
			return action, true
		}
	}
	return control.Action{}, false
}

func drainControlV2NodeEffects(
	ctx context.Context,
	runtime *controlruntime.Runtime,
	node control.NodeID,
	bound int,
) error {
	for decision := 0; decision < bound; decision++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return err
		}
		var effect control.Action
		found := false
		for _, action := range actions {
			if action.Kind == control.ActionCompleteEffect && action.Node.Node == node {
				effect = action
				found = true
				break
			}
		}
		if !found {
			return nil
		}
		if _, err := runtime.Select(ctx, effect.ID); err != nil {
			return err
		}
	}
	return errors.New("MIGRATION_V2_RECOVERY_EFFECT_BOUND_EXCEEDED")
}

func applicationDigestsDiverge(evidence etcdraftv2.Evidence) bool {
	var digest string
	var count int
	for _, node := range evidence.Nodes {
		if digest == "" {
			digest = node.ApplicationDigest
			count = node.ApplicationCommands
			continue
		}
		if node.ApplicationDigest != digest || node.ApplicationCommands != count {
			return true
		}
	}
	return false
}

func actionByKind(actions []control.Action, kind control.ActionKind) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind {
			return action, true
		}
	}
	return control.Action{}, false
}

func controlV2TraceHasKind(trace controlruntime.Trace, kind control.ActionKind) bool {
	for _, record := range trace.Records {
		if record.Action.Kind == kind {
			return true
		}
	}
	return false
}

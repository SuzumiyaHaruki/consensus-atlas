package etcdraftv1v2

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/drivers/etcdraft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/host"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/migration"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const legacyPathID = "legacy-v1-engine"

var legacyConfigurationID = migration.ValueDigest([]byte(
	"nodes=n1,n2,n3;election_tick=10;heartbeat_tick=1;ready_sync=conservative",
))

func runLegacyNormal(ctx context.Context) (migration.Summary, error) {
	return replayLegacy(
		ctx, ScenarioNormalCommit, executeLegacyNormal,
		[]string{"commit-all-nodes"},
	)
}

func runLegacyTransport(ctx context.Context) (migration.Summary, error) {
	return replayLegacy(
		ctx, ScenarioTransportControl, executeLegacyTransport,
		[]string{"commit-all-nodes", "message-drop", "message-duplicate"},
	)
}

func runLegacyRecovery(ctx context.Context) (migration.Summary, error) {
	return replayLegacy(
		ctx, ScenarioFollowerRecovery, executeLegacyRecovery,
		[]string{"commit-all-nodes", "node-restart-preserves-commit"},
	)
}

type legacyExecutor func(context.Context) ([]core.TraceRecord, error)

func replayLegacy(
	ctx context.Context,
	scenarioID string,
	execute legacyExecutor,
	witnesses []string,
) (migration.Summary, error) {
	first, err := execute(ctx)
	if err != nil {
		return migration.Summary{}, err
	}
	second, err := execute(ctx)
	if err != nil {
		return migration.Summary{}, err
	}
	left, err := semantic.ExecutionFingerprint(first)
	if err != nil {
		return migration.Summary{}, err
	}
	right, err := semantic.ExecutionFingerprint(second)
	if err != nil {
		return migration.Summary{}, err
	}
	return legacySummary(scenarioID, first, left == right, witnesses)
}

func executeLegacyNormal(ctx context.Context) ([]core.TraceRecord, error) {
	runtime, adapter, err := newLegacyRuntime()
	if err != nil {
		return nil, err
	}
	if err := startLegacyCluster(ctx, runtime); err != nil {
		return nil, err
	}
	if err := prepareLegacyCommit(ctx, runtime, "alpha"); err != nil {
		return nil, err
	}
	if len(runtime.Pending()) != 0 {
		return nil, fmt.Errorf("MIGRATION_V1_PENDING_AT_NORMAL_END: %d", len(runtime.Pending()))
	}
	if err := adapter.CheckConformance(); err != nil {
		return nil, err
	}
	return runtime.Trace(), nil
}

func executeLegacyTransport(ctx context.Context) ([]core.TraceRecord, error) {
	runtime, adapter, err := newLegacyRuntime()
	if err != nil {
		return nil, err
	}
	if err := startLegacyCluster(ctx, runtime); err != nil {
		return nil, err
	}
	campaign := runtime.Schedule(core.Event{Kind: core.EventCampaign, Target: "n1"})
	if _, err := runtime.Execute(ctx, campaign); err != nil {
		return nil, err
	}
	if err := finishLegacyBatch(ctx, runtime, "n1"); err != nil {
		return nil, err
	}
	first, err := findLegacyMessage(runtime.Pending(), "n1", "n2", "MsgVote")
	if err != nil {
		return nil, err
	}
	second, err := findLegacyMessage(runtime.Pending(), "n1", "n3", "MsgVote")
	if err != nil {
		return nil, err
	}
	if _, err := runtime.Duplicate(first.ID); err != nil {
		return nil, err
	}
	if _, err := runtime.Drop(second.ID); err != nil {
		return nil, err
	}
	if err := runtime.Run(ctx, 600); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]string{"value": "after-drop-duplicate"})
	if err != nil {
		return nil, err
	}
	if err := executeLegacyAndDrain(ctx, runtime, core.Event{
		Kind: core.EventPropose, Target: "n1", Payload: payload,
	}); err != nil {
		return nil, err
	}
	trace := runtime.Trace()
	if !legacyTraceHasKind(trace, core.EventDuplicate) || !legacyTraceHasDroppedMessage(trace) {
		return nil, fmt.Errorf("MIGRATION_V1_TRANSPORT_WITNESS_MISSING")
	}
	if len(runtime.Pending()) != 0 {
		return nil, fmt.Errorf("MIGRATION_V1_PENDING_AT_TRANSPORT_END: %d", len(runtime.Pending()))
	}
	if err := adapter.CheckConformance(); err != nil {
		return nil, err
	}
	return trace, nil
}

func executeLegacyRecovery(ctx context.Context) ([]core.TraceRecord, error) {
	runtime, adapter, err := newLegacyRuntime()
	if err != nil {
		return nil, err
	}
	if err := startLegacyCluster(ctx, runtime); err != nil {
		return nil, err
	}
	if err := prepareLegacyCommit(ctx, runtime, "alpha"); err != nil {
		return nil, err
	}
	crash := runtime.Schedule(core.Event{Kind: core.EventCrash, Target: "n2"})
	if _, err := runtime.Execute(ctx, crash); err != nil {
		return nil, err
	}
	restart := runtime.Schedule(core.Event{Kind: core.EventRestart, Target: "n2"})
	if _, err := runtime.Execute(ctx, restart); err != nil {
		return nil, err
	}
	if err := runtime.Run(ctx, 600); err != nil {
		return nil, err
	}
	if err := verifyLegacyRecoverySnapshot(runtime.Snapshot(), "n2", "alpha"); err != nil {
		return nil, err
	}
	trace := runtime.Trace()
	if !legacyTraceHasKind(trace, core.EventCrash) || !legacyTraceHasKind(trace, core.EventRestart) {
		return nil, fmt.Errorf("MIGRATION_V1_RECOVERY_WITNESS_MISSING")
	}
	if len(runtime.Pending()) != 0 {
		return nil, fmt.Errorf("MIGRATION_V1_PENDING_AT_RECOVERY_END: %d", len(runtime.Pending()))
	}
	if err := adapter.CheckConformance(); err != nil {
		return nil, err
	}
	return trace, nil
}

func prepareLegacyCommit(ctx context.Context, runtime *engine.Engine, value string) error {
	if err := executeLegacyAndDrain(ctx, runtime, core.Event{Kind: core.EventCampaign, Target: "n1"}); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"value": value})
	if err != nil {
		return err
	}
	return executeLegacyAndDrain(ctx, runtime, core.Event{
		Kind: core.EventPropose, Target: "n1", Payload: payload,
	})
}

func newLegacyRuntime() (*engine.Engine, *host.Adapter, error) {
	driver, err := etcdraft.New([]string{"n1", "n2", "n3"})
	if err != nil {
		return nil, nil, err
	}
	adapter, err := host.New(driver)
	if err != nil {
		return nil, nil, err
	}
	runtime, err := engine.New(adapter)
	if err != nil {
		return nil, nil, err
	}
	return runtime, adapter, nil
}

func startLegacyCluster(ctx context.Context, runtime *engine.Engine) error {
	for _, node := range []string{"n1", "n2", "n3"} {
		runtime.Schedule(core.Event{Kind: core.EventStart, Target: node})
	}
	if err := runtime.Run(ctx, 300); err != nil {
		return err
	}
	if len(runtime.Pending()) != 0 {
		return fmt.Errorf("MIGRATION_V1_START_PENDING: %d", len(runtime.Pending()))
	}
	return nil
}

func executeLegacyAndDrain(ctx context.Context, runtime *engine.Engine, event core.Event) error {
	id := runtime.Schedule(event)
	if _, err := runtime.Execute(ctx, id); err != nil {
		return err
	}
	if err := runtime.Run(ctx, 600); err != nil {
		return err
	}
	return nil
}

func finishLegacyBatch(ctx context.Context, runtime *engine.Engine, node string) error {
	group := ""
	for decision := 0; decision < 64; decision++ {
		var selected core.Event
		found := false
		for _, kind := range []core.EventKind{
			core.EventPersist, core.EventSync, core.EventEmit, core.EventApply, core.EventAcknowledge,
		} {
			for _, event := range runtime.Enabled() {
				if event.Kind == kind && event.Target == node && (group == "" || event.Group == group) {
					selected = event
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return fmt.Errorf("MIGRATION_V1_BATCH_OPERATION_MISSING: %s", node)
		}
		if group == "" {
			group = selected.Group
		}
		if _, err := runtime.Execute(ctx, selected.ID); err != nil {
			return err
		}
		if selected.Kind == core.EventAcknowledge {
			return nil
		}
	}
	return fmt.Errorf("MIGRATION_V1_BATCH_BOUND_EXCEEDED: %s", node)
}

func findLegacyMessage(
	events []core.Event,
	source string,
	target string,
	typeHint string,
) (core.Event, error) {
	for _, event := range events {
		if event.Kind == core.EventMessage && event.Source == source && event.Target == target &&
			event.Message != nil && event.Message.TypeHint == typeHint {
			return event, nil
		}
	}
	return core.Event{}, fmt.Errorf("MIGRATION_V1_MESSAGE_MISSING: %s/%s/%s", source, target, typeHint)
}

func legacyTraceHasKind(trace []core.TraceRecord, kind core.EventKind) bool {
	for _, record := range trace {
		if record.Event.Kind == kind {
			return true
		}
	}
	return false
}

func legacyTraceHasDroppedMessage(trace []core.TraceRecord) bool {
	for _, record := range trace {
		if record.Event.Kind == core.EventMessage && record.Outcome == "dropped" {
			return true
		}
	}
	return false
}

func verifyLegacyRecoverySnapshot(snapshot any, node string, value string) error {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	var view struct {
		Driver struct {
			Nodes map[string]struct {
				Running bool              `json:"running"`
				Epoch   uint64            `json:"epoch"`
				Values  map[string]string `json:"values"`
			} `json:"nodes"`
		} `json:"driver"`
	}
	if err := json.Unmarshal(encoded, &view); err != nil {
		return err
	}
	current, ok := view.Driver.Nodes[node]
	if !ok || !current.Running || current.Epoch != 2 {
		return fmt.Errorf("MIGRATION_V1_RECOVERY_STATE_INVALID: %s", node)
	}
	for _, committed := range current.Values {
		if committed == value {
			return nil
		}
	}
	return fmt.Errorf("MIGRATION_V1_RECOVERED_VALUE_MISSING: %s", value)
}

func legacySummary(
	scenarioID string,
	trace []core.TraceRecord,
	replayStable bool,
	witnesses []string,
) (migration.Summary, error) {
	type committed struct {
		value string
		nodes map[string]struct{}
	}
	byIndex := make(map[uint64]*committed)
	for _, record := range trace {
		for _, observation := range record.Observations {
			if observation.Kind != "commit" {
				continue
			}
			index, err := strconv.ParseUint(observation.Evidence["index"], 10, 64)
			if err != nil || index == 0 || observation.Node == "" {
				return migration.Summary{}, fmt.Errorf("MIGRATION_V1_COMMIT_EVIDENCE_INVALID")
			}
			current := byIndex[index]
			if current == nil {
				current = &committed{value: observation.Value, nodes: make(map[string]struct{})}
				byIndex[index] = current
			}
			if current.value != observation.Value {
				return migration.Summary{}, fmt.Errorf("MIGRATION_V1_COMMIT_CONFLICT: %d", index)
			}
			current.nodes[observation.Node] = struct{}{}
		}
	}
	indices := make([]uint64, 0, len(byIndex))
	for index := range byIndex {
		indices = append(indices, index)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	commands := make([]migration.Command, 0, len(indices))
	for ordinal, index := range indices {
		current := byIndex[index]
		nodes := make([]string, 0, len(current.nodes))
		for node := range current.nodes {
			nodes = append(nodes, node)
		}
		sort.Strings(nodes)
		commands = append(commands, migration.Command{
			Ordinal: ordinal + 1, ValueDigest: migration.ValueDigest([]byte(current.value)), AppliedNodes: nodes,
		})
	}
	checked := oracle.Check(trace, oracle.TraceIntegrity{}, oracle.Agreement{})
	safety := migration.Safety{}
	for _, violation := range checked.Violations {
		switch violation.Monitor {
		case "agreement":
			safety.AgreementViolations++
		case "trace-integrity":
			safety.TraceIntegrityViolations++
		}
	}
	return migration.SealSummary(migration.Summary{
		ScenarioID: scenarioID, PathID: legacyPathID,
		ImplementationID: "go.etcd.io/raft/v3@v3.6.0", ConfigurationID: legacyConfigurationID,
		Commands: commands, Safety: safety,
		Replay:    migration.Replay{Mode: "fresh-execution-fingerprint", Stable: replayStable},
		Witnesses: append([]string(nil), witnesses...),
	})
}

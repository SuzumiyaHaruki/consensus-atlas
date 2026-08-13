package controlexperiment

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

// reprojectBundleCorePSS is the trusted bridge from an execution bundle back
// to its semantic state samples. Discovery and evaluation share this helper;
// it deliberately exposes no standalone feedback-artifact model.
func reprojectBundleCorePSS(
	bundle ExecutionBundle,
	mapper psscore.SemanticMapper,
) ([]CorePSSSample, error) {
	if err := bundle.Validate(); err != nil {
		return nil, fmt.Errorf("EXPERIMENT_PSS_REPROJECTION_BUNDLE_INVALID: %w", err)
	}
	if mapper == nil || mapper.ID() == "" || mapper.ID() != bundle.Identity.PSSID {
		return nil, errors.New("EXPERIMENT_PSS_REPROJECTION_MAPPER_MISMATCH")
	}
	snapshots, err := reconstructControlSnapshots(bundle.Trace, bundle.FinalSnapshot)
	if err != nil {
		return nil, err
	}
	evidence := bundle.Trace.InitialEvidence
	projected := make([]CorePSSSample, 0, len(snapshots))
	for step, snapshot := range snapshots {
		observation, err := mapper.Map(evidence)
		if err != nil {
			return nil, err
		}
		state, err := psscore.Project(snapshot, mapper.ID(), observation)
		if err != nil {
			return nil, err
		}
		sample := CorePSSSample{Step: step, Key: state.Digest, State: state}
		if !reflect.DeepEqual(sample, bundle.CorePSS[step]) {
			return nil, fmt.Errorf("EXPERIMENT_PSS_REPROJECTION_MISMATCH: %d", step)
		}
		projected = append(projected, sample)
		if step < len(bundle.Trace.Records) && bundle.Trace.Records[step].Evidence != nil {
			evidence = *bundle.Trace.Records[step].Evidence
			evidence.Payload.Bytes = append([]byte(nil), bundle.Trace.Records[step].Evidence.Payload.Bytes...)
		}
	}
	return projected, nil
}

// reconstructControlSnapshots reverses only fields consumed by psscore.Project.
// Adapter state, entropy and offered actions are deliberately not treated as
// protocol state by Core PSS.
func reconstructControlSnapshots(
	trace controlruntime.Trace,
	final controlruntime.Snapshot,
) ([]controlruntime.Snapshot, error) {
	if len(trace.Records) != int(final.Step) {
		return nil, errors.New("EXPERIMENT_PSS_REPROJECTION_FINAL_STEP_MISMATCH")
	}
	snapshots := make([]controlruntime.Snapshot, len(trace.Records)+1)
	current := cloneControlSnapshot(final)
	snapshots[len(trace.Records)] = cloneControlSnapshot(current)
	for index := len(trace.Records) - 1; index >= 0; index-- {
		record := trace.Records[index]
		if record.Step != uint64(index+1) || current.Step != record.Step ||
			current.LogicalTime != record.LogicalTime {
			return nil, fmt.Errorf("EXPERIMENT_PSS_REPROJECTION_TRACE_STEP_MISMATCH: %d", index+1)
		}
		before := cloneControlSnapshot(current)
		before.Step = uint64(index)
		if record.ClockAdvance != nil {
			before.LogicalTime = record.ClockAdvance.From
		}
		if err := reverseNodeTransitions(&before, record.NodeTransitions); err != nil {
			return nil, err
		}
		if err := reverseItemTransitions(&before, record.ItemTransitions); err != nil {
			return nil, err
		}
		if err := reversePartitionAction(&before, record.Action); err != nil {
			return nil, err
		}
		before.Offered = nil
		current = before
		snapshots[index] = cloneControlSnapshot(current)
	}
	return snapshots, nil
}

func reverseNodeTransitions(snapshot *controlruntime.Snapshot, transitions []controlruntime.NodeTransition) error {
	for _, transition := range transitions {
		found := false
		for index := range snapshot.Nodes {
			if snapshot.Nodes[index].Ref.Node != transition.Node {
				continue
			}
			if snapshot.Nodes[index] != transition.After {
				return errors.New("EXPERIMENT_PSS_REPROJECTION_NODE_TRANSITION_MISMATCH")
			}
			snapshot.Nodes[index] = transition.Before
			found = true
			break
		}
		if !found {
			return errors.New("EXPERIMENT_PSS_REPROJECTION_NODE_TRANSITION_MISSING")
		}
	}
	return nil
}

func reverseItemTransitions(snapshot *controlruntime.Snapshot, transitions []controlruntime.ItemTransition) error {
	for _, transition := range transitions {
		index := -1
		for candidate := range snapshot.Items {
			if snapshot.Items[candidate].ID == transition.Item {
				index = candidate
				break
			}
		}
		if index < 0 || snapshot.Items[index].State != transition.After {
			return errors.New("EXPERIMENT_PSS_REPROJECTION_ITEM_TRANSITION_MISMATCH")
		}
		if transition.Before == "" {
			snapshot.Items = append(snapshot.Items[:index], snapshot.Items[index+1:]...)
			continue
		}
		snapshot.Items[index].State = transition.Before
	}
	return nil
}

func reversePartitionAction(snapshot *controlruntime.Snapshot, action control.Action) error {
	if action.Kind != control.ActionPartition && action.Kind != control.ActionHeal {
		return nil
	}
	parameters, err := control.DecodePartitionParameters(action.Parameters)
	if err != nil {
		return err
	}
	index := -1
	for candidate := range snapshot.Partitions {
		if snapshot.Partitions[candidate].ID == parameters.ID {
			index = candidate
			break
		}
	}
	if action.Kind == control.ActionPartition {
		if index < 0 {
			return errors.New("EXPERIMENT_PSS_REPROJECTION_PARTITION_MISSING")
		}
		snapshot.Partitions = append(snapshot.Partitions[:index], snapshot.Partitions[index+1:]...)
		return nil
	}
	if index >= 0 {
		return errors.New("EXPERIMENT_PSS_REPROJECTION_PARTITION_DUPLICATE")
	}
	snapshot.Partitions = append(snapshot.Partitions, controlruntime.PartitionSnapshot{
		ID: parameters.ID, Left: append([]control.NodeID(nil), parameters.Left...),
		Right: append([]control.NodeID(nil), parameters.Right...),
	})
	sort.Slice(snapshot.Partitions, func(i, j int) bool {
		return snapshot.Partitions[i].ID < snapshot.Partitions[j].ID
	})
	return nil
}

func cloneControlSnapshot(snapshot controlruntime.Snapshot) controlruntime.Snapshot {
	copySnapshot := snapshot
	copySnapshot.Nodes = append([]controlruntime.NodeSnapshot(nil), snapshot.Nodes...)
	copySnapshot.Items = make([]controlruntime.ItemSnapshot, len(snapshot.Items))
	for index, item := range snapshot.Items {
		copySnapshot.Items[index] = item
		copySnapshot.Items[index].Value = cloneProducedItem(item.Value)
	}
	copySnapshot.Partitions = make([]controlruntime.PartitionSnapshot, len(snapshot.Partitions))
	for index, partition := range snapshot.Partitions {
		copySnapshot.Partitions[index] = partition
		copySnapshot.Partitions[index].Left = append([]control.NodeID(nil), partition.Left...)
		copySnapshot.Partitions[index].Right = append([]control.NodeID(nil), partition.Right...)
	}
	copySnapshot.CloneCounts = append([]controlruntime.CloneCounter(nil), snapshot.CloneCounts...)
	copySnapshot.Offered = append([]control.Action(nil), snapshot.Offered...)
	return copySnapshot
}

func cloneProducedItem(item control.ProducedItem) control.ProducedItem {
	copyItem := item
	copyItem.Dependencies = append([]control.ItemID(nil), item.Dependencies...)
	if item.Message != nil {
		value := *item.Message
		value.Payload.Bytes = append([]byte(nil), item.Message.Payload.Bytes...)
		if item.Message.Metadata != nil {
			value.Metadata = make(map[string]string, len(item.Message.Metadata))
			for key, entry := range item.Message.Metadata {
				value.Metadata[key] = entry
			}
		}
		copyItem.Message = &value
	}
	if item.Temporal != nil {
		value := *item.Temporal
		value.Callback.Bytes = append([]byte(nil), item.Temporal.Callback.Bytes...)
		copyItem.Temporal = &value
	}
	if item.Effect != nil {
		value := *item.Effect
		value.Request.Bytes = append([]byte(nil), item.Effect.Request.Bytes...)
		value.AllowedResults = append([]string(nil), item.Effect.AllowedResults...)
		value.AllowedFailures = append([]string(nil), item.Effect.AllowedFailures...)
		copyItem.Effect = &value
	}
	if item.Callback != nil {
		value := *item.Callback
		value.Request.Bytes = append([]byte(nil), item.Callback.Request.Bytes...)
		value.AllowedResults = append([]string(nil), item.Callback.AllowedResults...)
		copyItem.Callback = &value
	}
	if item.Response != nil {
		value := *item.Response
		value.Payload.Bytes = append([]byte(nil), item.Response.Payload.Bytes...)
		copyItem.Response = &value
	}
	if item.Observation != nil {
		value := *item.Observation
		value.Payload.Bytes = append([]byte(nil), item.Observation.Payload.Bytes...)
		copyItem.Observation = &value
	}
	return copyItem
}

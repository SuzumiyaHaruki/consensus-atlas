package controlruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

var ErrInvokeNotEligible = errors.New("OFFER_INVOKE_NOT_ELIGIBLE")

func (runtime *Runtime) OfferInvoke(
	ctx context.Context,
	node control.NodeID,
	input control.PayloadEnvelope,
) (control.ActionID, error) {
	if runtime.failed != nil {
		return "", runtime.failed
	}
	if err := input.Validate(); err != nil {
		return "", err
	}
	state, ok := runtime.nodes[node]
	if !ok {
		return "", fmt.Errorf("OFFER_NODE_UNKNOWN: %s", node)
	}
	if state.Lifecycle != control.NodeRunning {
		return "", fmt.Errorf("OFFER_NODE_NOT_RUNNING: %s", node)
	}
	action, err := runtime.makeAction(control.ActionInvoke, state.Ref, "", control.AdapterInvokeParameters{Input: input})
	if err != nil {
		return "", err
	}
	command, err := runtime.buildCommand(action)
	if err != nil {
		return "", err
	}
	status, err := runtime.adapter.Check(ctx, command)
	if err != nil {
		return "", fmt.Errorf("ADAPTER_CHECK_FAILED: %w", err)
	}
	if err := status.Validate(); err != nil {
		return "", err
	}
	if err := runtime.verifyStableAudit(ctx); err != nil {
		return "", err
	}
	if !status.Eligible {
		return "", fmt.Errorf("%w: %s", ErrInvokeNotEligible, status.ReasonCode)
	}
	runtime.offered[action.ID] = action
	return action.ID, nil
}

func (runtime *Runtime) OfferPartition(left, right []control.NodeID) (control.ActionID, error) {
	parameters, err := control.NewPartitionParameters(left, right)
	if err != nil {
		return "", err
	}
	for _, node := range parameters.Left {
		if _, ok := runtime.nodes[node]; !ok {
			return "", fmt.Errorf("PARTITION_NODE_UNKNOWN: %s", node)
		}
	}
	for _, node := range parameters.Right {
		if _, ok := runtime.nodes[node]; !ok {
			return "", fmt.Errorf("PARTITION_NODE_UNKNOWN: %s", node)
		}
	}
	action, err := runtime.makeAction(control.ActionPartition, control.NodeRef{}, "", parameters)
	if err != nil {
		return "", err
	}
	runtime.offered[action.ID] = action
	return action.ID, nil
}

func (runtime *Runtime) EnabledActions(ctx context.Context) ([]control.Action, error) {
	if runtime.failed != nil {
		return nil, runtime.failed
	}
	runtime.refreshItemStates()
	actions := make([]control.Action, 0)
	for _, entry := range runtime.items {
		if entry.state != control.ItemEnabled {
			continue
		}
		switch entry.item.Kind {
		case control.ItemMessage:
			if runtime.supportsAction(control.ActionDropMessage) {
				actions = append(actions, runtime.mustAction(control.ActionDropMessage, entry.item.Owner, entry.item.ID, nil))
			}
			if runtime.supportsAction(control.ActionDuplicateMessage) &&
				runtime.cloneCounts[entry.item.ID] < runtime.maxClones {
				actions = append(actions, runtime.mustAction(control.ActionDuplicateMessage, entry.item.Owner, entry.item.ID, nil))
			}
			target := runtime.nodes[entry.item.Message.Target]
			if target.Ref.Node != "" && target.Lifecycle == control.NodeRunning &&
				!runtime.isPartitioned(entry.item.Message.Source.Node, entry.item.Message.Target) &&
				runtime.supportsAction(control.ActionDeliverMessage) {
				actions = append(actions, runtime.mustAction(control.ActionDeliverMessage, target.Ref, entry.item.ID, nil))
			}
		case control.ItemTemporal:
			if runtime.supportsAction(control.ActionFireTemporal) {
				actions = append(actions, runtime.mustAction(control.ActionFireTemporal, entry.item.Owner, entry.item.ID, nil))
			}
		case control.ItemEffect:
			if runtime.supportsAction(control.ActionCompleteEffect) {
				for _, result := range entry.item.Effect.AllowedResults {
					actions = append(actions, runtime.mustAction(control.ActionCompleteEffect, entry.item.Owner, entry.item.ID, control.AdapterResultParameters{Result: result}))
				}
			}
			if runtime.supportsAction(control.ActionFailEffect) {
				for _, result := range entry.item.Effect.AllowedFailures {
					actions = append(actions, runtime.mustAction(control.ActionFailEffect, entry.item.Owner, entry.item.ID, control.AdapterResultParameters{Result: result}))
				}
			}
		case control.ItemCallback:
			if runtime.supportsAction(control.ActionCompleteCallback) {
				for _, result := range entry.item.Callback.AllowedResults {
					actions = append(actions, runtime.mustAction(control.ActionCompleteCallback, entry.item.Owner, entry.item.ID, control.AdapterResultParameters{Result: result}))
				}
			}
		}
	}

	for _, node := range runtime.nodes {
		switch node.Lifecycle {
		case control.NodeRunning:
			if runtime.supportsAction(control.ActionCrash) {
				for _, mode := range runtime.manifest.Capabilities.CrashModes {
					actions = append(actions, runtime.mustAction(control.ActionCrash, node.Ref, "", control.AdapterModeParameters{Mode: mode}))
				}
			}
		case control.NodeStopped:
			if runtime.supportsAction(control.ActionRestart) {
				next := control.NodeRef{Node: node.Ref.Node, Incarnation: node.Ref.Incarnation + 1}
				actions = append(actions, runtime.mustAction(control.ActionRestart, next, "", nil))
			}
		}
	}
	if runtime.supportsAction(control.ActionHeal) {
		for _, value := range runtime.partitions {
			actions = append(actions, runtime.mustAction(control.ActionHeal, control.NodeRef{}, "", control.PartitionParameters{
				ID: value.id, Left: value.left, Right: value.right,
			}))
		}
	}
	for _, action := range runtime.offered {
		if runtime.offeredEnabled(action) {
			actions = append(actions, action)
		}
	}

	eligible := actions[:0]
	for _, action := range actions {
		if !runtime.adapterDirected(action.Kind) {
			status, err := runtime.adapter.CheckRuntimeAction(ctx, action)
			if err != nil {
				return nil, fmt.Errorf("ADAPTER_RUNTIME_CHECK_FAILED: %w", err)
			}
			if err := status.Validate(); err != nil {
				return nil, err
			}
			if status.Eligible {
				eligible = append(eligible, action)
			}
			continue
		}
		command, err := runtime.buildCommand(action)
		if err != nil {
			return nil, err
		}
		status, err := runtime.adapter.Check(ctx, command)
		if err != nil {
			return nil, fmt.Errorf("ADAPTER_CHECK_FAILED: %w", err)
		}
		if err := status.Validate(); err != nil {
			return nil, err
		}
		if status.Eligible {
			eligible = append(eligible, action)
		}
	}
	if err := runtime.verifyStableAudit(ctx); err != nil {
		return nil, err
	}
	sort.Slice(eligible, func(i, j int) bool { return actionKey(eligible[i]) < actionKey(eligible[j]) })
	return append([]control.Action(nil), eligible...), nil
}

func (runtime *Runtime) offeredEnabled(action control.Action) bool {
	if !runtime.supportsAction(action.Kind) {
		return false
	}
	switch action.Kind {
	case control.ActionInvoke:
		node := runtime.nodes[action.Node.Node]
		return node.Ref == action.Node && node.Lifecycle == control.NodeRunning
	case control.ActionPartition:
		parameters, err := control.DecodePartitionParameters(action.Parameters)
		return err == nil && runtime.partitions[parameters.ID] == nil
	default:
		return false
	}
}

func (runtime *Runtime) makeAction(kind control.ActionKind, node control.NodeRef, item control.ItemID, parameters any) (control.Action, error) {
	if err := kind.Validate(); err != nil {
		return control.Action{}, err
	}
	raw := json.RawMessage(nil)
	if parameters != nil {
		encoded, err := json.Marshal(parameters)
		if err != nil {
			return control.Action{}, err
		}
		raw = encoded
	}
	parameterDigest, err := control.CanonicalDigest(raw)
	if err != nil {
		return control.Action{}, err
	}
	id, err := control.StableID("action", string(kind), string(node.Node),
		strconv.FormatUint(node.Incarnation, 10), string(item), parameterDigest)
	if err != nil {
		return control.Action{}, err
	}
	return control.Action{ID: control.ActionID(id), Kind: kind, Node: node, Item: item, Parameters: raw}, nil
}

func (runtime *Runtime) mustAction(kind control.ActionKind, node control.NodeRef, item control.ItemID, parameters any) control.Action {
	action, err := runtime.makeAction(kind, node, item, parameters)
	if err != nil {
		panic(err)
	}
	return action
}

func (runtime *Runtime) buildCommand(action control.Action) (control.AdapterCommand, error) {
	logicalTime := runtime.now
	var item *control.ProducedItem
	if action.Item != "" {
		entry := runtime.items[action.Item]
		if entry == nil {
			return control.AdapterCommand{}, fmt.Errorf("ACTION_ITEM_UNKNOWN: %s", action.Item)
		}
		copyItem := cloneItem(entry.item)
		item = &copyItem
		if action.Kind == control.ActionFireTemporal {
			logicalTime = entry.item.Temporal.Deadline
			if logicalTime < runtime.now {
				logicalTime = runtime.now
			}
		}
	}
	payload, err := control.NewAdapterCommandPayload(logicalTime, action.Parameters, item)
	if err != nil {
		return control.AdapterCommand{}, err
	}
	id, err := control.StableID("command", strconv.FormatUint(runtime.step+1, 10), string(action.ID))
	if err != nil {
		return control.AdapterCommand{}, err
	}
	return control.AdapterCommand{
		ID: control.CommandID(id), Action: action.ID, Kind: action.Kind,
		Node: action.Node, Item: action.Item, Payload: payload,
	}, nil
}

func actionKey(action control.Action) string {
	return string(action.Kind) + "\x00" + string(action.Node.Node) + "\x00" +
		strconv.FormatUint(action.Node.Incarnation, 10) + "\x00" + string(action.Item) + "\x00" + string(action.Parameters)
}

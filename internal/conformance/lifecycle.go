package conformance

import (
	"context"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

// NaturalLifecyclePlan validates adapters whose own temporal/effect activity
// eventually produces a message. It contains no protocol message type or
// protocol state predicate.
type NaturalLifecyclePlan struct {
	Seed          []byte
	DecisionBound int
}

func (plan NaturalLifecyclePlan) Validate() error {
	if len(plan.Seed) == 0 {
		return errors.New("CONFORMANCE_LIFECYCLE_SEED_REQUIRED")
	}
	if plan.DecisionBound <= 0 {
		return errors.New("CONFORMANCE_LIFECYCLE_BOUND_REQUIRED")
	}
	return nil
}

// EvaluateNaturalLifecycle checks the public ownership and recovery contract
// without importing or decoding the adapted protocol.
func EvaluateNaturalLifecycle(
	ctx context.Context,
	factory Factory,
	plan NaturalLifecyclePlan,
) (Report, error) {
	if err := plan.Validate(); err != nil {
		return Report{}, err
	}
	manifest, err := readFactoryManifest(ctx, factory)
	if err != nil {
		return Report{}, err
	}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		return Report{}, err
	}
	tests := []struct {
		id         string
		capability string
		run        func(context.Context, Factory, NaturalLifecyclePlan) error
	}{
		{"natural-crash-cancels-captured", "crash-incarnation", checkNaturalCrashCancelsCaptured},
		{"natural-released-message-recovery", "runtime-owned-message", checkNaturalMessageRecovery},
	}
	report := Report{SchemaVersion: ReportSchemaVersion, ManifestDigest: manifestDigest, Passed: true}
	for _, test := range tests {
		result := CaseResult{ID: test.id, Passed: true}
		if err := test.run(ctx, factory, plan); err != nil {
			result.Passed = false
			result.ReasonCode = stableReason(err)
			report.Passed = false
		} else {
			report.ValidatedCapabilities = append(report.ValidatedCapabilities, test.capability)
		}
		report.Cases = append(report.Cases, result)
	}
	return report.Seal()
}

// EvaluateReleasedMessageLifecycle checks only Runtime ownership and
// incarnation semantics. Unlike EvaluateNaturalLifecycle, it does not require
// a controllable clock, a captured pre-release message, or strict replay.
func EvaluateReleasedMessageLifecycle(
	ctx context.Context,
	factory Factory,
	plan NaturalLifecyclePlan,
) (Report, error) {
	if err := plan.Validate(); err != nil {
		return Report{}, err
	}
	manifest, err := readFactoryManifest(ctx, factory)
	if err != nil {
		return Report{}, err
	}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		return Report{}, err
	}
	result := CaseResult{ID: "released-message-lifecycle", Passed: true}
	runtime, err := runReleasedMessageLifecycle(ctx, factory, plan)
	if err != nil {
		result.Passed = false
		result.ReasonCode = stableReason(err)
	} else if err := runtime.Close(); err != nil {
		result.Passed = false
		result.ReasonCode = stableReason(err)
	}
	report := Report{
		SchemaVersion: ReportSchemaVersion, ManifestDigest: manifestDigest,
		Cases: []CaseResult{result}, Passed: result.Passed,
	}
	if result.Passed {
		report.ValidatedCapabilities = []string{"crash-restart-incarnation", "runtime-owned-message"}
	}
	return report.Seal()
}

func checkNaturalCrashCancelsCaptured(
	ctx context.Context,
	factory Factory,
	plan NaturalLifecyclePlan,
) error {
	runtime, err := newNaturalRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	message, err := driveToNaturalMessage(ctx, runtime, control.ItemBlocked, plan.DecisionBound)
	if err != nil {
		return err
	}
	if message.Value.Message == nil || len(message.Value.Dependencies) == 0 {
		return errors.New("NATURAL_CAPTURED_MESSAGE_DEPENDENCY_MISSING")
	}
	crash, err := findNodeAction(ctx, runtime, control.ActionCrash, message.Value.Message.Source.Node)
	if err != nil {
		return err
	}
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		return err
	}
	if stateOf(runtime.Snapshot(), message.ID) != control.ItemCanceled {
		return errors.New("NATURAL_CAPTURED_MESSAGE_NOT_CANCELED")
	}
	for _, dependency := range message.Value.Dependencies {
		if stateOf(runtime.Snapshot(), dependency) != control.ItemCanceled {
			return errors.New("NATURAL_CAPTURED_DEPENDENCY_NOT_CANCELED")
		}
	}
	return replayNatural(ctx, factory, plan, runtime)
}

func checkNaturalMessageRecovery(
	ctx context.Context,
	factory Factory,
	plan NaturalLifecyclePlan,
) error {
	runtime, err := runReleasedMessageLifecycle(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	trace, err := runtime.Trace()
	if err != nil {
		return err
	}
	temporalSeen := false
	for _, record := range trace.Records {
		if record.Action.Kind == control.ActionFireTemporal {
			temporalSeen = true
			break
		}
	}
	if !temporalSeen {
		return errors.New("NATURAL_TEMPORAL_ACTION_MISSING")
	}
	return replayNatural(ctx, factory, plan, runtime)
}

func runReleasedMessageLifecycle(
	ctx context.Context,
	factory Factory,
	plan NaturalLifecyclePlan,
) (_ *controlruntime.Runtime, err error) {
	runtime, err := newNaturalRuntime(ctx, factory, plan)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, runtime.Close())
		}
	}()
	message, err := driveToNaturalMessage(ctx, runtime, control.ItemEnabled, plan.DecisionBound)
	if err != nil {
		return nil, err
	}
	if message.Value.Message == nil {
		return nil, errors.New("NATURAL_RELEASED_MESSAGE_MISSING")
	}
	source := message.Value.Message.Source.Node
	target := message.Value.Message.Target
	if source == target {
		return nil, errors.New("NATURAL_MESSAGE_ROUTE_NOT_DISTINCT")
	}
	crash, err := findNodeAction(ctx, runtime, control.ActionCrash, source)
	if err != nil {
		return nil, err
	}
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		return nil, err
	}
	if stateOf(runtime.Snapshot(), message.ID) != control.ItemEnabled {
		return nil, errors.New("NATURAL_RELEASED_MESSAGE_LOST_ON_SOURCE_CRASH")
	}
	restart, err := findNodeAction(ctx, runtime, control.ActionRestart, source)
	if err != nil {
		return nil, err
	}
	if restart.Node.Incarnation != 2 {
		return nil, errors.New("NATURAL_SOURCE_RESTART_INCARNATION_INVALID")
	}
	if _, err := runtime.Select(ctx, restart.ID); err != nil {
		return nil, err
	}
	if err := drainNaturalEffects(ctx, runtime, source, plan.DecisionBound); err != nil {
		return nil, err
	}
	crash, err = findNodeAction(ctx, runtime, control.ActionCrash, target)
	if err != nil {
		return nil, err
	}
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		return nil, err
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return nil, err
	}
	if hasAction(actions, control.ActionDeliverMessage, message.ID) ||
		!hasAction(actions, control.ActionDropMessage, message.ID) {
		return nil, errors.New("NATURAL_STOPPED_TARGET_MESSAGE_ACTIONS_INVALID")
	}
	restart, err = findNodeAction(ctx, runtime, control.ActionRestart, target)
	if err != nil {
		return nil, err
	}
	if _, err := runtime.Select(ctx, restart.ID); err != nil {
		return nil, err
	}
	if err := drainNaturalEffects(ctx, runtime, target, plan.DecisionBound); err != nil {
		return nil, err
	}
	actions, err = runtime.EnabledActions(ctx)
	if err != nil {
		return nil, err
	}
	deliver, ok := actionByItem(actions, control.ActionDeliverMessage, message.ID)
	if !ok || deliver.Node.Incarnation != 2 {
		return nil, errors.New("NATURAL_MESSAGE_NOT_DELIVERABLE_TO_RESTARTED_TARGET")
	}
	if _, err := runtime.Select(ctx, deliver.ID); err != nil {
		return nil, err
	}
	if stateOf(runtime.Snapshot(), message.ID) != control.ItemCompleted {
		return nil, errors.New("NATURAL_MESSAGE_DELIVERY_NOT_COMPLETED")
	}
	return runtime, nil
}

func newNaturalRuntime(
	ctx context.Context,
	factory Factory,
	plan NaturalLifecyclePlan,
) (*controlruntime.Runtime, error) {
	return controlruntime.New(ctx, factory(), controlruntime.Config{
		Seed: append([]byte(nil), plan.Seed...), ClockError: 0, MaxClones: 1,
	})
}

func driveToNaturalMessage(
	ctx context.Context,
	runtime *controlruntime.Runtime,
	state control.ItemState,
	bound int,
) (controlruntime.ItemSnapshot, error) {
	for decision := 0; decision < bound; decision++ {
		for _, item := range runtime.Snapshot().Items {
			if item.Kind == control.ItemMessage && item.State == state {
				return item, nil
			}
		}
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return controlruntime.ItemSnapshot{}, err
		}
		action, ok := firstNaturalProgress(actions)
		if !ok {
			return controlruntime.ItemSnapshot{}, errors.New("NATURAL_MESSAGE_PROGRESS_ACTION_MISSING")
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			return controlruntime.ItemSnapshot{}, err
		}
	}
	return controlruntime.ItemSnapshot{}, fmt.Errorf("NATURAL_MESSAGE_DECISION_BOUND_EXCEEDED")
}

func firstNaturalProgress(actions []control.Action) (control.Action, bool) {
	for _, kind := range []control.ActionKind{control.ActionCompleteEffect, control.ActionFireTemporal} {
		for _, action := range actions {
			if action.Kind == kind {
				return action, true
			}
		}
	}
	return control.Action{}, false
}

func drainNaturalEffects(
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
	return errors.New("NATURAL_EFFECT_DRAIN_BOUND_EXCEEDED")
}

func replayNatural(
	ctx context.Context,
	factory Factory,
	plan NaturalLifecyclePlan,
	runtime *controlruntime.Runtime,
) error {
	trace, err := runtime.Trace()
	if err != nil {
		return err
	}
	err = replayAndClose(ctx, factory(), controlruntime.Config{
		Seed: append([]byte(nil), plan.Seed...), ClockError: 0, MaxClones: 1,
	}, trace)
	return err
}

package conformance

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

// EvaluateIndependentControl gives temporal, message, and replay paths
// independent witnesses. It never requires lifecycle Actions or protocol
// payload knowledge.
func EvaluateIndependentControl(
	ctx context.Context,
	factory Factory,
	plan NaturalLifecyclePlan,
) (Report, error) {
	if factory == nil || factory() == nil {
		return Report{}, errors.New("CONFORMANCE_FACTORY_REQUIRED")
	}
	if err := plan.Validate(); err != nil {
		return Report{}, err
	}
	manifest, err := factory().Manifest(ctx)
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
		{"natural-temporal-progress-independent", CapabilityNaturalTemporal, checkIndependentTemporal},
		{"released-message-control", CapabilityRuntimeOwnedMessage, checkIndependentMessage},
		{"action-trace-replay", CapabilityStrictDecisionReplay, checkIndependentReplay},
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

func checkIndependentTemporal(ctx context.Context, factory Factory, plan NaturalLifecyclePlan) error {
	runtime, err := newNaturalRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	return selectIndependentTemporal(ctx, runtime, plan.DecisionBound)
}

func selectIndependentTemporal(ctx context.Context, runtime *controlruntime.Runtime, bound int) error {
	for decision := 0; decision < bound; decision++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return err
		}
		for _, action := range actions {
			if action.Kind != control.ActionFireTemporal {
				continue
			}
			record, err := runtime.Select(ctx, action.ID)
			if err != nil {
				return err
			}
			if record.ClockAdvance == nil || record.Evidence == nil || record.Yield == nil ||
				record.BeforeStateDigest == record.AfterStateDigest {
				return errors.New("INDEPENDENT_TEMPORAL_DECISION_INCOMPLETE")
			}
			return nil
		}
		progress, ok := firstNaturalProgress(actions)
		if !ok {
			return errors.New("INDEPENDENT_TEMPORAL_PROGRESS_ACTION_MISSING")
		}
		if _, err := runtime.Select(ctx, progress.ID); err != nil {
			return err
		}
	}
	return errors.New("INDEPENDENT_TEMPORAL_DECISION_BOUND_EXCEEDED")
}

func checkIndependentMessage(ctx context.Context, factory Factory, plan NaturalLifecyclePlan) error {
	runtime, err := newNaturalRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	first, err := driveToNaturalMessage(ctx, runtime, control.ItemEnabled, plan.DecisionBound)
	if err != nil {
		return err
	}
	drop, ok, err := enabledActionByItem(ctx, runtime, control.ActionDropMessage, first.ID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("INDEPENDENT_MESSAGE_DROP_MISSING")
	}
	if _, err := runtime.Select(ctx, drop.ID); err != nil {
		return err
	}
	if stateOf(runtime.Snapshot(), first.ID) != control.ItemDropped {
		return errors.New("INDEPENDENT_MESSAGE_NOT_DROPPED")
	}
	second, err := driveToNaturalMessage(ctx, runtime, control.ItemEnabled, plan.DecisionBound)
	if err != nil {
		return err
	}
	deliver, ok, err := enabledActionByItem(ctx, runtime, control.ActionDeliverMessage, second.ID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("INDEPENDENT_MESSAGE_DELIVERY_MISSING")
	}
	if _, err := runtime.Select(ctx, deliver.ID); err != nil {
		return err
	}
	if stateOf(runtime.Snapshot(), second.ID) != control.ItemCompleted {
		return errors.New("INDEPENDENT_MESSAGE_NOT_DELIVERED")
	}
	return nil
}

func checkIndependentReplay(ctx context.Context, factory Factory, plan NaturalLifecyclePlan) error {
	runtime, err := newNaturalRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	if err := selectIndependentTemporal(ctx, runtime, plan.DecisionBound); err != nil {
		return err
	}
	message, err := driveToNaturalMessage(ctx, runtime, control.ItemEnabled, plan.DecisionBound)
	if err != nil {
		return err
	}
	drop, ok, err := enabledActionByItem(ctx, runtime, control.ActionDropMessage, message.ID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("INDEPENDENT_REPLAY_DROP_MISSING")
	}
	if _, err := runtime.Select(ctx, drop.ID); err != nil {
		return err
	}
	trace, err := runtime.Trace()
	if err != nil {
		return err
	}
	_, err = controlruntime.Replay(ctx, factory(), controlruntime.Config{
		Seed: append([]byte(nil), plan.Seed...), ClockError: 0, MaxClones: 1,
	}, trace)
	return err
}

func enabledActionByItem(
	ctx context.Context,
	runtime *controlruntime.Runtime,
	kind control.ActionKind,
	item control.ItemID,
) (control.Action, bool, error) {
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return control.Action{}, false, err
	}
	action, ok := actionByItem(actions, kind, item)
	return action, ok, nil
}

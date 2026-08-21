package conformance

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

// OpaqueInvokePlan contains only Runtime-level facts. The input schema and its
// interpretation remain private to the Adapter under test.
type OpaqueInvokePlan struct {
	Seed          []byte
	Node          control.NodeID
	Input         control.PayloadEnvelope
	DecisionBound int
}

func (plan OpaqueInvokePlan) Validate() error {
	if len(plan.Seed) == 0 {
		return errors.New("CONFORMANCE_INVOKE_SEED_REQUIRED")
	}
	if plan.Node == "" {
		return errors.New("CONFORMANCE_INVOKE_NODE_REQUIRED")
	}
	if err := plan.Input.Validate(); err != nil {
		return errors.New("CONFORMANCE_INVOKE_INPUT_INVALID")
	}
	if plan.DecisionBound <= 0 {
		return errors.New("CONFORMANCE_INVOKE_BOUND_REQUIRED")
	}
	return nil
}

// EvaluateOpaqueInvoke validates that a declared opaque input can cross the
// Runtime/Adapter boundary at a stable point and that the resulting decision
// is fully replayable. It deliberately does not decode protocol evidence or
// claim that the operation has committed.
func EvaluateOpaqueInvoke(
	ctx context.Context,
	factory Factory,
	plan OpaqueInvokePlan,
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
	report := Report{SchemaVersion: ReportSchemaVersion, ManifestDigest: manifestDigest, Passed: true}
	result := CaseResult{ID: "opaque-invoke-boundary", Passed: true}
	if err := checkOpaqueInvoke(ctx, factory, plan, manifest); err != nil {
		result.Passed = false
		result.ReasonCode = stableReason(err)
		report.Passed = false
	} else {
		report.ValidatedCapabilities = append(report.ValidatedCapabilities, "opaque-invoke-boundary")
	}
	report.Cases = append(report.Cases, result)
	return report.Seal()
}

// EvaluateOpaqueInvokeAccepted validates only the opaque Runtime/Adapter
// boundary. Replay remains a separate capability and witness.
func EvaluateOpaqueInvokeAccepted(
	ctx context.Context,
	factory Factory,
	plan OpaqueInvokePlan,
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
	result := CaseResult{ID: "opaque-invoke-accepted", Passed: true}
	runtime, err := runOpaqueInvoke(ctx, factory, plan, manifest)
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
		report.ValidatedCapabilities = []string{"opaque-invoke-boundary"}
	}
	return report.Seal()
}

func checkOpaqueInvoke(
	ctx context.Context,
	factory Factory,
	plan OpaqueInvokePlan,
	manifest control.AdapterManifest,
) error {
	runtime, err := runOpaqueInvoke(ctx, factory, plan, manifest)
	if err != nil {
		return err
	}
	defer runtime.Close()
	trace, err := runtime.Trace()
	if err != nil {
		return err
	}
	err = replayAndClose(ctx, factory(), controlruntime.Config{
		Seed: append([]byte(nil), plan.Seed...), ClockError: 0, MaxClones: 1,
	}, trace)
	return err
}

func runOpaqueInvoke(
	ctx context.Context,
	factory Factory,
	plan OpaqueInvokePlan,
	manifest control.AdapterManifest,
) (_ *controlruntime.Runtime, err error) {
	if !manifestHasNode(manifest, plan.Node) {
		return nil, errors.New("OPAQUE_INVOKE_NODE_NOT_DECLARED")
	}
	if !manifestHasAction(manifest, control.ActionInvoke) {
		return nil, errors.New("OPAQUE_INVOKE_ACTION_NOT_DECLARED")
	}
	config := controlruntime.Config{
		Seed: append([]byte(nil), plan.Seed...), ClockError: 0, MaxClones: 1,
	}
	runtime, err := controlruntime.New(ctx, factory(), config)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, runtime.Close())
		}
	}()
	for decision := 0; decision < plan.DecisionBound; decision++ {
		offered, err := runtime.OfferInvoke(ctx, plan.Node, plan.Input)
		if err == nil {
			record, err := runtime.Select(ctx, offered)
			if err != nil {
				return nil, err
			}
			if record.Action.Kind != control.ActionInvoke || record.Command == nil ||
				record.Command.Kind != control.ActionInvoke || record.Yield == nil || record.Evidence == nil ||
				record.Outcome != "applied" {
				return nil, errors.New("OPAQUE_INVOKE_DECISION_INCOMPLETE")
			}
			return runtime, nil
		}
		if !errors.Is(err, controlruntime.ErrInvokeNotEligible) {
			return nil, err
		}
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return nil, err
		}
		progress, ok := firstOpaqueInvokeProgress(actions)
		if !ok {
			return nil, errors.New("OPAQUE_INVOKE_PROGRESS_ACTION_MISSING")
		}
		if _, err := runtime.Select(ctx, progress.ID); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("OPAQUE_INVOKE_DECISION_BOUND_EXCEEDED")
}

func firstOpaqueInvokeProgress(actions []control.Action) (control.Action, bool) {
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

func manifestHasNode(manifest control.AdapterManifest, wanted control.NodeID) bool {
	for _, node := range manifest.Nodes {
		if node == wanted {
			return true
		}
	}
	return false
}

func manifestHasAction(manifest control.AdapterManifest, wanted control.ActionKind) bool {
	for _, action := range manifest.Capabilities.Actions {
		if action == wanted {
			return true
		}
	}
	return false
}

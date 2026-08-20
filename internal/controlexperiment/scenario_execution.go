package controlexperiment

import (
	"context"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const ActionFrontierViewSchemaVersion = "consensus-atlas/action-frontier-view/v1"

// ActionFrontierView is the protocol-neutral exact-prefix frontier. It does
// not contain PSS, RiskWitness, target evidence, or an Agent-supplied action.
type ActionFrontierView struct {
	SchemaVersion        string              `json:"schema_version"`
	ID                   string              `json:"id"`
	PrefixDecisions      int                 `json:"prefix_decisions"`
	NextDecision         int                 `json:"next_decision"`
	PrefixTraceDigest    string              `json:"prefix_trace_digest"`
	SnapshotDigest       string              `json:"snapshot_digest"`
	RuntimeEnabledDigest string              `json:"runtime_enabled_digest"`
	AdmissibleDigest     string              `json:"admissible_digest"`
	RuntimeActionCount   int                 `json:"runtime_action_count"`
	Actions              []FrontierActionRef `json:"actions"`
	Digest               string              `json:"digest"`
}

// ScenarioExecutionWork separates frontier reconstruction, live Action
// execution, and fresh replay verification. RuntimeInitializations are
// reported but, consistently with PhaseWork, are not duplicated in WorkUnits.
type ScenarioExecutionWork struct {
	FrontierReconstruction PhaseWork `json:"frontier_reconstruction"`
	ChildMaterialization   PhaseWork `json:"child_materialization"`
	ChildVerification      PhaseWork `json:"child_verification"`
	TotalWorkUnits         int       `json:"total_work_units"`
}

func (work ScenarioExecutionWork) Validate() error {
	for _, phase := range []PhaseWork{
		work.FrontierReconstruction, work.ChildMaterialization, work.ChildVerification,
	} {
		if phase.SetupAttempts < 0 || phase.RuntimeInitializations < 0 ||
			phase.RuntimeInitializations > phase.SetupAttempts || phase.PrepareActions < 0 ||
			phase.SchedulerDecisions < 0 || phase.WorkUnits < 0 ||
			phase.SetupAttempts > int(^uint(0)>>1)-phase.PrepareActions ||
			phase.SetupAttempts+phase.PrepareActions > int(^uint(0)>>1)-phase.SchedulerDecisions ||
			phase.WorkUnits != phase.SetupAttempts+phase.PrepareActions+phase.SchedulerDecisions {
			return errors.New("EXPERIMENT_SCENARIO_EXECUTION_WORK_INVALID")
		}
	}
	total := work.FrontierReconstruction.WorkUnits
	if total > int(^uint(0)>>1)-work.ChildMaterialization.WorkUnits {
		return errors.New("EXPERIMENT_SCENARIO_EXECUTION_WORK_INVALID")
	}
	total += work.ChildMaterialization.WorkUnits
	if total > int(^uint(0)>>1)-work.ChildVerification.WorkUnits {
		return errors.New("EXPERIMENT_SCENARIO_EXECUTION_WORK_INVALID")
	}
	total += work.ChildVerification.WorkUnits
	if work.TotalWorkUnits != total {
		return errors.New("EXPERIMENT_SCENARIO_EXECUTION_WORK_INVALID")
	}
	return nil
}

func AggregateScenarioExecutionWork(values []ScenarioExecutionWork) (ScenarioExecutionWork, error) {
	var total ScenarioExecutionWork
	for _, value := range values {
		if value.Validate() != nil || !mergeScenarioExecutionPhase(
			&total.FrontierReconstruction, value.FrontierReconstruction,
		) || !mergeScenarioExecutionPhase(
			&total.ChildMaterialization, value.ChildMaterialization,
		) || !mergeScenarioExecutionPhase(
			&total.ChildVerification, value.ChildVerification,
		) {
			return ScenarioExecutionWork{}, errors.New("EXPERIMENT_SCENARIO_EXECUTION_WORK_OVERFLOW")
		}
	}
	total.TotalWorkUnits = total.FrontierReconstruction.WorkUnits
	for _, units := range []int{
		total.ChildMaterialization.WorkUnits, total.ChildVerification.WorkUnits,
	} {
		if total.TotalWorkUnits > int(^uint(0)>>1)-units {
			return ScenarioExecutionWork{}, errors.New("EXPERIMENT_SCENARIO_EXECUTION_WORK_OVERFLOW")
		}
		total.TotalWorkUnits += units
	}
	return total, total.Validate()
}

func mergeScenarioExecutionPhase(total *PhaseWork, value PhaseWork) bool {
	pairs := [][2]*int{
		{&total.SetupAttempts, &value.SetupAttempts},
		{&total.RuntimeInitializations, &value.RuntimeInitializations},
		{&total.PrepareActions, &value.PrepareActions},
		{&total.SchedulerDecisions, &value.SchedulerDecisions},
		{&total.WorkUnits, &value.WorkUnits},
	}
	for _, pair := range pairs {
		if *pair[1] < 0 || *pair[0] > int(^uint(0)>>1)-*pair[1] {
			return false
		}
		*pair[0] += *pair[1]
	}
	return true
}

// ScenarioExecutionError preserves work already performed before a frontier,
// Action, or replay failure. Callers may charge the work but cannot treat the
// partial execution as a resumable or qualified result.
type ScenarioExecutionError struct {
	Work  ScenarioExecutionWork
	cause error
}

func (failure *ScenarioExecutionError) Error() string {
	if failure == nil || failure.cause == nil {
		return "EXPERIMENT_SCENARIO_EXECUTION_FAILED"
	}
	return "EXPERIMENT_SCENARIO_EXECUTION_FAILED: " + failure.cause.Error()
}

func (failure *ScenarioExecutionError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

// ReconstructActionFrontierView replays one exact prefix on a fresh Adapter
// and exposes only the canonical Runtime/admission action boundary.
func ReconstructActionFrontierView(
	ctx context.Context,
	id string,
	trace controlruntime.Trace,
	completedDecisions int,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
) (ActionFrontierView, PhaseWork, error) {
	prefix, err := ExecutionTracePrefix(trace, completedDecisions)
	if err != nil {
		return ActionFrontierView{}, PhaseWork{}, err
	}
	view, runtime, work, err := reconstructActionFrontierPrefix(
		ctx, id, prefix, runtimeConfig, faultEnvelope, newAdapter,
	)
	if runtime != nil {
		err = errors.Join(err, runtime.Close())
	}
	return view, work, err
}

func reconstructActionFrontierPrefix(
	ctx context.Context,
	id string,
	prefix controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
) (ActionFrontierView, *controlruntime.Runtime, PhaseWork, error) {
	if !validMethodToken(id) || prefix.Validate() != nil || newAdapter == nil {
		return ActionFrontierView{}, nil, PhaseWork{}, errors.New("EXPERIMENT_ACTION_FRONTIER_INPUT_INVALID")
	}
	var work PhaseWork
	chargeSetup(&work)
	config, err := runtimeConfig.runtimeConfig()
	if err != nil {
		return ActionFrontierView{}, nil, work, err
	}
	adapter, err := newAdapter()
	if err != nil {
		return ActionFrontierView{}, nil, work, err
	}
	runtime, replay, err := controlruntime.ReplayWithProgress(ctx, adapter, config, prefix)
	if replay.RuntimeInitialized {
		chargeRuntimeInitialization(&work)
	}
	chargePrepareActions(&work, replay.PrepareActions)
	chargeDecisions(&work, replay.Decisions)
	if err != nil {
		return ActionFrontierView{}, nil, work, err
	}
	enabled, err := runtime.EnabledActions(ctx)
	if err != nil {
		return ActionFrontierView{}, nil, work, errors.Join(err, runtime.Close())
	}
	admissible := admissibleActions(
		faultEnvelope, faultUsageFromRecords(prefix.Records), enabled, runtime.Snapshot(),
	)
	view, err := newActionFrontierView(id, prefix, runtime.Snapshot(), enabled, admissible)
	if err != nil {
		return ActionFrontierView{}, nil, work, errors.Join(err, runtime.Close())
	}
	return view, runtime, work, err
}

func newActionFrontierView(
	id string,
	prefix controlruntime.Trace,
	snapshot controlruntime.Snapshot,
	runtimeEnabled []control.Action,
	admissible []control.Action,
) (ActionFrontierView, error) {
	if !validMethodToken(id) || prefix.Validate() != nil || int(snapshot.Step) != len(prefix.Records) ||
		!frontierSubset(runtimeEnabled, admissible) {
		return ActionFrontierView{}, errors.New("EXPERIMENT_ACTION_FRONTIER_VIEW_INPUT_INVALID")
	}
	runtimeDigest, err := control.CanonicalDigest(runtimeEnabled)
	if err != nil {
		return ActionFrontierView{}, err
	}
	admissibleDigest, err := control.CanonicalDigest(admissible)
	if err != nil {
		return ActionFrontierView{}, err
	}
	snapshotDigest, err := snapshot.Digest()
	if err != nil {
		return ActionFrontierView{}, err
	}
	actions, err := frontierActionRefs(admissible, snapshot)
	if err != nil {
		return ActionFrontierView{}, err
	}
	view := ActionFrontierView{
		SchemaVersion: ActionFrontierViewSchemaVersion, ID: id,
		PrefixDecisions: len(prefix.Records), NextDecision: len(prefix.Records) + 1,
		PrefixTraceDigest: prefix.Digest, SnapshotDigest: snapshotDigest,
		RuntimeEnabledDigest: runtimeDigest, AdmissibleDigest: admissibleDigest,
		RuntimeActionCount: len(runtimeEnabled), Actions: actions,
	}
	return view.seal()
}

func (view ActionFrontierView) Validate() error {
	if view.SchemaVersion != ActionFrontierViewSchemaVersion || !validMethodToken(view.ID) ||
		view.PrefixDecisions < 0 || view.NextDecision != view.PrefixDecisions+1 ||
		!validSHA256(view.PrefixTraceDigest) || !validSHA256(view.SnapshotDigest) ||
		!validSHA256(view.RuntimeEnabledDigest) || !validSHA256(view.AdmissibleDigest) ||
		view.RuntimeActionCount < len(view.Actions) {
		return errors.New("EXPERIMENT_ACTION_FRONTIER_VIEW_INVALID")
	}
	for index, action := range view.Actions {
		if action.validate() != nil || (index > 0 && view.Actions[index-1].ActionID >= action.ActionID) {
			return errors.New("EXPERIMENT_ACTION_FRONTIER_ACTIONS_INVALID")
		}
	}
	sealed, err := view.seal()
	if err != nil || !validSHA256(view.Digest) || sealed.Digest != view.Digest {
		return errors.New("EXPERIMENT_ACTION_FRONTIER_DIGEST_MISMATCH")
	}
	return nil
}

// executeScenarioChildOnLiveRuntime advances a Scenario-owned branch without
// closing it. The caller keeps exclusive ownership and must eventually close
// the Runtime and fresh-replay the promoted final Trace.
func executeScenarioChildOnLiveRuntime(
	ctx context.Context,
	wantView ActionFrontierView,
	action FrontierActionRef,
	runtime *controlruntime.Runtime,
	preparedPrefixes ...controlruntime.Trace,
) (controlruntime.Trace, PhaseWork, error) {
	var work PhaseWork
	if runtime == nil || wantView.Validate() != nil || len(preparedPrefixes) > 1 {
		return controlruntime.Trace{}, work, errors.New("EXPERIMENT_SCENARIO_LIVE_RUNTIME_INVALID")
	}
	prefix, err := runtime.Trace()
	prefixMatches := err == nil && prefix.Digest == wantView.PrefixTraceDigest &&
		len(prefix.Records) == wantView.PrefixDecisions
	if len(preparedPrefixes) == 1 {
		prefixMatches = preparedRuntimeHasExactTracePrefix(prefix, preparedPrefixes[0], wantView)
	}
	if !prefixMatches {
		return controlruntime.Trace{}, work, errors.Join(
			errors.New("EXPERIMENT_SCENARIO_LIVE_RUNTIME_DRIFT"), err,
		)
	}
	snapshotDigest, err := runtime.Snapshot().Digest()
	if err != nil || snapshotDigest != wantView.SnapshotDigest {
		return controlruntime.Trace{}, work, errors.Join(
			errors.New("EXPERIMENT_SCENARIO_LIVE_RUNTIME_DRIFT"), err,
		)
	}
	found := false
	for _, candidate := range wantView.Actions {
		if reflect.DeepEqual(candidate, action) {
			found = true
			break
		}
	}
	if !found {
		return controlruntime.Trace{}, work, errors.New("EXPERIMENT_SCENARIO_LIVE_ACTION_NOT_ADMISSIBLE")
	}
	chargeDecisions(&work, 1)
	if _, err := runtime.Select(ctx, action.ActionID); err != nil {
		return controlruntime.Trace{}, work, err
	}
	child, err := runtime.Trace()
	if err != nil || child.Validate() != nil || len(child.Records) != len(prefix.Records)+1 ||
		!scenarioTraceHasPrefix(child, prefix) {
		return controlruntime.Trace{}, work, errors.Join(
			errors.New("EXPERIMENT_SCENARIO_LIVE_CHILD_INVALID"), err,
		)
	}
	return child, work, nil
}

func preparedRuntimeHasExactTracePrefix(
	current controlruntime.Trace,
	trusted controlruntime.Trace,
	view ActionFrontierView,
) bool {
	if trusted.Validate() != nil || trusted.Digest != view.PrefixTraceDigest ||
		len(trusted.Records) != view.PrefixDecisions ||
		len(current.Records) != len(trusted.Records) {
		return false
	}
	// An Offer changes only the current state and therefore Trace's derived
	// final-state/digest fields. Every executed record and all source identity
	// fields must remain byte-for-byte equal to the trusted prefix.
	normalized := current
	normalized.FinalStateDigest = trusted.FinalStateDigest
	normalized.Digest = trusted.Digest
	return reflect.DeepEqual(normalized, trusted)
}

func verifyScenarioChild(
	ctx context.Context,
	child controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	newAdapter AdapterFactory,
) (PhaseWork, error) {
	verificationRuntime, verification, err := verifyScenarioChildRuntime(
		ctx, child, runtimeConfig, newAdapter,
	)
	if err != nil {
		if verificationRuntime != nil {
			err = errors.Join(err, verificationRuntime.Close())
		}
		return verification, err
	}
	if err := verificationRuntime.Close(); err != nil {
		return verification, err
	}
	return verification, nil
}

func verifyScenarioChildRuntime(
	ctx context.Context,
	child controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	newAdapter AdapterFactory,
) (*controlruntime.Runtime, PhaseWork, error) {
	var verification PhaseWork
	chargeSetup(&verification)
	adapter, err := newAdapter()
	if err != nil {
		return nil, verification, err
	}
	config, err := runtimeConfig.runtimeConfig()
	if err != nil {
		return nil, verification, err
	}
	verificationRuntime, replay, err := controlruntime.ReplayWithProgress(ctx, adapter, config, child)
	if replay.RuntimeInitialized {
		chargeRuntimeInitialization(&verification)
	}
	chargePrepareActions(&verification, replay.PrepareActions)
	chargeDecisions(&verification, replay.Decisions)
	if err != nil {
		return verificationRuntime, verification, err
	}
	return verificationRuntime, verification, nil
}

func addScenarioPhase(total *PhaseWork, delta PhaseWork) {
	total.SetupAttempts += delta.SetupAttempts
	total.RuntimeInitializations += delta.RuntimeInitializations
	total.PrepareActions += delta.PrepareActions
	total.SchedulerDecisions += delta.SchedulerDecisions
	updateWorkUnits(total)
}

func (view ActionFrontierView) seal() (ActionFrontierView, error) {
	view.Actions = cloneFrontierActionRefs(view.Actions)
	view.Digest = ""
	digest, err := control.CanonicalDigest(view)
	if err != nil {
		return ActionFrontierView{}, err
	}
	view.Digest = digest
	return view, nil
}

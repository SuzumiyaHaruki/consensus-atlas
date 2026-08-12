package controlexperiment

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const (
	ActionFrontierViewSchemaVersion   = "consensus-atlas/action-frontier-view/v1"
	StatelessDFSSpecSchemaVersion     = "consensus-atlas/stateless-dfs-spec/v1"
	StatelessDFSWorkItemSchemaVersion = "consensus-atlas/stateless-dfs-work-item/v1"
	StatelessDFSResultSchemaVersion   = "consensus-atlas/stateless-dfs-result/v1"
	StatelessDFSStopComplete          = "search-complete"
	StatelessDFSStopItems             = "work-item-limit"
	StatelessDFSStopWork              = "work-unit-limit"
)

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

type StatelessDFSSpec struct {
	SchemaVersion    string         `json:"schema_version"`
	ID               string         `json:"id"`
	RootPrefixDigest string         `json:"root_prefix_digest"`
	ManifestDigest   string         `json:"manifest_digest"`
	RootDecisions    int            `json:"root_decisions"`
	Runtime          RuntimeConfig  `json:"runtime"`
	FaultEnvelope    *FaultEnvelope `json:"fault_envelope,omitempty"`
	MaxDepth         int            `json:"max_depth"`
	MaxWorkItems     int            `json:"max_work_items"`
	MaxWorkUnits     int            `json:"max_work_units"`
	Digest           string         `json:"digest"`
}

type StatelessDFSStateRef struct {
	PrefixTraceDigest string `json:"prefix_trace_digest"`
	SnapshotDigest    string `json:"snapshot_digest"`
	PrefixDecisions   int    `json:"prefix_decisions"`
}

type StatelessDFSPathMetadata struct {
	ParentOrdinal int `json:"parent_ordinal"`
	Depth         int `json:"depth"`
	Decision      int `json:"decision"`
}

type StatelessDFSWorkItem struct {
	SchemaVersion     string                   `json:"schema_version"`
	SearchDigest      string                   `json:"search_digest"`
	Ordinal           int                      `json:"ordinal"`
	State             StatelessDFSStateRef     `json:"state_ref"`
	Action            FrontierActionRef        `json:"action_ref"`
	Path              StatelessDFSPathMetadata `json:"path_metadata"`
	FrontierDigest    string                   `json:"frontier_digest"`
	ChildPrefixDigest string                   `json:"child_prefix_digest"`
	ChildStateDigest  string                   `json:"child_state_digest"`
	ReplayStable      bool                     `json:"replay_stable"`
	Digest            string                   `json:"digest"`
}

// StatelessDFSWork separates search-side prefix reconstruction from the later
// measured execution. RuntimeInitializations are reported but, consistently
// with PhaseWork, are not duplicated in WorkUnits.
type StatelessDFSWork struct {
	FrontierReconstruction PhaseWork `json:"frontier_reconstruction"`
	ChildMaterialization   PhaseWork `json:"child_materialization"`
	ChildVerification      PhaseWork `json:"child_verification"`
	TotalWorkUnits         int       `json:"total_work_units"`
}

type StatelessDFSResult struct {
	SchemaVersion  string                 `json:"schema_version"`
	Spec           StatelessDFSSpec       `json:"spec"`
	Items          []StatelessDFSWorkItem `json:"items"`
	StatesExpanded int                    `json:"states_expanded"`
	Work           StatelessDFSWork       `json:"work"`
	StopReason     string                 `json:"stop_reason"`
	Digest         string                 `json:"digest"`
}

func NewStatelessDFSSpec(
	id string,
	root controlruntime.Trace,
	runtime RuntimeConfig,
	envelope *FaultEnvelope,
	maxDepth int,
	maxWorkItems int,
	maxWorkUnits int,
) (StatelessDFSSpec, error) {
	if !validMethodToken(id) || root.Validate() != nil || maxDepth <= 0 || maxWorkItems <= 0 ||
		maxWorkUnits <= 0 {
		return StatelessDFSSpec{}, errors.New("EXPERIMENT_STATELESS_DFS_SPEC_INPUT_INVALID")
	}
	if _, err := runtime.runtimeConfig(); err != nil {
		return StatelessDFSSpec{}, err
	}
	var copied *FaultEnvelope
	if envelope != nil {
		if err := envelope.Validate(); err != nil {
			return StatelessDFSSpec{}, err
		}
		value := *envelope
		copied = &value
	}
	spec := StatelessDFSSpec{
		SchemaVersion: StatelessDFSSpecSchemaVersion, ID: id,
		RootPrefixDigest: root.Digest, ManifestDigest: root.ManifestDigest,
		RootDecisions: len(root.Records), Runtime: runtime, FaultEnvelope: copied,
		MaxDepth: maxDepth, MaxWorkItems: maxWorkItems, MaxWorkUnits: maxWorkUnits,
	}
	return spec.seal()
}

func (spec StatelessDFSSpec) Validate(root controlruntime.Trace) error {
	if spec.SchemaVersion != StatelessDFSSpecSchemaVersion || !validMethodToken(spec.ID) ||
		root.Validate() != nil || spec.RootPrefixDigest != root.Digest ||
		spec.ManifestDigest != root.ManifestDigest || spec.RootDecisions != len(root.Records) ||
		spec.MaxDepth <= 0 || spec.MaxWorkItems <= 0 || spec.MaxWorkUnits <= 0 {
		return errors.New("EXPERIMENT_STATELESS_DFS_SPEC_INVALID")
	}
	if _, err := spec.Runtime.runtimeConfig(); err != nil {
		return err
	}
	if spec.FaultEnvelope != nil && spec.FaultEnvelope.Validate() != nil {
		return errors.New("EXPERIMENT_STATELESS_DFS_ENVELOPE_INVALID")
	}
	sealed, err := spec.seal()
	if err != nil || !validSHA256(spec.Digest) || sealed.Digest != spec.Digest {
		return errors.New("EXPERIMENT_STATELESS_DFS_SPEC_DIGEST_MISMATCH")
	}
	return nil
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
	view, _, work, err := reconstructActionFrontierPrefix(
		ctx, id, prefix, runtimeConfig, faultEnvelope, newAdapter,
	)
	return view, work, err
}

func ExploreBoundedStatelessDFS(
	ctx context.Context,
	spec StatelessDFSSpec,
	root controlruntime.Trace,
	newAdapter AdapterFactory,
) (StatelessDFSResult, error) {
	return exploreBoundedStatelessDFS(ctx, spec, root, newAdapter, nil)
}

type statelessTraversalOrderer func(ActionFrontierView) ([]FrontierActionRef, error)

func exploreBoundedStatelessDFS(
	ctx context.Context,
	spec StatelessDFSSpec,
	root controlruntime.Trace,
	newAdapter AdapterFactory,
	orderer statelessTraversalOrderer,
) (StatelessDFSResult, error) {
	if err := spec.Validate(root); err != nil || newAdapter == nil {
		return StatelessDFSResult{}, errors.New("EXPERIMENT_STATELESS_DFS_INPUT_INVALID")
	}
	result := StatelessDFSResult{SchemaVersion: StatelessDFSResultSchemaVersion, Spec: spec}
	stop := ""
	var visit func(controlruntime.Trace, int, int) error
	visit = func(prefix controlruntime.Trace, depth int, parentOrdinal int) error {
		if depth >= spec.MaxDepth || stop != "" {
			return nil
		}
		reconstructionEstimate := prefixReplayWork(prefix)
		if result.Work.TotalWorkUnits+reconstructionEstimate.WorkUnits > spec.MaxWorkUnits {
			stop = StatelessDFSStopWork
			return nil
		}
		viewID := fmt.Sprintf("%s-frontier-%06d", spec.ID, result.StatesExpanded+1)
		view, _, reconstruction, err := reconstructActionFrontierPrefix(
			ctx, viewID, prefix, spec.Runtime, spec.FaultEnvelope, newAdapter,
		)
		if err != nil {
			return err
		}
		if reconstruction != reconstructionEstimate {
			return errors.New("EXPERIMENT_STATELESS_DFS_RECONSTRUCTION_WORK_MISMATCH")
		}
		addDFSPhase(&result.Work.FrontierReconstruction, reconstruction)
		result.Work.TotalWorkUnits += reconstruction.WorkUnits
		result.StatesExpanded++
		actions := append([]FrontierActionRef(nil), view.Actions...)
		if orderer != nil {
			actions, err = orderer(view)
		}
		if err != nil {
			return err
		}
		for _, action := range actions {
			if len(result.Items) >= spec.MaxWorkItems {
				stop = StatelessDFSStopItems
				return nil
			}
			materializationEstimate, verificationEstimate := childWorkEstimate(prefix, action)
			needed := materializationEstimate.WorkUnits + verificationEstimate.WorkUnits
			if result.Work.TotalWorkUnits+needed > spec.MaxWorkUnits {
				stop = StatelessDFSStopWork
				return nil
			}
			child, materialization, verification, err := materializeDFSChild(
				ctx, view, action, prefix, spec.Runtime, spec.FaultEnvelope, newAdapter,
			)
			if err != nil {
				return err
			}
			if materialization != materializationEstimate || verification != verificationEstimate {
				return errors.New("EXPERIMENT_STATELESS_DFS_CHILD_WORK_MISMATCH")
			}
			item := StatelessDFSWorkItem{
				SchemaVersion: StatelessDFSWorkItemSchemaVersion, SearchDigest: spec.Digest,
				Ordinal: len(result.Items) + 1,
				State: StatelessDFSStateRef{
					PrefixTraceDigest: prefix.Digest, SnapshotDigest: view.SnapshotDigest,
					PrefixDecisions: len(prefix.Records),
				},
				Action: action,
				Path: StatelessDFSPathMetadata{
					ParentOrdinal: parentOrdinal, Depth: depth + 1, Decision: len(child.Records),
				},
				FrontierDigest: view.Digest, ChildPrefixDigest: child.Digest,
				ChildStateDigest: child.FinalStateDigest, ReplayStable: true,
			}
			item, err = item.seal()
			if err != nil {
				return err
			}
			result.Items = append(result.Items, item)
			addDFSPhase(&result.Work.ChildMaterialization, materialization)
			addDFSPhase(&result.Work.ChildVerification, verification)
			result.Work.TotalWorkUnits += needed
			if err := visit(child, depth+1, item.Ordinal); err != nil {
				return err
			}
			if stop != "" {
				return nil
			}
		}
		return nil
	}
	if err := visit(root, 0, 0); err != nil {
		return StatelessDFSResult{}, err
	}
	if stop == "" {
		stop = StatelessDFSStopComplete
	}
	result.StopReason = stop
	sealed, err := result.seal()
	if err != nil {
		return StatelessDFSResult{}, err
	}
	if err := sealed.Validate(root); err != nil {
		return StatelessDFSResult{}, err
	}
	return sealed, nil
}

// CompileStatelessDFSPath converts the trusted root prefix and one root-to-item
// chain into exact Policy rules. The fallback is used only after that path.
func CompileStatelessDFSPath(
	id string,
	result StatelessDFSResult,
	root controlruntime.Trace,
	leafOrdinal int,
	fallback []control.ActionKind,
	decisionBudget int,
) (Policy, error) {
	if err := result.Validate(root); err != nil || !validMethodToken(id) ||
		leafOrdinal <= 0 || leafOrdinal > len(result.Items) {
		return Policy{}, errors.New("EXPERIMENT_STATELESS_DFS_COMPILE_INPUT_INVALID")
	}
	var reverse []StatelessDFSWorkItem
	ordinal := leafOrdinal
	for ordinal != 0 {
		item := result.Items[ordinal-1]
		reverse = append(reverse, item)
		ordinal = item.Path.ParentOrdinal
	}
	rules := make([]DecisionRule, 0, len(root.Records)+len(reverse))
	for index, record := range root.Records {
		rules = append(rules, DecisionRule{
			Decision: index + 1, Kind: record.Action.Kind,
			Node: record.Action.Node.Node, ActionID: record.Action.ID,
		})
	}
	for index := range reverse {
		item := reverse[len(reverse)-1-index]
		rules = append(rules, DecisionRule{
			Decision: item.Path.Decision, Kind: item.Action.Kind,
			Node: item.Action.Node.Node, ActionID: item.Action.ActionID,
		})
	}
	policy := Policy{
		Version: PolicyVersion, ID: id, Rules: rules,
		Priority: append([]control.ActionKind(nil), fallback...),
	}
	if err := policy.Validate(decisionBudget); err != nil {
		return Policy{}, err
	}
	return policy, nil
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
	adapter, err := newAdapter()
	if err != nil {
		return ActionFrontierView{}, nil, work, err
	}
	config, err := runtimeConfig.runtimeConfig()
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
		return ActionFrontierView{}, nil, work, err
	}
	admissible := admissibleActions(
		faultEnvelope, faultUsageFromRecords(prefix.Records), enabled, runtime.Snapshot(),
	)
	view, err := newActionFrontierView(id, prefix, runtime.Snapshot(), enabled, admissible)
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

func materializeDFSChild(
	ctx context.Context,
	wantView ActionFrontierView,
	action FrontierActionRef,
	prefix controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
) (controlruntime.Trace, PhaseWork, PhaseWork, error) {
	view, runtime, materialization, err := reconstructActionFrontierPrefix(
		ctx, wantView.ID, prefix, runtimeConfig, faultEnvelope, newAdapter,
	)
	if err != nil {
		return controlruntime.Trace{}, materialization, PhaseWork{}, err
	}
	if view.Digest != wantView.Digest {
		return controlruntime.Trace{}, materialization, PhaseWork{},
			errors.New("EXPERIMENT_STATELESS_DFS_FRONTIER_DRIFT")
	}
	found := false
	for _, candidate := range view.Actions {
		if reflect.DeepEqual(candidate, action) {
			found = true
			break
		}
	}
	if !found {
		return controlruntime.Trace{}, materialization, PhaseWork{},
			errors.New("EXPERIMENT_STATELESS_DFS_ACTION_NOT_ADMISSIBLE")
	}
	if _, err := runtime.Select(ctx, action.ActionID); err != nil {
		return controlruntime.Trace{}, materialization, PhaseWork{}, err
	}
	chargeDecisions(&materialization, 1)
	child, err := runtime.Trace()
	if err != nil {
		return controlruntime.Trace{}, materialization, PhaseWork{}, err
	}
	var verification PhaseWork
	chargeSetup(&verification)
	adapter, err := newAdapter()
	if err != nil {
		return controlruntime.Trace{}, materialization, verification, err
	}
	config, err := runtimeConfig.runtimeConfig()
	if err != nil {
		return controlruntime.Trace{}, materialization, verification, err
	}
	_, replay, err := controlruntime.ReplayWithProgress(ctx, adapter, config, child)
	if replay.RuntimeInitialized {
		chargeRuntimeInitialization(&verification)
	}
	chargePrepareActions(&verification, replay.PrepareActions)
	chargeDecisions(&verification, replay.Decisions)
	if err != nil {
		return controlruntime.Trace{}, materialization, verification, err
	}
	return child, materialization, verification, nil
}

func prefixReplayWork(prefix controlruntime.Trace) PhaseWork {
	work := PhaseWork{SetupAttempts: 1, RuntimeInitializations: 1}
	for _, record := range prefix.Records {
		if record.Action.Kind == control.ActionInvoke || record.Action.Kind == control.ActionPartition {
			work.PrepareActions++
		}
	}
	work.SchedulerDecisions = len(prefix.Records)
	updateWorkUnits(&work)
	return work
}

func childWorkEstimate(prefix controlruntime.Trace, action FrontierActionRef) (PhaseWork, PhaseWork) {
	materialization := prefixReplayWork(prefix)
	chargeDecisions(&materialization, 1)
	verification := prefixReplayWork(prefix)
	if action.Kind == control.ActionInvoke || action.Kind == control.ActionPartition {
		chargePrepareActions(&verification, 1)
	}
	chargeDecisions(&verification, 1)
	return materialization, verification
}

func addDFSPhase(total *PhaseWork, delta PhaseWork) {
	total.SetupAttempts += delta.SetupAttempts
	total.RuntimeInitializations += delta.RuntimeInitializations
	total.PrepareActions += delta.PrepareActions
	total.SchedulerDecisions += delta.SchedulerDecisions
	updateWorkUnits(total)
}

func (result StatelessDFSResult) Validate(root controlruntime.Trace) error {
	if result.SchemaVersion != StatelessDFSResultSchemaVersion || result.Spec.Validate(root) != nil ||
		result.StatesExpanded < 0 || len(result.Items) > result.Spec.MaxWorkItems ||
		!validDFSPhaseWork(result.Work.FrontierReconstruction) ||
		!validDFSPhaseWork(result.Work.ChildMaterialization) ||
		!validDFSPhaseWork(result.Work.ChildVerification) ||
		result.Work.TotalWorkUnits > result.Spec.MaxWorkUnits ||
		result.Work.TotalWorkUnits != result.Work.FrontierReconstruction.WorkUnits+
			result.Work.ChildMaterialization.WorkUnits+result.Work.ChildVerification.WorkUnits {
		return errors.New("EXPERIMENT_STATELESS_DFS_RESULT_INVALID")
	}
	if result.StopReason != StatelessDFSStopComplete && result.StopReason != StatelessDFSStopItems &&
		result.StopReason != StatelessDFSStopWork {
		return errors.New("EXPERIMENT_STATELESS_DFS_STOP_REASON_INVALID")
	}
	if result.StopReason == StatelessDFSStopItems && len(result.Items) != result.Spec.MaxWorkItems {
		return errors.New("EXPERIMENT_STATELESS_DFS_ITEM_STOP_INVALID")
	}
	for index, item := range result.Items {
		if item.SchemaVersion != StatelessDFSWorkItemSchemaVersion || item.SearchDigest != result.Spec.Digest ||
			item.Ordinal != index+1 || item.Path.Depth <= 0 || item.Path.Depth > result.Spec.MaxDepth ||
			item.Path.Decision != item.State.PrefixDecisions+1 || item.Action.validate() != nil ||
			!validSHA256(item.State.PrefixTraceDigest) || !validSHA256(item.State.SnapshotDigest) ||
			!validSHA256(item.FrontierDigest) || !validSHA256(item.ChildPrefixDigest) ||
			!validSHA256(item.ChildStateDigest) || !item.ReplayStable {
			return errors.New("EXPERIMENT_STATELESS_DFS_WORK_ITEM_INVALID")
		}
		if item.Path.ParentOrdinal == 0 {
			if item.Path.Depth != 1 || item.State.PrefixTraceDigest != root.Digest ||
				item.State.PrefixDecisions != len(root.Records) {
				return errors.New("EXPERIMENT_STATELESS_DFS_ROOT_ITEM_INVALID")
			}
		} else if item.Path.ParentOrdinal >= item.Ordinal {
			return errors.New("EXPERIMENT_STATELESS_DFS_PARENT_INVALID")
		} else {
			parent := result.Items[item.Path.ParentOrdinal-1]
			if item.Path.Depth != parent.Path.Depth+1 ||
				item.State.PrefixTraceDigest != parent.ChildPrefixDigest ||
				item.State.PrefixDecisions != parent.Path.Decision {
				return errors.New("EXPERIMENT_STATELESS_DFS_PATH_INVALID")
			}
		}
		sealed, err := item.seal()
		if err != nil || !validSHA256(item.Digest) || sealed.Digest != item.Digest {
			return errors.New("EXPERIMENT_STATELESS_DFS_WORK_ITEM_DIGEST_MISMATCH")
		}
	}
	sealed, err := result.seal()
	if err != nil || !validSHA256(result.Digest) || sealed.Digest != result.Digest {
		return errors.New("EXPERIMENT_STATELESS_DFS_RESULT_DIGEST_MISMATCH")
	}
	return nil
}

func validDFSPhaseWork(work PhaseWork) bool {
	return work.SetupAttempts >= 0 && work.RuntimeInitializations >= 0 &&
		work.RuntimeInitializations <= work.SetupAttempts && work.PrepareActions >= 0 &&
		work.SchedulerDecisions >= 0 &&
		work.WorkUnits == work.SetupAttempts+work.PrepareActions+work.SchedulerDecisions
}

func (view ActionFrontierView) seal() (ActionFrontierView, error) {
	view.Actions = append([]FrontierActionRef(nil), view.Actions...)
	view.Digest = ""
	digest, err := control.CanonicalDigest(view)
	if err != nil {
		return ActionFrontierView{}, err
	}
	view.Digest = digest
	return view, nil
}

func (spec StatelessDFSSpec) seal() (StatelessDFSSpec, error) {
	if spec.FaultEnvelope != nil {
		value := *spec.FaultEnvelope
		spec.FaultEnvelope = &value
	}
	spec.Digest = ""
	digest, err := control.CanonicalDigest(spec)
	if err != nil {
		return StatelessDFSSpec{}, err
	}
	spec.Digest = digest
	return spec, nil
}

func (item StatelessDFSWorkItem) seal() (StatelessDFSWorkItem, error) {
	item.Digest = ""
	digest, err := control.CanonicalDigest(item)
	if err != nil {
		return StatelessDFSWorkItem{}, err
	}
	item.Digest = digest
	return item, nil
}

func (result StatelessDFSResult) seal() (StatelessDFSResult, error) {
	result.Items = append([]StatelessDFSWorkItem(nil), result.Items...)
	result.Digest = ""
	digest, err := control.CanonicalDigest(result)
	if err != nil {
		return StatelessDFSResult{}, err
	}
	result.Digest = digest
	return result, nil
}

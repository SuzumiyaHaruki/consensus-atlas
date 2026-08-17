package controlexperiment

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	RiskFrontierViewSchemaVersion = "consensus-atlas/risk-frontier-view/v1"
	FrontierChoiceSchemaVersion   = "consensus-atlas/frontier-choice/v1"
)

// FrontierActionRef exposes only common control semantics. The exact Action
// remains bound by ActionDigest, while payload and protocol evidence stay out
// of the planner-facing view.
type FrontierActionRef struct {
	ActionID      control.ActionID        `json:"action_id"`
	ActionDigest  string                  `json:"action_digest"`
	Kind          control.ActionKind      `json:"kind"`
	Node          control.NodeRef         `json:"node_ref,omitempty"`
	ItemID        control.ItemID          `json:"item_id,omitempty"`
	ItemKind      control.ItemKind        `json:"item_kind,omitempty"`
	Owner         control.NodeRef         `json:"owner,omitempty"`
	MessageSource control.NodeRef         `json:"message_source,omitempty"`
	MessageTarget control.NodeID          `json:"message_target,omitempty"`
	TemporalKind  control.TemporalKind    `json:"temporal_kind,omitempty"`
	EffectKind    string                  `json:"effect_kind,omitempty"`
	Durability    control.DurabilityClass `json:"durability,omitempty"`
}

type RiskFrontierView struct {
	SchemaVersion        string                       `json:"schema_version"`
	ID                   string                       `json:"id"`
	Progress             semantic.RiskWitnessProgress `json:"progress"`
	PrefixDecisions      int                          `json:"prefix_decisions"`
	NextDecision         int                          `json:"next_decision"`
	PrefixTraceDigest    string                       `json:"prefix_trace_digest"`
	SnapshotDigest       string                       `json:"snapshot_digest"`
	RuntimeEnabledDigest string                       `json:"runtime_enabled_digest"`
	AdmissibleDigest     string                       `json:"admissible_digest"`
	RuntimeActionCount   int                          `json:"runtime_action_count"`
	Actions              []FrontierActionRef          `json:"actions"`
	Digest               string                       `json:"digest"`
}

type FrontierActionSelector struct {
	ActionID       control.ActionID        `json:"action_id,omitempty"`
	Kind           control.ActionKind      `json:"kind,omitempty"`
	Node           control.NodeID          `json:"node,omitempty"`
	ItemKind       control.ItemKind        `json:"item_kind,omitempty"`
	Owner          control.NodeID          `json:"owner,omitempty"`
	MessageSource  control.NodeID          `json:"message_source,omitempty"`
	MessageTarget  control.NodeID          `json:"message_target,omitempty"`
	TemporalKind   control.TemporalKind    `json:"temporal_kind,omitempty"`
	EffectKind     string                  `json:"effect_kind,omitempty"`
	Durability     control.DurabilityClass `json:"durability,omitempty"`
	ActorRole      string                  `json:"actor_role,omitempty"`
	MessageClass   string                  `json:"message_class,omitempty"`
	EpochRelation  string                  `json:"epoch_relation,omitempty"`
	OperationState string                  `json:"operation_state,omitempty"`
}

type FrontierChoice struct {
	SchemaVersion string            `json:"schema_version"`
	ID            string            `json:"id"`
	ViewDigest    string            `json:"view_digest"`
	Decision      int               `json:"decision"`
	Action        FrontierActionRef `json:"action"`
	Digest        string            `json:"digest"`
}

// ExecutionTracePrefix creates the exact trace accepted by strict Replay for
// the first completedDecisions records. It does not synthesize actions.
func ExecutionTracePrefix(
	trace controlruntime.Trace,
	completedDecisions int,
) (controlruntime.Trace, error) {
	if err := trace.Validate(); err != nil {
		return controlruntime.Trace{}, err
	}
	if completedDecisions < 0 || completedDecisions > len(trace.Records) {
		return controlruntime.Trace{}, errors.New("EXPERIMENT_FRONTIER_PREFIX_INVALID")
	}
	prefix := trace
	prefix.Records = append([]controlruntime.ActionRecord(nil), trace.Records[:completedDecisions]...)
	prefix.FinalStateDigest = prefix.InitialStateDigest
	if completedDecisions > 0 {
		prefix.FinalStateDigest = prefix.Records[completedDecisions-1].AfterStateDigest
	}
	sealed, err := prefix.Seal()
	if err != nil {
		return controlruntime.Trace{}, err
	}
	return sealed, sealed.Validate()
}

// ReconstructRiskFrontierView replays a trusted prefix using a fresh Adapter,
// then observes the actual next enabled/admissible frontier. Returned work is
// explicit reconstruction cost, not free planner computation.
func ReconstructRiskFrontierView(
	ctx context.Context,
	id string,
	spec semantic.RiskWitnessSpec,
	result semantic.RiskWitnessResult,
	trace controlruntime.Trace,
	completedDecisions int,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
) (RiskFrontierView, PhaseWork, error) {
	view, _, work, err := ReconstructRiskFrontierState(
		ctx, id, spec, result, trace, completedDecisions, runtimeConfig, faultEnvelope, newAdapter,
	)
	return view, work, err
}

// ReconstructRiskFrontierState additionally returns the trusted Runtime
// snapshot for target-local semantic projection. The planner never receives
// this raw snapshot.
func ReconstructRiskFrontierState(
	ctx context.Context,
	id string,
	spec semantic.RiskWitnessSpec,
	result semantic.RiskWitnessResult,
	trace controlruntime.Trace,
	completedDecisions int,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
) (RiskFrontierView, controlruntime.Snapshot, PhaseWork, error) {
	view, snapshot, runtime, work, err := reconstructRiskFrontierRuntime(
		ctx, id, spec, result, trace, completedDecisions, runtimeConfig, faultEnvelope, newAdapter,
	)
	if err != nil {
		return RiskFrontierView{}, controlruntime.Snapshot{}, work, err
	}
	if err := runtime.Close(); err != nil {
		return RiskFrontierView{}, controlruntime.Snapshot{}, work, err
	}
	return view, snapshot, work, nil
}

// reconstructRiskFrontierRuntime is the Scenario-only prepared form of
// ReconstructRiskFrontierState. The caller must either consume the returned
// Runtime with materializeDFSChildFromRuntime or close it without exposing
// any state derived after this frontier.
func reconstructRiskFrontierRuntime(
	ctx context.Context,
	id string,
	spec semantic.RiskWitnessSpec,
	result semantic.RiskWitnessResult,
	trace controlruntime.Trace,
	completedDecisions int,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
) (RiskFrontierView, controlruntime.Snapshot, *controlruntime.Runtime, PhaseWork, error) {
	prefix, err := ExecutionTracePrefix(trace, completedDecisions)
	if err != nil {
		return RiskFrontierView{}, controlruntime.Snapshot{}, nil, PhaseWork{}, err
	}
	if result.ExecutionDigest != prefix.Digest || result.TargetIdentityDigest != prefix.ManifestDigest {
		return RiskFrontierView{}, controlruntime.Snapshot{}, nil, PhaseWork{}, errors.New("EXPERIMENT_FRONTIER_PROGRESS_PREFIX_MISMATCH")
	}
	progress, err := semantic.NewRiskWitnessProgress(spec, result)
	if err != nil {
		return RiskFrontierView{}, controlruntime.Snapshot{}, nil, PhaseWork{}, err
	}
	frontier, runtime, work, err := reconstructActionFrontierPrefix(
		ctx, id, prefix, runtimeConfig, faultEnvelope, newAdapter,
	)
	if err != nil {
		if runtime != nil {
			err = errors.Join(err, runtime.Close())
		}
		return RiskFrontierView{}, controlruntime.Snapshot{}, nil, work, err
	}
	snapshot := runtime.Snapshot()
	view, err := newRiskFrontierView(id, spec, progress, frontier)
	if err != nil {
		return RiskFrontierView{}, controlruntime.Snapshot{}, nil, work, errors.Join(err, runtime.Close())
	}
	return view, snapshot, runtime, work, nil
}

func projectRiskFrontierFromLiveRuntime(
	ctx context.Context,
	id string,
	spec semantic.RiskWitnessSpec,
	result semantic.RiskWitnessResult,
	trace controlruntime.Trace,
	faultEnvelope *FaultEnvelope,
	runtime *controlruntime.Runtime,
) (RiskFrontierView, controlruntime.Snapshot, error) {
	if runtime == nil || result.ExecutionDigest != trace.Digest ||
		result.TargetIdentityDigest != trace.ManifestDigest {
		return RiskFrontierView{}, controlruntime.Snapshot{},
			errors.New("EXPERIMENT_SCENARIO_LIVE_FRONTIER_INPUT_INVALID")
	}
	current, err := runtime.Trace()
	if err != nil || current.Digest != trace.Digest || len(current.Records) != len(trace.Records) {
		return RiskFrontierView{}, controlruntime.Snapshot{}, errors.Join(
			errors.New("EXPERIMENT_SCENARIO_LIVE_FRONTIER_DRIFT"), err,
		)
	}
	progress, err := semantic.NewRiskWitnessProgress(spec, result)
	if err != nil {
		return RiskFrontierView{}, controlruntime.Snapshot{}, err
	}
	enabled, err := runtime.EnabledActions(ctx)
	if err != nil {
		return RiskFrontierView{}, controlruntime.Snapshot{}, err
	}
	snapshot := runtime.Snapshot()
	admissible := admissibleActions(
		faultEnvelope, faultUsageFromRecords(trace.Records), enabled, snapshot,
	)
	frontier, err := newActionFrontierView(id, trace, snapshot, enabled, admissible)
	if err != nil {
		return RiskFrontierView{}, controlruntime.Snapshot{}, err
	}
	view, err := newRiskFrontierView(id, spec, progress, frontier)
	return view, snapshot, err
}

func newRiskFrontierView(
	id string,
	spec semantic.RiskWitnessSpec,
	progress semantic.RiskWitnessProgress,
	frontier ActionFrontierView,
) (RiskFrontierView, error) {
	if !validMethodToken(id) || progress.Validate(spec) != nil || frontier.Validate() != nil ||
		progress.EvidencePrefixDigest != frontier.PrefixTraceDigest || id != frontier.ID {
		return RiskFrontierView{}, errors.New("EXPERIMENT_FRONTIER_VIEW_INPUT_INVALID")
	}
	view := RiskFrontierView{
		SchemaVersion: RiskFrontierViewSchemaVersion, ID: id, Progress: progress,
		PrefixDecisions: frontier.PrefixDecisions, NextDecision: frontier.NextDecision,
		PrefixTraceDigest: frontier.PrefixTraceDigest, SnapshotDigest: frontier.SnapshotDigest,
		RuntimeEnabledDigest: frontier.RuntimeEnabledDigest, AdmissibleDigest: frontier.AdmissibleDigest,
		RuntimeActionCount: frontier.RuntimeActionCount,
		Actions:            append([]FrontierActionRef(nil), frontier.Actions...),
	}
	sealed, err := view.seal()
	if err != nil {
		return RiskFrontierView{}, err
	}
	if err := sealed.Validate(spec); err != nil {
		return RiskFrontierView{}, err
	}
	return sealed, nil
}

func (view RiskFrontierView) Validate(spec semantic.RiskWitnessSpec) error {
	if view.SchemaVersion != RiskFrontierViewSchemaVersion || !validMethodToken(view.ID) ||
		view.Progress.Validate(spec) != nil || view.PrefixDecisions < 0 ||
		view.NextDecision != view.PrefixDecisions+1 ||
		view.Progress.EvidencePrefixDigest != view.PrefixTraceDigest ||
		!validSHA256(view.PrefixTraceDigest) || !validSHA256(view.SnapshotDigest) ||
		!validSHA256(view.RuntimeEnabledDigest) || !validSHA256(view.AdmissibleDigest) ||
		view.RuntimeActionCount < len(view.Actions) {
		return errors.New("EXPERIMENT_FRONTIER_VIEW_INVALID")
	}
	for index, action := range view.Actions {
		if err := action.validate(); err != nil ||
			(index > 0 && view.Actions[index-1].ActionID >= action.ActionID) {
			return errors.New("EXPERIMENT_FRONTIER_ACTIONS_INVALID")
		}
	}
	sealed, err := view.seal()
	if err != nil || !validSHA256(view.Digest) || sealed.Digest != view.Digest {
		return errors.New("EXPERIMENT_FRONTIER_VIEW_DIGEST_MISMATCH")
	}
	return nil
}

func ChooseFirstFrontierAction(
	id string,
	view RiskFrontierView,
	spec semantic.RiskWitnessSpec,
	selectors []FrontierActionSelector,
) (FrontierChoice, error) {
	if err := view.Validate(spec); err != nil || !validMethodToken(id) || len(selectors) == 0 {
		return FrontierChoice{}, errors.New("EXPERIMENT_FRONTIER_SELECTOR_INVALID")
	}
	for _, selector := range selectors {
		if err := selector.validate(); err != nil {
			return FrontierChoice{}, err
		}
		for _, action := range view.Actions {
			if selector.matches(action) {
				return NewFrontierChoice(id, view, spec, action.ActionID)
			}
		}
	}
	return FrontierChoice{}, errors.New("EXPERIMENT_FRONTIER_SELECTOR_NO_MATCH")
}

func (selector FrontierActionSelector) validate() error {
	if selector.ActionID != "" {
		if selector.Kind != "" || selector.Node != "" || selector.ItemKind != "" || selector.Owner != "" ||
			selector.MessageSource != "" || selector.MessageTarget != "" || selector.TemporalKind != "" ||
			selector.EffectKind != "" || selector.Durability != "" || selector.ActorRole != "" ||
			selector.MessageClass != "" || selector.EpochRelation != "" || selector.OperationState != "" {
			return errors.New("EXPERIMENT_FRONTIER_SELECTOR_EXACT_MIXED")
		}
		return nil
	}
	if selector.Kind.Validate() != nil {
		return errors.New("EXPERIMENT_FRONTIER_SELECTOR_KIND_INVALID")
	}
	if selector.ItemKind != "" && selector.ItemKind.Validate() != nil {
		return errors.New("EXPERIMENT_FRONTIER_SELECTOR_ITEM_INVALID")
	}
	if selector.TemporalKind != "" && selector.TemporalKind.Validate() != nil {
		return errors.New("EXPERIMENT_FRONTIER_SELECTOR_TEMPORAL_INVALID")
	}
	if selector.Durability != "" && selector.Durability != control.DurabilityVolatile &&
		selector.Durability != control.DurabilityVisible && selector.Durability != control.DurabilityDurable &&
		selector.Durability != control.DurabilityApplied {
		return errors.New("EXPERIMENT_FRONTIER_SELECTOR_DURABILITY_INVALID")
	}
	if selector.ActorRole != "" && (selector.ActorRole == ConsensusSemanticUnknown ||
		!validConsensusActorRole(selector.ActorRole)) ||
		selector.MessageClass != "" && (selector.MessageClass == ConsensusSemanticUnknown ||
			!validConsensusMessageClass(selector.MessageClass)) ||
		selector.EpochRelation != "" && (selector.EpochRelation == ConsensusSemanticUnknown ||
			!validConsensusEpochRelation(selector.EpochRelation)) ||
		selector.OperationState != "" && (selector.OperationState == ConsensusSemanticUnknown ||
			!validConsensusOperationState(selector.OperationState)) {
		return errors.New("EXPERIMENT_FRONTIER_SELECTOR_SEMANTICS_INVALID")
	}
	return nil
}

func (selector FrontierActionSelector) matches(action FrontierActionRef, hints ...ConsensusActionHint) bool {
	if selector.ActionID != "" {
		return selector.ActionID == action.ActionID
	}
	matched := selector.Kind == action.Kind &&
		(selector.Node == "" || selector.Node == action.Node.Node) &&
		(selector.ItemKind == "" || selector.ItemKind == action.ItemKind) &&
		(selector.Owner == "" || selector.Owner == action.Owner.Node) &&
		(selector.MessageSource == "" || selector.MessageSource == action.MessageSource.Node) &&
		(selector.MessageTarget == "" || selector.MessageTarget == action.MessageTarget) &&
		(selector.TemporalKind == "" || selector.TemporalKind == action.TemporalKind) &&
		(selector.EffectKind == "" || selector.EffectKind == action.EffectKind) &&
		(selector.Durability == "" || selector.Durability == action.Durability)
	if !matched || selector.ActorRole == "" && selector.MessageClass == "" &&
		selector.EpochRelation == "" && selector.OperationState == "" {
		return matched
	}
	if len(hints) != 1 || hints[0].ActionID != action.ActionID ||
		hints[0].ActionDigest != action.ActionDigest {
		return false
	}
	hint := hints[0]
	return (selector.ActorRole == "" || selector.ActorRole == hint.ActorRole) &&
		(selector.MessageClass == "" || selector.MessageClass == hint.MessageClass) &&
		(selector.EpochRelation == "" || selector.EpochRelation == hint.EpochRelation) &&
		(selector.OperationState == "" || selector.OperationState == hint.OperationState)
}

func NewFrontierChoice(
	id string,
	view RiskFrontierView,
	spec semantic.RiskWitnessSpec,
	actionID control.ActionID,
) (FrontierChoice, error) {
	if err := view.Validate(spec); err != nil || !validMethodToken(id) {
		return FrontierChoice{}, errors.New("EXPERIMENT_FRONTIER_CHOICE_INPUT_INVALID")
	}
	for _, action := range view.Actions {
		if action.ActionID != actionID {
			continue
		}
		choice := FrontierChoice{
			SchemaVersion: FrontierChoiceSchemaVersion, ID: id,
			ViewDigest: view.Digest, Decision: view.NextDecision, Action: action,
		}
		return choice.seal()
	}
	return FrontierChoice{}, errors.New("EXPERIMENT_FRONTIER_CHOICE_NOT_ADMISSIBLE")
}

func (choice FrontierChoice) Validate(view RiskFrontierView, spec semantic.RiskWitnessSpec) error {
	if err := view.Validate(spec); err != nil || choice.SchemaVersion != FrontierChoiceSchemaVersion ||
		!validMethodToken(choice.ID) || choice.ViewDigest != view.Digest ||
		choice.Decision != view.NextDecision || choice.Action.validate() != nil {
		return errors.New("EXPERIMENT_FRONTIER_CHOICE_INVALID")
	}
	found := false
	for _, action := range view.Actions {
		if reflect.DeepEqual(action, choice.Action) {
			found = true
			break
		}
	}
	if !found {
		return errors.New("EXPERIMENT_FRONTIER_CHOICE_NOT_ADMISSIBLE")
	}
	sealed, err := choice.seal()
	if err != nil || !validSHA256(choice.Digest) || sealed.Digest != choice.Digest {
		return errors.New("EXPERIMENT_FRONTIER_CHOICE_DIGEST_MISMATCH")
	}
	return nil
}

func CompileFrontierChoices(
	id string,
	spec semantic.RiskWitnessSpec,
	views []RiskFrontierView,
	choices []FrontierChoice,
	fallback []control.ActionKind,
	decisionBudget int,
) (Policy, error) {
	if len(views) == 0 || len(views) != len(choices) {
		return Policy{}, errors.New("EXPERIMENT_FRONTIER_COMPILE_INPUT_INVALID")
	}
	rules := make([]DecisionRule, len(choices))
	lastDecision := 0
	for index, choice := range choices {
		if err := choice.Validate(views[index], spec); err != nil || choice.Decision <= lastDecision {
			return Policy{}, errors.New("EXPERIMENT_FRONTIER_COMPILE_CHOICE_INVALID")
		}
		lastDecision = choice.Decision
		rules[index] = DecisionRule{
			Decision: choice.Decision, Kind: choice.Action.Kind,
			Node: choice.Action.Node.Node, ActionID: choice.Action.ActionID,
		}
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

func (action FrontierActionRef) validate() error {
	if action.ActionID == "" || !validSHA256(action.ActionDigest) || action.Kind.Validate() != nil {
		return errors.New("EXPERIMENT_FRONTIER_ACTION_INVALID")
	}
	if (action.Node.Node == "") != (action.Node.Incarnation == 0) {
		return errors.New("EXPERIMENT_FRONTIER_ACTION_NODE_INVALID")
	}
	if action.Node.Node != "" && action.Node.Validate() != nil {
		return errors.New("EXPERIMENT_FRONTIER_ACTION_NODE_INVALID")
	}
	if action.ItemID == "" {
		if action.ItemKind != "" || action.Owner.Node != "" || action.MessageSource.Node != "" ||
			action.MessageTarget != "" || action.TemporalKind != "" || action.EffectKind != "" ||
			action.Durability != "" {
			return errors.New("EXPERIMENT_FRONTIER_ACTION_ITEM_INVALID")
		}
		return nil
	}
	if action.ItemKind.Validate() != nil || action.Owner.Validate() != nil {
		return errors.New("EXPERIMENT_FRONTIER_ACTION_ITEM_INVALID")
	}
	return nil
}

func frontierActionRefs(
	actions []control.Action,
	snapshot controlruntime.Snapshot,
) ([]FrontierActionRef, error) {
	items := make(map[control.ItemID]controlruntime.ItemSnapshot, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items[item.ID] = item
	}
	refs := make([]FrontierActionRef, 0, len(actions))
	for _, action := range actions {
		digest, err := control.CanonicalDigest(action)
		if err != nil {
			return nil, err
		}
		ref := FrontierActionRef{
			ActionID: action.ID, ActionDigest: digest, Kind: action.Kind,
			Node: action.Node, ItemID: action.Item,
		}
		if action.Item != "" {
			item, ok := items[action.Item]
			if !ok {
				return nil, errors.New("EXPERIMENT_FRONTIER_ACTION_ITEM_MISSING")
			}
			ref.ItemKind, ref.Owner = item.Kind, item.Owner
			if item.Value.Message != nil {
				ref.MessageSource = item.Value.Message.Source
				ref.MessageTarget = item.Value.Message.Target
			}
			if item.Value.Temporal != nil {
				ref.TemporalKind = item.Value.Temporal.Kind
			}
			if item.Value.Effect != nil {
				ref.EffectKind = item.Value.Effect.Kind
				ref.Durability = item.Value.Effect.Durability
			}
		}
		if err := ref.validate(); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ActionID < refs[j].ActionID })
	for index := 1; index < len(refs); index++ {
		if refs[index-1].ActionID == refs[index].ActionID {
			return nil, errors.New("EXPERIMENT_FRONTIER_ACTION_DUPLICATE")
		}
	}
	return refs, nil
}

func frontierSubset(runtimeEnabled, admissible []control.Action) bool {
	runtimeActions := make(map[control.ActionID]string, len(runtimeEnabled))
	for _, action := range runtimeEnabled {
		digest, err := control.CanonicalDigest(action)
		if err != nil || action.ID == "" {
			return false
		}
		runtimeActions[action.ID] = digest
	}
	for _, action := range admissible {
		digest, err := control.CanonicalDigest(action)
		if err != nil || runtimeActions[action.ID] != digest {
			return false
		}
	}
	return true
}

func (view RiskFrontierView) seal() (RiskFrontierView, error) {
	view.Actions = append([]FrontierActionRef(nil), view.Actions...)
	view.Digest = ""
	digest, err := control.CanonicalDigest(view)
	if err != nil {
		return RiskFrontierView{}, err
	}
	view.Digest = digest
	return view, nil
}

func (choice FrontierChoice) seal() (FrontierChoice, error) {
	choice.Digest = ""
	digest, err := control.CanonicalDigest(choice)
	if err != nil {
		return FrontierChoice{}, err
	}
	choice.Digest = digest
	return choice, nil
}

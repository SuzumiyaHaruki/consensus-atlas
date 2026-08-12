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
	Kind control.ActionKind `json:"kind"`
	Node control.NodeID     `json:"node,omitempty"`
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
	prefix, err := ExecutionTracePrefix(trace, completedDecisions)
	if err != nil {
		return RiskFrontierView{}, PhaseWork{}, err
	}
	if result.ExecutionDigest != prefix.Digest || result.TargetIdentityDigest != prefix.ManifestDigest {
		return RiskFrontierView{}, PhaseWork{}, errors.New("EXPERIMENT_FRONTIER_PROGRESS_PREFIX_MISMATCH")
	}
	progress, err := semantic.NewRiskWitnessProgress(spec, result)
	if err != nil {
		return RiskFrontierView{}, PhaseWork{}, err
	}
	frontier, work, err := ReconstructActionFrontierView(
		ctx, id, trace, completedDecisions, runtimeConfig, faultEnvelope, newAdapter,
	)
	if err != nil {
		return RiskFrontierView{}, work, err
	}
	view, err := newRiskFrontierView(id, spec, progress, frontier)
	return view, work, err
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
		if err := selector.Kind.Validate(); err != nil {
			return FrontierChoice{}, err
		}
		for _, action := range view.Actions {
			if action.Kind == selector.Kind && (selector.Node == "" || action.Node.Node == selector.Node) {
				return NewFrontierChoice(id, view, spec, action.ActionID)
			}
		}
	}
	return FrontierChoice{}, errors.New("EXPERIMENT_FRONTIER_SELECTOR_NO_MATCH")
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

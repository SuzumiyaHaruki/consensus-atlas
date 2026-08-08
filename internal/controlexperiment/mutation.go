package controlexperiment

import (
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const (
	TraceMutationPlanVersion   = "consensus-atlas/adjacent-trace-mutation/v1"
	TraceMutationPlanVersionV2 = "consensus-atlas/adjacent-trace-mutation/v2"
	TraceMutationOperator      = "adjacent-swap-with-priority-suffix"
)

// ActionOccurrenceRef identifies one source-trace occurrence. ActionID alone
// is insufficient when a Runtime action becomes enabled more than once across
// a state cycle.
type ActionOccurrenceRef struct {
	ActionID   control.ActionID `json:"action_id"`
	Occurrence int              `json:"occurrence"`
}

// TraceMutationPlan replays the exact source prefix, swaps one adjacent pair,
// then deliberately switches to a declared deterministic priority suffix. It
// never substitutes another Action when an exact splice Action is unavailable.
type TraceMutationPlan struct {
	SchemaVersion      string                `json:"schema_version"`
	ID                 string                `json:"id"`
	Operator           string                `json:"operator"`
	SeedTraceDigest    string                `json:"seed_trace_digest"`
	SourcePolicyDigest string                `json:"source_policy_digest"`
	SourceEntryDigest  string                `json:"source_entry_digest,omitempty"`
	GuidanceDigest     string                `json:"guidance_digest,omitempty"`
	FirstDecision      int                   `json:"first_decision"`
	PrefixActionIDs    []control.ActionID    `json:"prefix_action_ids,omitempty"`
	PrefixActionRefs   []ActionOccurrenceRef `json:"prefix_action_refs,omitempty"`
	FirstActionID      control.ActionID      `json:"first_action_id"`
	SecondActionID     control.ActionID      `json:"second_action_id"`
	FirstActionRef     *ActionOccurrenceRef  `json:"first_action_ref,omitempty"`
	SecondActionRef    *ActionOccurrenceRef  `json:"second_action_ref,omitempty"`
	SuffixPriority     []control.ActionKind  `json:"suffix_priority"`
	Digest             string                `json:"digest"`
}

func NewAdjacentTraceMutation(
	id string,
	trace controlruntime.Trace,
	source Policy,
	firstDecision int,
) (TraceMutationPlan, error) {
	if err := trace.Validate(); err != nil {
		return TraceMutationPlan{}, err
	}
	if source.Version != PolicyVersion || len(source.Rules) != 0 || source.TraceMutation != nil {
		return TraceMutationPlan{}, errors.New("EXPERIMENT_TRACE_MUTATION_SOURCE_POLICY_UNSUPPORTED")
	}
	if err := source.Validate(len(trace.Records)); err != nil {
		return TraceMutationPlan{}, err
	}
	if firstDecision <= 0 || firstDecision >= len(trace.Records) {
		return TraceMutationPlan{}, fmt.Errorf("EXPERIMENT_TRACE_MUTATION_DECISION_INVALID: %d", firstDecision)
	}
	prefix := make([]control.ActionID, 0, firstDecision-1)
	for index := 0; index < firstDecision-1; index++ {
		prefix = append(prefix, trace.Records[index].Action.ID)
	}
	sourceDigest, err := source.Digest()
	if err != nil {
		return TraceMutationPlan{}, err
	}
	plan := TraceMutationPlan{
		SchemaVersion: TraceMutationPlanVersion, ID: id, Operator: TraceMutationOperator,
		SeedTraceDigest: trace.Digest, SourcePolicyDigest: sourceDigest,
		FirstDecision: firstDecision, PrefixActionIDs: prefix,
		FirstActionID:  trace.Records[firstDecision-1].Action.ID,
		SecondActionID: trace.Records[firstDecision].Action.ID,
		SuffixPriority: append([]control.ActionKind(nil), source.Priority...),
	}
	return plan.sealAndValidate(len(trace.Records))
}

// NewOccurrenceAwareAdjacentTraceMutation constructs the v2 plan from a
// validated corpus entry. The suffix is operator configuration, not an
// assumption about the source policy shape.
func NewOccurrenceAwareAdjacentTraceMutation(
	id string,
	trace controlruntime.Trace,
	source MutationSourceEntry,
	suffix []control.ActionKind,
	firstDecision int,
) (TraceMutationPlan, error) {
	if err := trace.Validate(); err != nil {
		return TraceMutationPlan{}, err
	}
	if err := source.ValidateTrace(trace); err != nil {
		return TraceMutationPlan{}, err
	}
	if firstDecision <= 0 || firstDecision >= len(trace.Records) {
		return TraceMutationPlan{}, fmt.Errorf("EXPERIMENT_TRACE_MUTATION_DECISION_INVALID: %d", firstDecision)
	}
	if err := validatePriority(suffix); err != nil || len(suffix) == 0 {
		return TraceMutationPlan{}, errors.New("EXPERIMENT_TRACE_MUTATION_SUFFIX_INVALID")
	}
	refs := actionOccurrenceRefs(trace.Records[:firstDecision+1])
	prefixIDs := make([]control.ActionID, firstDecision-1)
	prefixRefs := make([]ActionOccurrenceRef, firstDecision-1)
	for index := 0; index < firstDecision-1; index++ {
		prefixIDs[index] = refs[index].ActionID
		prefixRefs[index] = refs[index]
	}
	firstRef := refs[firstDecision-1]
	secondRef := refs[firstDecision]
	plan := TraceMutationPlan{
		SchemaVersion: TraceMutationPlanVersionV2, ID: id, Operator: TraceMutationOperator,
		SeedTraceDigest: trace.Digest, SourcePolicyDigest: source.PolicyDigest,
		SourceEntryDigest: source.Digest, FirstDecision: firstDecision,
		PrefixActionIDs: prefixIDs, PrefixActionRefs: prefixRefs,
		FirstActionID: firstRef.ActionID, SecondActionID: secondRef.ActionID,
		FirstActionRef: &firstRef, SecondActionRef: &secondRef,
		SuffixPriority: append([]control.ActionKind(nil), suffix...),
	}
	return plan.sealAndValidate(len(trace.Records))
}

// NewFirstAdjacentTraceMutation selects the first adjacent pair of one common
// Action kind. The scan order is part of the baseline, so a composition cannot
// try several pairs and retain only a convenient successful mutation.
func NewFirstAdjacentTraceMutation(
	id string,
	trace controlruntime.Trace,
	source Policy,
	kind control.ActionKind,
) (TraceMutationPlan, error) {
	if err := kind.Validate(); err != nil {
		return TraceMutationPlan{}, err
	}
	for index := 0; index+1 < len(trace.Records); index++ {
		if trace.Records[index].Action.Kind == kind && trace.Records[index+1].Action.Kind == kind {
			return NewAdjacentTraceMutation(id, trace, source, index+1)
		}
	}
	return TraceMutationPlan{}, fmt.Errorf("EXPERIMENT_TRACE_MUTATION_PAIR_NOT_FOUND: %s", kind)
}

// NewFirstAdjacentCorpusMutation applies the public first-pair rule to an
// arbitrary validated corpus entry. It never tries later pairs after observing
// whether an execution succeeds.
func NewFirstAdjacentCorpusMutation(
	id string,
	trace controlruntime.Trace,
	source MutationSourceEntry,
	kind control.ActionKind,
	suffix []control.ActionKind,
) (TraceMutationPlan, error) {
	if err := kind.Validate(); err != nil {
		return TraceMutationPlan{}, err
	}
	for index := 0; index+1 < len(trace.Records); index++ {
		if trace.Records[index].Action.Kind == kind && trace.Records[index+1].Action.Kind == kind {
			return NewOccurrenceAwareAdjacentTraceMutation(id, trace, source, suffix, index+1)
		}
	}
	return TraceMutationPlan{}, fmt.Errorf("EXPERIMENT_TRACE_MUTATION_PAIR_NOT_FOUND: %s", kind)
}

func (plan TraceMutationPlan) Validate(decisionBudget int) error {
	if (plan.SchemaVersion != TraceMutationPlanVersion && plan.SchemaVersion != TraceMutationPlanVersionV2) || plan.ID == "" ||
		plan.Operator != TraceMutationOperator || !validSHA256(plan.SeedTraceDigest) ||
		!validSHA256(plan.SourcePolicyDigest) || plan.FirstDecision <= 0 ||
		plan.FirstDecision >= decisionBudget || len(plan.PrefixActionIDs) != plan.FirstDecision-1 ||
		plan.FirstActionID == "" || plan.SecondActionID == "" || plan.FirstActionID == plan.SecondActionID ||
		len(plan.SuffixPriority) == 0 {
		return errors.New("EXPERIMENT_TRACE_MUTATION_PLAN_INVALID")
	}
	if plan.SchemaVersion == TraceMutationPlanVersion {
		if plan.SourceEntryDigest != "" || plan.GuidanceDigest != "" || len(plan.PrefixActionRefs) != 0 ||
			plan.FirstActionRef != nil || plan.SecondActionRef != nil {
			return errors.New("EXPERIMENT_TRACE_MUTATION_V1_FIELDS_INVALID")
		}
		seen := make(map[control.ActionID]bool, len(plan.PrefixActionIDs)+2)
		for _, id := range append(append([]control.ActionID(nil), plan.PrefixActionIDs...), plan.FirstActionID, plan.SecondActionID) {
			if id == "" || seen[id] {
				return errors.New("EXPERIMENT_TRACE_MUTATION_ACTION_SEQUENCE_INVALID")
			}
			seen[id] = true
		}
	} else {
		if plan.GuidanceDigest != "" && !validSHA256(plan.GuidanceDigest) {
			return errors.New("EXPERIMENT_TRACE_MUTATION_GUIDANCE_INVALID")
		}
		if err := plan.validateOccurrences(); err != nil {
			return err
		}
	}
	if err := validatePriority(plan.SuffixPriority); err != nil {
		return err
	}
	sealed, err := plan.seal()
	if err != nil || sealed.Digest != plan.Digest {
		return errors.New("EXPERIMENT_TRACE_MUTATION_DIGEST_MISMATCH")
	}
	return nil
}

func (plan TraceMutationPlan) sealAndValidate(decisionBudget int) (TraceMutationPlan, error) {
	sealed, err := plan.seal()
	if err != nil {
		return TraceMutationPlan{}, err
	}
	if err := sealed.Validate(decisionBudget); err != nil {
		return TraceMutationPlan{}, err
	}
	return sealed, nil
}

func (plan TraceMutationPlan) seal() (TraceMutationPlan, error) {
	plan.PrefixActionIDs = append([]control.ActionID(nil), plan.PrefixActionIDs...)
	plan.PrefixActionRefs = append([]ActionOccurrenceRef(nil), plan.PrefixActionRefs...)
	if plan.FirstActionRef != nil {
		ref := *plan.FirstActionRef
		plan.FirstActionRef = &ref
	}
	if plan.SecondActionRef != nil {
		ref := *plan.SecondActionRef
		plan.SecondActionRef = &ref
	}
	plan.SuffixPriority = append([]control.ActionKind(nil), plan.SuffixPriority...)
	plan.Digest = ""
	digest, err := portableJSONDigest(plan)
	if err != nil {
		return TraceMutationPlan{}, err
	}
	plan.Digest = digest
	return plan, nil
}

func (plan TraceMutationPlan) validateOccurrences() error {
	if !validSHA256(plan.SourceEntryDigest) ||
		len(plan.PrefixActionRefs) != len(plan.PrefixActionIDs) ||
		plan.FirstActionRef == nil || plan.SecondActionRef == nil ||
		plan.FirstActionRef.ActionID != plan.FirstActionID ||
		plan.SecondActionRef.ActionID != plan.SecondActionID {
		return errors.New("EXPERIMENT_TRACE_MUTATION_OCCURRENCE_INVALID")
	}
	refs := append([]ActionOccurrenceRef(nil), plan.PrefixActionRefs...)
	refs = append(refs, *plan.FirstActionRef, *plan.SecondActionRef)
	counts := make(map[control.ActionID]int)
	for index, ref := range refs {
		var id control.ActionID
		switch {
		case index < len(plan.PrefixActionIDs):
			id = plan.PrefixActionIDs[index]
		case index == len(plan.PrefixActionIDs):
			id = plan.FirstActionID
		default:
			id = plan.SecondActionID
		}
		counts[id]++
		if ref.ActionID == "" || ref.ActionID != id || ref.Occurrence != counts[id] {
			return errors.New("EXPERIMENT_TRACE_MUTATION_OCCURRENCE_INVALID")
		}
	}
	return nil
}

func actionOccurrenceRefs(records []controlruntime.ActionRecord) []ActionOccurrenceRef {
	counts := make(map[control.ActionID]int)
	refs := make([]ActionOccurrenceRef, len(records))
	for index, record := range records {
		counts[record.Action.ID]++
		refs[index] = ActionOccurrenceRef{
			ActionID: record.Action.ID, Occurrence: counts[record.Action.ID],
		}
	}
	return refs
}

func (plan TraceMutationPlan) selectAction(decision int, enabled []control.Action) (control.Action, error) {
	var exact control.ActionID
	switch {
	case decision < plan.FirstDecision:
		exact = plan.PrefixActionIDs[decision-1]
	case decision == plan.FirstDecision:
		exact = plan.SecondActionID
	case decision == plan.FirstDecision+1:
		exact = plan.FirstActionID
	default:
		for _, kind := range plan.SuffixPriority {
			for _, action := range enabled {
				if action.Kind == kind {
					return action, nil
				}
			}
		}
		return control.Action{}, &policySelectionError{
			code:   "EXPERIMENT_TRACE_MUTATION_SUFFIX_NO_ACTION",
			detail: fmt.Sprintf("decision=%d", decision),
		}
	}
	for _, action := range enabled {
		if action.ID == exact {
			return action, nil
		}
	}
	return control.Action{}, &policySelectionError{
		code:   "EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED",
		detail: fmt.Sprintf("decision=%d action=%s", decision, exact),
	}
}

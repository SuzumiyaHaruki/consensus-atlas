package controlexperiment

import (
	"errors"
	"fmt"
	"slices"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const (
	PSSGuidedMutationChoiceVersion = "consensus-atlas/pss-guided-mutation-choice/v1"
	PSSGuidedMutationRule          = "max-source-exclusive-states/then-min-global-visits/then-earliest-pair/v1"
)

// PSSGuidedMutationChoice is a batch boundary, not an online scheduler. It
// consumes only trusted feedback from completed source bundles, freezes one
// mutation proposal, and never prunes a Runtime state or changes enabled.
type PSSGuidedMutationChoice struct {
	SchemaVersion              string               `json:"schema_version"`
	ID                         string               `json:"id"`
	Rule                       string               `json:"rule"`
	CorpusDigest               string               `json:"corpus_digest"`
	FeedbackDigests            []string             `json:"feedback_digests"`
	ActionKind                 control.ActionKind   `json:"action_kind"`
	SuffixPriority             []control.ActionKind `json:"suffix_priority"`
	CandidateCount             int                  `json:"candidate_count"`
	CandidateSetDigest         string               `json:"candidate_set_digest"`
	SelectedSourceOrdinal      int                  `json:"selected_source_ordinal"`
	SelectedSourceEntryDigest  string               `json:"selected_source_entry_digest"`
	SelectedSourceUniqueStates int                  `json:"selected_source_unique_states"`
	SelectedStateKey           string               `json:"selected_state_key"`
	SelectedStateGlobalVisits  int                  `json:"selected_state_global_visits"`
	FirstDecision              int                  `json:"first_decision"`
	DecisionBudget             int                  `json:"decision_budget"`
	GuidanceDigest             string               `json:"guidance_digest"`
	Mutation                   TraceMutationPlan    `json:"mutation"`
	Digest                     string               `json:"digest"`
}

type pssGuidedCandidate struct {
	SourceOrdinal      int                 `json:"source_ordinal"`
	SourceEntryDigest  string              `json:"source_entry_digest"`
	SourceUniqueStates int                 `json:"source_unique_states"`
	StateKey           string              `json:"state_key"`
	GlobalVisits       int                 `json:"global_visits"`
	FirstDecision      int                 `json:"first_decision"`
	FirstAction        ActionOccurrenceRef `json:"first_action"`
	SecondAction       ActionOccurrenceRef `json:"second_action"`
}

type pssGuidanceIdentity struct {
	SchemaVersion              string               `json:"schema_version"`
	ID                         string               `json:"id"`
	Rule                       string               `json:"rule"`
	CorpusDigest               string               `json:"corpus_digest"`
	FeedbackDigests            []string             `json:"feedback_digests"`
	ActionKind                 control.ActionKind   `json:"action_kind"`
	SuffixPriority             []control.ActionKind `json:"suffix_priority"`
	CandidateCount             int                  `json:"candidate_count"`
	CandidateSetDigest         string               `json:"candidate_set_digest"`
	SelectedSourceOrdinal      int                  `json:"selected_source_ordinal"`
	SelectedSourceEntryDigest  string               `json:"selected_source_entry_digest"`
	SelectedSourceUniqueStates int                  `json:"selected_source_unique_states"`
	SelectedStateKey           string               `json:"selected_state_key"`
	SelectedStateGlobalVisits  int                  `json:"selected_state_global_visits"`
	FirstDecision              int                  `json:"first_decision"`
	DecisionBudget             int                  `json:"decision_budget"`
}

// NewPSSGuidedMutationChoice derives all feedback itself. The ranking has two
// deterministic stages: choose the source with the most states exclusive to
// that source in this batch, then choose its adjacent Action pair whose
// pre-state has the fewest visits across the whole batch. Stable corpus order
// and decision order break ties before any mutation is executed.
func NewPSSGuidedMutationChoice(
	id string,
	corpus MutationSourceCorpus,
	bundles []ExecutionBundle,
	mapper psscore.SemanticMapper,
	kind control.ActionKind,
	suffix []control.ActionKind,
) (PSSGuidedMutationChoice, error) {
	if id == "" || len(bundles) != len(corpus.Entries) || len(bundles) == 0 || mapper == nil {
		return PSSGuidedMutationChoice{}, errors.New("EXPERIMENT_PSS_GUIDANCE_INPUT_INVALID")
	}
	if err := corpus.Validate(); err != nil {
		return PSSGuidedMutationChoice{}, err
	}
	if err := kind.Validate(); err != nil {
		return PSSGuidedMutationChoice{}, err
	}
	if len(suffix) == 0 || validatePriority(suffix) != nil {
		return PSSGuidedMutationChoice{}, errors.New("EXPERIMENT_PSS_GUIDANCE_SUFFIX_INVALID")
	}
	feedback := make([]PSSFeedback, len(bundles))
	for index, bundle := range bundles {
		if err := corpus.Entries[index].ValidateBundle(bundle); err != nil {
			return PSSGuidedMutationChoice{}, err
		}
		if index > 0 {
			if err := validateGuidanceSourceCompatibility(bundles[0], bundle); err != nil {
				return PSSGuidedMutationChoice{}, err
			}
		}
		var err error
		feedback[index], err = NewPSSFeedback(
			fmt.Sprintf("%s/source-%02d", id, index+1), bundle, mapper,
		)
		if err != nil {
			return PSSGuidedMutationChoice{}, err
		}
	}

	globalVisits := make(map[string]int)
	presence := make(map[string]int)
	for _, current := range feedback {
		for _, state := range current.States {
			globalVisits[state.Key] += state.Visits
			presence[state.Key]++
		}
	}
	sourceUnique := make([]int, len(feedback))
	for index, current := range feedback {
		for _, state := range current.States {
			if presence[state.Key] == 1 {
				sourceUnique[index]++
			}
		}
	}

	candidates := make([]pssGuidedCandidate, 0)
	for sourceIndex, bundle := range bundles {
		refs := actionOccurrenceRefs(bundle.Trace.Records)
		for recordIndex := 0; recordIndex+1 < len(bundle.Trace.Records); recordIndex++ {
			first := bundle.Trace.Records[recordIndex].Action
			second := bundle.Trace.Records[recordIndex+1].Action
			if first.Kind != kind || second.Kind != kind || first.ID == second.ID {
				continue
			}
			stateKey := bundle.CorePSS[recordIndex].Key
			visits := globalVisits[stateKey]
			if visits <= 0 {
				return PSSGuidedMutationChoice{}, errors.New("EXPERIMENT_PSS_GUIDANCE_STATE_MISSING")
			}
			candidates = append(candidates, pssGuidedCandidate{
				SourceOrdinal: sourceIndex + 1, SourceEntryDigest: corpus.Entries[sourceIndex].Digest,
				SourceUniqueStates: sourceUnique[sourceIndex], StateKey: stateKey,
				GlobalVisits: visits, FirstDecision: recordIndex + 1,
				FirstAction: refs[recordIndex], SecondAction: refs[recordIndex+1],
			})
		}
	}
	if len(candidates) == 0 {
		return PSSGuidedMutationChoice{}, fmt.Errorf("EXPERIMENT_PSS_GUIDANCE_PAIR_NOT_FOUND: %s", kind)
	}
	candidateSetDigest, err := portableJSONDigest(candidates)
	if err != nil {
		return PSSGuidedMutationChoice{}, err
	}
	selectedSource := candidates[0].SourceOrdinal
	selectedUnique := candidates[0].SourceUniqueStates
	for _, candidate := range candidates[1:] {
		if candidate.SourceUniqueStates > selectedUnique ||
			(candidate.SourceUniqueStates == selectedUnique && candidate.SourceOrdinal < selectedSource) {
			selectedSource = candidate.SourceOrdinal
			selectedUnique = candidate.SourceUniqueStates
		}
	}
	var selected *pssGuidedCandidate
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.SourceOrdinal != selectedSource {
			continue
		}
		if selected == nil || candidate.GlobalVisits < selected.GlobalVisits ||
			(candidate.GlobalVisits == selected.GlobalVisits && candidate.FirstDecision < selected.FirstDecision) {
			selected = candidate
		}
	}
	if selected == nil {
		return PSSGuidedMutationChoice{}, errors.New("EXPERIMENT_PSS_GUIDANCE_SELECTION_FAILED")
	}
	selectedIndex := selected.SourceOrdinal - 1
	choice := PSSGuidedMutationChoice{
		SchemaVersion: PSSGuidedMutationChoiceVersion, ID: id, Rule: PSSGuidedMutationRule,
		CorpusDigest: corpus.Digest, ActionKind: kind,
		SuffixPriority: append([]control.ActionKind(nil), suffix...),
		CandidateCount: len(candidates), CandidateSetDigest: candidateSetDigest,
		SelectedSourceOrdinal:      selected.SourceOrdinal,
		SelectedSourceEntryDigest:  selected.SourceEntryDigest,
		SelectedSourceUniqueStates: selected.SourceUniqueStates,
		SelectedStateKey:           selected.StateKey, SelectedStateGlobalVisits: selected.GlobalVisits,
		FirstDecision: selected.FirstDecision, DecisionBudget: corpus.Entries[selectedIndex].Decisions,
	}
	for _, current := range feedback {
		choice.FeedbackDigests = append(choice.FeedbackDigests, current.Digest)
	}
	choice.GuidanceDigest, err = choice.computeGuidanceDigest()
	if err != nil {
		return PSSGuidedMutationChoice{}, err
	}
	mutation, err := NewOccurrenceAwareAdjacentTraceMutation(
		id+"/mutation", bundles[selectedIndex].Trace, corpus.Entries[selectedIndex], suffix,
		selected.FirstDecision,
	)
	if err != nil {
		return PSSGuidedMutationChoice{}, err
	}
	mutation.GuidanceDigest = choice.GuidanceDigest
	mutation, err = mutation.sealAndValidate(corpus.Entries[selectedIndex].Decisions)
	if err != nil {
		return PSSGuidedMutationChoice{}, err
	}
	choice.Mutation = mutation
	choice, err = choice.seal()
	if err != nil {
		return PSSGuidedMutationChoice{}, err
	}
	if err := choice.Validate(); err != nil {
		return PSSGuidedMutationChoice{}, err
	}
	return choice, nil
}

func (choice PSSGuidedMutationChoice) Validate() error {
	if choice.SchemaVersion != PSSGuidedMutationChoiceVersion || choice.ID == "" ||
		choice.Rule != PSSGuidedMutationRule || !validSHA256(choice.CorpusDigest) ||
		len(choice.FeedbackDigests) == 0 || choice.CandidateCount <= 0 ||
		!validSHA256(choice.CandidateSetDigest) || choice.SelectedSourceOrdinal <= 0 ||
		choice.SelectedSourceOrdinal > len(choice.FeedbackDigests) ||
		!validSHA256(choice.SelectedSourceEntryDigest) || choice.SelectedSourceUniqueStates < 0 ||
		!validSHA256(choice.SelectedStateKey) || choice.SelectedStateGlobalVisits <= 0 ||
		choice.FirstDecision <= 0 || choice.DecisionBudget <= choice.FirstDecision ||
		!validSHA256(choice.GuidanceDigest) {
		return errors.New("EXPERIMENT_PSS_GUIDANCE_INVALID")
	}
	if err := choice.ActionKind.Validate(); err != nil {
		return err
	}
	if len(choice.SuffixPriority) == 0 || validatePriority(choice.SuffixPriority) != nil {
		return errors.New("EXPERIMENT_PSS_GUIDANCE_SUFFIX_INVALID")
	}
	seenFeedback := make(map[string]bool, len(choice.FeedbackDigests))
	for _, digest := range choice.FeedbackDigests {
		if !validSHA256(digest) {
			return errors.New("EXPERIMENT_PSS_GUIDANCE_FEEDBACK_INVALID")
		}
		if seenFeedback[digest] {
			return errors.New("EXPERIMENT_PSS_GUIDANCE_FEEDBACK_DUPLICATE")
		}
		seenFeedback[digest] = true
	}
	wantGuidance, err := choice.computeGuidanceDigest()
	if err != nil || wantGuidance != choice.GuidanceDigest {
		return errors.New("EXPERIMENT_PSS_GUIDANCE_DIGEST_MISMATCH")
	}
	if choice.Mutation.GuidanceDigest != choice.GuidanceDigest ||
		choice.Mutation.SourceEntryDigest != choice.SelectedSourceEntryDigest ||
		choice.Mutation.FirstDecision != choice.FirstDecision ||
		!slices.Equal(choice.Mutation.SuffixPriority, choice.SuffixPriority) {
		return errors.New("EXPERIMENT_PSS_GUIDANCE_MUTATION_MISMATCH")
	}
	if err := choice.Mutation.Validate(choice.DecisionBudget); err != nil {
		return err
	}
	sealed, err := choice.seal()
	if err != nil || sealed.Digest != choice.Digest {
		return errors.New("EXPERIMENT_PSS_GUIDANCE_ARTIFACT_DIGEST_MISMATCH")
	}
	return nil
}

// ValidateInputs is required at a trust boundary. Validate alone checks the
// self-contained artifact; this method reprojects each source bundle and
// recomputes the complete candidate ranking.
func (choice PSSGuidedMutationChoice) ValidateInputs(
	corpus MutationSourceCorpus,
	bundles []ExecutionBundle,
	mapper psscore.SemanticMapper,
) error {
	if err := choice.Validate(); err != nil {
		return err
	}
	want, err := NewPSSGuidedMutationChoice(
		choice.ID, corpus, bundles, mapper, choice.ActionKind, choice.SuffixPriority,
	)
	if err != nil {
		return err
	}
	if want.Digest != choice.Digest {
		return errors.New("EXPERIMENT_PSS_GUIDANCE_INPUT_MISMATCH")
	}
	return nil
}

func (choice PSSGuidedMutationChoice) computeGuidanceDigest() (string, error) {
	return portableJSONDigest(pssGuidanceIdentity{
		SchemaVersion: choice.SchemaVersion, ID: choice.ID, Rule: choice.Rule,
		CorpusDigest:    choice.CorpusDigest,
		FeedbackDigests: append([]string(nil), choice.FeedbackDigests...),
		ActionKind:      choice.ActionKind, SuffixPriority: append([]control.ActionKind(nil), choice.SuffixPriority...),
		CandidateCount: choice.CandidateCount, CandidateSetDigest: choice.CandidateSetDigest,
		SelectedSourceOrdinal:      choice.SelectedSourceOrdinal,
		SelectedSourceEntryDigest:  choice.SelectedSourceEntryDigest,
		SelectedSourceUniqueStates: choice.SelectedSourceUniqueStates,
		SelectedStateKey:           choice.SelectedStateKey,
		SelectedStateGlobalVisits:  choice.SelectedStateGlobalVisits,
		FirstDecision:              choice.FirstDecision, DecisionBudget: choice.DecisionBudget,
	})
}

func (choice PSSGuidedMutationChoice) seal() (PSSGuidedMutationChoice, error) {
	choice.FeedbackDigests = append([]string(nil), choice.FeedbackDigests...)
	choice.SuffixPriority = append([]control.ActionKind(nil), choice.SuffixPriority...)
	choice.Digest = ""
	digest, err := portableJSONDigest(choice)
	if err != nil {
		return PSSGuidedMutationChoice{}, err
	}
	choice.Digest = digest
	return choice, nil
}

func validateGuidanceSourceCompatibility(reference ExecutionBundle, candidate ExecutionBundle) error {
	if reference.SchemaVersion != candidate.SchemaVersion ||
		reference.Identity.PSSID != candidate.Identity.PSSID ||
		reference.Identity.ManifestDigest != candidate.Identity.ManifestDigest ||
		reference.Qualification.Qualification.Digest != candidate.Qualification.Qualification.Digest ||
		reference.Trace.SchemaVersion != candidate.Trace.SchemaVersion ||
		reference.Trace.SeedDigest != candidate.Trace.SeedDigest ||
		reference.Run.TargetDecisions != candidate.Run.TargetDecisions ||
		reference.Run.Workload == nil || candidate.Run.Workload == nil ||
		reference.Run.Workload.PlanDigest != candidate.Run.Workload.PlanDigest ||
		reference.Run.Workload.Planned != candidate.Run.Workload.Planned ||
		(reference.Run.Faults == nil) != (candidate.Run.Faults == nil) {
		return errors.New("EXPERIMENT_PSS_GUIDANCE_SOURCE_INCOMPATIBLE")
	}
	return nil
}

package controlexperiment

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	SemanticBestFirstAlgorithmID   = "bounded-semantic-best-first-v1"
	SemanticBestFirstGuidanceID    = "risk-progress-lexicographic-v1"
	SemanticQueueViewSchemaVersion = "consensus-atlas/semantic-queue-view/v1"
	SemanticBestFirstResultVersion = "consensus-atlas/semantic-best-first-result/v1"
	SemanticCandidateIDPrefix      = "semantic-candidate-"
)

// SemanticPrefixProjector is trusted target composition. It derives a frozen
// RiskWitness result from an exact replay-stable prefix; generic search checks
// every returned source identity but never interprets protocol evidence.
type SemanticPrefixProjector interface {
	ID() string
	Project(string, semantic.RiskWitnessSpec, controlruntime.Trace) (semantic.RiskWitnessResult, error)
}

// SemanticQueueCandidate is the planner-safe per-candidate feature set. It
// deliberately omits ActionID, trace/work-item digests, evidence and verdicts.
type SemanticQueueCandidate struct {
	CandidateID           string             `json:"candidate_id"`
	TargetUnreached       bool               `json:"target_unreached"`
	SatisfiedMilestones   []string           `json:"satisfied_milestones"`
	FirstMissingMilestone string             `json:"first_missing_milestone,omitempty"`
	SemanticBucketVisits  int                `json:"semantic_bucket_visits"`
	PrefixDecisions       int                `json:"prefix_decisions"`
	ActionKind            control.ActionKind `json:"action_kind"`
}

type SemanticQueueView struct {
	SchemaVersion  string                   `json:"schema_version"`
	ID             string                   `json:"id"`
	RiskSpecDigest string                   `json:"risk_spec_digest"`
	Candidates     []SemanticQueueCandidate `json:"candidates"`
	Digest         string                   `json:"digest"`
}

// SemanticQueueGuidance may only return a complete permutation of the frozen
// queue candidate IDs. Search validates the set before choosing its head.
type SemanticQueueGuidance interface {
	ID() string
	Order(context.Context, SemanticQueueView) ([]string, error)
}

type SemanticCandidateRecord struct {
	CandidateID    string                     `json:"candidate_id"`
	WorkItemDigest string                     `json:"work_item_digest"`
	RiskResult     semantic.RiskWitnessResult `json:"risk_result"`
}

// SemanticBestFirstResult is the formal outer method identity. Search retains
// the existing exact-prefix tree/work artifact, while this wrapper binds the
// second algorithm, guidance, projector and semantic expansion order.
type SemanticBestFirstResult struct {
	SchemaVersion  string                     `json:"schema_version"`
	AlgorithmID    string                     `json:"algorithm_id"`
	GuidanceID     string                     `json:"guidance_id"`
	ProjectorID    string                     `json:"projector_id"`
	RiskSpecDigest string                     `json:"risk_spec_digest"`
	RootRiskResult semantic.RiskWitnessResult `json:"root_risk_result"`
	Search         StatelessDFSResult         `json:"exact_prefix_search"`
	Candidates     []SemanticCandidateRecord  `json:"candidates"`
	ExpansionOrder []string                   `json:"expansion_order"`
	Digest         string                     `json:"digest"`
}

type semanticQueueEntry struct {
	trace     controlruntime.Trace
	item      StatelessDFSWorkItem
	candidate SemanticCandidateRecord
}

type deterministicSemanticGuidance struct{}

func NewDeterministicSemanticBestFirstGuidance() SemanticQueueGuidance {
	return deterministicSemanticGuidance{}
}

func (deterministicSemanticGuidance) ID() string { return SemanticBestFirstGuidanceID }

func (deterministicSemanticGuidance) Order(_ context.Context, view SemanticQueueView) ([]string, error) {
	if err := view.Validate(); err != nil {
		return nil, err
	}
	candidates := cloneSemanticQueueCandidates(view.Candidates)
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.TargetUnreached != right.TargetUnreached {
			return left.TargetUnreached
		}
		if len(left.SatisfiedMilestones) != len(right.SatisfiedMilestones) {
			return len(left.SatisfiedMilestones) > len(right.SatisfiedMilestones)
		}
		if left.SemanticBucketVisits != right.SemanticBucketVisits {
			return left.SemanticBucketVisits < right.SemanticBucketVisits
		}
		if left.PrefixDecisions != right.PrefixDecisions {
			return left.PrefixDecisions < right.PrefixDecisions
		}
		return left.CandidateID < right.CandidateID
	})
	ordered := make([]string, len(candidates))
	for index, candidate := range candidates {
		ordered[index] = candidate.CandidateID
	}
	return ordered, nil
}

// ExploreBoundedSemanticBestFirst expands the root once, then repeatedly
// chooses one globally pending exact prefix. Children are materialized only
// when their parent is expanded, so the full DFS corpus is never precomputed.
func ExploreBoundedSemanticBestFirst(
	ctx context.Context,
	spec StatelessDFSSpec,
	root controlruntime.Trace,
	riskSpec semantic.RiskWitnessSpec,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	guidance SemanticQueueGuidance,
) (SemanticBestFirstResult, error) {
	if spec.Validate(root) != nil || riskSpec.Validate() != nil || newAdapter == nil ||
		isNilSemanticComponent(projector) || isNilSemanticComponent(guidance) ||
		!validMethodToken(projector.ID()) || !validMethodToken(guidance.ID()) {
		return SemanticBestFirstResult{}, errors.New("EXPERIMENT_SEMANTIC_BEST_FIRST_INPUT_INVALID")
	}
	rootRisk, err := projectSemanticPrefix(projector, spec.ID+"-root-risk", riskSpec, root)
	if err != nil {
		return SemanticBestFirstResult{}, err
	}
	search := StatelessDFSResult{SchemaVersion: StatelessDFSResultSchemaVersion, Spec: spec}
	result := SemanticBestFirstResult{
		SchemaVersion: SemanticBestFirstResultVersion,
		AlgorithmID:   SemanticBestFirstAlgorithmID, GuidanceID: guidance.ID(), ProjectorID: projector.ID(),
		RiskSpecDigest: riskSpec.Digest, RootRiskResult: rootRisk,
	}
	queue := make([]semanticQueueEntry, 0)
	bucketVisits := make(map[string]int)
	stop := ""

	expand := func(prefix controlruntime.Trace, depth int, parentOrdinal int) (bool, error) {
		reconstructionEstimate := prefixReplayWork(prefix)
		if search.Work.TotalWorkUnits+reconstructionEstimate.WorkUnits > spec.MaxWorkUnits {
			stop = StatelessDFSStopWork
			return false, nil
		}
		viewID := fmt.Sprintf("%s-semantic-frontier-%06d", spec.ID, search.StatesExpanded+1)
		view, _, reconstruction, err := reconstructActionFrontierPrefix(
			ctx, viewID, prefix, spec.Runtime, spec.FaultEnvelope, newAdapter,
		)
		if err != nil {
			return false, err
		}
		if reconstruction != reconstructionEstimate {
			return false, errors.New("EXPERIMENT_SEMANTIC_RECONSTRUCTION_WORK_MISMATCH")
		}
		addDFSPhase(&search.Work.FrontierReconstruction, reconstruction)
		search.Work.TotalWorkUnits += reconstruction.WorkUnits
		search.StatesExpanded++
		for _, action := range view.Actions {
			if len(search.Items) >= spec.MaxWorkItems {
				stop = StatelessDFSStopItems
				return true, nil
			}
			materializationEstimate, verificationEstimate := childWorkEstimate(prefix, action)
			needed := materializationEstimate.WorkUnits + verificationEstimate.WorkUnits
			if search.Work.TotalWorkUnits+needed > spec.MaxWorkUnits {
				stop = StatelessDFSStopWork
				return true, nil
			}
			child, materialization, verification, err := materializeDFSChild(
				ctx, view, action, prefix, spec.Runtime, spec.FaultEnvelope, newAdapter,
			)
			if err != nil {
				return true, err
			}
			if materialization != materializationEstimate || verification != verificationEstimate {
				return true, errors.New("EXPERIMENT_SEMANTIC_CHILD_WORK_MISMATCH")
			}
			addDFSPhase(&search.Work.ChildMaterialization, materialization)
			addDFSPhase(&search.Work.ChildVerification, verification)
			search.Work.TotalWorkUnits += needed
			ordinal := len(search.Items) + 1
			item := StatelessDFSWorkItem{
				SchemaVersion: StatelessDFSWorkItemSchemaVersion, SearchDigest: spec.Digest, Ordinal: ordinal,
				State: StatelessDFSStateRef{PrefixTraceDigest: prefix.Digest, SnapshotDigest: view.SnapshotDigest,
					PrefixDecisions: len(prefix.Records)},
				Action: action, Path: StatelessDFSPathMetadata{ParentOrdinal: parentOrdinal, Depth: depth + 1,
					Decision: len(child.Records)},
				FrontierDigest: view.Digest, ChildPrefixDigest: child.Digest,
				ChildStateDigest: child.FinalStateDigest, ReplayStable: true,
			}
			item, err = item.seal()
			if err != nil {
				return true, err
			}
			candidateID := semanticCandidateID(ordinal)
			risk, err := projectSemanticPrefix(
				projector, fmt.Sprintf("%s-risk-%06d", spec.ID, ordinal), riskSpec, child,
			)
			if err != nil {
				return true, err
			}
			candidate := SemanticCandidateRecord{CandidateID: candidateID, WorkItemDigest: item.Digest, RiskResult: risk}
			search.Items = append(search.Items, item)
			result.Candidates = append(result.Candidates, candidate)
			if depth+1 < spec.MaxDepth {
				queue = append(queue, semanticQueueEntry{trace: child, item: item, candidate: candidate})
			}
		}
		return true, nil
	}

	if _, err := expand(root, 0, 0); err != nil {
		return SemanticBestFirstResult{}, &StatelessDFSExecutionError{Work: search.Work, cause: err}
	}
	for stop == "" && len(queue) > 0 {
		view, err := newSemanticQueueView(spec.ID, riskSpec, queue, bucketVisits)
		if err != nil {
			return SemanticBestFirstResult{}, &StatelessDFSExecutionError{Work: search.Work, cause: err}
		}
		guidanceView := view
		guidanceView.Candidates = cloneSemanticQueueCandidates(view.Candidates)
		ordered, err := guidance.Order(ctx, guidanceView)
		if err != nil {
			return SemanticBestFirstResult{}, &StatelessDFSExecutionError{Work: search.Work, cause: err}
		}
		ordered, err = validateSemanticQueueOrder(view, ordered)
		if err != nil {
			return SemanticBestFirstResult{}, &StatelessDFSExecutionError{Work: search.Work, cause: err}
		}
		selectedIndex := semanticQueueIndex(queue, ordered[0])
		selected := queue[selectedIndex]
		queue = append(queue[:selectedIndex], queue[selectedIndex+1:]...)
		expanded, err := expand(selected.trace, selected.item.Path.Depth, selected.item.Ordinal)
		if err != nil {
			return SemanticBestFirstResult{}, &StatelessDFSExecutionError{Work: search.Work, cause: err}
		}
		if expanded {
			result.ExpansionOrder = append(result.ExpansionOrder, selected.candidate.CandidateID)
			bucketVisits[semanticProgressBucket(selected.candidate.RiskResult)]++
		}
	}
	if stop == "" {
		stop = StatelessDFSStopComplete
	}
	search.StopReason = stop
	search, err = search.seal()
	if err != nil || search.Validate(root) != nil {
		return SemanticBestFirstResult{}, errors.New("EXPERIMENT_SEMANTIC_EXACT_PREFIX_RESULT_INVALID")
	}
	result.Search = search
	result, err = result.seal()
	if err != nil || result.Validate(root, riskSpec) != nil {
		return SemanticBestFirstResult{}, errors.New("EXPERIMENT_SEMANTIC_BEST_FIRST_RESULT_INVALID")
	}
	return result, nil
}

func (result SemanticBestFirstResult) Validate(root controlruntime.Trace, spec semantic.RiskWitnessSpec) error {
	if result.SchemaVersion != SemanticBestFirstResultVersion || result.AlgorithmID != SemanticBestFirstAlgorithmID ||
		!validMethodToken(result.GuidanceID) || !validMethodToken(result.ProjectorID) || spec.Validate() != nil ||
		result.RiskSpecDigest != spec.Digest || result.Search.Validate(root) != nil ||
		result.RootRiskResult.Validate(spec) != nil || result.RootRiskResult.ProjectorID != result.ProjectorID ||
		result.RootRiskResult.ExecutionDigest != root.Digest ||
		result.RootRiskResult.TargetIdentityDigest != root.ManifestDigest ||
		len(result.Candidates) != len(result.Search.Items) {
		return errors.New("EXPERIMENT_SEMANTIC_BEST_FIRST_RESULT_INVALID")
	}
	known := make(map[string]int, len(result.Candidates))
	for index, candidate := range result.Candidates {
		item := result.Search.Items[index]
		if candidate.CandidateID != semanticCandidateID(index+1) || candidate.WorkItemDigest != item.Digest ||
			candidate.RiskResult.Validate(spec) != nil || candidate.RiskResult.ProjectorID != result.ProjectorID ||
			candidate.RiskResult.ExecutionDigest != item.ChildPrefixDigest ||
			candidate.RiskResult.TargetIdentityDigest != root.ManifestDigest {
			return errors.New("EXPERIMENT_SEMANTIC_CANDIDATE_SOURCE_INVALID")
		}
		known[candidate.CandidateID] = index
	}
	seen := make(map[string]bool, len(result.ExpansionOrder))
	for _, candidateID := range result.ExpansionOrder {
		index, ok := known[candidateID]
		if !ok || seen[candidateID] || result.Search.Items[index].Path.Depth >= result.Search.Spec.MaxDepth {
			return errors.New("EXPERIMENT_SEMANTIC_EXPANSION_ORDER_INVALID")
		}
		parent := result.Search.Items[index].Path.ParentOrdinal
		if parent > 0 {
			parentID := semanticCandidateID(parent)
			if result.Search.Items[parent-1].Path.Depth < result.Search.Spec.MaxDepth && !seen[parentID] {
				return errors.New("EXPERIMENT_SEMANTIC_EXPANSION_PARENT_INVALID")
			}
		}
		seen[candidateID] = true
	}
	for _, item := range result.Search.Items {
		if item.Path.ParentOrdinal == 0 {
			continue
		}
		if !seen[semanticCandidateID(item.Path.ParentOrdinal)] {
			return errors.New("EXPERIMENT_SEMANTIC_EXPANSION_CHILD_WITHOUT_PARENT")
		}
	}
	wantExpansions := result.Search.StatesExpanded - 1
	if wantExpansions < 0 {
		wantExpansions = 0
	}
	if len(result.ExpansionOrder) != wantExpansions {
		return errors.New("EXPERIMENT_SEMANTIC_EXPANSION_COUNT_INVALID")
	}
	sealed, err := result.seal()
	if err != nil || !validSHA256(result.Digest) || sealed.Digest != result.Digest {
		return errors.New("EXPERIMENT_SEMANTIC_BEST_FIRST_DIGEST_MISMATCH")
	}
	return nil
}

func (result SemanticBestFirstResult) ValidateSources(
	ctx context.Context,
	root controlruntime.Trace,
	spec semantic.RiskWitnessSpec,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	guidance SemanticQueueGuidance,
) error {
	if result.Validate(root, spec) != nil || newAdapter == nil || isNilSemanticComponent(projector) ||
		isNilSemanticComponent(guidance) ||
		projector.ID() != result.ProjectorID || guidance.ID() != result.GuidanceID {
		return errors.New("EXPERIMENT_SEMANTIC_BEST_FIRST_SOURCE_INVALID")
	}
	want, err := ExploreBoundedSemanticBestFirst(
		ctx, result.Search.Spec, root, spec, newAdapter, projector, guidance,
	)
	if err != nil || !reflect.DeepEqual(want, result) {
		return errors.New("EXPERIMENT_SEMANTIC_BEST_FIRST_SOURCE_MISMATCH")
	}
	return nil
}

func newSemanticQueueView(
	id string,
	spec semantic.RiskWitnessSpec,
	queue []semanticQueueEntry,
	bucketVisits map[string]int,
) (SemanticQueueView, error) {
	view := SemanticQueueView{SchemaVersion: SemanticQueueViewSchemaVersion, ID: id + "-semantic-queue",
		RiskSpecDigest: spec.Digest, Candidates: make([]SemanticQueueCandidate, len(queue))}
	for index, entry := range queue {
		result := entry.candidate.RiskResult
		if err := result.Validate(spec); err != nil {
			return SemanticQueueView{}, err
		}
		firstMissing := ""
		if len(result.MissingMilestones) > 0 {
			firstMissing = result.MissingMilestones[0]
		}
		view.Candidates[index] = SemanticQueueCandidate{
			CandidateID: entry.candidate.CandidateID, TargetUnreached: result.Status != semantic.RiskWitnessReached,
			SatisfiedMilestones:   append([]string(nil), result.SatisfiedMilestones...),
			FirstMissingMilestone: firstMissing,
			SemanticBucketVisits:  bucketVisits[semanticProgressBucket(entry.candidate.RiskResult)],
			PrefixDecisions:       entry.item.Path.Decision, ActionKind: entry.item.Action.Kind,
		}
	}
	sort.Slice(view.Candidates, func(i, j int) bool { return view.Candidates[i].CandidateID < view.Candidates[j].CandidateID })
	return view.seal()
}

func (view SemanticQueueView) Validate() error {
	if view.SchemaVersion != SemanticQueueViewSchemaVersion || !validMethodToken(view.ID) ||
		!validSHA256(view.RiskSpecDigest) || len(view.Candidates) == 0 {
		return errors.New("EXPERIMENT_SEMANTIC_QUEUE_VIEW_INVALID")
	}
	for index, candidate := range view.Candidates {
		if !validSemanticCandidateID(candidate.CandidateID) || candidate.SemanticBucketVisits < 0 ||
			candidate.PrefixDecisions <= 0 || candidate.ActionKind.Validate() != nil ||
			(index > 0 && view.Candidates[index-1].CandidateID >= candidate.CandidateID) {
			return errors.New("EXPERIMENT_SEMANTIC_QUEUE_CANDIDATE_INVALID")
		}
		if !candidate.TargetUnreached && candidate.FirstMissingMilestone != "" {
			return errors.New("EXPERIMENT_SEMANTIC_QUEUE_PROGRESS_INVALID")
		}
	}
	sealed, err := view.seal()
	if err != nil || !validSHA256(view.Digest) || sealed.Digest != view.Digest {
		return errors.New("EXPERIMENT_SEMANTIC_QUEUE_VIEW_DIGEST_MISMATCH")
	}
	return nil
}

func projectSemanticPrefix(
	projector SemanticPrefixProjector,
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	result, err := projector.Project(id, spec, trace)
	if err != nil || result.Validate(spec) != nil || result.ID != id || result.ProjectorID != projector.ID() ||
		result.ExecutionDigest != trace.Digest || result.TargetIdentityDigest != trace.ManifestDigest {
		return semantic.RiskWitnessResult{}, errors.New("EXPERIMENT_SEMANTIC_PREFIX_PROJECTION_INVALID")
	}
	return result, nil
}

func validateSemanticQueueOrder(view SemanticQueueView, ordered []string) ([]string, error) {
	if view.Validate() != nil || len(ordered) != len(view.Candidates) {
		return nil, errors.New("EXPERIMENT_SEMANTIC_QUEUE_ORDER_INVALID")
	}
	known := make(map[string]bool, len(view.Candidates))
	for _, candidate := range view.Candidates {
		known[candidate.CandidateID] = true
	}
	result := append([]string(nil), ordered...)
	seen := make(map[string]bool, len(result))
	for _, candidateID := range result {
		if !known[candidateID] || seen[candidateID] {
			return nil, errors.New("EXPERIMENT_SEMANTIC_QUEUE_ORDER_INVALID")
		}
		seen[candidateID] = true
	}
	return result, nil
}

func semanticProgressBucket(result semantic.RiskWitnessResult) string {
	return result.Status + "\x00" + strings.Join(result.SatisfiedMilestones, "\x00") + "\x00" +
		strings.Join(result.MissingMilestones, "\x00")
}

func semanticQueueIndex(queue []semanticQueueEntry, candidateID string) int {
	for index := range queue {
		if queue[index].candidate.CandidateID == candidateID {
			return index
		}
	}
	return -1
}

func semanticCandidateID(ordinal int) string {
	return fmt.Sprintf("%s%06d", SemanticCandidateIDPrefix, ordinal)
}

func validSemanticCandidateID(id string) bool {
	if !strings.HasPrefix(id, SemanticCandidateIDPrefix) {
		return false
	}
	var ordinal int
	_, err := fmt.Sscanf(id, SemanticCandidateIDPrefix+"%06d", &ordinal)
	return err == nil && ordinal > 0 && id == semanticCandidateID(ordinal)
}

func cloneSemanticQueueCandidates(values []SemanticQueueCandidate) []SemanticQueueCandidate {
	result := append([]SemanticQueueCandidate(nil), values...)
	for index := range result {
		result[index].SatisfiedMilestones = append([]string(nil), result[index].SatisfiedMilestones...)
	}
	return result
}

func isNilSemanticComponent(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (view SemanticQueueView) seal() (SemanticQueueView, error) {
	view.Candidates = cloneSemanticQueueCandidates(view.Candidates)
	view.Digest = ""
	digest, err := control.CanonicalDigest(view)
	view.Digest = digest
	return view, err
}

func (result SemanticBestFirstResult) seal() (SemanticBestFirstResult, error) {
	result.Candidates = append([]SemanticCandidateRecord(nil), result.Candidates...)
	result.ExpansionOrder = append([]string(nil), result.ExpansionOrder...)
	result.Digest = ""
	digest, err := control.CanonicalDigest(result)
	result.Digest = digest
	return result, err
}

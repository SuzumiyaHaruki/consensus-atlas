package controlexperiment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	TestHypothesisSchemaVersion      = "consensus-atlas/test-hypothesis/v1"
	SemanticEpisodeViewSchemaVersion = "consensus-atlas/semantic-episode-view/v1"
	EpisodePlanSchemaVersion         = "consensus-atlas/episode-plan/v1"
	PlanningFeedbackSchemaVersion    = "consensus-atlas/planning-feedback/v1"
	EpisodeReportSchemaVersion       = "consensus-atlas/episode-report/v1"
	PlanningCandidateSetVersion      = "consensus-atlas/planning-candidate-set/v1"

	EpisodeExposureOpaqueSource     = "opaque-source-v1"
	EpisodeFeedbackPublicMechanical = "public-mechanical-v1"
	EpisodeOutcomePlanRejected      = "plan-rejected"
	EpisodeOutcomeExecuted          = "executed"

	EpisodeReasonPlanJSONInvalid      = "episode-plan-json-invalid"
	EpisodeReasonPlanBindingInvalid   = "episode-plan-binding-invalid"
	EpisodeReasonPlanCandidateInvalid = "episode-plan-candidate-set-invalid"
)

var (
	errEpisodePlanJSON      = errors.New("EXPERIMENT_EPISODE_PLAN_JSON_INVALID")
	errEpisodePlanBinding   = errors.New("EXPERIMENT_EPISODE_PLAN_BINDING_INVALID")
	errEpisodePlanCandidate = errors.New("EXPERIMENT_EPISODE_PLAN_CANDIDATE_SET_INVALID")
)

// TestHypothesis is a non-authoritative semantic claim. It identifies one
// existing trusted risk; root, algorithm, faults, budget, and stop rules remain
// outside the hypothesis and are bound by the trusted episode view/search.
type TestHypothesis struct {
	SchemaVersion   string `json:"schema_version"`
	ID              string `json:"id"`
	KnowledgeDigest string `json:"knowledge_digest"`
	RiskID          string `json:"risk_id"`
	RiskSpecDigest  string `json:"risk_spec_digest"`
	Rationale       string `json:"rationale"`
	Digest          string `json:"digest"`
}

// SemanticCandidateRef is the planner-visible projection of one already
// trusted WorkItem. CandidateID is an episode-local opaque ordinal; exact
// trace, Action, WorkItem, and target identities stay behind the compiler.
type SemanticCandidateRef struct {
	CandidateID           string             `json:"candidate_id"`
	ParentPrefixDecisions int                `json:"parent_prefix_decisions"`
	PathDepth             int                `json:"path_depth"`
	ActionKind            control.ActionKind `json:"action_kind"`
}

// SemanticEpisodeView is a trusted, read-only global candidate view. It does
// not expose Action IDs, WorkItem digests, target variant identity, private
// monitors, or verdicts. RiskProgress retains trusted trace/result binding
// digests; a trial-local wire projection must mask those before A5.
type SemanticEpisodeView struct {
	SchemaVersion              string                       `json:"schema_version"`
	ID                         string                       `json:"id"`
	HypothesisDigest           string                       `json:"hypothesis_digest"`
	SearchAlgorithmID          string                       `json:"search_algorithm_id"`
	RiskProgress               semantic.RiskWitnessProgress `json:"risk_progress"`
	Candidates                 []SemanticCandidateRef       `json:"candidates"`
	PlanningCandidateSetDigest string                       `json:"planning_candidate_set_digest"`
	ExposurePolicyID           string                       `json:"exposure_policy_id"`
	FeedbackPolicyID           string                       `json:"feedback_policy_id"`
	PriorFeedbackDigest        string                       `json:"prior_feedback_digest,omitempty"`
	Digest                     string                       `json:"digest"`
}

// EpisodePlan may only select the risk and provide a complete ordering of the
// frozen candidate set. It deliberately has no Action, algorithm, operator,
// root, fault, budget, stop, assertion, or verdict fields.
type EpisodePlan struct {
	SchemaVersion              string   `json:"schema_version"`
	ID                         string   `json:"id"`
	HypothesisID               string   `json:"hypothesis_id"`
	HypothesisDigest           string   `json:"hypothesis_digest"`
	ViewDigest                 string   `json:"view_digest"`
	PlanningCandidateSetDigest string   `json:"planning_candidate_set_digest"`
	TargetRiskID               string   `json:"target_risk_id"`
	OrderedCandidateIDs        []string `json:"ordered_candidate_ids"`
	Digest                     string   `json:"digest"`
}

// PlanningFeedback is the only Episode projection that may return to a formal
// planner. SearchWork and ExecutionWork reuse existing accounting types; no
// second ledger or evaluator outcome is embedded here.
type PlanningFeedback struct {
	SchemaVersion              string                       `json:"schema_version"`
	Outcome                    string                       `json:"outcome"`
	ReasonCode                 string                       `json:"reason_code,omitempty"`
	HypothesisDigest           string                       `json:"hypothesis_digest"`
	ViewDigest                 string                       `json:"view_digest"`
	PlanningCandidateSetDigest string                       `json:"planning_candidate_set_digest"`
	ProposalDigest             string                       `json:"proposal_digest"`
	PlanDigest                 string                       `json:"plan_digest,omitempty"`
	SelectedCandidateID        string                       `json:"selected_candidate_id,omitempty"`
	RiskProgress               semantic.RiskWitnessProgress `json:"risk_progress"`
	SearchWork                 StatelessDFSWork             `json:"search_work"`
	ExecutionWork              *WorkLedger                  `json:"execution_work,omitempty"`
	ModelWork                  ModelWork                    `json:"model_work"`
	Digest                     string                       `json:"digest"`
}

// EpisodeReport contains only online planning feedback. Private terminal
// evaluator results remain in the existing evaluator artifacts.
type EpisodeReport struct {
	SchemaVersion    string           `json:"schema_version"`
	ID               string           `json:"id"`
	PlanningFeedback PlanningFeedback `json:"planning_feedback"`
	Digest           string           `json:"digest"`
}

type planningCandidateSetIdentity struct {
	SchemaVersion string   `json:"schema_version"`
	SourceDigests []string `json:"source_digests"`
}

func (view SemanticEpisodeView) validateEpisodeSources(
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
	riskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
) error {
	return view.validateBaseSources(hypothesis, knowledge, riskSpec, riskResult, search, root)
}

func NewTestHypothesis(
	id string,
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
	rationale string,
) (TestHypothesis, error) {
	return NewTestHypothesisForBackend(
		id, knowledge, riskSpec, rationale, StatelessSearchBoundedDepthFirst,
	)
}

// NewTestHypothesisForBackend keeps hypothesis content independent from a
// search implementation while requiring the trusted composition root to bind
// one backend that the knowledge pack explicitly permits.
func NewTestHypothesisForBackend(
	id string,
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
	rationale string,
	backendID string,
) (TestHypothesis, error) {
	hypothesis := TestHypothesis{
		SchemaVersion: TestHypothesisSchemaVersion, ID: id,
		KnowledgeDigest: knowledge.Digest, RiskID: riskSpec.RiskID,
		RiskSpecDigest: riskSpec.Digest, Rationale: rationale,
	}
	sealed, err := hypothesis.seal()
	if err != nil {
		return TestHypothesis{}, err
	}
	if err := sealed.ValidateForBackend(knowledge, riskSpec, backendID); err != nil {
		return TestHypothesis{}, err
	}
	return sealed, nil
}

func (hypothesis TestHypothesis) Validate(
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
) error {
	return hypothesis.ValidateForBackend(knowledge, riskSpec, StatelessSearchBoundedDepthFirst)
}

// ValidateForBackend keeps TestHypothesis protocol-semantic while requiring
// the trusted composition root to name the concrete search backend.
func (hypothesis TestHypothesis) ValidateForBackend(
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
	backendID string,
) error {
	if hypothesis.validateIdentity() != nil || knowledge.Validate() != nil || riskSpec.Validate() != nil ||
		hypothesis.KnowledgeDigest != knowledge.Digest || hypothesis.RiskID != riskSpec.RiskID ||
		hypothesis.RiskSpecDigest != riskSpec.Digest || riskSpec.FamilyID != knowledge.Family ||
		!validMethodToken(backendID) {
		return errors.New("EXPERIMENT_TEST_HYPOTHESIS_SOURCE_INVALID")
	}
	for _, risk := range knowledge.Risks {
		if risk.ID == hypothesis.RiskID {
			if !containsString(risk.AllowedBackendIDs, backendID) {
				return errors.New("EXPERIMENT_TEST_HYPOTHESIS_BACKEND_UNSUPPORTED")
			}
			return nil
		}
	}
	return errors.New("EXPERIMENT_TEST_HYPOTHESIS_RISK_UNKNOWN")
}

// ValidateIdentity permits transport and presentation layers to reject a
// tampered public hypothesis without acquiring the risk spec needed for full
// trusted source validation.
func (hypothesis TestHypothesis) ValidateIdentity() error {
	return hypothesis.validateIdentity()
}

// NewSemanticEpisodeView derives planner-visible progress from the exact
// trusted RiskWitnessResult. A prior report is accepted only for the A1
// rejection-repair path and must validate against its own prior view plus the
// same trusted search sources.
func NewSemanticEpisodeView(
	id string,
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
	riskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
	exposurePolicyID string,
	feedbackPolicyID string,
	priorReport *EpisodeReport,
	priorView *SemanticEpisodeView,
	priorProposal []byte,
	priorModelWork ModelWork,
) (SemanticEpisodeView, error) {
	if hypothesis.Validate(knowledge, riskSpec) != nil || search.Validate(root) != nil ||
		riskResult.Validate(riskSpec) != nil || riskResult.ExecutionDigest != root.Digest ||
		riskResult.TargetIdentityDigest != root.ManifestDigest ||
		exposurePolicyID != EpisodeExposureOpaqueSource ||
		feedbackPolicyID != EpisodeFeedbackPublicMechanical {
		return SemanticEpisodeView{}, errors.New("EXPERIMENT_SEMANTIC_EPISODE_VIEW_INPUT_INVALID")
	}
	progress, err := semantic.NewRiskWitnessProgress(riskSpec, riskResult)
	if err != nil {
		return SemanticEpisodeView{}, err
	}
	candidates, setDigest, err := semanticCandidates(search)
	if err != nil {
		return SemanticEpisodeView{}, err
	}
	view := SemanticEpisodeView{
		SchemaVersion: SemanticEpisodeViewSchemaVersion, ID: id,
		HypothesisDigest: hypothesis.Digest, SearchAlgorithmID: StatelessSearchBoundedDepthFirst,
		RiskProgress: progress, Candidates: candidates, PlanningCandidateSetDigest: setDigest,
		ExposurePolicyID: exposurePolicyID, FeedbackPolicyID: feedbackPolicyID,
	}
	if (priorReport == nil) != (priorView == nil) ||
		(priorReport == nil && (priorProposal != nil || priorModelWork != (ModelWork{}))) {
		return SemanticEpisodeView{}, errors.New("EXPERIMENT_SEMANTIC_EPISODE_PRIOR_PAIR_INVALID")
	}
	if priorReport != nil {
		if priorReport.ValidateRejectedSources(
			hypothesis, knowledge, *priorView, riskSpec, riskResult, search, root,
			priorProposal, priorModelWork,
		) != nil || priorView.PlanningCandidateSetDigest != setDigest {
			return SemanticEpisodeView{}, errors.New("EXPERIMENT_SEMANTIC_EPISODE_PRIOR_FEEDBACK_INVALID")
		}
		view.PriorFeedbackDigest = priorReport.PlanningFeedback.Digest
	}
	sealed, err := view.seal()
	if err != nil {
		return SemanticEpisodeView{}, err
	}
	if priorReport == nil {
		err = sealed.ValidateInitialSources(hypothesis, knowledge, riskSpec, riskResult, search, root)
	} else {
		err = sealed.ValidateRepairSources(
			hypothesis, knowledge, *priorView, *priorReport, riskSpec, riskResult,
			search, root, priorProposal, priorModelWork,
		)
	}
	if err != nil {
		return SemanticEpisodeView{}, err
	}
	return sealed, nil
}

// ValidateInitialSources validates a view that has no prior feedback. A
// feedback-bearing view instead requires ValidateRepairSources so its prior
// report and exact rejected proposal cannot be replaced by a bare digest.
func (view SemanticEpisodeView) ValidateInitialSources(
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
	riskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
) error {
	if view.PriorFeedbackDigest != "" {
		return errors.New("EXPERIMENT_SEMANTIC_EPISODE_UNEXPECTED_PRIOR_FEEDBACK")
	}
	return view.validateBaseSources(hypothesis, knowledge, riskSpec, riskResult, search, root)
}

// ValidateRepairSources binds a repaired view to the exact mechanically
// rejected proposal and report that authorized the next planning attempt.
func (view SemanticEpisodeView) ValidateRepairSources(
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	priorView SemanticEpisodeView,
	priorReport EpisodeReport,
	riskSpec semantic.RiskWitnessSpec,
	riskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
	priorProposal []byte,
	priorModelWork ModelWork,
) error {
	if view.validateBaseSources(hypothesis, knowledge, riskSpec, riskResult, search, root) != nil ||
		priorView.ValidateInitialSources(hypothesis, knowledge, riskSpec, riskResult, search, root) != nil ||
		priorReport.ValidateRejectedSources(
			hypothesis, knowledge, priorView, riskSpec, riskResult, search, root, priorProposal, priorModelWork,
		) != nil || view.PriorFeedbackDigest != priorReport.PlanningFeedback.Digest ||
		view.PlanningCandidateSetDigest != priorView.PlanningCandidateSetDigest {
		return errors.New("EXPERIMENT_SEMANTIC_EPISODE_REPAIR_SOURCE_INVALID")
	}
	return nil
}

func (view SemanticEpisodeView) validateBaseSources(
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
	riskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
) error {
	if hypothesis.Validate(knowledge, riskSpec) != nil || view.validateIdentity(hypothesis, riskSpec) != nil ||
		search.Validate(root) != nil || riskResult.Validate(riskSpec) != nil ||
		riskResult.ExecutionDigest != root.Digest || riskResult.TargetIdentityDigest != root.ManifestDigest ||
		view.RiskProgress.ValidateSource(riskSpec, riskResult) != nil {
		return errors.New("EXPERIMENT_SEMANTIC_EPISODE_VIEW_SOURCE_INVALID")
	}
	candidates, setDigest, err := semanticCandidates(search)
	if err != nil || setDigest != view.PlanningCandidateSetDigest ||
		!reflect.DeepEqual(candidates, view.Candidates) {
		return errors.New("EXPERIMENT_SEMANTIC_EPISODE_CANDIDATES_MISMATCH")
	}
	return nil
}

func NewEpisodePlan(
	id string,
	hypothesis TestHypothesis,
	view SemanticEpisodeView,
	riskSpec semantic.RiskWitnessSpec,
	orderedCandidateIDs []string,
) (EpisodePlan, error) {
	plan := EpisodePlan{
		SchemaVersion: EpisodePlanSchemaVersion, ID: id,
		HypothesisID: hypothesis.ID, HypothesisDigest: hypothesis.Digest,
		ViewDigest: view.Digest, PlanningCandidateSetDigest: view.PlanningCandidateSetDigest,
		TargetRiskID:        hypothesis.RiskID,
		OrderedCandidateIDs: append([]string(nil), orderedCandidateIDs...),
	}
	sealed, err := plan.seal()
	if err != nil {
		return EpisodePlan{}, err
	}
	if err := sealed.Validate(hypothesis, view, riskSpec); err != nil {
		return EpisodePlan{}, err
	}
	return sealed, nil
}

// ParseEpisodePlan is the strict model boundary. Unknown fields fail closed,
// and inbound plans cannot supply a trusted digest.
func ParseEpisodePlan(
	data []byte,
	hypothesis TestHypothesis,
	view SemanticEpisodeView,
	riskSpec semantic.RiskWitnessSpec,
) (EpisodePlan, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var plan EpisodePlan
	if err := decoder.Decode(&plan); err != nil {
		return EpisodePlan{}, fmt.Errorf("%w: decode", errEpisodePlanJSON)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return EpisodePlan{}, fmt.Errorf("%w: trailing", errEpisodePlanJSON)
	}
	if plan.SchemaVersion != EpisodePlanSchemaVersion || plan.Digest != "" {
		return EpisodePlan{}, fmt.Errorf("%w: identity", errEpisodePlanBinding)
	}
	sealed, err := plan.seal()
	if err != nil {
		return EpisodePlan{}, fmt.Errorf("%w: digest", errEpisodePlanBinding)
	}
	if err := sealed.Validate(hypothesis, view, riskSpec); err != nil {
		return EpisodePlan{}, err
	}
	return sealed, nil
}

func (plan EpisodePlan) Validate(
	hypothesis TestHypothesis,
	view SemanticEpisodeView,
	riskSpec semantic.RiskWitnessSpec,
) error {
	if hypothesis.validateIdentity() != nil || view.validateIdentity(hypothesis, riskSpec) != nil ||
		plan.SchemaVersion != EpisodePlanSchemaVersion || !validMethodToken(plan.ID) ||
		plan.HypothesisID != hypothesis.ID || plan.HypothesisDigest != hypothesis.Digest ||
		plan.ViewDigest != view.Digest ||
		plan.PlanningCandidateSetDigest != view.PlanningCandidateSetDigest ||
		plan.TargetRiskID != hypothesis.RiskID || len(plan.OrderedCandidateIDs) == 0 ||
		len(plan.OrderedCandidateIDs) != len(view.Candidates) {
		return errEpisodePlanBinding
	}
	available := make(map[string]bool, len(view.Candidates))
	for _, candidate := range view.Candidates {
		available[candidate.CandidateID] = true
	}
	seen := make(map[string]bool, len(plan.OrderedCandidateIDs))
	for _, candidateID := range plan.OrderedCandidateIDs {
		if !available[candidateID] || seen[candidateID] {
			return errEpisodePlanCandidate
		}
		seen[candidateID] = true
	}
	sealed, err := plan.seal()
	if err != nil || !validSHA256(plan.Digest) || sealed.Digest != plan.Digest {
		return fmt.Errorf("%w: digest", errEpisodePlanBinding)
	}
	return nil
}

// CompileSemanticEpisodePlan resolves only the first trusted global candidate
// into the existing exact-path compiler. No plan field can create an Action.
func CompileSemanticEpisodePlan(
	id string,
	plan EpisodePlan,
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	view SemanticEpisodeView,
	riskSpec semantic.RiskWitnessSpec,
	riskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
) (StatelessDFSWorkItem, Policy, error) {
	if !validMethodToken(id) || plan.Validate(hypothesis, view, riskSpec) != nil ||
		view.validateEpisodeSources(hypothesis, knowledge, riskSpec, riskResult, search, root) != nil {
		return StatelessDFSWorkItem{}, Policy{}, errors.New("EXPERIMENT_EPISODE_COMPILE_INPUT_INVALID")
	}
	item, err := resolveEpisodeCandidate(plan.OrderedCandidateIDs[0], search)
	if err != nil {
		return StatelessDFSWorkItem{}, Policy{}, err
	}
	policy, err := CompileStatelessDFSPath(
		id, search, root, item.Ordinal, []control.ActionKind{item.Action.Kind}, item.Path.Decision,
	)
	return item, policy, err
}

// NewRejectedEpisodeReport reruns the strict parser over exact proposal bytes.
// A valid proposal therefore cannot be relabelled as a rejection, and the
// stable reason code is mechanically derived rather than caller-authored.
func NewRejectedEpisodeReport(
	id string,
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	view SemanticEpisodeView,
	riskSpec semantic.RiskWitnessSpec,
	riskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
	proposal []byte,
	modelWork ModelWork,
) (EpisodeReport, error) {
	if view.validateEpisodeSources(hypothesis, knowledge, riskSpec, riskResult, search, root) != nil ||
		!validStatelessPlannerWork(modelWork) {
		return EpisodeReport{}, errors.New("EXPERIMENT_EPISODE_REJECTION_INPUT_INVALID")
	}
	if _, err := ParseEpisodePlan(proposal, hypothesis, view, riskSpec); err == nil {
		return EpisodeReport{}, errors.New("EXPERIMENT_EPISODE_REJECTION_NOT_APPLICABLE")
	} else if reason := episodePlanReason(err); reason != "" {
		feedback, sealErr := (PlanningFeedback{
			SchemaVersion: PlanningFeedbackSchemaVersion, Outcome: EpisodeOutcomePlanRejected,
			ReasonCode: reason, HypothesisDigest: hypothesis.Digest, ViewDigest: view.Digest,
			PlanningCandidateSetDigest: view.PlanningCandidateSetDigest,
			ProposalDigest:             AgentInvocationDigest(proposal), RiskProgress: cloneRiskProgress(view.RiskProgress),
			SearchWork: search.Work, ModelWork: modelWork,
		}).seal()
		if sealErr != nil {
			return EpisodeReport{}, sealErr
		}
		report, sealErr := (EpisodeReport{
			SchemaVersion: EpisodeReportSchemaVersion, ID: id, PlanningFeedback: feedback,
		}).seal()
		if sealErr != nil {
			return EpisodeReport{}, sealErr
		}
		if report.ValidateRejectedSources(
			hypothesis, knowledge, view, riskSpec, riskResult, search, root, proposal, modelWork,
		) != nil {
			return EpisodeReport{}, errors.New("EXPERIMENT_EPISODE_REJECTION_SOURCE_INVALID")
		}
		return report, nil
	}
	return EpisodeReport{}, errors.New("EXPERIMENT_EPISODE_REJECTION_REASON_UNKNOWN")
}

func NewExecutedEpisodeReport(
	id string,
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	view SemanticEpisodeView,
	plan EpisodePlan,
	riskSpec semantic.RiskWitnessSpec,
	rootRiskResult semantic.RiskWitnessResult,
	executedRiskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
	actual controlruntime.Trace,
	proposal []byte,
	executionWork WorkLedger,
	expectedModelWork ModelWork,
) (EpisodeReport, error) {
	if view.validateEpisodeSources(hypothesis, knowledge, riskSpec, rootRiskResult, search, root) != nil ||
		plan.Validate(hypothesis, view, riskSpec) != nil || actual.Validate() != nil ||
		executedRiskResult.Validate(riskSpec) != nil || executedRiskResult.ExecutionDigest != actual.Digest ||
		executedRiskResult.TargetIdentityDigest != actual.ManifestDigest ||
		executionWork.Model != expectedModelWork || !validStatelessPlannerWork(expectedModelWork) {
		return EpisodeReport{}, errors.New("EXPERIMENT_EPISODE_EXECUTION_INPUT_INVALID")
	}
	parsed, err := ParseEpisodePlan(proposal, hypothesis, view, riskSpec)
	if err != nil || !reflect.DeepEqual(parsed, plan) {
		return EpisodeReport{}, errors.New("EXPERIMENT_EPISODE_EXECUTION_PROPOSAL_MISMATCH")
	}
	item, err := resolveEpisodeCandidate(plan.OrderedCandidateIDs[0], search)
	if err != nil || actual.Digest != item.ChildPrefixDigest ||
		executionWork != expectedEpisodeExecutionWork(root, actual, executionWork.Model) {
		return EpisodeReport{}, errors.New("EXPERIMENT_EPISODE_EXECUTION_SOURCE_MISMATCH")
	}
	progress, err := semantic.NewRiskWitnessProgress(riskSpec, executedRiskResult)
	if err != nil {
		return EpisodeReport{}, err
	}
	work := executionWork
	feedback, err := (PlanningFeedback{
		SchemaVersion: PlanningFeedbackSchemaVersion, Outcome: EpisodeOutcomeExecuted,
		HypothesisDigest: hypothesis.Digest, ViewDigest: view.Digest,
		PlanningCandidateSetDigest: view.PlanningCandidateSetDigest,
		ProposalDigest:             AgentInvocationDigest(proposal), PlanDigest: plan.Digest,
		SelectedCandidateID: plan.OrderedCandidateIDs[0], RiskProgress: progress,
		SearchWork: search.Work, ExecutionWork: &work, ModelWork: executionWork.Model,
	}).seal()
	if err != nil {
		return EpisodeReport{}, err
	}
	report, err := (EpisodeReport{
		SchemaVersion: EpisodeReportSchemaVersion, ID: id, PlanningFeedback: feedback,
	}).seal()
	if err != nil {
		return EpisodeReport{}, err
	}
	if report.ValidateExecutedSources(
		hypothesis, knowledge, view, plan, riskSpec, rootRiskResult, executedRiskResult,
		search, root, actual, proposal,
		expectedModelWork,
	) != nil {
		return EpisodeReport{}, errors.New("EXPERIMENT_EPISODE_EXECUTION_SOURCE_INVALID")
	}
	return report, nil
}

func (report EpisodeReport) ValidateRejectedSources(
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	view SemanticEpisodeView,
	riskSpec semantic.RiskWitnessSpec,
	riskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
	proposal []byte,
	expectedModelWork ModelWork,
) error {
	feedback := report.PlanningFeedback
	_, parseErr := ParseEpisodePlan(proposal, hypothesis, view, riskSpec)
	if report.validateIdentity(riskSpec) != nil ||
		view.validateEpisodeSources(hypothesis, knowledge, riskSpec, riskResult, search, root) != nil ||
		parseErr == nil || episodePlanReason(parseErr) != feedback.ReasonCode ||
		feedback.Outcome != EpisodeOutcomePlanRejected || !validEpisodeReason(feedback.ReasonCode) ||
		feedback.HypothesisDigest != hypothesis.Digest || feedback.ViewDigest != view.Digest ||
		feedback.PlanningCandidateSetDigest != view.PlanningCandidateSetDigest ||
		feedback.ProposalDigest != AgentInvocationDigest(proposal) ||
		!validStatelessPlannerWork(expectedModelWork) || feedback.ModelWork != expectedModelWork ||
		!reflect.DeepEqual(feedback.RiskProgress, view.RiskProgress) || feedback.SearchWork != search.Work {
		return errors.New("EXPERIMENT_EPISODE_REJECTION_SOURCE_INVALID")
	}
	return nil
}

func (report EpisodeReport) ValidateExecutedSources(
	hypothesis TestHypothesis,
	knowledge ProtocolKnowledgePack,
	view SemanticEpisodeView,
	plan EpisodePlan,
	riskSpec semantic.RiskWitnessSpec,
	rootRiskResult semantic.RiskWitnessResult,
	executedRiskResult semantic.RiskWitnessResult,
	search StatelessDFSResult,
	root controlruntime.Trace,
	actual controlruntime.Trace,
	proposal []byte,
	expectedModelWork ModelWork,
) error {
	feedback := report.PlanningFeedback
	item, err := resolveEpisodeCandidate(feedback.SelectedCandidateID, search)
	if err != nil || report.validateIdentity(riskSpec) != nil ||
		view.validateEpisodeSources(hypothesis, knowledge, riskSpec, rootRiskResult, search, root) != nil ||
		plan.Validate(hypothesis, view, riskSpec) != nil || actual.Validate() != nil ||
		executedRiskResult.Validate(riskSpec) != nil ||
		feedback.RiskProgress.ValidateSource(riskSpec, executedRiskResult) != nil ||
		executedRiskResult.ExecutionDigest != actual.Digest ||
		executedRiskResult.TargetIdentityDigest != actual.ManifestDigest ||
		!validStatelessPlannerWork(expectedModelWork) || feedback.ModelWork != expectedModelWork ||
		feedback.Outcome != EpisodeOutcomeExecuted || feedback.ReasonCode != "" ||
		feedback.HypothesisDigest != hypothesis.Digest || feedback.ViewDigest != view.Digest ||
		feedback.PlanningCandidateSetDigest != view.PlanningCandidateSetDigest ||
		feedback.ProposalDigest != AgentInvocationDigest(proposal) || feedback.PlanDigest != plan.Digest ||
		feedback.SelectedCandidateID != plan.OrderedCandidateIDs[0] || actual.Digest != item.ChildPrefixDigest ||
		feedback.SearchWork != search.Work || feedback.ExecutionWork == nil ||
		*feedback.ExecutionWork != expectedEpisodeExecutionWork(root, actual, expectedModelWork) {
		return errors.New("EXPERIMENT_EPISODE_EXECUTION_SOURCE_INVALID")
	}
	parsed, err := ParseEpisodePlan(proposal, hypothesis, view, riskSpec)
	if err != nil || !reflect.DeepEqual(parsed, plan) {
		return errors.New("EXPERIMENT_EPISODE_EXECUTION_PROPOSAL_MISMATCH")
	}
	return nil
}

func expectedEpisodeExecutionWork(
	root controlruntime.Trace,
	actual controlruntime.Trace,
	model ModelWork,
) WorkLedger {
	primary := prefixReplayWork(root)
	chargeDecisions(&primary, len(actual.Records)-len(root.Records))
	replay := prefixReplayWork(actual)
	return WorkLedger{
		Primary: primary, Replay: replay, Model: model,
		Resources: ResourceAccounting{
			WallTime: ResourceNotCollected, CPUTime: ResourceNotCollected, PeakRSS: ResourceNotCollected,
		},
	}
}

func (hypothesis TestHypothesis) validateIdentity() error {
	if hypothesis.SchemaVersion != TestHypothesisSchemaVersion || !validMethodToken(hypothesis.ID) ||
		!validSHA256(hypothesis.KnowledgeDigest) || !validMethodToken(hypothesis.RiskID) ||
		!validSHA256(hypothesis.RiskSpecDigest) || hypothesis.Rationale == "" ||
		len(hypothesis.Rationale) > 2048 || strings.TrimSpace(hypothesis.Rationale) != hypothesis.Rationale {
		return errors.New("EXPERIMENT_TEST_HYPOTHESIS_INVALID")
	}
	sealed, err := hypothesis.seal()
	if err != nil || !validSHA256(hypothesis.Digest) || sealed.Digest != hypothesis.Digest {
		return errors.New("EXPERIMENT_TEST_HYPOTHESIS_DIGEST_MISMATCH")
	}
	return nil
}

func (view SemanticEpisodeView) validateIdentity(
	hypothesis TestHypothesis,
	riskSpec semantic.RiskWitnessSpec,
) error {
	if hypothesis.validateIdentity() != nil || riskSpec.Validate() != nil ||
		view.SchemaVersion != SemanticEpisodeViewSchemaVersion || !validMethodToken(view.ID) ||
		view.HypothesisDigest != hypothesis.Digest || view.SearchAlgorithmID != StatelessSearchBoundedDepthFirst ||
		view.RiskProgress.Validate(riskSpec) != nil || !validSHA256(view.PlanningCandidateSetDigest) ||
		view.ExposurePolicyID != EpisodeExposureOpaqueSource ||
		view.FeedbackPolicyID != EpisodeFeedbackPublicMechanical ||
		(view.PriorFeedbackDigest != "" && !validSHA256(view.PriorFeedbackDigest)) {
		return errors.New("EXPERIMENT_SEMANTIC_EPISODE_VIEW_INVALID")
	}
	for index, candidate := range view.Candidates {
		if candidate.validate() != nil ||
			(index > 0 && view.Candidates[index-1].CandidateID >= candidate.CandidateID) {
			return errors.New("EXPERIMENT_SEMANTIC_EPISODE_CANDIDATE_INVALID")
		}
	}
	sealed, err := view.seal()
	if err != nil || !validSHA256(view.Digest) || sealed.Digest != view.Digest {
		return errors.New("EXPERIMENT_SEMANTIC_EPISODE_VIEW_DIGEST_MISMATCH")
	}
	return nil
}

func (candidate SemanticCandidateRef) validate() error {
	if !validMethodToken(candidate.CandidateID) || candidate.ParentPrefixDecisions < 0 ||
		candidate.PathDepth <= 0 || candidate.ActionKind.Validate() != nil {
		return errors.New("EXPERIMENT_SEMANTIC_CANDIDATE_INVALID")
	}
	return nil
}

func (feedback PlanningFeedback) validateIdentity(riskSpec semantic.RiskWitnessSpec) error {
	if feedback.SchemaVersion != PlanningFeedbackSchemaVersion ||
		!validSHA256(feedback.HypothesisDigest) || !validSHA256(feedback.ViewDigest) ||
		!validSHA256(feedback.PlanningCandidateSetDigest) || !validSHA256(feedback.ProposalDigest) ||
		feedback.RiskProgress.Validate(riskSpec) != nil || !validSearchWork(feedback.SearchWork) ||
		!validStatelessPlannerWork(feedback.ModelWork) {
		return errors.New("EXPERIMENT_PLANNING_FEEDBACK_INVALID")
	}
	switch feedback.Outcome {
	case EpisodeOutcomePlanRejected:
		if !validEpisodeReason(feedback.ReasonCode) || feedback.PlanDigest != "" ||
			feedback.SelectedCandidateID != "" || feedback.ExecutionWork != nil {
			return errors.New("EXPERIMENT_PLANNING_FEEDBACK_REJECTION_INVALID")
		}
	case EpisodeOutcomeExecuted:
		if feedback.ReasonCode != "" || !validSHA256(feedback.PlanDigest) ||
			!validMethodToken(feedback.SelectedCandidateID) || feedback.ExecutionWork == nil ||
			validateMethodWork(*feedback.ExecutionWork) != nil || feedback.ExecutionWork.Model != feedback.ModelWork {
			return errors.New("EXPERIMENT_PLANNING_FEEDBACK_EXECUTION_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_PLANNING_FEEDBACK_OUTCOME_INVALID")
	}
	sealed, err := feedback.seal()
	if err != nil || !validSHA256(feedback.Digest) || sealed.Digest != feedback.Digest {
		return errors.New("EXPERIMENT_PLANNING_FEEDBACK_DIGEST_MISMATCH")
	}
	return nil
}

func (report EpisodeReport) validateIdentity(riskSpec semantic.RiskWitnessSpec) error {
	if report.SchemaVersion != EpisodeReportSchemaVersion || !validMethodToken(report.ID) ||
		report.PlanningFeedback.validateIdentity(riskSpec) != nil {
		return errors.New("EXPERIMENT_EPISODE_REPORT_INVALID")
	}
	sealed, err := report.seal()
	if err != nil || !validSHA256(report.Digest) || sealed.Digest != report.Digest {
		return errors.New("EXPERIMENT_EPISODE_REPORT_DIGEST_MISMATCH")
	}
	return nil
}

func semanticCandidates(search StatelessDFSResult) ([]SemanticCandidateRef, string, error) {
	items := append([]StatelessDFSWorkItem(nil), search.Items...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].ChildPrefixDigest != items[j].ChildPrefixDigest {
			return items[i].ChildPrefixDigest < items[j].ChildPrefixDigest
		}
		return items[i].Digest < items[j].Digest
	})
	candidates := make([]SemanticCandidateRef, len(items))
	sources := make([]string, len(items))
	for index, item := range items {
		candidates[index] = SemanticCandidateRef{
			CandidateID:           fmt.Sprintf("candidate-%06d", index+1),
			ParentPrefixDecisions: item.State.PrefixDecisions,
			PathDepth:             item.Path.Depth, ActionKind: item.Action.Kind,
		}
		sources[index] = item.Digest
		if candidates[index].validate() != nil || !validSHA256(sources[index]) {
			return nil, "", errors.New("EXPERIMENT_SEMANTIC_EPISODE_CANDIDATE_INVALID")
		}
	}
	digest, err := control.CanonicalDigest(planningCandidateSetIdentity{
		SchemaVersion: PlanningCandidateSetVersion, SourceDigests: sources,
	})
	return candidates, digest, err
}

func resolveEpisodeCandidate(
	candidateID string,
	search StatelessDFSResult,
) (StatelessDFSWorkItem, error) {
	items := append([]StatelessDFSWorkItem(nil), search.Items...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].ChildPrefixDigest != items[j].ChildPrefixDigest {
			return items[i].ChildPrefixDigest < items[j].ChildPrefixDigest
		}
		return items[i].Digest < items[j].Digest
	})
	ordinal, err := strconv.Atoi(strings.TrimPrefix(candidateID, "candidate-"))
	if err != nil ||
		ordinal <= 0 || ordinal > len(items) || candidateID != fmt.Sprintf("candidate-%06d", ordinal) {
		return StatelessDFSWorkItem{}, errors.New("EXPERIMENT_EPISODE_CANDIDATE_UNKNOWN")
	}
	return items[ordinal-1], nil
}

func episodePlanReason(err error) string {
	switch {
	case errors.Is(err, errEpisodePlanJSON):
		return EpisodeReasonPlanJSONInvalid
	case errors.Is(err, errEpisodePlanCandidate):
		return EpisodeReasonPlanCandidateInvalid
	case errors.Is(err, errEpisodePlanBinding):
		return EpisodeReasonPlanBindingInvalid
	default:
		return ""
	}
}

func validEpisodeReason(reason string) bool {
	return reason == EpisodeReasonPlanJSONInvalid || reason == EpisodeReasonPlanBindingInvalid ||
		reason == EpisodeReasonPlanCandidateInvalid
}

func validSearchWork(work StatelessDFSWork) bool {
	return validDFSPhaseWork(work.FrontierReconstruction) &&
		validDFSPhaseWork(work.ChildMaterialization) && validDFSPhaseWork(work.ChildVerification) &&
		work.TotalWorkUnits == work.FrontierReconstruction.WorkUnits+
			work.ChildMaterialization.WorkUnits+work.ChildVerification.WorkUnits
}

func cloneRiskProgress(progress semantic.RiskWitnessProgress) semantic.RiskWitnessProgress {
	progress.SatisfiedMilestones = append([]string(nil), progress.SatisfiedMilestones...)
	return progress
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (hypothesis TestHypothesis) seal() (TestHypothesis, error) {
	hypothesis.Digest = ""
	digest, err := control.CanonicalDigest(hypothesis)
	hypothesis.Digest = digest
	return hypothesis, err
}

func (view SemanticEpisodeView) seal() (SemanticEpisodeView, error) {
	view.RiskProgress = cloneRiskProgress(view.RiskProgress)
	view.Candidates = append([]SemanticCandidateRef(nil), view.Candidates...)
	view.Digest = ""
	digest, err := control.CanonicalDigest(view)
	view.Digest = digest
	return view, err
}

func (plan EpisodePlan) seal() (EpisodePlan, error) {
	plan.OrderedCandidateIDs = append([]string(nil), plan.OrderedCandidateIDs...)
	plan.Digest = ""
	digest, err := control.CanonicalDigest(plan)
	plan.Digest = digest
	return plan, err
}

func (feedback PlanningFeedback) seal() (PlanningFeedback, error) {
	feedback.RiskProgress = cloneRiskProgress(feedback.RiskProgress)
	if feedback.ExecutionWork != nil {
		work := *feedback.ExecutionWork
		feedback.ExecutionWork = &work
	}
	feedback.Digest = ""
	digest, err := control.CanonicalDigest(feedback)
	feedback.Digest = digest
	return feedback, err
}

func (report EpisodeReport) seal() (EpisodeReport, error) {
	feedback, err := report.PlanningFeedback.seal()
	if err != nil {
		return EpisodeReport{}, err
	}
	report.PlanningFeedback = feedback
	report.Digest = ""
	digest, err := control.CanonicalDigest(report)
	report.Digest = digest
	return report, err
}

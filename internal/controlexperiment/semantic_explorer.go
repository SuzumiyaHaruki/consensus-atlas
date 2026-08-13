package controlexperiment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	SemanticExplorerRequestVersion  = "consensus-atlas/semantic-explorer-request/v1"
	SemanticExplorerProposalVersion = "consensus-atlas/semantic-explorer-proposal/v1"
	SemanticExplorerFeedbackVersion = "consensus-atlas/semantic-explorer-feedback/v1"
	SemanticExplorerCallVersion     = "consensus-atlas/semantic-explorer-call/v1"
	SemanticExplorerResultVersion   = "consensus-atlas/semantic-explorer-result/v1"
	SemanticExplorerFailureVersion  = "consensus-atlas/semantic-explorer-failure/v1"

	SemanticExplorerCallAccepted = "accepted"
	SemanticExplorerCallRejected = "proposal-rejected"

	SemanticExplorerFeedbackRejected = "proposal-rejected"
	SemanticExplorerFeedbackExpanded = "selected-prefix-expanded"

	SemanticExplorerReasonJSON      = "semantic-explorer-proposal-json-invalid"
	SemanticExplorerReasonBinding   = "semantic-explorer-proposal-binding-invalid"
	SemanticExplorerReasonCandidate = "semantic-explorer-candidate-set-invalid"

	SemanticExplorerFailurePlanner     = "semantic-explorer-planner-failed"
	SemanticExplorerFailureCallBudget  = "semantic-explorer-call-budget-exhausted"
	SemanticExplorerFailureTokenBudget = "semantic-explorer-token-budget-exceeded"
	SemanticExplorerFailureAllRejected = "semantic-explorer-all-proposals-rejected"
)

var (
	errSemanticExplorerJSON      = errors.New("EXPERIMENT_SEMANTIC_EXPLORER_PROPOSAL_JSON_INVALID")
	errSemanticExplorerBinding   = errors.New("EXPERIMENT_SEMANTIC_EXPLORER_PROPOSAL_BINDING_INVALID")
	errSemanticExplorerCandidate = errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CANDIDATE_SET_INVALID")
)

type SemanticExplorerBudget struct {
	MaxCalls  int `json:"max_calls"`
	MaxTokens int `json:"max_tokens"`
}

func (budget SemanticExplorerBudget) Validate() error {
	if budget.MaxCalls <= 0 || budget.MaxTokens <= 0 {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_BUDGET_INVALID")
	}
	return nil
}

// SemanticExplorerFeedback is mechanically derived from the immediately
// preceding call. It contains no evaluator outcome or private target identity.
type SemanticExplorerFeedback struct {
	SchemaVersion       string `json:"schema_version"`
	Outcome             string `json:"outcome"`
	ReasonCode          string `json:"reason_code,omitempty"`
	PriorRequestDigest  string `json:"prior_request_digest"`
	PriorQueueDigest    string `json:"prior_queue_digest"`
	ProposalDigest      string `json:"proposal_digest"`
	SelectedCandidateID string `json:"selected_candidate_id,omitempty"`
	CurrentQueueDigest  string `json:"current_queue_digest"`
	Digest              string `json:"digest"`
}

type SemanticExplorerRequest struct {
	SchemaVersion    string                    `json:"schema_version"`
	ID               string                    `json:"id"`
	Ordinal          int                       `json:"ordinal"`
	AlgorithmID      string                    `json:"algorithm_id"`
	GuidanceID       string                    `json:"guidance_id"`
	KnowledgeDigest  string                    `json:"knowledge_digest"`
	HypothesisDigest string                    `json:"hypothesis_digest"`
	RemainingCalls   int                       `json:"remaining_calls"`
	RemainingTokens  int                       `json:"remaining_tokens"`
	Queue            SemanticQueueView         `json:"queue"`
	PriorFeedback    *SemanticExplorerFeedback `json:"prior_feedback,omitempty"`
	MutableFields    []string                  `json:"mutable_fields"`
	Digest           string                    `json:"digest"`
}

// SemanticExplorerAgentView is the complete public input to one planner call.
// Only OrderedCandidateIDs in the proposal are mutable.
type SemanticExplorerAgentView struct {
	Knowledge  ProtocolKnowledgePack   `json:"knowledge"`
	Hypothesis TestHypothesis          `json:"hypothesis"`
	Request    SemanticExplorerRequest `json:"request"`
}

type SemanticExplorerProposal struct {
	SchemaVersion       string   `json:"schema_version"`
	ID                  string   `json:"id"`
	RequestDigest       string   `json:"request_digest"`
	QueueDigest         string   `json:"queue_digest"`
	OrderedCandidateIDs []string `json:"ordered_candidate_ids"`
	Digest              string   `json:"digest"`
}

// SemanticExplorerCall persists the exact semantic response bytes. Provider
// transport identity remains in the existing Agent call journal composition.
type SemanticExplorerCall struct {
	SchemaVersion  string                    `json:"schema_version"`
	Request        SemanticExplorerRequest   `json:"request"`
	Status         string                    `json:"status"`
	ReasonCode     string                    `json:"reason_code,omitempty"`
	ResponseBytes  []byte                    `json:"response_bytes"`
	ResponseDigest string                    `json:"response_digest"`
	Proposal       *SemanticExplorerProposal `json:"proposal,omitempty"`
	ModelWork      ModelWork                 `json:"model_work"`
	Digest         string                    `json:"digest"`
}

type SemanticExplorerResult struct {
	SchemaVersion    string                  `json:"schema_version"`
	GuidanceID       string                  `json:"guidance_id"`
	KnowledgeDigest  string                  `json:"knowledge_digest"`
	HypothesisDigest string                  `json:"hypothesis_digest"`
	Budget           SemanticExplorerBudget  `json:"budget"`
	Search           SemanticBestFirstResult `json:"search"`
	Calls            []SemanticExplorerCall  `json:"calls"`
	ModelWork        ModelWork               `json:"model_work"`
	Digest           string                  `json:"digest"`
}

// SemanticExplorerFailure is a sealed terminal artifact for an Explorer run
// that cannot produce a valid search result. Exact provider failure evidence
// remains in the shared call journal; TerminalModelWork charges a dispatched
// call that did not yield a semantic response.
type SemanticExplorerFailure struct {
	SchemaVersion     string                 `json:"schema_version"`
	GuidanceID        string                 `json:"guidance_id"`
	KnowledgeDigest   string                 `json:"knowledge_digest"`
	HypothesisDigest  string                 `json:"hypothesis_digest"`
	Budget            SemanticExplorerBudget `json:"budget"`
	ReasonCode        string                 `json:"reason_code"`
	SearchWork        StatelessDFSWork       `json:"search_work"`
	Calls             []SemanticExplorerCall `json:"calls"`
	TerminalModelWork ModelWork              `json:"terminal_model_work"`
	ModelWork         ModelWork              `json:"model_work"`
	Digest            string                 `json:"digest"`
}

type SemanticExplorerExecutionError struct {
	Failure SemanticExplorerFailure
	cause   error
}

func (failure *SemanticExplorerExecutionError) Error() string {
	if failure == nil || failure.cause == nil {
		return "EXPERIMENT_SEMANTIC_EXPLORER_EXECUTION_FAILED"
	}
	return "EXPERIMENT_SEMANTIC_EXPLORER_EXECUTION_FAILED: " + failure.cause.Error()
}

func (failure *SemanticExplorerExecutionError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

type SemanticExplorerPlanner func(context.Context, SemanticExplorerAgentView) ([]byte, ModelWork, error)

type semanticExplorerGuidance struct {
	id         string
	knowledge  ProtocolKnowledgePack
	hypothesis TestHypothesis
	budget     SemanticExplorerBudget
	planner    SemanticExplorerPlanner
	replay     []SemanticExplorerCall
	replayAt   int
	calls      []SemanticExplorerCall
	work       ModelWork
	pending    *SemanticExplorerCall
	failure    string
	failedWork ModelWork
}

func ExploreBoundedSemanticBestFirstWithExplorer(
	ctx context.Context,
	guidanceID string,
	budget SemanticExplorerBudget,
	knowledge ProtocolKnowledgePack,
	hypothesis TestHypothesis,
	searchSpec StatelessDFSSpec,
	root controlruntime.Trace,
	riskSpec semantic.RiskWitnessSpec,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	planner SemanticExplorerPlanner,
) (SemanticExplorerResult, error) {
	if !validMethodToken(guidanceID) || budget.Validate() != nil || planner == nil ||
		hypothesis.Validate(knowledge, riskSpec, SemanticBestFirstAlgorithmID) != nil {
		return SemanticExplorerResult{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_INPUT_INVALID")
	}
	guidance := &semanticExplorerGuidance{
		id: guidanceID, knowledge: cloneProtocolKnowledge(knowledge), hypothesis: hypothesis,
		budget: budget, planner: planner,
	}
	search, err := ExploreBoundedSemanticBestFirst(
		ctx, searchSpec, root, riskSpec, newAdapter, projector, guidance,
	)
	if err != nil {
		return SemanticExplorerResult{}, newSemanticExplorerExecutionError(
			guidanceID, budget, knowledge.Digest, hypothesis.Digest, guidance, err,
		)
	}
	result := SemanticExplorerResult{
		SchemaVersion: SemanticExplorerResultVersion, GuidanceID: guidanceID,
		KnowledgeDigest: knowledge.Digest, HypothesisDigest: hypothesis.Digest,
		Budget: budget, Search: search, Calls: cloneSemanticExplorerCalls(guidance.calls), ModelWork: guidance.work,
	}
	sealed, err := result.seal()
	if err != nil || sealed.Validate(root, riskSpec) != nil {
		return SemanticExplorerResult{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_RESULT_INVALID")
	}
	return sealed, nil
}

func (guidance *semanticExplorerGuidance) ID() string { return guidance.id }

func (guidance *semanticExplorerGuidance) Order(
	ctx context.Context,
	queue SemanticQueueView,
) ([]string, error) {
	var feedback *SemanticExplorerFeedback
	if guidance.pending != nil {
		value, err := newSemanticExplorerFeedback(*guidance.pending, queue)
		if err != nil {
			return nil, err
		}
		feedback = &value
		guidance.pending = nil
	}
	for {
		if len(guidance.calls) >= guidance.budget.MaxCalls {
			guidance.failure = SemanticExplorerFailureCallBudget
			if semanticExplorerCallsAllRejected(guidance.calls) {
				guidance.failure = SemanticExplorerFailureAllRejected
			}
			return nil, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_BUDGET_EXHAUSTED")
		}
		request, err := newSemanticExplorerRequest(
			fmt.Sprintf("%s-request-%06d", guidance.id, len(guidance.calls)+1),
			len(guidance.calls)+1, guidance.id, guidance.knowledge.Digest,
			guidance.hypothesis.Digest, guidance.budget.MaxCalls-len(guidance.calls),
			guidance.budget.MaxTokens-guidance.work.TotalTokens, queue, feedback,
		)
		if err != nil {
			return nil, err
		}
		call, err := guidance.nextCall(ctx, request)
		if err != nil {
			return nil, err
		}
		guidance.calls = append(guidance.calls, call)
		addModelWork(&guidance.work, call.ModelWork)
		if guidance.work.TotalTokens > guidance.budget.MaxTokens {
			guidance.failure = SemanticExplorerFailureTokenBudget
			return nil, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_TOKEN_BUDGET_EXCEEDED")
		}
		if call.Status == SemanticExplorerCallRejected {
			value, feedbackErr := newSemanticExplorerFeedback(call, queue)
			if feedbackErr != nil {
				return nil, feedbackErr
			}
			feedback = &value
			continue
		}
		guidance.pending = &guidance.calls[len(guidance.calls)-1]
		return append([]string(nil), call.Proposal.OrderedCandidateIDs...), nil
	}
}

func (guidance *semanticExplorerGuidance) nextCall(
	ctx context.Context,
	request SemanticExplorerRequest,
) (SemanticExplorerCall, error) {
	if guidance.replay != nil {
		if guidance.replayAt >= len(guidance.replay) {
			return SemanticExplorerCall{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_REPLAY_CALL_MISSING")
		}
		call := cloneSemanticExplorerCall(guidance.replay[guidance.replayAt])
		guidance.replayAt++
		if !reflect.DeepEqual(call.Request, request) || call.ValidateSource() != nil {
			return SemanticExplorerCall{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_REPLAY_REQUEST_MISMATCH")
		}
		return call, nil
	}
	view := SemanticExplorerAgentView{
		Knowledge: cloneProtocolKnowledge(guidance.knowledge), Hypothesis: guidance.hypothesis,
		Request: cloneSemanticExplorerRequest(request),
	}
	response, work, err := guidance.planner(ctx, view)
	if err != nil {
		guidance.failure = SemanticExplorerFailurePlanner
		if validSemanticExplorerTerminalWork(work) {
			guidance.failedWork = work
		}
		return SemanticExplorerCall{}, fmt.Errorf("EXPERIMENT_SEMANTIC_EXPLORER_PLANNER_FAILED: %w", err)
	}
	if !validStatelessPlannerWork(work) || work.Calls != 1 {
		guidance.failure = SemanticExplorerFailurePlanner
		return SemanticExplorerCall{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_MODEL_WORK_INVALID")
	}
	return newSemanticExplorerCall(request, response, work)
}

func newSemanticExplorerExecutionError(
	guidanceID string,
	budget SemanticExplorerBudget,
	knowledgeDigest string,
	hypothesisDigest string,
	guidance *semanticExplorerGuidance,
	cause error,
) error {
	if guidance == nil || guidance.failure == "" {
		return cause
	}
	var searchWork StatelessDFSWork
	var searchFailure *StatelessDFSExecutionError
	if errors.As(cause, &searchFailure) {
		searchWork = searchFailure.Work
	}
	total := guidance.work
	addModelWork(&total, guidance.failedWork)
	failure := SemanticExplorerFailure{
		SchemaVersion: SemanticExplorerFailureVersion, GuidanceID: guidanceID,
		KnowledgeDigest: knowledgeDigest, HypothesisDigest: hypothesisDigest,
		Budget: budget, ReasonCode: guidance.failure, SearchWork: searchWork,
		Calls:             cloneSemanticExplorerCalls(guidance.calls),
		TerminalModelWork: guidance.failedWork, ModelWork: total,
	}
	sealed, err := failure.seal()
	if err != nil || sealed.Validate() != nil {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FAILURE_INVALID")
	}
	return &SemanticExplorerExecutionError{Failure: sealed, cause: cause}
}

func (failure SemanticExplorerFailure) Validate() error {
	if failure.SchemaVersion != SemanticExplorerFailureVersion || !validMethodToken(failure.GuidanceID) ||
		!validSHA256(failure.KnowledgeDigest) || !validSHA256(failure.HypothesisDigest) ||
		failure.Budget.Validate() != nil || !validSearchWork(failure.SearchWork) ||
		len(failure.Calls) > failure.Budget.MaxCalls ||
		!validSemanticExplorerTerminalWork(failure.TerminalModelWork) {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FAILURE_INVALID")
	}
	var total ModelWork
	allRejected := len(failure.Calls) > 0
	for index, call := range failure.Calls {
		if call.ValidateSource() != nil || call.Request.Ordinal != index+1 ||
			call.Request.GuidanceID != failure.GuidanceID ||
			call.Request.KnowledgeDigest != failure.KnowledgeDigest ||
			call.Request.HypothesisDigest != failure.HypothesisDigest ||
			call.Request.RemainingCalls != failure.Budget.MaxCalls-index ||
			call.Request.RemainingTokens != failure.Budget.MaxTokens-total.TotalTokens {
			return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FAILURE_CALL_INVALID")
		}
		addModelWork(&total, call.ModelWork)
		allRejected = allRejected && call.Status == SemanticExplorerCallRejected
	}
	addModelWork(&total, failure.TerminalModelWork)
	if total != failure.ModelWork || failure.ModelWork.Calls < 0 ||
		failure.ModelWork.TotalTokens != failure.ModelWork.InputTokens+failure.ModelWork.OutputTokens {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FAILURE_WORK_INVALID")
	}
	switch failure.ReasonCode {
	case SemanticExplorerFailurePlanner:
	case SemanticExplorerFailureAllRejected:
		if !allRejected || len(failure.Calls) != failure.Budget.MaxCalls ||
			failure.TerminalModelWork != (ModelWork{}) {
			return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FAILURE_REJECTION_INVALID")
		}
	case SemanticExplorerFailureCallBudget:
		if allRejected || len(failure.Calls) != failure.Budget.MaxCalls ||
			failure.TerminalModelWork != (ModelWork{}) {
			return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FAILURE_CALL_BUDGET_INVALID")
		}
	case SemanticExplorerFailureTokenBudget:
		if failure.ModelWork.TotalTokens <= failure.Budget.MaxTokens ||
			failure.TerminalModelWork != (ModelWork{}) {
			return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FAILURE_TOKEN_BUDGET_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FAILURE_REASON_INVALID")
	}
	want, err := failure.seal()
	if err != nil || !validSHA256(failure.Digest) || want.Digest != failure.Digest {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FAILURE_DIGEST_MISMATCH")
	}
	return nil
}

func validSemanticExplorerTerminalWork(work ModelWork) bool {
	return work.Calls >= 0 && work.Calls <= 1 && work.InputTokens >= 0 && work.OutputTokens >= 0 &&
		work.TotalTokens == work.InputTokens+work.OutputTokens &&
		(work.Calls != 0 || work == (ModelWork{}))
}

func semanticExplorerCallsAllRejected(calls []SemanticExplorerCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if call.Status != SemanticExplorerCallRejected {
			return false
		}
	}
	return true
}

func newSemanticExplorerRequest(
	id string,
	ordinal int,
	guidanceID string,
	knowledgeDigest string,
	hypothesisDigest string,
	remainingCalls int,
	remainingTokens int,
	queue SemanticQueueView,
	feedback *SemanticExplorerFeedback,
) (SemanticExplorerRequest, error) {
	request := SemanticExplorerRequest{
		SchemaVersion: SemanticExplorerRequestVersion, ID: id, Ordinal: ordinal,
		AlgorithmID: SemanticBestFirstAlgorithmID, GuidanceID: guidanceID,
		KnowledgeDigest: knowledgeDigest, HypothesisDigest: hypothesisDigest,
		RemainingCalls: remainingCalls, RemainingTokens: remainingTokens,
		Queue: cloneSemanticQueueView(queue), MutableFields: []string{"ordered_candidate_ids"},
	}
	if feedback != nil {
		value := *feedback
		request.PriorFeedback = &value
	}
	sealed, err := request.seal()
	if err != nil || sealed.Validate() != nil {
		return SemanticExplorerRequest{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_REQUEST_INVALID")
	}
	return sealed, nil
}

func (request SemanticExplorerRequest) Validate() error {
	if request.SchemaVersion != SemanticExplorerRequestVersion || !validMethodToken(request.ID) ||
		request.Ordinal <= 0 || request.AlgorithmID != SemanticBestFirstAlgorithmID ||
		!validMethodToken(request.GuidanceID) || !validSHA256(request.KnowledgeDigest) ||
		!validSHA256(request.HypothesisDigest) || request.RemainingCalls <= 0 || request.RemainingTokens <= 0 ||
		request.Queue.Validate() != nil ||
		!reflect.DeepEqual(request.MutableFields, []string{"ordered_candidate_ids"}) ||
		(request.Ordinal == 1) != (request.PriorFeedback == nil) {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_REQUEST_INVALID")
	}
	if request.PriorFeedback != nil && (request.PriorFeedback.Validate() != nil ||
		request.PriorFeedback.CurrentQueueDigest != request.Queue.Digest) {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_REQUEST_FEEDBACK_INVALID")
	}
	sealed, err := request.seal()
	if err != nil || !validSHA256(request.Digest) || sealed.Digest != request.Digest {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_REQUEST_DIGEST_MISMATCH")
	}
	return nil
}

func ParseSemanticExplorerProposal(data []byte) (SemanticExplorerProposal, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var proposal SemanticExplorerProposal
	if err := decoder.Decode(&proposal); err != nil {
		return SemanticExplorerProposal{}, fmt.Errorf("%w: decode", errSemanticExplorerJSON)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return SemanticExplorerProposal{}, fmt.Errorf("%w: trailing", errSemanticExplorerJSON)
	}
	if proposal.SchemaVersion != SemanticExplorerProposalVersion || proposal.Digest != "" {
		return SemanticExplorerProposal{}, fmt.Errorf("%w: identity", errSemanticExplorerBinding)
	}
	return proposal.seal()
}

func ValidateSemanticExplorerProposal(
	request SemanticExplorerRequest,
	proposal SemanticExplorerProposal,
) ([]string, error) {
	if request.Validate() != nil || proposal.SchemaVersion != SemanticExplorerProposalVersion ||
		proposal.ID != request.ID || proposal.RequestDigest != request.Digest ||
		proposal.QueueDigest != request.Queue.Digest {
		return nil, errSemanticExplorerBinding
	}
	sealed, err := proposal.seal()
	if err != nil || !validSHA256(proposal.Digest) || sealed.Digest != proposal.Digest {
		return nil, errSemanticExplorerBinding
	}
	ordered, err := validateSemanticQueueOrder(request.Queue, proposal.OrderedCandidateIDs)
	if err != nil {
		return nil, errSemanticExplorerCandidate
	}
	return ordered, nil
}

func newSemanticExplorerCall(
	request SemanticExplorerRequest,
	response []byte,
	work ModelWork,
) (SemanticExplorerCall, error) {
	if request.Validate() != nil || !validStatelessPlannerWork(work) || work.Calls != 1 || len(response) == 0 ||
		len(response) > statelessAgentCallMaxBytes {
		return SemanticExplorerCall{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_INPUT_INVALID")
	}
	call := SemanticExplorerCall{
		SchemaVersion: SemanticExplorerCallVersion, Request: request,
		Status: SemanticExplorerCallAccepted, ResponseBytes: append([]byte(nil), response...),
		ResponseDigest: AgentInvocationDigest(response), ModelWork: work,
	}
	proposal, err := ParseSemanticExplorerProposal(response)
	if err == nil {
		_, err = ValidateSemanticExplorerProposal(request, proposal)
	}
	if err != nil {
		call.Status = SemanticExplorerCallRejected
		call.ReasonCode = semanticExplorerProposalReason(err)
		if call.ReasonCode == "" {
			return SemanticExplorerCall{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_REJECTION_UNKNOWN")
		}
	} else {
		call.Proposal = &proposal
	}
	sealed, sealErr := call.seal()
	if sealErr != nil || sealed.validateIdentity() != nil {
		return SemanticExplorerCall{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_INVALID")
	}
	return sealed, nil
}

func (call SemanticExplorerCall) ValidateSource() error {
	if call.validateIdentity() != nil {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_INVALID")
	}
	want, err := newSemanticExplorerCall(call.Request, call.ResponseBytes, call.ModelWork)
	if err != nil || !reflect.DeepEqual(want, call) {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_SOURCE_MISMATCH")
	}
	return nil
}

func (call SemanticExplorerCall) validateIdentity() error {
	if call.SchemaVersion != SemanticExplorerCallVersion || call.Request.Validate() != nil ||
		len(call.ResponseBytes) == 0 || AgentInvocationDigest(call.ResponseBytes) != call.ResponseDigest ||
		!validStatelessPlannerWork(call.ModelWork) || call.ModelWork.Calls != 1 {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_INVALID")
	}
	switch call.Status {
	case SemanticExplorerCallAccepted:
		if call.ReasonCode != "" || call.Proposal == nil {
			return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_ACCEPTED_INVALID")
		}
		if _, err := ValidateSemanticExplorerProposal(call.Request, *call.Proposal); err != nil {
			return err
		}
	case SemanticExplorerCallRejected:
		if !validSemanticExplorerReason(call.ReasonCode) || call.Proposal != nil {
			return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_REJECTED_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_STATUS_INVALID")
	}
	sealed, err := call.seal()
	if err != nil || !validSHA256(call.Digest) || sealed.Digest != call.Digest {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_DIGEST_MISMATCH")
	}
	return nil
}

func newSemanticExplorerFeedback(
	call SemanticExplorerCall,
	current SemanticQueueView,
) (SemanticExplorerFeedback, error) {
	if call.ValidateSource() != nil || current.Validate() != nil {
		return SemanticExplorerFeedback{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FEEDBACK_INPUT_INVALID")
	}
	feedback := SemanticExplorerFeedback{
		SchemaVersion: SemanticExplorerFeedbackVersion, PriorRequestDigest: call.Request.Digest,
		PriorQueueDigest: call.Request.Queue.Digest, ProposalDigest: call.ResponseDigest,
		CurrentQueueDigest: current.Digest,
	}
	if call.Status == SemanticExplorerCallRejected {
		feedback.Outcome = SemanticExplorerFeedbackRejected
		feedback.ReasonCode = call.ReasonCode
		if current.Digest != call.Request.Queue.Digest {
			return SemanticExplorerFeedback{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_REJECTION_QUEUE_CHANGED")
		}
	} else {
		feedback.Outcome = SemanticExplorerFeedbackExpanded
		feedback.SelectedCandidateID = call.Proposal.OrderedCandidateIDs[0]
		if current.Digest == call.Request.Queue.Digest {
			return SemanticExplorerFeedback{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_EXPANSION_QUEUE_UNCHANGED")
		}
	}
	sealed, err := feedback.seal()
	if err != nil || sealed.Validate() != nil {
		return SemanticExplorerFeedback{}, errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FEEDBACK_INVALID")
	}
	return sealed, nil
}

func (feedback SemanticExplorerFeedback) Validate() error {
	if feedback.SchemaVersion != SemanticExplorerFeedbackVersion ||
		!validSHA256(feedback.PriorRequestDigest) || !validSHA256(feedback.PriorQueueDigest) ||
		!validSHA256(feedback.ProposalDigest) || !validSHA256(feedback.CurrentQueueDigest) {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FEEDBACK_INVALID")
	}
	switch feedback.Outcome {
	case SemanticExplorerFeedbackRejected:
		if !validSemanticExplorerReason(feedback.ReasonCode) || feedback.SelectedCandidateID != "" ||
			feedback.PriorQueueDigest != feedback.CurrentQueueDigest {
			return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_REJECTION_FEEDBACK_INVALID")
		}
	case SemanticExplorerFeedbackExpanded:
		if feedback.ReasonCode != "" || !validSemanticCandidateID(feedback.SelectedCandidateID) ||
			feedback.PriorQueueDigest == feedback.CurrentQueueDigest {
			return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_EXPANSION_FEEDBACK_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FEEDBACK_OUTCOME_INVALID")
	}
	sealed, err := feedback.seal()
	if err != nil || !validSHA256(feedback.Digest) || sealed.Digest != feedback.Digest {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_FEEDBACK_DIGEST_MISMATCH")
	}
	return nil
}

func (result SemanticExplorerResult) Validate(root controlruntime.Trace, riskSpec semantic.RiskWitnessSpec) error {
	if result.SchemaVersion != SemanticExplorerResultVersion || !validMethodToken(result.GuidanceID) ||
		!validSHA256(result.KnowledgeDigest) || !validSHA256(result.HypothesisDigest) ||
		result.Budget.Validate() != nil || result.Search.Validate(root, riskSpec) != nil ||
		result.Search.AlgorithmID != SemanticBestFirstAlgorithmID ||
		result.Search.GuidanceID != result.GuidanceID || len(result.Calls) > result.Budget.MaxCalls ||
		!validStatelessAggregateModelWork(result.ModelWork) || result.ModelWork.TotalTokens > result.Budget.MaxTokens {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_RESULT_INVALID")
	}
	var total ModelWork
	accepted := make([]string, 0)
	for index, call := range result.Calls {
		if call.ValidateSource() != nil || call.Request.Ordinal != index+1 ||
			call.Request.GuidanceID != result.GuidanceID || call.Request.KnowledgeDigest != result.KnowledgeDigest ||
			call.Request.HypothesisDigest != result.HypothesisDigest ||
			call.Request.RemainingCalls != result.Budget.MaxCalls-index ||
			call.Request.RemainingTokens != result.Budget.MaxTokens-total.TotalTokens {
			return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_CALL_BINDING_INVALID")
		}
		addModelWork(&total, call.ModelWork)
		if call.Status == SemanticExplorerCallAccepted {
			accepted = append(accepted, call.Proposal.OrderedCandidateIDs[0])
		}
	}
	if total != result.ModelWork || len(accepted) < len(result.Search.ExpansionOrder) ||
		len(accepted) > len(result.Search.ExpansionOrder)+1 ||
		!reflect.DeepEqual(accepted[:len(result.Search.ExpansionOrder)], result.Search.ExpansionOrder) {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_EXPANSION_BINDING_INVALID")
	}
	sealed, err := result.seal()
	if err != nil || !validSHA256(result.Digest) || sealed.Digest != result.Digest {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_RESULT_DIGEST_MISMATCH")
	}
	return nil
}

func (result SemanticExplorerResult) ValidateSources(
	ctx context.Context,
	root controlruntime.Trace,
	riskSpec semantic.RiskWitnessSpec,
	knowledge ProtocolKnowledgePack,
	hypothesis TestHypothesis,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
) error {
	if result.Validate(root, riskSpec) != nil ||
		hypothesis.Validate(knowledge, riskSpec, SemanticBestFirstAlgorithmID) != nil ||
		knowledge.Digest != result.KnowledgeDigest || hypothesis.Digest != result.HypothesisDigest {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_SOURCE_INVALID")
	}
	replay := &semanticExplorerGuidance{
		id: result.GuidanceID, knowledge: cloneProtocolKnowledge(knowledge), hypothesis: hypothesis,
		budget: result.Budget, replay: cloneSemanticExplorerCalls(result.Calls),
	}
	want, err := ExploreBoundedSemanticBestFirst(
		ctx, result.Search.Search.Spec, root, riskSpec, newAdapter, projector, replay,
	)
	if err != nil || replay.replayAt != len(result.Calls) || !reflect.DeepEqual(want, result.Search) ||
		!reflect.DeepEqual(replay.calls, result.Calls) || replay.work != result.ModelWork {
		return errors.New("EXPERIMENT_SEMANTIC_EXPLORER_SOURCE_MISMATCH")
	}
	return nil
}

func semanticExplorerProposalReason(err error) string {
	switch {
	case errors.Is(err, errSemanticExplorerJSON):
		return SemanticExplorerReasonJSON
	case errors.Is(err, errSemanticExplorerCandidate):
		return SemanticExplorerReasonCandidate
	case errors.Is(err, errSemanticExplorerBinding):
		return SemanticExplorerReasonBinding
	default:
		return ""
	}
}

func validSemanticExplorerReason(reason string) bool {
	return reason == SemanticExplorerReasonJSON || reason == SemanticExplorerReasonBinding ||
		reason == SemanticExplorerReasonCandidate
}

func validSearchWork(work StatelessDFSWork) bool {
	return validDFSPhaseWork(work.FrontierReconstruction) &&
		validDFSPhaseWork(work.ChildMaterialization) && validDFSPhaseWork(work.ChildVerification) &&
		work.TotalWorkUnits == work.FrontierReconstruction.WorkUnits+
			work.ChildMaterialization.WorkUnits+work.ChildVerification.WorkUnits
}

func addModelWork(total *ModelWork, current ModelWork) {
	total.Calls += current.Calls
	total.InputTokens += current.InputTokens
	total.OutputTokens += current.OutputTokens
	total.TotalTokens += current.TotalTokens
}

func cloneProtocolKnowledge(value ProtocolKnowledgePack) ProtocolKnowledgePack {
	value.Knowledge = append([]KnowledgeStatement(nil), value.Knowledge...)
	value.Risks = cloneProtocolKnowledgeRisks(value.Risks)
	return value
}

func cloneSemanticQueueView(value SemanticQueueView) SemanticQueueView {
	value.Candidates = cloneSemanticQueueCandidates(value.Candidates)
	return value
}

func cloneSemanticExplorerRequest(value SemanticExplorerRequest) SemanticExplorerRequest {
	value.Queue = cloneSemanticQueueView(value.Queue)
	value.MutableFields = append([]string(nil), value.MutableFields...)
	if value.PriorFeedback != nil {
		feedback := *value.PriorFeedback
		value.PriorFeedback = &feedback
	}
	return value
}

func cloneSemanticExplorerCall(value SemanticExplorerCall) SemanticExplorerCall {
	value.Request = cloneSemanticExplorerRequest(value.Request)
	value.ResponseBytes = append([]byte(nil), value.ResponseBytes...)
	if value.Proposal != nil {
		proposal := *value.Proposal
		proposal.OrderedCandidateIDs = append([]string(nil), value.Proposal.OrderedCandidateIDs...)
		value.Proposal = &proposal
	}
	return value
}

func cloneSemanticExplorerCalls(values []SemanticExplorerCall) []SemanticExplorerCall {
	result := make([]SemanticExplorerCall, len(values))
	for index := range values {
		result[index] = cloneSemanticExplorerCall(values[index])
	}
	return result
}

func (failure SemanticExplorerFailure) seal() (SemanticExplorerFailure, error) {
	failure.Calls = cloneSemanticExplorerCalls(failure.Calls)
	failure.Digest = ""
	digest, err := control.CanonicalDigest(failure)
	failure.Digest = digest
	return failure, err
}

func (feedback SemanticExplorerFeedback) seal() (SemanticExplorerFeedback, error) {
	feedback.Digest = ""
	digest, err := control.CanonicalDigest(feedback)
	feedback.Digest = digest
	return feedback, err
}

func (request SemanticExplorerRequest) seal() (SemanticExplorerRequest, error) {
	request = cloneSemanticExplorerRequest(request)
	request.Digest = ""
	digest, err := control.CanonicalDigest(request)
	request.Digest = digest
	return request, err
}

func (proposal SemanticExplorerProposal) seal() (SemanticExplorerProposal, error) {
	proposal.OrderedCandidateIDs = append([]string(nil), proposal.OrderedCandidateIDs...)
	proposal.Digest = ""
	digest, err := control.CanonicalDigest(proposal)
	proposal.Digest = digest
	return proposal, err
}

func (call SemanticExplorerCall) seal() (SemanticExplorerCall, error) {
	call = cloneSemanticExplorerCall(call)
	call.Digest = ""
	digest, err := control.CanonicalDigest(call)
	call.Digest = digest
	return call, err
}

func (result SemanticExplorerResult) seal() (SemanticExplorerResult, error) {
	result.Calls = cloneSemanticExplorerCalls(result.Calls)
	result.Digest = ""
	digest, err := control.CanonicalDigest(result)
	result.Digest = digest
	return result, err
}

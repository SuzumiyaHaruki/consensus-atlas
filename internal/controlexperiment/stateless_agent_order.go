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
)

const (
	StatelessFrontierOrderRequestVersion  = "consensus-atlas/stateless-frontier-order-request/v1"
	StatelessFrontierOrderProposalVersion = "consensus-atlas/stateless-frontier-order-proposal/v1"
	StatelessFrontierOrderRecordVersion   = "consensus-atlas/stateless-frontier-order-record/v1"
	StatelessAgentTraversalResultVersion  = "consensus-atlas/stateless-agent-traversal-result/v1"
)

// StatelessSearchHistoryEntry contains only completed discovery information.
// It cannot grant Action authority or create trusted evaluation evidence.
type StatelessSearchHistoryEntry struct {
	Ordinal                 int    `json:"ordinal"`
	RootID                  string `json:"root_id"`
	CorpusNovelPSSStates    int    `json:"corpus_novel_pss_states"`
	CorpusNovelPSSSetDigest string `json:"corpus_novel_pss_set_digest"`
	DiscoveryDigest         string `json:"discovery_digest"`
}

type StatelessFrontierOrderRequest struct {
	SchemaVersion    string                        `json:"schema_version"`
	ID               string                        `json:"id"`
	Ordinal          int                           `json:"ordinal"`
	MethodDigest     string                        `json:"method_digest"`
	KnowledgeDigest  string                        `json:"knowledge_digest"`
	Frontier         ActionFrontierView            `json:"frontier"`
	CompletedHistory []StatelessSearchHistoryEntry `json:"completed_history"`
	MutableFields    []string                      `json:"mutable_fields"`
	Digest           string                        `json:"digest"`
}

// StatelessSearchAgentView is untrusted planning input. Knowledge text is
// visible to the planner, but only the request's Action IDs may be returned.
type StatelessSearchAgentView struct {
	Knowledge ProtocolKnowledgePack         `json:"knowledge"`
	Request   StatelessFrontierOrderRequest `json:"request"`
}

type StatelessFrontierOrderProposal struct {
	SchemaVersion string             `json:"schema_version"`
	ID            string             `json:"id"`
	RequestDigest string             `json:"request_digest"`
	ViewDigest    string             `json:"view_digest"`
	ActionIDs     []control.ActionID `json:"action_ids"`
	Digest        string             `json:"digest"`
}

type StatelessFrontierOrderRecord struct {
	SchemaVersion string                         `json:"schema_version"`
	Request       StatelessFrontierOrderRequest  `json:"request"`
	Proposal      StatelessFrontierOrderProposal `json:"proposal"`
	ModelWork     ModelWork                      `json:"model_work"`
	Digest        string                         `json:"digest"`
}

type StatelessAgentTraversalResult struct {
	SchemaVersion      string                         `json:"schema_version"`
	Traversal          StatelessTraversalResult       `json:"traversal"`
	PlannerInvocations int                            `json:"planner_invocations"`
	AcceptedProposals  int                            `json:"accepted_proposals"`
	ModelWork          ModelWork                      `json:"model_work"`
	Records            []StatelessFrontierOrderRecord `json:"records"`
	Digest             string                         `json:"digest"`
}

type StatelessFrontierOrderPlanner func(
	context.Context,
	StatelessSearchAgentView,
) ([]byte, ModelWork, error)

func NewStatelessFrontierOrderRequest(
	id string,
	ordinal int,
	method StatelessTraversalMethod,
	knowledge ProtocolKnowledgePack,
	frontier ActionFrontierView,
	history []StatelessSearchHistoryEntry,
) (StatelessFrontierOrderRequest, error) {
	request := StatelessFrontierOrderRequest{
		SchemaVersion: StatelessFrontierOrderRequestVersion, ID: id, Ordinal: ordinal,
		MethodDigest: method.Digest, KnowledgeDigest: knowledge.Digest, Frontier: frontier,
		CompletedHistory: append([]StatelessSearchHistoryEntry(nil), history...),
		MutableFields:    []string{"action_ids"},
	}
	if method.Validate() != nil || method.Strategy != StatelessTraversalAgentOrder ||
		knowledge.Validate() != nil || method.KnowledgeDigest != knowledge.Digest ||
		request.validateContent() != nil {
		return StatelessFrontierOrderRequest{}, errors.New("EXPERIMENT_STATELESS_AGENT_REQUEST_INPUT_INVALID")
	}
	return request.seal()
}

func (request StatelessFrontierOrderRequest) Validate() error {
	if err := request.validateContent(); err != nil {
		return err
	}
	want, err := request.seal()
	if err != nil || !validSHA256(request.Digest) || want.Digest != request.Digest {
		return errors.New("EXPERIMENT_STATELESS_AGENT_REQUEST_DIGEST_MISMATCH")
	}
	return nil
}

func (request StatelessFrontierOrderRequest) validateContent() error {
	if request.SchemaVersion != StatelessFrontierOrderRequestVersion || !validMethodToken(request.ID) ||
		request.Ordinal <= 0 || !validSHA256(request.MethodDigest) || !validSHA256(request.KnowledgeDigest) ||
		request.Frontier.Validate() != nil || !reflect.DeepEqual(request.MutableFields, []string{"action_ids"}) {
		return errors.New("EXPERIMENT_STATELESS_AGENT_REQUEST_INVALID")
	}
	for index, entry := range request.CompletedHistory {
		if entry.Ordinal != index+1 || !validMethodToken(entry.RootID) ||
			entry.CorpusNovelPSSStates < 0 || !validSHA256(entry.CorpusNovelPSSSetDigest) ||
			!validSHA256(entry.DiscoveryDigest) {
			return errors.New("EXPERIMENT_STATELESS_AGENT_HISTORY_INVALID")
		}
	}
	return nil
}

func NewStatelessFrontierOrderProposal(
	request StatelessFrontierOrderRequest,
	actionIDs []control.ActionID,
) (StatelessFrontierOrderProposal, error) {
	if request.Validate() != nil {
		return StatelessFrontierOrderProposal{}, errors.New("EXPERIMENT_STATELESS_AGENT_PROPOSAL_REQUEST_INVALID")
	}
	proposal := StatelessFrontierOrderProposal{
		SchemaVersion: StatelessFrontierOrderProposalVersion, ID: request.ID,
		RequestDigest: request.Digest, ViewDigest: request.Frontier.Digest,
		ActionIDs: append([]control.ActionID(nil), actionIDs...),
	}
	sealed, err := proposal.seal()
	if err != nil {
		return StatelessFrontierOrderProposal{}, err
	}
	if _, err := ValidateStatelessFrontierOrderProposal(request, sealed); err != nil {
		return StatelessFrontierOrderProposal{}, err
	}
	return sealed, nil
}

// ParseStatelessFrontierOrderProposal is the strict model boundary. The model
// supplies typed fields with an empty digest; trusted code seals the proposal.
func ParseStatelessFrontierOrderProposal(data []byte) (StatelessFrontierOrderProposal, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var proposal StatelessFrontierOrderProposal
	if err := decoder.Decode(&proposal); err != nil {
		return StatelessFrontierOrderProposal{}, errors.New("EXPERIMENT_STATELESS_AGENT_PROPOSAL_JSON_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return StatelessFrontierOrderProposal{}, errors.New("EXPERIMENT_STATELESS_AGENT_PROPOSAL_JSON_TRAILING")
	}
	if proposal.SchemaVersion != StatelessFrontierOrderProposalVersion || proposal.Digest != "" {
		return StatelessFrontierOrderProposal{}, errors.New("EXPERIMENT_STATELESS_AGENT_PROPOSAL_IDENTITY_INVALID")
	}
	return proposal.seal()
}

func ValidateStatelessFrontierOrderProposal(
	request StatelessFrontierOrderRequest,
	proposal StatelessFrontierOrderProposal,
) ([]FrontierActionRef, error) {
	if request.Validate() != nil || proposal.SchemaVersion != StatelessFrontierOrderProposalVersion ||
		proposal.ID != request.ID || proposal.RequestDigest != request.Digest ||
		proposal.ViewDigest != request.Frontier.Digest || len(proposal.ActionIDs) != len(request.Frontier.Actions) {
		return nil, errors.New("EXPERIMENT_STATELESS_AGENT_PROPOSAL_BINDING_INVALID")
	}
	want, err := proposal.seal()
	if err != nil || !validSHA256(proposal.Digest) || want.Digest != proposal.Digest {
		return nil, errors.New("EXPERIMENT_STATELESS_AGENT_PROPOSAL_DIGEST_MISMATCH")
	}
	byID := make(map[control.ActionID]FrontierActionRef, len(request.Frontier.Actions))
	for _, action := range request.Frontier.Actions {
		byID[action.ActionID] = action
	}
	ordered := make([]FrontierActionRef, 0, len(proposal.ActionIDs))
	seen := make(map[control.ActionID]bool, len(proposal.ActionIDs))
	for _, actionID := range proposal.ActionIDs {
		action, ok := byID[actionID]
		if !ok || seen[actionID] {
			return nil, errors.New("EXPERIMENT_STATELESS_AGENT_ACTION_SET_INVALID")
		}
		seen[actionID] = true
		ordered = append(ordered, action)
	}
	return ordered, nil
}

func ExploreBoundedStatelessDFSWithAgent(
	ctx context.Context,
	method StatelessTraversalMethod,
	knowledge ProtocolKnowledgePack,
	history []StatelessSearchHistoryEntry,
	planner StatelessFrontierOrderPlanner,
	spec StatelessDFSSpec,
	root controlruntime.Trace,
	newAdapter AdapterFactory,
) (StatelessAgentTraversalResult, error) {
	if method.Validate() != nil || method.Strategy != StatelessTraversalAgentOrder ||
		knowledge.Validate() != nil || method.KnowledgeDigest != knowledge.Digest || planner == nil {
		return StatelessAgentTraversalResult{}, errors.New("EXPERIMENT_STATELESS_AGENT_TRAVERSAL_INPUT_INVALID")
	}
	var records []StatelessFrontierOrderRecord
	var modelWork ModelWork
	orderer := func(frontier ActionFrontierView) ([]FrontierActionRef, error) {
		ordinal := len(records) + 1
		request, err := NewStatelessFrontierOrderRequest(
			fmt.Sprintf("%s-request-%d", method.ID, ordinal), ordinal,
			method, knowledge, frontier, history,
		)
		if err != nil {
			return nil, err
		}
		response, work, err := planner(ctx, StatelessSearchAgentView{Knowledge: knowledge, Request: request})
		if err != nil || !validStatelessPlannerWork(work) {
			if err != nil {
				return nil, fmt.Errorf("EXPERIMENT_STATELESS_AGENT_PLANNER_FAILED: %w", err)
			}
			return nil, errors.New("EXPERIMENT_STATELESS_AGENT_PLANNER_WORK_INVALID")
		}
		proposal, err := ParseStatelessFrontierOrderProposal(response)
		if err != nil {
			return nil, err
		}
		ordered, err := ValidateStatelessFrontierOrderProposal(request, proposal)
		if err != nil {
			return nil, err
		}
		record, err := (StatelessFrontierOrderRecord{
			SchemaVersion: StatelessFrontierOrderRecordVersion,
			Request:       request, Proposal: proposal, ModelWork: work,
		}).seal()
		if err != nil {
			return nil, err
		}
		records = append(records, record)
		modelWork.Calls += work.Calls
		modelWork.InputTokens += work.InputTokens
		modelWork.OutputTokens += work.OutputTokens
		modelWork.TotalTokens += work.TotalTokens
		return ordered, nil
	}
	search, err := exploreBoundedStatelessDFS(ctx, spec, root, newAdapter, orderer)
	if err != nil {
		return StatelessAgentTraversalResult{}, err
	}
	traversal, err := (StatelessTraversalResult{
		SchemaVersion: StatelessTraversalResultVersion, Method: method, Search: search,
	}).seal()
	if err != nil || traversal.Validate(root) != nil {
		return StatelessAgentTraversalResult{}, errors.New("EXPERIMENT_STATELESS_AGENT_TRAVERSAL_INVALID")
	}
	result := StatelessAgentTraversalResult{
		SchemaVersion: StatelessAgentTraversalResultVersion, Traversal: traversal,
		PlannerInvocations: len(records), AcceptedProposals: len(records),
		ModelWork: modelWork, Records: records,
	}
	sealed, err := result.seal()
	if err != nil {
		return StatelessAgentTraversalResult{}, err
	}
	if err := sealed.Validate(root, knowledge); err != nil {
		return StatelessAgentTraversalResult{}, err
	}
	return sealed, nil
}

func (result StatelessAgentTraversalResult) Validate(
	root controlruntime.Trace,
	knowledge ProtocolKnowledgePack,
) error {
	if result.SchemaVersion != StatelessAgentTraversalResultVersion || knowledge.Validate() != nil ||
		result.Traversal.Validate(root) != nil ||
		result.Traversal.Method.Strategy != StatelessTraversalAgentOrder ||
		result.Traversal.Method.KnowledgeDigest != knowledge.Digest ||
		result.PlannerInvocations != len(result.Records) ||
		result.AcceptedProposals != len(result.Records) || !validStatelessAggregateModelWork(result.ModelWork) {
		return errors.New("EXPERIMENT_STATELESS_AGENT_RESULT_INVALID")
	}
	var total ModelWork
	frontiers := make(map[string]bool, len(result.Records))
	for index, record := range result.Records {
		if record.Validate() != nil || record.Request.Ordinal != index+1 ||
			record.Request.MethodDigest != result.Traversal.Method.Digest ||
			record.Request.KnowledgeDigest != knowledge.Digest {
			return errors.New("EXPERIMENT_STATELESS_AGENT_RECORD_INVALID")
		}
		if _, err := ValidateStatelessFrontierOrderProposal(record.Request, record.Proposal); err != nil {
			return err
		}
		if frontiers[record.Request.Frontier.Digest] {
			return errors.New("EXPERIMENT_STATELESS_AGENT_FRONTIER_DUPLICATE")
		}
		frontiers[record.Request.Frontier.Digest] = true
		var selected []control.ActionID
		for _, item := range result.Traversal.Search.Items {
			if item.FrontierDigest == record.Request.Frontier.Digest {
				selected = append(selected, item.Action.ActionID)
			}
		}
		if len(selected) > len(record.Proposal.ActionIDs) ||
			!reflect.DeepEqual(selected, record.Proposal.ActionIDs[:len(selected)]) {
			return errors.New("EXPERIMENT_STATELESS_AGENT_ORDER_NOT_APPLIED")
		}
		total.Calls += record.ModelWork.Calls
		total.InputTokens += record.ModelWork.InputTokens
		total.OutputTokens += record.ModelWork.OutputTokens
		total.TotalTokens += record.ModelWork.TotalTokens
	}
	for _, item := range result.Traversal.Search.Items {
		if !frontiers[item.FrontierDigest] {
			return errors.New("EXPERIMENT_STATELESS_AGENT_FRONTIER_UNBOUND")
		}
	}
	if total != result.ModelWork {
		return errors.New("EXPERIMENT_STATELESS_AGENT_MODEL_WORK_MISMATCH")
	}
	want, err := result.seal()
	if err != nil || !validSHA256(result.Digest) || want.Digest != result.Digest {
		return errors.New("EXPERIMENT_STATELESS_AGENT_RESULT_DIGEST_MISMATCH")
	}
	return nil
}

func validStatelessPlannerWork(work ModelWork) bool {
	return (work.Calls == 0 && work.InputTokens == 0 && work.OutputTokens == 0 && work.TotalTokens == 0) ||
		(work.Calls == 1 && work.InputTokens >= 0 && work.OutputTokens >= 0 &&
			work.TotalTokens > 0 && work.TotalTokens == work.InputTokens+work.OutputTokens)
}

func validStatelessAggregateModelWork(work ModelWork) bool {
	return work.Calls >= 0 && work.InputTokens >= 0 && work.OutputTokens >= 0 &&
		work.TotalTokens == work.InputTokens+work.OutputTokens &&
		((work.Calls == 0 && work.TotalTokens == 0) || (work.Calls > 0 && work.TotalTokens > 0))
}

func (request StatelessFrontierOrderRequest) seal() (StatelessFrontierOrderRequest, error) {
	request.CompletedHistory = append([]StatelessSearchHistoryEntry(nil), request.CompletedHistory...)
	request.MutableFields = append([]string(nil), request.MutableFields...)
	request.Digest = ""
	digest, err := control.CanonicalDigest(request)
	if err != nil {
		return StatelessFrontierOrderRequest{}, err
	}
	request.Digest = digest
	return request, nil
}

func (proposal StatelessFrontierOrderProposal) seal() (StatelessFrontierOrderProposal, error) {
	proposal.ActionIDs = append([]control.ActionID(nil), proposal.ActionIDs...)
	proposal.Digest = ""
	digest, err := control.CanonicalDigest(proposal)
	if err != nil {
		return StatelessFrontierOrderProposal{}, err
	}
	proposal.Digest = digest
	return proposal, nil
}

func (record StatelessFrontierOrderRecord) Validate() error {
	if record.SchemaVersion != StatelessFrontierOrderRecordVersion || record.Request.Validate() != nil ||
		!validStatelessPlannerWork(record.ModelWork) {
		return errors.New("EXPERIMENT_STATELESS_AGENT_RECORD_INVALID")
	}
	want, err := record.seal()
	if err != nil || !validSHA256(record.Digest) || want.Digest != record.Digest {
		return errors.New("EXPERIMENT_STATELESS_AGENT_RECORD_DIGEST_MISMATCH")
	}
	return nil
}

func (record StatelessFrontierOrderRecord) seal() (StatelessFrontierOrderRecord, error) {
	record.Digest = ""
	digest, err := control.CanonicalDigest(record)
	if err != nil {
		return StatelessFrontierOrderRecord{}, err
	}
	record.Digest = digest
	return record, nil
}

func (result StatelessAgentTraversalResult) seal() (StatelessAgentTraversalResult, error) {
	result.Records = append([]StatelessFrontierOrderRecord(nil), result.Records...)
	result.Digest = ""
	digest, err := control.CanonicalDigest(result)
	if err != nil {
		return StatelessAgentTraversalResult{}, err
	}
	result.Digest = digest
	return result, nil
}

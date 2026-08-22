package controlexperiment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	RiskAgentMaxCalls               = 4
	RiskCandidateMaxBytes           = 64 << 10
	RiskCandidateMaxSteps           = 6
	RiskCandidatePortfolioMax       = 3
	RiskExplorationMemoryMax        = 8
	RiskMechanismStepMaxBytes       = 256
	RiskMechanismSupportMax         = 3
	riskSupportReferenceMaxBytes    = targetEvidenceReferenceMaxBytes + len("source/")
	RiskKnowledgeRequestsPerCall    = 1
	RiskKnowledgeRequestsRequired   = 2
	RiskKnowledgeRequestRetryMax    = 1
	RiskKnowledgeRequestMaxAttempts = RiskKnowledgeRequestsRequired + RiskKnowledgeRequestRetryMax
	RiskKnowledgeRequestMaxLines    = 80

	RiskMemoryOutcomeExecutionCompleted  = "execution-completed"
	RiskMemoryOutcomeBudgetExhausted     = "budget-exhausted"
	RiskMemoryOutcomeWitnessNearMiss     = "witness-near-miss"
	RiskMemoryOutcomePlanningStopped     = "planning-stopped"
	RiskMemoryOutcomeExecutionFailed     = "execution-failed"
	RiskMemoryOutcomeHypothesisAbandoned = "hypothesis-abandoned"

	RiskAgentAccepted = "accepted"
	RiskAgentStopped  = "stopped"

	RiskAgentReasonJSON                 = "risk-candidate-json-invalid"
	RiskAgentReasonCandidate            = "risk-candidate-invalid"
	RiskAgentReasonBinding              = "risk-candidate-single-use-binding"
	RiskAgentReasonBindingDomain        = "risk-candidate-binding-domain-mismatch"
	RiskAgentReasonProperty             = "risk-candidate-property-unknown"
	RiskAgentReasonInspiration          = "risk-candidate-inspiration-unknown"
	RiskAgentReasonAlignment            = "risk-candidate-mechanism-unaligned"
	RiskAgentReasonSupport              = "risk-candidate-support-invisible"
	RiskAgentReasonDuplicate            = "risk-candidate-existing-risk"
	RiskAgentReasonNotExecutable        = "risk-candidate-not-executable"
	RiskAgentReasonTokenBudget          = "risk-agent-token-budget-exceeded"
	RiskAgentReasonPortfolio            = "risk-portfolio-no-executable-candidate"
	RiskAgentReasonKnowledgeRead        = "risk-knowledge-read-completed"
	RiskAgentReasonKnowledgeReadStopped = "risk-knowledge-read-stopped"
	RiskAgentReasonKnowledgeInvalid     = "risk-knowledge-request-invalid"
	RiskAgentReasonKnowledgeUnavailable = "risk-knowledge-reader-unavailable"
	RiskAgentReasonKnowledgeBudget      = "risk-knowledge-request-budget-exceeded"
	RiskAgentReasonKnowledgeGrounding   = "risk-knowledge-grounding-required"
	RiskAgentReasonResponseFinishLength = "response-finish-length"
	RiskAgentReasonFidelity             = AgentCapabilityGapTargetFidelity

	RiskCandidateExecutable = "executable"
)

func addModelWork(total *ModelWork, current ModelWork) {
	total.Calls += current.Calls
	total.InputTokens += current.InputTokens
	total.OutputTokens += current.OutputTokens
	total.TotalTokens += current.TotalTokens
}

var (
	errRiskCandidateJSON          = errors.New("EXPERIMENT_RISK_CANDIDATE_JSON_INVALID")
	errRiskCandidateDuplicate     = errors.New("EXPERIMENT_RISK_CANDIDATE_EXISTING_RISK")
	errRiskCandidateBinding       = errors.New("EXPERIMENT_RISK_CANDIDATE_BINDING_INVALID")
	errRiskCandidateBindingDomain = errors.New("EXPERIMENT_RISK_CANDIDATE_BINDING_DOMAIN_MISMATCH")
	errRiskCandidateProperty      = errors.New("EXPERIMENT_RISK_CANDIDATE_PROPERTY_UNKNOWN")
	errRiskCandidatePattern       = errors.New("EXPERIMENT_RISK_CANDIDATE_INSPIRATION_UNKNOWN")
	errRiskCandidateAlignment     = errors.New("EXPERIMENT_RISK_CANDIDATE_MECHANISM_UNALIGNED")
	errRiskCandidateSupport       = errors.New("EXPERIMENT_RISK_CANDIDATE_SUPPORT_INVALID")
	errRiskCandidateBridge        = errors.New("EXPERIMENT_RISK_CANDIDATE_SCENARIO_BRIDGE_INVALID")
)

// RiskMechanismStep binds one causal explanation to the exact Observation
// milestone that makes it testable. Provider prose is never accepted as a
// second, independent description of the trigger sequence.
type RiskMechanismStep struct {
	MilestoneID string                   `json:"milestone_id"`
	Kind        semantic.ObservationKind `json:"kind"`
	Rationale   string                   `json:"rationale"`
	SupportRefs []string                 `json:"support_refs,omitempty"`
}

// RiskCandidate is deliberately only a semantic hypothesis. Family, order
// edges, capabilities, execution controls and verdicts remain trusted inputs.
type RiskCandidate struct {
	ID                 string                          `json:"id"`
	PropertyRef        string                          `json:"property_ref,omitempty"`
	InspirationRef     string                          `json:"inspiration_ref,omitempty"`
	Summary            string                          `json:"summary"`
	SuspectedMechanism string                          `json:"suspected_mechanism,omitempty"`
	MechanismSteps     []RiskMechanismStep             `json:"mechanism_steps,omitempty"`
	RequiredFidelity   []string                        `json:"required_fidelity,omitempty"`
	Predicates         []semantic.ObservationPredicate `json:"predicates"`
}

// RiskCandidatePortfolio is one untrusted model proposal. Candidate order is
// the Agent's priority; trusted code assesses every entry and retains every
// mechanically executable candidate. Accepted remains the first entry for
// single-Episode compatibility; an Investigation can consume the remainder.
type RiskCandidatePortfolio struct {
	Candidates                  []RiskCandidate `json:"candidates"`
	requiresBoundMechanismSteps bool
}

// RiskCandidateSemanticIdentity returns a canonical structural value rather
// than trusting the Agent-authored Candidate ID. Explanatory prose and source
// citations are intentionally excluded: the executable predicates, property,
// fidelity requirements and causal milestone kinds define semantic reuse.
func RiskCandidateSemanticIdentity(candidate RiskCandidate) (string, error) {
	if candidate.Validate() != nil {
		return "", errors.New("EXPERIMENT_RISK_CANDIDATE_SEMANTIC_IDENTITY_INVALID")
	}
	type mechanismStep struct {
		MilestoneID string                   `json:"milestone_id"`
		Kind        semantic.ObservationKind `json:"kind"`
	}
	projection := struct {
		PropertyRef      string                          `json:"property_ref"`
		RequiredFidelity []string                        `json:"required_fidelity,omitempty"`
		Predicates       []semantic.ObservationPredicate `json:"predicates"`
		MechanismSteps   []mechanismStep                 `json:"mechanism_steps,omitempty"`
	}{
		PropertyRef:      candidate.PropertyRef,
		RequiredFidelity: append([]string(nil), candidate.RequiredFidelity...),
		Predicates:       append([]semantic.ObservationPredicate(nil), candidate.Predicates...),
		MechanismSteps:   make([]mechanismStep, len(candidate.MechanismSteps)),
	}
	for index, step := range candidate.MechanismSteps {
		projection.MechanismSteps[index] = mechanismStep{
			MilestoneID: step.MilestoneID, Kind: step.Kind,
		}
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

type RiskKnowledgeRequestBatch struct {
	KnowledgeRequests []KnowledgeReadRequest `json:"knowledge_requests"`
}

const (
	riskAgentResponsePortfolio      = "portfolio"
	riskAgentResponseKnowledgeQuery = "knowledge-query"
)

// riskAgentResponseEnvelope lets the Agent decide whether the supplied
// materials are already sufficient or another bounded source read is useful.
// Trusted code still validates the selected branch and every reference.
type riskAgentResponseEnvelope struct {
	ResponseKind      string                 `json:"response_kind"`
	Candidates        []RiskCandidate        `json:"candidates"`
	KnowledgeRequests []KnowledgeReadRequest `json:"knowledge_requests"`
}

type RiskAgentBudget struct {
	MaxCalls  int `json:"max_calls"`
	MaxTokens int `json:"max_tokens"`
}

// RiskPlannerResponseFailure preserves charged provider work while allowing
// one bounded, typed repair attempt. It never carries partial provider output.
type RiskPlannerResponseFailure struct {
	Code       string
	Repairable bool
}

func (failure *RiskPlannerResponseFailure) Error() string {
	if failure == nil || failure.Code == "" {
		return "EXPERIMENT_RISK_PLANNER_RESPONSE_FAILED"
	}
	return failure.Code
}

type RiskAgentFeedback struct {
	Outcome         string                            `json:"outcome"`
	ReasonCode      string                            `json:"reason_code,omitempty"`
	CandidateID     string                            `json:"candidate_id,omitempty"`
	Issues          []semantic.RiskQualificationIssue `json:"issues,omitempty"`
	CapabilityGaps  []AgentCapabilityGap              `json:"capability_gaps,omitempty"`
	FidelityNotices []AgentCapabilityGap              `json:"fidelity_notices,omitempty"`
	Reviews         []RiskCandidateReview             `json:"candidate_reviews,omitempty"`
}

type RiskCandidateReview struct {
	Ordinal         int                               `json:"ordinal"`
	CandidateID     string                            `json:"candidate_id,omitempty"`
	Outcome         string                            `json:"outcome"`
	ReasonCode      string                            `json:"reason_code,omitempty"`
	Issues          []semantic.RiskQualificationIssue `json:"issues,omitempty"`
	CapabilityGaps  []AgentCapabilityGap              `json:"capability_gaps,omitempty"`
	FidelityNotices []AgentCapabilityGap              `json:"fidelity_notices,omitempty"`
}

// RiskExplorationMemoryEntry is a compact, trusted summary of one earlier
// episode. Exact execution and all Oracle-derived evidence stay in the
// original artifacts; this view only helps the Agent avoid repeating an
// investigation through mechanical execution, Risk and cost feedback.
type RiskExplorationMemoryEntry struct {
	Episode               int                  `json:"episode"`
	CandidateID           string               `json:"candidate_id,omitempty"`
	PropertyRef           string               `json:"property_ref,omitempty"`
	EvidenceLevel         string               `json:"evidence_level,omitempty"`
	Summary               string               `json:"summary,omitempty"`
	SuspectedMechanism    string               `json:"suspected_mechanism,omitempty"`
	EpisodeOutcome        string               `json:"episode_outcome"`
	RepeatedCandidate     bool                 `json:"repeated_candidate,omitempty"`
	RiskStatus            string               `json:"risk_status,omitempty"`
	SatisfiedMilestones   []string             `json:"satisfied_milestones,omitempty"`
	FirstMissingMilestone string               `json:"first_missing_milestone,omitempty"`
	MechanicalReasonCodes []string             `json:"mechanical_reason_codes,omitempty"`
	CapabilityGaps        []AgentCapabilityGap `json:"capability_gaps,omitempty"`
	ModelCalls            int                  `json:"model_calls"`
	ModelTokens           int                  `json:"model_tokens"`
	SearchWorkUnits       int                  `json:"search_work_units"`
	ExecutionWorkUnits    int                  `json:"execution_work_units"`
}

type RiskAgentView struct {
	Knowledge               ProtocolKnowledgePack               `json:"knowledge"`
	TargetSurface           *AgentTargetSurface                 `json:"target_surface,omitempty"`
	ObservationCapabilities []semantic.ObservationCapability    `json:"observation_capabilities"`
	BindingDomains          []semantic.ObservationBindingDomain `json:"binding_domains,omitempty"`
	Actions                 []control.ActionKind                `json:"actions"`
	MaxMilestones           int                                 `json:"max_milestones"`
	MaxCandidates           int                                 `json:"max_candidates"`
	ExplorationMemory       []RiskExplorationMemoryEntry        `json:"exploration_memory,omitempty"`
	KnowledgeSources        []KnowledgeSource                   `json:"knowledge_sources,omitempty"`
	KnowledgeResults        []KnowledgeReadResult               `json:"knowledge_results,omitempty"`
	AvailableSupportRefs    []string                            `json:"available_support_refs"`
	MaxKnowledgeRequests    int                                 `json:"max_knowledge_requests,omitempty"`
	Prior                   *RiskAgentFeedback                  `json:"prior_feedback,omitempty"`
}

type RiskCandidateAssessment struct {
	Candidate          RiskCandidate              `json:"candidate"`
	Spec               semantic.RiskWitnessSpec   `json:"spec"`
	Qualification      semantic.RiskQualification `json:"qualification"`
	CapabilityGaps     []AgentCapabilityGap       `json:"capability_gaps,omitempty"`
	FidelityAssessment string                     `json:"fidelity_assessment,omitempty"`
	FidelityNotices    []AgentCapabilityGap       `json:"fidelity_notices,omitempty"`
}

type RiskAgentAttempt struct {
	Ordinal           int                      `json:"ordinal"`
	ResponseBytes     []byte                   `json:"response_bytes"`
	Assessment        *RiskCandidateAssessment `json:"assessment,omitempty"`
	Feedback          RiskAgentFeedback        `json:"feedback"`
	ModelWork         ModelWork                `json:"model_work"`
	KnowledgeRequests []KnowledgeReadRequest   `json:"knowledge_requests,omitempty"`
	KnowledgeResults  []KnowledgeReadResult    `json:"knowledge_results,omitempty"`
}

type RiskAgentResult struct {
	Status     string                    `json:"status"`
	Attempts   []RiskAgentAttempt        `json:"attempts"`
	Accepted   *RiskCandidateAssessment  `json:"accepted,omitempty"`
	Executable []RiskCandidateAssessment `json:"executable_candidates,omitempty"`
	ModelWork  ModelWork                 `json:"model_work"`
}

const (
	RiskSourceGroundingNotRequested    = "not-requested"
	RiskSourceGroundingIncomplete      = "incomplete"
	RiskSourceGroundingCompletedUnused = "completed-unused"
	RiskSourceGroundingCompletedUsed   = "completed-used"
)

// RiskSourceGroundingUsage reports whether a completed bounded source read
// was actually cited by any candidate returned in the same Risk Agent run. It
// is descriptive evidence only: an unused read does not reject a candidate,
// and a cited read does not establish a defect.
type RiskSourceGroundingUsage struct {
	Status         string   `json:"status"`
	ReadReferences []string `json:"read_references,omitempty"`
	UsedReferences []string `json:"used_references,omitempty"`
}

func (usage RiskSourceGroundingUsage) Validate() error {
	validStatus := usage.Status == RiskSourceGroundingNotRequested ||
		usage.Status == RiskSourceGroundingIncomplete ||
		usage.Status == RiskSourceGroundingCompletedUnused ||
		usage.Status == RiskSourceGroundingCompletedUsed
	unique := func(values []string) bool {
		for index := 1; index < len(values); index++ {
			if values[index] == values[index-1] {
				return false
			}
		}
		return true
	}
	if !validStatus || !sort.StringsAreSorted(usage.ReadReferences) ||
		!sort.StringsAreSorted(usage.UsedReferences) ||
		!unique(usage.ReadReferences) || !unique(usage.UsedReferences) {
		return errors.New("EXPERIMENT_RISK_SOURCE_GROUNDING_USAGE_INVALID")
	}
	read := make(map[string]bool, len(usage.ReadReferences))
	for _, reference := range usage.ReadReferences {
		if !strings.HasPrefix(reference, "source/") {
			return errors.New("EXPERIMENT_RISK_SOURCE_GROUNDING_REFERENCE_INVALID")
		}
		read[reference] = true
	}
	for _, reference := range usage.UsedReferences {
		if !read[reference] {
			return errors.New("EXPERIMENT_RISK_SOURCE_GROUNDING_USE_INVALID")
		}
	}
	switch usage.Status {
	case RiskSourceGroundingNotRequested, RiskSourceGroundingIncomplete:
		if len(usage.ReadReferences) != 0 || len(usage.UsedReferences) != 0 {
			return errors.New("EXPERIMENT_RISK_SOURCE_GROUNDING_STATUS_INVALID")
		}
	case RiskSourceGroundingCompletedUnused:
		if len(usage.ReadReferences) == 0 || len(usage.UsedReferences) != 0 {
			return errors.New("EXPERIMENT_RISK_SOURCE_GROUNDING_STATUS_INVALID")
		}
	case RiskSourceGroundingCompletedUsed:
		if len(usage.ReadReferences) == 0 || len(usage.UsedReferences) == 0 {
			return errors.New("EXPERIMENT_RISK_SOURCE_GROUNDING_STATUS_INVALID")
		}
	}
	return nil
}

func AnalyzeRiskSourceGrounding(result RiskAgentResult) RiskSourceGroundingUsage {
	read := make(map[string]bool)
	used := make(map[string]bool)
	requested := false
	for _, attempt := range result.Attempts {
		requested = requested || len(attempt.KnowledgeRequests) > 0
		for _, knowledge := range attempt.KnowledgeResults {
			if knowledge.Status == KnowledgeDiscoveryCompleted && knowledge.Query == "" &&
				knowledge.Source.Reference != "" {
				read["source/"+knowledge.Source.Reference] = true
			}
		}
		portfolio, _, isKnowledge, err := parseRiskAgentResponse(attempt.ResponseBytes)
		if err != nil || isKnowledge {
			continue
		}
		for _, candidate := range portfolio.Candidates {
			for _, step := range candidate.MechanismSteps {
				for _, reference := range step.SupportRefs {
					if read[reference] {
						used[reference] = true
					}
				}
			}
		}
	}
	usage := RiskSourceGroundingUsage{Status: RiskSourceGroundingNotRequested}
	if requested {
		usage.Status = RiskSourceGroundingIncomplete
	}
	for reference := range read {
		usage.ReadReferences = append(usage.ReadReferences, reference)
	}
	for reference := range used {
		usage.UsedReferences = append(usage.UsedReferences, reference)
	}
	sort.Strings(usage.ReadReferences)
	sort.Strings(usage.UsedReferences)
	if len(usage.ReadReferences) > 0 {
		usage.Status = RiskSourceGroundingCompletedUnused
		if len(usage.UsedReferences) > 0 {
			usage.Status = RiskSourceGroundingCompletedUsed
		}
	}
	return usage
}

type RiskPlanner func(context.Context, RiskAgentView) ([]byte, ModelWork, error)
type RiskKnowledgeReader func(KnowledgeReadRequest) (KnowledgeReadResult, error)

func (budget RiskAgentBudget) Validate() error {
	if budget.MaxCalls <= 0 || budget.MaxCalls > RiskAgentMaxCalls || budget.MaxTokens <= 0 {
		return errors.New("EXPERIMENT_RISK_AGENT_BUDGET_INVALID")
	}
	return nil
}

func (view RiskAgentView) Validate() error {
	bindingDomains, bindingErr := semantic.ObservationBindingDomains(view.ObservationCapabilities)
	if view.Knowledge.ValidateAgentMaterials() != nil ||
		view.TargetSurface != nil && view.TargetSurface.Validate() != nil ||
		semantic.ValidateObservationCapabilities(view.ObservationCapabilities) != nil ||
		bindingErr != nil || !reflect.DeepEqual(view.BindingDomains, bindingDomains) ||
		!validRiskAgentActions(view.Actions) || view.MaxMilestones != RiskCandidateMaxSteps ||
		view.MaxCandidates != RiskCandidatePortfolioMax ||
		!validRiskExplorationMemory(view.ExplorationMemory) ||
		!validRiskKnowledgeView(view) || !validRiskSupportView(view) {
		return errors.New("EXPERIMENT_RISK_AGENT_VIEW_INVALID")
	}
	if view.Prior != nil && (view.Prior.Outcome != RiskAgentStopped || view.Prior.ReasonCode == "") {
		return errors.New("EXPERIMENT_RISK_AGENT_FEEDBACK_INVALID")
	}
	return nil
}

func DiscoverRiskWithPlanner(
	ctx context.Context,
	budget RiskAgentBudget,
	knowledge ProtocolKnowledgePack,
	capabilities []semantic.ObservationCapability,
	actions []control.ActionKind,
	memory []RiskExplorationMemoryEntry,
	targetSurface *AgentTargetSurface,
	knowledgeReader RiskKnowledgeReader,
	planner RiskPlanner,
) (RiskAgentResult, error) {
	if budget.Validate() != nil ||
		knowledge.ValidateAgentMaterials() != nil || semantic.ValidateObservationCapabilities(capabilities) != nil ||
		!validRiskAgentActions(actions) || !validRiskExplorationMemory(memory) ||
		targetSurface != nil && targetSurface.Validate() != nil || planner == nil {
		return RiskAgentResult{}, errors.New("EXPERIMENT_RISK_AGENT_INPUT_INVALID")
	}
	var knowledgeSources []KnowledgeSource
	if knowledgeReader != nil {
		var err error
		knowledgeSources, err = KnowledgeSourceCatalog(knowledge)
		if err != nil || len(knowledgeSources) == 0 {
			return RiskAgentResult{}, errors.New("EXPERIMENT_RISK_AGENT_KNOWLEDGE_INPUT_INVALID")
		}
	}
	bindingDomains, err := semantic.ObservationBindingDomains(capabilities)
	if err != nil {
		return RiskAgentResult{}, errors.New("EXPERIMENT_RISK_AGENT_BINDING_INPUT_INVALID")
	}
	result := RiskAgentResult{Status: RiskAgentStopped}
	var prior *RiskAgentFeedback
	var knowledgeResults []KnowledgeReadResult
	knowledgeRequests := 0
	stoppedKnowledgeRequests := 0
	completedSearch := false
	completedSourceRead := false
	seenKnowledgeRequests := make(map[string]bool)
	for ordinal := 1; ordinal <= budget.MaxCalls; ordinal++ {
		view := RiskAgentView{
			Knowledge:               cloneProtocolKnowledge(knowledge),
			TargetSurface:           cloneAgentTargetSurface(targetSurface),
			ObservationCapabilities: cloneObservationCapabilities(capabilities),
			BindingDomains:          append([]semantic.ObservationBindingDomain(nil), bindingDomains...),
			Actions:                 append([]control.ActionKind(nil), actions...), MaxMilestones: RiskCandidateMaxSteps,
			MaxCandidates:     RiskCandidatePortfolioMax,
			ExplorationMemory: cloneRiskExplorationMemory(memory),
			KnowledgeSources:  cloneKnowledgeSources(knowledgeSources),
			KnowledgeResults:  cloneKnowledgeReadResults(knowledgeResults),
			AvailableSupportRefs: VisibleRiskSupportRefs(
				knowledge, targetSurface, knowledgeResults,
			),
			Prior: cloneRiskAgentFeedback(prior),
		}
		remainingKnowledgeRequests := RiskKnowledgeRequestMaxAttempts - knowledgeRequests
		if knowledgeReader != nil && !completedSourceRead &&
			remainingKnowledgeRequests > 0 && ordinal < budget.MaxCalls &&
			stoppedKnowledgeRequests <= RiskKnowledgeRequestRetryMax {
			view.MaxKnowledgeRequests = min(RiskKnowledgeRequestsPerCall, remainingKnowledgeRequests)
		}
		response, work, err := planner(ctx, view)
		if !validStatelessPlannerWork(work) {
			return result, errors.New("EXPERIMENT_RISK_AGENT_MODEL_WORK_INVALID")
		}
		addModelWork(&result.ModelWork, work)
		attempt := RiskAgentAttempt{
			Ordinal: ordinal, ResponseBytes: append([]byte(nil), response...), ModelWork: work,
		}
		if err != nil {
			var failure *RiskPlannerResponseFailure
			if !errors.As(err, &failure) || failure.Code == "" {
				return result, err
			}
			attempt.ResponseBytes = nil
			attempt.Feedback = RiskAgentFeedback{Outcome: RiskAgentStopped, ReasonCode: failure.Code}
			if result.ModelWork.TotalTokens > budget.MaxTokens {
				attempt.Feedback.ReasonCode = RiskAgentReasonTokenBudget
			}
			result.Attempts = append(result.Attempts, attempt)
			if result.ModelWork.TotalTokens > budget.MaxTokens || !failure.Repairable || ordinal == budget.MaxCalls {
				return result, nil
			}
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		if result.ModelWork.TotalTokens > budget.MaxTokens {
			attempt.Feedback = RiskAgentFeedback{Outcome: RiskAgentStopped, ReasonCode: RiskAgentReasonTokenBudget}
			result.Attempts = append(result.Attempts, attempt)
			return result, nil
		}
		portfolio, requests, isKnowledgeRequest, err := parseRiskAgentResponse(response)
		if err != nil {
			reason := RiskAgentReasonCandidate
			if errors.Is(err, errRiskCandidateJSON) {
				reason = RiskAgentReasonJSON
			} else if errors.Is(err, errRiskCandidateBinding) {
				reason = RiskAgentReasonBinding
			}
			attempt.Feedback = RiskAgentFeedback{Outcome: RiskAgentStopped, ReasonCode: reason}
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		if isKnowledgeRequest {
			attempt.KnowledgeRequests = append([]KnowledgeReadRequest(nil), requests...)
			reason := RiskAgentReasonKnowledgeRead
			if knowledgeReader == nil {
				reason = RiskAgentReasonKnowledgeUnavailable
			} else if !riskKnowledgeRequestMatchesGroundingPhase(
				requests, completedSearch, completedSourceRead, knowledgeResults,
			) {
				reason = RiskAgentReasonKnowledgeGrounding
			} else if knowledgeRequests+len(requests) > RiskKnowledgeRequestMaxAttempts ||
				!validRiskKnowledgeRequests(
					requests, knowledgeSources, knowledgeResults, seenKnowledgeRequests,
				) {
				reason = RiskAgentReasonKnowledgeBudget
				if knowledgeRequests+len(requests) <= RiskKnowledgeRequestMaxAttempts {
					reason = RiskAgentReasonKnowledgeInvalid
				}
			} else {
				completedRead := false
				for _, request := range requests {
					read, readErr := knowledgeReader(request)
					if readErr != nil || read.Validate() != nil ||
						(request.Query == "" && read.Source.Reference != request.Reference) ||
						(request.Query != "" && read.Query != request.Query) {
						return result, errors.New("EXPERIMENT_RISK_AGENT_KNOWLEDGE_READ_INVALID")
					}
					for _, match := range read.Matches {
						knowledgeSources = appendRiskKnowledgeSource(
							knowledgeSources,
							KnowledgeSource{Reference: match.Reference, Path: match.Reference},
						)
					}
					knowledgeResults = append(knowledgeResults, read)
					attempt.KnowledgeResults = append(attempt.KnowledgeResults, cloneKnowledgeReadResult(read))
					knowledgeRequests++
					seenKnowledgeRequests[riskKnowledgeRequestKey(request)] = true
					completedRead = completedRead || read.Status == KnowledgeDiscoveryCompleted
					if read.Status == KnowledgeDiscoveryStopped {
						stoppedKnowledgeRequests++
					}
					if read.Status == KnowledgeDiscoveryCompleted {
						if request.Query != "" {
							completedSearch = true
						} else {
							completedSourceRead = true
						}
					}
				}
				if !completedRead {
					reason = RiskAgentReasonKnowledgeReadStopped
				}
			}
			attempt.Feedback = RiskAgentFeedback{Outcome: RiskAgentStopped, ReasonCode: reason}
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		if knowledgeReader != nil && (!completedSearch || !completedSourceRead) {
			attempt.Feedback = RiskAgentFeedback{
				Outcome: RiskAgentStopped, ReasonCode: RiskAgentReasonKnowledgeGrounding,
			}
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		reviews := make([]RiskCandidateReview, 0, len(portfolio.Candidates))
		seen := make(map[string]bool, len(portfolio.Candidates))
		seenSemantic := make(map[string]bool, len(portfolio.Candidates))
		var firstAssessment *RiskCandidateAssessment
		var selected *RiskCandidateAssessment
		var executable []RiskCandidateAssessment
		for index, candidate := range portfolio.Candidates {
			review := RiskCandidateReview{Ordinal: index + 1, CandidateID: candidate.ID, Outcome: RiskAgentStopped}
			if seen[candidate.ID] {
				review.ReasonCode = RiskAgentReasonDuplicate
				reviews = append(reviews, review)
				continue
			}
			seen[candidate.ID] = true
			candidate, draftErr := normalizeRiskCandidateDraft(
				candidate, portfolio.requiresBoundMechanismSteps,
			)
			if draftErr != nil {
				review.ReasonCode = riskCandidateReason(draftErr)
				reviews = append(reviews, review)
				continue
			}
			if supportErr := validateRiskCandidateSupport(
				candidate, view.AvailableSupportRefs, portfolio.requiresBoundMechanismSteps,
			); supportErr != nil {
				review.ReasonCode = RiskAgentReasonSupport
				reviews = append(reviews, review)
				continue
			}
			if draftErr := candidate.ValidateAgentDraft(); draftErr != nil {
				review.ReasonCode = riskCandidateReason(draftErr)
				reviews = append(reviews, review)
				continue
			}
			semanticIdentity, identityErr := RiskCandidateSemanticIdentity(candidate)
			if identityErr != nil {
				review.ReasonCode = RiskAgentReasonCandidate
				reviews = append(reviews, review)
				continue
			}
			if seenSemantic[semanticIdentity] {
				review.ReasonCode = RiskAgentReasonDuplicate
				reviews = append(reviews, review)
				continue
			}
			seenSemantic[semanticIdentity] = true
			assessment, assessErr := AssessRiskCandidateForTarget(
				knowledge, candidate, capabilities, actions, targetSurface,
			)
			if assessErr != nil {
				review.ReasonCode = riskCandidateReason(assessErr)
				reviews = append(reviews, review)
				continue
			}
			if firstAssessment == nil {
				value := assessment
				firstAssessment = &value
			}
			review.Issues = append([]semantic.RiskQualificationIssue(nil), assessment.Qualification.Issues...)
			review.CapabilityGaps = cloneAgentCapabilityGaps(assessment.CapabilityGaps)
			review.FidelityNotices = cloneAgentCapabilityGaps(assessment.FidelityNotices)
			if len(assessment.CapabilityGaps) > 0 {
				review.ReasonCode = RiskAgentReasonFidelity
				reviews = append(reviews, review)
				continue
			}
			if !assessment.Qualification.Qualified {
				review.ReasonCode = RiskAgentReasonNotExecutable
				reviews = append(reviews, review)
				continue
			}
			review.Outcome = RiskCandidateExecutable
			reviews = append(reviews, review)
			executable = append(executable, assessment)
			if selected == nil {
				value := assessment
				selected = &value
			}
		}
		attempt.Feedback = RiskAgentFeedback{
			Outcome: RiskAgentStopped, ReasonCode: RiskAgentReasonPortfolio,
			Reviews: cloneRiskCandidateReviews(reviews),
		}
		attempt.Assessment = firstAssessment
		if selected == nil {
			if len(reviews) == 1 {
				attempt.Feedback.CandidateID = reviews[0].CandidateID
				attempt.Feedback.ReasonCode = reviews[0].ReasonCode
				attempt.Feedback.Issues = append(
					[]semantic.RiskQualificationIssue(nil), reviews[0].Issues...,
				)
				attempt.Feedback.CapabilityGaps = cloneAgentCapabilityGaps(
					reviews[0].CapabilityGaps,
				)
			}
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		attempt.Assessment = selected
		attempt.Feedback.Outcome = RiskAgentAccepted
		attempt.Feedback.ReasonCode = ""
		attempt.Feedback.CandidateID = selected.Candidate.ID
		attempt.Feedback.Issues = append(
			[]semantic.RiskQualificationIssue(nil), selected.Qualification.Issues...,
		)
		attempt.Feedback.FidelityNotices = cloneAgentCapabilityGaps(selected.FidelityNotices)
		result.Attempts = append(result.Attempts, attempt)
		accepted := *selected
		result.Accepted = &accepted
		result.Executable = executable
		result.Status = RiskAgentAccepted
		return result, nil
	}
	return result, nil
}

func riskKnowledgeRequestMatchesGroundingPhase(
	requests []KnowledgeReadRequest,
	completedSearch bool,
	completedSourceRead bool,
	results []KnowledgeReadResult,
) bool {
	if len(requests) != 1 || completedSourceRead {
		return false
	}
	for _, request := range requests {
		if !completedSearch && request.Query == "" {
			return false
		}
		if completedSearch && request.Query != "" {
			return false
		}
		if completedSearch {
			found := false
			for _, result := range results {
				for _, match := range result.Matches {
					found = found || match.Reference == request.Reference
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func riskCandidateReason(err error) string {
	switch {
	case errors.Is(err, errRiskCandidateJSON):
		return RiskAgentReasonJSON
	case errors.Is(err, errRiskCandidateBinding):
		return RiskAgentReasonBinding
	case errors.Is(err, errRiskCandidateBindingDomain):
		return RiskAgentReasonBindingDomain
	case errors.Is(err, errRiskCandidateDuplicate):
		return RiskAgentReasonDuplicate
	case errors.Is(err, errRiskCandidateProperty):
		return RiskAgentReasonProperty
	case errors.Is(err, errRiskCandidatePattern):
		return RiskAgentReasonInspiration
	case errors.Is(err, errRiskCandidateAlignment):
		return RiskAgentReasonAlignment
	case errors.Is(err, errRiskCandidateSupport):
		return RiskAgentReasonSupport
	default:
		return RiskAgentReasonCandidate
	}
}

func parseRiskAgentResponse(data []byte) (
	RiskCandidatePortfolio,
	[]KnowledgeReadRequest,
	bool,
	error,
) {
	if len(data) == 0 || len(data) > RiskCandidateMaxBytes {
		return RiskCandidatePortfolio{}, nil, false, errRiskCandidateJSON
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) != nil {
		return RiskCandidatePortfolio{}, nil, false, errRiskCandidateJSON
	}
	if _, ok := probe["response_kind"]; ok {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var envelope riskAgentResponseEnvelope
		if decoder.Decode(&envelope) != nil {
			return RiskCandidatePortfolio{}, nil, false, errRiskCandidateJSON
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return RiskCandidatePortfolio{}, nil, false, errRiskCandidateJSON
		}
		switch envelope.ResponseKind {
		case riskAgentResponsePortfolio:
			if len(envelope.Candidates) == 0 || len(envelope.Candidates) > RiskCandidatePortfolioMax ||
				len(envelope.KnowledgeRequests) != 0 {
				return RiskCandidatePortfolio{}, nil, false, errRiskCandidateJSON
			}
			return RiskCandidatePortfolio{
				Candidates: envelope.Candidates, requiresBoundMechanismSteps: true,
			}, nil, false, nil
		case riskAgentResponseKnowledgeQuery:
			if len(envelope.Candidates) != 0 || len(envelope.KnowledgeRequests) == 0 ||
				len(envelope.KnowledgeRequests) > RiskKnowledgeRequestsPerCall {
				return RiskCandidatePortfolio{}, nil, false, errRiskCandidateJSON
			}
			return RiskCandidatePortfolio{}, append(
				[]KnowledgeReadRequest(nil), envelope.KnowledgeRequests...,
			), true, nil
		default:
			return RiskCandidatePortfolio{}, nil, false, errRiskCandidateJSON
		}
	}
	if _, ok := probe["knowledge_requests"]; !ok {
		portfolio, err := ParseRiskCandidatePortfolio(data)
		return portfolio, nil, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var batch RiskKnowledgeRequestBatch
	if decoder.Decode(&batch) != nil || len(batch.KnowledgeRequests) == 0 ||
		len(batch.KnowledgeRequests) > RiskKnowledgeRequestsPerCall {
		return RiskCandidatePortfolio{}, nil, false, errRiskCandidateJSON
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return RiskCandidatePortfolio{}, nil, false, errRiskCandidateJSON
	}
	return RiskCandidatePortfolio{}, append([]KnowledgeReadRequest(nil), batch.KnowledgeRequests...), true, nil
}

func ParseRiskCandidatePortfolio(data []byte) (RiskCandidatePortfolio, error) {
	if len(data) == 0 || len(data) > RiskCandidateMaxBytes {
		return RiskCandidatePortfolio{}, errRiskCandidateJSON
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) != nil {
		return RiskCandidatePortfolio{}, errRiskCandidateJSON
	}
	if _, ok := probe["candidates"]; !ok {
		candidate, err := ParseRiskCandidate(data)
		if err != nil {
			return RiskCandidatePortfolio{}, err
		}
		return RiskCandidatePortfolio{
			Candidates: []RiskCandidate{candidate}, requiresBoundMechanismSteps: true,
		}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var portfolio RiskCandidatePortfolio
	if decoder.Decode(&portfolio) != nil || len(portfolio.Candidates) == 0 ||
		len(portfolio.Candidates) > RiskCandidatePortfolioMax {
		return RiskCandidatePortfolio{}, errRiskCandidateJSON
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return RiskCandidatePortfolio{}, errRiskCandidateJSON
	}
	portfolio.requiresBoundMechanismSteps = true
	return portfolio, nil
}

func ParseRiskCandidate(data []byte) (RiskCandidate, error) {
	if len(data) == 0 || len(data) > RiskCandidateMaxBytes {
		return RiskCandidate{}, errRiskCandidateJSON
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var candidate RiskCandidate
	if err := decoder.Decode(&candidate); err != nil {
		return RiskCandidate{}, errRiskCandidateJSON
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return RiskCandidate{}, errRiskCandidateJSON
	}
	var err error
	candidate, err = normalizeRiskCandidateDraft(candidate, false)
	if err != nil {
		return RiskCandidate{}, err
	}
	return candidate, nil
}

func (candidate RiskCandidate) Validate() error {
	if !validMethodToken(candidate.ID) || len(candidate.ID) > 128 ||
		candidate.Summary == "" || len(candidate.Summary) > 2048 ||
		strings.TrimSpace(candidate.Summary) != candidate.Summary || len(candidate.Predicates) < 2 ||
		len(candidate.Predicates) > RiskCandidateMaxSteps ||
		!canonicalStrings(candidate.RequiredFidelity, false) {
		return errors.New("EXPERIMENT_RISK_CANDIDATE_INVALID")
	}
	seen := make(map[string]bool, len(candidate.Predicates))
	bindings := make(map[string]int)
	for _, predicate := range candidate.Predicates {
		if !validMethodToken(predicate.MilestoneID) || len(predicate.MilestoneID) > 128 || seen[predicate.MilestoneID] {
			return errors.New("EXPERIMENT_RISK_CANDIDATE_MILESTONE_INVALID")
		}
		seen[predicate.MilestoneID] = true
		for _, constraint := range predicate.Constraints {
			if constraint.BindAs != "" {
				bindings[constraint.BindAs]++
			}
		}
	}
	for _, count := range bindings {
		if count < 2 {
			return errRiskCandidateBinding
		}
	}
	if len(candidate.MechanismSteps) > 0 {
		derived, err := deriveRiskMechanism(candidate.Predicates, candidate.MechanismSteps)
		if err != nil || candidate.SuspectedMechanism != derived {
			return errRiskCandidateAlignment
		}
		if validateRiskCandidateSupport(candidate, nil, false) != nil {
			return errRiskCandidateSupport
		}
	}
	requirements, err := semantic.CompileRiskRequirements(candidate.Predicates)
	if err != nil || len(requirements.Actions) == 0 {
		return errors.New("EXPERIMENT_RISK_CANDIDATE_PREDICATE_INVALID")
	}
	return nil
}

// ValidateAgentDraft requires the richer A9e authoring fields. Validate stays
// backward compatible so previously published A9d artifacts remain readable.
func (candidate RiskCandidate) ValidateAgentDraft() error {
	if err := candidate.Validate(); err != nil {
		return err
	}
	if !validMethodToken(candidate.PropertyRef) || len(candidate.PropertyRef) > agentMaterialReferenceMaxBytes ||
		(candidate.InspirationRef != "original" && (!validMethodToken(candidate.InspirationRef) ||
			len(candidate.InspirationRef) > agentMaterialReferenceMaxBytes)) ||
		candidate.SuspectedMechanism == "" || len(candidate.SuspectedMechanism) > 2048 ||
		strings.TrimSpace(candidate.SuspectedMechanism) != candidate.SuspectedMechanism {
		return errors.New("EXPERIMENT_RISK_CANDIDATE_AGENT_FIELDS_INVALID")
	}
	if _, err := scenarioRiskCandidateKnowledge(candidate); err != nil {
		return err
	}
	return nil
}

func normalizeRiskCandidateDraft(candidate RiskCandidate, requireBoundSteps bool) (RiskCandidate, error) {
	candidate.RequiredFidelity = append([]string(nil), candidate.RequiredFidelity...)
	sort.Strings(candidate.RequiredFidelity)
	if len(candidate.MechanismSteps) == 0 {
		if requireBoundSteps {
			return RiskCandidate{}, errRiskCandidateAlignment
		}
		return candidate, candidate.ValidateAgentDraft()
	}
	derived, err := deriveRiskMechanism(candidate.Predicates, candidate.MechanismSteps)
	if err != nil || (candidate.SuspectedMechanism != "" && candidate.SuspectedMechanism != derived) {
		return RiskCandidate{}, errRiskCandidateAlignment
	}
	candidate.MechanismSteps = append([]RiskMechanismStep(nil), candidate.MechanismSteps...)
	for index := range candidate.MechanismSteps {
		candidate.MechanismSteps[index].SupportRefs = append(
			[]string(nil), candidate.MechanismSteps[index].SupportRefs...,
		)
	}
	candidate.SuspectedMechanism = derived
	if err := candidate.ValidateAgentDraft(); err != nil {
		return RiskCandidate{}, err
	}
	return candidate, nil
}

func deriveRiskMechanism(
	predicates []semantic.ObservationPredicate,
	steps []RiskMechanismStep,
) (string, error) {
	if len(steps) != len(predicates) || len(steps) < 2 || len(steps) > RiskCandidateMaxSteps {
		return "", errRiskCandidateAlignment
	}
	parts := make([]string, len(steps))
	for index, step := range steps {
		predicate := predicates[index]
		if step.MilestoneID != predicate.MilestoneID || step.Kind != predicate.Kind ||
			step.Rationale == "" || len(step.Rationale) > RiskMechanismStepMaxBytes ||
			strings.TrimSpace(step.Rationale) != step.Rationale {
			return "", errRiskCandidateAlignment
		}
		parts[index] = step.MilestoneID + " [" + string(step.Kind) + "]: " + step.Rationale
	}
	mechanism := strings.Join(parts, " -> ")
	if len(mechanism) > 2048 {
		return "", errRiskCandidateAlignment
	}
	return mechanism, nil
}

func validateRiskCandidateSupport(
	candidate RiskCandidate,
	available []string,
	required bool,
) error {
	visible := make(map[string]bool, len(available))
	for _, reference := range available {
		visible[reference] = true
	}
	for _, step := range candidate.MechanismSteps {
		if (required && len(step.SupportRefs) == 0) || len(step.SupportRefs) > RiskMechanismSupportMax {
			return errRiskCandidateSupport
		}
		seen := make(map[string]bool, len(step.SupportRefs))
		for _, reference := range step.SupportRefs {
			if reference == "" || len(reference) > riskSupportReferenceMaxBytes || seen[reference] ||
				available != nil && !visible[reference] {
				return errRiskCandidateSupport
			}
			seen[reference] = true
		}
	}
	return nil
}

func AssessRiskCandidate(
	knowledge ProtocolKnowledgePack,
	candidate RiskCandidate,
	capabilities []semantic.ObservationCapability,
	actions []control.ActionKind,
) (RiskCandidateAssessment, error) {
	if len(candidate.RequiredFidelity) > 0 {
		return RiskCandidateAssessment{}, errors.New("EXPERIMENT_RISK_CANDIDATE_TARGET_REQUIRED")
	}
	return assessRiskCandidate(knowledge, candidate, capabilities, actions, nil)
}

func AssessRiskCandidateForTarget(
	knowledge ProtocolKnowledgePack,
	candidate RiskCandidate,
	capabilities []semantic.ObservationCapability,
	actions []control.ActionKind,
	targetSurface *AgentTargetSurface,
) (RiskCandidateAssessment, error) {
	if targetSurface == nil {
		return AssessRiskCandidate(knowledge, candidate, capabilities, actions)
	}
	if targetSurface.ValidateAgainstKnowledge(knowledge) != nil ||
		!targetSurface.MatchesPlanningInputs(actions, capabilities) {
		return RiskCandidateAssessment{}, errors.New("EXPERIMENT_RISK_CANDIDATE_TARGET_INVALID")
	}
	return assessRiskCandidate(knowledge, candidate, capabilities, actions, targetSurface)
}

func assessRiskCandidate(
	knowledge ProtocolKnowledgePack,
	candidate RiskCandidate,
	capabilities []semantic.ObservationCapability,
	actions []control.ActionKind,
	targetSurface *AgentTargetSurface,
) (RiskCandidateAssessment, error) {
	if knowledge.Validate() != nil || candidate.ValidateAgentDraft() != nil {
		return RiskCandidateAssessment{}, errors.New("EXPERIMENT_RISK_CANDIDATE_INPUT_INVALID")
	}
	if err := semantic.ValidateObservationPredicateBindings(candidate.Predicates, capabilities); err != nil {
		return RiskCandidateAssessment{}, errors.Join(errRiskCandidateBindingDomain, err)
	}
	propertyFound := false
	for _, property := range knowledge.Properties {
		if property.ID == candidate.PropertyRef {
			propertyFound = true
			break
		}
	}
	if !propertyFound {
		return RiskCandidateAssessment{}, errRiskCandidateProperty
	}
	if candidate.InspirationRef != "original" {
		patternFound := false
		for _, pattern := range knowledge.IssuePatterns {
			if pattern.ID == candidate.InspirationRef {
				patternFound = true
				break
			}
		}
		if !patternFound {
			return RiskCandidateAssessment{}, errRiskCandidatePattern
		}
	}
	for _, existing := range knowledge.Risks {
		if existing.ID == candidate.ID {
			return RiskCandidateAssessment{}, errRiskCandidateDuplicate
		}
	}
	spec, err := riskCandidateWitnessSpec(knowledge.Family, candidate)
	if err != nil {
		return RiskCandidateAssessment{}, err
	}
	qualification, err := semantic.QualifyRisk(candidate.Predicates, capabilities, actions)
	if err != nil {
		return RiskCandidateAssessment{}, err
	}
	assessment := RiskCandidateAssessment{Candidate: candidate, Spec: spec, Qualification: qualification}
	if targetSurface != nil {
		assessment.CapabilityGaps, err = targetSurface.FidelityGaps(candidate.RequiredFidelity)
		if err != nil {
			return RiskCandidateAssessment{}, err
		}
		assessment.FidelityAssessment, assessment.FidelityNotices, err =
			targetSurface.FidelityAssessmentForProperty(candidate.PropertyRef, candidate.RequiredFidelity)
		if err != nil {
			return RiskCandidateAssessment{}, err
		}
	}
	return assessment, nil
}

func riskCandidateWitnessSpec(
	family string,
	candidate RiskCandidate,
) (semantic.RiskWitnessSpec, error) {
	milestones := make([]string, len(candidate.Predicates))
	orders := make([]semantic.RiskWitnessOrder, 0, len(candidate.Predicates)-1)
	for index, predicate := range candidate.Predicates {
		milestones[index] = predicate.MilestoneID
		if index > 0 {
			orders = append(orders, semantic.RiskWitnessOrder{
				Before: candidate.Predicates[index-1].MilestoneID, After: predicate.MilestoneID,
			})
		}
	}
	return semantic.NewRiskWitnessSpec(
		candidate.ID+"-witness", family, candidate.ID, milestones, orders,
	)
}

func validRiskAgentActions(actions []control.ActionKind) bool {
	seen := make(map[control.ActionKind]bool, len(actions))
	for _, action := range actions {
		if action.Validate() != nil || seen[action] {
			return false
		}
		seen[action] = true
	}
	return len(actions) > 0
}

func validRiskExplorationMemory(values []RiskExplorationMemoryEntry) bool {
	if len(values) > RiskExplorationMemoryMax {
		return false
	}
	previousEpisode := 0
	for _, value := range values {
		if value.Episode <= previousEpisode || !validRiskMemoryOutcome(value.EpisodeOutcome) ||
			value.ModelCalls < 0 || value.ModelTokens < 0 ||
			value.SearchWorkUnits < 0 || value.ExecutionWorkUnits < 0 ||
			(value.RepeatedCandidate && value.CandidateID == "") {
			return false
		}
		if value.CandidateID != "" && (!validMethodToken(value.CandidateID) ||
			len(value.Summary) > 2048 || len(value.SuspectedMechanism) > 2048) {
			return false
		}
		if (value.PropertyRef == "") != (value.EvidenceLevel == "") ||
			value.PropertyRef != "" && (!validMethodToken(value.PropertyRef) ||
				!validPropertyEvidenceLevel(value.EvidenceLevel)) {
			return false
		}
		if value.RiskStatus != "" && value.RiskStatus != semantic.RiskWitnessReached &&
			value.RiskStatus != semantic.RiskWitnessNotReached {
			return false
		}
		if value.FirstMissingMilestone != "" && !validMethodToken(value.FirstMissingMilestone) {
			return false
		}
		for _, milestone := range value.SatisfiedMilestones {
			if !validMethodToken(milestone) {
				return false
			}
		}
		for _, reason := range value.MechanicalReasonCodes {
			if !validMethodToken(reason) {
				return false
			}
		}
		seenGaps := make(map[string]bool, len(value.CapabilityGaps))
		for _, gap := range value.CapabilityGaps {
			key := gap.Code + "\x00" + gap.Reference + "\x00" + gap.Summary
			if !validMethodToken(gap.Code) || !validMethodToken(gap.Reference) ||
				strings.TrimSpace(gap.Summary) != gap.Summary || gap.Summary == "" ||
				len(gap.Summary) > protocolKnowledgeTextMaxBytes || seenGaps[key] {
				return false
			}
			seenGaps[key] = true
		}
		previousEpisode = value.Episode
	}
	return true
}

func validRiskMemoryOutcome(outcome string) bool {
	switch outcome {
	case RiskMemoryOutcomeExecutionCompleted, RiskMemoryOutcomeBudgetExhausted,
		RiskMemoryOutcomeWitnessNearMiss, RiskMemoryOutcomePlanningStopped,
		RiskMemoryOutcomeExecutionFailed, RiskMemoryOutcomeHypothesisAbandoned:
		return true
	default:
		return false
	}
}

func validRiskKnowledgeView(view RiskAgentView) bool {
	if len(view.KnowledgeSources) == 0 {
		return view.MaxKnowledgeRequests == 0 && len(view.KnowledgeResults) == 0
	}
	if view.MaxKnowledgeRequests < 0 || view.MaxKnowledgeRequests > RiskKnowledgeRequestsPerCall ||
		(view.MaxKnowledgeRequests == 0 && len(view.KnowledgeResults) == 0) ||
		len(view.KnowledgeResults) > RiskKnowledgeRequestMaxAttempts {
		return false
	}
	want, err := KnowledgeSourceCatalog(view.Knowledge)
	if err != nil || len(view.KnowledgeSources) < len(want) ||
		!reflect.DeepEqual(view.KnowledgeSources[:len(want)], want) {
		return false
	}
	declared := make(map[string]bool, len(view.KnowledgeSources))
	for index, source := range view.KnowledgeSources {
		if source.Reference == "" || source.Path == "" || declared[source.Reference] ||
			index >= len(want) && (source.Reference != source.Path || len(source.MaterialIDs) != 0) {
			return false
		}
		declared[source.Reference] = true
	}
	for _, result := range view.KnowledgeResults {
		if result.Validate() != nil ||
			result.Query == "" && !declared[result.Source.Reference] {
			return false
		}
		for _, match := range result.Matches {
			if !declared[match.Reference] {
				return false
			}
		}
	}
	return true
}

func validRiskSupportView(view RiskAgentView) bool {
	want := VisibleRiskSupportRefs(view.Knowledge, view.TargetSurface, view.KnowledgeResults)
	return len(want) > 0 && reflect.DeepEqual(view.AvailableSupportRefs, want)
}

// VisibleRiskSupportRefs derives the exact material identifiers an Agent may
// cite in the current call. A source reference appears only after a completed
// bounded read, not merely because the Dossier declared it.
func VisibleRiskSupportRefs(
	knowledge ProtocolKnowledgePack,
	surface *AgentTargetSurface,
	results []KnowledgeReadResult,
) []string {
	var refs []string
	for _, statement := range knowledge.Knowledge {
		refs = append(refs, "primer/"+statement.ID)
	}
	for _, property := range knowledge.Properties {
		refs = append(refs, "property/"+property.ID)
	}
	for _, pattern := range knowledge.IssuePatterns {
		refs = append(refs, "pattern/"+pattern.ID)
	}
	if knowledge.TargetDossier != nil {
		sections := []struct {
			name      string
			materials []TargetMaterial
		}{
			{"assumptions", knowledge.TargetDossier.Assumptions},
			{"components", knowledge.TargetDossier.Components},
			{"contracts", knowledge.TargetDossier.Contracts},
			{"control-semantics", knowledge.TargetDossier.ControlSemantics},
			{"active-experiment", knowledge.TargetDossier.ActiveExperiment},
			{"blind-spots", knowledge.TargetDossier.BlindSpots},
		}
		for _, section := range sections {
			for _, material := range section.materials {
				refs = append(refs, "dossier/"+section.name+"/"+material.ID)
			}
		}
	}
	if surface != nil {
		refs = append(refs,
			"surface/target", "surface/topology", "surface/workload",
			"surface/runtime", "surface/fault-allowance", "surface/actions",
			"surface/observations", "surface/oracles",
		)
		if len(surface.Capabilities.FidelityBoundaries) > 0 {
			refs = append(refs, "surface/fidelity")
		}
		if len(surface.TemporalKinds) > 0 {
			refs = append(refs, "surface/temporal")
		}
		if len(surface.CrashModes) > 0 {
			refs = append(refs, "surface/lifecycle")
		}
		if len(surface.EffectKinds) > 0 {
			refs = append(refs, "surface/effects")
		}
		if surface.DurableCheckpoints {
			refs = append(refs, "surface/durability")
		}
	}
	for _, result := range results {
		if result.Status == KnowledgeDiscoveryCompleted && result.Query == "" {
			refs = append(refs, "source/"+result.Source.Reference)
		}
	}
	sort.Strings(refs)
	return compactSortedStrings(refs)
}

func validRiskKnowledgeRequests(
	requests []KnowledgeReadRequest,
	sources []KnowledgeSource,
	results []KnowledgeReadResult,
	seen map[string]bool,
) bool {
	if len(requests) == 0 || len(requests) > RiskKnowledgeRequestsPerCall {
		return false
	}
	declared := make(map[string]bool, len(sources))
	for _, source := range sources {
		declared[source.Reference] = true
	}
	current := make(map[string]bool, len(requests))
	completedSourceRead := false
	for _, result := range results {
		if result.Status == KnowledgeDiscoveryCompleted && result.Query == "" {
			completedSourceRead = true
		}
	}
	if completedSourceRead {
		return false
	}
	for _, request := range requests {
		key := riskKnowledgeRequestKey(request)
		if current[key] || seen[key] {
			return false
		}
		if request.Query != "" {
			if !validKnowledgeSearchRequest(request) {
				return false
			}
			current[key] = true
			continue
		}
		if !declared[request.Reference] || !validKnowledgeReadRequest(request) ||
			request.MaxLines > RiskKnowledgeRequestMaxLines {
			return false
		}
		current[key] = true
	}
	return true
}

func riskKnowledgeRequestKey(request KnowledgeReadRequest) string {
	if request.Query != "" {
		return "search\x00" + request.Query + "\x00" + fmt.Sprintf("%d", request.MaxResults)
	}
	return request.Reference + "\x00" + fmt.Sprintf("%d:%d", request.StartLine, request.MaxLines)
}

func appendRiskKnowledgeSource(values []KnowledgeSource, source KnowledgeSource) []KnowledgeSource {
	for _, existing := range values {
		if existing.Reference == source.Reference {
			return values
		}
	}
	return append(values, source)
}

func cloneObservationCapabilities(
	values []semantic.ObservationCapability,
) []semantic.ObservationCapability {
	result := append([]semantic.ObservationCapability(nil), values...)
	for index := range result {
		result[index].Fields = append([]semantic.ObservationField(nil), values[index].Fields...)
		if values[index].FieldTypes != nil {
			result[index].FieldTypes = make(
				map[semantic.ObservationField]semantic.ObservationValueType,
				len(values[index].FieldTypes),
			)
			for field, valueType := range values[index].FieldTypes {
				result[index].FieldTypes[field] = valueType
			}
		}
		if values[index].Values != nil {
			result[index].Values = make(map[semantic.ObservationField][]string, len(values[index].Values))
			for field, allowed := range values[index].Values {
				result[index].Values[field] = append([]string(nil), allowed...)
			}
		}
	}
	return result
}

func cloneKnowledgeSources(values []KnowledgeSource) []KnowledgeSource {
	result := append([]KnowledgeSource(nil), values...)
	for index := range result {
		result[index].MaterialIDs = append([]string(nil), values[index].MaterialIDs...)
	}
	return result
}

func cloneKnowledgeReadResult(value KnowledgeReadResult) KnowledgeReadResult {
	result := value
	result.Source.MaterialIDs = append([]string(nil), value.Source.MaterialIDs...)
	result.Matches = append([]KnowledgeSearchMatch(nil), value.Matches...)
	return result
}

func cloneKnowledgeReadResults(values []KnowledgeReadResult) []KnowledgeReadResult {
	result := make([]KnowledgeReadResult, len(values))
	for index, value := range values {
		result[index] = cloneKnowledgeReadResult(value)
	}
	return result
}

func cloneRiskExplorationMemory(values []RiskExplorationMemoryEntry) []RiskExplorationMemoryEntry {
	result := append([]RiskExplorationMemoryEntry(nil), values...)
	for index := range result {
		result[index].SatisfiedMilestones = append(
			[]string(nil), values[index].SatisfiedMilestones...,
		)
		result[index].MechanicalReasonCodes = append(
			[]string(nil), values[index].MechanicalReasonCodes...,
		)
		result[index].CapabilityGaps = cloneAgentCapabilityGaps(values[index].CapabilityGaps)
	}
	return result
}

func cloneRiskAgentFeedback(value *RiskAgentFeedback) *RiskAgentFeedback {
	if value == nil {
		return nil
	}
	result := *value
	result.Issues = append([]semantic.RiskQualificationIssue(nil), value.Issues...)
	result.CapabilityGaps = cloneAgentCapabilityGaps(value.CapabilityGaps)
	result.FidelityNotices = cloneAgentCapabilityGaps(value.FidelityNotices)
	result.Reviews = cloneRiskCandidateReviews(value.Reviews)
	return &result
}

func cloneRiskCandidateReviews(values []RiskCandidateReview) []RiskCandidateReview {
	result := append([]RiskCandidateReview(nil), values...)
	for index := range result {
		result[index].Issues = append(
			[]semantic.RiskQualificationIssue(nil), values[index].Issues...,
		)
		result[index].CapabilityGaps = cloneAgentCapabilityGaps(values[index].CapabilityGaps)
		result[index].FidelityNotices = cloneAgentCapabilityGaps(values[index].FidelityNotices)
	}
	return result
}

func cloneAgentCapabilityGaps(values []AgentCapabilityGap) []AgentCapabilityGap {
	return append([]AgentCapabilityGap(nil), values...)
}

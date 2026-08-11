package controlexperiment

import (
	"errors"
	"sort"
)

const (
	AgentFollowUpSpecVersion  = "consensus-atlas/agent-follow-up-spec/v1"
	AgentFollowUpReusePolicy  = "shared-verified-source-charged-to-each-arm"
	AgentFollowUpBaselineRule = "workload-completion-then-execution-canonical-v2"
)

type AgentFollowUpObservedWork struct {
	ExecutionAttempts int       `json:"execution_attempts"`
	PrimaryWorkUnits  int       `json:"primary_work_units"`
	ReplayWorkUnits   int       `json:"replay_work_units"`
	Model             ModelWork `json:"model"`
}

// AgentFollowUpSpec freezes the data split and equal-budget boundary before
// any no-feedback or with-feedback model call. Source bundles may be reused
// physically, but their full logical cost is charged to every evaluated arm.
// Target composition remains responsible for proving that native policy
// identities correspond to SourceSeeds and FollowUpSeed.
type AgentFollowUpSpec struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`

	SemanticViewDigest   string   `json:"semantic_view_digest"`
	FeedbackDigest       string   `json:"feedback_digest"`
	FeedbackSourceDigest string   `json:"feedback_source_digest"`
	PSSID                string   `json:"pss_id"`
	BackendIDs           []string `json:"backend_ids"`

	SourceSeeds  []uint64 `json:"source_seeds"`
	FollowUpSeed uint64   `json:"follow_up_seed"`
	Decisions    int      `json:"decisions"`

	SourceBudget       MethodBudget              `json:"source_budget"`
	SourceObservedWork AgentFollowUpObservedWork `json:"source_observed_work"`
	FollowUpBudget     MethodBudget              `json:"follow_up_budget"`
	PerArmBudget       MethodBudget              `json:"per_arm_budget"`
	MaxModelCalls      int                       `json:"max_model_calls"`
	MaxOutputTokens    int                       `json:"max_output_tokens"`

	SourceReusePolicy      string `json:"source_reuse_policy"`
	DeterministicRule      string `json:"deterministic_rule"`
	DeterministicBackendID string `json:"deterministic_backend_id"`
	Digest                 string `json:"digest"`
}

func NewAgentFollowUpSpec(
	id string,
	semantic AgentSemanticView,
	feedback AgentBatchFeedbackView,
	inputs []AgentBatchFeedbackInput,
	sourceSeeds []uint64,
	followUpSeed uint64,
	decisions int,
	followUpBudget MethodBudget,
	maxModelCalls int,
	maxOutputTokens int,
) (AgentFollowUpSpec, error) {
	if !validMethodToken(id) || decisions <= 0 || maxModelCalls != 1 ||
		maxOutputTokens <= 0 || maxOutputTokens > 4096 {
		return AgentFollowUpSpec{}, errors.New("EXPERIMENT_AGENT_FOLLOW_UP_INPUT_INVALID")
	}
	if err := feedback.ValidateInputs(semantic, inputs); err != nil {
		return AgentFollowUpSpec{}, err
	}
	backendBounds := make(map[string]IntentBackendView, len(semantic.EligibleBackends))
	for _, backend := range semantic.EligibleBackends {
		backendBounds[backend.ID] = backend
	}
	for _, backend := range feedback.Backends {
		bound, ok := backendBounds[backend.BackendID]
		if !ok || decisions < bound.MinDecisions || decisions > bound.MaxDecisions {
			return AgentFollowUpSpec{}, errors.New("EXPERIMENT_AGENT_FOLLOW_UP_DECISION_BOUND_INVALID")
		}
	}
	if followUpBudget.validate() != nil || followUpBudget.MaxExecutionAttempts != 1 ||
		followUpBudget.MaxPrimaryWorkUnits < decisions || followUpBudget.MaxReplayWorkUnits < decisions {
		return AgentFollowUpSpec{}, errors.New("EXPERIMENT_AGENT_FOLLOW_UP_BUDGET_INVALID")
	}
	sourceBudget, err := multiplyMethodBudget(feedback.Budget, len(feedback.Backends))
	if err != nil {
		return AgentFollowUpSpec{}, err
	}
	perArmBudget, err := addMethodBudgets(sourceBudget, followUpBudget)
	if err != nil {
		return AgentFollowUpSpec{}, err
	}
	backendIDs := make([]string, 0, len(feedback.Backends))
	observed := AgentFollowUpObservedWork{}
	for _, backend := range feedback.Backends {
		backendIDs = append(backendIDs, backend.BackendID)
		observed.ExecutionAttempts += backend.ExecutionAttempts
		observed.PrimaryWorkUnits += backend.Work.PrimaryWorkUnits
		observed.ReplayWorkUnits += backend.Work.ReplayWorkUnits
		observed.Model.Calls += backend.Work.Model.Calls
		observed.Model.InputTokens += backend.Work.Model.InputTokens
		observed.Model.OutputTokens += backend.Work.Model.OutputTokens
		observed.Model.TotalTokens += backend.Work.Model.TotalTokens
	}
	spec := AgentFollowUpSpec{
		SchemaVersion: AgentFollowUpSpecVersion, ID: id,
		SemanticViewDigest: semantic.Digest, FeedbackDigest: feedback.Digest,
		FeedbackSourceDigest: feedback.SourceDigest, PSSID: feedback.PSSID,
		BackendIDs: backendIDs, SourceSeeds: append([]uint64(nil), sourceSeeds...),
		FollowUpSeed: followUpSeed, Decisions: decisions,
		SourceBudget: sourceBudget, SourceObservedWork: observed,
		FollowUpBudget: followUpBudget, PerArmBudget: perArmBudget,
		MaxModelCalls: maxModelCalls, MaxOutputTokens: maxOutputTokens,
		SourceReusePolicy:      AgentFollowUpReusePolicy,
		DeterministicRule:      AgentFollowUpBaselineRule,
		DeterministicBackendID: selectCompletionFirstBackend(feedback.Backends),
	}
	spec, err = spec.seal()
	if err != nil {
		return AgentFollowUpSpec{}, err
	}
	if err := spec.Validate(); err != nil {
		return AgentFollowUpSpec{}, err
	}
	return spec, nil
}

func (spec AgentFollowUpSpec) Validate() error {
	if spec.SchemaVersion != AgentFollowUpSpecVersion || !validMethodToken(spec.ID) ||
		!validSHA256(spec.SemanticViewDigest) || !validSHA256(spec.FeedbackDigest) ||
		!validSHA256(spec.FeedbackSourceDigest) || spec.PSSID == "" || len(spec.BackendIDs) == 0 ||
		!canonicalTokens(spec.BackendIDs, true) || len(spec.SourceSeeds) == 0 || spec.Decisions <= 0 ||
		spec.MaxModelCalls != 1 || spec.MaxOutputTokens <= 0 || spec.MaxOutputTokens > 4096 ||
		spec.SourceReusePolicy != AgentFollowUpReusePolicy ||
		spec.DeterministicRule != AgentFollowUpBaselineRule ||
		!validMethodToken(spec.DeterministicBackendID) {
		return errors.New("EXPERIMENT_AGENT_FOLLOW_UP_INVALID")
	}
	if err := spec.SourceBudget.validate(); err != nil {
		return err
	}
	if err := spec.FollowUpBudget.validate(); err != nil {
		return err
	}
	if err := spec.PerArmBudget.validate(); err != nil {
		return err
	}
	if spec.FollowUpBudget.MaxExecutionAttempts != 1 ||
		len(spec.SourceSeeds)*len(spec.BackendIDs) != spec.SourceBudget.MaxExecutionAttempts ||
		!strictlyIncreasingSeeds(spec.SourceSeeds) || seedPresent(spec.SourceSeeds, spec.FollowUpSeed) ||
		spec.SourceObservedWork.ExecutionAttempts != spec.SourceBudget.MaxExecutionAttempts ||
		spec.SourceObservedWork.PrimaryWorkUnits < 0 ||
		spec.SourceObservedWork.PrimaryWorkUnits > spec.SourceBudget.MaxPrimaryWorkUnits ||
		spec.SourceObservedWork.ReplayWorkUnits < 0 ||
		spec.SourceObservedWork.ReplayWorkUnits > spec.SourceBudget.MaxReplayWorkUnits ||
		spec.SourceObservedWork.Model.Calls < 0 ||
		spec.SourceObservedWork.Model.InputTokens < 0 ||
		spec.SourceObservedWork.Model.OutputTokens < 0 ||
		spec.SourceObservedWork.Model.TotalTokens !=
			spec.SourceObservedWork.Model.InputTokens+spec.SourceObservedWork.Model.OutputTokens {
		return errors.New("EXPERIMENT_AGENT_FOLLOW_UP_SOURCE_INVALID")
	}
	wantArm, err := addMethodBudgets(spec.SourceBudget, spec.FollowUpBudget)
	if err != nil || wantArm != spec.PerArmBudget {
		return errors.New("EXPERIMENT_AGENT_FOLLOW_UP_BUDGET_INVALID")
	}
	if !tokenPresent(spec.BackendIDs, spec.DeterministicBackendID) {
		return errors.New("EXPERIMENT_AGENT_FOLLOW_UP_BASELINE_INVALID")
	}
	sealed, err := spec.seal()
	if err != nil || !validSHA256(spec.Digest) || sealed.Digest != spec.Digest {
		return errors.New("EXPERIMENT_AGENT_FOLLOW_UP_DIGEST_MISMATCH")
	}
	return nil
}

func (spec AgentFollowUpSpec) ValidateInputs(
	semantic AgentSemanticView,
	feedback AgentBatchFeedbackView,
	inputs []AgentBatchFeedbackInput,
) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	want, err := NewAgentFollowUpSpec(
		spec.ID, semantic, feedback, inputs, spec.SourceSeeds, spec.FollowUpSeed,
		spec.Decisions, spec.FollowUpBudget, spec.MaxModelCalls, spec.MaxOutputTokens,
	)
	if err != nil {
		return err
	}
	if want.Digest != spec.Digest {
		return errors.New("EXPERIMENT_AGENT_FOLLOW_UP_INPUT_MISMATCH")
	}
	return nil
}

func (spec AgentFollowUpSpec) ValidatePlan(intent GuardedTestIntent, plan CompiledIntentPlan) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := intent.Validate(); err != nil {
		return err
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if intent.ViewDigest != spec.SemanticViewDigest || plan.ViewDigest != spec.SemanticViewDigest ||
		plan.IntentDigest != intent.Digest || plan.Decisions != spec.Decisions ||
		!tokenPresent(spec.BackendIDs, plan.BackendID) {
		return errors.New("EXPERIMENT_AGENT_FOLLOW_UP_PLAN_MISMATCH")
	}
	return nil
}

func (spec AgentFollowUpSpec) seal() (AgentFollowUpSpec, error) {
	spec.BackendIDs = append([]string(nil), spec.BackendIDs...)
	sort.Strings(spec.BackendIDs)
	spec.SourceSeeds = append([]uint64(nil), spec.SourceSeeds...)
	spec.Digest = ""
	digest, err := portableJSONDigest(spec)
	if err != nil {
		return AgentFollowUpSpec{}, err
	}
	spec.Digest = digest
	return spec, nil
}

func selectCompletionFirstBackend(backends []AgentBackendFeedback) string {
	sorted := append([]AgentBackendFeedback(nil), backends...)
	statusRank := func(status string) int {
		switch status {
		case AgentFeedbackComplete:
			return 0
		case AgentFeedbackPartial:
			return 1
		default:
			return 2
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		left, right := sorted[i], sorted[j]
		if left.WorkloadPlanned > 0 && right.WorkloadPlanned > 0 {
			leftCompleted := left.WorkloadCompleted * right.WorkloadPlanned
			rightCompleted := right.WorkloadCompleted * left.WorkloadPlanned
			if leftCompleted != rightCompleted {
				return leftCompleted > rightCompleted
			}
		}
		if statusRank(left.Status) != statusRank(right.Status) {
			return statusRank(left.Status) < statusRank(right.Status)
		}
		if left.CompletedAttempts != right.CompletedAttempts {
			return left.CompletedAttempts > right.CompletedAttempts
		}
		if left.FailedAttempts+left.RejectedProposals != right.FailedAttempts+right.RejectedProposals {
			return left.FailedAttempts+left.RejectedProposals < right.FailedAttempts+right.RejectedProposals
		}
		return left.BackendID < right.BackendID
	})
	if len(sorted) == 0 {
		return ""
	}
	return sorted[0].BackendID
}

func multiplyMethodBudget(budget MethodBudget, factor int) (MethodBudget, error) {
	if budget.validate() != nil || factor <= 0 {
		return MethodBudget{}, errors.New("EXPERIMENT_AGENT_FOLLOW_UP_BUDGET_INVALID")
	}
	maxInt := int(^uint(0) >> 1)
	if budget.MaxExecutionAttempts > maxInt/factor ||
		budget.MaxPrimaryWorkUnits > maxInt/factor || budget.MaxReplayWorkUnits > maxInt/factor {
		return MethodBudget{}, errors.New("EXPERIMENT_AGENT_FOLLOW_UP_BUDGET_INVALID")
	}
	return MethodBudget{
		MaxExecutionAttempts: budget.MaxExecutionAttempts * factor,
		MaxPrimaryWorkUnits:  budget.MaxPrimaryWorkUnits * factor,
		MaxReplayWorkUnits:   budget.MaxReplayWorkUnits * factor,
	}, nil
}

func addMethodBudgets(left, right MethodBudget) (MethodBudget, error) {
	if left.validate() != nil || right.validate() != nil {
		return MethodBudget{}, errors.New("EXPERIMENT_AGENT_FOLLOW_UP_BUDGET_INVALID")
	}
	maxInt := int(^uint(0) >> 1)
	if left.MaxExecutionAttempts > maxInt-right.MaxExecutionAttempts ||
		left.MaxPrimaryWorkUnits > maxInt-right.MaxPrimaryWorkUnits ||
		left.MaxReplayWorkUnits > maxInt-right.MaxReplayWorkUnits {
		return MethodBudget{}, errors.New("EXPERIMENT_AGENT_FOLLOW_UP_BUDGET_INVALID")
	}
	return MethodBudget{
		MaxExecutionAttempts: left.MaxExecutionAttempts + right.MaxExecutionAttempts,
		MaxPrimaryWorkUnits:  left.MaxPrimaryWorkUnits + right.MaxPrimaryWorkUnits,
		MaxReplayWorkUnits:   left.MaxReplayWorkUnits + right.MaxReplayWorkUnits,
	}, nil
}

func strictlyIncreasingSeeds(seeds []uint64) bool {
	for index := 1; index < len(seeds); index++ {
		if seeds[index-1] >= seeds[index] {
			return false
		}
	}
	return true
}

func seedPresent(seeds []uint64, seed uint64) bool {
	for _, current := range seeds {
		if current == seed {
			return true
		}
	}
	return false
}

func tokenPresent(tokens []string, token string) bool {
	for _, current := range tokens {
		if current == token {
			return true
		}
	}
	return false
}

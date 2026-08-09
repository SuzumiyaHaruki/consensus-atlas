package controlexperiment

import (
	"errors"
	"slices"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const (
	AgentBatchFeedbackViewVersion   = "consensus-atlas/agent-batch-feedback-view/v1"
	AgentBatchFeedbackViewVersionV2 = "consensus-atlas/agent-batch-feedback-view/v2"
	AgentFeedbackInterpretation     = "coarse-discovery-without-completeness-denominator"

	AgentFeedbackComplete = "complete"
	AgentFeedbackPartial  = "partial"
	AgentFeedbackFailed   = "failed"

	AgentFeedbackProposalRejected = "proposal-rejected"
	AgentFeedbackExecutionFailed  = "execution-failed"
)

// AgentBatchFeedbackInput is a trusted composition input, not a serializable
// Agent artifact. Bundles and Mapper are required so the constructor can
// reproject PSS evidence instead of trusting MethodObservation fields alone.
type AgentBatchFeedbackInput struct {
	BackendID   string
	Observation MethodObservation
	Bundles     []ExecutionBundle
	Mapper      psscore.SemanticMapper
}

type AgentFeedbackFailure struct {
	Class string `json:"class"`
	Count int    `json:"count"`
}

type AgentFeedbackWork struct {
	PrimaryWorkUnits int       `json:"primary_work_units"`
	ReplayWorkUnits  int       `json:"replay_work_units"`
	Model            ModelWork `json:"model"`
}

// AgentBackendFeedback exposes only batch-level cost, completion and coarse
// PSS discovery. It intentionally omits bundle/trace/manifest/build identities,
// exact PSS keys, Oracle results and defect labels.
type AgentBackendFeedback struct {
	BackendID         string                 `json:"backend_id"`
	Status            string                 `json:"status"`
	ExecutionAttempts int                    `json:"execution_attempts"`
	CompletedAttempts int                    `json:"completed_attempts"`
	FailedAttempts    int                    `json:"failed_attempts"`
	RejectedProposals int                    `json:"rejected_proposals"`
	ObservedRuns      int                    `json:"observed_runs"`
	PSSSamples        int                    `json:"pss_samples"`
	UniquePSSStates   int                    `json:"unique_pss_states"`
	NewStatesByRun    []int                  `json:"new_states_by_run"`
	WorkloadPlanned   int                    `json:"workload_planned,omitempty"`
	WorkloadCompleted int                    `json:"workload_completed,omitempty"`
	WorkloadPending   int                    `json:"workload_pending,omitempty"`
	Failures          []AgentFeedbackFailure `json:"failures,omitempty"`
	Work              AgentFeedbackWork      `json:"work"`
}

// AgentBatchFeedbackView binds mechanically verified MethodObservations to an
// existing semantic view. SourceDigest identifies trusted inputs without
// exposing their implementation or execution identities to the Agent.
type AgentBatchFeedbackView struct {
	SchemaVersion      string                 `json:"schema_version"`
	ID                 string                 `json:"id"`
	SemanticViewDigest string                 `json:"semantic_view_digest"`
	SourceDigest       string                 `json:"source_digest"`
	PSSID              string                 `json:"pss_id"`
	Budget             MethodBudget           `json:"common_budget"`
	Interpretation     string                 `json:"interpretation"`
	Backends           []AgentBackendFeedback `json:"backends"`
	Digest             string                 `json:"digest"`
}

type agentFeedbackSourceIdentity struct {
	BackendID         string `json:"backend_id"`
	ObservationDigest string `json:"observation_digest"`
}

func NewAgentBatchFeedbackView(
	id string,
	semantic AgentSemanticView,
	inputs []AgentBatchFeedbackInput,
) (AgentBatchFeedbackView, error) {
	return newAgentBatchFeedbackView(AgentBatchFeedbackViewVersion, id, semantic, inputs)
}

func NewAgentBatchFeedbackViewV2(
	id string,
	semantic AgentSemanticView,
	inputs []AgentBatchFeedbackInput,
) (AgentBatchFeedbackView, error) {
	return newAgentBatchFeedbackView(AgentBatchFeedbackViewVersionV2, id, semantic, inputs)
}

func newAgentBatchFeedbackView(
	version string,
	id string,
	semantic AgentSemanticView,
	inputs []AgentBatchFeedbackInput,
) (AgentBatchFeedbackView, error) {
	if !validMethodToken(id) || len(inputs) == 0 {
		return AgentBatchFeedbackView{}, errors.New("EXPERIMENT_AGENT_FEEDBACK_INPUT_INVALID")
	}
	if err := semantic.Validate(); err != nil {
		return AgentBatchFeedbackView{}, err
	}
	eligible := make(map[string]bool, len(semantic.EligibleBackends))
	for _, backend := range semantic.EligibleBackends {
		eligible[backend.ID] = true
	}
	sortedInputs := append([]AgentBatchFeedbackInput(nil), inputs...)
	sort.Slice(sortedInputs, func(i, j int) bool { return sortedInputs[i].BackendID < sortedInputs[j].BackendID })
	view := AgentBatchFeedbackView{
		SchemaVersion: version, ID: id,
		SemanticViewDigest: semantic.Digest, Interpretation: AgentFeedbackInterpretation,
	}
	sources := make([]agentFeedbackSourceIdentity, 0, len(sortedInputs))
	seen := make(map[string]bool, len(sortedInputs))
	for index, input := range sortedInputs {
		if !validMethodToken(input.BackendID) || seen[input.BackendID] || !eligible[input.BackendID] ||
			input.Mapper == nil || input.Mapper.ID() == "" {
			return AgentBatchFeedbackView{}, errors.New("EXPERIMENT_AGENT_FEEDBACK_BACKEND_INVALID")
		}
		seen[input.BackendID] = true
		if err := input.Observation.ValidateBundles(input.Bundles, input.Mapper); err != nil {
			return AgentBatchFeedbackView{}, err
		}
		if index == 0 {
			view.Budget = input.Observation.Budget
			view.PSSID = input.Observation.Measurement.PSSID
		} else if input.Observation.Budget != view.Budget ||
			input.Observation.Measurement.PSSID != view.PSSID {
			return AgentBatchFeedbackView{}, errors.New("EXPERIMENT_AGENT_FEEDBACK_COMPARABILITY_INVALID")
		}
		entry, err := projectAgentBackendFeedback(
			version, input.BackendID, input.Observation, input.Bundles,
		)
		if err != nil {
			return AgentBatchFeedbackView{}, err
		}
		view.Backends = append(view.Backends, entry)
		sources = append(sources, agentFeedbackSourceIdentity{
			BackendID: input.BackendID, ObservationDigest: input.Observation.Digest,
		})
	}
	var err error
	view.SourceDigest, err = portableJSONDigest(sources)
	if err != nil {
		return AgentBatchFeedbackView{}, err
	}
	view, err = view.seal()
	if err != nil {
		return AgentBatchFeedbackView{}, err
	}
	if err := view.Validate(); err != nil {
		return AgentBatchFeedbackView{}, err
	}
	return view, nil
}

func (view AgentBatchFeedbackView) Validate() error {
	if (view.SchemaVersion != AgentBatchFeedbackViewVersion &&
		view.SchemaVersion != AgentBatchFeedbackViewVersionV2) || !validMethodToken(view.ID) ||
		!validSHA256(view.SemanticViewDigest) || !validSHA256(view.SourceDigest) || view.PSSID == "" ||
		view.Budget.validate() != nil || view.Interpretation != AgentFeedbackInterpretation ||
		len(view.Backends) == 0 {
		return errors.New("EXPERIMENT_AGENT_FEEDBACK_INVALID")
	}
	seen := make(map[string]bool, len(view.Backends))
	for index, backend := range view.Backends {
		if err := backend.validate(view.SchemaVersion, view.Budget); err != nil {
			return err
		}
		if seen[backend.BackendID] || (index > 0 && view.Backends[index-1].BackendID >= backend.BackendID) {
			return errors.New("EXPERIMENT_AGENT_FEEDBACK_BACKEND_ORDER_INVALID")
		}
		seen[backend.BackendID] = true
	}
	sealed, err := view.seal()
	if err != nil || !validSHA256(view.Digest) || sealed.Digest != view.Digest {
		return errors.New("EXPERIMENT_AGENT_FEEDBACK_DIGEST_MISMATCH")
	}
	return nil
}

func (view AgentBatchFeedbackView) ValidateInputs(
	semantic AgentSemanticView,
	inputs []AgentBatchFeedbackInput,
) error {
	if err := view.Validate(); err != nil {
		return err
	}
	var want AgentBatchFeedbackView
	var err error
	if view.SchemaVersion == AgentBatchFeedbackViewVersionV2 {
		want, err = NewAgentBatchFeedbackViewV2(view.ID, semantic, inputs)
	} else {
		want, err = NewAgentBatchFeedbackView(view.ID, semantic, inputs)
	}
	if err != nil {
		return err
	}
	if want.Digest != view.Digest {
		return errors.New("EXPERIMENT_AGENT_FEEDBACK_INPUT_MISMATCH")
	}
	return nil
}

// ValidateFeedbackPreferenceAblation fixes the authority of batch feedback.
// A with-feedback proposal may change preferences only; risk selection, hard
// constraints and budget remain byte-for-byte equivalent to the no-feedback
// proposal before both are compiled by the trusted compiler.
func ValidateFeedbackPreferenceAblation(
	semantic AgentSemanticView,
	withoutFeedback GuardedTestIntent,
	withFeedback GuardedTestIntent,
) error {
	if err := semantic.Validate(); err != nil {
		return err
	}
	if err := withoutFeedback.Validate(); err != nil {
		return err
	}
	if err := withFeedback.Validate(); err != nil {
		return err
	}
	if withoutFeedback.ViewDigest != semantic.Digest || withFeedback.ViewDigest != semantic.Digest ||
		withoutFeedback.RiskID != withFeedback.RiskID ||
		withoutFeedback.Must.Decisions != withFeedback.Must.Decisions ||
		withoutFeedback.Must.FaultEnvelope != withFeedback.Must.FaultEnvelope ||
		!slices.Equal(withoutFeedback.Must.RequiredCapabilities, withFeedback.Must.RequiredCapabilities) ||
		!slices.Equal(withoutFeedback.Must.RequiredActions, withFeedback.Must.RequiredActions) {
		return errors.New("EXPERIMENT_AGENT_FEEDBACK_ABLATION_HARD_CONSTRAINT_CHANGED")
	}
	return nil
}

// ValidatePreferenceOnlyProposal checks one model proposal against the
// trusted pre-call baseline. Pairwise arm equality is insufficient: two arms
// could otherwise make the same unauthorized hard-field change.
func ValidatePreferenceOnlyProposal(
	semantic AgentSemanticView,
	baseline GuardedTestIntent,
	proposal GuardedTestIntent,
) error {
	if err := semantic.Validate(); err != nil {
		return err
	}
	if err := baseline.Validate(); err != nil {
		return err
	}
	if err := proposal.Validate(); err != nil {
		return err
	}
	if baseline.ViewDigest != semantic.Digest || proposal.ViewDigest != semantic.Digest ||
		baseline.RiskID != proposal.RiskID || baseline.Must.Decisions != proposal.Must.Decisions ||
		baseline.Must.FaultEnvelope != proposal.Must.FaultEnvelope ||
		!slices.Equal(baseline.Must.RequiredCapabilities, proposal.Must.RequiredCapabilities) ||
		!slices.Equal(baseline.Must.RequiredActions, proposal.Must.RequiredActions) {
		return errors.New("EXPERIMENT_AGENT_PREFERENCE_PROPOSAL_HARD_CONSTRAINT_CHANGED")
	}
	return nil
}

func projectAgentBackendFeedback(
	version string,
	backendID string,
	observation MethodObservation,
	bundles []ExecutionBundle,
) (AgentBackendFeedback, error) {
	entry := AgentBackendFeedback{
		BackendID: backendID, ObservedRuns: len(observation.Measurement.Runs),
		PSSSamples:      observation.Measurement.TotalSamples,
		UniquePSSStates: observation.Measurement.UniqueStates,
		Work: AgentFeedbackWork{
			PrimaryWorkUnits: observation.Ledger.Totals.Primary.WorkUnits,
			ReplayWorkUnits:  observation.Ledger.Totals.Replay.WorkUnits,
			Model:            observation.Ledger.Totals.Model,
		},
	}
	failureCounts := make(map[string]int)
	for _, run := range observation.Measurement.Runs {
		entry.NewStatesByRun = append(entry.NewStatesByRun, run.NewStates)
	}
	if version == AgentBatchFeedbackViewVersionV2 {
		if len(bundles) != entry.ObservedRuns {
			return AgentBackendFeedback{}, errors.New("EXPERIMENT_AGENT_FEEDBACK_WORKLOAD_INPUT_INVALID")
		}
		for _, bundle := range bundles {
			if bundle.Run.Workload == nil || bundle.Run.Workload.Planned <= 0 ||
				bundle.Run.Workload.Completed < 0 || bundle.Run.Workload.Pending < 0 ||
				bundle.Run.Workload.Completed+bundle.Run.Workload.Pending != bundle.Run.Workload.Planned {
				return AgentBackendFeedback{}, errors.New("EXPERIMENT_AGENT_FEEDBACK_WORKLOAD_INPUT_INVALID")
			}
			entry.WorkloadPlanned += bundle.Run.Workload.Planned
			entry.WorkloadCompleted += bundle.Run.Workload.Completed
			entry.WorkloadPending += bundle.Run.Workload.Pending
		}
	}
	for _, record := range observation.Ledger.Records {
		switch record.Kind {
		case MethodRecordSource:
			entry.ExecutionAttempts++
			entry.CompletedAttempts++
		case MethodRecordProposal:
			if record.Outcome == MethodOutcomeRejected {
				entry.RejectedProposals++
				failureCounts[AgentFeedbackProposalRejected]++
			}
		case MethodRecordExecution:
			entry.ExecutionAttempts++
			if record.Outcome == MethodOutcomeCompleted {
				entry.CompletedAttempts++
			} else {
				entry.FailedAttempts++
				failureCounts[AgentFeedbackExecutionFailed]++
			}
		}
	}
	for class, count := range failureCounts {
		entry.Failures = append(entry.Failures, AgentFeedbackFailure{Class: class, Count: count})
	}
	sort.Slice(entry.Failures, func(i, j int) bool { return entry.Failures[i].Class < entry.Failures[j].Class })
	switch {
	case entry.FailedAttempts > 0 || entry.RejectedProposals > 0:
		entry.Status = AgentFeedbackFailed
	case entry.ExecutionAttempts == observation.Budget.MaxExecutionAttempts:
		entry.Status = AgentFeedbackComplete
	default:
		entry.Status = AgentFeedbackPartial
	}
	if err := entry.validate(version, observation.Budget); err != nil {
		return AgentBackendFeedback{}, err
	}
	return entry, nil
}

func (entry AgentBackendFeedback) validate(version string, budget MethodBudget) error {
	if !validMethodToken(entry.BackendID) ||
		(entry.Status != AgentFeedbackComplete && entry.Status != AgentFeedbackPartial &&
			entry.Status != AgentFeedbackFailed) ||
		entry.ExecutionAttempts <= 0 || entry.ExecutionAttempts > budget.MaxExecutionAttempts ||
		entry.CompletedAttempts < 0 || entry.FailedAttempts < 0 ||
		entry.CompletedAttempts+entry.FailedAttempts != entry.ExecutionAttempts ||
		entry.RejectedProposals < 0 || entry.ObservedRuns <= 0 ||
		entry.ObservedRuns != entry.CompletedAttempts ||
		entry.PSSSamples <= 0 || entry.UniquePSSStates <= 0 ||
		len(entry.NewStatesByRun) != entry.ObservedRuns ||
		entry.Work.PrimaryWorkUnits <= 0 || entry.Work.PrimaryWorkUnits > budget.MaxPrimaryWorkUnits ||
		entry.Work.ReplayWorkUnits < 0 || entry.Work.ReplayWorkUnits > budget.MaxReplayWorkUnits ||
		entry.Work.Model.Calls < 0 || entry.Work.Model.InputTokens < 0 ||
		entry.Work.Model.OutputTokens < 0 ||
		entry.Work.Model.TotalTokens != entry.Work.Model.InputTokens+entry.Work.Model.OutputTokens {
		return errors.New("EXPERIMENT_AGENT_FEEDBACK_BACKEND_CONTENT_INVALID")
	}
	if version == AgentBatchFeedbackViewVersionV2 {
		if entry.WorkloadPlanned <= 0 || entry.WorkloadCompleted < 0 || entry.WorkloadPending < 0 ||
			entry.WorkloadCompleted+entry.WorkloadPending != entry.WorkloadPlanned {
			return errors.New("EXPERIMENT_AGENT_FEEDBACK_WORKLOAD_INVALID")
		}
	} else if entry.WorkloadPlanned != 0 || entry.WorkloadCompleted != 0 || entry.WorkloadPending != 0 {
		return errors.New("EXPERIMENT_AGENT_FEEDBACK_WORKLOAD_INVALID")
	}
	newStates := 0
	for _, count := range entry.NewStatesByRun {
		if count < 0 {
			return errors.New("EXPERIMENT_AGENT_FEEDBACK_DISCOVERY_INVALID")
		}
		newStates += count
	}
	if newStates != entry.UniquePSSStates {
		return errors.New("EXPERIMENT_AGENT_FEEDBACK_DISCOVERY_INVALID")
	}
	failures := 0
	for index, failure := range entry.Failures {
		if (failure.Class != AgentFeedbackProposalRejected && failure.Class != AgentFeedbackExecutionFailed) ||
			failure.Count <= 0 || (index > 0 && entry.Failures[index-1].Class >= failure.Class) {
			return errors.New("EXPERIMENT_AGENT_FEEDBACK_FAILURE_INVALID")
		}
		failures += failure.Count
	}
	if failures != entry.RejectedProposals+entry.FailedAttempts ||
		(entry.Status == AgentFeedbackComplete && entry.ExecutionAttempts != budget.MaxExecutionAttempts) ||
		(entry.Status == AgentFeedbackFailed && failures == 0) ||
		(entry.Status == AgentFeedbackPartial &&
			(entry.ExecutionAttempts == budget.MaxExecutionAttempts || failures != 0)) {
		return errors.New("EXPERIMENT_AGENT_FEEDBACK_STATUS_INVALID")
	}
	return nil
}

func (view AgentBatchFeedbackView) seal() (AgentBatchFeedbackView, error) {
	view.Backends = cloneAgentBackendFeedback(view.Backends)
	sort.Slice(view.Backends, func(i, j int) bool { return view.Backends[i].BackendID < view.Backends[j].BackendID })
	view.Digest = ""
	digest, err := portableJSONDigest(view)
	if err != nil {
		return AgentBatchFeedbackView{}, err
	}
	view.Digest = digest
	return view, nil
}

func cloneAgentBackendFeedback(entries []AgentBackendFeedback) []AgentBackendFeedback {
	result := append([]AgentBackendFeedback(nil), entries...)
	for index := range result {
		result[index].NewStatesByRun = append([]int(nil), result[index].NewStatesByRun...)
		result[index].Failures = append([]AgentFeedbackFailure(nil), result[index].Failures...)
	}
	return result
}

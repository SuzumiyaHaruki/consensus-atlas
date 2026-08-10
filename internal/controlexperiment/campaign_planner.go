package controlexperiment

import (
	"errors"
	"sort"
)

const (
	CampaignPlannerViewVersion = "consensus-atlas/campaign-planner-view/v1"
	CampaignPlannerSemantics   = "discovery-counts-have-no-completeness-denominator;monitor-triggers-are-not-verdicts"
)

// CampaignPlannerAttemptFeedback deliberately omits artifact, bundle, trace,
// witness, build and defect identities. It is an attempt-indexed summary, not
// evidence from which a Planner may award a verdict.
type CampaignPlannerAttemptFeedback struct {
	Ordinal           int                        `json:"ordinal"`
	Outcome           string                     `json:"outcome"`
	ExecutionEvidence bool                       `json:"execution_evidence"`
	ChargedDecisions  int                        `json:"charged_decisions,omitempty"`
	PSSSamples        int                        `json:"pss_samples,omitempty"`
	PSSStates         int                        `json:"pss_states,omitempty"`
	NewPSSStates      int                        `json:"new_pss_states,omitempty"`
	MonitorTriggers   int                        `json:"monitor_triggers,omitempty"`
	Choice            *CampaignChoiceObservation `json:"choice,omitempty"`
}

type CampaignPlannerPSSFeedback struct {
	PSSID            string `json:"pss_id"`
	EvidenceAttempts int    `json:"evidence_attempts"`
	TotalDecisions   int    `json:"total_decisions"`
	TotalSamples     int    `json:"total_samples"`
	UniqueStates     int    `json:"unique_states"`
}

type CampaignPlannerMonitorFeedback struct {
	ObservedAttempts      int                    `json:"observed_attempts"`
	Checked               []CampaignMonitorCount `json:"checked"`
	RequirementViolations int                    `json:"requirement_violations"`
	EvidenceInvalid       int                    `json:"evidence_invalid"`
}

type CampaignPlannerFeedback struct {
	Attempts []CampaignPlannerAttemptFeedback `json:"attempts"`
	PSS      *CampaignPlannerPSSFeedback      `json:"pss,omitempty"`
	Faults   CampaignFaultObservation         `json:"faults"`
	Workload CampaignWorkloadObservation      `json:"workload"`
	Monitors CampaignPlannerMonitorFeedback   `json:"monitors"`
}

// CampaignPlannerView is the complete untrusted planning input. The trusted
// coordinator remains responsible for the exact request and hard intent.
type CampaignPlannerView struct {
	SchemaVersion     string                  `json:"schema_version"`
	ID                string                  `json:"id"`
	SemanticView      AgentSemanticView       `json:"semantic_view"`
	Request           CampaignAttemptRequest  `json:"request"`
	Baseline          GuardedTestIntent       `json:"baseline"`
	ObservationDigest string                  `json:"observation_digest"`
	EvidenceSemantics string                  `json:"evidence_semantics"`
	Feedback          CampaignPlannerFeedback `json:"feedback"`
	Digest            string                  `json:"digest"`
}

func NewCampaignPlannerView(
	id string,
	semantic AgentSemanticView,
	observation CampaignObservation,
	request CampaignAttemptRequest,
	baseline GuardedTestIntent,
) (CampaignPlannerView, error) {
	if err := semantic.Validate(); err != nil {
		return CampaignPlannerView{}, err
	}
	if err := observation.Validate(); err != nil {
		return CampaignPlannerView{}, err
	}
	if err := request.Validate(); err != nil {
		return CampaignPlannerView{}, err
	}
	if err := baseline.Validate(); err != nil {
		return CampaignPlannerView{}, err
	}
	if observation.Terminal.Status != CampaignSummaryStatusRunning ||
		observation.CampaignID != request.CampaignID || observation.ConfigDigest != request.ConfigDigest ||
		observation.TargetID != request.TargetID ||
		observation.TargetIdentityDigest != request.TargetIdentityDigest ||
		observation.ExperimentSpecDigest != request.ExperimentSpecDigest ||
		observation.HeadCheckpointDigest != request.PreviousDigest ||
		request.Ordinal != observation.Terminal.Attempts+1 || baseline.ViewDigest != semantic.Digest ||
		baseline.Must.Decisions > request.Allowance.PrimarySchedulerDecisions {
		return CampaignPlannerView{}, errors.New("EXPERIMENT_CAMPAIGN_PLANNER_PREFIX_MISMATCH")
	}
	view := CampaignPlannerView{
		SchemaVersion: CampaignPlannerViewVersion, ID: id,
		SemanticView: semantic, Request: request, Baseline: baseline, ObservationDigest: observation.Digest,
		EvidenceSemantics: CampaignPlannerSemantics,
		Feedback:          projectCampaignPlannerFeedback(observation),
	}
	sealed, err := view.seal()
	if err != nil {
		return CampaignPlannerView{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CampaignPlannerView{}, err
	}
	return sealed, nil
}

func (view CampaignPlannerView) Validate() error {
	if view.SchemaVersion != CampaignPlannerViewVersion || !validMethodToken(view.ID) ||
		!validSHA256(view.ObservationDigest) || view.EvidenceSemantics != CampaignPlannerSemantics {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_VIEW_INVALID")
	}
	if err := view.SemanticView.Validate(); err != nil {
		return err
	}
	if err := view.Request.Validate(); err != nil {
		return err
	}
	if err := view.Baseline.Validate(); err != nil {
		return err
	}
	if view.Baseline.ViewDigest != view.SemanticView.Digest ||
		view.Baseline.Must.Decisions > view.Request.Allowance.PrimarySchedulerDecisions {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_BASELINE_INVALID")
	}
	if err := validateCampaignPlannerFeedback(view.Feedback, view.Request.Ordinal-1); err != nil {
		return err
	}
	if err := validateCampaignPlannerChoices(view.SemanticView, view.Baseline, view.Feedback.Attempts); err != nil {
		return err
	}
	sealed, err := view.seal()
	if err != nil || !validSHA256(view.Digest) || sealed.Digest != view.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_VIEW_DIGEST_MISMATCH")
	}
	return nil
}

func validateCampaignPlannerChoices(
	semantic AgentSemanticView,
	baseline GuardedTestIntent,
	attempts []CampaignPlannerAttemptFeedback,
) error {
	risk, ok := findProtocolRisk(semantic.KnowledgePack.Risks, baseline.RiskID)
	if !ok {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_RISK_UNKNOWN")
	}
	allowed := stringSet(risk.AllowedBackendIDs)
	eligible := make(map[string]bool, len(semantic.EligibleBackends))
	for _, backend := range semantic.EligibleBackends {
		eligible[backend.ID] = true
	}
	for _, attempt := range attempts {
		if attempt.Choice == nil || !allowed[attempt.Choice.BackendID] || !eligible[attempt.Choice.BackendID] {
			return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_CHOICE_BACKEND_INVALID")
		}
	}
	return nil
}

func (view CampaignPlannerView) ValidateInputs(
	semantic AgentSemanticView,
	observation CampaignObservation,
	request CampaignAttemptRequest,
	baseline GuardedTestIntent,
) error {
	want, err := NewCampaignPlannerView(view.ID, semantic, observation, request, baseline)
	if err != nil {
		return err
	}
	if want.Digest != view.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_VIEW_INPUT_MISMATCH")
	}
	return nil
}

// PlanDeterministicCampaignFixture is a zero-model plumbing fixture, not a
// search-quality claim. It rotates to the next allowed backend only after the
// latest execution contributes no new PSS state.
func PlanDeterministicCampaignFixture(
	view CampaignPlannerView,
) (GuardedTestIntent, error) {
	if err := view.Validate(); err != nil {
		return GuardedTestIntent{}, err
	}
	baseline := view.Baseline
	risk, ok := findProtocolRisk(view.SemanticView.KnowledgePack.Risks, baseline.RiskID)
	if !ok {
		return GuardedTestIntent{}, errors.New("EXPERIMENT_CAMPAIGN_PLANNER_RISK_UNKNOWN")
	}
	eligible := make(map[string]bool, len(view.SemanticView.EligibleBackends))
	for _, backend := range view.SemanticView.EligibleBackends {
		eligible[backend.ID] = true
	}
	backends := make([]string, 0, len(risk.AllowedBackendIDs))
	for _, backend := range risk.AllowedBackendIDs {
		if eligible[backend] {
			backends = append(backends, backend)
		}
	}
	if len(backends) == 0 {
		return GuardedTestIntent{}, errors.New("EXPERIMENT_CAMPAIGN_PLANNER_NO_BACKEND")
	}
	if attempts := view.Feedback.Attempts; len(attempts) > 0 {
		latest := attempts[len(attempts)-1]
		selected := 0
		for index, backend := range backends {
			if backend == latest.Choice.BackendID {
				selected = index
				break
			}
		}
		if latest.ExecutionEvidence && latest.NewPSSStates == 0 && len(backends) > 1 {
			selected = (selected + 1) % len(backends)
		}
		backends = append(backends[selected:], backends[:selected]...)
	}
	proposal, err := NewGuardedTestIntent(GuardedTestIntent{
		ID: baseline.ID, ViewDigest: baseline.ViewDigest, RiskID: baseline.RiskID,
		Must: baseline.Must, Prefer: IntentPrefer{BackendIDs: backends},
	})
	if err != nil {
		return GuardedTestIntent{}, err
	}
	if err := ValidateCampaignPlannerProposal(view, proposal); err != nil {
		return GuardedTestIntent{}, err
	}
	return proposal, nil
}

// ValidateCampaignPlannerProposal narrows the older preference-only boundary:
// even descriptive identity is frozen, so Prefer is the sole mutable field.
func ValidateCampaignPlannerProposal(view CampaignPlannerView, proposal GuardedTestIntent) error {
	if err := view.Validate(); err != nil {
		return err
	}
	if proposal.ID != view.Baseline.ID {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_PROPOSAL_ID_CHANGED")
	}
	return ValidatePreferenceOnlyProposal(view.SemanticView, view.Baseline, proposal)
}

func projectCampaignPlannerFeedback(observation CampaignObservation) CampaignPlannerFeedback {
	feedback := CampaignPlannerFeedback{
		Faults: observation.Faults, Workload: observation.Workload,
		Monitors: CampaignPlannerMonitorFeedback{
			ObservedAttempts: observation.Monitors.ObservedAttempts,
			Checked:          append([]CampaignMonitorCount(nil), observation.Monitors.Checked...),
		},
	}
	feedback.Workload.ResultStatuses = append([]CampaignStatusCount(nil), observation.Workload.ResultStatuses...)
	for _, attempt := range observation.Attempts {
		projected := CampaignPlannerAttemptFeedback{
			Ordinal: attempt.Ordinal, Outcome: attempt.Outcome,
			ExecutionEvidence: attempt.ExecutionEvidence, ChargedDecisions: attempt.ChargedDecisions,
			PSSSamples: attempt.PSSSamples, PSSStates: attempt.PSSStates,
			NewPSSStates: attempt.NewPSSStates, MonitorTriggers: attempt.MonitorTriggers,
		}
		if attempt.Choice != nil {
			choice := *attempt.Choice
			projected.Choice = &choice
		}
		feedback.Attempts = append(feedback.Attempts, projected)
	}
	if observation.PSS != nil {
		feedback.PSS = &CampaignPlannerPSSFeedback{
			PSSID: observation.PSS.PSSID, EvidenceAttempts: observation.PSS.EvidenceAttempts,
			TotalDecisions: observation.PSS.TotalDecisions, TotalSamples: observation.PSS.TotalSamples,
			UniqueStates: observation.PSS.UniqueStates,
		}
	}
	for _, trigger := range observation.Monitors.Triggers {
		if trigger.Class == CampaignMonitorRequirement {
			feedback.Monitors.RequirementViolations++
		} else {
			feedback.Monitors.EvidenceInvalid++
		}
	}
	return feedback
}

func validateCampaignPlannerFeedback(feedback CampaignPlannerFeedback, attempts int) error {
	if len(feedback.Attempts) != attempts || feedback.Faults.ObservedAttempts < 0 ||
		!validCampaignFaultUsage(feedback.Faults.Usage) ||
		!validCampaignPlannerWorkload(feedback.Workload, attempts) ||
		feedback.Monitors.ObservedAttempts < 0 || feedback.Monitors.RequirementViolations < 0 ||
		feedback.Monitors.EvidenceInvalid < 0 {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_FEEDBACK_INVALID")
	}
	evidence, decisions, samples, states, newStates, triggers := 0, 0, 0, 0, 0, 0
	for index, attempt := range feedback.Attempts {
		if attempt.Ordinal != index+1 || attempt.MonitorTriggers < 0 ||
			(attempt.Outcome != CampaignAttemptCompleted && attempt.Outcome != CampaignAttemptRejected &&
				attempt.Outcome != CampaignAttemptFailed && attempt.Outcome != CampaignAttemptInvalid) {
			return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_ATTEMPT_INVALID")
		}
		if attempt.Choice == nil || attempt.Choice.validate() != nil {
			return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_ATTEMPT_CHOICE_INVALID")
		}
		if !attempt.ExecutionEvidence {
			if attempt.ChargedDecisions != 0 || attempt.PSSSamples != 0 || attempt.PSSStates != 0 ||
				attempt.NewPSSStates != 0 || attempt.MonitorTriggers != 0 {
				return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_ATTEMPT_INVALID")
			}
			continue
		}
		if attempt.ChargedDecisions < 0 || attempt.PSSSamples != attempt.ChargedDecisions+1 ||
			attempt.PSSStates <= 0 || attempt.PSSStates > attempt.PSSSamples || attempt.NewPSSStates < 0 ||
			attempt.NewPSSStates > attempt.PSSStates {
			return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_ATTEMPT_INVALID")
		}
		evidence++
		decisions += attempt.ChargedDecisions
		samples += attempt.PSSSamples
		states += attempt.PSSStates
		newStates += attempt.NewPSSStates
		triggers += attempt.MonitorTriggers
	}
	if feedback.Faults.ObservedAttempts != evidence || feedback.Monitors.ObservedAttempts != evidence ||
		feedback.Monitors.RequirementViolations+feedback.Monitors.EvidenceInvalid != triggers ||
		!validPlannerMonitorCounts(feedback.Monitors.Checked, evidence) {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_FEEDBACK_INVALID")
	}
	if feedback.PSS == nil {
		if evidence != 0 {
			return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_PSS_INVALID")
		}
	} else if feedback.PSS.PSSID == "" || feedback.PSS.EvidenceAttempts != evidence ||
		feedback.PSS.TotalDecisions != decisions || feedback.PSS.TotalSamples != samples ||
		feedback.PSS.UniqueStates != newStates || feedback.PSS.UniqueStates <= 0 || states < newStates {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNER_PSS_INVALID")
	}
	return nil
}

func validCampaignPlannerWorkload(workload CampaignWorkloadObservation, attempts int) bool {
	if workload.ObservedAttempts < 0 || workload.ObservedAttempts > attempts || workload.Planned < 0 ||
		workload.Offered < 0 || workload.Completed < 0 || workload.Pending < 0 ||
		workload.Offered > workload.Planned || workload.Completed > workload.Offered ||
		workload.Pending != workload.Planned-workload.Completed {
		return false
	}
	total, last := 0, ""
	for _, status := range workload.ResultStatuses {
		if status.Status == "" || status.Count <= 0 || (last != "" && last >= status.Status) {
			return false
		}
		last = status.Status
		total += status.Count
	}
	return total == workload.Completed
}

func validPlannerMonitorCounts(counts []CampaignMonitorCount, attempts int) bool {
	for index, count := range counts {
		if count.Monitor == "" || count.Count <= 0 || count.Count > attempts ||
			(index > 0 && counts[index-1].Monitor >= count.Monitor) {
			return false
		}
	}
	return true
}

func (view CampaignPlannerView) seal() (CampaignPlannerView, error) {
	semantic, err := view.SemanticView.seal()
	if err != nil {
		return CampaignPlannerView{}, err
	}
	view.SemanticView = semantic
	view.Feedback.Attempts = append([]CampaignPlannerAttemptFeedback(nil), view.Feedback.Attempts...)
	for index := range view.Feedback.Attempts {
		if view.Feedback.Attempts[index].Choice != nil {
			choice := *view.Feedback.Attempts[index].Choice
			view.Feedback.Attempts[index].Choice = &choice
		}
	}
	view.Feedback.Monitors.Checked = append([]CampaignMonitorCount(nil), view.Feedback.Monitors.Checked...)
	sort.Slice(view.Feedback.Monitors.Checked, func(i, j int) bool {
		return view.Feedback.Monitors.Checked[i].Monitor < view.Feedback.Monitors.Checked[j].Monitor
	})
	view.Feedback.Workload.ResultStatuses = append(
		[]CampaignStatusCount(nil), view.Feedback.Workload.ResultStatuses...,
	)
	if view.Feedback.PSS != nil {
		copyPSS := *view.Feedback.PSS
		view.Feedback.PSS = &copyPSS
	}
	view.Digest = ""
	digest, err := portableJSONDigest(view)
	if err != nil {
		return CampaignPlannerView{}, err
	}
	view.Digest = digest
	return view, nil
}

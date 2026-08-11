package controlexperiment

import "errors"

const CampaignPlannedAttemptVersion = "consensus-atlas/campaign-planned-attempt/v1"

// CampaignPlannedAttempt is the durable boundary between macro planning and
// target execution. It contains no target trace or Oracle result.
type CampaignPlannedAttempt struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	RequestDigest string `json:"request_digest"`

	View         CampaignPlannerView     `json:"planner_view"`
	Proposal     GuardedTestIntent       `json:"proposal"`
	Plan         CompiledIntentPlanV2    `json:"plan"`
	Instance     IntentExecutionInstance `json:"instance"`
	Choice       CampaignExecutionChoice `json:"choice"`
	PlanningWork ModelWork               `json:"planning_work"`
	Digest       string                  `json:"digest"`
}

func NewCampaignPlannedAttempt(
	id string,
	view CampaignPlannerView,
	proposal GuardedTestIntent,
	plan CompiledIntentPlanV2,
	instance IntentExecutionInstance,
	choice CampaignExecutionChoice,
	planningWork ModelWork,
) (CampaignPlannedAttempt, error) {
	planned := CampaignPlannedAttempt{
		SchemaVersion: CampaignPlannedAttemptVersion, ID: id,
		RequestDigest: view.Request.Digest, View: view, Proposal: proposal,
		Plan: plan, Instance: instance, Choice: choice, PlanningWork: planningWork,
	}
	sealed, err := planned.seal()
	if err != nil {
		return CampaignPlannedAttempt{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CampaignPlannedAttempt{}, err
	}
	return sealed, nil
}

func (planned CampaignPlannedAttempt) Validate() error {
	if planned.SchemaVersion != CampaignPlannedAttemptVersion || !validMethodToken(planned.ID) ||
		!validSHA256(planned.RequestDigest) || planned.RequestDigest != planned.View.Request.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNED_ATTEMPT_INVALID")
	}
	if err := planned.View.Validate(); err != nil {
		return err
	}
	if err := ValidateCampaignPlannerProposal(planned.View, planned.Proposal); err != nil {
		return err
	}
	if err := planned.Plan.Validate(); err != nil {
		return err
	}
	if planned.Plan.IntentDigest != planned.Proposal.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNED_ATTEMPT_PLAN_MISMATCH")
	}
	if err := planned.Instance.ValidatePlan(planned.Plan); err != nil {
		return err
	}
	if err := planned.Choice.ValidateInputs(planned.Proposal, planned.Plan, planned.Instance); err != nil {
		return err
	}
	if !validCampaignPlanningWork(planned.PlanningWork) ||
		planned.PlanningWork.Calls > planned.View.Request.Allowance.ModelCalls ||
		planned.PlanningWork.TotalTokens > planned.View.Request.Allowance.ModelTokens ||
		planned.Instance.Budget.MaxPrimaryWorkUnits > planned.View.Request.Allowance.PrimaryWorkUnits ||
		planned.Instance.Budget.MaxReplayWorkUnits > planned.View.Request.Allowance.ReplayWorkUnits {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNED_ATTEMPT_ALLOWANCE_EXCEEDED")
	}
	sealed, err := planned.seal()
	if err != nil || !validSHA256(planned.Digest) || sealed.Digest != planned.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNED_ATTEMPT_DIGEST_MISMATCH")
	}
	return nil
}

func (planned CampaignPlannedAttempt) ValidateRequest(request CampaignAttemptRequest) error {
	if err := planned.Validate(); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if planned.RequestDigest != request.Digest || planned.View.Request.Digest != request.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_PLANNED_ATTEMPT_REQUEST_MISMATCH")
	}
	return nil
}

func (planned CampaignPlannedAttempt) AttachPlanningWork(execution WorkLedger) (WorkLedger, error) {
	if err := planned.Validate(); err != nil {
		return WorkLedger{}, err
	}
	if err := validateMethodWork(execution); err != nil {
		return WorkLedger{}, err
	}
	if execution.Model != (ModelWork{}) {
		return WorkLedger{}, errors.New("EXPERIMENT_CAMPAIGN_EXECUTION_MODEL_WORK_UNEXPECTED")
	}
	combined := AgentInvocationWork(execution, planned.PlanningWork)
	if err := validateMethodWork(combined); err != nil {
		return WorkLedger{}, err
	}
	return combined, nil
}

func validCampaignPlanningWork(work ModelWork) bool {
	return work.Calls >= 0 && work.InputTokens >= 0 && work.OutputTokens >= 0 &&
		work.TotalTokens == work.InputTokens+work.OutputTokens &&
		((work.Calls == 0 && work.TotalTokens == 0) || (work.Calls > 0 && work.TotalTokens > 0))
}

func (planned CampaignPlannedAttempt) seal() (CampaignPlannedAttempt, error) {
	planned.Digest = ""
	digest, err := portableJSONDigest(planned)
	if err != nil {
		return CampaignPlannedAttempt{}, err
	}
	planned.Digest = digest
	return planned, nil
}

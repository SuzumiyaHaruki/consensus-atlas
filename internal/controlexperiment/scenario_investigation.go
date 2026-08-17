package controlexperiment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const (
	ScenarioIntentContinue = "continue"
	ScenarioIntentRevise   = "revise"
	ScenarioIntentBranch   = "branch"
	ScenarioIntentControl  = "control"
	ScenarioIntentAblate   = "ablate"
	ScenarioIntentSelect   = "select"
	ScenarioIntentAbandon  = "abandon"
	ScenarioIntentMinimize = "minimize"

	ScenarioReasonBranchUnknown          = "branch-unknown"
	ScenarioReasonBranchDuplicate        = "branch-duplicate"
	ScenarioReasonRevisionUnavailable    = "revision-unavailable"
	ScenarioReasonAblationInvalid        = "ablation-invalid"
	ScenarioReasonTrustedFindingRequired = "trusted-finding-required"

	ScenarioProposalIssueJSONInvalid        = "proposal-json-invalid"
	ScenarioProposalIssueIntentInvalid      = "intent-invalid"
	ScenarioProposalIssueIdentifierInvalid  = "identifier-invalid"
	ScenarioProposalIssuePlanInvalid        = "plan-invalid"
	ScenarioProposalIssueZeroActionInvalid  = "zero-action-fields-invalid"
	ScenarioProposalIssueContinueBranch     = "continue-forbids-branch-id"
	ScenarioProposalIssueReviseBranch       = "revise-forbids-branch-fields"
	ScenarioProposalIssueBranchFields       = "branch-fields-invalid"
	ScenarioProposalIssueControlFields      = "control-fields-invalid"
	ScenarioProposalIssueAblationFields     = "ablation-fields-invalid"
	ScenarioProposalIssueMinimizeFields     = "minimize-fields-invalid"
	ScenarioProposalIssueLaterExactActionID = "later-step-action-id-forbidden"
	ScenarioProposalIssueIntentUnavailable  = "intent-not-currently-available"
)

// ScenarioProposalValidationIssue is bounded mechanical repair feedback. It
// describes why an untrusted proposal was rejected without selecting an
// Action, changing the current frontier, or interpreting protocol behavior.
type ScenarioProposalValidationIssue struct {
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
}

func (issue ScenarioProposalValidationIssue) Validate() error {
	switch issue.Code {
	case ScenarioProposalIssueJSONInvalid,
		ScenarioProposalIssueIntentInvalid,
		ScenarioProposalIssueIdentifierInvalid,
		ScenarioProposalIssuePlanInvalid,
		ScenarioProposalIssueZeroActionInvalid,
		ScenarioProposalIssueContinueBranch,
		ScenarioProposalIssueReviseBranch,
		ScenarioProposalIssueBranchFields,
		ScenarioProposalIssueControlFields,
		ScenarioProposalIssueAblationFields,
		ScenarioProposalIssueMinimizeFields,
		ScenarioProposalIssueLaterExactActionID,
		ScenarioProposalIssueIntentUnavailable:
		if issue.Field == "" || !validScenarioProposalField(issue.Field) {
			return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_ISSUE_INVALID")
		}
		return nil
	default:
		return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_ISSUE_INVALID")
	}
}

func validScenarioProposalField(field string) bool {
	switch field {
	case "proposal", "intent", "branch_id", "from_branch_id", "reference_branch_id",
		"omitted_step_ids", "plan", "plan.steps[].selector.action_id":
		return true
	default:
		return false
	}
}

// ScenarioInvestigationProposal is the untrusted Agent-level instruction
// around one ordinary ScenarioPlan. Branch identifiers select coordinator-
// owned deterministic checkpoints; they never identify Runtime Actions.
type ScenarioInvestigationProposal struct {
	Intent            string       `json:"intent"`
	BranchID          string       `json:"branch_id,omitempty"`
	FromBranchID      string       `json:"from_branch_id,omitempty"`
	ReferenceBranchID string       `json:"reference_branch_id,omitempty"`
	OmittedStepIDs    []string     `json:"omitted_step_ids,omitempty"`
	Plan              ScenarioPlan `json:"plan"`
}

func ParseScenarioInvestigationProposal(data []byte) (ScenarioInvestigationProposal, error) {
	proposal, issue := InspectScenarioInvestigationProposal(data)
	if issue != nil {
		return ScenarioInvestigationProposal{}, errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
	}
	return proposal, nil
}

// InspectScenarioInvestigationProposal preserves a successfully decoded,
// bounded proposal when its conditional contract is invalid. Callers can feed
// the issue and the known proposal fields back to an Agent; malformed or
// unknown-field JSON is never retained as a proposal.
func InspectScenarioInvestigationProposal(
	data []byte,
) (ScenarioInvestigationProposal, *ScenarioProposalValidationIssue) {
	if len(data) == 0 || len(data) > ScenarioPlanMaxBytes {
		return invalidScenarioProposal(ScenarioProposalIssueJSONInvalid, "proposal")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var proposal ScenarioInvestigationProposal
	if err := decoder.Decode(&proposal); err != nil {
		return invalidScenarioProposal(ScenarioProposalIssueJSONInvalid, "proposal")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return invalidScenarioProposal(ScenarioProposalIssueJSONInvalid, "proposal")
	}
	if issue := proposal.validationIssue(); issue != nil {
		return proposal, issue
	}
	return proposal, nil
}

func invalidScenarioProposal(
	code string,
	field string,
) (ScenarioInvestigationProposal, *ScenarioProposalValidationIssue) {
	issue := &ScenarioProposalValidationIssue{Code: code, Field: field}
	return ScenarioInvestigationProposal{}, issue
}

func (proposal ScenarioInvestigationProposal) Validate() error {
	if proposal.validationIssue() != nil {
		return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
	}
	return nil
}

func (proposal ScenarioInvestigationProposal) validationIssue() *ScenarioProposalValidationIssue {
	issue := func(code string, field string) *ScenarioProposalValidationIssue {
		return &ScenarioProposalValidationIssue{Code: code, Field: field}
	}
	if !validScenarioIntent(proposal.Intent) {
		return issue(ScenarioProposalIssueIntentInvalid, "intent")
	}
	if proposal.BranchID != "" && !validMethodToken(proposal.BranchID) {
		return issue(ScenarioProposalIssueIdentifierInvalid, "branch_id")
	}
	if proposal.FromBranchID != "" && !validMethodToken(proposal.FromBranchID) {
		return issue(ScenarioProposalIssueIdentifierInvalid, "from_branch_id")
	}
	if proposal.ReferenceBranchID != "" && !validMethodToken(proposal.ReferenceBranchID) {
		return issue(ScenarioProposalIssueIdentifierInvalid, "reference_branch_id")
	}
	if proposal.Intent == ScenarioIntentSelect || proposal.Intent == ScenarioIntentAbandon {
		if proposal.BranchID != "" ||
			(proposal.Intent == ScenarioIntentSelect) != (proposal.FromBranchID != "") ||
			proposal.ReferenceBranchID != "" || len(proposal.OmittedStepIDs) != 0 ||
			proposal.Plan.ID != "" || len(proposal.Plan.Steps) != 0 {
			return issue(ScenarioProposalIssueZeroActionInvalid, "proposal")
		}
		return nil
	}
	if proposal.Plan.Validate() != nil {
		return issue(ScenarioProposalIssuePlanInvalid, "plan")
	}
	seen := make(map[string]bool, len(proposal.OmittedStepIDs))
	for _, id := range proposal.OmittedStepIDs {
		if !validMethodToken(id) || seen[id] {
			return issue(ScenarioProposalIssueIdentifierInvalid, "omitted_step_ids")
		}
		seen[id] = true
	}
	switch proposal.Intent {
	case ScenarioIntentContinue:
		if proposal.BranchID != "" {
			return issue(ScenarioProposalIssueContinueBranch, "branch_id")
		}
		if proposal.ReferenceBranchID != "" ||
			len(proposal.OmittedStepIDs) != 0 {
			return issue(ScenarioProposalIssueBranchFields, "reference_branch_id")
		}
	case ScenarioIntentRevise:
		if proposal.BranchID != "" || proposal.FromBranchID != "" ||
			proposal.ReferenceBranchID != "" || len(proposal.OmittedStepIDs) != 0 {
			return issue(ScenarioProposalIssueReviseBranch, "branch_id")
		}
	case ScenarioIntentBranch:
		if proposal.BranchID == "" || proposal.ReferenceBranchID != "" ||
			len(proposal.OmittedStepIDs) != 0 {
			return issue(ScenarioProposalIssueBranchFields, "branch_id")
		}
	case ScenarioIntentControl:
		if proposal.BranchID == "" || proposal.FromBranchID != "" ||
			proposal.ReferenceBranchID == "" || len(proposal.OmittedStepIDs) != 0 ||
			proposal.BranchID == proposal.ReferenceBranchID || proposal.usesExactActionID() {
			return issue(ScenarioProposalIssueControlFields, "proposal")
		}
	case ScenarioIntentAblate:
		if proposal.BranchID == "" || proposal.FromBranchID != "" ||
			proposal.ReferenceBranchID == "" || len(proposal.OmittedStepIDs) == 0 ||
			proposal.BranchID == proposal.ReferenceBranchID || proposal.usesExactActionID() {
			return issue(ScenarioProposalIssueAblationFields, "proposal")
		}
	case ScenarioIntentMinimize:
		if proposal.BranchID == "" || proposal.FromBranchID != "" ||
			proposal.ReferenceBranchID == "" || proposal.BranchID == proposal.ReferenceBranchID ||
			len(proposal.OmittedStepIDs) != 0 {
			return issue(ScenarioProposalIssueMinimizeFields, "proposal")
		}
	}
	for index := 1; index < len(proposal.Plan.Steps); index++ {
		if proposal.Plan.Steps[index].Selector.ActionID != "" {
			return issue(ScenarioProposalIssueLaterExactActionID, "plan.steps[].selector.action_id")
		}
	}
	return nil
}

func (proposal ScenarioInvestigationProposal) usesExactActionID() bool {
	for _, step := range proposal.Plan.Steps {
		if step.Selector.ActionID != "" {
			return true
		}
	}
	return false
}

func validScenarioIntent(intent string) bool {
	switch intent {
	case ScenarioIntentContinue, ScenarioIntentRevise, ScenarioIntentBranch,
		ScenarioIntentControl, ScenarioIntentAblate, ScenarioIntentSelect,
		ScenarioIntentAbandon, ScenarioIntentMinimize:
		return true
	default:
		return false
	}
}

// ScenarioInvestigationBranch is compact trusted feedback about one side
// execution. The full Trace remains in ScenarioAgentAttempt and final evidence;
// subsequent Agent calls receive only the checkpoint frontier and progress.
type ScenarioInvestigationBranch struct {
	ID                   string                        `json:"id"`
	Intent               string                        `json:"intent"`
	ReferenceBranchID    string                        `json:"reference_branch_id,omitempty"`
	RootDecision         int                           `json:"root_decision"`
	FinalDecision        int                           `json:"final_decision"`
	AvailableActions     []FrontierActionRef           `json:"available_actions,omitempty"`
	Plan                 ScenarioPlan                  `json:"plan"`
	AppliedInterventions []ScenarioAppliedIntervention `json:"applied_interventions,omitempty"`
	Outcome              string                        `json:"outcome"`
	ReasonCode           string                        `json:"reason_code,omitempty"`
	ProgressDelta        *ScenarioProgressDelta        `json:"progress_delta,omitempty"`
}

// ScenarioAppliedIntervention records what the trusted Runtime actually
// selected for one strategic plan step. It deliberately excludes automatic
// natural progress, so treatment/control/ablation differences are mechanical
// execution facts rather than differences between Agent-authored plan text.
type ScenarioAppliedIntervention struct {
	StepID   string            `json:"step_id"`
	Decision int               `json:"decision"`
	Action   FrontierActionRef `json:"action"`
}

// ScenarioCandidateExecution is retained trusted evidence for an experiment
// branch. It is not the Agent-selected final path, but it has already passed
// the same fresh Replay used for ordinary Scenario continuation.
type ScenarioCandidateExecution struct {
	BranchID          string            `json:"branch_id"`
	Intent            string            `json:"intent"`
	ReferenceBranchID string            `json:"reference_branch_id,omitempty"`
	Execution         ScenarioExecution `json:"execution"`
}

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
	ScenarioIntentAbandon  = "abandon"

	ScenarioReasonRevisionUnavailable = "revision-unavailable"

	ScenarioProposalIssueJSONInvalid        = "proposal-json-invalid"
	ScenarioProposalIssueIntentInvalid      = "intent-invalid"
	ScenarioProposalIssuePlanInvalid        = "plan-invalid"
	ScenarioProposalIssueZeroActionInvalid  = "zero-action-fields-invalid"
	ScenarioProposalIssueLaterExactActionID = "later-step-action-id-forbidden"
	ScenarioProposalIssueIntentUnavailable  = "intent-not-currently-available"
	ScenarioProposalIssueActionNotStrategic = "action-not-strategic"
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
		ScenarioProposalIssuePlanInvalid,
		ScenarioProposalIssueZeroActionInvalid,
		ScenarioProposalIssueLaterExactActionID,
		ScenarioProposalIssueIntentUnavailable,
		ScenarioProposalIssueActionNotStrategic:
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
	case "proposal", "intent", "plan", "plan.steps[].selector.action_id",
		"plan.steps[].selector":
		return true
	default:
		return false
	}
}

// ScenarioInvestigationProposal is the untrusted single-path Agent-level
// instruction around one ordinary ScenarioPlan.
type ScenarioInvestigationProposal struct {
	Intent string       `json:"intent"`
	Plan   ScenarioPlan `json:"plan"`
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
	if proposal.Intent == ScenarioIntentAbandon {
		if proposal.Plan.ID != "" || len(proposal.Plan.Steps) != 0 {
			return issue(ScenarioProposalIssueZeroActionInvalid, "proposal")
		}
		return nil
	}
	if proposal.Plan.Validate() != nil {
		return issue(ScenarioProposalIssuePlanInvalid, "plan")
	}
	for index := 1; index < len(proposal.Plan.Steps); index++ {
		if proposal.Plan.Steps[index].Selector.ActionID != "" {
			return issue(ScenarioProposalIssueLaterExactActionID, "plan.steps[].selector.action_id")
		}
	}
	return nil
}

func validScenarioIntent(intent string) bool {
	switch intent {
	case ScenarioIntentContinue, ScenarioIntentRevise, ScenarioIntentAbandon:
		return true
	default:
		return false
	}
}

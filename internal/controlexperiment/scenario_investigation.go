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
	ScenarioIntentMinimize = "minimize"

	ScenarioReasonBranchUnknown          = "branch-unknown"
	ScenarioReasonBranchDuplicate        = "branch-duplicate"
	ScenarioReasonRevisionUnavailable    = "revision-unavailable"
	ScenarioReasonAblationInvalid        = "ablation-invalid"
	ScenarioReasonTrustedFindingRequired = "trusted-finding-required"
)

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
	if len(data) == 0 || len(data) > ScenarioPlanMaxBytes {
		return ScenarioInvestigationProposal{}, errors.New("EXPERIMENT_SCENARIO_PROPOSAL_JSON_INVALID")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var proposal ScenarioInvestigationProposal
	if err := decoder.Decode(&proposal); err != nil {
		return ScenarioInvestigationProposal{}, errors.New("EXPERIMENT_SCENARIO_PROPOSAL_JSON_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ScenarioInvestigationProposal{}, errors.New("EXPERIMENT_SCENARIO_PROPOSAL_JSON_TRAILING")
	}
	if err := proposal.Validate(); err != nil {
		return ScenarioInvestigationProposal{}, err
	}
	return proposal, nil
}

func (proposal ScenarioInvestigationProposal) Validate() error {
	if !validScenarioIntent(proposal.Intent) ||
		(proposal.BranchID != "" && !validMethodToken(proposal.BranchID)) ||
		(proposal.FromBranchID != "" && !validMethodToken(proposal.FromBranchID)) ||
		(proposal.ReferenceBranchID != "" && !validMethodToken(proposal.ReferenceBranchID)) {
		return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
	}
	if proposal.Intent == ScenarioIntentSelect {
		if proposal.BranchID != "" || proposal.FromBranchID == "" ||
			proposal.ReferenceBranchID != "" || len(proposal.OmittedStepIDs) != 0 ||
			proposal.Plan.ID != "" || len(proposal.Plan.Steps) != 0 {
			return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
		}
		return nil
	}
	if proposal.Plan.Validate() != nil {
		return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
	}
	seen := make(map[string]bool, len(proposal.OmittedStepIDs))
	for _, id := range proposal.OmittedStepIDs {
		if !validMethodToken(id) || seen[id] {
			return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
		}
		seen[id] = true
	}
	switch proposal.Intent {
	case ScenarioIntentContinue:
		if proposal.BranchID != "" || proposal.ReferenceBranchID != "" ||
			len(proposal.OmittedStepIDs) != 0 {
			return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
		}
	case ScenarioIntentRevise:
		if proposal.BranchID != "" || proposal.FromBranchID != "" ||
			proposal.ReferenceBranchID != "" || len(proposal.OmittedStepIDs) != 0 {
			return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
		}
	case ScenarioIntentBranch:
		if proposal.BranchID == "" || proposal.ReferenceBranchID != "" ||
			len(proposal.OmittedStepIDs) != 0 {
			return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
		}
	case ScenarioIntentControl:
		if proposal.BranchID == "" || proposal.FromBranchID != "" ||
			proposal.ReferenceBranchID == "" || len(proposal.OmittedStepIDs) != 0 ||
			proposal.BranchID == proposal.ReferenceBranchID || proposal.usesExactActionID() {
			return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
		}
	case ScenarioIntentAblate:
		if proposal.BranchID == "" || proposal.FromBranchID != "" ||
			proposal.ReferenceBranchID == "" || len(proposal.OmittedStepIDs) == 0 ||
			proposal.BranchID == proposal.ReferenceBranchID || proposal.usesExactActionID() {
			return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
		}
	case ScenarioIntentMinimize:
		if proposal.BranchID == "" || proposal.FromBranchID != "" ||
			proposal.ReferenceBranchID == "" || proposal.BranchID == proposal.ReferenceBranchID ||
			len(proposal.OmittedStepIDs) != 0 {
			return errors.New("EXPERIMENT_SCENARIO_PROPOSAL_INVALID")
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
		ScenarioIntentControl, ScenarioIntentAblate, ScenarioIntentSelect, ScenarioIntentMinimize:
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

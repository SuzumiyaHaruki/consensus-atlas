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
	ScenarioPlanMaxSteps = 8
	ScenarioPlanMaxBytes = 64 << 10

	ScenarioStatusCompleted = "completed"
	ScenarioStatusStopped   = "stopped"

	ScenarioStepApplied  = "applied"
	ScenarioStepRejected = "rejected"

	ScenarioReasonNoMatch              = "no-match"
	ScenarioReasonAmbiguous            = "ambiguous"
	ScenarioReasonBudgetExhausted      = "budget-exhausted"
	ScenarioReasonMilestoneUnknown     = "milestone-unknown"
	ScenarioReasonMilestoneUnreachable = "milestone-unreachable"
)

// ScenarioPlan is an untrusted complete but bounded test intent. Later steps
// may wait for an existing trusted Risk milestone; the plan still carries no
// execution budget, fault policy, assertion, verdict, or trusted digest.
type ScenarioPlan struct {
	ID    string         `json:"id"`
	Steps []ScenarioStep `json:"steps"`
}

type ScenarioStep struct {
	ID             string                 `json:"id"`
	AfterMilestone string                 `json:"after_milestone,omitempty"`
	Selector       FrontierActionSelector `json:"selector"`
}

type ScenarioStepFeedback struct {
	StepID       string                       `json:"step_id"`
	Outcome      string                       `json:"outcome"`
	ReasonCode   string                       `json:"reason_code,omitempty"`
	Decision     int                          `json:"decision"`
	ViewDigest   string                       `json:"view_digest,omitempty"`
	MatchCount   int                          `json:"match_count"`
	Choice       *FrontierChoice              `json:"choice,omitempty"`
	Available    []FrontierActionRef          `json:"available_actions,omitempty"`
	RiskProgress semantic.RiskWitnessProgress `json:"risk_progress"`
}

// ScenarioExecution is a composition result, not a persistence contract. The
// final Trace remains the sole exact execution record and every applied choice
// was materialized and fresh-replayed by the existing stateless substrate.
type ScenarioExecution struct {
	PlanID               string                     `json:"plan_id"`
	Status               string                     `json:"status"`
	Steps                []ScenarioStepFeedback     `json:"steps"`
	AutomaticProgress    []ScenarioStepFeedback     `json:"automatic_progress,omitempty"`
	NaturalProgressStop  string                     `json:"natural_progress_stop,omitempty"`
	FinalTrace           controlruntime.Trace       `json:"final_trace"`
	FinalRisk            semantic.RiskWitnessResult `json:"final_risk"`
	Work                 StatelessDFSWork           `json:"work"`
	continuationFrontier *RiskFrontierView
	continuationSnapshot *controlruntime.Snapshot
}

// ScenarioActionPreparer materializes one trusted, target-composed Action only
// after an Agent selector has no match in the ordinary Runtime frontier. It is
// intended for author-supplied actions such as Partition that require an Offer
// call; the returned Action must immediately become the unique selector match.
type ScenarioActionPreparer func(
	context.Context,
	FrontierActionSelector,
	controlruntime.Trace,
	*controlruntime.Runtime,
) (control.ActionID, bool, error)

func ParseScenarioPlan(data []byte) (ScenarioPlan, error) {
	if len(data) == 0 || len(data) > ScenarioPlanMaxBytes {
		return ScenarioPlan{}, errors.New("EXPERIMENT_SCENARIO_PLAN_JSON_INVALID")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var plan ScenarioPlan
	if err := decoder.Decode(&plan); err != nil {
		return ScenarioPlan{}, errors.New("EXPERIMENT_SCENARIO_PLAN_JSON_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ScenarioPlan{}, errors.New("EXPERIMENT_SCENARIO_PLAN_JSON_TRAILING")
	}
	if err := plan.Validate(); err != nil {
		return ScenarioPlan{}, err
	}
	return plan, nil
}

func (plan ScenarioPlan) Validate() error {
	if !validMethodToken(plan.ID) || len(plan.Steps) == 0 || len(plan.Steps) > ScenarioPlanMaxSteps {
		return errors.New("EXPERIMENT_SCENARIO_PLAN_INVALID")
	}
	seen := make(map[string]bool, len(plan.Steps))
	for _, step := range plan.Steps {
		if !validMethodToken(step.ID) || seen[step.ID] ||
			(step.AfterMilestone != "" && !validMethodToken(step.AfterMilestone)) ||
			step.Selector.validate() != nil {
			return errors.New("EXPERIMENT_SCENARIO_STEP_INVALID")
		}
		seen[step.ID] = true
	}
	return nil
}

// ExecuteBoundedScenarioPlan resolves each selector only against the current
// trusted admissible frontier. One exclusively owned Runtime carries strategic
// steps and after_milestone progress; the promoted final Trace is independently
// fresh-replayed once. Zero or multiple matches stop with mechanical feedback.
func ExecuteBoundedScenarioPlan(
	ctx context.Context,
	executionID string,
	plan ScenarioPlan,
	maxSteps int,
	maxDecisions int,
	spec semantic.RiskWitnessSpec,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
	naturalProgressLimit int,
	preparers ...ScenarioActionPreparer,
) (ScenarioExecution, error) {
	if !validMethodToken(executionID) || plan.Validate() != nil || maxSteps <= 0 ||
		maxSteps > ScenarioPlanMaxSteps || spec.Validate() != nil || root.Validate() != nil ||
		maxDecisions <= 0 || maxDecisions > ScenarioAgentMaxDecisions ||
		rootRisk.Validate(spec) != nil || rootRisk.ExecutionDigest != root.Digest ||
		rootRisk.TargetIdentityDigest != root.ManifestDigest || newAdapter == nil ||
		isNilSemanticComponent(projector) || projector.ID() != rootRisk.ProjectorID ||
		naturalProgressLimit < 0 || naturalProgressLimit > maxDecisions ||
		len(preparers) > 1 || len(preparers) == 1 && preparers[0] == nil {
		return ScenarioExecution{}, errors.New("EXPERIMENT_SCENARIO_EXECUTION_INPUT_INVALID")
	}
	var preparer ScenarioActionPreparer
	if len(preparers) == 1 {
		preparer = preparers[0]
	}
	result := ScenarioExecution{
		PlanID: plan.ID, Status: ScenarioStatusCompleted, FinalTrace: root, FinalRisk: rootRisk,
	}
	view, snapshot, runtime, reconstruction, err := reconstructRiskFrontierRuntime(
		ctx, executionID+"-frontier-01", spec, rootRisk, root, len(root.Records),
		runtimeConfig, faultEnvelope, newAdapter,
	)
	addDFSPhase(&result.Work.FrontierReconstruction, reconstruction)
	if err != nil {
		return ScenarioExecution{}, err
	}
	closeWith := func(cause error) error {
		return errors.Join(cause, runtime.Close())
	}
	refreshFrontier := func(id string) error {
		var refreshErr error
		view, snapshot, refreshErr = projectRiskFrontierFromLiveRuntime(
			ctx, id, spec, result.FinalRisk, result.FinalTrace, faultEnvelope, runtime,
		)
		return refreshErr
	}
	projectChild := func(id string, child controlruntime.Trace) error {
		risk, projectErr := projector.Project(id, spec, child)
		if projectErr != nil {
			return fmt.Errorf("EXPERIMENT_SCENARIO_RISK_PROJECTION_FAILED: %w", projectErr)
		}
		if risk.Validate(spec) != nil || risk.ProjectorID != projector.ID() ||
			risk.ExecutionDigest != child.Digest || risk.TargetIdentityDigest != child.ManifestDigest {
			return errors.New("EXPERIMENT_SCENARIO_RISK_PROJECTION_INVALID")
		}
		result.FinalTrace, result.FinalRisk = child, risk
		return nil
	}
	for index, step := range plan.Steps {
		progress, err := semantic.NewRiskWitnessProgress(spec, result.FinalRisk)
		if err != nil {
			return ScenarioExecution{}, closeWith(err)
		}
		if index >= maxSteps {
			result.Status = ScenarioStatusStopped
			result.Steps = append(result.Steps, ScenarioStepFeedback{
				StepID: step.ID, Outcome: ScenarioStepRejected, ReasonCode: ScenarioReasonBudgetExhausted,
				Decision: len(result.FinalTrace.Records) + 1, RiskProgress: progress,
			})
			break
		}
		if step.AfterMilestone != "" {
			if !scenarioSpecHasMilestone(spec, step.AfterMilestone) {
				result.Status = ScenarioStatusStopped
				result.Steps = append(result.Steps, ScenarioStepFeedback{
					StepID: step.ID, Outcome: ScenarioStepRejected,
					ReasonCode: ScenarioReasonMilestoneUnknown,
					Decision:   len(result.FinalTrace.Records) + 1, RiskProgress: progress,
				})
				break
			}
			for !scenarioRiskHasMilestone(result.FinalRisk, step.AfterMilestone) {
				if len(result.FinalTrace.Records)-len(root.Records) >= maxDecisions {
					result.Status = ScenarioStatusStopped
					result.Steps = append(result.Steps, ScenarioStepFeedback{
						StepID: step.ID, Outcome: ScenarioStepRejected,
						ReasonCode: ScenarioReasonBudgetExhausted,
						Decision:   len(result.FinalTrace.Records) + 1, RiskProgress: progress,
					})
					break
				}
				if scenarioClientTerminal(snapshot) {
					result.Status = ScenarioStatusStopped
					result.Steps = append(result.Steps, ScenarioStepFeedback{
						StepID: step.ID, Outcome: ScenarioStepRejected,
						ReasonCode: ScenarioReasonMilestoneUnreachable,
						Decision:   len(result.FinalTrace.Records) + 1, RiskProgress: progress,
					})
					break
				}
				action, ok := scenarioNaturalProgressAction(view.Actions)
				if !ok {
					result.Status = ScenarioStatusStopped
					result.Steps = append(result.Steps, ScenarioStepFeedback{
						StepID: step.ID, Outcome: ScenarioStepRejected,
						ReasonCode: ScenarioReasonMilestoneUnreachable,
						Decision:   len(result.FinalTrace.Records) + 1, RiskProgress: progress,
					})
					break
				}
				choice, choiceErr := NewFrontierChoice(
					fmt.Sprintf("%s-step-%02d-wait-choice-%02d", executionID, index+1,
						len(result.AutomaticProgress)+1), view, spec, action.ActionID,
				)
				if choiceErr != nil {
					return ScenarioExecution{}, closeWith(choiceErr)
				}
				frontier, frontierErr := scenarioActionFrontier(view)
				if frontierErr != nil {
					return ScenarioExecution{}, closeWith(frontierErr)
				}
				child, materialization, executeErr := executeDFSChildOnLiveRuntime(
					ctx, frontier, choice.Action, runtime,
				)
				addDFSPhase(&result.Work.ChildMaterialization, materialization)
				if executeErr != nil {
					return result, &StatelessDFSExecutionError{
						Work: result.Work, cause: closeWith(executeErr),
					}
				}
				if projectErr := projectChild(
					fmt.Sprintf("%s-step-%02d-wait-risk-%02d", executionID, index+1,
						len(result.AutomaticProgress)+1), child,
				); projectErr != nil {
					return ScenarioExecution{}, closeWith(projectErr)
				}
				progress, err = semantic.NewRiskWitnessProgress(spec, result.FinalRisk)
				if err != nil {
					return ScenarioExecution{}, closeWith(err)
				}
				result.AutomaticProgress = append(result.AutomaticProgress, ScenarioStepFeedback{
					StepID:  fmt.Sprintf("%s-wait-%02d", step.ID, len(result.AutomaticProgress)+1),
					Outcome: ScenarioStepApplied, Decision: view.NextDecision, ViewDigest: view.Digest,
					MatchCount: 1, Choice: &choice, RiskProgress: progress,
				})
				if refreshErr := refreshFrontier(fmt.Sprintf(
					"%s-step-%02d-wait-frontier-%02d", executionID, index+1,
					len(result.AutomaticProgress)+1,
				)); refreshErr != nil {
					return ScenarioExecution{}, closeWith(refreshErr)
				}
			}
			if result.Status == ScenarioStatusStopped {
				break
			}
		}
		if len(result.FinalTrace.Records)-len(root.Records) >= maxDecisions {
			result.Status = ScenarioStatusStopped
			result.Steps = append(result.Steps, ScenarioStepFeedback{
				StepID: step.ID, Outcome: ScenarioStepRejected,
				ReasonCode: ScenarioReasonBudgetExhausted,
				Decision:   len(result.FinalTrace.Records) + 1, RiskProgress: progress,
			})
			break
		}
		matches := scenarioMatches(view.Actions, step.Selector)
		preparedAction := false
		if len(matches) == 0 && preparer != nil {
			preparedID, prepared, prepareErr := preparer(
				ctx, step.Selector, result.FinalTrace, runtime,
			)
			if prepareErr != nil {
				return ScenarioExecution{}, closeWith(prepareErr)
			}
			if prepared {
				preparedAction = true
				chargePrepareActions(&result.Work.ChildMaterialization, 1)
				enabled, enabledErr := runtime.EnabledActions(ctx)
				if enabledErr != nil {
					return ScenarioExecution{}, closeWith(enabledErr)
				}
				admissible := admissibleActions(
					faultEnvelope, faultUsageFromRecords(result.FinalTrace.Records),
					enabled, runtime.Snapshot(),
				)
				actionFrontier, frontierErr := newActionFrontierView(
					view.ID, result.FinalTrace, runtime.Snapshot(), enabled, admissible,
				)
				if frontierErr != nil {
					return ScenarioExecution{}, closeWith(frontierErr)
				}
				view, frontierErr = newRiskFrontierView(view.ID, spec, progress, actionFrontier)
				if frontierErr != nil {
					return ScenarioExecution{}, closeWith(frontierErr)
				}
				matches = scenarioMatches(view.Actions, step.Selector)
				if preparedID == "" || len(matches) != 1 || matches[0].ActionID != preparedID {
					return ScenarioExecution{}, closeWith(
						errors.New("EXPERIMENT_SCENARIO_PREPARED_ACTION_MISMATCH"),
					)
				}
			}
		}
		feedback := ScenarioStepFeedback{
			StepID: step.ID, Outcome: ScenarioStepRejected, Decision: view.NextDecision,
			ViewDigest: view.Digest, MatchCount: len(matches), RiskProgress: progress,
		}
		if len(matches) != 1 {
			result.Status = ScenarioStatusStopped
			feedback.ReasonCode = ScenarioReasonNoMatch
			if len(matches) > 1 {
				feedback.ReasonCode = ScenarioReasonAmbiguous
				feedback.Available = append([]FrontierActionRef(nil), matches...)
			}
			result.Steps = append(result.Steps, feedback)
			break
		}
		choice, err := NewFrontierChoice(
			fmt.Sprintf("%s-choice-%02d", executionID, index+1), view, spec, matches[0].ActionID,
		)
		if err != nil {
			return ScenarioExecution{}, closeWith(err)
		}
		frontier, err := scenarioActionFrontier(view)
		if err != nil {
			return ScenarioExecution{}, closeWith(err)
		}
		var preparedPrefixes []controlruntime.Trace
		if preparedAction {
			preparedPrefixes = append(preparedPrefixes, result.FinalTrace)
		}
		child, materialization, err := executeDFSChildOnLiveRuntime(
			ctx, frontier, choice.Action, runtime, preparedPrefixes...,
		)
		addDFSPhase(&result.Work.ChildMaterialization, materialization)
		if err != nil {
			result.Work.TotalWorkUnits = result.Work.FrontierReconstruction.WorkUnits +
				result.Work.ChildMaterialization.WorkUnits + result.Work.ChildVerification.WorkUnits
			return result, &StatelessDFSExecutionError{Work: result.Work, cause: closeWith(err)}
		}
		if projectErr := projectChild(
			fmt.Sprintf("%s-risk-%02d", executionID, index+1), child,
		); projectErr != nil {
			return ScenarioExecution{}, closeWith(projectErr)
		}
		progress, err = semantic.NewRiskWitnessProgress(spec, result.FinalRisk)
		if err != nil {
			return ScenarioExecution{}, closeWith(err)
		}
		feedback.Outcome, feedback.Choice, feedback.RiskProgress = ScenarioStepApplied, &choice, progress
		result.Steps = append(result.Steps, feedback)
		if index+1 < len(plan.Steps) {
			if refreshErr := refreshFrontier(fmt.Sprintf(
				"%s-step-%02d-frontier", executionID, index+2,
			)); refreshErr != nil {
				return ScenarioExecution{}, closeWith(refreshErr)
			}
		}
	}
	if result.Status == ScenarioStatusCompleted && naturalProgressLimit > 0 {
		remaining := maxDecisions - (len(result.FinalTrace.Records) - len(root.Records))
		if remaining > naturalProgressLimit {
			remaining = naturalProgressLimit
		}
		if remaining > 0 {
			if view.PrefixTraceDigest != result.FinalTrace.Digest {
				if refreshErr := refreshFrontier(executionID + "-natural-frontier"); refreshErr != nil {
					return ScenarioExecution{}, closeWith(refreshErr)
				}
			}
			live, liveErr := executeScenarioNaturalProgressOnLiveRuntime(
				ctx, executionID+"-natural", remaining, spec, result.FinalRisk,
				result.FinalTrace, view, snapshot, faultEnvelope, runtime, projector,
			)
			addDFSPhase(&result.Work.ChildMaterialization, live.Work.ChildMaterialization)
			result.NaturalProgressStop = live.StopReason
			result.AutomaticProgress = append(result.AutomaticProgress, live.Steps...)
			result.FinalTrace, result.FinalRisk = live.FinalTrace, live.FinalRisk
			if liveErr != nil {
				return result, &StatelessDFSExecutionError{
					Work: result.Work, cause: closeWith(liveErr),
				}
			}
		}
	}
	if err := runtime.Close(); err != nil {
		return ScenarioExecution{}, err
	}
	if len(result.FinalTrace.Records) > len(root.Records) {
		verificationRuntime, verification, verifyErr := verifyDFSChildRuntime(
			ctx, result.FinalTrace, runtimeConfig, newAdapter,
		)
		addDFSPhase(&result.Work.ChildVerification, verification)
		if verifyErr != nil {
			if verificationRuntime != nil {
				verifyErr = errors.Join(verifyErr, verificationRuntime.Close())
			}
			result.Work.TotalWorkUnits = result.Work.FrontierReconstruction.WorkUnits +
				result.Work.ChildMaterialization.WorkUnits + result.Work.ChildVerification.WorkUnits
			return result, &StatelessDFSExecutionError{Work: result.Work, cause: verifyErr}
		}
		if verificationRuntime == nil {
			return result, errors.New("EXPERIMENT_SCENARIO_VERIFICATION_RUNTIME_MISSING")
		}
		continuation, continuationSnapshot, continuationErr := projectRiskFrontierFromLiveRuntime(
			ctx, executionID+"-verified-frontier", spec, result.FinalRisk, result.FinalTrace,
			faultEnvelope, verificationRuntime,
		)
		continuationErr = errors.Join(continuationErr, verificationRuntime.Close())
		if continuationErr != nil {
			return result, continuationErr
		}
		result.continuationFrontier = &continuation
		result.continuationSnapshot = &continuationSnapshot
	}
	result.Work.TotalWorkUnits = result.Work.FrontierReconstruction.WorkUnits +
		result.Work.ChildMaterialization.WorkUnits + result.Work.ChildVerification.WorkUnits
	return result, nil
}

func scenarioSpecHasMilestone(spec semantic.RiskWitnessSpec, id string) bool {
	for _, milestone := range spec.Milestones {
		if milestone.ID == id {
			return true
		}
	}
	return false
}

func scenarioRiskHasMilestone(result semantic.RiskWitnessResult, id string) bool {
	for _, milestone := range result.SatisfiedMilestones {
		if milestone == id {
			return true
		}
	}
	return false
}

// CompileScenarioPolicy converts one successful, already replay-verified
// execution into exact rules for the existing qualified executor.
func CompileScenarioPolicy(
	id string,
	root controlruntime.Trace,
	execution ScenarioExecution,
	fallback []control.ActionKind,
) (Policy, error) {
	if !validMethodToken(id) || root.Validate() != nil || execution.Status != ScenarioStatusCompleted ||
		execution.FinalTrace.Validate() != nil || len(execution.Steps) == 0 ||
		len(execution.FinalTrace.Records) != len(root.Records)+len(execution.Steps)+
			len(execution.AutomaticProgress) ||
		execution.FinalTrace.ManifestDigest != root.ManifestDigest ||
		!scenarioTraceHasPrefix(execution.FinalTrace, root) {
		return Policy{}, errors.New("EXPERIMENT_SCENARIO_POLICY_INPUT_INVALID")
	}
	covered := make(map[int]bool, len(execution.Steps)+len(execution.AutomaticProgress))
	all := append(cloneScenarioStepFeedback(execution.Steps),
		cloneScenarioStepFeedback(execution.AutomaticProgress)...)
	for _, feedback := range all {
		index := feedback.Decision - 1
		if index < len(root.Records) || index >= len(execution.FinalTrace.Records) || covered[index] {
			return Policy{}, errors.New("EXPERIMENT_SCENARIO_POLICY_STEP_INVALID")
		}
		covered[index] = true
		record := execution.FinalTrace.Records[index]
		if !scenarioFeedbackMatchesRecord(feedback, record) {
			return Policy{}, errors.New("EXPERIMENT_SCENARIO_POLICY_ACTION_MISMATCH")
		}
	}
	if len(covered) != len(execution.FinalTrace.Records)-len(root.Records) {
		return Policy{}, errors.New("EXPERIMENT_SCENARIO_POLICY_STEP_INVALID")
	}
	rules := make([]DecisionRule, len(execution.FinalTrace.Records))
	for index, record := range execution.FinalTrace.Records {
		rule := DecisionRule{
			Decision: index + 1, Kind: record.Action.Kind,
			Node: record.Action.Node.Node, ActionID: record.Action.ID,
		}
		if record.Action.Kind == control.ActionPartition {
			rule.Parameters = append(json.RawMessage(nil), record.Action.Parameters...)
		}
		rules[index] = rule
	}
	policy := Policy{
		Version: PolicyVersion, ID: id, Rules: rules,
		Priority: append([]control.ActionKind(nil), fallback...),
	}
	if err := policy.Validate(len(rules)); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func scenarioFeedbackMatchesRecord(feedback ScenarioStepFeedback, record controlruntime.ActionRecord) bool {
	if feedback.Outcome != ScenarioStepApplied || feedback.ReasonCode != "" ||
		feedback.MatchCount != 1 || feedback.Choice == nil || feedback.Decision != int(record.Step) ||
		feedback.Choice.Decision != int(record.Step) || feedback.Choice.Action.ActionID != record.Action.ID ||
		feedback.Choice.Action.Kind != record.Action.Kind || feedback.Choice.Action.Node != record.Action.Node ||
		feedback.Choice.Action.ItemID != record.Action.Item {
		return false
	}
	digest, err := control.CanonicalDigest(record.Action)
	return err == nil && digest == feedback.Choice.Action.ActionDigest
}

func scenarioTraceHasPrefix(trace controlruntime.Trace, prefix controlruntime.Trace) bool {
	if len(trace.Records) < len(prefix.Records) {
		return false
	}
	for index := range prefix.Records {
		if !reflect.DeepEqual(trace.Records[index], prefix.Records[index]) {
			return false
		}
	}
	return true
}

func scenarioMatches(actions []FrontierActionRef, selector FrontierActionSelector) []FrontierActionRef {
	result := make([]FrontierActionRef, 0, 1)
	for _, action := range actions {
		if selector.matches(action) {
			result = append(result, action)
		}
	}
	return result
}

func scenarioActionFrontier(view RiskFrontierView) (ActionFrontierView, error) {
	frontier := ActionFrontierView{
		SchemaVersion: ActionFrontierViewSchemaVersion, ID: view.ID,
		PrefixDecisions: view.PrefixDecisions, NextDecision: view.NextDecision,
		PrefixTraceDigest: view.PrefixTraceDigest, SnapshotDigest: view.SnapshotDigest,
		RuntimeEnabledDigest: view.RuntimeEnabledDigest, AdmissibleDigest: view.AdmissibleDigest,
		RuntimeActionCount: view.RuntimeActionCount, Actions: append([]FrontierActionRef(nil), view.Actions...),
	}
	return frontier.seal()
}

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

	ScenarioReasonNoMatch         = "no-match"
	ScenarioReasonAmbiguous       = "ambiguous"
	ScenarioReasonBudgetExhausted = "budget-exhausted"
)

// ScenarioPlan is an untrusted short-horizon intent. It deliberately carries
// no execution budget, fault policy, assertion, verdict, or trusted digest.
type ScenarioPlan struct {
	ID    string         `json:"id"`
	Steps []ScenarioStep `json:"steps"`
}

type ScenarioStep struct {
	ID       string                 `json:"id"`
	Selector FrontierActionSelector `json:"selector"`
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
	PlanID     string                     `json:"plan_id"`
	Status     string                     `json:"status"`
	Steps      []ScenarioStepFeedback     `json:"steps"`
	FinalTrace controlruntime.Trace       `json:"final_trace"`
	FinalRisk  semantic.RiskWitnessResult `json:"final_risk"`
	Work       StatelessDFSWork           `json:"work"`
}

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
		if !validMethodToken(step.ID) || seen[step.ID] || step.Selector.validate() != nil {
			return errors.New("EXPERIMENT_SCENARIO_STEP_INVALID")
		}
		seen[step.ID] = true
	}
	return nil
}

// ExecuteBoundedScenarioPlan resolves each selector only against the current
// trusted admissible frontier. Zero or multiple matches stop with mechanical
// feedback; the function never guesses and never accepts an Agent-authored
// execution fact.
func ExecuteBoundedScenarioPlan(
	ctx context.Context,
	executionID string,
	plan ScenarioPlan,
	maxSteps int,
	spec semantic.RiskWitnessSpec,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	runtimeConfig RuntimeConfig,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
	projector SemanticPrefixProjector,
) (ScenarioExecution, error) {
	if !validMethodToken(executionID) || plan.Validate() != nil || maxSteps <= 0 ||
		maxSteps > ScenarioPlanMaxSteps || spec.Validate() != nil || root.Validate() != nil ||
		rootRisk.Validate(spec) != nil || rootRisk.ExecutionDigest != root.Digest ||
		rootRisk.TargetIdentityDigest != root.ManifestDigest || newAdapter == nil ||
		isNilSemanticComponent(projector) || projector.ID() != rootRisk.ProjectorID {
		return ScenarioExecution{}, errors.New("EXPERIMENT_SCENARIO_EXECUTION_INPUT_INVALID")
	}
	result := ScenarioExecution{
		PlanID: plan.ID, Status: ScenarioStatusCompleted, FinalTrace: root, FinalRisk: rootRisk,
	}
	for index, step := range plan.Steps {
		progress, err := semantic.NewRiskWitnessProgress(spec, result.FinalRisk)
		if err != nil {
			return ScenarioExecution{}, err
		}
		if index >= maxSteps {
			result.Status = ScenarioStatusStopped
			result.Steps = append(result.Steps, ScenarioStepFeedback{
				StepID: step.ID, Outcome: ScenarioStepRejected, ReasonCode: ScenarioReasonBudgetExhausted,
				Decision: len(result.FinalTrace.Records) + 1, RiskProgress: progress,
			})
			break
		}
		view, _, runtime, reconstruction, err := reconstructRiskFrontierRuntime(
			ctx, fmt.Sprintf("%s-step-%02d", executionID, index+1), spec, result.FinalRisk,
			result.FinalTrace, len(result.FinalTrace.Records), runtimeConfig, faultEnvelope, newAdapter,
		)
		addDFSPhase(&result.Work.FrontierReconstruction, reconstruction)
		if err != nil {
			return ScenarioExecution{}, err
		}
		matches := scenarioMatches(view.Actions, step.Selector)
		feedback := ScenarioStepFeedback{
			StepID: step.ID, Outcome: ScenarioStepRejected, Decision: view.NextDecision,
			ViewDigest: view.Digest, MatchCount: len(matches), RiskProgress: progress,
		}
		if len(matches) != 1 {
			if err := runtime.Close(); err != nil {
				return ScenarioExecution{}, err
			}
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
			return ScenarioExecution{}, errors.Join(err, runtime.Close())
		}
		frontier, err := scenarioActionFrontier(view)
		if err != nil {
			return ScenarioExecution{}, errors.Join(err, runtime.Close())
		}
		child, materialization, verification, err := materializeDFSChildFromRuntime(
			ctx, frontier, choice.Action, runtime, runtimeConfig, newAdapter,
		)
		addDFSPhase(&result.Work.ChildMaterialization, materialization)
		addDFSPhase(&result.Work.ChildVerification, verification)
		if err != nil {
			return ScenarioExecution{}, err
		}
		risk, err := projector.Project(
			fmt.Sprintf("%s-risk-%02d", executionID, index+1), spec, child,
		)
		if err != nil {
			return ScenarioExecution{}, fmt.Errorf("EXPERIMENT_SCENARIO_RISK_PROJECTION_FAILED: %w", err)
		}
		if risk.Validate(spec) != nil || risk.ProjectorID != projector.ID() ||
			risk.ExecutionDigest != child.Digest || risk.TargetIdentityDigest != child.ManifestDigest {
			return ScenarioExecution{}, errors.New("EXPERIMENT_SCENARIO_RISK_PROJECTION_INVALID")
		}
		progress, err = semantic.NewRiskWitnessProgress(spec, risk)
		if err != nil {
			return ScenarioExecution{}, err
		}
		feedback.Outcome, feedback.Choice, feedback.RiskProgress = ScenarioStepApplied, &choice, progress
		result.Steps = append(result.Steps, feedback)
		result.FinalTrace, result.FinalRisk = child, risk
	}
	result.Work.TotalWorkUnits = result.Work.FrontierReconstruction.WorkUnits +
		result.Work.ChildMaterialization.WorkUnits + result.Work.ChildVerification.WorkUnits
	return result, nil
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
		len(execution.FinalTrace.Records) != len(root.Records)+len(execution.Steps) ||
		execution.FinalTrace.ManifestDigest != root.ManifestDigest ||
		!scenarioTraceHasPrefix(execution.FinalTrace, root) {
		return Policy{}, errors.New("EXPERIMENT_SCENARIO_POLICY_INPUT_INVALID")
	}
	for index, feedback := range execution.Steps {
		record := execution.FinalTrace.Records[len(root.Records)+index]
		if feedback.Outcome != ScenarioStepApplied || feedback.ReasonCode != "" ||
			feedback.MatchCount != 1 || feedback.Choice == nil || feedback.Decision != int(record.Step) ||
			feedback.Choice.Decision != int(record.Step) || feedback.Choice.Action.ActionID != record.Action.ID ||
			feedback.Choice.Action.Kind != record.Action.Kind || feedback.Choice.Action.Node != record.Action.Node ||
			feedback.Choice.Action.ItemID != record.Action.Item {
			return Policy{}, errors.New("EXPERIMENT_SCENARIO_POLICY_STEP_INVALID")
		}
		digest, err := control.CanonicalDigest(record.Action)
		if err != nil || digest != feedback.Choice.Action.ActionDigest {
			return Policy{}, errors.New("EXPERIMENT_SCENARIO_POLICY_ACTION_MISMATCH")
		}
	}
	rules := make([]DecisionRule, len(execution.FinalTrace.Records))
	for index, record := range execution.FinalTrace.Records {
		rules[index] = DecisionRule{
			Decision: index + 1, Kind: record.Action.Kind,
			Node: record.Action.Node.Node, ActionID: record.Action.ID,
		}
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

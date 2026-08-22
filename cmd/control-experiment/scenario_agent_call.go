package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	scenarioAgentPromptVersion                = "scenario-agent-investigation-v26"
	scenarioInvestigationStructuredOutputName = "scenario_investigation_v7"
)

type scenarioAgentCallJournal struct {
	core          *statelessAgentCallJournal
	repairPending *controlexperiment.ScenarioAgentView
}

type scenarioPromptStepFeedback struct {
	StepID        string                                     `json:"step_id"`
	Outcome       string                                     `json:"outcome"`
	ReasonCode    string                                     `json:"reason_code,omitempty"`
	Decision      int                                        `json:"decision,omitempty"`
	MatchCount    int                                        `json:"match_count,omitempty"`
	Available     []controlexperiment.FrontierActionRef      `json:"available_actions,omitempty"`
	SelectorTrace []controlexperiment.ScenarioSelectorFilter `json:"selector_trace,omitempty"`
}

// scenarioPromptAction combines the exact enabled Action and its trusted
// semantic classification. The previous prompt repeated every Action once in
// root_frontier and once in action_semantics, which made large frontiers both
// expensive and difficult to read.
type scenarioPromptAction struct {
	controlexperiment.FrontierActionRef
	ActorRole      string `json:"actor_role"`
	MessageClass   string `json:"message_class"`
	EpochRelation  string `json:"epoch_relation"`
	OperationState string `json:"operation_state"`
}

type scenarioPromptFrontier struct {
	SchemaVersion        string                                         `json:"schema_version"`
	ID                   string                                         `json:"id"`
	Progress             semantic.RiskWitnessProgress                   `json:"progress"`
	PrefixDecisions      int                                            `json:"prefix_decisions"`
	NextDecision         int                                            `json:"next_decision"`
	PrefixTraceDigest    string                                         `json:"prefix_trace_digest"`
	SnapshotDigest       string                                         `json:"snapshot_digest"`
	RuntimeEnabledDigest string                                         `json:"runtime_enabled_digest"`
	AdmissibleDigest     string                                         `json:"admissible_digest"`
	RuntimeActionCount   int                                            `json:"runtime_action_count"`
	Coordination         *controlexperiment.ConsensusCoordinationStatus `json:"coordination,omitempty"`
	Actions              []scenarioPromptAction                         `json:"actions"`
	Digest               string                                         `json:"digest"`
}

func newScenarioPromptFrontier(
	view controlexperiment.ScenarioAgentView,
) (scenarioPromptFrontier, error) {
	if view.Semantics.Validate(view.Frontier) != nil ||
		len(view.Frontier.Actions) != len(view.Semantics.ActionHints) {
		return scenarioPromptFrontier{}, errors.New("SCENARIO_AGENT_PROMPT_FRONTIER_INVALID")
	}
	actions := make([]scenarioPromptAction, len(view.Frontier.Actions))
	for index, action := range view.Frontier.Actions {
		hint := view.Semantics.ActionHints[index]
		actions[index] = scenarioPromptAction{
			FrontierActionRef: action,
			ActorRole:         hint.ActorRole, MessageClass: hint.MessageClass,
			EpochRelation: hint.EpochRelation, OperationState: hint.OperationState,
		}
	}
	coordination := view.Semantics.Coordination
	if coordination != nil {
		cloned := *coordination
		cloned.ElectionProgress.CandidateNodes = append(
			[]control.NodeID(nil), coordination.ElectionProgress.CandidateNodes...,
		)
		coordination = &cloned
	}
	frontier := view.Frontier
	return scenarioPromptFrontier{
		SchemaVersion: frontier.SchemaVersion, ID: frontier.ID, Progress: frontier.Progress,
		PrefixDecisions: frontier.PrefixDecisions, NextDecision: frontier.NextDecision,
		PrefixTraceDigest: frontier.PrefixTraceDigest, SnapshotDigest: frontier.SnapshotDigest,
		RuntimeEnabledDigest: frontier.RuntimeEnabledDigest, AdmissibleDigest: frontier.AdmissibleDigest,
		RuntimeActionCount: frontier.RuntimeActionCount, Coordination: coordination,
		Actions: actions, Digest: frontier.Digest,
	}, nil
}

func compactScenarioPromptSteps(
	steps []controlexperiment.ScenarioStepFeedback,
) []scenarioPromptStepFeedback {
	result := make([]scenarioPromptStepFeedback, 0, len(steps))
	for _, step := range steps {
		compact := scenarioPromptStepFeedback{
			StepID: step.StepID, Outcome: step.Outcome, ReasonCode: step.ReasonCode,
			Decision: step.Decision, MatchCount: step.MatchCount,
			SelectorTrace: append(
				[]controlexperiment.ScenarioSelectorFilter(nil), step.SelectorTrace...,
			),
		}
		if step.Outcome == controlexperiment.ScenarioStepRejected {
			compact.Available = append(
				[]controlexperiment.FrontierActionRef(nil), step.Available...,
			)
		}
		result = append(result, compact)
	}
	return result
}

func newScenarioAgentCallJournal(
	directory string,
	client agentIntentTransport,
	key string,
) (*scenarioAgentCallJournal, error) {
	core, err := newStatelessAgentCallJournal(directory, client, key)
	if err != nil {
		return nil, err
	}
	return &scenarioAgentCallJournal{core: core}, nil
}

func recoverScenarioAgentCallJournal(
	directory string,
	client agentIntentTransport,
) (*scenarioAgentCallJournal, error) {
	core, err := recoverStatelessAgentCallJournal(directory, client)
	if err != nil {
		return nil, err
	}
	return &scenarioAgentCallJournal{core: core}, nil
}

func (journal *scenarioAgentCallJournal) SetRoot(rootID string) error {
	if journal == nil || journal.core == nil {
		return errors.New("SCENARIO_AGENT_CALL_JOURNAL_INVALID")
	}
	return journal.core.SetRoot(rootID)
}

func (journal *scenarioAgentCallJournal) ActivateKey(key string) error {
	if journal == nil || journal.core == nil {
		return errors.New("SCENARIO_AGENT_CALL_JOURNAL_INVALID")
	}
	return journal.core.ActivateKey(key)
}

func (journal *scenarioAgentCallJournal) Audits() (
	[]controlexperiment.StatelessAgentCallAudit,
	error,
) {
	if journal == nil || journal.core == nil {
		return nil, errors.New("SCENARIO_AGENT_CALL_JOURNAL_INVALID")
	}
	return journal.core.Audits()
}

func (journal *scenarioAgentCallJournal) Planner(
	ctx context.Context,
	spec semantic.RiskWitnessSpec,
	view controlexperiment.ScenarioAgentView,
) ([]byte, controlexperiment.ModelWork, error) {
	if journal == nil || journal.core == nil {
		return nil, controlexperiment.ModelWork{}, errors.New("SCENARIO_AGENT_CALL_JOURNAL_INVALID")
	}
	repair := journal.repairPending != nil
	promptView := view
	if repair {
		if journal.repairPending.Frontier.Digest != view.Frontier.Digest {
			return nil, controlexperiment.ModelWork{}, errors.New("SCENARIO_AGENT_REPAIR_FRONTIER_DRIFT")
		}
		promptView = *journal.repairPending
	}
	var system, user string
	var err error
	if repair {
		system, user, err = scenarioAgentRepairPrompt(spec, promptView)
	} else {
		system, user, err = scenarioAgentPrompt(spec, promptView)
	}
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	output, err := scenarioInvestigationStructuredOutput(promptView)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	prepared, err := journal.core.client.prepare(system, user, output)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	requestDigest, err := control.CanonicalDigest(promptView)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	ordinal := journal.core.next + 1
	intentID := fmt.Sprintf("scenario-agent-call-%d-binding-%s", ordinal, requestDigest)
	if repair {
		intentID = fmt.Sprintf(
			"scenario-agent-repair-call-%d-from-%d-binding-%s",
			ordinal, ordinal-1, requestDigest,
		)
	}
	content, work, callErr := journal.core.planningCall(ctx, planningAgentCallPlan{
		intentID:      intentID,
		requestDigest: requestDigest, prepared: prepared, contentReady: true,
	})
	if errors.Is(callErr, errStatelessAgentCallKeyRequired) {
		return nil, work, callErr
	}
	if repair {
		journal.repairPending = nil
	}
	failureCode := statelessAgentFailureCode(callErr)
	if failureCode == "" {
		return content, work, callErr
	}
	reasonCode, responseFailure := scenarioProviderFailure(failureCode)
	if !responseFailure {
		return nil, work, callErr
	}
	repairable := failureCode == statelessAgentFailureFinishLength && !repair
	if repairable {
		pending := promptView
		journal.repairPending = &pending
	}
	return nil, work, &controlexperiment.ScenarioPlannerResponseFailure{
		Code: reasonCode, Repairable: repairable,
	}
}

func scenarioProviderFailure(code string) (string, bool) {
	switch code {
	case statelessAgentFailureFinishLength:
		return controlexperiment.ScenarioAgentReasonResponseFinishLength, true
	case statelessAgentFailureEmptyContent:
		return controlexperiment.ScenarioAgentReasonResponseEmptyContent, true
	case statelessAgentFailureMalformed:
		return controlexperiment.ScenarioAgentReasonResponseMalformed, true
	case statelessAgentFailureResponseTooBig:
		return controlexperiment.ScenarioAgentReasonResponseTooLarge, true
	default:
		return "", false
	}
}

func scenarioInvestigationStructuredOutput(view controlexperiment.ScenarioAgentView) (openRouterStructuredOutput, error) {
	if view.MaxSteps <= 0 || view.MaxSteps > controlexperiment.ScenarioPlanMaxSteps ||
		len(view.AvailableIntents) == 0 {
		return openRouterStructuredOutput{}, errors.New("SCENARIO_AGENT_OUTPUT_SCHEMA_INVALID")
	}
	stringField := map[string]any{"type": "string", "minLength": 1}
	visibleKinds := make([]string, 0, len(view.Frontier.Actions))
	seenKinds := make(map[control.ActionKind]bool)
	for _, action := range view.Frontier.Actions {
		if seenKinds[action.Kind] {
			continue
		}
		seenKinds[action.Kind] = true
		visibleKinds = append(visibleKinds, string(action.Kind))
	}
	if len(visibleKinds) == 0 {
		return openRouterStructuredOutput{}, errors.New("SCENARIO_AGENT_OUTPUT_SCHEMA_INVALID")
	}
	selector := map[string]any{
		"oneOf": []any{
			map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"action_id": stringField},
				"required":   []string{"action_id"},
			},
			map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"kind": map[string]any{"type": "string", "enum": visibleKinds},
					"node": stringField, "item_kind": stringField,
					"owner": stringField, "message_source": stringField,
					"message_target": stringField, "message_type_hint": stringField,
					"temporal_kind": stringField, "effect_kind": stringField,
					"effect_phase": stringField, "effect_outcome": stringField,
					"durability": stringField,
					"actor_role": map[string]any{"type": "string", "enum": []string{
						controlexperiment.ConsensusActorLeader, controlexperiment.ConsensusActorReplica,
						controlexperiment.ConsensusActorContender,
					}},
					"message_class": map[string]any{"type": "string", "enum": []string{
						controlexperiment.ConsensusMessageVote, controlexperiment.ConsensusMessageProposal,
						controlexperiment.ConsensusMessageReplication, controlexperiment.ConsensusMessageHeartbeat,
						controlexperiment.ConsensusMessageRecovery,
					}},
					"epoch_relation": map[string]any{"type": "string", "enum": []string{
						controlexperiment.ConsensusEpochStale, controlexperiment.ConsensusEpochCurrent,
						controlexperiment.ConsensusEpochFuture,
					}},
					"operation_state": map[string]any{"type": "string", "enum": []string{
						controlexperiment.ConsensusOperationNone, controlexperiment.ConsensusOperationInflight,
						controlexperiment.ConsensusOperationDecidedNotApplied,
					}},
				},
				"required": []string{"kind"},
			},
		},
	}
	plan := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"id": stringField,
			"steps": map[string]any{
				"type": "array", "minItems": 1, "maxItems": view.MaxSteps,
				"items": map[string]any{
					"type": "object", "additionalProperties": false,
					"properties": map[string]any{
						"id": stringField, "after_milestone": stringField, "selector": selector,
					},
					"required": []string{"id", "selector"},
				},
			},
		},
		"required": []string{"id", "steps"},
	}
	properties := map[string]any{
		"intent": map[string]any{"type": "string", "enum": view.AvailableIntents},
		"plan":   plan,
	}
	required := []string{"intent"}
	minimalPathPlan := len(view.AvailableIntents) == 1 &&
		(view.AvailableIntents[0] == controlexperiment.ScenarioIntentContinue ||
			view.AvailableIntents[0] == controlexperiment.ScenarioIntentRevise)
	if minimalPathPlan {
		required = append(required, "plan")
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": properties,
		// A single continue/revise phase uses the exact minimal shape above.
		// Multi-intent phases keep plan conditional in the trusted parser to
		// avoid provider-specific root oneOf behavior.
		"required": required,
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return openRouterStructuredOutput{}, err
	}
	return openRouterStructuredOutput{Name: scenarioInvestigationStructuredOutputName, Schema: encoded}, nil
}

// scenarioAgentRepairPrompt is used once after a charged response reaches the
// provider output limit. It is bound to the same trusted frontier as the failed
// call and omits repeated step evidence already summarized by ProgressDelta.
func scenarioAgentRepairPrompt(
	spec semantic.RiskWitnessSpec,
	view controlexperiment.ScenarioAgentView,
) (string, string, error) {
	if err := validateScenarioPromptInput(spec, view); err != nil {
		return "", "", err
	}
	frontier, err := newScenarioPromptFrontier(view)
	if err != nil {
		return "", "", err
	}
	type targetSurfaceView struct {
		TargetID       string                          `json:"target_id"`
		Nodes          []control.NodeID                `json:"nodes"`
		FaultAllowance controlexperiment.FaultEnvelope `json:"fault_allowance"`
	}
	type priorView struct {
		PreviousProposal    *controlexperiment.ScenarioInvestigationProposal `json:"previous_proposal,omitempty"`
		NaturalProgressStop string                                           `json:"natural_progress_stop,omitempty"`
		ProgressDelta       *controlexperiment.ScenarioProgressDelta         `json:"progress_delta,omitempty"`
	}
	var surface *targetSurfaceView
	if view.TargetSurface != nil {
		surface = &targetSurfaceView{
			TargetID:       view.TargetSurface.TargetID,
			Nodes:          append([]control.NodeID(nil), view.TargetSurface.Nodes...),
			FaultAllowance: view.TargetSurface.FaultAllowance,
		}
	}
	var prior *priorView
	if view.Prior != nil {
		prior = &priorView{
			PreviousProposal:    view.Prior.PreviousProposal,
			NaturalProgressStop: view.Prior.NaturalProgressStop,
			ProgressDelta:       view.Prior.ProgressDelta,
		}
	}
	input := struct {
		PromptVersion      string                                       `json:"prompt_version"`
		RepairReason       string                                       `json:"repair_reason"`
		AcceptedHypothesis *controlexperiment.AcceptedHypothesisContext `json:"accepted_hypothesis,omitempty"`
		TargetSurface      *targetSurfaceView                           `json:"target_surface,omitempty"`
		OrderedMilestones  []string                                     `json:"ordered_milestones"`
		Frontier           scenarioPromptFrontier                       `json:"action_frontier"`
		MaxSteps           int                                          `json:"max_steps"`
		DecisionAllowance  int                                          `json:"decision_allowance"`
		RemainingDecisions int                                          `json:"remaining_decisions"`
		AvailableIntents   []string                                     `json:"available_intents"`
		Prior              *priorView                                   `json:"prior_feedback,omitempty"`
	}{
		PromptVersion:      scenarioAgentPromptVersion,
		RepairReason:       controlexperiment.ScenarioAgentReasonResponseFinishLength,
		AcceptedHypothesis: view.AcceptedHypothesis,
		TargetSurface:      surface,
		OrderedMilestones:  append([]string(nil), view.OrderedMilestones...),
		Frontier:           frontier,
		MaxSteps:           view.MaxSteps, DecisionAllowance: view.DecisionAllowance,
		RemainingDecisions: view.RemainingDecisions,
		AvailableIntents:   append([]string(nil), view.AvailableIntents...),
		Prior:              prior,
	}
	encoded, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "The preceding charged Scenario response reached the provider output limit. " +
		"Return exactly one minimal ScenarioInvestigationProposal JSON object and no prose. " +
		"Use one intent from available_intents. For continue/revise, return exactly one plan with exactly one " +
		"strategic Action step selected from the supplied current action_frontier; omit every optional field not needed " +
		"to identify that Action. Do not repeat analysis, evidence, rationale, assertions, verdicts, budgets, or digests."
	user := "Repair only the truncated response against this unchanged trusted frontier. The previous response executed no " +
		"Runtime Action. A proposal outside the current frontier or available intent remains invalid. Frozen repair input JSON:\n" +
		string(encoded)
	return system, user, nil
}

func scenarioAgentPrompt(
	spec semantic.RiskWitnessSpec,
	view controlexperiment.ScenarioAgentView,
) (string, string, error) {
	if err := validateScenarioPromptInput(spec, view); err != nil {
		return "", "", err
	}
	frontier, err := newScenarioPromptFrontier(view)
	if err != nil {
		return "", "", err
	}
	promptView := view
	agentView := any(promptView)
	implementationContext := "Use only the accepted candidate, current trusted frontier, action semantics, and recent mechanical feedback. "
	if promptView.AcceptedHypothesis != nil {
		type targetSurfaceView struct {
			TargetID          string                                 `json:"target_id"`
			Nodes             []control.NodeID                       `json:"nodes"`
			Workload          controlexperiment.AgentWorkloadSurface `json:"workload"`
			FaultAllowance    controlexperiment.FaultEnvelope        `json:"fault_allowance"`
			ComposableActions []control.ActionKind                   `json:"composable_actions"`
		}
		type feedbackView struct {
			Outcome             string                                              `json:"outcome"`
			ReasonCode          string                                              `json:"reason_code,omitempty"`
			PreviousProposal    *controlexperiment.ScenarioInvestigationProposal    `json:"previous_proposal,omitempty"`
			ValidationIssues    []controlexperiment.ScenarioProposalValidationIssue `json:"validation_issues,omitempty"`
			CapabilityGaps      []controlexperiment.AgentCapabilityGap              `json:"capability_gaps,omitempty"`
			AllowedIntents      []string                                            `json:"allowed_intents,omitempty"`
			FailedStep          *controlexperiment.ScenarioStep                     `json:"failed_step,omitempty"`
			Steps               []scenarioPromptStepFeedback                        `json:"steps,omitempty"`
			NaturalProgress     []scenarioPromptStepFeedback                        `json:"natural_progress,omitempty"`
			NaturalProgressStop string                                              `json:"natural_progress_stop,omitempty"`
			ProgressDelta       *controlexperiment.ScenarioProgressDelta            `json:"progress_delta,omitempty"`
		}
		var surface *targetSurfaceView
		if promptView.TargetSurface != nil {
			surface = &targetSurfaceView{
				TargetID:       promptView.TargetSurface.TargetID,
				Nodes:          append([]control.NodeID(nil), promptView.TargetSurface.Nodes...),
				Workload:       promptView.TargetSurface.Workload,
				FaultAllowance: promptView.TargetSurface.FaultAllowance,
				ComposableActions: append([]control.ActionKind(nil),
					promptView.TargetSurface.Capabilities.ComposableActions...),
			}
		}
		var prior *feedbackView
		if promptView.Prior != nil {
			prior = &feedbackView{
				Outcome: promptView.Prior.Outcome, ReasonCode: promptView.Prior.ReasonCode,
				PreviousProposal: promptView.Prior.PreviousProposal,
				ValidationIssues: append([]controlexperiment.ScenarioProposalValidationIssue(nil),
					promptView.Prior.ValidationIssues...),
				CapabilityGaps:      append([]controlexperiment.AgentCapabilityGap(nil), promptView.Prior.CapabilityGaps...),
				AllowedIntents:      append([]string(nil), promptView.Prior.AllowedIntents...),
				FailedStep:          promptView.Prior.FailedStep,
				Steps:               compactScenarioPromptSteps(promptView.Prior.Steps),
				NaturalProgress:     compactScenarioPromptSteps(promptView.Prior.NaturalProgress),
				NaturalProgressStop: promptView.Prior.NaturalProgressStop,
				ProgressDelta:       promptView.Prior.ProgressDelta,
			}
		}
		type planningFocusView struct {
			NextMissingMilestone  string                                `json:"next_missing_milestone,omitempty"`
			ResolvedBindings      []semantic.RiskWitnessResolvedBinding `json:"resolved_bindings,omitempty"`
			LastAgentActionEffect *scenarioPromptStepFeedback           `json:"last_agent_action_effect,omitempty"`
		}
		focus := planningFocusView{
			NextMissingMilestone: promptView.Frontier.Progress.FirstMissingMilestone,
			ResolvedBindings: append(
				[]semantic.RiskWitnessResolvedBinding(nil),
				promptView.Frontier.Progress.ResolvedBindings...,
			),
		}
		if promptView.Prior != nil && len(promptView.Prior.Steps) > 0 {
			last := compactScenarioPromptSteps(
				promptView.Prior.Steps[len(promptView.Prior.Steps)-1:],
			)[0]
			focus.LastAgentActionEffect = &last
		}
		agentView = struct {
			PlanningFocus      planningFocusView                            `json:"planning_focus"`
			AcceptedHypothesis *controlexperiment.AcceptedHypothesisContext `json:"accepted_hypothesis"`
			TargetSurface      *targetSurfaceView                           `json:"target_surface,omitempty"`
			OrderedMilestones  []string                                     `json:"ordered_milestones"`
			Frontier           scenarioPromptFrontier                       `json:"action_frontier"`
			MaxSteps           int                                          `json:"max_steps"`
			DecisionAllowance  int                                          `json:"decision_allowance"`
			RemainingDecisions int                                          `json:"remaining_decisions"`
			AvailableIntents   []string                                     `json:"available_intents"`
			Prior              *feedbackView                                `json:"prior_feedback,omitempty"`
		}{
			PlanningFocus:      focus,
			AcceptedHypothesis: promptView.AcceptedHypothesis,
			TargetSurface:      surface, OrderedMilestones: promptView.OrderedMilestones,
			Frontier: frontier,
			MaxSteps: promptView.MaxSteps, DecisionAllowance: promptView.DecisionAllowance,
			RemainingDecisions: promptView.RemainingDecisions,
			AvailableIntents:   promptView.AvailableIntents,
			Prior:              prior,
		}
		implementationContext = "Use accepted_hypothesis as the investigated mechanism and executable witness. It is an " +
			"Agent proposal accepted for execution, not a protocol fact or verdict. "
	}
	input := struct {
		PromptVersion  string   `json:"prompt_version"`
		SelectorFields []string `json:"selector_fields"`
		AgentView      any      `json:"agent_view"`
	}{
		PromptVersion: scenarioAgentPromptVersion,
		SelectorFields: []string{
			"action_id", "kind", "node", "item_kind", "owner", "message_source",
			"message_target", "message_type_hint", "temporal_kind", "effect_kind",
			"effect_phase", "effect_outcome", "durability", "actor_role",
			"message_class", "epoch_relation", "operation_state",
		},
		AgentView: agentView,
	}
	encoded, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "Return exactly one ScenarioInvestigationProposal JSON object and no prose. This is a single-path " +
		"investigation. Use only an intent listed in available_intents. For continue/revise the nested plan may use only " +
		"id, steps, and selector_fields listed in the input. abandon is a zero-Action hypothesis choice: provide only " +
		"intent=abandon and omit plan. action_id is valid only for an Action in the supplied current action_frontier. " +
		"The semantic fields embedded in action_frontier describe only their bound current Actions and grant no authority to invent Actions or facts. " +
		"The optional actor_role, message_class, epoch_relation, and operation_state selector fields may use only non-unknown " +
		"values present on that Action; they narrow the current frontier but do not create an Action. " +
		"message_type_hint, effect_phase, and effect_outcome are opaque target-declared values copied from the current " +
		"frontier; use only values present on an enabled Action. " +
		"Selector node, owner, message_source, and message_target values are node ID JSON strings such as n1, never " +
		"identity objects with node/incarnation fields. A stopped prior_feedback must be answered with intent=revise, not continue. " +
		"When prior_feedback.capability_gaps is present, the trusted Target surface proves those requested controls unavailable; " +
		"revise the plan using declared composable Actions and capability values, or abandon when allowed. A capability gap is not a verdict. " +
		"Never copy an ActionID from prior_feedback. Never add branches, controls, ablations, path selection, budgets, " +
		"faults, assertions, verdicts, or digests."
	if len(view.AvailableIntents) == 1 &&
		(view.AvailableIntents[0] == controlexperiment.ScenarioIntentContinue ||
			view.AvailableIntents[0] == controlexperiment.ScenarioIntentRevise) {
		system += " This phase permits only intent=" + view.AvailableIntents[0] +
			" with plan."
	}
	investigationGuidance := "The trusted coordinator has already derived continue versus revise from the previous mechanical result. " +
		"The current action_frontier is already restricted to strategic Actions that can instantiate planning_focus.next_missing_milestone. " +
		"Choose one of those current Actions; do not substitute a different enabled fault or create or refer to branches, controls, ablations, or path selection. " +
		"When abandon is available, use it only when progress_delta shows this hypothesis is no longer worth the remaining budget. "
	user := "Create one complete but bounded investigation proposal of at most max_steps that advances the supplied hypothesis. " +
		"Use target_surface as the authoritative current topology, workload, runtime and fault allowance. " +
		implementationContext +
		"A later step may use after_milestone only with an ID listed in ordered_milestones; the trusted " +
		"executor will advance ordinary effects, messages, and naturally due timers until that milestone is observed. " +
		"after_milestone is checked before its step: never attach a milestone whose observation requires that same step " +
		"or any later step. " +
		"A trusted concretizer requires " +
		"each selector to match exactly one current admissible Action. A completed prior_feedback means the trusted root has " +
		"advanced and this plan must continue from the supplied current frontier. A stopped prior_feedback includes the complete " +
		"previous_proposal and failed_step; use revise with a complete repaired plan from the current frontier. For selector " +
		"no-match or ambiguous feedback, match_count is the final candidate count and selector_trace shows the count after each " +
		"field is applied. The first zero-count field is a concrete conflict; a final count above one requires another stable " +
		"field from available_actions. " +
		investigationGuidance +
		"When present, prior_feedback.progress_delta is the compact trusted account of decisions, new milestones, the first missing " +
		"milestone, newly observed milestone evidence, transition novelty, Action counts, temporal callbacks versus actual logical-clock " +
		"advances, repeated scheduling-pattern depth, fault allowance/usage/remaining, available interventions, and recent " +
		"Action kinds. milestone_progress=milestone-stalled means selectors executed but no new milestone appeared; " +
		"natural_progress_stop distinguishes a returned client operation, a truly quiescent frontier, an episode-wide decision limit, and " +
		"natural-progress-slice-exhausted: the last value means only that one bounded public-progress slice ended while the remaining_decisions " +
		"budget is still available, not that the hypothesis stalled. Use these mechanical " +
		"facts to continue, revise, change the intervention, or abandon. " +
		"Repetition and a missing milestone are not protocol verdicts. " +
		"Before selecting the single strategic Action, start with planning_focus and action_frontier.coordination. Check the Action source/target against " +
		"resolved_bindings; whether it can advance next_missing_milestone; whether it resets or contradicts the current protocol state; " +
		"whether it is only housekeeping that trusted natural progress can perform; whether DuplicateMessage must be followed by delivery " +
		"to have the claimed effect; and whether a crashed receiver must first be restarted. Treat action_frontier.actions " +
		"as the current admissible choices to assess, not as trusted proof that every choice is causally useful. " +
		"Not selecting an enabled Action does not block it because trusted natural progress may execute it. A temporal-fired milestone " +
		"means one timer callback, not timeout expiry, unless later trusted milestone evidence establishes the protocol transition. Do not " +
		"continue a mechanism whose claimed stall or timeout contradicts the supplied mechanical steps; revise to an Action-supported path " +
		"or abandon it. " +
		"decision_allowance bounds this proposal plus its deterministic natural-progress slice; remaining_decisions is the " +
		"episode-wide successful Action budget still available. " +
		"A semantic kind=invoke selector may be prepared by the trusted Target when the configured workload has one invocation and current " +
		"evidence identifies exactly one coordinator; preparation still creates an ordinary enabled Runtime Action and otherwise returns no-match. " +
		"Frozen input JSON:\n" + string(encoded)
	if view.MaxSteps == 1 {
		system += " For every intent other than abandon, the nested plan must contain exactly one step."
	} else {
		system += " For every intent other than abandon and every step after the first, omit action_id and use stable semantic selector " +
			"fields because executing an " +
			"earlier step rebuilds the frontier and may invalidate every current ActionID."
	}
	return system, user, nil
}

func validateScenarioPromptInput(
	spec semantic.RiskWitnessSpec,
	view controlexperiment.ScenarioAgentView,
) error {
	if spec.Validate() != nil || view.Knowledge.Validate() != nil ||
		view.TargetSurface != nil && view.TargetSurface.Validate() != nil ||
		view.Hypothesis.Validate(
			view.Knowledge, spec, controlexperiment.ScenarioPlanningBackendID,
		) != nil || view.AcceptedHypothesis != nil &&
		view.AcceptedHypothesis.Validate(view.Knowledge, view.Hypothesis, spec) != nil ||
		view.Frontier.Validate(spec) != nil ||
		!scenarioPromptMilestonesMatch(spec, view.OrderedMilestones) ||
		view.Semantics.Validate(view.Frontier) != nil ||
		(view.MaxSteps <= 0 || view.MaxSteps > controlexperiment.ScenarioPlanMaxSteps ||
			view.DecisionAllowance < view.MaxSteps || view.RemainingDecisions < view.DecisionAllowance ||
			len(view.Frontier.Actions) == 0) {
		return errors.New("SCENARIO_AGENT_PROMPT_VIEW_INVALID")
	}
	return nil
}

func scenarioPromptMilestonesMatch(spec semantic.RiskWitnessSpec, values []string) bool {
	if len(values) != len(spec.Milestones) {
		return false
	}
	for index, milestone := range spec.Milestones {
		if values[index] != milestone.ID {
			return false
		}
	}
	return true
}

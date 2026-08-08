package agentcampaign

// This file implements the narrow interface used for blinded method
// experiments. It defines the only model-facing request shape and excludes
// full obligation and Driver metadata.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

const BlindVersion = 1

// BlindScope is supplied by the trusted experiment composition boundary.  Its
// TrialID is an opaque label: it must not encode a candidate, source change,
// trigger, or expected Oracle result.
type BlindScope struct {
	BenchmarkID     string `json:"benchmark_id"`
	BenchmarkDigest string `json:"benchmark_digest"`
	TrialID         string `json:"trial_id"`
}

func (scope BlindScope) Validate() error {
	if !safeAgentID.MatchString(scope.BenchmarkID) || !safeAgentID.MatchString(scope.TrialID) {
		return errors.New("blind benchmark and trial ids contain unsafe characters")
	}
	if len(scope.BenchmarkDigest) != 64 {
		return errors.New("blind benchmark digest must be a SHA-256 hex value")
	}
	for _, char := range scope.BenchmarkDigest {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return errors.New("blind benchmark digest must be lowercase hexadecimal")
		}
	}
	return nil
}

// BlindCapabilitySnapshot removes Driver/SUT identity and capability Detail.
// It contains only the supported, plan-relevant capability names and a digest
// over that projection.  The Agent never receives driver.Manifest.
type BlindCapabilitySnapshot struct {
	Digest       string       `json:"digest"`
	Capabilities []string     `json:"capabilities"`
	Inputs       []BlindInput `json:"inputs"`
}

// BlindInput is a Driver-declared input shape with all implementation detail
// removed. For protocol-input, ID is an opaque operation handle; it does not
// expose an Oracle, obligation, SUT identity, or trace fact.
type BlindInput struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	PayloadMode string `json:"payload_mode"`
}

func newBlindCapabilities(manifest driver.Manifest) (BlindCapabilitySnapshot, error) {
	seen := make(map[string]bool)
	for _, capability := range manifest.Capabilities {
		if capability.Supported && capability.ID != "" {
			seen[capability.ID] = true
		}
	}
	capabilities := make([]string, 0, len(seen))
	for capability := range seen {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	inputs := make([]BlindInput, 0, len(manifest.Inputs))
	for _, input := range manifest.Inputs {
		if input.ID == "" || input.Kind == "" {
			continue
		}
		inputs = append(inputs, BlindInput{ID: input.ID, Kind: string(input.Kind), PayloadMode: input.PayloadMode})
	}
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Kind != inputs[j].Kind {
			return inputs[i].Kind < inputs[j].Kind
		}
		return inputs[i].ID < inputs[j].ID
	})
	encoded, err := json.Marshal(struct {
		Version      int          `json:"version"`
		Capabilities []string     `json:"capabilities"`
		Inputs       []BlindInput `json:"inputs"`
	}{BlindVersion, capabilities, inputs})
	if err != nil {
		return BlindCapabilitySnapshot{}, err
	}
	return BlindCapabilitySnapshot{Digest: sha256Hex(encoded), Capabilities: capabilities, Inputs: inputs}, nil
}

// BlindDebt is the only coverage-debt information that crosses into an
// untrusted Planner. Ref is a deterministic opaque alias, not an obligation
// ID. Descriptions, trace predicates, monitors, requirements and evidence
// references stay in the trusted Coordinator.
type BlindDebt struct {
	Ref      string `json:"ref"`
	Category string `json:"category"`
	Risk     string `json:"risk"`
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
}

type BlindGenerationRequest struct {
	Version           int                     `json:"version"`
	CampaignID        string                  `json:"campaign_id"`
	Attempt           int                     `json:"attempt"`
	Scope             BlindScope              `json:"blind_scope"`
	Profile           ProfileScope            `json:"profile"`
	Capabilities      BlindCapabilitySnapshot `json:"capability_snapshot"`
	Progress          campaign.Progress       `json:"progress"`
	Debt              []BlindDebt             `json:"coverage_debt"`
	PreviousProposals []BlindProposal         `json:"previous_proposals,omitempty"`
	PreviousFindings  []BlindFinding          `json:"previous_findings,omitempty"`
	Remaining         RemainingBudget         `json:"remaining_budget"`
	Policy            DSLPolicy               `json:"dsl_policy"`
}

type BlindProposal struct {
	Version int           `json:"version"`
	ID      string        `json:"id"`
	Plan    testplan.Plan `json:"plan"`
}

func (proposal BlindProposal) Validate() error {
	if proposal.Version != BlindVersion || proposal.ID == "" || proposal.Plan.ID == "" {
		return errors.New("blind proposal version, id, and plan id are required")
	}
	return nil
}

// BlindFinding intentionally provides codes and counts, not an execution
// error string, Oracle text, evidence, trace, or real obligation identifier.
type BlindFinding struct {
	Code             string   `json:"code"`
	Attempt          int      `json:"attempt"`
	ProposalID       string   `json:"proposal_id,omitempty"`
	PlanID           string   `json:"plan_id,omitempty"`
	Targeted         []string `json:"targeted,omitempty"`
	NewlyCovered     []string `json:"newly_covered,omitempty"`
	RemainingTargets []string `json:"remaining_targets,omitempty"`
	ScoreBefore      float64  `json:"score_before,omitempty"`
	ScoreAfter       float64  `json:"score_after,omitempty"`
	CoveredBefore    int      `json:"covered_before,omitempty"`
	CoveredAfter     int      `json:"covered_after,omitempty"`
	Runs             int      `json:"runs,omitempty"`
	Decisions        int      `json:"decisions,omitempty"`
	WorkUnits        int      `json:"work_units,omitempty"`
	ReplayFailures   int      `json:"replay_failures,omitempty"`
	ExecutionErrors  int      `json:"execution_errors,omitempty"`
	OracleViolations int      `json:"oracle_violations,omitempty"`
}

type BlindPlanner interface {
	GenerateBlind(context.Context, BlindGenerationRequest) (*BlindProposal, error)
}

type BlindAuditProvider interface {
	LastBlindGenerationAudit() GenerationAudit
}

type BlindExecutionSummary struct {
	PlanID           string            `json:"plan_id"`
	Before           campaign.Progress `json:"before"`
	After            campaign.Progress `json:"after"`
	Runs             int               `json:"runs"`
	Decisions        int               `json:"decisions"`
	WorkUnits        int               `json:"work_units"`
	ExecutionError   bool              `json:"execution_error"`
	NewlyCovered     []string          `json:"newly_covered,omitempty"`
	ReplayFailures   int               `json:"replay_failures"`
	ExecutionErrors  int               `json:"execution_errors"`
	OracleViolations int               `json:"oracle_violations"`
}

type BlindRound struct {
	Attempt   int                    `json:"attempt"`
	Proposal  *BlindProposal         `json:"proposal,omitempty"`
	Audit     GenerationAudit        `json:"generation"`
	Execution *BlindExecutionSummary `json:"execution,omitempty"`
	Finding   BlindFinding           `json:"finding"`
}

// BlindReport is safe to persist as the Planner-visible campaign transcript.
// TrustedCampaign is intentionally excluded from JSON: it contains the
// private Driver identity, raw Ledger, trace references and monitor results.
type BlindReport struct {
	Version               int                     `json:"version"`
	Status                string                  `json:"status"`
	StopReason            string                  `json:"stop_reason"`
	Config                Config                  `json:"config"`
	Scope                 BlindScope              `json:"blind_scope"`
	Profile               ProfileScope            `json:"profile"`
	Capabilities          BlindCapabilitySnapshot `json:"capability_snapshot"`
	Final                 campaign.Progress       `json:"final"`
	TotalTokens           int                     `json:"total_tokens"`
	ConsecutiveNoProgress int                     `json:"consecutive_no_progress"`
	Rounds                []BlindRound            `json:"rounds"`
	Blackboard            BlackboardReport        `json:"blackboard"`
	TrustedCampaign       campaign.Report         `json:"-"`
	TrustedPlans          []testplan.Plan         `json:"-"`
}

// CoordinateBlind is the sole route from an untrusted blind proposal to the
// existing trusted Session. The alias map is private and reconstructed from
// the frozen Profile on each round; no API exposed to the Planner accepts raw
// obligation IDs.
func CoordinateBlind(
	ctx context.Context,
	profile coverage.Profile,
	manifest driver.Manifest,
	matcher coverage.SemanticMatcher,
	config Config,
	scope BlindScope,
	planner BlindPlanner,
	newAdapter campaign.AdapterFactory,
	options campaign.Options,
) (BlindReport, error) {
	if err := config.Validate(); err != nil {
		return BlindReport{}, err
	}
	if !safeAgentID.MatchString(config.ID) {
		return BlindReport{}, errors.New("agent campaign id contains unsafe characters")
	}
	if err := scope.Validate(); err != nil {
		return BlindReport{}, err
	}
	if planner == nil {
		return BlindReport{}, errors.New("blind planner is nil")
	}
	profileDigest, err := coverage.Digest(profile)
	if err != nil {
		return BlindReport{}, err
	}
	capabilities, err := newBlindCapabilities(manifest)
	if err != nil {
		return BlindReport{}, err
	}
	configDigest, err := blindCoordinatorDigest(config, profileDigest, scope, capabilities.Digest)
	if err != nil {
		return BlindReport{}, err
	}
	session, err := campaign.NewSession(profile, manifest, matcher, config.ID, configDigest, newAdapter, options)
	if err != nil {
		return BlindReport{}, err
	}
	board, err := newBlackboard(config.ID)
	if err != nil {
		return BlindReport{}, err
	}
	profileScope := ProfileScope{ID: profile.ID, Digest: profileDigest, Protocol: profile.Protocol, PSSID: profile.PSSID, Nodes: append([]string(nil), profile.Nodes...)}
	report := BlindReport{Version: BlindVersion, Status: StatusAttemptLimit, StopReason: "maximum attempts reached", Config: config, Scope: scope, Profile: profileScope, Capabilities: capabilities, Rounds: make([]BlindRound, 0, config.MaxAttempts)}
	seen, seenProposalIDs, seenPlanIDs := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	var findings []BlindFinding
	var proposals []BlindProposal
	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		status, reason := stopStatus(session.Report(), report.TotalTokens, report.ConsecutiveNoProgress, config)
		if status != "" {
			report.Status, report.StopReason = status, reason
			break
		}
		request, refs := buildBlindRequest(config, scope, profileScope, capabilities, session, attempt, report.TotalTokens, proposals, findings)
		if err := board.append(RecordRequest, attempt, request); err != nil {
			return BlindReport{}, err
		}
		proposal, generateErr := planner.GenerateBlind(ctx, request)
		round := BlindRound{Attempt: attempt, Proposal: cloneBlindProposal(proposal)}
		if provider, ok := planner.(BlindAuditProvider); ok {
			round.Audit = provider.LastBlindGenerationAudit()
			if round.Audit.Provider != "" {
				report.TotalTokens += round.Audit.TotalTokens
				if err := board.append(RecordAudit, attempt, round.Audit); err != nil {
					return BlindReport{}, err
				}
			}
		}
		if generateErr != nil {
			if ctx.Err() != nil {
				return BlindReport{}, ctx.Err()
			}
			round.Finding = BlindFinding{Code: FindingGeneratorError, Attempt: attempt}
			report.ConsecutiveNoProgress++
			findings = append(findings, round.Finding)
			if err := board.append(RecordFinding, attempt, round.Finding); err != nil {
				return BlindReport{}, err
			}
			report.Rounds = append(report.Rounds, round)
			continue
		}
		if err := board.append(RecordProposal, attempt, proposal); err != nil {
			return BlindReport{}, err
		}
		proposals = append(proposals, *cloneBlindProposal(proposal))
		if report.TotalTokens > config.MaxTotalTokens {
			round.Finding = BlindFinding{Code: FindingBudgetRejected, Attempt: attempt, ProposalID: proposal.ID, PlanID: proposal.Plan.ID, Targeted: append([]string(nil), proposal.Plan.Targets...)}
			report.ConsecutiveNoProgress++
			_ = board.append(RecordFinding, attempt, round.Finding)
			report.Rounds = append(report.Rounds, round)
			report.Status, report.StopReason = StatusTokenBudget, "generation token budget exhausted"
			break
		}
		resolved, behaviorDigest, validationErr := validateBlindProposal(profile, manifest, session, config, proposal, refs, seen, seenProposalIDs, seenPlanIDs)
		if proposal != nil {
			if proposal.ID != "" {
				seenProposalIDs[proposal.ID] = true
			}
			if proposal.Plan.ID != "" {
				seenPlanIDs[proposal.Plan.ID] = true
			}
		}
		if validationErr != nil {
			code := FindingProposalRejected
			if behaviorDigest != "" && seen[behaviorDigest] {
				code = FindingDuplicateProposal
			} else {
				var budgetFailure blindBudgetError
				if errors.As(validationErr, &budgetFailure) {
					code = FindingBudgetRejected
				}
			}
			if behaviorDigest != "" {
				seen[behaviorDigest] = true
			}
			round.Finding = BlindFinding{Code: code, Attempt: attempt, ProposalID: blindProposalID(proposal), PlanID: blindPlanID(proposal), Targeted: blindTargets(proposal)}
			report.ConsecutiveNoProgress++
			findings = append(findings, round.Finding)
			if err := board.append(RecordFinding, attempt, round.Finding); err != nil {
				return BlindReport{}, err
			}
			report.Rounds = append(report.Rounds, round)
			continue
		}
		seen[behaviorDigest] = true
		planReport, err := session.ExecutePlan(ctx, resolved)
		if err != nil {
			return BlindReport{}, err
		}
		report.TrustedPlans = append(report.TrustedPlans, cloneTrustedPlan(resolved))
		execution := blindExecutionSummary(summarizeExecution(planReport), refs)
		round.Execution = &execution
		if err := board.append(RecordExecution, attempt, execution); err != nil {
			return BlindReport{}, err
		}
		round.Finding = blindFindingFromExecution(attempt, *proposal, execution, session.Debts(), refs)
		if round.Finding.Code == FindingProgress {
			report.ConsecutiveNoProgress = 0
		} else {
			report.ConsecutiveNoProgress++
		}
		findings = append(findings, round.Finding)
		if err := board.append(RecordFinding, attempt, round.Finding); err != nil {
			return BlindReport{}, err
		}
		report.Rounds = append(report.Rounds, round)
	}
	if len(session.Debts()) == 0 {
		report.Status, report.StopReason = StatusComplete, "all actionable coverage debt was covered"
	} else if report.Status == StatusAttemptLimit {
		if status, reason := stopStatus(session.Report(), report.TotalTokens, report.ConsecutiveNoProgress, config); status != "" {
			report.Status, report.StopReason = status, reason
		}
	}
	report.TrustedCampaign, report.Final, report.Blackboard = session.Report(), session.Report().Final, board.report()
	if err := VerifyBlackboard(report.Blackboard); err != nil {
		return BlindReport{}, fmt.Errorf("verify final blind blackboard: %w", err)
	}
	return report, nil
}

func buildBlindRequest(config Config, scope BlindScope, profile ProfileScope, capabilities BlindCapabilitySnapshot, session *campaign.Session, attempt, usedTokens int, proposals []BlindProposal, findings []BlindFinding) (BlindGenerationRequest, map[string]string) {
	campaignReport := session.Report()
	remainingRuns, remainingDecisions := config.MaxTotalRuns-campaignReport.ChargedRuns, config.MaxTotalDecisions-campaignReport.ChargedDecisions
	debtViews, refs := blindDebtViews(scope, profile.Digest, session.Debts())
	maxPlanRuns := min(remainingRuns, testplan.MaxRunsPerPlan)
	maxPlanDecisions := min(remainingDecisions, testplan.MaxDecisionsPerRun*maxPlanRuns)
	return BlindGenerationRequest{Version: BlindVersion, CampaignID: config.ID, Attempt: attempt, Scope: scope, Profile: profile, Capabilities: capabilities, Progress: campaignReport.Final, Debt: debtViews, PreviousProposals: append([]BlindProposal(nil), proposals...), PreviousFindings: append([]BlindFinding(nil), findings...), Remaining: RemainingBudget{Attempts: config.MaxAttempts - attempt + 1, Runs: remainingRuns, Decisions: remainingDecisions, Tokens: config.MaxTotalTokens - usedTokens, MaxPlanRuns: maxPlanRuns, MaxPlanDecisions: maxPlanDecisions}, Policy: defaultDSLPolicy(capabilities.Inputs)}, refs
}

func defaultDSLPolicy(inputs []BlindInput) DSLPolicy {
	kinds := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		kinds[input.Kind] = true
	}
	allowed := make([]string, 0, len(kinds))
	for kind := range kinds {
		allowed = append(allowed, kind)
	}
	sort.Strings(allowed)
	return DSLPolicy{AllowedInputs: allowed, ProtocolInputs: append([]BlindInput(nil), inputs...), AllowedPrepareOps: []string{"inject", "execute", "execute_optional", "drop", "duplicate", "capture_message", "execute_ref", "partition", "heal", "drain", "advance"}, AllowedStrategies: []string{"random", "dfs"}, DirectMessageInject: false, TransientSelectors: false, MaxTargets: testplan.MaxTargetsPerPlan, MaxPrepareActions: testplan.MaxPrepareActions, MaxStimuli: testplan.MaxStimuliPerPlan, MaxDuplicatesPerRun: 4}
}

func blindDebtViews(scope BlindScope, profileDigest string, debts []coverage.Debt) ([]BlindDebt, map[string]string) {
	views, refs := make([]BlindDebt, 0, len(debts)), make(map[string]string, len(debts))
	for _, debt := range debts {
		ref := blindDebtRef(scope, profileDigest, debt.Obligation.ID)
		refs[ref] = debt.Obligation.ID
		views = append(views, BlindDebt{Ref: ref, Category: debt.Obligation.Category, Risk: debt.Obligation.Risk, Status: debt.Status, Attempts: debt.Attempts})
	}
	return views, refs
}

func blindDebtRef(scope BlindScope, profileDigest, obligationID string) string {
	return "debt-" + sha256Hex([]byte("consensus-atlas/blind-planner-v1\x00"+scope.BenchmarkID+"\x00"+scope.BenchmarkDigest+"\x00"+scope.TrialID+"\x00"+profileDigest+"\x00"+obligationID))
}

func validateBlindProposal(profile coverage.Profile, manifest driver.Manifest, session *campaign.Session, config Config, proposal *BlindProposal, refs map[string]string, seen map[string]bool, seenProposalIDs map[string]bool, seenPlanIDs map[string]bool) (testplan.Plan, string, error) {
	if proposal == nil {
		return testplan.Plan{}, "", errors.New("planner returned a nil blind proposal")
	}
	if err := proposal.Validate(); err != nil {
		return testplan.Plan{}, "", err
	}
	if !safeAgentID.MatchString(proposal.ID) {
		return testplan.Plan{}, "", errors.New("blind proposal id contains unsafe characters")
	}
	resolved := proposal.Plan
	resolved.Targets = make([]string, len(proposal.Plan.Targets))
	for index, ref := range proposal.Plan.Targets {
		id, ok := refs[ref]
		if !ok {
			return testplan.Plan{}, "", errors.New("blind proposal contains an unknown target reference")
		}
		resolved.Targets[index] = id
	}
	behaviorDigest, err := planBehaviorDigest(resolved)
	if err != nil {
		return testplan.Plan{}, "", err
	}
	if seenProposalIDs[proposal.ID] {
		return testplan.Plan{}, behaviorDigest, errors.New("blind proposal id was already submitted")
	}
	if seenPlanIDs[proposal.Plan.ID] {
		return testplan.Plan{}, behaviorDigest, errors.New("blind plan id was already submitted")
	}
	if seen[behaviorDigest] {
		return testplan.Plan{}, behaviorDigest, errors.New("blind proposal repeats a previously evaluated plan behavior")
	}
	if _, err := testplan.Concretize(profile, resolved); err != nil {
		return testplan.Plan{}, behaviorDigest, err
	}
	if err := testplan.ValidateInputCapabilities(resolved, manifest); err != nil {
		return testplan.Plan{}, behaviorDigest, err
	}
	if resolved.Search.Config.Runs > config.MaxTotalRuns-session.Report().ChargedRuns {
		return testplan.Plan{}, behaviorDigest, budgetError("run", resolved.Search.Config.Runs, config.MaxTotalRuns-session.Report().ChargedRuns)
	}
	if resolved.Search.Config.TargetDecisionBudget() > config.MaxTotalDecisions-session.Report().ChargedDecisions {
		return testplan.Plan{}, behaviorDigest, budgetError("decision", resolved.Search.Config.TargetDecisionBudget(), config.MaxTotalDecisions-session.Report().ChargedDecisions)
	}
	return resolved, behaviorDigest, nil
}

func blindExecutionSummary(summary ExecutionSummary, refs map[string]string) BlindExecutionSummary {
	return BlindExecutionSummary{PlanID: summary.PlanID, Before: summary.Before, After: summary.After, Runs: summary.Runs, Decisions: summary.Decisions, WorkUnits: summary.WorkUnits, ExecutionError: summary.ExecutionError != "", NewlyCovered: refsForIDs(summary.NewlyCovered, refs), ReplayFailures: summary.ReplayFailures, ExecutionErrors: summary.ExecutionErrors, OracleViolations: summary.OracleViolations}
}

func blindFindingFromExecution(attempt int, proposal BlindProposal, execution BlindExecutionSummary, debts []coverage.Debt, refs map[string]string) BlindFinding {
	code := FindingNoProgress
	if execution.ExecutionError {
		code = FindingPlanError
	} else if len(execution.NewlyCovered) != 0 {
		code = FindingProgress
	}
	remainingIDs := make(map[string]bool, len(debts))
	for _, debt := range debts {
		remainingIDs[debt.Obligation.ID] = true
	}
	remaining := make([]string, 0, len(proposal.Plan.Targets))
	for _, ref := range proposal.Plan.Targets {
		if id, ok := refs[ref]; ok && remainingIDs[id] {
			remaining = append(remaining, ref)
		}
	}
	return BlindFinding{Code: code, Attempt: attempt, ProposalID: proposal.ID, PlanID: proposal.Plan.ID, Targeted: append([]string(nil), proposal.Plan.Targets...), NewlyCovered: append([]string(nil), execution.NewlyCovered...), RemainingTargets: remaining, ScoreBefore: execution.Before.Score, ScoreAfter: execution.After.Score, CoveredBefore: execution.Before.Covered, CoveredAfter: execution.After.Covered, Runs: execution.Runs, Decisions: execution.Decisions, WorkUnits: execution.WorkUnits, ReplayFailures: execution.ReplayFailures, ExecutionErrors: execution.ExecutionErrors, OracleViolations: execution.OracleViolations}
}

func refsForIDs(ids []string, refs map[string]string) []string {
	byID := make(map[string]string, len(refs))
	for ref, id := range refs {
		byID[id] = ref
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if ref, ok := byID[id]; ok {
			result = append(result, ref)
		}
	}
	sort.Strings(result)
	return result
}

func blindCoordinatorDigest(config Config, profileDigest string, scope BlindScope, capabilityDigest string) (string, error) {
	encoded, err := json.Marshal(struct {
		Version          int        `json:"version"`
		Config           Config     `json:"config"`
		ProfileDigest    string     `json:"profile_digest"`
		Scope            BlindScope `json:"blind_scope"`
		CapabilityDigest string     `json:"capability_digest"`
	}{BlindVersion, config, profileDigest, scope, capabilityDigest})
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func cloneBlindProposal(proposal *BlindProposal) *BlindProposal {
	if proposal == nil {
		return nil
	}
	encoded, err := json.Marshal(proposal)
	if err != nil {
		return nil
	}
	var cloned BlindProposal
	if json.Unmarshal(encoded, &cloned) != nil {
		return nil
	}
	return &cloned
}

func blindProposalID(proposal *BlindProposal) string {
	if proposal == nil {
		return ""
	}
	return proposal.ID
}
func blindPlanID(proposal *BlindProposal) string {
	if proposal == nil {
		return ""
	}
	return proposal.Plan.ID
}
func blindTargets(proposal *BlindProposal) []string {
	if proposal == nil {
		return nil
	}
	return append([]string(nil), proposal.Plan.Targets...)
}

package agentcampaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

var safeAgentID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func Coordinate(
	ctx context.Context,
	profile coverage.Profile,
	manifest driver.Manifest,
	matcher coverage.SemanticMatcher,
	config Config,
	planner Planner,
	newAdapter campaign.AdapterFactory,
	options campaign.Options,
) (Report, error) {
	if err := config.Validate(); err != nil {
		return Report{}, err
	}
	if !safeAgentID.MatchString(config.ID) {
		return Report{}, errors.New("agent campaign id contains unsafe characters")
	}
	if planner == nil {
		return Report{}, errors.New("agent campaign planner is nil")
	}
	profileDigest, err := coverage.Digest(profile)
	if err != nil {
		return Report{}, err
	}
	configDigest, err := coordinatorDigest(config, profileDigest)
	if err != nil {
		return Report{}, err
	}
	session, err := campaign.NewSession(profile, manifest, matcher, config.ID, configDigest, newAdapter, options)
	if err != nil {
		return Report{}, err
	}
	board, err := newBlackboard(config.ID)
	if err != nil {
		return Report{}, err
	}
	scope := ProfileScope{
		ID: profile.ID, Digest: profileDigest, Protocol: profile.Protocol,
		PSSID: profile.PSSID, Nodes: append([]string(nil), profile.Nodes...),
	}
	report := Report{
		Version: Version, Status: StatusAttemptLimit, StopReason: "maximum attempts reached",
		Config: config, Profile: scope, Rounds: make([]Round, 0, config.MaxAttempts),
	}
	seen := make(map[string]bool)
	seenProposalIDs := make(map[string]bool)
	seenPlanIDs := make(map[string]bool)
	var findings []Finding
	var proposals []Proposal
	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		status, reason := stopStatus(session.Report(), report.TotalTokens, report.ConsecutiveNoProgress, config)
		if status != "" {
			report.Status, report.StopReason = status, reason
			break
		}
		request := buildRequest(config, scope, manifest, session, attempt, report.TotalTokens, proposals, findings)
		if err := board.append(RecordRequest, attempt, request); err != nil {
			return Report{}, err
		}
		proposal, generateErr := planner.Generate(ctx, request)
		round := Round{Attempt: attempt, Proposal: cloneProposal(proposal)}
		if provider, ok := planner.(AuditProvider); ok {
			round.Audit = provider.LastGenerationAudit()
			if round.Audit.Provider != "" {
				report.TotalTokens += round.Audit.TotalTokens
				if err := board.append(RecordAudit, attempt, round.Audit); err != nil {
					return Report{}, err
				}
			}
		}
		if generateErr != nil {
			if ctx.Err() != nil {
				return Report{}, ctx.Err()
			}
			round.Finding = Finding{
				Code: FindingGeneratorError, Attempt: attempt,
				Message: generateErr.Error(),
			}
			report.ConsecutiveNoProgress++
			findings = append(findings, round.Finding)
			if err := board.append(RecordFinding, attempt, round.Finding); err != nil {
				return Report{}, err
			}
			report.Rounds = append(report.Rounds, round)
			continue
		}
		if err := board.append(RecordProposal, attempt, proposal); err != nil {
			return Report{}, err
		}
		proposals = append(proposals, *cloneProposal(proposal))
		if report.TotalTokens > config.MaxTotalTokens {
			round.Finding = Finding{
				Code: FindingBudgetRejected, Attempt: attempt, ProposalID: proposal.ID, PlanID: proposal.Plan.ID,
				Message: fmt.Sprintf("generation raised token usage to %d above limit %d", report.TotalTokens, config.MaxTotalTokens),
			}
			report.ConsecutiveNoProgress++
			findings = append(findings, round.Finding)
			_ = board.append(RecordFinding, attempt, round.Finding)
			report.Rounds = append(report.Rounds, round)
			report.Status, report.StopReason = StatusTokenBudget, "generation token budget exhausted"
			break
		}
		behaviorDigest, validationErr := validateProposal(
			profile, session, config, proposal, seen, seenProposalIDs, seenPlanIDs,
		)
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
				var budgetFailure proposalBudgetError
				if errors.As(validationErr, &budgetFailure) {
					code = FindingBudgetRejected
				}
			}
			if behaviorDigest != "" {
				seen[behaviorDigest] = true
			}
			round.Finding = Finding{
				Code: code, Attempt: attempt, ProposalID: proposal.ID, PlanID: proposal.Plan.ID,
				Message: validationErr.Error(), Targeted: append([]string(nil), proposal.Plan.Targets...),
			}
			report.ConsecutiveNoProgress++
			findings = append(findings, round.Finding)
			if err := board.append(RecordFinding, attempt, round.Finding); err != nil {
				return Report{}, err
			}
			report.Rounds = append(report.Rounds, round)
			continue
		}
		seen[behaviorDigest] = true
		planReport, err := session.ExecutePlan(ctx, proposal.Plan)
		if err != nil {
			return Report{}, err
		}
		execution := summarizeExecution(planReport)
		round.Execution = &execution
		if err := board.append(RecordExecution, attempt, execution); err != nil {
			return Report{}, err
		}
		round.Finding = findingFromExecution(attempt, *proposal, execution, session.Debts())
		if round.Finding.Code == FindingProgress {
			report.ConsecutiveNoProgress = 0
		} else {
			report.ConsecutiveNoProgress++
		}
		findings = append(findings, round.Finding)
		if err := board.append(RecordFinding, attempt, round.Finding); err != nil {
			return Report{}, err
		}
		report.Rounds = append(report.Rounds, round)
	}
	if len(session.Debts()) == 0 {
		report.Status, report.StopReason = StatusComplete, "all actionable coverage debt was covered"
	} else if report.Status == StatusAttemptLimit {
		status, reason := stopStatus(session.Report(), report.TotalTokens, report.ConsecutiveNoProgress, config)
		if status != "" {
			report.Status, report.StopReason = status, reason
		}
	}
	report.Campaign = session.Report()
	report.Blackboard = board.report()
	if err := VerifyBlackboard(report.Blackboard); err != nil {
		return Report{}, fmt.Errorf("verify final blackboard: %w", err)
	}
	return report, nil
}

func buildRequest(
	config Config, scope ProfileScope, manifest driver.Manifest, session *campaign.Session,
	attempt, usedTokens int, proposals []Proposal, findings []Finding,
) GenerationRequest {
	campaignReport := session.Report()
	remainingRuns := config.MaxTotalRuns - campaignReport.ChargedRuns
	remainingDecisions := config.MaxTotalDecisions - campaignReport.ChargedDecisions
	views := make([]DebtView, 0, len(session.Debts()))
	for _, debt := range session.Debts() {
		views = append(views, DebtView{Obligation: debt.Obligation, Status: debt.Status, Attempts: debt.Attempts})
	}
	maxPlanRuns := remainingRuns
	if maxPlanRuns > testplan.MaxRunsPerPlan {
		maxPlanRuns = testplan.MaxRunsPerPlan
	}
	maxPlanDecisions := remainingDecisions
	if maxPlanDecisions > testplan.MaxDecisionsPerRun*maxPlanRuns {
		maxPlanDecisions = testplan.MaxDecisionsPerRun * maxPlanRuns
	}
	return GenerationRequest{
		Version: Version, CampaignID: config.ID, Attempt: attempt, Profile: scope, Manifest: manifest,
		Progress: campaignReport.Final, Debt: views,
		PreviousProposals: append([]Proposal(nil), proposals...),
		PreviousFindings:  append([]Finding(nil), findings...),
		Remaining: RemainingBudget{
			Attempts: config.MaxAttempts - attempt + 1, Runs: remainingRuns,
			Decisions: remainingDecisions, Tokens: config.MaxTotalTokens - usedTokens,
			MaxPlanRuns: maxPlanRuns, MaxPlanDecisions: maxPlanDecisions,
		},
		Policy: DSLPolicy{
			AllowedInputs:     []string{"campaign", "propose", "timeout", "crash", "restart"},
			AllowedPrepareOps: []string{"inject", "execute", "drop", "duplicate", "partition", "heal", "drain", "advance"},
			AllowedStrategies: []string{"random", "dfs"}, DirectMessageInject: false, TransientSelectors: false,
			MaxTargets: testplan.MaxTargetsPerPlan, MaxPrepareActions: testplan.MaxPrepareActions,
			MaxStimuli: testplan.MaxStimuliPerPlan, MaxDuplicatesPerRun: 4,
		},
	}
}

func validateProposal(
	profile coverage.Profile,
	session *campaign.Session,
	config Config,
	proposal *Proposal,
	seenBehavior map[string]bool,
	seenProposalIDs map[string]bool,
	seenPlanIDs map[string]bool,
) (string, error) {
	if proposal == nil {
		return "", errors.New("planner returned a nil proposal")
	}
	if err := proposal.Validate(); err != nil {
		return "", err
	}
	if !safeAgentID.MatchString(proposal.ID) {
		return "", errors.New("proposal id contains unsafe characters")
	}
	behaviorDigest, err := planBehaviorDigest(proposal.Plan)
	if err != nil {
		return "", err
	}
	if seenProposalIDs[proposal.ID] {
		return behaviorDigest, fmt.Errorf("proposal id %q was already submitted", proposal.ID)
	}
	if seenPlanIDs[proposal.Plan.ID] {
		return behaviorDigest, fmt.Errorf("plan id %q was already submitted", proposal.Plan.ID)
	}
	if seenBehavior[behaviorDigest] {
		return behaviorDigest, errors.New("proposal repeats a previously evaluated plan behavior")
	}
	if _, err := testplan.Concretize(profile, proposal.Plan); err != nil {
		return behaviorDigest, err
	}
	current := make(map[string]bool)
	for _, debt := range session.Debts() {
		current[debt.Obligation.ID] = true
	}
	for _, target := range proposal.Plan.Targets {
		if !current[target] {
			return behaviorDigest, fmt.Errorf("target %q is not current actionable coverage debt", target)
		}
	}
	campaignReport := session.Report()
	remainingRuns := config.MaxTotalRuns - campaignReport.ChargedRuns
	remainingDecisions := config.MaxTotalDecisions - campaignReport.ChargedDecisions
	if proposal.Plan.Search.Config.Runs > remainingRuns {
		return behaviorDigest, budgetError("run", proposal.Plan.Search.Config.Runs, remainingRuns)
	}
	if proposal.Plan.Search.Config.TargetDecisionBudget() > remainingDecisions {
		return behaviorDigest, budgetError("decision", proposal.Plan.Search.Config.TargetDecisionBudget(), remainingDecisions)
	}
	return behaviorDigest, nil
}

func planBehaviorDigest(plan testplan.Plan) (string, error) {
	copy := plan
	copy.ID = ""
	copy.Targets = append([]string(nil), plan.Targets...)
	sort.Strings(copy.Targets)
	// Seeds and opaque application values do not change the protocol-level
	// causal shape of a plan. Ignoring them prevents an Agent from evading
	// duplicate feedback by renaming data or resampling the same schedule
	// space. Distinct input kinds, nodes, action order and search policy remain
	// part of the digest.
	copy.Search.Config.Seed = 0
	for index := range copy.Prepare {
		if copy.Prepare[index].Kind == "propose" {
			copy.Prepare[index].Payload = json.RawMessage(`{"opaque":true}`)
		}
	}
	for index := range copy.Stimuli {
		if copy.Stimuli[index].Kind == "propose" {
			copy.Stimuli[index].Payload = json.RawMessage(`{"opaque":true}`)
		}
	}
	encoded, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func summarizeExecution(report campaign.PlanReport) ExecutionSummary {
	summary := ExecutionSummary{
		PlanID: report.ID, Before: report.Before, After: report.After,
		Runs: len(report.Runs), Decisions: report.ChargedDecisions, ExecutionError: report.ExecutionError,
	}
	seen := make(map[string]bool)
	for _, run := range report.Runs {
		if !run.ReplayStable {
			summary.ReplayFailures++
		}
		if run.Explorer.ExecutionError != "" {
			summary.ExecutionErrors++
		}
		summary.OracleViolations += len(run.Oracle.Violations)
		for _, id := range run.NewlyCovered {
			if !seen[id] {
				seen[id] = true
				summary.NewlyCovered = append(summary.NewlyCovered, id)
			}
		}
	}
	sort.Strings(summary.NewlyCovered)
	return summary
}

func findingFromExecution(attempt int, proposal Proposal, execution ExecutionSummary, debts []coverage.Debt) Finding {
	code, message := FindingNoProgress, "plan executed but produced no new strong coverage evidence"
	if execution.ExecutionError != "" {
		code, message = FindingPlanError, execution.ExecutionError
	} else if len(execution.NewlyCovered) != 0 {
		code = FindingProgress
		message = fmt.Sprintf("plan produced %d newly covered obligations", len(execution.NewlyCovered))
	}
	remaining := make(map[string]bool)
	for _, debt := range debts {
		remaining[debt.Obligation.ID] = true
	}
	var remainingTargets []string
	for _, target := range proposal.Plan.Targets {
		if remaining[target] {
			remainingTargets = append(remainingTargets, target)
		}
	}
	return Finding{
		Code: code, Attempt: attempt, ProposalID: proposal.ID, PlanID: proposal.Plan.ID, Message: message,
		Targeted: append([]string(nil), proposal.Plan.Targets...), NewlyCovered: append([]string(nil), execution.NewlyCovered...),
		RemainingTargets: remainingTargets, ScoreBefore: execution.Before.Score, ScoreAfter: execution.After.Score,
		CoveredBefore: execution.Before.Covered, CoveredAfter: execution.After.Covered,
		Runs: execution.Runs, Decisions: execution.Decisions, ReplayFailures: execution.ReplayFailures,
		ExecutionErrors: execution.ExecutionErrors, OracleViolations: execution.OracleViolations,
	}
}

func stopStatus(report campaign.Report, tokens, noProgress int, config Config) (string, string) {
	if report.Final.Debt == 0 {
		return StatusComplete, "all actionable coverage debt was covered"
	}
	if noProgress >= config.MaxNoProgress {
		return StatusNoProgress, "consecutive no-progress limit reached"
	}
	if report.ChargedRuns >= config.MaxTotalRuns {
		return StatusRunBudget, "run budget exhausted"
	}
	if report.ChargedDecisions >= config.MaxTotalDecisions {
		return StatusDecisionBudget, "decision budget exhausted"
	}
	if tokens >= config.MaxTotalTokens {
		return StatusTokenBudget, "generation token budget exhausted"
	}
	return "", ""
}

func coordinatorDigest(config Config, profileDigest string) (string, error) {
	encoded, err := json.Marshal(struct {
		Version       int    `json:"version"`
		Config        Config `json:"config"`
		ProfileDigest string `json:"profile_digest"`
	}{Version, config, profileDigest})
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

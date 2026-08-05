// Package campaign executes bounded test plans while keeping all coverage
// decisions inside the trusted Coverage Ledger.
package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/explore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

const ReportVersion = 1

type AdapterFactory func() (adapter.Adapter, error)

type Options struct {
	Artifact string
}

type Progress struct {
	Score       float64 `json:"score"`
	Covered     int     `json:"covered"`
	Total       int     `json:"total"`
	Unsupported int     `json:"unsupported"`
	Debt        int     `json:"actionable_debt"`
}

type RunReport struct {
	ID               string            `json:"id"`
	DeclaredTargets  []string          `json:"declared_targets"`
	ActiveTargets    []string          `json:"active_targets,omitempty"`
	ReplayStable     bool              `json:"replay_stable"`
	ReplayError      string            `json:"replay_error,omitempty"`
	AcceptedEvidence bool              `json:"accepted_evidence"`
	NewlyCovered     []string          `json:"newly_covered,omitempty"`
	After            Progress          `json:"after"`
	Oracle           oracle.Result     `json:"oracle"`
	Explorer         explore.RunResult `json:"explorer"`
}

type PlanReport struct {
	ID               string                `json:"id"`
	DeclaredTargets  []string              `json:"declared_targets"`
	ActiveAtStart    []string              `json:"active_at_start,omitempty"`
	Skipped          bool                  `json:"skipped"`
	SkipReason       string                `json:"skip_reason,omitempty"`
	ExecutionError   string                `json:"execution_error,omitempty"`
	Concrete         testplan.ConcretePlan `json:"concrete"`
	Before           Progress              `json:"before"`
	After            Progress              `json:"after"`
	TargetDecisions  int                   `json:"target_decisions"`
	ChargedDecisions int                   `json:"charged_decisions"`
	BudgetReached    bool                  `json:"budget_reached"`
	StopReason       string                `json:"stop_reason,omitempty"`
	SetupTrace       []core.TraceRecord    `json:"setup_trace,omitempty"`
	Runs             []RunReport           `json:"runs,omitempty"`
}

type Report struct {
	Version          int                   `json:"version"`
	SuiteID          string                `json:"suite_id"`
	SuiteDigest      string                `json:"suite_digest"`
	ProfileID        string                `json:"profile_id"`
	ProfileDigest    string                `json:"profile_digest"`
	Protocol         string                `json:"protocol"`
	PSSID            string                `json:"pss_id,omitempty"`
	Manifest         driver.Manifest       `json:"manifest"`
	Initial          Progress              `json:"initial"`
	Final            Progress              `json:"final"`
	ChargedRuns      int                   `json:"charged_runs"`
	ChargedDecisions int                   `json:"charged_decisions"`
	Plans            []PlanReport          `json:"plans"`
	Ledger           coverage.LedgerReport `json:"coverage_ledger"`
	Debt             []coverage.Debt       `json:"coverage_debt"`
}

// Session owns one private Ledger across incrementally proposed plans. Agent
// orchestration receives only detached snapshots and can mutate coverage only
// by executing a validated plan through ExecutePlan.
type Session struct {
	id               string
	digest           string
	profile          coverage.Profile
	profileDigest    string
	manifest         driver.Manifest
	newAdapter       AdapterFactory
	options          Options
	ledger           *coverage.Ledger
	initial          Progress
	plans            []PlanReport
	planIDs          map[string]bool
	chargedRuns      int
	chargedDecisions int
}

func NewSession(
	profile coverage.Profile,
	manifest driver.Manifest,
	matcher coverage.SemanticMatcher,
	id, digest string,
	newAdapter AdapterFactory,
	options Options,
) (*Session, error) {
	if newAdapter == nil || id == "" || digest == "" {
		return nil, errors.New("campaign id, digest, and adapter factory are required")
	}
	profileDigest, err := coverage.Digest(profile)
	if err != nil {
		return nil, err
	}
	var ledgerOptions []coverage.LedgerOption
	if matcher != nil {
		ledgerOptions = append(ledgerOptions, coverage.WithSemanticMatcher(matcher))
	}
	ledger, err := coverage.NewLedger(profile, manifest, ledgerOptions...)
	if err != nil {
		return nil, fmt.Errorf("create coverage ledger: %w", err)
	}
	session := &Session{
		id: id, digest: digest, profile: profile, profileDigest: profileDigest,
		manifest: manifest, newAdapter: newAdapter, options: options, ledger: ledger,
		planIDs: make(map[string]bool),
	}
	session.initial = progress(ledger)
	return session, nil
}

func Run(
	ctx context.Context,
	profile coverage.Profile,
	manifest driver.Manifest,
	matcher coverage.SemanticMatcher,
	suite testplan.Suite,
	newAdapter AdapterFactory,
	options Options,
) (Report, error) {
	if err := suite.Validate(profile); err != nil {
		return Report{}, fmt.Errorf("validate test plan suite: %w", err)
	}
	suiteDigest, err := testplan.Digest(suite)
	if err != nil {
		return Report{}, err
	}
	session, err := NewSession(profile, manifest, matcher, suite.ID, suiteDigest, newAdapter, options)
	if err != nil {
		return Report{}, err
	}
	for _, proposed := range suite.Plans {
		if _, err := session.ExecutePlan(ctx, proposed); err != nil {
			return Report{}, err
		}
	}
	return session.Report(), nil
}

func (session *Session) ExecutePlan(ctx context.Context, proposed testplan.Plan) (PlanReport, error) {
	if session == nil || session.ledger == nil {
		return PlanReport{}, errors.New("campaign session is nil")
	}
	if err := ctx.Err(); err != nil {
		return PlanReport{}, err
	}
	if session.planIDs[proposed.ID] {
		return PlanReport{}, fmt.Errorf("campaign plan id %q was already executed", proposed.ID)
	}
	concrete, err := testplan.Concretize(session.profile, proposed)
	if err != nil {
		return PlanReport{}, fmt.Errorf("concretize plan %s: %w", proposed.ID, err)
	}
	session.planIDs[proposed.ID] = true
	active := activeTargets(session.ledger.Debts(), proposed.Targets)
	planReport := PlanReport{
		ID: proposed.ID, DeclaredTargets: append([]string(nil), proposed.Targets...),
		ActiveAtStart: active, Concrete: concrete, Before: progress(session.ledger),
	}
	if len(active) == 0 {
		planReport.Skipped = true
		planReport.SkipReason = "all declared targets were already covered"
		planReport.After = planReport.Before
		session.appendPlan(planReport)
		return clonePlanReport(planReport), nil
	}
	searcher, err := explore.New(concrete.Search.Strategy)
	if err != nil {
		return PlanReport{}, err
	}
	factory := func(factoryCtx context.Context) (*engine.Engine, error) {
		protocolAdapter, err := session.newAdapter()
		if err != nil {
			return nil, err
		}
		execution := engine.New(protocolAdapter)
		if err := scenario.Run(factoryCtx, execution, concrete.Setup); err != nil {
			return nil, fmt.Errorf("plan %s setup: %w", proposed.ID, err)
		}
		if err := execution.CheckConformance(); err != nil {
			return nil, fmt.Errorf("plan %s setup conformance: %w", proposed.ID, err)
		}
		return execution, nil
	}
	explored, err := searcher.Explore(ctx, factory, concrete.Search.Config)
	if err != nil {
		if ctx.Err() != nil {
			return PlanReport{}, ctx.Err()
		}
		planReport.ExecutionError = err.Error()
		planReport.After = progress(session.ledger)
		session.appendPlan(planReport)
		return clonePlanReport(planReport), nil
	}
	if len(explored.Runs) == 0 {
		return PlanReport{}, fmt.Errorf("plan %s explorer produced no runs", proposed.ID)
	}
	planReport.TargetDecisions = explored.TargetDecisionBudget
	planReport.ChargedDecisions = explored.ChargedDecisions
	planReport.BudgetReached = explored.BudgetReached
	planReport.StopReason = explored.StopReason
	for _, current := range explored.Runs {
		if planReport.SetupTrace == nil {
			planReport.SetupTrace = append([]core.TraceRecord(nil), current.SetupTrace...)
		}
		runID := fmt.Sprintf("%s/%s/run-%d", session.id, proposed.ID, current.Run)
		replayStable := true
		replayError := ""
		if _, replayErr := explore.ReplayRun(ctx, factory, concrete.Search.Config.Actions, current); replayErr != nil {
			replayStable = false
			replayError = replayErr.Error()
		}
		checked := oracle.Check(current.FullTrace, oracle.TraceIntegrity{}, oracle.Agreement{})
		active = activeTargets(session.ledger.Debts(), proposed.Targets)
		before := coveredSet(session.ledger.Report())
		accepted := replayStable && current.Conform && current.ExecutionError == ""
		if err := session.ledger.AddRun(session.profile, coverage.RunEvidence{
			ID: runID, Scenario: concrete.Setup.Name, Artifact: session.options.Artifact,
			Targets: active, Trace: current.FullTrace, Oracle: checked,
			ReplayStable: replayStable, Conformant: current.Conform && current.ExecutionError == "",
			Manifest: session.manifest,
		}); err != nil {
			return PlanReport{}, fmt.Errorf("record plan %s run %d: %w", proposed.ID, current.Run, err)
		}
		planReport.Runs = append(planReport.Runs, RunReport{
			ID: runID, DeclaredTargets: append([]string(nil), proposed.Targets...), ActiveTargets: active,
			ReplayStable: replayStable, ReplayError: replayError, AcceptedEvidence: accepted,
			NewlyCovered: newlyCovered(before, session.ledger.Report()), After: progress(session.ledger), Oracle: checked,
			Explorer: current,
		})
		session.chargedRuns++
		session.chargedDecisions += len(current.Decisions)
	}
	planReport.After = progress(session.ledger)
	session.appendPlan(planReport)
	return clonePlanReport(planReport), nil
}

func (session *Session) Debts() []coverage.Debt {
	if session == nil || session.ledger == nil {
		return nil
	}
	return session.ledger.Debts()
}

func (session *Session) Report() Report {
	if session == nil || session.ledger == nil {
		return Report{}
	}
	report := Report{
		Version: ReportVersion, SuiteID: session.id, SuiteDigest: session.digest,
		ProfileID: session.profile.ID, ProfileDigest: session.profileDigest,
		Protocol: session.profile.Protocol, PSSID: session.profile.PSSID,
		Manifest: session.manifest, Initial: session.initial, Final: progress(session.ledger),
		ChargedRuns: session.chargedRuns, ChargedDecisions: session.chargedDecisions,
		Plans: session.plans, Ledger: session.ledger.Report(), Debt: session.ledger.Debts(),
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return Report{}
	}
	var detached Report
	if json.Unmarshal(encoded, &detached) != nil {
		return Report{}
	}
	return detached
}

func (session *Session) appendPlan(report PlanReport) {
	session.plans = append(session.plans, clonePlanReport(report))
}

func clonePlanReport(report PlanReport) PlanReport {
	encoded, err := json.Marshal(report)
	if err != nil {
		return PlanReport{}
	}
	var detached PlanReport
	if json.Unmarshal(encoded, &detached) != nil {
		return PlanReport{}
	}
	return detached
}

func progress(ledger *coverage.Ledger) Progress {
	summary := ledger.Summary()
	return Progress{
		Score: summary.Score, Covered: summary.Covered, Total: summary.Total,
		Unsupported: summary.Unsupported, Debt: len(ledger.Debts()),
	}
}

func activeTargets(debts []coverage.Debt, declared []string) []string {
	wanted := make(map[string]bool, len(debts))
	for _, debt := range debts {
		wanted[debt.Obligation.ID] = true
	}
	active := make([]string, 0, len(declared))
	for _, target := range declared {
		if wanted[target] {
			active = append(active, target)
		}
	}
	return active
}

func coveredSet(report coverage.LedgerReport) map[string]bool {
	result := make(map[string]bool)
	for _, entry := range report.Entries {
		if entry.Status == coverage.LedgerCovered {
			result[entry.ObligationID] = true
		}
	}
	return result
}

func newlyCovered(before map[string]bool, after coverage.LedgerReport) []string {
	var result []string
	for _, entry := range after.Entries {
		if entry.Status == coverage.LedgerCovered && !before[entry.ObligationID] {
			result = append(result, entry.ObligationID)
		}
	}
	return result
}

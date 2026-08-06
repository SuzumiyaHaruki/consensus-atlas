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

const ReportVersion = 2

type AdapterFactory func() (adapter.Adapter, error)

type Options struct {
	Artifact string
	// Monitors are selected by the trusted CLI composition boundary, never by
	// an Agent-authored Test Plan. Empty preserves the generic Agreement
	// baseline for fixtures and callers without a protocol family.
	Monitors []oracle.Monitor
}

type Progress struct {
	Score       float64 `json:"score"`
	Covered     int     `json:"covered"`
	Total       int     `json:"total"`
	Unsupported int     `json:"unsupported"`
	Debt        int     `json:"actionable_debt"`
}

// PhaseCost separates repeated fresh-SUT/setup work from measurement work.
// WorkUnits charges one unit for every fresh SUT attempt, every scenario step
// (or Runtime event when a step drains several events), and every Explorer
// measurement event. It is an implementation-neutral execution budget, not a
// wall-clock or CPU-time estimate.
type PhaseCost struct {
	SetupAttempts      int `json:"setup_attempts"`
	SetupSteps         int `json:"setup_steps"`
	SetupRuntimeEvents int `json:"setup_runtime_events"`
	MeasurementEvents  int `json:"measurement_events"`
	WorkUnits          int `json:"work_units"`
}

type ExecutionCost struct {
	Primary PhaseCost `json:"primary"`
	Replay  PhaseCost `json:"replay"`
}

type RunReport struct {
	ID                   string            `json:"id"`
	DeclaredTargets      []string          `json:"declared_targets"`
	ActiveTargets        []string          `json:"active_targets,omitempty"`
	ReplayStable         bool              `json:"replay_stable"`
	ReplayError          string            `json:"replay_error,omitempty"`
	MonitorEvidenceError string            `json:"monitor_evidence_error,omitempty"`
	AcceptedEvidence     bool              `json:"accepted_evidence"`
	NewlyCovered         []string          `json:"newly_covered,omitempty"`
	After                Progress          `json:"after"`
	Oracle               oracle.Result     `json:"oracle"`
	Explorer             explore.RunResult `json:"explorer"`
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
	Cost             ExecutionCost         `json:"execution_cost"`
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
	Cost             ExecutionCost         `json:"execution_cost"`
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
	monitors         []oracle.Monitor
	ledger           *coverage.Ledger
	initial          Progress
	plans            []PlanReport
	planIDs          map[string]bool
	chargedRuns      int
	chargedDecisions int
	cost             ExecutionCost
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
	monitors, err := campaignMonitors(options.Monitors)
	if err != nil {
		return nil, err
	}
	session := &Session{
		id: id, digest: digest, profile: profile, profileDigest: profileDigest,
		manifest: manifest, newAdapter: newAdapter, options: options, ledger: ledger,
		monitors: monitors, planIDs: make(map[string]bool),
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
	if err := testplan.ValidateInputCapabilities(proposed, session.manifest); err != nil {
		return PlanReport{}, fmt.Errorf("validate plan %s against driver inputs: %w", proposed.ID, err)
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
	var primaryCost, replayCost PhaseCost
	primaryFactory := session.engineFactory(proposed.ID, concrete.Setup, &primaryCost)
	replayFactory := session.engineFactory(proposed.ID, concrete.Setup, &replayCost)
	explored, err := searcher.Explore(ctx, primaryFactory, concrete.Search.Config)
	planReport.Cost.Primary = primaryCost
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
	primaryCost.MeasurementEvents += explored.ChargedDecisions
	primaryCost.WorkUnits += explored.ChargedDecisions
	planReport.Cost.Primary = primaryCost
	planReport.BudgetReached = explored.BudgetReached
	planReport.StopReason = explored.StopReason
	for _, current := range explored.Runs {
		if planReport.SetupTrace == nil {
			planReport.SetupTrace = append([]core.TraceRecord(nil), current.SetupTrace...)
		}
		runID := fmt.Sprintf("%s/%s/run-%d", session.id, proposed.ID, current.Run)
		replayStable := true
		replayError := ""
		replayed, replayErr := explore.ReplayRun(ctx, replayFactory, concrete.Search.Config.Actions, current)
		replayCost.MeasurementEvents += len(replayed.Decisions)
		replayCost.WorkUnits += len(replayed.Decisions)
		if replayErr != nil {
			replayStable = false
			replayError = replayErr.Error()
		}
		monitorEvidenceErr := oracle.ValidateEvidence(current.FullTrace, session.monitors...)
		checked := oracle.Check(current.FullTrace, append([]oracle.Monitor{oracle.TraceIntegrity{}}, session.monitors...)...)
		active = activeTargets(session.ledger.Debts(), proposed.Targets)
		before := coveredSet(session.ledger.Report())
		accepted := replayStable && current.Conform && current.ExecutionError == "" && monitorEvidenceErr == nil
		if err := session.ledger.AddRun(session.profile, coverage.RunEvidence{
			ID: runID, Scenario: concrete.Setup.Name, Artifact: session.options.Artifact,
			Targets: active, Trace: current.FullTrace, Oracle: checked,
			ReplayStable: replayStable, Conformant: current.Conform && current.ExecutionError == "" && monitorEvidenceErr == nil,
			Manifest: session.manifest,
		}); err != nil {
			return PlanReport{}, fmt.Errorf("record plan %s run %d: %w", proposed.ID, current.Run, err)
		}
		planReport.Runs = append(planReport.Runs, RunReport{
			ID: runID, DeclaredTargets: append([]string(nil), proposed.Targets...), ActiveTargets: active,
			ReplayStable: replayStable, ReplayError: replayError,
			MonitorEvidenceError: oracle.EvidenceError(monitorEvidenceErr), AcceptedEvidence: accepted,
			NewlyCovered: newlyCovered(before, session.ledger.Report()), After: progress(session.ledger), Oracle: checked,
			Explorer: current,
		})
		session.chargedRuns++
		session.chargedDecisions += len(current.Decisions)
	}
	planReport.Cost.Replay = replayCost
	planReport.After = progress(session.ledger)
	session.appendPlan(planReport)
	return clonePlanReport(planReport), nil
}

func campaignMonitors(configured []oracle.Monitor) ([]oracle.Monitor, error) {
	if len(configured) == 0 {
		return []oracle.Monitor{oracle.Agreement{}}, nil
	}
	seen := make(map[string]bool, len(configured))
	result := make([]oracle.Monitor, 0, len(configured))
	for _, monitor := range configured {
		if monitor == nil || monitor.Name() == "" {
			return nil, errors.New("campaign monitor names must be non-empty")
		}
		if monitor.Name() == (oracle.TraceIntegrity{}).Name() {
			return nil, errors.New("trace-integrity is always enforced by the Campaign runtime")
		}
		if seen[monitor.Name()] {
			return nil, fmt.Errorf("duplicate campaign monitor %q", monitor.Name())
		}
		seen[monitor.Name()] = true
		result = append(result, monitor)
	}
	return result, nil
}

func (session *Session) engineFactory(
	planID string,
	setup scenario.Spec,
	cost *PhaseCost,
) explore.Factory {
	return func(factoryCtx context.Context) (*engine.Engine, error) {
		cost.SetupAttempts++
		cost.WorkUnits++
		protocolAdapter, err := session.newAdapter()
		if err != nil {
			return nil, err
		}
		execution, err := engine.New(protocolAdapter)
		if err != nil {
			return nil, fmt.Errorf("plan %s initialize engine: %w", planID, err)
		}
		setupCost, err := scenario.RunWithCost(factoryCtx, execution, setup)
		cost.SetupSteps += setupCost.Steps
		cost.SetupRuntimeEvents += setupCost.RuntimeEvents
		cost.WorkUnits += setupCost.WorkUnits
		if err != nil {
			return nil, fmt.Errorf("plan %s setup: %w", planID, err)
		}
		if err := execution.CheckConformance(); err != nil {
			return nil, fmt.Errorf("plan %s setup conformance: %w", planID, err)
		}
		return execution, nil
	}
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
		Cost:  session.cost,
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
	addExecutionCost(&session.cost, report.Cost)
	session.plans = append(session.plans, clonePlanReport(report))
}

func addExecutionCost(total *ExecutionCost, delta ExecutionCost) {
	addPhaseCost(&total.Primary, delta.Primary)
	addPhaseCost(&total.Replay, delta.Replay)
}

func addPhaseCost(total *PhaseCost, delta PhaseCost) {
	total.SetupAttempts += delta.SetupAttempts
	total.SetupSteps += delta.SetupSteps
	total.SetupRuntimeEvents += delta.SetupRuntimeEvents
	total.MeasurementEvents += delta.MeasurementEvents
	total.WorkUnits += delta.WorkUnits
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

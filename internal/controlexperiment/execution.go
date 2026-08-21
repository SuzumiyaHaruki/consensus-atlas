package controlexperiment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

type AdapterFactory func() (control.Adapter, error)

type runCapture struct {
	trace    controlruntime.Trace
	samples  []protocolstate.Sample
	snapshot controlruntime.Snapshot
	prepares []PreparationRecord
}

// ExecutionFailure preserves the work completed by the sole experiment path
// when a proposed policy cannot finish. It is an execution result, not a
// measurement-complete Report.
type ExecutionFailure struct {
	Phase    string
	Code     string
	Run      int
	Decision int
	Work     WorkLedger
	Terminal *controlruntime.TerminalOutcome
	cause    error
}

func (failure *ExecutionFailure) Error() string {
	return fmt.Sprintf("%s: phase=%s run=%d decision=%d: %v",
		failure.Code, failure.Phase, failure.Run, failure.Decision, failure.cause)
}

func (failure *ExecutionFailure) Unwrap() error {
	return failure.cause
}

func (failure *ExecutionFailure) MethodFailure() *MethodFailure {
	if failure == nil {
		return nil
	}
	result := &MethodFailure{
		Phase: failure.Phase, Code: failure.Code, Decision: failure.Decision,
	}
	if failure.Terminal != nil {
		terminal := *failure.Terminal
		terminal.AttemptedAction.Parameters = append(
			[]byte(nil), failure.Terminal.AttemptedAction.Parameters...,
		)
		result.Terminal = &terminal
	}
	return result
}

func executionFailure(
	phase string,
	code string,
	run int,
	decision int,
	work WorkLedger,
	cause error,
) *ExecutionFailure {
	if coded, ok := cause.(interface{ failureCode() string }); ok {
		code = coded.failureCode()
	}
	failure := &ExecutionFailure{
		Phase: phase, Code: code, Run: run, Decision: decision, Work: work, cause: cause,
	}
	var terminalError *controlruntime.TerminalExecutionError
	if errors.As(cause, &terminalError) {
		terminal := terminalError.Terminal
		terminal.AttemptedAction.Parameters = append(
			[]byte(nil), terminal.AttemptedAction.Parameters...,
		)
		failure.Terminal = &terminal
	}
	return failure
}

func ExecuteQualified(
	ctx context.Context,
	config Config,
	qualification conformance.QualificationReport,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
	router WorkloadRouter,
) (Report, error) {
	if err := VerifyConfigAdmission(config, qualification); err != nil {
		return Report{}, err
	}
	return execute(ctx, config, newAdapter, mapper, router, nil)
}

func execute(
	ctx context.Context,
	config Config,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
	router WorkloadRouter,
	captures *[]runCapture,
) (Report, error) {
	if err := config.Validate(); err != nil {
		return Report{}, err
	}
	if newAdapter == nil || mapper == nil || mapper.ID() != config.PSSID {
		return Report{}, errors.New("EXPERIMENT_COMPOSITION_INVALID")
	}
	if config.requiresWorkloadRouter() {
		if router == nil || router.ID() == "" ||
			(config.WorkloadRouterID != "" && router.ID() != config.WorkloadRouterID) {
			return Report{}, errors.New("EXPERIMENT_WORKLOAD_ROUTER_MISMATCH")
		}
	}
	runtimeConfig, _ := config.Runtime.runtimeConfig()
	configDigest, _ := config.Digest()
	report := Report{
		SchemaVersion: config.SchemaVersion, Status: StatusMeasurementComplete,
		ExperimentID: config.ID, PSSID: config.PSSID, Config: config,
		ConfigDigest: configDigest, Budget: expectedBudget(config), Work: emptyWork(),
	}
	measured := make([]protocolstate.MeasuredRun, 0, len(config.Runs))
	for _, plan := range config.Runs {
		var capture runCapture
		run, samples, err := executeRun(
			ctx, config.SchemaVersion, config.DecisionsPerRun, runtimeConfig, plan,
			config.Admission, config.FaultEnvelope, newAdapter, mapper, router, &report.Work, &capture,
		)
		if err != nil {
			return Report{}, err
		}
		if report.ManifestDigest == "" {
			report.ManifestDigest = run.ManifestDigest
		} else if report.ManifestDigest != run.ManifestDigest {
			cause := fmt.Errorf("EXPERIMENT_MANIFEST_CHANGED: run=%d", plan.Run)
			return Report{}, executionFailure(
				"aggregate", "EXPERIMENT_MANIFEST_CHANGED", plan.Run,
				config.DecisionsPerRun, report.Work, cause,
			)
		}
		report.Runs = append(report.Runs, run)
		if captures != nil {
			*captures = append(*captures, capture)
		}
		current := protocolstate.MeasuredRun{Run: plan.Run, Initial: samples[0]}
		for index := 1; index < len(samples); index++ {
			sample := samples[index]
			current.Decisions = append(current.Decisions, protocolstate.MeasuredDecision{
				Step: sample.Step, Sample: &sample,
			})
		}
		measured = append(measured, current)
	}
	var err error
	report.StateDiscovery, err = protocolstate.Aggregate(config.PSSID, measured)
	if err != nil {
		return Report{}, executionFailure(
			"aggregate", "EXPERIMENT_AGGREGATE_FAILED", 0, 0, report.Work, err,
		)
	}
	report, err = report.Seal()
	if err != nil {
		return Report{}, executionFailure(
			"report", "EXPERIMENT_REPORT_SEAL_FAILED", 0, 0, report.Work, err,
		)
	}
	if err := report.Validate(); err != nil {
		return Report{}, executionFailure(
			"report", "EXPERIMENT_REPORT_VALIDATE_FAILED", 0, 0, report.Work, err,
		)
	}
	return report, nil
}

func executeRun(
	ctx context.Context,
	schemaVersion string,
	decisionBudget int,
	runtimeConfig controlruntime.Config,
	plan RunPlan,
	admission *ExecutionAdmission,
	faultEnvelope *FaultEnvelope,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
	router WorkloadRouter,
	work *WorkLedger,
	capture *runCapture,
) (RunReport, []protocolstate.Sample, error) {
	chargeSetup(&work.Primary)
	adapter, err := newAdapter()
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-setup", "EXPERIMENT_PRIMARY_ADAPTER_FAILED", plan.Run, 0, *work, err,
		)
	}
	runtime, err := controlruntime.New(ctx, adapter, runtimeConfig)
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-runtime", "EXPERIMENT_PRIMARY_RUNTIME_FAILED", plan.Run, 0, *work, err,
		)
	}
	defer runtime.Close()
	chargeRuntimeInitialization(&work.Primary)
	initialTrace, err := runtime.Trace()
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-observe", "EXPERIMENT_PRIMARY_TRACE_FAILED", plan.Run, 0, *work, err,
		)
	}
	if admission != nil && initialTrace.ManifestDigest != admission.ManifestDigest {
		cause := fmt.Errorf("EXPERIMENT_ADMISSION_MANIFEST_MISMATCH: run=%d", plan.Run)
		return RunReport{}, nil, executionFailure(
			"admission", "EXPERIMENT_ADMISSION_MANIFEST_MISMATCH", plan.Run, 0, *work, cause,
		)
	}
	sampler, err := psscore.NewOnlineSampler(mapper, runtime.Snapshot(), initialTrace.InitialEvidence)
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-sample", "EXPERIMENT_PRIMARY_SAMPLE_FAILED", plan.Run, 0, *work, err,
		)
	}
	currentEvidence := initialTrace.InitialEvidence
	offeredWorkload := 0
	faultUsage := FaultUsage{}
	termination := RunTerminationBudget
	strictWorkload := schemaVersion == SchemaVersion
	var selections []SelectionAudit
	for decision := 1; decision <= decisionBudget; decision++ {
		prepareBefore := runtime.Snapshot()
		offeredAction, offered, err := offerPolicyPreparation(plan.Policy, decision, runtime)
		if err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-prepare", "EXPERIMENT_POLICY_PREPARE_FAILED", plan.Run, decision, *work, err,
			)
		}
		workloadOffered := false
		if !offered {
			offeredAction, workloadOffered, err = offerNextWorkloadInvocation(
				ctx, plan.Workload, offeredWorkload, runtime, router, currentEvidence, strictWorkload,
			)
			offered = workloadOffered
			if err != nil {
				return RunReport{}, nil, executionFailure(
					"primary-prepare", "EXPERIMENT_WORKLOAD_PREPARE_FAILED", plan.Run, decision, *work, err,
				)
			}
		}
		if offered {
			chargePrepareActions(&work.Primary, 1)
			prepare, err := newPreparationRecord(
				len(capture.prepares)+1, decision, offeredAction, prepareBefore, runtime.Snapshot(),
			)
			if err != nil {
				return RunReport{}, nil, executionFailure(
					"primary-prepare", "EXPERIMENT_PREPARATION_RECORD_FAILED", plan.Run, decision, *work, err,
				)
			}
			capture.prepares = append(capture.prepares, prepare)
		}
		enabled, err := runtime.EnabledActions(ctx)
		if err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-observe", "EXPERIMENT_ENABLED_ACTIONS_FAILED", plan.Run, decision, *work, err,
			)
		}
		runtimeEnabledDigest, err := control.CanonicalDigest(enabled)
		if err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-observe", "EXPERIMENT_ENABLED_DIGEST_FAILED", plan.Run, decision, *work, err,
			)
		}
		faultAdmissible := admissibleActions(faultEnvelope, faultUsage, enabled, runtime.Snapshot())
		selectable := plan.Policy.constrainSelectableActions(faultAdmissible)
		admissibleDigest, err := control.CanonicalDigest(selectable)
		if err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-admission", "EXPERIMENT_ADMISSIBLE_DIGEST_FAILED", plan.Run, decision, *work, err,
			)
		}
		if schemaVersion == SchemaVersionV2 && len(selectable) == 0 {
			termination = RunTerminationQuiescent
			if len(faultAdmissible) != 0 {
				termination = RunTerminationPolicySurface
			}
			break
		}
		action, err := plan.Policy.selectAction(decision, selectable)
		if err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-policy", "EXPERIMENT_POLICY_SELECTION_FAILED", plan.Run, decision, *work, err,
			)
		}
		if offered && action.ID != offeredAction {
			code := "EXPERIMENT_POLICY_PREPARED_ACTION_NOT_SELECTED"
			if workloadOffered {
				code = "EXPERIMENT_WORKLOAD_OFFER_NOT_SELECTED"
			}
			cause := fmt.Errorf("%s: %s", code, offeredAction)
			return RunReport{}, nil, executionFailure(
				"primary-policy", code, plan.Run, decision, *work, cause,
			)
		}
		if faultEnvelope != nil {
			if err := faultUsage.check(*faultEnvelope, action, runtime.Snapshot()); err != nil {
				return RunReport{}, nil, executionFailure(
					"primary-envelope", "EXPERIMENT_FAULT_ENVELOPE_EXCEEDED", plan.Run, decision, *work, err,
				)
			}
		}
		chargeDecisions(&work.Primary, 1)
		record, err := runtime.Select(ctx, action.ID)
		if err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-select", "EXPERIMENT_ACTION_SELECT_FAILED", plan.Run, decision, *work, err,
			)
		}
		if record.EnabledSetDigest != runtimeEnabledDigest {
			cause := errors.New("EXPERIMENT_RUNTIME_ENABLED_DIGEST_MISMATCH")
			return RunReport{}, nil, executionFailure(
				"primary-select", "EXPERIMENT_RUNTIME_ENABLED_DIGEST_MISMATCH", plan.Run, decision, *work, cause,
			)
		}
		if workloadOffered {
			offeredWorkload++
		}
		faultUsage.record(action)
		if record.Evidence != nil {
			currentEvidence = *record.Evidence
			currentEvidence.Payload.Bytes = append([]byte(nil), record.Evidence.Payload.Bytes...)
		}
		if err := sampler.Capture(record, runtime.Snapshot()); err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-sample", "EXPERIMENT_PRIMARY_SAMPLE_FAILED", plan.Run, decision, *work, err,
			)
		}
		if schemaVersion == SchemaVersionV2 {
			selections = append(selections, SelectionAudit{
				Decision: decision, RuntimeEnabledDigest: runtimeEnabledDigest,
				AdmissibleDigest: admissibleDigest, SelectedAction: action.ID,
			})
			completed, err := workloadCompleted(plan.Workload, runtime.Snapshot())
			if err != nil {
				return RunReport{}, nil, executionFailure(
					"primary-workload", "EXPERIMENT_WORKLOAD_RESULT_INVALID", plan.Run, decision, *work, err,
				)
			}
			if plan.StopAfterWorkload && completed && decision < decisionBudget {
				termination = RunTerminationConfigured
				break
			}
		}
	}
	trace, err := runtime.Trace()
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-observe", "EXPERIMENT_PRIMARY_TRACE_FAILED", plan.Run, decisionBudget, *work, err,
		)
	}
	if err := plan.Policy.validateTraceSurface(trace); err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-observe", "EXPERIMENT_PRIMARY_TRACE_OUTSIDE_POLICY_SURFACE",
			plan.Run, len(trace.Records), *work, err,
		)
	}
	workloadReport, err := finishWorkload(
		plan.Workload, offeredWorkload, runtime.Snapshot(), router, currentEvidence, strictWorkload,
	)
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-workload", "EXPERIMENT_WORKLOAD_INVALID", plan.Run, len(trace.Records), *work, err,
		)
	}
	chargeSetup(&work.Replay)
	replayAdapter, err := newAdapter()
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"replay-setup", "EXPERIMENT_REPLAY_ADAPTER_FAILED", plan.Run, 0, *work, err,
		)
	}
	replayed, progress, err := controlruntime.ReplayWithProgress(ctx, replayAdapter, runtimeConfig, trace)
	if progress.RuntimeInitialized {
		chargeRuntimeInitialization(&work.Replay)
	}
	chargePrepareActions(&work.Replay, progress.PrepareActions)
	chargeDecisions(&work.Replay, progress.Decisions)
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"replay", "EXPERIMENT_REPLAY_FAILED", plan.Run, progress.Decisions, *work, err,
		)
	}
	defer replayed.Close()
	replayTrace, err := replayed.Trace()
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"replay", "EXPERIMENT_REPLAY_TRACE_FAILED", plan.Run, progress.Decisions, *work, err,
		)
	}
	if err := plan.Policy.validateTraceSurface(replayTrace); err != nil {
		return RunReport{}, nil, executionFailure(
			"replay", "EXPERIMENT_REPLAY_TRACE_OUTSIDE_POLICY_SURFACE",
			plan.Run, len(replayTrace.Records), *work, err,
		)
	}
	if err := validateReplayedWorkloadRoutes(
		plan.Workload, progress.WorkloadOffers, router, replayTrace,
	); err != nil {
		return RunReport{}, nil, executionFailure(
			"replay-workload", "EXPERIMENT_REPLAY_WORKLOAD_ROUTE_MISMATCH",
			plan.Run, progress.Decisions, *work, err,
		)
	}
	replayEvidence := finalTraceEvidence(replayTrace)
	replayWorkload, err := finishWorkload(
		plan.Workload, progress.WorkloadOffers, replayed.Snapshot(), router, replayEvidence, strictWorkload,
	)
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"replay-workload", "EXPERIMENT_REPLAY_WORKLOAD_MISMATCH", plan.Run, progress.Decisions, *work, err,
		)
	}
	if !reflect.DeepEqual(workloadReport, replayWorkload) {
		cause := errors.New("EXPERIMENT_REPLAY_WORKLOAD_MISMATCH")
		return RunReport{}, nil, executionFailure(
			"replay-workload", "EXPERIMENT_REPLAY_WORKLOAD_MISMATCH", plan.Run, progress.Decisions, *work, cause,
		)
	}
	if schemaVersion == SchemaVersionV2 {
		replayFaultUsage := faultUsageFromRecords(replayTrace.Records)
		if replayFaultUsage != faultUsage {
			cause := errors.New("EXPERIMENT_REPLAY_FAULT_USAGE_MISMATCH")
			return RunReport{}, nil, executionFailure(
				"replay-termination", "EXPERIMENT_REPLAY_FAULT_USAGE_MISMATCH",
				plan.Run, progress.Decisions, *work, cause,
			)
		}
		if err := validateReplayedTermination(
			ctx, termination, decisionBudget, plan, faultEnvelope, replayFaultUsage, replayed,
		); err != nil {
			return RunReport{}, nil, executionFailure(
				"replay-termination", "EXPERIMENT_REPLAY_TERMINATION_MISMATCH",
				plan.Run, progress.Decisions, *work, err,
			)
		}
	}
	discovery, err := sampler.Discovery()
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-sample", "EXPERIMENT_DISCOVERY_FAILED", plan.Run, decisionBudget, *work, err,
		)
	}
	policyDigest, _ := plan.Policy.Digest()
	samples := sampler.Samples()
	samplesDigest, err := portableJSONDigest(samples)
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-sample", "EXPERIMENT_SAMPLE_DIGEST_FAILED", plan.Run, decisionBudget, *work, err,
		)
	}
	runReport := RunReport{
		Run: plan.Run, PolicyID: plan.Policy.ID, PolicyDigest: policyDigest,
		TargetDecisions: decisionBudget, ChargedDecisions: len(trace.Records),
		BudgetReached:  len(trace.Records) == decisionBudget,
		ManifestDigest: trace.ManifestDigest, TraceSchemaVersion: trace.SchemaVersion, TraceDigest: trace.Digest,
		SeedDigest: trace.SeedDigest, InitialStateDigest: trace.InitialStateDigest,
		FinalStateDigest: trace.FinalStateDigest, CorePSSSamples: len(samples),
		CorePSSSamplesDigest: samplesDigest, UniqueCoreStates: discovery.UniqueStates,
		Workload: workloadReport,
		Replay: ReplayResult{
			Required: true, Stable: true, Decisions: len(replayTrace.Records), TraceDigest: replayTrace.Digest,
		},
	}
	if schemaVersion == SchemaVersionV2 {
		runReport.Termination = termination
		runReport.Selections = selections
	}
	if capture != nil {
		capture.trace = trace
		capture.samples = append([]protocolstate.Sample(nil), samples...)
		capture.snapshot = runtime.Snapshot()
	}
	if faultEnvelope != nil {
		if err := faultUsage.validate(*faultEnvelope); err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-envelope", "EXPERIMENT_FAULT_USAGE_INVALID", plan.Run, decisionBudget, *work, err,
			)
		}
		usage := faultUsage
		runReport.Faults = &usage
	}
	return runReport, samples, nil
}

// offerPolicyPreparation reconstructs an exact author-supplied Action before
// policy selection. Ordinary Runtime actions and workload Invokes do not pass
// through this path.
func offerPolicyPreparation(
	policy Policy,
	decision int,
	runtime *controlruntime.Runtime,
) (control.ActionID, bool, error) {
	for _, rule := range policy.Rules {
		if rule.Decision != decision || rule.Kind != control.ActionPartition {
			continue
		}
		parameters, err := control.DecodePartitionParameters(rule.Parameters)
		if err != nil {
			return "", false, fmt.Errorf("EXPERIMENT_POLICY_PARTITION_DECODE_FAILED: %w", err)
		}
		actionID, err := runtime.OfferPartition(parameters.Left, parameters.Right)
		if err != nil {
			return "", false, fmt.Errorf("EXPERIMENT_POLICY_PARTITION_OFFER_FAILED: %w", err)
		}
		if actionID != rule.ActionID {
			return "", false, fmt.Errorf(
				"EXPERIMENT_POLICY_PARTITION_ID_MISMATCH: expected=%s actual=%s",
				rule.ActionID, actionID,
			)
		}
		return actionID, true, nil
	}
	return "", false, nil
}

func finalTraceEvidence(trace controlruntime.Trace) control.EvidenceEnvelope {
	evidence := trace.InitialEvidence
	for _, record := range trace.Records {
		if record.Evidence != nil {
			evidence = *record.Evidence
			evidence.Payload.Bytes = append([]byte(nil), record.Evidence.Payload.Bytes...)
		}
	}
	return evidence
}

func validateReplayedTermination(
	ctx context.Context,
	termination string,
	decisionBudget int,
	plan RunPlan,
	faultEnvelope *FaultEnvelope,
	usage FaultUsage,
	runtime *controlruntime.Runtime,
) error {
	decisions := int(runtime.Snapshot().Step)
	switch termination {
	case RunTerminationBudget:
		if decisions != decisionBudget {
			return errors.New("EXPERIMENT_REPLAY_BUDGET_TERMINATION_INVALID")
		}
	case RunTerminationConfigured:
		completed, err := workloadCompleted(plan.Workload, runtime.Snapshot())
		if err != nil || !plan.StopAfterWorkload || !completed || decisions >= decisionBudget {
			return errors.New("EXPERIMENT_REPLAY_CONFIGURED_TERMINATION_INVALID")
		}
	case RunTerminationQuiescent:
		enabled, err := runtime.EnabledActions(ctx)
		if err != nil {
			return err
		}
		if decisions >= decisionBudget ||
			len(admissibleActions(faultEnvelope, usage, enabled, runtime.Snapshot())) != 0 {
			return errors.New("EXPERIMENT_REPLAY_QUIESCENT_TERMINATION_INVALID")
		}
	case RunTerminationPolicySurface:
		enabled, err := runtime.EnabledActions(ctx)
		if err != nil {
			return err
		}
		faultAdmissible := admissibleActions(faultEnvelope, usage, enabled, runtime.Snapshot())
		if decisions >= decisionBudget || len(faultAdmissible) == 0 ||
			len(plan.Policy.constrainSelectableActions(faultAdmissible)) != 0 {
			return errors.New("EXPERIMENT_REPLAY_POLICY_SURFACE_TERMINATION_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_REPLAY_TERMINATION_INVALID")
	}
	return nil
}

func (report Report) Seal() (Report, error) {
	report.Digest = ""
	digest, err := portableJSONDigest(report)
	if err != nil {
		return Report{}, err
	}
	report.Digest = digest
	return report, nil
}

func (report Report) Validate() error {
	if (report.SchemaVersion != SchemaVersion && report.SchemaVersion != SchemaVersionV2) ||
		report.Status != StatusMeasurementComplete ||
		report.ExperimentID == "" || report.PSSID == "" {
		return errors.New("EXPERIMENT_REPORT_IDENTITY_INVALID")
	}
	if err := report.Config.Validate(); err != nil {
		return err
	}
	configDigest, _ := report.Config.Digest()
	if report.SchemaVersion != report.Config.SchemaVersion ||
		report.ExperimentID != report.Config.ID || report.PSSID != report.Config.PSSID ||
		report.ConfigDigest != configDigest || report.ManifestDigest == "" {
		return errors.New("EXPERIMENT_REPORT_CONFIG_MISMATCH")
	}
	wantWork := expectedWork(report.Config)
	if report.SchemaVersion == SchemaVersionV2 {
		wantWork = measuredWork(report)
	}
	if !reflect.DeepEqual(report.Budget, expectedBudget(report.Config)) ||
		!reflect.DeepEqual(report.Work, wantWork) {
		return errors.New("EXPERIMENT_REPORT_WORK_MISMATCH")
	}
	if len(report.Runs) != len(report.Config.Runs) {
		return errors.New("EXPERIMENT_REPORT_RUN_COUNT_MISMATCH")
	}
	runtimeConfig, _ := report.Config.Runtime.runtimeConfig()
	seedSum := sha256.Sum256(runtimeConfig.Seed)
	expectedSeedDigest := hex.EncodeToString(seedSum[:])
	for index, run := range report.Runs {
		plan := report.Config.Runs[index]
		policyDigest, _ := plan.Policy.Digest()
		if run.Run != plan.Run || run.PolicyID != plan.Policy.ID || run.PolicyDigest != policyDigest ||
			run.TargetDecisions != report.Config.DecisionsPerRun || run.ChargedDecisions < 0 ||
			run.ChargedDecisions > report.Config.DecisionsPerRun ||
			run.BudgetReached != (run.ChargedDecisions == report.Config.DecisionsPerRun) {
			return fmt.Errorf("EXPERIMENT_REPORT_RUN_MISMATCH: %d", run.Run)
		}
		if report.SchemaVersion == SchemaVersion {
			if run.ChargedDecisions != report.Config.DecisionsPerRun || !run.BudgetReached ||
				run.Termination != "" || len(run.Selections) != 0 {
				return fmt.Errorf("EXPERIMENT_REPORT_RUN_MISMATCH: %d", run.Run)
			}
		} else {
			if err := validateRunTermination(plan, run); err != nil {
				return fmt.Errorf("run %d: %w", run.Run, err)
			}
			if len(run.Selections) != run.ChargedDecisions {
				return fmt.Errorf("EXPERIMENT_SELECTION_AUDIT_COUNT_MISMATCH: %d", run.Run)
			}
			for decision, audit := range run.Selections {
				if err := audit.validate(decision + 1); err != nil {
					return err
				}
			}
		}
		if run.ManifestDigest != report.ManifestDigest || run.TraceSchemaVersion != controlruntime.TraceSchemaVersion ||
			run.TraceDigest == "" || run.SeedDigest != expectedSeedDigest ||
			run.InitialStateDigest == "" || run.FinalStateDigest == "" {
			return fmt.Errorf("EXPERIMENT_REPORT_TRACE_MISMATCH: %d", run.Run)
		}
		if !run.Replay.Required || !run.Replay.Stable ||
			run.Replay.Decisions != run.ChargedDecisions || run.Replay.TraceDigest != run.TraceDigest {
			return fmt.Errorf("EXPERIMENT_REPORT_REPLAY_MISMATCH: %d", run.Run)
		}
		if run.CorePSSSamples != run.ChargedDecisions+1 || run.CorePSSSamplesDigest == "" ||
			run.UniqueCoreStates <= 0 {
			return fmt.Errorf("EXPERIMENT_REPORT_SAMPLE_COUNT_MISMATCH: %d", run.Run)
		}
		if plan.Workload == nil {
			if run.Workload != nil {
				return fmt.Errorf("EXPERIMENT_WORKLOAD_REPORT_UNEXPECTED: %d", run.Run)
			}
		} else if run.Workload == nil {
			return fmt.Errorf("EXPERIMENT_WORKLOAD_REPORT_REQUIRED: %d", run.Run)
		} else if err := run.Workload.validate(
			*plan.Workload, report.SchemaVersion == SchemaVersion, report.Config.WorkloadRouterID,
		); err != nil {
			return fmt.Errorf("run %d: %w", run.Run, err)
		}
		if report.Config.FaultEnvelope == nil {
			if run.Faults != nil {
				return fmt.Errorf("EXPERIMENT_FAULT_USAGE_UNEXPECTED: %d", run.Run)
			}
		} else if run.Faults == nil {
			return fmt.Errorf("EXPERIMENT_FAULT_USAGE_REQUIRED: %d", run.Run)
		} else if err := run.Faults.validate(*report.Config.FaultEnvelope); err != nil {
			return fmt.Errorf("run %d: %w", run.Run, err)
		}
	}
	if err := validateDiscovery(report); err != nil {
		return err
	}
	sealed, err := report.Seal()
	if err != nil {
		return err
	}
	if sealed.Digest != report.Digest {
		return errors.New("EXPERIMENT_REPORT_DIGEST_MISMATCH")
	}
	return nil
}

func validateRunTermination(plan RunPlan, run RunReport) error {
	switch run.Termination {
	case RunTerminationBudget:
		if !run.BudgetReached {
			return errors.New("EXPERIMENT_RUN_BUDGET_TERMINATION_INVALID")
		}
	case RunTerminationQuiescent:
		if run.BudgetReached {
			return errors.New("EXPERIMENT_RUN_QUIESCENT_TERMINATION_INVALID")
		}
	case RunTerminationPolicySurface:
		if run.BudgetReached || !plan.Policy.bounded() {
			return errors.New("EXPERIMENT_RUN_POLICY_SURFACE_TERMINATION_INVALID")
		}
	case RunTerminationConfigured:
		if run.BudgetReached || !plan.StopAfterWorkload || run.Workload == nil ||
			run.Workload.Completed != run.Workload.Planned {
			return errors.New("EXPERIMENT_RUN_CONFIGURED_TERMINATION_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_RUN_TERMINATION_INVALID")
	}
	return nil
}

func validateDiscovery(report Report) error {
	discovery := report.StateDiscovery
	total := 0
	for _, run := range report.Runs {
		total += run.ChargedDecisions
	}
	if discovery.PSSID != report.PSSID || discovery.Runs != len(report.Runs) ||
		discovery.TotalDecisions != total || discovery.ProtocolSamples != total ||
		discovery.InitialStateKey == "" || discovery.UniqueStates != len(discovery.States) ||
		len(discovery.Curve) != total {
		return errors.New("EXPERIMENT_REPORT_DISCOVERY_MISMATCH")
	}
	var area int64
	for index, point := range discovery.Curve {
		if point.Decisions != index+1 || point.UniqueStates <= 0 || point.UniqueStates > discovery.UniqueStates {
			return errors.New("EXPERIMENT_REPORT_DISCOVERY_CURVE_INVALID")
		}
		area += int64(point.UniqueStates)
	}
	if area != discovery.PrefixArea ||
		(total > 0 && discovery.Curve[len(discovery.Curve)-1].UniqueStates != discovery.UniqueStates) ||
		(total == 0 && discovery.UniqueStates != 1) {
		return errors.New("EXPERIMENT_REPORT_DISCOVERY_AREA_INVALID")
	}
	return nil
}

// portableJSONDigest normalizes interface-backed state objects before hashing.
// This keeps a report identity stable after a JSON marshal/unmarshal cycle.
func portableJSONDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	return control.CanonicalDigest(normalized)
}

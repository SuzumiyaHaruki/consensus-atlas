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

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

type AdapterFactory func() (control.Adapter, error)

// ExecutionFailure preserves the work completed by the sole experiment path
// when a proposed policy cannot finish. It is an execution result, not a
// measurement-complete Report.
type ExecutionFailure struct {
	Phase    string
	Code     string
	Run      int
	Decision int
	Work     WorkLedger
	cause    error
}

func (failure *ExecutionFailure) Error() string {
	return fmt.Sprintf("%s: phase=%s run=%d decision=%d: %v",
		failure.Code, failure.Phase, failure.Run, failure.Decision, failure.cause)
}

func (failure *ExecutionFailure) Unwrap() error {
	return failure.cause
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
	return &ExecutionFailure{
		Phase: phase, Code: code, Run: run, Decision: decision, Work: work, cause: cause,
	}
}

func Execute(
	ctx context.Context,
	config Config,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
) (Report, error) {
	if err := config.Validate(); err != nil {
		return Report{}, err
	}
	if newAdapter == nil || mapper == nil || mapper.ID() != config.PSSID {
		return Report{}, errors.New("EXPERIMENT_COMPOSITION_INVALID")
	}
	runtimeConfig, _ := config.Runtime.runtimeConfig()
	configDigest, _ := config.Digest()
	report := Report{
		SchemaVersion: SchemaVersion, Status: StatusMeasurementComplete,
		ExperimentID: config.ID, PSSID: config.PSSID, Config: config,
		ConfigDigest: configDigest, Budget: expectedBudget(config), Work: emptyWork(),
	}
	measured := make([]protocolstate.MeasuredRun, 0, len(config.Runs))
	for _, plan := range config.Runs {
		run, samples, err := executeRun(
			ctx, config.DecisionsPerRun, runtimeConfig, plan, newAdapter, mapper, &report.Work,
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
	decisionBudget int,
	runtimeConfig controlruntime.Config,
	plan RunPlan,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
	work *WorkLedger,
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
	chargeRuntimeInitialization(&work.Primary)
	initialTrace, err := runtime.Trace()
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-observe", "EXPERIMENT_PRIMARY_TRACE_FAILED", plan.Run, 0, *work, err,
		)
	}
	sampler, err := psscore.NewOnlineSampler(mapper, runtime.Snapshot(), initialTrace.InitialEvidence)
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-sample", "EXPERIMENT_PRIMARY_SAMPLE_FAILED", plan.Run, 0, *work, err,
		)
	}
	for decision := 1; decision <= decisionBudget; decision++ {
		enabled, err := runtime.EnabledActions(ctx)
		if err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-observe", "EXPERIMENT_ENABLED_ACTIONS_FAILED", plan.Run, decision, *work, err,
			)
		}
		action, err := plan.Policy.selectAction(decision, enabled)
		if err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-policy", "EXPERIMENT_POLICY_SELECTION_FAILED", plan.Run, decision, *work, err,
			)
		}
		record, err := runtime.Select(ctx, action.ID)
		if err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-select", "EXPERIMENT_ACTION_SELECT_FAILED", plan.Run, decision, *work, err,
			)
		}
		chargeDecisions(&work.Primary, 1)
		if err := sampler.Capture(record, runtime.Snapshot()); err != nil {
			return RunReport{}, nil, executionFailure(
				"primary-sample", "EXPERIMENT_PRIMARY_SAMPLE_FAILED", plan.Run, decision, *work, err,
			)
		}
	}
	trace, err := runtime.Trace()
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"primary-observe", "EXPERIMENT_PRIMARY_TRACE_FAILED", plan.Run, decisionBudget, *work, err,
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
	chargeDecisions(&work.Replay, progress.Decisions)
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"replay", "EXPERIMENT_REPLAY_FAILED", plan.Run, progress.Decisions, *work, err,
		)
	}
	replayTrace, err := replayed.Trace()
	if err != nil {
		return RunReport{}, nil, executionFailure(
			"replay", "EXPERIMENT_REPLAY_TRACE_FAILED", plan.Run, progress.Decisions, *work, err,
		)
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
	return RunReport{
		Run: plan.Run, PolicyID: plan.Policy.ID, PolicyDigest: policyDigest,
		TargetDecisions: decisionBudget, ChargedDecisions: len(trace.Records), BudgetReached: true,
		ManifestDigest: trace.ManifestDigest, TraceSchemaVersion: trace.SchemaVersion, TraceDigest: trace.Digest,
		SeedDigest: trace.SeedDigest, InitialStateDigest: trace.InitialStateDigest,
		FinalStateDigest: trace.FinalStateDigest, CorePSSSamples: len(samples),
		CorePSSSamplesDigest: samplesDigest, UniqueCoreStates: discovery.UniqueStates,
		Replay: ReplayResult{
			Required: true, Stable: true, Decisions: len(replayTrace.Records), TraceDigest: replayTrace.Digest,
		},
	}, samples, nil
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
	if report.SchemaVersion != SchemaVersion || report.Status != StatusMeasurementComplete ||
		report.ExperimentID == "" || report.PSSID == "" {
		return errors.New("EXPERIMENT_REPORT_IDENTITY_INVALID")
	}
	if err := report.Config.Validate(); err != nil {
		return err
	}
	configDigest, _ := report.Config.Digest()
	if report.ExperimentID != report.Config.ID || report.PSSID != report.Config.PSSID ||
		report.ConfigDigest != configDigest || report.ManifestDigest == "" {
		return errors.New("EXPERIMENT_REPORT_CONFIG_MISMATCH")
	}
	if !reflect.DeepEqual(report.Budget, expectedBudget(report.Config)) ||
		!reflect.DeepEqual(report.Work, expectedWork(report.Config)) {
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
			run.TargetDecisions != report.Config.DecisionsPerRun ||
			run.ChargedDecisions != report.Config.DecisionsPerRun || !run.BudgetReached {
			return fmt.Errorf("EXPERIMENT_REPORT_RUN_MISMATCH: %d", run.Run)
		}
		if run.ManifestDigest != report.ManifestDigest || run.TraceSchemaVersion != controlruntime.TraceSchemaVersion ||
			run.TraceDigest == "" || run.SeedDigest != expectedSeedDigest ||
			run.InitialStateDigest == "" || run.FinalStateDigest == "" {
			return fmt.Errorf("EXPERIMENT_REPORT_TRACE_MISMATCH: %d", run.Run)
		}
		if !run.Replay.Required || !run.Replay.Stable ||
			run.Replay.Decisions != report.Config.DecisionsPerRun || run.Replay.TraceDigest != run.TraceDigest {
			return fmt.Errorf("EXPERIMENT_REPORT_REPLAY_MISMATCH: %d", run.Run)
		}
		if run.CorePSSSamples != report.Config.DecisionsPerRun+1 || run.CorePSSSamplesDigest == "" ||
			run.UniqueCoreStates <= 0 {
			return fmt.Errorf("EXPERIMENT_REPORT_SAMPLE_COUNT_MISMATCH: %d", run.Run)
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

func validateDiscovery(report Report) error {
	discovery := report.StateDiscovery
	total := len(report.Runs) * report.Config.DecisionsPerRun
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
	if area != discovery.PrefixArea || discovery.Curve[len(discovery.Curve)-1].UniqueStates != discovery.UniqueStates {
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

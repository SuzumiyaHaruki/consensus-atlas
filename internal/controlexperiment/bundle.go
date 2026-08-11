package controlexperiment

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	ExecutionBundleSchemaVersion   = "consensus-atlas/execution-bundle/v1"
	ExecutionBundleSchemaVersionV2 = "consensus-atlas/execution-bundle/v2"
	ExecutionBundleSchemaVersionV3 = "consensus-atlas/execution-bundle/v3"
	DecisionHistorySchemaVersion   = "consensus-atlas/decision-history/v1"
)

type BundleIdentity struct {
	ExperimentID     string `json:"experiment_id"`
	PSSID            string `json:"pss_id"`
	ConfigDigest     string `json:"config_digest"`
	ReportDigest     string `json:"report_digest"`
	ManifestDigest   string `json:"manifest_digest"`
	MethodSpecDigest string `json:"method_spec_digest,omitempty"`
}

type CorePSSSample struct {
	Step  int           `json:"step"`
	Key   string        `json:"key"`
	State psscore.State `json:"state"`
}

type ClientHistoryEntry struct {
	Step     int                    `json:"step"`
	Item     control.ItemID         `json:"item_id"`
	State    control.ItemState      `json:"state"`
	Response control.ClientResponse `json:"response"`
}

type DecisionHistory struct {
	SchemaVersion string                         `json:"schema_version"`
	ProjectorID   string                         `json:"projector_id"`
	Observations  []semantic.DecisionObservation `json:"observations"`
	Digest        string                         `json:"digest"`
}

type PreparationRecord struct {
	Ordinal           int            `json:"ordinal"`
	BeforeDecision    int            `json:"before_decision"`
	Action            control.Action `json:"action"`
	BeforeStateDigest string         `json:"before_state_digest"`
	AfterStateDigest  string         `json:"after_state_digest"`
	Digest            string         `json:"digest"`
}

type ExecutionBundle struct {
	SchemaVersion    string                          `json:"schema_version"`
	Identity         BundleIdentity                  `json:"identity"`
	Run              RunReport                       `json:"run"`
	Preparations     []PreparationRecord             `json:"preparations,omitempty"`
	Trace            controlruntime.Trace            `json:"trace"`
	FinalSnapshot    controlruntime.Snapshot         `json:"final_snapshot"`
	CorePSS          []CorePSSSample                 `json:"core_pss"`
	ClientHistory    []ClientHistoryEntry            `json:"client_history"`
	OperationHistory *OperationHistory               `json:"operation_history,omitempty"`
	Decisions        DecisionHistory                 `json:"decisions"`
	Qualification    conformance.QualificationBundle `json:"qualification"`
	Work             WorkLedger                      `json:"work"`
	Digest           string                          `json:"digest"`
}

// ExecuteQualifiedBundle is the sole bundle-producing path. It reuses the
// existing executor and only exposes data already captured during that run.
// Version 1 deliberately supports one measured run so cost ownership remains
// exact instead of being divided heuristically across runs.
func ExecuteQualifiedBundle(
	ctx context.Context,
	config Config,
	qualification conformance.QualificationBundle,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
	projector semantic.DecisionProjector,
	router WorkloadRouter,
) (Report, ExecutionBundle, error) {
	return executeQualifiedBundle(
		ctx, config, qualification, newAdapter, mapper, projector, router, "",
	)
}

// ExecuteQualifiedBundleV3 reuses the sole qualified executor but requests
// the additive v3 evidence envelope. Earlier entry points retain their frozen
// v1/v2 identities and never infer a method identity after execution.
func ExecuteQualifiedBundleV3(
	ctx context.Context,
	config Config,
	qualification conformance.QualificationBundle,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
	projector semantic.DecisionProjector,
	router WorkloadRouter,
	methodSpecDigest string,
) (Report, ExecutionBundle, error) {
	if !validSHA256(methodSpecDigest) {
		return Report{}, ExecutionBundle{}, errors.New("EXECUTION_BUNDLE_METHOD_SPEC_INVALID")
	}
	return executeQualifiedBundle(
		ctx, config, qualification, newAdapter, mapper, projector, router, methodSpecDigest,
	)
}

func executeQualifiedBundle(
	ctx context.Context,
	config Config,
	qualification conformance.QualificationBundle,
	newAdapter AdapterFactory,
	mapper psscore.SemanticMapper,
	projector semantic.DecisionProjector,
	router WorkloadRouter,
	methodSpecDigest string,
) (Report, ExecutionBundle, error) {
	if len(config.Runs) != 1 {
		return Report{}, ExecutionBundle{}, errors.New("EXECUTION_BUNDLE_SINGLE_RUN_REQUIRED")
	}
	if err := qualification.Validate(); err != nil {
		return Report{}, ExecutionBundle{}, fmt.Errorf("EXECUTION_BUNDLE_QUALIFICATION_INVALID: %w", err)
	}
	if err := VerifyConfigAdmission(config, qualification.Qualification); err != nil {
		return Report{}, ExecutionBundle{}, err
	}
	if projector == nil || projector.ID() == "" {
		return Report{}, ExecutionBundle{}, errors.New("EXECUTION_BUNDLE_DECISION_PROJECTOR_REQUIRED")
	}
	var captures []runCapture
	report, err := execute(ctx, config, newAdapter, mapper, router, &captures)
	if err != nil {
		return Report{}, ExecutionBundle{}, err
	}
	if len(captures) != 1 || len(report.Runs) != 1 {
		return Report{}, ExecutionBundle{}, errors.New("EXECUTION_BUNDLE_CAPTURE_COUNT_MISMATCH")
	}
	bundle, err := buildExecutionBundle(
		report, report.Runs[0], captures[0], qualification, projector, methodSpecDigest,
	)
	if err != nil {
		return Report{}, ExecutionBundle{}, err
	}
	return report, bundle, nil
}

func buildExecutionBundle(
	report Report,
	run RunReport,
	capture runCapture,
	qualification conformance.QualificationBundle,
	projector semantic.DecisionProjector,
	methodSpecDigest string,
) (ExecutionBundle, error) {
	coreSamples := make([]CorePSSSample, 0, len(capture.samples))
	for _, sample := range capture.samples {
		state, ok := sample.State.(psscore.State)
		if !ok {
			return ExecutionBundle{}, fmt.Errorf("EXECUTION_BUNDLE_CORE_PSS_TYPE_INVALID: %T", sample.State)
		}
		coreSamples = append(coreSamples, CorePSSSample{Step: sample.Step, Key: sample.Key, State: state})
	}
	clients, err := clientHistory(capture.trace, capture.snapshot)
	if err != nil {
		return ExecutionBundle{}, err
	}
	decisions, err := projectDecisionHistory(capture.trace, projector)
	if err != nil {
		return ExecutionBundle{}, err
	}
	bundleVersion := ExecutionBundleSchemaVersion
	if report.SchemaVersion == SchemaVersionV2 {
		bundleVersion = ExecutionBundleSchemaVersionV2
	}
	if methodSpecDigest != "" {
		if report.SchemaVersion != SchemaVersionV2 {
			return ExecutionBundle{}, errors.New("EXECUTION_BUNDLE_V3_EXPERIMENT_V2_REQUIRED")
		}
		bundleVersion = ExecutionBundleSchemaVersionV3
	}
	bundle := ExecutionBundle{
		SchemaVersion: bundleVersion,
		Identity: BundleIdentity{
			ExperimentID: report.ExperimentID, PSSID: report.PSSID,
			ConfigDigest: report.ConfigDigest, ReportDigest: report.Digest,
			ManifestDigest: report.ManifestDigest, MethodSpecDigest: methodSpecDigest,
		},
		Run: run, Preparations: append([]PreparationRecord(nil), capture.prepares...),
		Trace: capture.trace, FinalSnapshot: capture.snapshot,
		CorePSS: coreSamples, ClientHistory: clients, Decisions: decisions,
		Qualification: qualification, Work: report.Work,
	}
	if bundleVersion == ExecutionBundleSchemaVersionV3 {
		if report.Config.Runs[0].Workload == nil {
			return ExecutionBundle{}, errors.New("EXECUTION_BUNDLE_V3_WORKLOAD_REQUIRED")
		}
		operations, err := newOperationHistory(
			report.Config.Runs[0].Workload, run.Workload, capture.trace, clients,
		)
		if err != nil {
			return ExecutionBundle{}, err
		}
		bundle.OperationHistory = &operations
	}
	bundle, err = bundle.seal()
	if err != nil {
		return ExecutionBundle{}, err
	}
	if err := bundle.Validate(); err != nil {
		return ExecutionBundle{}, err
	}
	if err := bundle.ValidateProjection(projector); err != nil {
		return ExecutionBundle{}, err
	}
	return bundle, nil
}

func (bundle ExecutionBundle) seal() (ExecutionBundle, error) {
	bundle.Digest = ""
	digest, err := portableJSONDigest(bundle)
	if err != nil {
		return ExecutionBundle{}, err
	}
	bundle.Digest = digest
	return bundle, nil
}

func (bundle ExecutionBundle) Validate() error {
	if (bundle.SchemaVersion != ExecutionBundleSchemaVersion &&
		bundle.SchemaVersion != ExecutionBundleSchemaVersionV2 &&
		bundle.SchemaVersion != ExecutionBundleSchemaVersionV3) || bundle.Identity.ExperimentID == "" ||
		bundle.Identity.PSSID == "" || !validSHA256(bundle.Identity.ConfigDigest) ||
		!validSHA256(bundle.Identity.ReportDigest) || !validSHA256(bundle.Identity.ManifestDigest) {
		return errors.New("EXECUTION_BUNDLE_IDENTITY_INVALID")
	}
	if bundle.SchemaVersion == ExecutionBundleSchemaVersionV3 {
		if !validSHA256(bundle.Identity.MethodSpecDigest) || bundle.OperationHistory == nil {
			return errors.New("EXECUTION_BUNDLE_V3_IDENTITY_INVALID")
		}
	} else if bundle.Identity.MethodSpecDigest != "" || bundle.OperationHistory != nil {
		return errors.New("EXECUTION_BUNDLE_LEGACY_V3_EVIDENCE_FORBIDDEN")
	}
	if err := bundle.Qualification.Validate(); err != nil {
		return err
	}
	qualification := bundle.Qualification.Qualification
	if qualification.ManifestDigest != bundle.Identity.ManifestDigest ||
		bundle.Qualification.Manifest.BuildID != qualification.BuildID ||
		bundle.Trace.ManifestDigest != bundle.Identity.ManifestDigest ||
		bundle.Run.ManifestDigest != bundle.Identity.ManifestDigest {
		return errors.New("EXECUTION_BUNDLE_QUALIFICATION_MISMATCH")
	}
	if err := bundle.Trace.Validate(); err != nil {
		return err
	}
	if bundle.Run.TraceDigest != bundle.Trace.Digest || bundle.Run.TraceSchemaVersion != bundle.Trace.SchemaVersion ||
		bundle.Run.ChargedDecisions != len(bundle.Trace.Records) || !bundle.Run.Replay.Required ||
		!bundle.Run.Replay.Stable || bundle.Run.Replay.Decisions != len(bundle.Trace.Records) ||
		bundle.Run.Replay.TraceDigest != bundle.Trace.Digest {
		return errors.New("EXECUTION_BUNDLE_REPLAY_MISMATCH")
	}
	if err := bundle.validateRunAndWork(); err != nil {
		return err
	}
	if len(bundle.Preparations) != bundle.Work.Primary.PrepareActions {
		return errors.New("EXECUTION_BUNDLE_PREPARATION_COUNT_MISMATCH")
	}
	for index, preparation := range bundle.Preparations {
		if err := preparation.Validate(); err != nil {
			return err
		}
		if preparation.Ordinal != index+1 || preparation.BeforeDecision > len(bundle.Trace.Records) {
			return errors.New("EXECUTION_BUNDLE_PREPARATION_ORDER_INVALID")
		}
	}
	snapshotDigest, err := bundle.FinalSnapshot.Digest()
	if err != nil || snapshotDigest != bundle.Trace.FinalStateDigest ||
		bundle.FinalSnapshot.Step != uint64(len(bundle.Trace.Records)) {
		return errors.New("EXECUTION_BUNDLE_FINAL_SNAPSHOT_MISMATCH")
	}
	if len(bundle.CorePSS) != len(bundle.Trace.Records)+1 || bundle.Run.CorePSSSamples != len(bundle.CorePSS) {
		return errors.New("EXECUTION_BUNDLE_CORE_PSS_COUNT_MISMATCH")
	}
	for index, sample := range bundle.CorePSS {
		if sample.Step != index || sample.Key != sample.State.Digest {
			return fmt.Errorf("EXECUTION_BUNDLE_CORE_PSS_SAMPLE_INVALID: %d", index)
		}
		if err := sample.State.Validate(); err != nil {
			return err
		}
	}
	wantClients, err := clientHistory(bundle.Trace, bundle.FinalSnapshot)
	if err != nil {
		return err
	}
	gotClients, err := control.CanonicalDigest(bundle.ClientHistory)
	if err != nil {
		return err
	}
	wantClientDigest, err := control.CanonicalDigest(wantClients)
	if err != nil || gotClients != wantClientDigest {
		return errors.New("EXECUTION_BUNDLE_CLIENT_HISTORY_MISMATCH")
	}
	if err := bundle.Decisions.Validate(); err != nil {
		return err
	}
	if bundle.OperationHistory != nil {
		if err := bundle.OperationHistory.Validate(
			bundle.Run.Workload, bundle.Trace, bundle.ClientHistory,
		); err != nil {
			return err
		}
	}
	sealed, err := bundle.seal()
	if err != nil {
		return err
	}
	if !validSHA256(bundle.Digest) || sealed.Digest != bundle.Digest {
		return errors.New("EXECUTION_BUNDLE_DIGEST_MISMATCH")
	}
	return nil
}

func (bundle ExecutionBundle) validateRunAndWork() error {
	decisions := len(bundle.Trace.Records)
	if bundle.Run.TargetDecisions < decisions ||
		bundle.Run.SeedDigest != bundle.Trace.SeedDigest ||
		bundle.Run.InitialStateDigest != bundle.Trace.InitialStateDigest ||
		bundle.Run.FinalStateDigest != bundle.Trace.FinalStateDigest {
		return errors.New("EXECUTION_BUNDLE_RUN_TRACE_MISMATCH")
	}
	if bundle.SchemaVersion == ExecutionBundleSchemaVersion {
		if bundle.Run.TargetDecisions != decisions || !bundle.Run.BudgetReached ||
			bundle.Run.Termination != "" || len(bundle.Run.Selections) != 0 {
			return errors.New("EXECUTION_BUNDLE_RUN_TRACE_MISMATCH")
		}
	} else {
		if bundle.Run.BudgetReached != (bundle.Run.TargetDecisions == decisions) ||
			len(bundle.Run.Selections) != decisions {
			return errors.New("EXECUTION_BUNDLE_RUN_TERMINATION_MISMATCH")
		}
		switch bundle.Run.Termination {
		case RunTerminationBudget:
			if !bundle.Run.BudgetReached {
				return errors.New("EXECUTION_BUNDLE_RUN_TERMINATION_MISMATCH")
			}
		case RunTerminationQuiescent, RunTerminationPolicySurface, RunTerminationConfigured:
			if bundle.Run.BudgetReached {
				return errors.New("EXECUTION_BUNDLE_RUN_TERMINATION_MISMATCH")
			}
		default:
			return errors.New("EXECUTION_BUNDLE_RUN_TERMINATION_MISMATCH")
		}
		for index, audit := range bundle.Run.Selections {
			record := bundle.Trace.Records[index]
			if err := audit.validate(index + 1); err != nil ||
				audit.RuntimeEnabledDigest != record.EnabledSetDigest ||
				audit.SelectedAction != record.Action.ID {
				return errors.New("EXECUTION_BUNDLE_SELECTION_AUDIT_MISMATCH")
			}
		}
	}
	if bundle.Run.Faults != nil && *bundle.Run.Faults != faultUsageFromRecords(bundle.Trace.Records) {
		return errors.New("EXECUTION_BUNDLE_FAULT_USAGE_MISMATCH")
	}
	sampleDigest, err := portableJSONDigest(bundle.CorePSS)
	if err != nil || sampleDigest != bundle.Run.CorePSSSamplesDigest {
		return errors.New("EXECUTION_BUNDLE_RUN_SAMPLE_MISMATCH")
	}
	unique := make(map[string]bool, len(bundle.CorePSS))
	for _, sample := range bundle.CorePSS {
		unique[sample.Key] = true
	}
	if bundle.Run.UniqueCoreStates != len(unique) {
		return errors.New("EXECUTION_BUNDLE_RUN_DISCOVERY_MISMATCH")
	}
	prepare := len(bundle.Preparations)
	wantPhase := PhaseWork{
		SetupAttempts: 1, RuntimeInitializations: 1, PrepareActions: prepare,
		SchedulerDecisions: decisions, WorkUnits: 1 + prepare + decisions,
	}
	wantResources := ResourceAccounting{
		WallTime: ResourceNotCollected, CPUTime: ResourceNotCollected, PeakRSS: ResourceNotCollected,
	}
	if bundle.Work.Primary != wantPhase || bundle.Work.Replay != wantPhase ||
		bundle.Work.Model != (ModelWork{}) || bundle.Work.Resources != wantResources {
		return errors.New("EXECUTION_BUNDLE_WORK_MISMATCH")
	}
	if bundle.Run.Workload != nil {
		if bundle.Run.Workload.Completed != len(bundle.Run.Workload.Results) {
			return errors.New("EXECUTION_BUNDLE_WORKLOAD_MISMATCH")
		}
		if bundle.SchemaVersion == ExecutionBundleSchemaVersion &&
			(bundle.Run.Workload.Planned != len(bundle.ClientHistory) ||
				bundle.Run.Workload.Offered != bundle.Run.Workload.Planned ||
				bundle.Run.Workload.Completed != bundle.Run.Workload.Planned) {
			return errors.New("EXECUTION_BUNDLE_WORKLOAD_MISMATCH")
		}
		responses := make(map[string]control.ClientResponse, len(bundle.ClientHistory))
		for _, entry := range bundle.ClientHistory {
			responses[entry.Response.RequestID] = entry.Response
		}
		for _, result := range bundle.Run.Workload.Results {
			response, exists := responses[result.InvocationID]
			if !exists || response.Owner.Node != result.Owner || response.Status != result.Status ||
				response.Payload.Digest != result.PayloadDigest {
				return errors.New("EXECUTION_BUNDLE_WORKLOAD_RESULT_MISMATCH")
			}
		}
		resultsDigest, err := control.CanonicalDigest(bundle.Run.Workload.Results)
		if err != nil || resultsDigest != bundle.Run.Workload.ResultsDigest {
			return errors.New("EXECUTION_BUNDLE_WORKLOAD_RESULT_DIGEST_MISMATCH")
		}
	} else if len(bundle.ClientHistory) != 0 {
		return errors.New("EXECUTION_BUNDLE_WORKLOAD_REPORT_REQUIRED")
	}
	return nil
}

func newPreparationRecord(
	ordinal int,
	beforeDecision int,
	actionID control.ActionID,
	before controlruntime.Snapshot,
	after controlruntime.Snapshot,
) (PreparationRecord, error) {
	beforeDigest, err := before.Digest()
	if err != nil {
		return PreparationRecord{}, err
	}
	afterDigest, err := after.Digest()
	if err != nil {
		return PreparationRecord{}, err
	}
	var action control.Action
	for _, offered := range after.Offered {
		if offered.ID == actionID {
			action = offered
			break
		}
	}
	if action.ID == "" {
		return PreparationRecord{}, fmt.Errorf("EXPERIMENT_PREPARATION_ACTION_MISSING: %s", actionID)
	}
	record := PreparationRecord{
		Ordinal: ordinal, BeforeDecision: beforeDecision, Action: action,
		BeforeStateDigest: beforeDigest, AfterStateDigest: afterDigest,
	}
	return record.seal()
}

func (record PreparationRecord) seal() (PreparationRecord, error) {
	record.Digest = ""
	digest, err := control.CanonicalDigest(record)
	if err != nil {
		return PreparationRecord{}, err
	}
	record.Digest = digest
	return record, nil
}

func (record PreparationRecord) Validate() error {
	if record.Ordinal <= 0 || record.BeforeDecision <= 0 || record.Action.ID == "" ||
		!validSHA256(record.BeforeStateDigest) || !validSHA256(record.AfterStateDigest) ||
		record.BeforeStateDigest == record.AfterStateDigest {
		return errors.New("EXECUTION_BUNDLE_PREPARATION_INVALID")
	}
	if record.Action.Kind != control.ActionInvoke && record.Action.Kind != control.ActionPartition {
		return errors.New("EXECUTION_BUNDLE_PREPARATION_KIND_INVALID")
	}
	sealed, err := record.seal()
	if err != nil || !validSHA256(record.Digest) || sealed.Digest != record.Digest {
		return errors.New("EXECUTION_BUNDLE_PREPARATION_DIGEST_MISMATCH")
	}
	return nil
}

func (bundle ExecutionBundle) ValidateProjection(projector semantic.DecisionProjector) error {
	if projector == nil || projector.ID() != bundle.Decisions.ProjectorID {
		return errors.New("EXECUTION_BUNDLE_DECISION_PROJECTOR_MISMATCH")
	}
	want, err := projectDecisionHistory(bundle.Trace, projector)
	if err != nil {
		return err
	}
	if want.Digest != bundle.Decisions.Digest {
		return errors.New("EXECUTION_BUNDLE_DECISION_HISTORY_MISMATCH")
	}
	return nil
}

func (history DecisionHistory) Validate() error {
	if history.SchemaVersion != DecisionHistorySchemaVersion || history.ProjectorID == "" {
		return errors.New("DECISION_HISTORY_IDENTITY_INVALID")
	}
	normalized, err := semantic.NormalizeDecisionObservations(history.Observations)
	if err != nil {
		return err
	}
	for index := range normalized {
		if normalized[index] != history.Observations[index] {
			return errors.New("DECISION_HISTORY_NOT_CANONICAL")
		}
	}
	digest, err := control.CanonicalDigest(history.Observations)
	if err != nil || !validSHA256(history.Digest) || digest != history.Digest {
		return errors.New("DECISION_HISTORY_DIGEST_MISMATCH")
	}
	return nil
}

func projectDecisionHistory(
	trace controlruntime.Trace,
	projector semantic.DecisionProjector,
) (DecisionHistory, error) {
	if projector == nil || projector.ID() == "" {
		return DecisionHistory{}, errors.New("EXECUTION_BUNDLE_DECISION_PROJECTOR_REQUIRED")
	}
	points := []struct {
		step     int
		evidence control.EvidenceEnvelope
	}{{step: 0, evidence: trace.InitialEvidence}}
	for _, record := range trace.Records {
		if record.Evidence != nil {
			points = append(points, struct {
				step     int
				evidence control.EvidenceEnvelope
			}{step: int(record.Step), evidence: *record.Evidence})
		}
	}
	seen := make(map[string]bool)
	var observations []semantic.DecisionObservation
	for _, point := range points {
		projected, err := projector.Project(point.evidence)
		if err != nil {
			return DecisionHistory{}, err
		}
		for _, observation := range projected {
			if observation.Step != 0 {
				return DecisionHistory{}, errors.New("DECISION_PROJECTOR_STEP_AUTHORITY_FORBIDDEN")
			}
			observation.Step = point.step
			if err := observation.Validate(); err != nil {
				return DecisionHistory{}, err
			}
			key := string(observation.Participant) + "\x00" + observation.Position + "\x00" + observation.ValueDigest
			if seen[key] {
				continue
			}
			seen[key] = true
			observations = append(observations, observation)
		}
	}
	normalized, err := semantic.NormalizeDecisionObservations(observations)
	if err != nil {
		return DecisionHistory{}, err
	}
	digest, err := control.CanonicalDigest(normalized)
	if err != nil {
		return DecisionHistory{}, err
	}
	return DecisionHistory{
		SchemaVersion: DecisionHistorySchemaVersion, ProjectorID: projector.ID(),
		Observations: normalized, Digest: digest,
	}, nil
}

func clientHistory(
	trace controlruntime.Trace,
	snapshot controlruntime.Snapshot,
) ([]ClientHistoryEntry, error) {
	steps := make(map[control.ItemID]int)
	for _, record := range trace.Records {
		for _, transition := range record.ItemTransitions {
			if _, exists := steps[transition.Item]; !exists {
				steps[transition.Item] = int(record.Step)
			}
		}
	}
	var result []ClientHistoryEntry
	for _, item := range snapshot.Items {
		if item.Kind != control.ItemClientResult || item.Value.Response == nil {
			continue
		}
		if err := item.Value.Validate(); err != nil {
			return nil, err
		}
		step := steps[item.ID]
		if step <= 0 || step > len(trace.Records) {
			return nil, fmt.Errorf("EXECUTION_BUNDLE_CLIENT_STEP_INVALID: %s", item.ID)
		}
		result = append(result, ClientHistoryEntry{
			Step: step, Item: item.ID, State: item.State, Response: *item.Value.Response,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Step != result[j].Step {
			return result[i].Step < result[j].Step
		}
		return result[i].Item < result[j].Item
	})
	return result, nil
}

package controlexperiment

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const (
	CampaignObservationVersion     = "consensus-atlas/campaign-observation/v1"
	CampaignMonitorRequirement     = "requirement-violation"
	CampaignMonitorEvidenceInvalid = "evidence-invalid"
)

// CampaignMonitorFinding is a target projector's protocol-neutral monitor
// output. The generic Campaign layer never selects or implements monitors.
type CampaignMonitorFinding struct {
	Monitor string
	Step    int
	Class   string
	Message string
}

// CampaignAttemptProjection is the transient, target-owned interpretation of
// one committed artifact. It is never persisted as a second bundle copy.
type CampaignAttemptProjection struct {
	Ordinal           int
	ArtifactDigest    string
	Outcome           string
	Work              WorkLedger
	ExecutionEvidence bool
	BundleDigest      string
	PSSID             string
	CorePSS           []CorePSSSample
	Faults            *FaultUsage
	Workload          *WorkloadRunReport
	CheckedMonitors   []string
	MonitorFindings   []CampaignMonitorFinding
}

type CampaignTerminalObservation struct {
	Status        string     `json:"status"`
	StopReason    string     `json:"stop_reason"`
	Attempts      int        `json:"attempts"`
	Completed     int        `json:"completed"`
	Rejected      int        `json:"rejected"`
	Failed        int        `json:"failed"`
	Invalid       int        `json:"invalid"`
	Work          WorkLedger `json:"work"`
	ElapsedMillis int64      `json:"elapsed_ms"`
	FailureDigest string     `json:"failure_digest,omitempty"`
}

type CampaignAttemptObservation struct {
	Ordinal           int                                 `json:"ordinal"`
	RecordDigest      string                              `json:"record_digest"`
	ArtifactDigest    string                              `json:"artifact_digest"`
	Outcome           string                              `json:"outcome"`
	ExecutionEvidence bool                                `json:"execution_evidence"`
	BundleDigest      string                              `json:"bundle_digest,omitempty"`
	ChargedDecisions  int                                 `json:"charged_decisions,omitempty"`
	PSSSamples        int                                 `json:"pss_samples,omitempty"`
	PSSStates         int                                 `json:"pss_states,omitempty"`
	NewPSSStates      int                                 `json:"new_pss_states,omitempty"`
	Faults            *FaultUsage                         `json:"faults,omitempty"`
	Workload          *CampaignWorkloadAttemptObservation `json:"workload,omitempty"`
	CheckedMonitors   []string                            `json:"checked_monitors,omitempty"`
	MonitorTriggers   int                                 `json:"monitor_triggers,omitempty"`
}

type CampaignPSSWitness struct {
	Key                  string        `json:"key"`
	FirstAttempt         int           `json:"first_attempt"`
	FirstAttemptDecision int           `json:"first_attempt_decision"`
	FirstGlobalDecision  int           `json:"first_global_decision"`
	TraceStep            int           `json:"trace_step,omitempty"`
	State                psscore.State `json:"state"`
}

type CampaignPSSPoint struct {
	Decisions       int  `json:"decisions"`
	Attempt         int  `json:"attempt"`
	AttemptDecision int  `json:"attempt_decision"`
	UniqueStates    int  `json:"unique_states"`
	NewState        bool `json:"new_state"`
}

// CampaignPSSObservation is denominator-free discovery evidence. In
// particular, SelfNormalizedArea is a curve-shape statistic, not coverage.
type CampaignPSSObservation struct {
	PSSID              string               `json:"pss_id"`
	EvidenceAttempts   int                  `json:"evidence_attempts"`
	TotalDecisions     int                  `json:"total_decisions"`
	TotalSamples       int                  `json:"total_samples"`
	InitialStateKey    string               `json:"initial_state_key"`
	UniqueStates       int                  `json:"unique_states"`
	StateSetDigest     string               `json:"state_set_digest"`
	PrefixArea         int64                `json:"prefix_area"`
	SelfNormalizedArea float64              `json:"self_normalized_area"`
	Curve              []CampaignPSSPoint   `json:"curve"`
	States             []CampaignPSSWitness `json:"states"`
}

type CampaignFaultObservation struct {
	ObservedAttempts int        `json:"observed_attempts"`
	Usage            FaultUsage `json:"usage"`
}

type CampaignStatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type CampaignWorkloadAttemptObservation struct {
	Planned        int                   `json:"planned"`
	Offered        int                   `json:"offered"`
	Completed      int                   `json:"completed"`
	Pending        int                   `json:"pending"`
	ResultStatuses []CampaignStatusCount `json:"result_statuses"`
}

type CampaignWorkloadObservation struct {
	ObservedAttempts int                   `json:"observed_attempts"`
	Planned          int                   `json:"planned"`
	Offered          int                   `json:"offered"`
	Completed        int                   `json:"completed"`
	Pending          int                   `json:"pending"`
	ResultStatuses   []CampaignStatusCount `json:"result_statuses"`
}

type CampaignMonitorCount struct {
	Monitor string `json:"monitor"`
	Count   int    `json:"count"`
}

type CampaignMonitorTrigger struct {
	Attempt        int    `json:"attempt"`
	ArtifactDigest string `json:"artifact_digest"`
	BundleDigest   string `json:"bundle_digest"`
	Monitor        string `json:"monitor"`
	Step           int    `json:"step"`
	Class          string `json:"class"`
	Message        string `json:"message"`
}

type CampaignMonitorObservation struct {
	ObservedAttempts int                      `json:"observed_attempts"`
	Checked          []CampaignMonitorCount   `json:"checked"`
	Triggers         []CampaignMonitorTrigger `json:"triggers"`
}

// CampaignObservation is a compact derived index. Full reports, bundles and
// traces remain in the content-addressed Campaign artifact store.
type CampaignObservation struct {
	SchemaVersion        string `json:"schema_version"`
	CampaignID           string `json:"campaign_id"`
	ConfigDigest         string `json:"config_digest"`
	TargetID             string `json:"target_id"`
	TargetIdentityDigest string `json:"target_identity_digest"`
	ExperimentSpecDigest string `json:"experiment_spec_digest"`
	SummaryDigest        string `json:"summary_digest"`
	HeadCheckpointDigest string `json:"head_checkpoint_digest"`

	Terminal CampaignTerminalObservation  `json:"terminal"`
	Attempts []CampaignAttemptObservation `json:"attempts"`
	PSS      *CampaignPSSObservation      `json:"pss,omitempty"`
	Faults   CampaignFaultObservation     `json:"faults"`
	Workload CampaignWorkloadObservation  `json:"workload"`
	Monitors CampaignMonitorObservation   `json:"monitors"`
	Digest   string                       `json:"digest"`
}

func NewCampaignObservation(
	summary CampaignSummary,
	projections []CampaignAttemptProjection,
) (CampaignObservation, error) {
	if err := summary.Validate(); err != nil {
		return CampaignObservation{}, err
	}
	if len(projections) != len(summary.Attempts) {
		return CampaignObservation{}, errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PROJECTION_COUNT_MISMATCH")
	}
	observation := CampaignObservation{
		SchemaVersion: CampaignObservationVersion,
		CampaignID:    summary.CampaignID, ConfigDigest: summary.ConfigDigest,
		TargetID: summary.TargetID, TargetIdentityDigest: summary.TargetIdentityDigest,
		ExperimentSpecDigest: summary.ExperimentSpecDigest, SummaryDigest: summary.Digest,
		HeadCheckpointDigest: summary.HeadCheckpointDigest,
		Terminal: CampaignTerminalObservation{
			Status: summary.Status, StopReason: summary.StopReason, Attempts: summary.Sequence,
			Work: summary.Totals, ElapsedMillis: summary.ElapsedMillis,
		},
	}
	if summary.Failure != nil {
		observation.Terminal.FailureDigest = summary.Failure.Digest
	}

	measured := make([]protocolstate.MeasuredRun, 0, len(projections))
	statusCounts := make(map[string]int)
	monitorCounts := make(map[string]int)
	for index, projection := range projections {
		source := summary.Attempts[index]
		if err := validateCampaignAttemptProjection(source, projection); err != nil {
			return CampaignObservation{}, err
		}
		switch projection.Outcome {
		case CampaignAttemptCompleted:
			observation.Terminal.Completed++
		case CampaignAttemptRejected:
			observation.Terminal.Rejected++
		case CampaignAttemptFailed:
			observation.Terminal.Failed++
		case CampaignAttemptInvalid:
			observation.Terminal.Invalid++
		}
		attempt := CampaignAttemptObservation{
			Ordinal: projection.Ordinal, RecordDigest: source.Record.Digest,
			ArtifactDigest: projection.ArtifactDigest, Outcome: projection.Outcome,
			ExecutionEvidence: projection.ExecutionEvidence,
		}
		if projection.ExecutionEvidence {
			attempt.BundleDigest = projection.BundleDigest
			attempt.ChargedDecisions = len(projection.CorePSS) - 1
			attempt.PSSSamples = len(projection.CorePSS)
			attempt.PSSStates = countCampaignPSSStates(projection.CorePSS)
			attempt.Faults = cloneFaultUsage(projection.Faults)
			if projection.Workload != nil {
				attempt.Workload = newCampaignWorkloadAttemptObservation(*projection.Workload)
			}
			attempt.CheckedMonitors = sortedStrings(projection.CheckedMonitors)
			attempt.MonitorTriggers = len(projection.MonitorFindings)
			current := protocolstate.MeasuredRun{
				Run:     projection.Ordinal,
				Initial: protocolstate.Sample{Step: 0, Key: projection.CorePSS[0].Key, State: projection.CorePSS[0].State},
			}
			for decision := 1; decision < len(projection.CorePSS); decision++ {
				sample := projection.CorePSS[decision]
				current.Decisions = append(current.Decisions, protocolstate.MeasuredDecision{
					Step:   decision,
					Sample: &protocolstate.Sample{Step: decision, Key: sample.Key, State: sample.State},
				})
			}
			measured = append(measured, current)
			observation.Faults.ObservedAttempts++
			if projection.Faults != nil {
				addCampaignFaultUsage(&observation.Faults.Usage, *projection.Faults)
			}
			if projection.Workload != nil {
				addCampaignWorkload(&observation.Workload, *projection.Workload, statusCounts)
			}
			observation.Monitors.ObservedAttempts++
			for _, monitor := range projection.CheckedMonitors {
				monitorCounts[monitor]++
			}
			for _, finding := range projection.MonitorFindings {
				observation.Monitors.Triggers = append(observation.Monitors.Triggers, CampaignMonitorTrigger{
					Attempt: projection.Ordinal, ArtifactDigest: projection.ArtifactDigest,
					BundleDigest: projection.BundleDigest, Monitor: finding.Monitor,
					Step: finding.Step, Class: finding.Class, Message: finding.Message,
				})
			}
		}
		observation.Attempts = append(observation.Attempts, attempt)
	}
	if len(measured) > 0 {
		pss, err := newCampaignPSSObservation(measured)
		if err != nil {
			return CampaignObservation{}, err
		}
		observation.PSS = &pss
		for index := range observation.Attempts {
			for _, witness := range pss.States {
				if witness.FirstAttempt == observation.Attempts[index].Ordinal {
					observation.Attempts[index].NewPSSStates++
				}
			}
		}
	}
	observation.Workload.ResultStatuses = campaignStatusCounts(statusCounts)
	observation.Monitors.Checked = campaignMonitorCounts(monitorCounts)
	sortCampaignMonitorTriggers(observation.Monitors.Triggers)

	sealed, err := observation.seal()
	if err != nil {
		return CampaignObservation{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CampaignObservation{}, err
	}
	return sealed, nil
}

func validateCampaignAttemptProjection(
	source CampaignAttemptSummary,
	projection CampaignAttemptProjection,
) error {
	if projection.Ordinal != source.Ordinal || projection.ArtifactDigest != source.Record.ArtifactDigest ||
		projection.Outcome != source.Record.Outcome || projection.Work != source.Record.Work {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PROJECTION_BINDING_MISMATCH")
	}
	if !projection.ExecutionEvidence {
		if projection.BundleDigest != "" || projection.PSSID != "" || len(projection.CorePSS) != 0 ||
			projection.Faults != nil || projection.Workload != nil || len(projection.CheckedMonitors) != 0 ||
			len(projection.MonitorFindings) != 0 {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_UNBOUND_EVIDENCE")
		}
		return nil
	}
	if !validSHA256(projection.BundleDigest) || projection.PSSID == "" || len(projection.CorePSS) == 0 ||
		projection.Work.Primary.SchedulerDecisions != len(projection.CorePSS)-1 {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_EXECUTION_EVIDENCE_INVALID")
	}
	for index, sample := range projection.CorePSS {
		if sample.Step != index || sample.Key != sample.State.Digest || sample.State.MappingID != projection.PSSID {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_SAMPLE_INVALID")
		}
		if err := sample.State.Validate(); err != nil {
			return err
		}
	}
	if projection.Faults != nil && !validCampaignFaultUsage(*projection.Faults) {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_FAULT_USAGE_INVALID")
	}
	if projection.Workload != nil && !validCampaignWorkloadProjection(*projection.Workload) {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_WORKLOAD_INVALID")
	}
	checked := make(map[string]bool, len(projection.CheckedMonitors))
	for _, monitor := range projection.CheckedMonitors {
		if monitor == "" || checked[monitor] {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_MONITOR_SET_INVALID")
		}
		checked[monitor] = true
	}
	for _, finding := range projection.MonitorFindings {
		if !checked[finding.Monitor] || finding.Step < 0 || finding.Message == "" ||
			(finding.Class != CampaignMonitorRequirement && finding.Class != CampaignMonitorEvidenceInvalid) {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_MONITOR_FINDING_INVALID")
		}
	}
	return nil
}

func newCampaignPSSObservation(measured []protocolstate.MeasuredRun) (CampaignPSSObservation, error) {
	summary, err := protocolstate.Aggregate(measured[0].Initial.State.(psscore.State).MappingID, measured)
	if err != nil {
		return CampaignPSSObservation{}, err
	}
	result := CampaignPSSObservation{
		PSSID: summary.PSSID, EvidenceAttempts: summary.Runs,
		TotalDecisions: summary.TotalDecisions, TotalSamples: summary.TotalDecisions + summary.Runs,
		InitialStateKey: summary.InitialStateKey, UniqueStates: summary.UniqueStates,
		PrefixArea: summary.PrefixArea, SelfNormalizedArea: summary.SelfNormalizedArea,
	}
	keys := make([]string, 0, len(summary.States))
	for _, point := range summary.Curve {
		result.Curve = append(result.Curve, CampaignPSSPoint{
			Decisions: point.Decisions, Attempt: point.Run, AttemptDecision: point.RunDecision,
			UniqueStates: point.UniqueStates, NewState: point.NewState,
		})
	}
	for _, witness := range summary.States {
		state, ok := witness.State.(psscore.State)
		if !ok {
			return CampaignPSSObservation{}, errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_TYPE_INVALID")
		}
		keys = append(keys, witness.Key)
		result.States = append(result.States, CampaignPSSWitness{
			Key: witness.Key, FirstAttempt: witness.FirstRun,
			FirstAttemptDecision: witness.FirstRunDecision,
			FirstGlobalDecision:  witness.FirstGlobalDecision, TraceStep: witness.TraceStep,
			State: cloneCampaignPSSState(state),
		})
	}
	sort.Strings(keys)
	result.StateSetDigest, err = portableJSONDigest(keys)
	return result, err
}

func (observation CampaignObservation) Validate() error {
	if observation.SchemaVersion != CampaignObservationVersion ||
		!validMethodToken(observation.CampaignID) || !validSHA256(observation.ConfigDigest) ||
		!validMethodToken(observation.TargetID) || !validSHA256(observation.TargetIdentityDigest) ||
		!validSHA256(observation.ExperimentSpecDigest) || !validSHA256(observation.SummaryDigest) ||
		!validSHA256(observation.HeadCheckpointDigest) {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_IDENTITY_INVALID")
	}
	if err := validateCampaignTerminalObservation(observation.Terminal, len(observation.Attempts)); err != nil {
		return err
	}
	if err := validateCampaignObservationAttempts(observation.Attempts, observation.PSS); err != nil {
		return err
	}
	if !validCampaignFaultObservation(observation.Faults, observation.Attempts) ||
		!validCampaignWorkloadObservation(observation.Workload, observation.Attempts) ||
		!validCampaignMonitorObservation(observation.Monitors, observation.Attempts) {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_AGGREGATE_INVALID")
	}
	sealed, err := observation.seal()
	if err != nil || !validSHA256(observation.Digest) || sealed.Digest != observation.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_DIGEST_MISMATCH")
	}
	return nil
}

func validateCampaignTerminalObservation(terminal CampaignTerminalObservation, attempts int) error {
	if terminal.Attempts != attempts || terminal.Completed < 0 || terminal.Rejected < 0 ||
		terminal.Failed < 0 || terminal.Invalid < 0 || terminal.ElapsedMillis < 0 ||
		terminal.Completed+terminal.Rejected+terminal.Failed+terminal.Invalid != attempts {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_TERMINAL_INVALID")
	}
	if err := validateMethodWork(terminal.Work); err != nil {
		return err
	}
	switch terminal.Status {
	case CampaignSummaryStatusRunning:
		if terminal.StopReason != CampaignStopRunning || terminal.FailureDigest != "" {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_TERMINAL_INVALID")
		}
	case CampaignSummaryStatusStopped:
		if terminal.StopReason == CampaignStopRunning || terminal.FailureDigest != "" {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_TERMINAL_INVALID")
		}
	case CampaignSummaryStatusFailed:
		if terminal.StopReason != CampaignStopRunning || !validSHA256(terminal.FailureDigest) {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_TERMINAL_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_TERMINAL_INVALID")
	}
	switch terminal.StopReason {
	case CampaignStopRunning, CampaignStopAttemptLimit, CampaignStopLogicalBudget, CampaignStopWallClock:
	default:
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_TERMINAL_INVALID")
	}
	return nil
}

func validateCampaignObservationAttempts(
	attempts []CampaignAttemptObservation,
	pss *CampaignPSSObservation,
) error {
	evidence := 0
	totalDecisions := 0
	newStates := 0
	for index, attempt := range attempts {
		if attempt.Ordinal != index+1 || !validSHA256(attempt.RecordDigest) ||
			!validSHA256(attempt.ArtifactDigest) {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_ATTEMPT_INVALID")
		}
		switch attempt.Outcome {
		case CampaignAttemptCompleted, CampaignAttemptRejected, CampaignAttemptFailed, CampaignAttemptInvalid:
		default:
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_ATTEMPT_INVALID")
		}
		if !attempt.ExecutionEvidence {
			if attempt.BundleDigest != "" || attempt.ChargedDecisions != 0 || attempt.PSSSamples != 0 ||
				attempt.PSSStates != 0 || attempt.NewPSSStates != 0 || attempt.Faults != nil ||
				attempt.Workload != nil || len(attempt.CheckedMonitors) != 0 || attempt.MonitorTriggers != 0 {
				return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_ATTEMPT_EVIDENCE_INVALID")
			}
			continue
		}
		if !validSHA256(attempt.BundleDigest) || attempt.ChargedDecisions < 0 ||
			attempt.PSSSamples != attempt.ChargedDecisions+1 || attempt.PSSStates <= 0 ||
			attempt.PSSStates > attempt.PSSSamples || attempt.NewPSSStates < 0 ||
			attempt.NewPSSStates > attempt.PSSStates || attempt.MonitorTriggers < 0 ||
			!uniqueNonemptyStrings(attempt.CheckedMonitors) ||
			(attempt.Faults != nil && !validCampaignFaultUsage(*attempt.Faults)) ||
			(attempt.Workload != nil && !validCampaignWorkloadAttempt(*attempt.Workload)) {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_ATTEMPT_EVIDENCE_INVALID")
		}
		evidence++
		totalDecisions += attempt.ChargedDecisions
		newStates += attempt.NewPSSStates
	}
	if evidence == 0 {
		if pss != nil {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_UNEXPECTED")
		}
		return nil
	}
	if pss == nil || pss.EvidenceAttempts != evidence || pss.TotalDecisions != totalDecisions ||
		pss.TotalSamples != totalDecisions+evidence || pss.UniqueStates != newStates {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_TOTAL_MISMATCH")
	}
	return validateCampaignPSSObservation(*pss, attempts)
}

func validateCampaignPSSObservation(pss CampaignPSSObservation, attempts []CampaignAttemptObservation) error {
	if pss.PSSID == "" || pss.EvidenceAttempts <= 0 || pss.TotalDecisions < 0 ||
		pss.TotalSamples != pss.TotalDecisions+pss.EvidenceAttempts ||
		!validSHA256(pss.InitialStateKey) || pss.UniqueStates <= 0 ||
		pss.UniqueStates != len(pss.States) || len(pss.Curve) != pss.TotalDecisions ||
		!validSHA256(pss.StateSetDigest) {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_INVALID")
	}
	seen := make(map[string]bool, len(pss.States))
	keys := make([]string, 0, len(pss.States))
	witnesses := make(map[int]CampaignPSSWitness, len(pss.States))
	for _, witness := range pss.States {
		if !validSHA256(witness.Key) || seen[witness.Key] || witness.State.Digest != witness.Key ||
			witness.State.MappingID != pss.PSSID || witness.FirstAttempt <= 0 ||
			witness.FirstAttemptDecision < 0 || witness.FirstGlobalDecision < 0 || witness.TraceStep < 0 {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_WITNESS_INVALID")
		}
		if err := witness.State.Validate(); err != nil {
			return err
		}
		seen[witness.Key] = true
		keys = append(keys, witness.Key)
		if _, duplicate := witnesses[witness.FirstGlobalDecision]; duplicate {
			return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_WITNESS_INVALID")
		}
		witnesses[witness.FirstGlobalDecision] = witness
	}
	initial, ok := witnesses[0]
	if !ok || initial.Key != pss.InitialStateKey || initial.FirstAttemptDecision != 0 || initial.TraceStep != 0 {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_INITIAL_INVALID")
	}
	sort.Strings(keys)
	digest, err := portableJSONDigest(keys)
	if err != nil || digest != pss.StateSetDigest {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_STATE_SET_MISMATCH")
	}
	area := int64(0)
	unique := 1
	pointIndex := 0
	for _, attempt := range attempts {
		if !attempt.ExecutionEvidence {
			continue
		}
		for decision := 1; decision <= attempt.ChargedDecisions; decision++ {
			point := pss.Curve[pointIndex]
			pointIndex++
			if point.NewState {
				unique++
				witness, exists := witnesses[point.Decisions]
				if !exists || witness.FirstAttempt != attempt.Ordinal ||
					witness.FirstAttemptDecision != decision || witness.TraceStep != decision {
					return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_CURVE_WITNESS_MISMATCH")
				}
			} else if _, exists := witnesses[point.Decisions]; exists {
				return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_CURVE_WITNESS_MISMATCH")
			}
			if point.Decisions != pointIndex || point.Attempt != attempt.Ordinal ||
				point.AttemptDecision != decision || point.UniqueStates != unique {
				return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_CURVE_INVALID")
			}
			area += int64(unique)
		}
	}
	wantArea := float64(0)
	if pss.TotalDecisions > 0 {
		wantArea = float64(area) / float64(pss.TotalDecisions*pss.UniqueStates)
	}
	if pointIndex != pss.TotalDecisions || unique != pss.UniqueStates || area != pss.PrefixArea ||
		pss.SelfNormalizedArea != wantArea {
		return errors.New("EXPERIMENT_CAMPAIGN_OBSERVATION_PSS_AREA_INVALID")
	}
	return nil
}

func validCampaignFaultObservation(faults CampaignFaultObservation, attempts []CampaignAttemptObservation) bool {
	want := CampaignFaultObservation{}
	for _, attempt := range attempts {
		if !attempt.ExecutionEvidence {
			continue
		}
		want.ObservedAttempts++
		if attempt.Faults != nil {
			addCampaignFaultUsage(&want.Usage, *attempt.Faults)
		}
	}
	return validCampaignFaultUsage(faults.Usage) && faults == want
}

func validCampaignWorkloadObservation(workload CampaignWorkloadObservation, attempts []CampaignAttemptObservation) bool {
	want := CampaignWorkloadObservation{}
	statuses := make(map[string]int)
	for _, attempt := range attempts {
		if attempt.Workload != nil {
			want.ObservedAttempts++
			want.Planned += attempt.Workload.Planned
			want.Offered += attempt.Workload.Offered
			want.Completed += attempt.Workload.Completed
			want.Pending += attempt.Workload.Pending
			for _, status := range attempt.Workload.ResultStatuses {
				statuses[status.Status] += status.Count
			}
		}
	}
	want.ResultStatuses = campaignStatusCounts(statuses)
	if workload.ObservedAttempts != want.ObservedAttempts || workload.Planned != want.Planned ||
		workload.Offered != want.Offered || workload.Completed != want.Completed ||
		workload.Pending != want.Pending || len(workload.ResultStatuses) != len(want.ResultStatuses) ||
		workload.Planned < 0 || workload.Offered < 0 ||
		workload.Completed < 0 || workload.Pending < 0 || workload.Offered > workload.Planned ||
		workload.Completed > workload.Offered || workload.Pending != workload.Planned-workload.Completed {
		return false
	}
	totalStatuses := 0
	last := ""
	for index, status := range workload.ResultStatuses {
		if status.Status == "" || status.Count <= 0 || (last != "" && last >= status.Status) {
			return false
		}
		if status != want.ResultStatuses[index] {
			return false
		}
		last = status.Status
		totalStatuses += status.Count
	}
	return totalStatuses == workload.Completed
}

func validCampaignMonitorObservation(monitors CampaignMonitorObservation, attempts []CampaignAttemptObservation) bool {
	observed := 0
	counts := make(map[string]int)
	triggers := 0
	for _, attempt := range attempts {
		if !attempt.ExecutionEvidence {
			continue
		}
		observed++
		triggers += attempt.MonitorTriggers
		for _, monitor := range attempt.CheckedMonitors {
			counts[monitor]++
		}
	}
	if monitors.ObservedAttempts != observed || len(monitors.Triggers) != triggers ||
		len(monitors.Checked) != len(counts) {
		return false
	}
	last := ""
	for _, checked := range monitors.Checked {
		if checked.Monitor == "" || checked.Count != counts[checked.Monitor] ||
			(last != "" && last >= checked.Monitor) {
			return false
		}
		last = checked.Monitor
	}
	previousAttempt := 0
	for _, trigger := range monitors.Triggers {
		if trigger.Attempt <= 0 || trigger.Attempt > len(attempts) || trigger.Attempt < previousAttempt ||
			trigger.ArtifactDigest != attempts[trigger.Attempt-1].ArtifactDigest ||
			trigger.BundleDigest != attempts[trigger.Attempt-1].BundleDigest ||
			counts[trigger.Monitor] == 0 ||
			!containsSortedString(attempts[trigger.Attempt-1].CheckedMonitors, trigger.Monitor) ||
			trigger.Step < 0 || trigger.Message == "" ||
			(trigger.Class != CampaignMonitorRequirement && trigger.Class != CampaignMonitorEvidenceInvalid) {
			return false
		}
		previousAttempt = trigger.Attempt
	}
	return true
}

func validCampaignFaultUsage(usage FaultUsage) bool {
	return usage.Crashes >= 0 && usage.MessageDrops >= 0 &&
		usage.MessageDuplicates >= 0 && usage.Partitions >= 0
}

func validCampaignWorkloadProjection(report WorkloadRunReport) bool {
	if report.Planned < 0 || report.Offered < 0 || report.Completed < 0 || report.Pending < 0 ||
		report.Offered > report.Planned || report.Completed > report.Offered ||
		report.Pending != report.Planned-report.Completed || len(report.Results) != report.Completed {
		return false
	}
	for _, result := range report.Results {
		if result.Status == "" {
			return false
		}
	}
	return true
}

func newCampaignWorkloadAttemptObservation(report WorkloadRunReport) *CampaignWorkloadAttemptObservation {
	statuses := make(map[string]int)
	for _, result := range report.Results {
		statuses[result.Status]++
	}
	return &CampaignWorkloadAttemptObservation{
		Planned: report.Planned, Offered: report.Offered,
		Completed: report.Completed, Pending: report.Pending,
		ResultStatuses: campaignStatusCounts(statuses),
	}
}

func validCampaignWorkloadAttempt(workload CampaignWorkloadAttemptObservation) bool {
	if workload.Planned < 0 || workload.Offered < 0 || workload.Completed < 0 || workload.Pending < 0 ||
		workload.Offered > workload.Planned || workload.Completed > workload.Offered ||
		workload.Pending != workload.Planned-workload.Completed {
		return false
	}
	total := 0
	last := ""
	for _, status := range workload.ResultStatuses {
		if status.Status == "" || status.Count <= 0 || (last != "" && last >= status.Status) {
			return false
		}
		last = status.Status
		total += status.Count
	}
	return total == workload.Completed
}

func addCampaignFaultUsage(total *FaultUsage, current FaultUsage) {
	total.Crashes += current.Crashes
	total.MessageDrops += current.MessageDrops
	total.MessageDuplicates += current.MessageDuplicates
	total.Partitions += current.Partitions
}

func addCampaignWorkload(
	total *CampaignWorkloadObservation,
	current WorkloadRunReport,
	statuses map[string]int,
) {
	total.ObservedAttempts++
	total.Planned += current.Planned
	total.Offered += current.Offered
	total.Completed += current.Completed
	total.Pending += current.Pending
	for _, result := range current.Results {
		statuses[result.Status]++
	}
}

func campaignStatusCounts(counts map[string]int) []CampaignStatusCount {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]CampaignStatusCount, 0, len(keys))
	for _, key := range keys {
		result = append(result, CampaignStatusCount{Status: key, Count: counts[key]})
	}
	return result
}

func campaignMonitorCounts(counts map[string]int) []CampaignMonitorCount {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]CampaignMonitorCount, 0, len(keys))
	for _, key := range keys {
		result = append(result, CampaignMonitorCount{Monitor: key, Count: counts[key]})
	}
	return result
}

func countCampaignPSSStates(samples []CorePSSSample) int {
	seen := make(map[string]bool, len(samples))
	for _, sample := range samples {
		seen[sample.Key] = true
	}
	return len(seen)
}

func cloneFaultUsage(usage *FaultUsage) *FaultUsage {
	if usage == nil {
		return nil
	}
	cloned := *usage
	return &cloned
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func uniqueNonemptyStrings(values []string) bool {
	last := ""
	for _, value := range values {
		if value == "" || (last != "" && last >= value) {
			return false
		}
		last = value
	}
	return true
}

func containsSortedString(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func sortCampaignMonitorTriggers(triggers []CampaignMonitorTrigger) {
	sort.Slice(triggers, func(left, right int) bool {
		if triggers[left].Attempt != triggers[right].Attempt {
			return triggers[left].Attempt < triggers[right].Attempt
		}
		if triggers[left].Monitor != triggers[right].Monitor {
			return triggers[left].Monitor < triggers[right].Monitor
		}
		if triggers[left].Step != triggers[right].Step {
			return triggers[left].Step < triggers[right].Step
		}
		if triggers[left].Class != triggers[right].Class {
			return triggers[left].Class < triggers[right].Class
		}
		return triggers[left].Message < triggers[right].Message
	})
}

func cloneCampaignPSSState(state psscore.State) psscore.State {
	state.Control.Participants = append([]psscore.Participant(nil), state.Control.Participants...)
	state.Control.BlockedLinks = append([]psscore.BlockedLink(nil), state.Control.BlockedLinks...)
	state.Control.Pending = append([]psscore.PendingItem(nil), state.Control.Pending...)
	for index := range state.Control.Pending {
		state.Control.Pending[index].DependencyKinds = append(
			[]control.ItemKind(nil), state.Control.Pending[index].DependencyKinds...,
		)
	}
	state.Semantic.Entities = append([]psscore.Entity(nil), state.Semantic.Entities...)
	state.Semantic.Relations = append([]psscore.Relation(nil), state.Semantic.Relations...)
	return state
}

func (observation CampaignObservation) seal() (CampaignObservation, error) {
	observation.Attempts = append([]CampaignAttemptObservation(nil), observation.Attempts...)
	for index := range observation.Attempts {
		observation.Attempts[index].Faults = cloneFaultUsage(observation.Attempts[index].Faults)
		if observation.Attempts[index].Workload != nil {
			workload := *observation.Attempts[index].Workload
			workload.ResultStatuses = append([]CampaignStatusCount(nil), workload.ResultStatuses...)
			observation.Attempts[index].Workload = &workload
		}
		observation.Attempts[index].CheckedMonitors = append(
			[]string(nil), observation.Attempts[index].CheckedMonitors...,
		)
	}
	if observation.PSS != nil {
		pss := *observation.PSS
		pss.Curve = append([]CampaignPSSPoint(nil), pss.Curve...)
		pss.States = append([]CampaignPSSWitness(nil), pss.States...)
		for index := range pss.States {
			pss.States[index].State = cloneCampaignPSSState(pss.States[index].State)
		}
		observation.PSS = &pss
	}
	observation.Workload.ResultStatuses = append(
		[]CampaignStatusCount(nil), observation.Workload.ResultStatuses...,
	)
	observation.Monitors.Checked = append(
		[]CampaignMonitorCount(nil), observation.Monitors.Checked...,
	)
	observation.Monitors.Triggers = append(
		[]CampaignMonitorTrigger(nil), observation.Monitors.Triggers...,
	)
	observation.Digest = ""
	digest, err := portableJSONDigest(observation)
	if err != nil {
		return CampaignObservation{}, err
	}
	observation.Digest = digest
	return observation, nil
}

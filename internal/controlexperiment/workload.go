package controlexperiment

import (
	"context"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const (
	WorkloadPlanVersion            = "consensus-atlas/semantic-invoke-workload/v1"
	TargetSingleCoordinatingMember = "single-coordinating-participant"
	MaxWorkloadInvocations         = 8
)

// WorkloadInvocation keeps the target-specific command opaque. ID is the
// expected generic ClientResponse.RequestID; ExpectedStatus is a completion
// condition, not an Oracle verdict.
type WorkloadInvocation struct {
	ID             string                  `json:"id"`
	Input          control.PayloadEnvelope `json:"input"`
	ExpectedStatus string                  `json:"expected_status"`
}

type WorkloadPlan struct {
	SchemaVersion  string               `json:"schema_version"`
	ID             string               `json:"id"`
	TargetSelector string               `json:"target_selector"`
	Invocations    []WorkloadInvocation `json:"invocations"`
}

func (plan WorkloadPlan) Validate() error {
	if plan.SchemaVersion != WorkloadPlanVersion || plan.ID == "" ||
		plan.TargetSelector != TargetSingleCoordinatingMember {
		return errors.New("EXPERIMENT_WORKLOAD_IDENTITY_INVALID")
	}
	if len(plan.Invocations) == 0 || len(plan.Invocations) > MaxWorkloadInvocations {
		return fmt.Errorf("EXPERIMENT_WORKLOAD_SIZE_INVALID: %d", len(plan.Invocations))
	}
	seen := make(map[string]bool, len(plan.Invocations))
	for _, invocation := range plan.Invocations {
		if invocation.ID == "" || invocation.ExpectedStatus == "" || seen[invocation.ID] {
			return fmt.Errorf("EXPERIMENT_WORKLOAD_INVOCATION_INVALID: %s", invocation.ID)
		}
		if err := invocation.Input.Validate(); err != nil {
			return fmt.Errorf("EXPERIMENT_WORKLOAD_INPUT_INVALID: %s: %w", invocation.ID, err)
		}
		seen[invocation.ID] = true
	}
	return nil
}

func (plan WorkloadPlan) Digest() (string, error) {
	if err := plan.Validate(); err != nil {
		return "", err
	}
	return control.CanonicalDigest(plan)
}

// FaultEnvelope is a per-run upper bound. It never creates actions or edits
// Runtime's enabled set. Deterministic policies reject a selected action that
// exceeds it; local stochastic baselines may filter their selectable view.
type FaultEnvelope struct {
	MaxCrashes           int `json:"max_crashes"`
	MaxConcurrentCrashes int `json:"max_concurrent_crashes"`
	MaxMessageDrops      int `json:"max_message_drops"`
	MaxMessageDuplicates int `json:"max_message_duplicates"`
	MaxPartitions        int `json:"max_partitions"`
	MaxActivePartitions  int `json:"max_active_partitions"`
}

func (envelope FaultEnvelope) Validate() error {
	if envelope.MaxCrashes < 0 || envelope.MaxConcurrentCrashes < 0 ||
		envelope.MaxMessageDrops < 0 || envelope.MaxMessageDuplicates < 0 ||
		envelope.MaxPartitions < 0 || envelope.MaxActivePartitions < 0 {
		return errors.New("EXPERIMENT_FAULT_ENVELOPE_NEGATIVE")
	}
	if envelope.MaxConcurrentCrashes > envelope.MaxCrashes ||
		envelope.MaxActivePartitions > envelope.MaxPartitions {
		return errors.New("EXPERIMENT_FAULT_ENVELOPE_CONCURRENCY_INVALID")
	}
	return nil
}

type FaultUsage struct {
	Crashes           int `json:"crashes"`
	MessageDrops      int `json:"message_drops"`
	MessageDuplicates int `json:"message_duplicates"`
	Partitions        int `json:"partitions"`
}

func (usage FaultUsage) validate(envelope FaultEnvelope) error {
	if usage.Crashes < 0 || usage.Crashes > envelope.MaxCrashes ||
		usage.MessageDrops < 0 || usage.MessageDrops > envelope.MaxMessageDrops ||
		usage.MessageDuplicates < 0 || usage.MessageDuplicates > envelope.MaxMessageDuplicates ||
		usage.Partitions < 0 || usage.Partitions > envelope.MaxPartitions {
		return errors.New("EXPERIMENT_FAULT_USAGE_INVALID")
	}
	return nil
}

func (usage FaultUsage) check(
	envelope FaultEnvelope,
	action control.Action,
	snapshot controlruntime.Snapshot,
) error {
	exceeded := false
	switch action.Kind {
	case control.ActionCrash:
		stopped := 0
		for _, node := range snapshot.Nodes {
			if node.Lifecycle == control.NodeStopped {
				stopped++
			}
		}
		exceeded = usage.Crashes >= envelope.MaxCrashes ||
			stopped >= envelope.MaxConcurrentCrashes
	case control.ActionDropMessage:
		exceeded = usage.MessageDrops >= envelope.MaxMessageDrops
	case control.ActionDuplicateMessage:
		exceeded = usage.MessageDuplicates >= envelope.MaxMessageDuplicates
	case control.ActionPartition:
		exceeded = usage.Partitions >= envelope.MaxPartitions ||
			len(snapshot.Partitions) >= envelope.MaxActivePartitions
	}
	if exceeded {
		return fmt.Errorf("EXPERIMENT_FAULT_ENVELOPE_EXCEEDED: %s", action.Kind)
	}
	return nil
}

// constrain returns the Runtime-enabled Actions that remain selectable under
// the frozen envelope. It does not change Runtime state or its enabled set.
// Local stochastic baselines use this view so a hard budget is not converted
// into a seed-dependent execution failure after the budget has been consumed.
func (usage FaultUsage) constrain(
	envelope FaultEnvelope,
	enabled []control.Action,
	snapshot controlruntime.Snapshot,
) []control.Action {
	result := make([]control.Action, 0, len(enabled))
	for _, action := range enabled {
		if usage.check(envelope, action, snapshot) == nil {
			result = append(result, action)
		}
	}
	return result
}

func (usage *FaultUsage) record(action control.Action) {
	switch action.Kind {
	case control.ActionCrash:
		usage.Crashes++
	case control.ActionDropMessage:
		usage.MessageDrops++
	case control.ActionDuplicateMessage:
		usage.MessageDuplicates++
	case control.ActionPartition:
		usage.Partitions++
	}
}

type WorkloadResult struct {
	InvocationID  string         `json:"invocation_id"`
	Owner         control.NodeID `json:"owner"`
	Status        string         `json:"status"`
	PayloadDigest string         `json:"payload_digest"`
}

type WorkloadRunReport struct {
	PlanID        string           `json:"plan_id"`
	PlanDigest    string           `json:"plan_digest"`
	Planned       int              `json:"planned"`
	Offered       int              `json:"offered"`
	Completed     int              `json:"completed"`
	Results       []WorkloadResult `json:"results"`
	ResultsDigest string           `json:"results_digest"`
}

func (report WorkloadRunReport) validate(plan WorkloadPlan) error {
	planDigest, err := plan.Digest()
	if err != nil {
		return err
	}
	if report.PlanID != plan.ID || report.PlanDigest != planDigest ||
		report.Planned != len(plan.Invocations) || report.Offered != report.Planned ||
		report.Completed != report.Planned || len(report.Results) != report.Planned {
		return errors.New("EXPERIMENT_WORKLOAD_REPORT_MISMATCH")
	}
	for index, result := range report.Results {
		invocation := plan.Invocations[index]
		if result.InvocationID != invocation.ID || result.Owner == "" ||
			result.Status != invocation.ExpectedStatus || !validSHA256(result.PayloadDigest) {
			return fmt.Errorf("EXPERIMENT_WORKLOAD_RESULT_INVALID: %s", result.InvocationID)
		}
	}
	digest, err := control.CanonicalDigest(report.Results)
	if err != nil {
		return err
	}
	if digest != report.ResultsDigest {
		return errors.New("EXPERIMENT_WORKLOAD_RESULTS_DIGEST_MISMATCH")
	}
	return nil
}

func offerNextWorkloadInvocation(
	ctx context.Context,
	plan *WorkloadPlan,
	offered int,
	runtime *controlruntime.Runtime,
	mapper psscore.SemanticMapper,
	evidence control.EvidenceEnvelope,
) (control.ActionID, bool, error) {
	if plan == nil || offered >= len(plan.Invocations) {
		return "", false, nil
	}
	snapshot := runtime.Snapshot()
	if offered > 0 {
		previous := plan.Invocations[offered-1]
		result, completed, err := completedInvocation(snapshot, previous.ID)
		if err != nil {
			return "", false, err
		}
		if !completed {
			return "", false, nil
		}
		if result.Status != previous.ExpectedStatus {
			return "", false, fmt.Errorf(
				"EXPERIMENT_WORKLOAD_STATUS_MISMATCH: %s/%s", previous.ID, result.Status,
			)
		}
	}
	target, ready, err := coordinatingTarget(mapper, evidence, snapshot)
	if err != nil || !ready {
		return "", false, err
	}
	actionID, err := runtime.OfferInvoke(ctx, target, plan.Invocations[offered].Input)
	if err != nil {
		if errors.Is(err, controlruntime.ErrInvokeNotEligible) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("EXPERIMENT_WORKLOAD_OFFER_FAILED: %w", err)
	}
	return actionID, true, nil
}

func coordinatingTarget(
	mapper psscore.SemanticMapper,
	evidence control.EvidenceEnvelope,
	snapshot controlruntime.Snapshot,
) (control.NodeID, bool, error) {
	observation, err := mapper.Map(evidence)
	if err != nil {
		return "", false, err
	}
	if observation.LogicalTime != snapshot.LogicalTime {
		return "", false, errors.New("EXPERIMENT_WORKLOAD_EVIDENCE_STALE")
	}
	var candidate control.NodeID
	for _, entity := range observation.Graph.Entities {
		if entity.Kind != psscore.EntityParticipant || entity.Mode != psscore.ModeCoordinating {
			continue
		}
		if candidate != "" {
			return "", false, errors.New("EXPERIMENT_WORKLOAD_TARGET_AMBIGUOUS")
		}
		candidate = control.NodeID(entity.ID)
	}
	if candidate == "" {
		return "", false, nil
	}
	for _, node := range snapshot.Nodes {
		if node.Ref.Node == candidate && node.Lifecycle == control.NodeRunning {
			return candidate, true, nil
		}
	}
	return "", false, fmt.Errorf("EXPERIMENT_WORKLOAD_TARGET_INVALID: %s", candidate)
}

func finishWorkload(
	plan *WorkloadPlan,
	offered int,
	snapshot controlruntime.Snapshot,
) (*WorkloadRunReport, error) {
	if plan == nil {
		return nil, nil
	}
	if offered != len(plan.Invocations) {
		return nil, fmt.Errorf("EXPERIMENT_WORKLOAD_NOT_OFFERED: %d/%d", offered, len(plan.Invocations))
	}
	report := WorkloadRunReport{PlanID: plan.ID, Planned: len(plan.Invocations), Offered: offered}
	var err error
	report.PlanDigest, err = plan.Digest()
	if err != nil {
		return nil, err
	}
	for _, invocation := range plan.Invocations {
		result, completed, err := completedInvocation(snapshot, invocation.ID)
		if err != nil {
			return nil, err
		}
		if !completed {
			return nil, fmt.Errorf("EXPERIMENT_WORKLOAD_INCOMPLETE: %s", invocation.ID)
		}
		if result.Status != invocation.ExpectedStatus {
			return nil, fmt.Errorf(
				"EXPERIMENT_WORKLOAD_STATUS_MISMATCH: %s/%s", invocation.ID, result.Status,
			)
		}
		report.Results = append(report.Results, result)
	}
	report.Completed = len(report.Results)
	report.ResultsDigest, err = control.CanonicalDigest(report.Results)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

func completedInvocation(
	snapshot controlruntime.Snapshot,
	requestID string,
) (WorkloadResult, bool, error) {
	var result WorkloadResult
	found := false
	for _, item := range snapshot.Items {
		if item.Kind != control.ItemClientResult || item.Value.Response == nil ||
			item.Value.Response.RequestID != requestID {
			continue
		}
		if found {
			return WorkloadResult{}, false, fmt.Errorf("EXPERIMENT_WORKLOAD_RESULT_DUPLICATE: %s", requestID)
		}
		found = true
		if item.State != control.ItemCompleted {
			continue
		}
		response := item.Value.Response
		result = WorkloadResult{
			InvocationID: requestID, Owner: response.Owner.Node,
			Status: response.Status, PayloadDigest: response.Payload.Digest,
		}
	}
	return result, found && result.InvocationID != "", nil
}

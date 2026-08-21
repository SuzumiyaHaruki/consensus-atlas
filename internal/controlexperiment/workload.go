package controlexperiment

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const (
	WorkloadPlanVersion            = "consensus-atlas/semantic-invoke-workload/v1"
	TargetSingleCoordinatingMember = "single-coordinating-participant"
	MaxWorkloadInvocations         = 8
	WorkloadRouteReady             = "ready"
	WorkloadRouteNoCandidate       = "no-candidate"
	WorkloadRouteAmbiguous         = "ambiguous"
	WorkloadRouteTargetNotRunning  = "target-not-running"
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
// Runtime's enabled set. Every policy receives the same admissible view after
// this envelope has been applied.
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

// constrain returns the Runtime-enabled Actions that remain admissible under
// the frozen envelope. It preserves Runtime order and does not change Runtime
// state, Action identity, or the Runtime-enabled frontier.
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

func admissibleActions(
	envelope *FaultEnvelope,
	usage FaultUsage,
	enabled []control.Action,
	snapshot controlruntime.Snapshot,
) []control.Action {
	if envelope == nil {
		return append([]control.Action(nil), enabled...)
	}
	return usage.constrain(*envelope, enabled, snapshot)
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

func faultUsageFromRecords(records []controlruntime.ActionRecord) FaultUsage {
	usage := FaultUsage{}
	for _, record := range records {
		usage.record(record.Action)
	}
	return usage
}

type WorkloadResult struct {
	InvocationID  string         `json:"invocation_id"`
	Owner         control.NodeID `json:"owner"`
	Status        string         `json:"status"`
	PayloadDigest string         `json:"payload_digest"`
}

// WorkloadRoute is target-owned protocol knowledge. The Experiment validates
// canonical candidates and lifecycle state but never interprets leader, term,
// view, quorum, or another protocol role.
type WorkloadRoute struct {
	LogicalTime uint64           `json:"logical_time"`
	Candidates  []control.NodeID `json:"candidates"`
}

type WorkloadRouter interface {
	ID() string
	Route(selector string, evidence control.EvidenceEnvelope) (WorkloadRoute, error)
}

type WorkloadRouteReport struct {
	RouterID   string           `json:"router_id"`
	Selector   string           `json:"selector"`
	Candidates []control.NodeID `json:"candidates"`
	Status     string           `json:"status"`
}

func (report WorkloadRouteReport) validate(routerID string, selector string) error {
	if report.RouterID == "" || report.RouterID != routerID || report.Selector != selector {
		return errors.New("EXPERIMENT_WORKLOAD_ROUTE_IDENTITY_INVALID")
	}
	switch report.Status {
	case WorkloadRouteReady, WorkloadRouteNoCandidate, WorkloadRouteAmbiguous,
		WorkloadRouteTargetNotRunning:
	default:
		return errors.New("EXPERIMENT_WORKLOAD_ROUTE_STATUS_INVALID")
	}
	for index, candidate := range report.Candidates {
		if candidate == "" || (index > 0 && report.Candidates[index-1] >= candidate) {
			return errors.New("EXPERIMENT_WORKLOAD_ROUTE_CANDIDATES_INVALID")
		}
	}
	if (report.Status == WorkloadRouteNoCandidate && len(report.Candidates) != 0) ||
		(report.Status == WorkloadRouteAmbiguous && len(report.Candidates) < 2) ||
		((report.Status == WorkloadRouteReady || report.Status == WorkloadRouteTargetNotRunning) &&
			len(report.Candidates) != 1) {
		return errors.New("EXPERIMENT_WORKLOAD_ROUTE_CARDINALITY_INVALID")
	}
	return nil
}

type WorkloadRunReport struct {
	PlanID        string               `json:"plan_id"`
	PlanDigest    string               `json:"plan_digest"`
	Planned       int                  `json:"planned"`
	Offered       int                  `json:"offered"`
	Completed     int                  `json:"completed"`
	Pending       int                  `json:"pending,omitempty"`
	Results       []WorkloadResult     `json:"results"`
	ResultsDigest string               `json:"results_digest"`
	FinalRoute    *WorkloadRouteReport `json:"final_route,omitempty"`
}

func (report WorkloadRunReport) validate(plan WorkloadPlan, strict bool, routerID string) error {
	planDigest, err := plan.Digest()
	if err != nil {
		return err
	}
	if report.PlanID != plan.ID || report.PlanDigest != planDigest ||
		report.Planned != len(plan.Invocations) || report.Offered < 0 ||
		report.Offered > report.Planned || report.Completed < 0 ||
		report.Completed > report.Offered || len(report.Results) != report.Completed {
		return errors.New("EXPERIMENT_WORKLOAD_REPORT_MISMATCH")
	}
	if strict {
		if report.Offered != report.Planned || report.Completed != report.Planned ||
			report.Pending != 0 || report.FinalRoute != nil {
			return errors.New("EXPERIMENT_WORKLOAD_REPORT_MISMATCH")
		}
	} else {
		if report.Pending != report.Planned-report.Completed {
			return errors.New("EXPERIMENT_WORKLOAD_PENDING_MISMATCH")
		}
		if report.Offered < report.Planned {
			if report.FinalRoute == nil || report.FinalRoute.validate(routerID, plan.TargetSelector) != nil {
				return errors.New("EXPERIMENT_WORKLOAD_FINAL_ROUTE_REQUIRED")
			}
		} else if report.FinalRoute != nil {
			return errors.New("EXPERIMENT_WORKLOAD_FINAL_ROUTE_UNEXPECTED")
		}
	}
	invocations := make(map[string]WorkloadInvocation, len(plan.Invocations))
	for _, invocation := range plan.Invocations {
		invocations[invocation.ID] = invocation
	}
	seen := make(map[string]bool, len(report.Results))
	for _, result := range report.Results {
		invocation, exists := invocations[result.InvocationID]
		if !exists || seen[result.InvocationID] || result.Owner == "" || result.Status == "" ||
			!validSHA256(result.PayloadDigest) || (strict && result.Status != invocation.ExpectedStatus) {
			return fmt.Errorf("EXPERIMENT_WORKLOAD_RESULT_INVALID: %s", result.InvocationID)
		}
		seen[result.InvocationID] = true
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
	router WorkloadRouter,
	evidence control.EvidenceEnvelope,
	strict bool,
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
		if strict && result.Status != previous.ExpectedStatus {
			return "", false, fmt.Errorf(
				"EXPERIMENT_WORKLOAD_STATUS_MISMATCH: %s/%s", previous.ID, result.Status,
			)
		}
	}
	target, _, ready, err := resolveWorkloadTarget(router, plan.TargetSelector, evidence, snapshot)
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

func resolveWorkloadTarget(
	router WorkloadRouter,
	selector string,
	evidence control.EvidenceEnvelope,
	snapshot controlruntime.Snapshot,
) (control.NodeID, WorkloadRouteReport, bool, error) {
	if router == nil || router.ID() == "" {
		return "", WorkloadRouteReport{}, false, errors.New("EXPERIMENT_WORKLOAD_ROUTER_REQUIRED")
	}
	route, err := router.Route(selector, evidence)
	if err != nil {
		return "", WorkloadRouteReport{}, false, err
	}
	if route.LogicalTime != snapshot.LogicalTime {
		return "", WorkloadRouteReport{}, false, errors.New("EXPERIMENT_WORKLOAD_EVIDENCE_STALE")
	}
	candidates := make([]control.NodeID, len(route.Candidates))
	copy(candidates, route.Candidates)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	for index, candidate := range candidates {
		if candidate == "" || (index > 0 && candidates[index-1] == candidate) {
			return "", WorkloadRouteReport{}, false, errors.New("EXPERIMENT_WORKLOAD_ROUTE_INVALID")
		}
	}
	report := WorkloadRouteReport{
		RouterID: router.ID(), Selector: selector, Candidates: candidates,
	}
	if len(candidates) == 0 {
		report.Status = WorkloadRouteNoCandidate
		return "", report, false, nil
	}
	if len(candidates) > 1 {
		report.Status = WorkloadRouteAmbiguous
		return "", report, false, nil
	}
	candidate := candidates[0]
	for _, node := range snapshot.Nodes {
		if node.Ref.Node == candidate && node.Lifecycle == control.NodeRunning {
			report.Status = WorkloadRouteReady
			return candidate, report, true, nil
		}
	}
	report.Status = WorkloadRouteTargetNotRunning
	return "", report, false, nil
}

func finishWorkload(
	plan *WorkloadPlan,
	offered int,
	snapshot controlruntime.Snapshot,
	router WorkloadRouter,
	evidence control.EvidenceEnvelope,
	strict bool,
) (*WorkloadRunReport, error) {
	if plan == nil {
		return nil, nil
	}
	if strict && offered != len(plan.Invocations) {
		return nil, fmt.Errorf("EXPERIMENT_WORKLOAD_NOT_OFFERED: %d/%d", offered, len(plan.Invocations))
	}
	report := WorkloadRunReport{
		PlanID: plan.ID, Planned: len(plan.Invocations), Offered: offered,
		Results: []WorkloadResult{},
	}
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
			if strict {
				return nil, fmt.Errorf("EXPERIMENT_WORKLOAD_INCOMPLETE: %s", invocation.ID)
			}
			continue
		}
		if strict && result.Status != invocation.ExpectedStatus {
			return nil, fmt.Errorf(
				"EXPERIMENT_WORKLOAD_STATUS_MISMATCH: %s/%s", invocation.ID, result.Status,
			)
		}
		report.Results = append(report.Results, result)
	}
	report.Completed = len(report.Results)
	if !strict {
		report.Pending = report.Planned - report.Completed
		if offered < len(plan.Invocations) {
			_, route, _, err := resolveWorkloadTarget(router, plan.TargetSelector, evidence, snapshot)
			if err != nil {
				return nil, err
			}
			report.FinalRoute = &route
		}
	}
	report.ResultsDigest, err = control.CanonicalDigest(report.Results)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

// validateReplayedWorkloadRoutes independently re-evaluates every recorded
// workload target from the Evidence that preceded its Invoke. Runtime replay
// proves the frozen Action executable; this check additionally proves that the
// target-owned Router still selects that exact sole participant.
func validateReplayedWorkloadRoutes(
	plan *WorkloadPlan,
	offered int,
	router WorkloadRouter,
	trace controlruntime.Trace,
) error {
	if plan == nil {
		if offered != 0 {
			return errors.New("EXPERIMENT_REPLAY_WORKLOAD_OFFER_UNEXPECTED")
		}
		return nil
	}
	evidence := trace.InitialEvidence
	validated := 0
	for _, record := range trace.Records {
		if record.Action.Kind == control.ActionInvoke {
			if validated >= len(plan.Invocations) {
				return errors.New("EXPERIMENT_REPLAY_WORKLOAD_OFFER_OVERFLOW")
			}
			route, err := router.Route(plan.TargetSelector, evidence)
			if err != nil {
				return err
			}
			candidates := make([]control.NodeID, len(route.Candidates))
			copy(candidates, route.Candidates)
			sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
			for index, candidate := range candidates {
				if candidate == "" || (index > 0 && candidates[index-1] == candidate) {
					return errors.New("EXPERIMENT_REPLAY_WORKLOAD_ROUTE_INVALID")
				}
			}
			if route.LogicalTime != record.LogicalTime || len(candidates) != 1 ||
				candidates[0] != record.Action.Node.Node {
				return fmt.Errorf(
					"EXPERIMENT_REPLAY_WORKLOAD_ROUTE_MISMATCH: decision=%d", record.Step,
				)
			}
			validated++
		}
		if record.Evidence != nil {
			evidence = *record.Evidence
			evidence.Payload.Bytes = append([]byte(nil), record.Evidence.Payload.Bytes...)
		}
	}
	if validated != offered {
		return fmt.Errorf("EXPERIMENT_REPLAY_WORKLOAD_OFFER_MISMATCH: %d/%d", validated, offered)
	}
	return nil
}

func workloadCompleted(plan *WorkloadPlan, snapshot controlruntime.Snapshot) (bool, error) {
	if plan == nil {
		return false, nil
	}
	for _, invocation := range plan.Invocations {
		_, completed, err := completedInvocation(snapshot, invocation.ID)
		if err != nil || !completed {
			return false, err
		}
	}
	return true, nil
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

package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const ReportSchemaVersion = "consensus-atlas/adapter-conformance/v2alpha1"

type Factory func() control.Adapter

type WitnessPlan struct {
	Seed                         []byte
	Source                       control.NodeID
	Target                       control.NodeID
	MessageInput                 control.PayloadEnvelope
	DurableMessageInput          control.PayloadEnvelope
	SleepInput                   control.PayloadEnvelope
	OneShotInput                 control.PayloadEnvelope
	CallbackInput                control.PayloadEnvelope
	ExpectedInitialDeadlinePeers []control.NodeID
}

func (plan WitnessPlan) Validate() error {
	if len(plan.Seed) == 0 {
		return errors.New("CONFORMANCE_SEED_REQUIRED")
	}
	if plan.Source == "" || plan.Target == "" || plan.Source == plan.Target {
		return errors.New("CONFORMANCE_DISTINCT_NODES_REQUIRED")
	}
	for name, payload := range map[string]control.PayloadEnvelope{
		"message": plan.MessageInput, "durable-message": plan.DurableMessageInput,
		"sleep": plan.SleepInput, "one-shot": plan.OneShotInput, "callback": plan.CallbackInput,
	} {
		if err := payload.Validate(); err != nil {
			return fmt.Errorf("CONFORMANCE_%s_INPUT_INVALID: %w", name, err)
		}
	}
	if len(plan.ExpectedInitialDeadlinePeers) < 2 {
		return errors.New("CONFORMANCE_TEMPORAL_PEERS_REQUIRED")
	}
	return nil
}

type CaseResult struct {
	ID         string `json:"id"`
	Passed     bool   `json:"passed"`
	ReasonCode string `json:"reason_code,omitempty"`
}

type Report struct {
	SchemaVersion         string       `json:"schema_version"`
	ManifestDigest        string       `json:"manifest_digest"`
	Cases                 []CaseResult `json:"cases"`
	ValidatedCapabilities []string     `json:"validated_capabilities,omitempty"`
	Passed                bool         `json:"passed"`
	Digest                string       `json:"digest"`
}

func (report Report) Seal() (Report, error) {
	report.SchemaVersion = ReportSchemaVersion
	sort.Slice(report.Cases, func(i, j int) bool { return report.Cases[i].ID < report.Cases[j].ID })
	sort.Strings(report.ValidatedCapabilities)
	report.Digest = ""
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		return Report{}, err
	}
	report.Digest = digest
	return report, nil
}

// Validate checks that a persisted conformance report is internally
// consistent and still matches its canonical digest. Qualification accepts
// only reports produced by a trusted conformance runner; this method prevents
// accidental or post-run artifact rewriting, but is not a signature.
func (report Report) Validate() error {
	if report.SchemaVersion != ReportSchemaVersion {
		return errors.New("CONFORMANCE_REPORT_SCHEMA_MISMATCH")
	}
	if report.ManifestDigest == "" {
		return errors.New("CONFORMANCE_REPORT_MANIFEST_DIGEST_REQUIRED")
	}
	if len(report.Cases) == 0 {
		return errors.New("CONFORMANCE_REPORT_CASES_REQUIRED")
	}
	seenCases := make(map[string]struct{}, len(report.Cases))
	allPassed := true
	for _, result := range report.Cases {
		if result.ID == "" {
			return errors.New("CONFORMANCE_REPORT_CASE_ID_REQUIRED")
		}
		if _, ok := seenCases[result.ID]; ok {
			return errors.New("CONFORMANCE_REPORT_CASE_DUPLICATE")
		}
		seenCases[result.ID] = struct{}{}
		if result.Passed && result.ReasonCode != "" {
			return errors.New("CONFORMANCE_REPORT_PASSED_REASON_UNEXPECTED")
		}
		if !result.Passed && result.ReasonCode == "" {
			return errors.New("CONFORMANCE_REPORT_FAILED_REASON_REQUIRED")
		}
		allPassed = allPassed && result.Passed
	}
	if report.Passed != allPassed {
		return errors.New("CONFORMANCE_REPORT_PASS_STATUS_MISMATCH")
	}
	seenCapabilities := make(map[string]struct{}, len(report.ValidatedCapabilities))
	for index, capability := range report.ValidatedCapabilities {
		if capability == "" {
			return errors.New("CONFORMANCE_REPORT_CAPABILITY_ID_REQUIRED")
		}
		if _, ok := seenCapabilities[capability]; ok {
			return errors.New("CONFORMANCE_REPORT_CAPABILITY_DUPLICATE")
		}
		seenCapabilities[capability] = struct{}{}
		if index > 0 && report.ValidatedCapabilities[index-1] > capability {
			return errors.New("CONFORMANCE_REPORT_CAPABILITIES_NOT_SORTED")
		}
	}
	sealed, err := report.Seal()
	if err != nil {
		return err
	}
	if sealed.Digest != report.Digest {
		return errors.New("CONFORMANCE_REPORT_DIGEST_MISMATCH")
	}
	return nil
}

func Evaluate(ctx context.Context, factory Factory, plan WitnessPlan) (Report, error) {
	if err := plan.Validate(); err != nil {
		return Report{}, err
	}
	manifest, err := readFactoryManifest(ctx, factory)
	if err != nil {
		return Report{}, err
	}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		return Report{}, err
	}
	tests := []struct {
		id         string
		capability string
		run        func(context.Context, Factory, WitnessPlan) error
	}{
		{"collect-idempotent", "strict-yield", checkCollectIdempotent},
		{"enabled-check-pure", "pure-enabled-check", checkEnabledPure},
		{"earliest-temporal-only", "earliest-temporal-event", checkEarliestTemporal},
		{"sleep-does-not-skip", "sleep-wakeup", checkSleepBlockedByEarlier},
		{"one-shot-completes", "one-shot-timer", checkOneShotCompletes},
		{"callback-completes", "host-callback", checkCallbackCompletes},
		{"message-crash-retention", "runtime-owned-message", checkMessageCrashRetention},
		{"duplicate-lineage", "message-clone-lineage", checkDuplicateLineage},
		{"partition-retains-message", "partition-retention", checkPartitionRetention},
		{"effect-gates-release", "effect-dependency", checkEffectRelease},
		{"crash-cancels-captured", "crash-incarnation", checkCrashCancelsCaptured},
		{"fresh-replay", "strict-replay", checkFreshReplay},
		{"entropy-audit-stable", "strict-entropy-replay", checkEntropyStable},
	}
	report := Report{SchemaVersion: ReportSchemaVersion, ManifestDigest: manifestDigest, Passed: true}
	for _, test := range tests {
		result := CaseResult{ID: test.id, Passed: true}
		if err := test.run(ctx, factory, plan); err != nil {
			result.Passed = false
			result.ReasonCode = stableReason(err)
			report.Passed = false
		} else {
			report.ValidatedCapabilities = append(report.ValidatedCapabilities, test.capability)
		}
		report.Cases = append(report.Cases, result)
	}
	return report.Seal()
}

func checkCollectIdempotent(ctx context.Context, factory Factory, plan WitnessPlan) error {
	adapter := factory()
	defer closeAdapter(adapter)
	if err := adapter.Reset(ctx, plan.Seed); err != nil {
		return err
	}
	yield, err := adapter.RunUntilYield(ctx)
	if err != nil {
		return err
	}
	first, err := adapter.Collect(ctx, yield.ID)
	if err != nil {
		return err
	}
	second, err := adapter.Collect(ctx, yield.ID)
	if err != nil {
		return err
	}
	left, err := control.CanonicalDigest(first)
	if err != nil {
		return err
	}
	right, err := control.CanonicalDigest(second)
	if err != nil {
		return err
	}
	if left != right {
		return errors.New("COLLECT_NOT_IDEMPOTENT")
	}
	firstEvidence, err := adapter.SnapshotEvidence(ctx)
	if err != nil {
		return err
	}
	secondEvidence, err := adapter.SnapshotEvidence(ctx)
	if err != nil {
		return err
	}
	left, _ = control.CanonicalDigest(firstEvidence)
	right, _ = control.CanonicalDigest(secondEvidence)
	if left != right {
		return errors.New("EVIDENCE_NOT_IDEMPOTENT")
	}
	return nil
}

func checkEnabledPure(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	before, err := runtime.Snapshot().Digest()
	if err != nil {
		return err
	}
	first, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	second, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	after, err := runtime.Snapshot().Digest()
	if err != nil {
		return err
	}
	left, _ := control.CanonicalDigest(first)
	right, _ := control.CanonicalDigest(second)
	if before != after || left != right {
		return errors.New("ENABLED_CHECK_MUTATED_STATE")
	}
	return nil
}

func checkEarliestTemporal(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	initial := actionsByKind(actions, control.ActionFireTemporal)
	if len(initial) != len(plan.ExpectedInitialDeadlinePeers) {
		return fmt.Errorf("TEMPORAL_INITIAL_SET_UNEXPECTED")
	}
	selected, ok := actionByNode(initial, plan.ExpectedInitialDeadlinePeers[0])
	if !ok {
		return errors.New("TEMPORAL_SOURCE_ACTION_MISSING")
	}
	if _, err := runtime.Select(ctx, selected.ID); err != nil {
		return err
	}
	remaining, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	for _, action := range actionsByKind(remaining, control.ActionFireTemporal) {
		if action.Node.Node == selected.Node.Node {
			return errors.New("LATER_TEMPORAL_EXPOSED_BEFORE_EARLIEST_PEER")
		}
	}
	return nil
}

func checkSleepBlockedByEarlier(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	before := itemIDs(runtime.Snapshot())
	invokeID, err := runtime.OfferInvoke(ctx, plan.Source, plan.SleepInput)
	if err != nil {
		return err
	}
	if _, err := runtime.Select(ctx, invokeID); err != nil {
		return err
	}
	var added control.ItemID
	for _, item := range runtime.Snapshot().Items {
		if _, ok := before[item.ID]; !ok && item.Kind == control.ItemTemporal {
			added = item.ID
			if item.State != control.ItemBlocked {
				return errors.New("SLEEP_NOT_BLOCKED_BY_EARLIER_EVENT")
			}
		}
	}
	if added == "" {
		return errors.New("SLEEP_TEMPORAL_ITEM_MISSING")
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	if hasAction(actions, control.ActionFireTemporal, added) {
		return errors.New("SLEEP_ACTION_EXPOSED_TOO_EARLY")
	}
	return nil
}

func checkOneShotCompletes(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	item, err := invokeAndFind(ctx, runtime, plan.Source, plan.OneShotInput, control.ItemTemporal)
	if err != nil {
		return err
	}
	if temporalKindOf(runtime.Snapshot(), item) != control.TemporalOneShotTimer {
		return errors.New("ONE_SHOT_ITEM_KIND_INVALID")
	}
	for decisions := 0; decisions < 16; decisions++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return err
		}
		if fire, ok := actionByItem(actions, control.ActionFireTemporal, item); ok {
			if _, err := runtime.Select(ctx, fire.ID); err != nil {
				return err
			}
			if stateOf(runtime.Snapshot(), item) != control.ItemCompleted {
				return errors.New("ONE_SHOT_NOT_COMPLETED")
			}
			actions, err = runtime.EnabledActions(ctx)
			if err != nil {
				return err
			}
			if hasAction(actions, control.ActionFireTemporal, item) {
				return errors.New("ONE_SHOT_REENABLED")
			}
			return nil
		}
		temporal := actionsByKind(actions, control.ActionFireTemporal)
		if len(temporal) == 0 {
			return errors.New("ONE_SHOT_UNREACHABLE")
		}
		if _, err := runtime.Select(ctx, temporal[0].ID); err != nil {
			return err
		}
	}
	return errors.New("ONE_SHOT_DECISION_BOUND_EXCEEDED")
}

func checkCallbackCompletes(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	item, err := invokeAndFind(ctx, runtime, plan.Source, plan.CallbackInput, control.ItemCallback)
	if err != nil {
		return err
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	complete, ok := actionByItem(actions, control.ActionCompleteCallback, item)
	if !ok {
		return errors.New("COMPLETE_CALLBACK_ACTION_MISSING")
	}
	record, err := runtime.Select(ctx, complete.ID)
	if err != nil {
		return err
	}
	if stateOf(runtime.Snapshot(), item) != control.ItemCompleted ||
		record.EmissionDigest == "" || record.EvidenceDigest == "" {
		return errors.New("CALLBACK_RESULT_NOT_RECORDED")
	}
	return nil
}

func checkMessageCrashRetention(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	message, err := invokeAndFind(ctx, runtime, plan.Source, plan.MessageInput, control.ItemMessage)
	if err != nil {
		return err
	}
	crash, err := findNodeAction(ctx, runtime, control.ActionCrash, plan.Source)
	if err != nil {
		return err
	}
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		return err
	}
	if stateOf(runtime.Snapshot(), message) != control.ItemEnabled {
		return errors.New("RELEASED_MESSAGE_LOST_ON_SOURCE_CRASH")
	}
	crash, err = findNodeAction(ctx, runtime, control.ActionCrash, plan.Target)
	if err != nil {
		return err
	}
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		return err
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	if hasAction(actions, control.ActionDeliverMessage, message) || !hasAction(actions, control.ActionDropMessage, message) {
		return errors.New("STOPPED_TARGET_MESSAGE_ACTIONS_INVALID")
	}
	restart, err := findNodeAction(ctx, runtime, control.ActionRestart, plan.Target)
	if err != nil {
		return err
	}
	if _, err := runtime.Select(ctx, restart.ID); err != nil {
		return err
	}
	actions, err = runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	if !hasAction(actions, control.ActionDeliverMessage, message) {
		return errors.New("MESSAGE_NOT_DELIVERABLE_AFTER_RESTART")
	}
	return nil
}

func checkDuplicateLineage(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	message, err := invokeAndFind(ctx, runtime, plan.Source, plan.MessageInput, control.ItemMessage)
	if err != nil {
		return err
	}
	original := itemOf(runtime.Snapshot(), message)
	if original == nil || original.Value.Message == nil {
		return errors.New("ORIGINAL_MESSAGE_MISSING")
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	duplicate, ok := actionByItem(actions, control.ActionDuplicateMessage, message)
	if !ok {
		return errors.New("DUPLICATE_ACTION_MISSING")
	}
	if _, err := runtime.Select(ctx, duplicate.ID); err != nil {
		return err
	}
	after := runtime.Snapshot()
	unchanged := itemOf(after, message)
	if unchanged == nil || unchanged.Value.Message == nil ||
		unchanged.Value.Message.ID != original.Value.Message.ID || unchanged.Value.Message.CloneOf != "" {
		return errors.New("DUPLICATE_MUTATED_ORIGINAL")
	}
	for _, item := range after.Items {
		if item.ID != message && item.Value.Message != nil &&
			item.Value.Message.CloneOf == original.Value.Message.ID && item.State == control.ItemEnabled {
			return nil
		}
	}
	return errors.New("DUPLICATE_LINEAGE_MISSING")
}

func checkPartitionRetention(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	message, err := invokeAndFind(ctx, runtime, plan.Source, plan.MessageInput, control.ItemMessage)
	if err != nil {
		return err
	}
	partitionID, err := runtime.OfferPartition([]control.NodeID{plan.Source}, []control.NodeID{plan.Target})
	if err != nil {
		return err
	}
	if _, err := runtime.Select(ctx, partitionID); err != nil {
		return err
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	if stateOf(runtime.Snapshot(), message) != control.ItemEnabled || hasAction(actions, control.ActionDeliverMessage, message) {
		return errors.New("PARTITION_MESSAGE_STATE_INVALID")
	}
	heals := actionsByKind(actions, control.ActionHeal)
	if len(heals) != 1 {
		return errors.New("HEAL_ACTION_MISSING")
	}
	if _, err := runtime.Select(ctx, heals[0].ID); err != nil {
		return err
	}
	actions, err = runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	if !hasAction(actions, control.ActionDeliverMessage, message) {
		return errors.New("DELIVERY_NOT_RESTORED_AFTER_HEAL")
	}
	return nil
}

func checkEffectRelease(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	before := itemIDs(runtime.Snapshot())
	invokeID, err := runtime.OfferInvoke(ctx, plan.Source, plan.DurableMessageInput)
	if err != nil {
		return err
	}
	if _, err := runtime.Select(ctx, invokeID); err != nil {
		return err
	}
	message, effect := addedKinds(runtime.Snapshot(), before)
	if message == "" || effect == "" || stateOf(runtime.Snapshot(), message) != control.ItemBlocked {
		return errors.New("DEPENDENT_MESSAGE_NOT_BLOCKED")
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	complete, ok := actionByItem(actions, control.ActionCompleteEffect, effect)
	if !ok {
		return errors.New("COMPLETE_EFFECT_ACTION_MISSING")
	}
	if _, err := runtime.Select(ctx, complete.ID); err != nil {
		return err
	}
	if stateOf(runtime.Snapshot(), message) != control.ItemEnabled {
		return errors.New("DEPENDENT_MESSAGE_NOT_RELEASED")
	}
	return nil
}

func checkCrashCancelsCaptured(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	before := itemIDs(runtime.Snapshot())
	invokeID, err := runtime.OfferInvoke(ctx, plan.Source, plan.DurableMessageInput)
	if err != nil {
		return err
	}
	if _, err := runtime.Select(ctx, invokeID); err != nil {
		return err
	}
	message, effect := addedKinds(runtime.Snapshot(), before)
	crash, err := findNodeAction(ctx, runtime, control.ActionCrash, plan.Source)
	if err != nil {
		return err
	}
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		return err
	}
	if stateOf(runtime.Snapshot(), message) != control.ItemCanceled || stateOf(runtime.Snapshot(), effect) != control.ItemCanceled {
		return errors.New("CRASH_DID_NOT_CANCEL_VOLATILE_ITEMS")
	}
	return nil
}

func checkFreshReplay(ctx context.Context, factory Factory, plan WitnessPlan) error {
	runtime, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer runtime.Close()
	message, err := invokeAndFind(ctx, runtime, plan.Source, plan.MessageInput, control.ItemMessage)
	if err != nil {
		return err
	}
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return err
	}
	drop, ok := actionByItem(actions, control.ActionDropMessage, message)
	if !ok {
		return errors.New("DROP_ACTION_MISSING")
	}
	if _, err := runtime.Select(ctx, drop.ID); err != nil {
		return err
	}
	trace, err := runtime.Trace()
	if err != nil {
		return err
	}
	err = replayAndClose(ctx, factory(), runtimeConfig(plan), trace)
	return err
}

func checkEntropyStable(ctx context.Context, factory Factory, plan WitnessPlan) error {
	left, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer left.Close()
	right, err := newRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer right.Close()
	leftTrace, err := left.Trace()
	if err != nil {
		return err
	}
	rightTrace, err := right.Trace()
	if err != nil {
		return err
	}
	if leftTrace.InitialEntropyDigest != rightTrace.InitialEntropyDigest ||
		leftTrace.InitialEntropy.DrawCount == 0 {
		return errors.New("ENTROPY_AUDIT_UNSTABLE")
	}
	var tape controlentropy.Tape
	if err := json.Unmarshal(leftTrace.InitialEntropy.Tape.Bytes, &tape); err != nil {
		return fmt.Errorf("ENTROPY_TAPE_DECODE_FAILED: %w", err)
	}
	if err := tape.Validate(); err != nil {
		return err
	}
	seen := make(map[control.NodeID]bool, len(plan.ExpectedInitialDeadlinePeers))
	for _, draw := range tape.Draws {
		if draw.Domain.Node != "" && draw.Domain.Incarnation == 1 {
			seen[draw.Domain.Node] = true
		}
	}
	for _, node := range plan.ExpectedInitialDeadlinePeers {
		if !seen[node] {
			return fmt.Errorf("ENTROPY_NODE_DOMAIN_MISSING: %s", node)
		}
	}
	return nil
}

func newRuntime(ctx context.Context, factory Factory, plan WitnessPlan) (*controlruntime.Runtime, error) {
	return controlruntime.New(ctx, factory(), runtimeConfig(plan))
}

func runtimeConfig(plan WitnessPlan) controlruntime.Config {
	return controlruntime.Config{Seed: append([]byte(nil), plan.Seed...), ClockError: 0, MaxClones: 2}
}

func invokeAndFind(ctx context.Context, runtime *controlruntime.Runtime, node control.NodeID, input control.PayloadEnvelope, kind control.ItemKind) (control.ItemID, error) {
	before := itemIDs(runtime.Snapshot())
	id, err := runtime.OfferInvoke(ctx, node, input)
	if err != nil {
		return "", err
	}
	if _, err := runtime.Select(ctx, id); err != nil {
		return "", err
	}
	for _, item := range runtime.Snapshot().Items {
		if _, ok := before[item.ID]; !ok && item.Kind == kind {
			return item.ID, nil
		}
	}
	return "", fmt.Errorf("CONFORMANCE_ITEM_MISSING: %s", kind)
}

func findNodeAction(ctx context.Context, runtime *controlruntime.Runtime, kind control.ActionKind, node control.NodeID) (control.Action, error) {
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return control.Action{}, err
	}
	for _, action := range actions {
		if action.Kind == kind && action.Node.Node == node {
			return action, nil
		}
	}
	return control.Action{}, fmt.Errorf("CONFORMANCE_NODE_ACTION_MISSING: %s/%s", kind, node)
}

func actionsByKind(actions []control.Action, kind control.ActionKind) []control.Action {
	var result []control.Action
	for _, action := range actions {
		if action.Kind == kind {
			result = append(result, action)
		}
	}
	return result
}

func actionByNode(actions []control.Action, node control.NodeID) (control.Action, bool) {
	for _, action := range actions {
		if action.Node.Node == node {
			return action, true
		}
	}
	return control.Action{}, false
}

func actionByItem(actions []control.Action, kind control.ActionKind, item control.ItemID) (control.Action, bool) {
	for _, action := range actions {
		if action.Kind == kind && action.Item == item {
			return action, true
		}
	}
	return control.Action{}, false
}

func hasAction(actions []control.Action, kind control.ActionKind, item control.ItemID) bool {
	_, ok := actionByItem(actions, kind, item)
	return ok
}

func itemIDs(snapshot controlruntime.Snapshot) map[control.ItemID]struct{} {
	result := make(map[control.ItemID]struct{}, len(snapshot.Items))
	for _, item := range snapshot.Items {
		result[item.ID] = struct{}{}
	}
	return result
}

func stateOf(snapshot controlruntime.Snapshot, id control.ItemID) control.ItemState {
	for _, item := range snapshot.Items {
		if item.ID == id {
			return item.State
		}
	}
	return ""
}

func itemOf(snapshot controlruntime.Snapshot, id control.ItemID) *controlruntime.ItemSnapshot {
	for index := range snapshot.Items {
		if snapshot.Items[index].ID == id {
			return &snapshot.Items[index]
		}
	}
	return nil
}

func temporalKindOf(snapshot controlruntime.Snapshot, id control.ItemID) control.TemporalKind {
	for _, item := range snapshot.Items {
		if item.ID == id && item.Value.Temporal != nil {
			return item.Value.Temporal.Kind
		}
	}
	return ""
}

func addedKinds(snapshot controlruntime.Snapshot, before map[control.ItemID]struct{}) (message control.ItemID, effect control.ItemID) {
	for _, item := range snapshot.Items {
		if _, ok := before[item.ID]; ok {
			continue
		}
		switch item.Kind {
		case control.ItemMessage:
			message = item.ID
		case control.ItemEffect:
			effect = item.ID
		}
	}
	return message, effect
}

func stableReason(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	for index, character := range text {
		if character == ':' || character == ' ' {
			if index > 0 {
				return text[:index]
			}
		}
	}
	return text
}

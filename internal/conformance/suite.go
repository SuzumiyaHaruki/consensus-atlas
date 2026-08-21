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

func checkCollectIdempotent(ctx context.Context, factory Factory, plan CorePlan) error {
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

func checkEnabledPure(ctx context.Context, factory Factory, plan CorePlan) error {
	runtime, err := newCoreRuntime(ctx, factory, plan)
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

func checkEntropyStable(ctx context.Context, factory Factory, plan CorePlan) error {
	left, err := newCoreRuntime(ctx, factory, plan)
	if err != nil {
		return err
	}
	defer left.Close()
	right, err := newCoreRuntime(ctx, factory, plan)
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
	seen := make(map[control.NodeID]bool, len(plan.ExpectedEntropyNodes))
	for _, draw := range tape.Draws {
		if draw.Domain.Node != "" && draw.Domain.Incarnation == 1 {
			seen[draw.Domain.Node] = true
		}
	}
	for _, node := range plan.ExpectedEntropyNodes {
		if !seen[node] {
			return fmt.Errorf("ENTROPY_NODE_DOMAIN_MISSING: %s", node)
		}
	}
	return nil
}

func newCoreRuntime(ctx context.Context, factory Factory, plan CorePlan) (*controlruntime.Runtime, error) {
	return controlruntime.New(ctx, factory(), controlruntime.Config{
		Seed: append([]byte(nil), plan.Seed...), ClockError: 0, MaxClones: 2,
	})
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

func stateOf(snapshot controlruntime.Snapshot, id control.ItemID) control.ItemState {
	for _, item := range snapshot.Items {
		if item.ID == id {
			return item.State
		}
	}
	return ""
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

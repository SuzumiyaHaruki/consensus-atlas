package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

const m521hExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-effective-execution-m5.21h"

func TestEtcdraftM521hAdaptiveUniformExecutionMatchesFrozenIdentityAndComparison(t *testing.T) {
	const archive = "../../benchmarks/experiments/etcdraft-v2-planner-gate-m5.21f/campaign.tar.gz"
	agent := readArchivedCampaignPlan(
		t, archive, "campaign/plans/00000000000000000002.json",
	)
	spec, err := newEtcdraftCampaignSpec("etcdraft-effective-execution-m5-21h", 32, 171)
	if err != nil {
		t.Fatal(err)
	}
	base, err := newEtcdraftCampaignProvider(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	planner := newEtcdraftPlannedCampaignProvider(base, nil)
	adaptiveProposal, err := controlexperiment.PlanDeterministicBalancedCampaignBaseline(agent.View)
	if err != nil {
		t.Fatal(err)
	}
	adaptive, err := planner.buildPlannedAttempt(
		agent.View, adaptiveProposal, controlexperiment.ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	actionEffective, err := base.effectiveExecution(agent)
	if err != nil {
		t.Fatal(err)
	}
	uniformEffective, err := base.effectiveExecution(adaptive)
	if err != nil {
		t.Fatal(err)
	}
	if actionEffective.Digest != "e240b2cdaec26372435015adb42aa5b3001bc917d04b270a6603cf72e6e1c52b" ||
		uniformEffective.Digest != "089d9fbbfc7d8cbecee15d9b66b6a706378ccc6730f9130a8774ca6357f1b3d9" {
		t.Fatalf("frozen effective identities drifted: action=%s uniform=%s",
			actionEffective.Digest, uniformEffective.Digest)
	}

	actionArtifact, err := decodeEtcdraftCampaignArtifact(readArchivedCampaignMember(
		t, archive,
		"campaign/artifacts/6e496ffe7d354b6b181306fc02444aa20344a7c2bda21b3eb1e32d158a73cdc3.artifact",
		2<<20,
	))
	if err != nil || actionArtifact.Report == nil || actionArtifact.Bundle == nil {
		t.Fatalf("archived action-class evidence is invalid: %v", err)
	}
	uniformReport := readM521hGzipJSON[controlexperiment.Report](
		t, m521hExperimentDirectory+"/uniform-report.json.gz", 1<<20,
	)
	uniformBundle := readM521hGzipJSON[controlexperiment.ExecutionBundle](
		t, m521hExperimentDirectory+"/uniform-bundle.json.gz", 2<<20,
	)
	actionReport, actionBundle := *actionArtifact.Report, *actionArtifact.Bundle
	for name, pair := range map[string]struct {
		report controlexperiment.Report
		bundle controlexperiment.ExecutionBundle
	}{
		"action-class": {actionReport, actionBundle},
		"uniform":      {uniformReport, uniformBundle},
	} {
		if err := pair.report.Validate(); err != nil {
			t.Fatalf("%s report invalid: %v", name, err)
		}
		if err := pair.bundle.Validate(); err != nil {
			t.Fatalf("%s bundle invalid: %v", name, err)
		}
		if err := pair.bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
			t.Fatalf("%s projection invalid: %v", name, err)
		}
	}
	assertM521hExecutionBinding(t, actionReport, actionEffective)
	assertM521hExecutionBinding(t, uniformReport, uniformEffective)
	if actionReport.Config.Runtime != uniformReport.Config.Runtime ||
		actionReport.Config.DecisionsPerRun != uniformReport.Config.DecisionsPerRun ||
		actionReport.Config.FaultEnvelope == nil || uniformReport.Config.FaultEnvelope == nil ||
		*actionReport.Config.FaultEnvelope != *uniformReport.Config.FaultEnvelope ||
		actionReport.Work.Primary != uniformReport.Work.Primary ||
		actionReport.Work.Replay != uniformReport.Work.Replay {
		t.Fatal("methods were not executed under the same non-policy boundary")
	}
	actionWorkload, err := actionReport.Config.Runs[0].Workload.Digest()
	if err != nil {
		t.Fatal(err)
	}
	uniformWorkload, err := uniformReport.Config.Runs[0].Workload.Digest()
	if err != nil || actionWorkload != uniformWorkload {
		t.Fatalf("workload boundary drifted: action=%s uniform=%s err=%v",
			actionWorkload, uniformWorkload, err)
	}

	intersection, union, actionOnly, uniformOnly := compareM521hStateSets(
		actionReport, uniformReport,
	)
	comparison := m521hComparison{
		SchemaVersion:               "consensus-atlas/effective-execution-comparison/v1",
		SourceCampaignArchiveSHA256: "a0ccca92d064f78cc07f3d0a3edb63d77a9ef1b866b9283ee5cdead965534afd",
		PlannerViewDigest:           agent.View.Digest,
		TargetIdentityDigest:        base.targetIdentityDigest,
		ExecutionEnvironmentDigest:  actionEffective.ExecutionEnvironmentDigest,
		PolicySeed:                  adaptive.Instance.PolicySeed,
		Decisions:                   adaptive.Plan.Decisions,
		MaxPrimaryWorkUnits:         adaptive.Instance.Budget.MaxPrimaryWorkUnits,
		MaxReplayWorkUnits:          adaptive.Instance.Budget.MaxReplayWorkUnits,
		NewModelCalls:               0,
		NewSUTExecutions:            1,
		ActionClass: m521hMethodEvidenceFrom(
			etcdraftBackendActionClass, actionEffective, actionReport, actionBundle,
		),
		AdaptiveUniform: m521hMethodEvidenceFrom(
			etcdraftBackendUniform, uniformEffective, uniformReport, uniformBundle,
		),
		PSSSetComparison: m521hPSSSetComparison{
			Intersection: intersection, Union: union, ActionOnly: actionOnly,
			UniformOnly: uniformOnly, Jaccard: float64(intersection) / float64(union),
		},
		Classification: "diagnostic-behavior-delta-no-method-advantage-claim",
	}
	checked := readM521hJSONFile[m521hComparison](
		t, m521hExperimentDirectory+"/comparison.json", 1<<20,
	)
	if !reflect.DeepEqual(checked, comparison) {
		t.Fatalf("checked M5.21h comparison drifted: checked=%#v fresh=%#v", checked, comparison)
	}
}

func assertM521hExecutionBinding(
	t *testing.T,
	report controlexperiment.Report,
	effective controlexperiment.CampaignEffectiveExecution,
) {
	t.Helper()
	policyDigest, err := report.Config.Runs[0].Policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if report.ManifestDigest != effective.TargetIdentityDigest ||
		report.Config.DecisionsPerRun != effective.Decisions ||
		report.Config.FaultEnvelope == nil ||
		*report.Config.FaultEnvelope != effective.FaultEnvelope ||
		policyDigest != effective.PolicyDigest || !report.Runs[0].Replay.Stable {
		t.Fatalf("report does not match effective execution %s", effective.Digest)
	}
}

type m521hPhaseWork struct {
	SchedulerDecisions int `json:"scheduler_decisions"`
	WorkUnits          int `json:"work_units"`
}

type m521hPSSMetrics struct {
	UniqueStates       int     `json:"unique_states"`
	PrefixArea         int64   `json:"prefix_area"`
	SelfNormalizedArea float64 `json:"self_normalized_area"`
}

type m521hWorkloadOutcome struct {
	Planned   int `json:"planned"`
	Offered   int `json:"offered"`
	Completed int `json:"completed"`
	Pending   int `json:"pending"`
}

type m521hMethodEvidence struct {
	BackendID                string                       `json:"backend_id"`
	EffectiveExecutionDigest string                       `json:"effective_execution_digest"`
	ReportDigest             string                       `json:"report_digest"`
	BundleDigest             string                       `json:"bundle_digest"`
	PolicyVersion            string                       `json:"policy_version"`
	Primary                  m521hPhaseWork               `json:"primary"`
	Replay                   m521hPhaseWork               `json:"replay"`
	ReplayStable             bool                         `json:"replay_stable"`
	PSS                      m521hPSSMetrics              `json:"pss"`
	Faults                   controlexperiment.FaultUsage `json:"faults"`
	Workload                 m521hWorkloadOutcome         `json:"workload"`
	CheckedMonitors          []string                     `json:"checked_monitors"`
	MonitorViolations        int                          `json:"monitor_violations"`
}

type m521hPSSSetComparison struct {
	Intersection int     `json:"intersection"`
	Union        int     `json:"union"`
	ActionOnly   int     `json:"action_only"`
	UniformOnly  int     `json:"uniform_only"`
	Jaccard      float64 `json:"jaccard"`
}

type m521hComparison struct {
	SchemaVersion               string                `json:"schema_version"`
	SourceCampaignArchiveSHA256 string                `json:"source_campaign_archive_sha256"`
	PlannerViewDigest           string                `json:"planner_view_digest"`
	TargetIdentityDigest        string                `json:"target_identity_digest"`
	ExecutionEnvironmentDigest  string                `json:"execution_environment_digest"`
	PolicySeed                  uint64                `json:"policy_seed"`
	Decisions                   int                   `json:"decisions"`
	MaxPrimaryWorkUnits         int                   `json:"max_primary_work_units"`
	MaxReplayWorkUnits          int                   `json:"max_replay_work_units"`
	NewModelCalls               int                   `json:"new_model_calls"`
	NewSUTExecutions            int                   `json:"new_sut_executions"`
	ActionClass                 m521hMethodEvidence   `json:"action_class"`
	AdaptiveUniform             m521hMethodEvidence   `json:"adaptive_uniform"`
	PSSSetComparison            m521hPSSSetComparison `json:"pss_set_comparison"`
	Classification              string                `json:"classification"`
}

func m521hMethodEvidenceFrom(
	backendID string,
	effective controlexperiment.CampaignEffectiveExecution,
	report controlexperiment.Report,
	bundle controlexperiment.ExecutionBundle,
) m521hMethodEvidence {
	checked := oracle.CheckBundle(bundle, oracle.BundleAgreement{}, oracle.BundleTraceIntegrity{})
	workload := report.Runs[0].Workload
	return m521hMethodEvidence{
		BackendID: backendID, EffectiveExecutionDigest: effective.Digest,
		ReportDigest: report.Digest, BundleDigest: bundle.Digest,
		PolicyVersion: report.Config.Runs[0].Policy.Version,
		Primary:       m521hPhaseWork{report.Work.Primary.SchedulerDecisions, report.Work.Primary.WorkUnits},
		Replay:        m521hPhaseWork{report.Work.Replay.SchedulerDecisions, report.Work.Replay.WorkUnits},
		ReplayStable:  report.Runs[0].Replay.Stable,
		PSS: m521hPSSMetrics{
			UniqueStates:       report.StateDiscovery.UniqueStates,
			PrefixArea:         report.StateDiscovery.PrefixArea,
			SelfNormalizedArea: report.StateDiscovery.SelfNormalizedArea,
		},
		Faults: *report.Runs[0].Faults,
		Workload: m521hWorkloadOutcome{
			Planned: workload.Planned, Offered: workload.Offered,
			Completed: workload.Completed, Pending: workload.Pending,
		},
		CheckedMonitors: checked.Checked, MonitorViolations: len(checked.Violations),
	}
}

func compareM521hStateSets(
	action controlexperiment.Report,
	uniform controlexperiment.Report,
) (intersection int, union int, actionOnly int, uniformOnly int) {
	actionKeys := make(map[string]bool, len(action.StateDiscovery.States))
	unionKeys := make(map[string]bool, len(action.StateDiscovery.States)+len(uniform.StateDiscovery.States))
	for _, state := range action.StateDiscovery.States {
		actionKeys[state.Key] = true
		unionKeys[state.Key] = true
	}
	for _, state := range uniform.StateDiscovery.States {
		unionKeys[state.Key] = true
		if actionKeys[state.Key] {
			intersection++
		} else {
			uniformOnly++
		}
	}
	union = len(unionKeys)
	actionOnly = len(actionKeys) - intersection
	return intersection, union, actionOnly, uniformOnly
}

func readM521hGzipJSON[T any](t *testing.T, path string, limit int64) T {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(data)) > limit {
		t.Fatalf("read %s: size=%d err=%v", path, len(data), err)
	}
	return decodeM521hJSON[T](t, path, data)
}

func readM521hJSONFile[T any](t *testing.T, path string, limit int64) T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) > limit {
		t.Fatalf("read %s: size=%d err=%v", path, len(data), err)
	}
	return decodeM521hJSON[T](t, path, data)
}

func decodeM521hJSON[T any](t *testing.T, path string, data []byte) T {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("%s has trailing JSON", path)
	}
	return value
}

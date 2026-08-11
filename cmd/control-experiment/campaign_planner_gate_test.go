package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM521fFrozenViewBaselinesProduceFullIdentityDelta(t *testing.T) {
	historical := readArchivedCampaignPlan(
		t,
		"../../benchmarks/experiments/etcdraft-v2-agent-vs-zero-m5.21e/zero/campaign.tar.gz",
		"campaign/plans/00000000000000000002.json",
	)
	if historical.View.Digest != "7ddce1feb9b90fd947899db19ee4a5bcf87cb94602c70a96fcd860521eeb784b" {
		t.Fatalf("frozen planner view changed: %s", historical.View.Digest)
	}
	spec, err := newEtcdraftCampaignSpec("etcdraft-planner-gate-m5-21f", 32, 161)
	if err != nil {
		t.Fatal(err)
	}
	base, err := newEtcdraftCampaignProvider(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	provider := newEtcdraftPlannedCampaignProvider(base, nil)
	zeroProposal, err := controlexperiment.PlanDeterministicCampaignFixture(historical.View)
	if err != nil {
		t.Fatal(err)
	}
	adaptiveProposal, err := controlexperiment.PlanDeterministicBalancedCampaignBaseline(historical.View)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := provider.buildPlannedAttempt(
		historical.View, zeroProposal, controlexperiment.ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	adaptive, err := provider.buildPlannedAttempt(
		historical.View, adaptiveProposal, controlexperiment.ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if zero.Choice.BackendID != etcdraftBackendActionClass ||
		adaptive.Choice.BackendID != etcdraftBackendUniform ||
		zero.Instance.PolicySeed != adaptive.Instance.PolicySeed ||
		zero.Instance.Digest == adaptive.Instance.Digest || zero.Plan.Digest == adaptive.Plan.Digest {
		t.Fatalf("expected same-view full-identity delta: zero=%#v adaptive=%#v", zero.Choice, adaptive.Choice)
	}
	if zero.Choice.Digest != historical.Choice.Digest || zero.Instance.Digest != historical.Instance.Digest ||
		zero.Plan.Digest != historical.Plan.Digest {
		t.Fatalf("zero-model no longer reproduces the frozen M5.21e choice: got=%#v historical=%#v",
			zero.Choice, historical.Choice)
	}
	t.Logf("view=%s zero_plan=%s zero_instance=%s adaptive_plan=%s adaptive_instance=%s",
		historical.View.Digest, zero.Plan.Digest, zero.Instance.Digest,
		adaptive.Plan.Digest, adaptive.Instance.Digest)
}

func TestEtcdraftM521hB4StrategiesCanPersistQualifiedBundles(t *testing.T) {
	for _, strategy := range []string{
		"workload-action-class-random-b4",
		"workload-admissible-uniform-b4",
	} {
		if !supportsQualifiedBundleOutput(strategy) {
			t.Fatalf("B4 strategy %q cannot persist its trusted execution bundle", strategy)
		}
	}
	if supportsQualifiedBundleOutput("workload-action-class-random-v2") ||
		supportsQualifiedBundleOutput(etcdraftCampaignRunnerStrategy) {
		t.Fatal("bundle output allowlist widened beyond directly qualified strategies")
	}
}

func TestEtcdraftM521fLiveAgentMatchesZeroButNotAdaptiveBehavior(t *testing.T) {
	agent := readArchivedCampaignPlan(
		t,
		"../../benchmarks/experiments/etcdraft-v2-planner-gate-m5.21f/campaign.tar.gz",
		"campaign/plans/00000000000000000002.json",
	)
	if agent.View.Digest != "7a130d516a8f2d15e7742aeedb0dbb17bf9bf61efa5e6f343b3ade3d97276a5f" {
		t.Fatalf("live planner view changed: %s", agent.View.Digest)
	}
	spec, err := newEtcdraftCampaignSpec("etcdraft-live-planner-gate-m5-21f", 32, 171)
	if err != nil {
		t.Fatal(err)
	}
	base, err := newEtcdraftCampaignProvider(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	provider := newEtcdraftPlannedCampaignProvider(base, nil)
	zeroProposal, err := controlexperiment.PlanDeterministicCampaignFixture(agent.View)
	if err != nil {
		t.Fatal(err)
	}
	adaptiveProposal, err := controlexperiment.PlanDeterministicBalancedCampaignBaseline(agent.View)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := provider.buildPlannedAttempt(agent.View, zeroProposal, controlexperiment.ModelWork{})
	if err != nil {
		t.Fatal(err)
	}
	adaptive, err := provider.buildPlannedAttempt(agent.View, adaptiveProposal, controlexperiment.ModelWork{})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Choice.BackendID != etcdraftBackendActionClass ||
		zero.Choice.BackendID != etcdraftBackendActionClass ||
		adaptive.Choice.BackendID != etcdraftBackendUniform {
		t.Fatalf("unexpected live planner choices: agent=%s zero=%s adaptive=%s",
			agent.Choice.BackendID, zero.Choice.BackendID, adaptive.Choice.BackendID)
	}
	if len(agent.Proposal.Prefer.Actions) == 0 || len(zero.Proposal.Prefer.Actions) != 0 ||
		agent.Plan.Digest == zero.Plan.Digest || agent.Instance.Digest == zero.Instance.Digest {
		t.Fatalf("inert preference metadata did not remain audit-visible: agent=%#v zero=%#v",
			agent.Proposal.Prefer, zero.Proposal.Prefer)
	}
	if agent.Plan.BackendID != zero.Plan.BackendID || agent.Plan.Strategy != zero.Plan.Strategy ||
		agent.Plan.Decisions != zero.Plan.Decisions || agent.Plan.FaultEnvelope != zero.Plan.FaultEnvelope ||
		agent.Instance.PolicySeed != zero.Instance.PolicySeed || agent.Instance.Budget != zero.Instance.Budget {
		t.Fatalf("Agent and zero-model do not have the same effective execution inputs: agent=%#v zero=%#v",
			agent.Choice, zero.Choice)
	}
}

func TestEtcdraftM521gThreeMethodGateUsesEffectiveExecutionIdentity(t *testing.T) {
	const archive = "../../benchmarks/experiments/etcdraft-v2-planner-gate-m5.21f/campaign.tar.gz"
	artifactMembers := []string{
		"campaign/artifacts/4b8fb115689d2dee079e813780b640aa2d0b357af3e471b7385b1b078d7d8472.artifact",
		"campaign/artifacts/6e496ffe7d354b6b181306fc02444aa20344a7c2bda21b3eb1e32d158a73cdc3.artifact",
	}
	agentPlans := []controlexperiment.CampaignPlannedAttempt{
		readArchivedCampaignPlan(t, archive, "campaign/plans/00000000000000000001.json"),
		readArchivedCampaignPlan(t, archive, "campaign/plans/00000000000000000002.json"),
	}
	spec, err := newEtcdraftCampaignSpec("etcdraft-three-method-gate-m5-21g", 32, 171)
	if err != nil {
		t.Fatal(err)
	}
	base, err := newEtcdraftCampaignProvider(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	provider := newEtcdraftPlannedCampaignProvider(base, nil)
	comparison := plannerGateCheckedArtifact{
		SchemaVersion:               "consensus-atlas/planner-usefulness-gate/v1",
		SourceCampaignArchiveSHA256: "a0ccca92d064f78cc07f3d0a3edb63d77a9ef1b866b9283ee5cdead965534afd",
		SourceModelCalls:            2, SourceModelTokens: 6027,
	}
	for index, agent := range agentPlans {
		zeroProposal, err := controlexperiment.PlanDeterministicCampaignFixture(agent.View)
		if err != nil {
			t.Fatal(err)
		}
		adaptiveProposal, err := controlexperiment.PlanDeterministicBalancedCampaignBaseline(agent.View)
		if err != nil {
			t.Fatal(err)
		}
		zero, err := provider.buildPlannedAttempt(agent.View, zeroProposal, controlexperiment.ModelWork{})
		if err != nil {
			t.Fatal(err)
		}
		adaptive, err := provider.buildPlannedAttempt(agent.View, adaptiveProposal, controlexperiment.ModelWork{})
		if err != nil {
			t.Fatal(err)
		}
		agentEffective, err := base.effectiveExecution(agent)
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := decodeEtcdraftCampaignArtifact(
			readArchivedCampaignMember(t, archive, artifactMembers[index], 2<<20),
		)
		if err != nil || artifact.Report == nil || len(artifact.Report.Config.Runs) != 1 ||
			artifact.Report.Config.Runs[0].Workload == nil {
			t.Fatalf("attempt %d archived execution is invalid: %#v/%v", index+1, artifact, err)
		}
		observedPolicyDigest, err := artifact.Report.Config.Runs[0].Policy.Digest()
		if err != nil {
			t.Fatal(err)
		}
		observedWorkloadDigest, err := artifact.Report.Config.Runs[0].Workload.Digest()
		if err != nil {
			t.Fatal(err)
		}
		expectedWorkload, err := etcdraftCampaignWorkload()
		if err != nil {
			t.Fatal(err)
		}
		expectedWorkloadDigest, err := expectedWorkload.Digest()
		if err != nil {
			t.Fatal(err)
		}
		if observedPolicyDigest != agentEffective.PolicyDigest ||
			observedWorkloadDigest != expectedWorkloadDigest ||
			artifact.Report.ManifestDigest != agentEffective.TargetIdentityDigest ||
			artifact.Report.Config.Runtime != etcdraftCampaignRuntimeConfig() ||
			artifact.Report.Config.DecisionsPerRun != agentEffective.Decisions ||
			artifact.Report.Config.FaultEnvelope == nil ||
			*artifact.Report.Config.FaultEnvelope != agentEffective.FaultEnvelope {
			t.Fatalf("attempt %d effective identity does not match observed executor inputs", index+1)
		}
		zeroEffective, err := base.effectiveExecution(zero)
		if err != nil {
			t.Fatal(err)
		}
		adaptiveEffective, err := base.effectiveExecution(adaptive)
		if err != nil {
			t.Fatal(err)
		}
		if agentEffective.Digest != zeroEffective.Digest {
			t.Fatalf("attempt %d gave Agent and zero-model different effective identities: %s/%s",
				index+1, agentEffective.Digest, zeroEffective.Digest)
		}
		if index == 0 && adaptiveEffective.Digest != agentEffective.Digest {
			t.Fatalf("no-feedback methods should share one execution: agent=%s adaptive=%s",
				agentEffective.Digest, adaptiveEffective.Digest)
		}
		if index == 1 && adaptiveEffective.Digest == agentEffective.Digest {
			t.Fatal("feedback view did not produce an adaptive effective-execution delta")
		}
		unique := 1
		if adaptiveEffective.Digest != agentEffective.Digest {
			unique++
		}
		comparison.Attempts = append(comparison.Attempts, plannerGateCheckedAttempt{
			Ordinal: index + 1, PlannerViewDigest: agent.View.Digest,
			Agent:                     plannerGateCheckedMethod{BackendID: agent.Choice.BackendID, EffectiveExecutionDigest: agentEffective.Digest},
			ZeroModel:                 plannerGateCheckedMethod{BackendID: zero.Choice.BackendID, EffectiveExecutionDigest: zeroEffective.Digest},
			Adaptive:                  plannerGateCheckedMethod{BackendID: adaptive.Choice.BackendID, EffectiveExecutionDigest: adaptiveEffective.Digest},
			UniqueEffectiveExecutions: unique,
		})
		t.Logf("attempt=%d agent=%s zero=%s adaptive=%s", index+1,
			agentEffective.Digest, zeroEffective.Digest, adaptiveEffective.Digest)
	}
	checked := readPlannerGateCheckedArtifact(t)
	if !reflect.DeepEqual(checked, comparison) {
		t.Fatalf("checked comparison drifted: checked=%#v fresh=%#v", checked, comparison)
	}
}

type plannerGateCheckedMethod struct {
	BackendID                string `json:"backend_id"`
	EffectiveExecutionDigest string `json:"effective_execution_digest"`
}

type plannerGateCheckedAttempt struct {
	Ordinal                   int                      `json:"ordinal"`
	PlannerViewDigest         string                   `json:"planner_view_digest"`
	Agent                     plannerGateCheckedMethod `json:"agent"`
	ZeroModel                 plannerGateCheckedMethod `json:"zero_model"`
	Adaptive                  plannerGateCheckedMethod `json:"adaptive"`
	UniqueEffectiveExecutions int                      `json:"unique_effective_executions"`
}

type plannerGateCheckedArtifact struct {
	SchemaVersion               string                      `json:"schema_version"`
	SourceCampaignArchiveSHA256 string                      `json:"source_campaign_archive_sha256"`
	SourceModelCalls            int                         `json:"source_model_calls"`
	SourceModelTokens           int                         `json:"source_model_tokens"`
	NewModelCalls               int                         `json:"new_model_calls"`
	NewSUTExecutions            int                         `json:"new_sut_executions"`
	Attempts                    []plannerGateCheckedAttempt `json:"attempts"`
}

func readPlannerGateCheckedArtifact(t *testing.T) plannerGateCheckedArtifact {
	t.Helper()
	file, err := os.Open("../../benchmarks/experiments/etcdraft-v2-three-method-gate-m5.21g/comparison.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var artifact plannerGateCheckedArtifact
	if err := decoder.Decode(&artifact); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatal("comparison artifact has trailing JSON")
	}
	return artifact
}

func readArchivedCampaignPlan(
	t *testing.T,
	path string,
	member string,
) controlexperiment.CampaignPlannedAttempt {
	t.Helper()
	data := readArchivedCampaignMember(t, path, member, 1<<20)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var planned controlexperiment.CampaignPlannedAttempt
	if err := decoder.Decode(&planned); err != nil {
		t.Fatal(err)
	}
	if err := planned.Validate(); err != nil {
		t.Fatal(err)
	}
	return planned
}

func readArchivedCampaignMember(
	t *testing.T,
	path string,
	member string,
	limit int64,
) []byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	for {
		header, nextErr := archive.Next()
		if nextErr == io.EOF {
			t.Fatalf("%s is missing", member)
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		if header.Name != member {
			continue
		}
		if header.Size <= 0 || header.Size > limit {
			t.Fatalf("unexpected frozen member size: %d", header.Size)
		}
		data, err := io.ReadAll(io.LimitReader(archive, header.Size))
		if err != nil || int64(len(data)) != header.Size {
			t.Fatal(err)
		}
		return data
	}
}

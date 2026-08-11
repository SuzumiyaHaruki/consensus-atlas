package controlexperiment

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

func TestCampaignObservationAggregatesBoundEvidenceWithoutACompositeScore(t *testing.T) {
	summary := campaignObservationSummary(t)
	initial := campaignObservationState(t, psscore.ModePassive)
	changed := campaignObservationState(t, psscore.ModeCoordinating)
	projections := []CampaignAttemptProjection{
		campaignObservationProjection(summary.Attempts[0], initial, changed),
		campaignObservationProjection(summary.Attempts[1], initial, initial),
	}
	projections[0].Faults = &FaultUsage{Crashes: 1, MessageDrops: 2}
	projections[1].Faults = &FaultUsage{Partitions: 1}
	projections[0].Workload = &WorkloadRunReport{
		Planned: 2, Offered: 2, Completed: 1, Pending: 1,
		Results: []WorkloadResult{{Status: "committed"}},
	}
	projections[0].MonitorFindings = []CampaignMonitorFinding{{
		Monitor: "agreement", Step: 1, Class: CampaignMonitorRequirement,
		Message: "fixture trigger",
	}}

	observation, err := NewCampaignObservation(summary, projections)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Terminal.Status != CampaignSummaryStatusStopped ||
		observation.Terminal.Completed != 2 || observation.Terminal.Work != summary.Totals ||
		observation.PSS == nil || observation.PSS.EvidenceAttempts != 2 ||
		observation.PSS.TotalDecisions != 2 || observation.PSS.TotalSamples != 4 ||
		observation.PSS.UniqueStates != 2 || len(observation.PSS.Curve) != 2 ||
		!observation.PSS.Curve[0].NewState || observation.PSS.Curve[1].NewState ||
		observation.Attempts[0].NewPSSStates != 2 || observation.Attempts[1].NewPSSStates != 0 {
		t.Fatalf("PSS/terminal aggregation drifted: %#v", observation)
	}
	if observation.Faults.ObservedAttempts != 2 ||
		observation.Faults.Usage != (FaultUsage{Crashes: 1, MessageDrops: 2, Partitions: 1}) ||
		observation.Workload.ObservedAttempts != 1 || observation.Workload.Planned != 2 ||
		observation.Workload.Completed != 1 || observation.Workload.Pending != 1 ||
		len(observation.Workload.ResultStatuses) != 1 ||
		observation.Workload.ResultStatuses[0] != (CampaignStatusCount{Status: "committed", Count: 1}) {
		t.Fatalf("fault/workload aggregation drifted: %#v/%#v", observation.Faults, observation.Workload)
	}
	if observation.Monitors.ObservedAttempts != 2 || len(observation.Monitors.Checked) != 2 ||
		len(observation.Monitors.Triggers) != 1 ||
		observation.Monitors.Triggers[0].ArtifactDigest != summary.Attempts[0].Record.ArtifactDigest ||
		observation.Monitors.Triggers[0].Class != CampaignMonitorRequirement {
		t.Fatalf("monitor index drifted: %#v", observation.Monitors)
	}

	encoded, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "trace\"") || strings.Contains(string(encoded), "final_snapshot") {
		t.Fatal("compact observation copied execution-bundle payload")
	}
	var decoded CampaignObservation
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil || decoded.Digest != observation.Digest {
		t.Fatalf("round trip failed: %v", err)
	}

	tampered := observation
	tampered.PSS = cloneCampaignPSSObservationForTest(observation.PSS)
	tampered.PSS.Curve[0].NewState = false
	if err := tampered.Validate(); err == nil {
		t.Fatalf("tampered observation remained valid: %v", err)
	}
	wrong := append([]CampaignAttemptProjection(nil), projections...)
	wrong[0].ArtifactDigest = strings.Repeat("f", 64)
	if _, err := NewCampaignObservation(summary, wrong); err == nil ||
		!strings.Contains(err.Error(), "PROJECTION_BINDING_MISMATCH") {
		t.Fatalf("unbound projection was accepted: %v", err)
	}
}

func TestCampaignObservationKeepsArtifactlessFailureSeparateFromAttempts(t *testing.T) {
	config := campaignStoreTestConfig(t, "observation-failure")
	recovered, err := CreateCampaignDirectory(t.TempDir()+"/campaign", config)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewCampaignAttemptRequest(config, recovered.Head)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.FailAttempt(request, CampaignFailureProvider); err != nil {
		t.Fatal(err)
	}
	summary, err := NewCampaignSummary(&recovered)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := NewCampaignObservation(summary, nil)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Terminal.Status != CampaignSummaryStatusFailed ||
		observation.Terminal.Attempts != 0 || observation.Terminal.FailureDigest == "" ||
		observation.PSS != nil || observation.Faults.ObservedAttempts != 0 ||
		observation.Workload.ObservedAttempts != 0 || observation.Monitors.ObservedAttempts != 0 {
		t.Fatalf("artifact-less failure became execution evidence: %#v", observation)
	}
}

func campaignObservationSummary(t *testing.T) CampaignSummary {
	t.Helper()
	config := campaignTestConfig(t, "observation", strings.Repeat("a", 64), CampaignLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: 2,
		MaxPrimaryWorkUnits: 4, MaxReplayWorkUnits: 4,
	}, 1_000)
	recovered, err := CreateCampaignDirectory(t.TempDir()+"/campaign", config)
	if err != nil {
		t.Fatal(err)
	}
	for ordinal := 1; ordinal <= 2; ordinal++ {
		artifact := []byte("observation-artifact-" + string(rune('0'+ordinal)))
		record, err := NewCampaignAttemptRecord(CampaignAttemptRecord{
			Ordinal: ordinal, ID: "observation-attempt-" + string(rune('0'+ordinal)),
			InputDigest: strings.Repeat("d", 64), ArtifactDigest: CampaignArtifactDigest(artifact),
			Outcome: CampaignAttemptCompleted, Work: campaignTestWork(1, 1, 0, 0),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := recovered.CommitAttempt(record, artifact, int64(ordinal)); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := NewCampaignSummary(&recovered)
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func campaignObservationProjection(
	attempt CampaignAttemptSummary,
	initial psscore.State,
	final psscore.State,
) CampaignAttemptProjection {
	return CampaignAttemptProjection{
		Ordinal: attempt.Ordinal, ArtifactDigest: attempt.Record.ArtifactDigest,
		Outcome: attempt.Record.Outcome, Work: attempt.Record.Work,
		ExecutionEvidence: true, BundleDigest: strings.Repeat(string(rune('a'+attempt.Ordinal)), 64),
		PSSID: initial.MappingID,
		CorePSS: []CorePSSSample{
			{Step: 0, Key: initial.Digest, State: initial},
			{Step: 1, Key: final.Digest, State: final},
		},
		CheckedMonitors: []string{"trace-integrity", "agreement"},
	}
}

func campaignObservationState(t *testing.T, mode psscore.ParticipantMode) psscore.State {
	t.Helper()
	snapshot := controlruntime.Snapshot{
		Nodes: []controlruntime.NodeSnapshot{{
			Ref: control.NodeRef{Node: "node", Incarnation: 1}, Lifecycle: control.NodeRunning,
		}},
	}
	state, err := psscore.Project(snapshot, "fixture/core-v1", psscore.SemanticObservation{
		Graph: psscore.SemanticGraph{Entities: []psscore.Entity{{
			ID: "node", Kind: psscore.EntityParticipant, Mode: mode,
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func cloneCampaignPSSObservationForTest(source *CampaignPSSObservation) *CampaignPSSObservation {
	cloned := *source
	cloned.Curve = append([]CampaignPSSPoint(nil), source.Curve...)
	cloned.States = append([]CampaignPSSWitness(nil), source.States...)
	return &cloned
}

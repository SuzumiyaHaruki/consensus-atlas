package main

import (
	"reflect"
	"sort"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

const m521iExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-method-corpus-m5.21i"

func TestEtcdraftM521iAggregatesOrderedMethodCorporaFromEffectiveEvidence(t *testing.T) {
	const archive = "../../benchmarks/experiments/etcdraft-v2-planner-gate-m5.21f/campaign.tar.gz"
	members := []string{
		"campaign/artifacts/4b8fb115689d2dee079e813780b640aa2d0b357af3e471b7385b1b078d7d8472.artifact",
		"campaign/artifacts/6e496ffe7d354b6b181306fc02444aa20344a7c2bda21b3eb1e32d158a73cdc3.artifact",
	}
	actionBundles := make([]controlexperiment.ExecutionBundle, 0, 2)
	for _, member := range members {
		artifact, err := decodeEtcdraftCampaignArtifact(
			readArchivedCampaignMember(t, archive, member, 2<<20),
		)
		if err != nil || artifact.Bundle == nil {
			t.Fatalf("decode %s: %v", member, err)
		}
		actionBundles = append(actionBundles, *artifact.Bundle)
	}
	uniform := readM521hGzipJSON[controlexperiment.ExecutionBundle](
		t, m521hExperimentDirectory+"/uniform-bundle.json.gz", 2<<20,
	)
	gate := readPlannerGateCheckedArtifact(t)
	if len(gate.Attempts) != 2 {
		t.Fatalf("unexpected effective gate attempt count: %d", len(gate.Attempts))
	}
	actionDigests := []string{
		gate.Attempts[0].Agent.EffectiveExecutionDigest,
		gate.Attempts[1].Agent.EffectiveExecutionDigest,
	}
	adaptiveDigests := []string{
		gate.Attempts[0].Adaptive.EffectiveExecutionDigest,
		gate.Attempts[1].Adaptive.EffectiveExecutionDigest,
	}
	actionCorpus, actionStates := newM521iMethodCorpus(
		t, "action-class-action-class", actionDigests, actionBundles,
	)
	adaptiveCorpus, adaptiveStates := newM521iMethodCorpus(
		t, "action-class-uniform", adaptiveDigests,
		[]controlexperiment.ExecutionBundle{actionBundles[0], uniform},
	)
	intersection, union, actionOnly, adaptiveOnly := compareM521iStateSets(
		actionStates, adaptiveStates,
	)
	comparison := m521iComparison{
		SchemaVersion:               "consensus-atlas/method-corpus-comparison/v1",
		SourceCampaignArchiveSHA256: gate.SourceCampaignArchiveSHA256,
		TargetIdentityDigest:        actionBundles[0].Identity.ManifestDigest,
		NewModelCalls:               0,
		NewSUTExecutions:            0,
		PhysicalEvidenceExecutions:  3,
		ActionClassCorpus:           actionCorpus,
		AdaptiveCorpus:              adaptiveCorpus,
		MethodViews: []m521iMethodView{
			{
				ID: "agent", CorpusID: actionCorpus.ID,
				Model: m521iModelWork{Calls: 2, InputTokens: 5515, OutputTokens: 512, TotalTokens: 6027},
			},
			{ID: "zero-model", CorpusID: actionCorpus.ID},
			{ID: "deterministic-adaptive", CorpusID: adaptiveCorpus.ID},
		},
		PSSSetComparison: m521iPSSSetComparison{
			Intersection: intersection, Union: union, ActionClassOnly: actionOnly,
			AdaptiveOnly: adaptiveOnly, Jaccard: float64(intersection) / float64(union),
		},
		Classification: "public-two-attempt-discovery-diagnostic-no-method-advantage-claim",
	}
	checked := readM521hJSONFile[m521iComparison](
		t, m521iExperimentDirectory+"/comparison.json", 1<<20,
	)
	if !reflect.DeepEqual(checked, comparison) {
		t.Fatalf("checked M5.21i comparison drifted: checked=%#v fresh=%#v", checked, comparison)
	}
}

type m521iPhaseWork struct {
	SchedulerDecisions int `json:"scheduler_decisions"`
	WorkUnits          int `json:"work_units"`
}

type m521iPSSObservation struct {
	TotalDecisions     int     `json:"total_decisions"`
	TotalSamples       int     `json:"total_samples"`
	UniqueStates       int     `json:"unique_states"`
	StateSetDigest     string  `json:"state_set_digest"`
	PrefixArea         int64   `json:"prefix_area"`
	SelfNormalizedArea float64 `json:"self_normalized_area"`
	NewStatesByAttempt []int   `json:"new_states_by_attempt"`
	UniqueStateCurve   []int   `json:"unique_state_curve"`
}

type m521iWorkloadObservation struct {
	Planned   int `json:"planned"`
	Offered   int `json:"offered"`
	Completed int `json:"completed"`
	Pending   int `json:"pending"`
}

type m521iMonitorObservation struct {
	Checks     int `json:"checks"`
	Violations int `json:"violations"`
}

type m521iMethodCorpus struct {
	ID                        string                       `json:"id"`
	EffectiveExecutionDigests []string                     `json:"effective_execution_digests"`
	BundleDigests             []string                     `json:"bundle_digests"`
	LogicalExecutionAttempts  int                          `json:"logical_execution_attempts"`
	Primary                   m521iPhaseWork               `json:"primary"`
	Replay                    m521iPhaseWork               `json:"replay"`
	ReplayStable              bool                         `json:"replay_stable"`
	PSS                       m521iPSSObservation          `json:"pss"`
	Faults                    controlexperiment.FaultUsage `json:"faults"`
	Workload                  m521iWorkloadObservation     `json:"workload"`
	Monitors                  m521iMonitorObservation      `json:"monitors"`
}

type m521iModelWork struct {
	Calls        int `json:"calls"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type m521iMethodView struct {
	ID       string         `json:"id"`
	CorpusID string         `json:"corpus_id"`
	Model    m521iModelWork `json:"model"`
}

type m521iPSSSetComparison struct {
	Intersection    int     `json:"intersection"`
	Union           int     `json:"union"`
	ActionClassOnly int     `json:"action_class_only"`
	AdaptiveOnly    int     `json:"adaptive_only"`
	Jaccard         float64 `json:"jaccard"`
}

type m521iComparison struct {
	SchemaVersion               string                `json:"schema_version"`
	SourceCampaignArchiveSHA256 string                `json:"source_campaign_archive_sha256"`
	TargetIdentityDigest        string                `json:"target_identity_digest"`
	NewModelCalls               int                   `json:"new_model_calls"`
	NewSUTExecutions            int                   `json:"new_sut_executions"`
	PhysicalEvidenceExecutions  int                   `json:"physical_evidence_executions"`
	ActionClassCorpus           m521iMethodCorpus     `json:"action_class_corpus"`
	AdaptiveCorpus              m521iMethodCorpus     `json:"adaptive_corpus"`
	MethodViews                 []m521iMethodView     `json:"method_views"`
	PSSSetComparison            m521iPSSSetComparison `json:"pss_set_comparison"`
	Classification              string                `json:"classification"`
}

func newM521iMethodCorpus(
	t *testing.T,
	id string,
	effectiveDigests []string,
	bundles []controlexperiment.ExecutionBundle,
) (m521iMethodCorpus, map[string]bool) {
	t.Helper()
	if len(bundles) != 2 || len(effectiveDigests) != len(bundles) {
		t.Fatal("M5.21i requires exactly two effective executions per corpus")
	}
	corpus := m521iMethodCorpus{
		ID: id, EffectiveExecutionDigests: append([]string(nil), effectiveDigests...),
		LogicalExecutionAttempts: len(bundles), ReplayStable: true,
	}
	measured := make([]protocolstate.MeasuredRun, 0, len(bundles))
	for index, bundle := range bundles {
		if err := bundle.Validate(); err != nil {
			t.Fatalf("%s bundle %d invalid: %v", id, index+1, err)
		}
		if err := bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
			t.Fatalf("%s bundle %d projection invalid: %v", id, index+1, err)
		}
		if _, err := controlexperiment.NewPSSFeedback(
			id+"-attempt-"+string(rune('1'+index)), bundle, etcdraftv2.CorePSSMapper{},
		); err != nil {
			t.Fatalf("%s bundle %d PSS reprojection invalid: %v", id, index+1, err)
		}
		if bundle.Run.Faults == nil || bundle.Run.Workload == nil || !bundle.Run.Replay.Stable {
			t.Fatalf("%s bundle %d lacks comparable evidence", id, index+1)
		}
		corpus.BundleDigests = append(corpus.BundleDigests, bundle.Digest)
		corpus.Primary.SchedulerDecisions += bundle.Work.Primary.SchedulerDecisions
		corpus.Primary.WorkUnits += bundle.Work.Primary.WorkUnits
		corpus.Replay.SchedulerDecisions += bundle.Work.Replay.SchedulerDecisions
		corpus.Replay.WorkUnits += bundle.Work.Replay.WorkUnits
		corpus.Faults.Crashes += bundle.Run.Faults.Crashes
		corpus.Faults.MessageDrops += bundle.Run.Faults.MessageDrops
		corpus.Faults.MessageDuplicates += bundle.Run.Faults.MessageDuplicates
		corpus.Faults.Partitions += bundle.Run.Faults.Partitions
		corpus.Workload.Planned += bundle.Run.Workload.Planned
		corpus.Workload.Offered += bundle.Run.Workload.Offered
		corpus.Workload.Completed += bundle.Run.Workload.Completed
		corpus.Workload.Pending += bundle.Run.Workload.Pending
		checked := oracle.CheckBundle(bundle, oracle.BundleAgreement{}, oracle.BundleTraceIntegrity{})
		corpus.Monitors.Checks += len(checked.Checked)
		corpus.Monitors.Violations += len(checked.Violations)
		current := protocolstate.MeasuredRun{
			Run: index + 1,
			Initial: protocolstate.Sample{
				Key: bundle.CorePSS[0].Key, State: bundle.CorePSS[0].State,
			},
		}
		for decision := 1; decision < len(bundle.CorePSS); decision++ {
			sample := bundle.CorePSS[decision]
			current.Decisions = append(current.Decisions, protocolstate.MeasuredDecision{
				Step:   decision,
				Sample: &protocolstate.Sample{Step: decision, Key: sample.Key, State: sample.State},
			})
		}
		measured = append(measured, current)
	}
	summary, err := protocolstate.Aggregate(bundles[0].Identity.PSSID, measured)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(summary.States))
	states := make(map[string]bool, len(summary.States))
	newStates := make([]int, len(bundles))
	for _, witness := range summary.States {
		keys = append(keys, witness.Key)
		states[witness.Key] = true
		newStates[witness.FirstRun-1]++
	}
	sort.Strings(keys)
	stateSetDigest, err := control.CanonicalDigest(keys)
	if err != nil {
		t.Fatal(err)
	}
	curve := make([]int, 0, len(summary.Curve))
	for _, point := range summary.Curve {
		curve = append(curve, point.UniqueStates)
	}
	corpus.PSS = m521iPSSObservation{
		TotalDecisions:     summary.TotalDecisions,
		TotalSamples:       summary.TotalDecisions + summary.Runs,
		UniqueStates:       summary.UniqueStates,
		StateSetDigest:     stateSetDigest,
		PrefixArea:         summary.PrefixArea,
		SelfNormalizedArea: summary.SelfNormalizedArea,
		NewStatesByAttempt: newStates,
		UniqueStateCurve:   curve,
	}
	return corpus, states
}

func compareM521iStateSets(
	action map[string]bool,
	adaptive map[string]bool,
) (intersection int, union int, actionOnly int, adaptiveOnly int) {
	for key := range action {
		union++
		if adaptive[key] {
			intersection++
		} else {
			actionOnly++
		}
	}
	for key := range adaptive {
		if !action[key] {
			union++
			adaptiveOnly++
		}
	}
	return intersection, union, actionOnly, adaptiveOnly
}

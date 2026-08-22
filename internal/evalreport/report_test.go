package evalreport

import (
	"math"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func TestBuildSummarizesAndPairsRootCauseOutcomes(t *testing.T) {
	left := testEvidence("agentic", "benchmark-a", map[string]string{
		"root-a": defectbench.BundleStatusKilled,
		"root-b": defectbench.BundleStatusKilled,
		"root-c": defectbench.BundleStatusKilled,
		"root-d": defectbench.BundleStatusSurvived,
	}, map[string]string{"root-d": defectbench.BundleStatusFalsePositive})
	left.Source = SourceAgenticHoldout
	left.ModelWorkMeasured = true
	left.ModelWork.Calls = 8
	left.ModelWork.TotalTokens = 1200
	left.ScenarioDecisions = 21

	right := testEvidence("bounded-random", "benchmark-a", map[string]string{
		"root-a": defectbench.BundleStatusSurvived,
		"root-b": defectbench.BundleStatusKilled,
		"root-c": defectbench.BundleStatusKilled,
		"root-d": defectbench.BundleStatusKilled,
	}, nil)
	right.Source = SourceFormalFresh

	report, err := Build([]Evidence{right, left})
	if err != nil {
		t.Fatal(err)
	}
	if report.BenchmarkID != "benchmark-a" || len(report.Methods) != 2 || len(report.Comparisons) != 1 {
		t.Fatalf("unexpected report shape: %#v", report)
	}
	if report.Methods[0].Label != "agentic" || report.Methods[0].KilledRootCauses != 3 ||
		report.Methods[0].FalsePositives != 1 || report.Methods[0].ModelTokens != 1200 {
		t.Fatalf("agentic summary = %#v", report.Methods[0])
	}
	comparison := report.Comparisons[0]
	if comparison.BothKilled != 2 || comparison.LeftOnly != 1 || comparison.RightOnly != 1 ||
		comparison.Neither != 0 || comparison.Delta != 0 || comparison.ExactP != 1 {
		t.Fatalf("comparison = %#v", comparison)
	}

	markdown := report.Markdown()
	for _, required := range []string{"agentic", "bounded-random", "75.0%", "Exact McNemar p", "1200"} {
		if !strings.Contains(markdown, required) {
			t.Fatalf("markdown missing %q:\n%s", required, markdown)
		}
	}
	for _, private := range []string{"root-a", "control-root-a", "candidate-root-a"} {
		if strings.Contains(markdown, private) {
			t.Fatalf("markdown leaked private identifier %q", private)
		}
	}
}

func TestBuildRejectsIncomparableEvidence(t *testing.T) {
	base := testEvidence("a", "benchmark-a", map[string]string{
		"root-a": defectbench.BundleStatusKilled,
		"root-b": defectbench.BundleStatusSurvived,
		"root-c": defectbench.BundleStatusSurvived,
	}, nil)
	tests := []struct {
		name  string
		other Evidence
		want  string
	}{
		{name: "benchmark", other: testEvidence("b", "benchmark-b", map[string]string{
			"root-a": defectbench.BundleStatusKilled,
			"root-b": defectbench.BundleStatusSurvived,
			"root-c": defectbench.BundleStatusSurvived,
		}, nil), want: "EVALUATION_REPORT_BENCHMARK_MISMATCH"},
		{name: "root set", other: testEvidence("b", "benchmark-a", map[string]string{
			"root-a": defectbench.BundleStatusKilled,
			"root-b": defectbench.BundleStatusSurvived,
			"root-x": defectbench.BundleStatusSurvived,
		}, nil), want: "EVALUATION_REPORT_ROOT_SET_MISMATCH"},
		{name: "duplicate label", other: testEvidence("a", "benchmark-a", map[string]string{
			"root-a": defectbench.BundleStatusKilled,
			"root-b": defectbench.BundleStatusSurvived,
			"root-c": defectbench.BundleStatusSurvived,
		}, nil), want: "EVALUATION_REPORT_LABEL_DUPLICATE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Build([]Evidence{base, test.other})
			if err == nil || err.Error() != test.want {
				t.Fatalf("err = %v, want %s", err, test.want)
			}
		})
	}
}

func TestStatisticalHelpers(t *testing.T) {
	interval := wilson95(5, 10)
	if math.Abs(interval.Low-0.236593) > 0.00001 || math.Abs(interval.High-0.763407) > 0.00001 {
		t.Fatalf("Wilson interval = %#v", interval)
	}
	if got := exactMcNemar(3, 0); math.Abs(got-0.25) > 1e-12 {
		t.Fatalf("exactMcNemar(3, 0) = %v", got)
	}
	if got := exactMcNemar(5, 1); math.Abs(got-0.21875) > 1e-12 {
		t.Fatalf("exactMcNemar(5, 1) = %v", got)
	}
}

func testEvidence(
	label string,
	benchmark string,
	candidateStatus map[string]string,
	controlStatus map[string]string,
) Evidence {
	evidence := Evidence{Label: label, Source: SourceFormalFresh, BenchmarkID: benchmark}
	index := 0
	for root, status := range candidateStatus {
		index++
		controlTrial := "control-" + root
		candidateTrial := "candidate-" + root
		evidence.Pairs = append(evidence.Pairs, defectbench.FormalPairEvaluation{
			PairID: "pair-" + root, RootCauseID: root,
			ControlTrialID: controlTrial, CandidateTrialID: candidateTrial,
		})
		currentControlStatus := defectbench.BundleStatusControlPass
		if override := controlStatus[root]; override != "" {
			currentControlStatus = override
		}
		evidence.Results = append(evidence.Results,
			defectbench.BundleTrialResult{
				TrialID: controlTrial, Kind: defectbench.BundleKindControl,
				Status: currentControlStatus, Decisions: index, PrimaryWork: index + 1, ReplayWork: 1,
			},
			defectbench.BundleTrialResult{
				TrialID: candidateTrial, Kind: defectbench.FormalBundleKindCandidate,
				RootCauseID: root, Status: status, Decisions: index + 2, PrimaryWork: index + 3, ReplayWork: 2,
			},
		)
	}
	return evidence
}

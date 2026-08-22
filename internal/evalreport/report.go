// Package evalreport derives publication-facing descriptive statistics from
// trusted defect-benchmark verdicts. It is intentionally downstream of the
// evaluator: nothing in this package can create or change a finding.
package evalreport

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

const (
	SourceFormalFresh    = "formal-fresh"
	SourceAgenticHoldout = "agentic-holdout"
)

// Evidence is the narrow, read-only projection needed for paper statistics.
// Constructors accept only reports that pass their trusted evaluator's own
// validation.
type Evidence struct {
	Label             string
	Source            string
	BenchmarkID       string
	Pairs             []defectbench.FormalPairEvaluation
	Results           []defectbench.BundleTrialResult
	ModelWork         controlexperiment.ModelWork
	ModelWorkMeasured bool
	ScenarioDecisions int
}

// Interval is a Wilson score confidence interval for a binomial proportion.
type Interval struct {
	Low  float64
	High float64
}

// MethodSummary contains denominators and costs without exposing private
// pair, trial, or root-cause identifiers.
type MethodSummary struct {
	Label                 string
	Source                string
	Controls              int
	FalsePositives        int
	FalsePositiveInterval Interval
	Candidates            int
	KilledCandidates      int
	RootCauses            int
	KilledRootCauses      int
	RootCauseInterval     Interval
	InvalidTrials         int
	Decisions             int64
	PrimaryWork           int64
	ReplayWork            int64
	ModelCalls            int64
	ModelTokens           int64
	ScenarioDecisions     int64
	ModelWorkMeasured     bool
}

// PairedComparison is an exact root-cause-level comparison. ExactP is the
// two-sided exact McNemar p-value over discordant roots.
type PairedComparison struct {
	Left       string
	Right      string
	RootCauses int
	BothKilled int
	LeftOnly   int
	RightOnly  int
	Neither    int
	Delta      float64
	ExactP     float64
}

// Report is an in-memory rendering model, not a trusted evidence schema.
type Report struct {
	BenchmarkID string
	Methods     []MethodSummary
	Comparisons []PairedComparison
}

func FromFormal(label string, report defectbench.FormalFreshEvaluation) (Evidence, error) {
	if err := report.Validate(); err != nil {
		return Evidence{}, fmt.Errorf("formal evaluation validation failed: %w", err)
	}
	return Evidence{
		Label: label, Source: SourceFormalFresh, BenchmarkID: report.BenchmarkID,
		Pairs:   append([]defectbench.FormalPairEvaluation(nil), report.Pairs...),
		Results: append([]defectbench.BundleTrialResult(nil), report.Results...),
	}, nil
}

func FromAgentic(label string, report defectbench.AgenticHoldoutEvaluation) (Evidence, error) {
	if err := report.Validate(); err != nil {
		return Evidence{}, fmt.Errorf("agentic holdout validation failed: %w", err)
	}
	result := Evidence{
		Label: label, Source: SourceAgenticHoldout, BenchmarkID: report.BenchmarkID,
		Pairs:             append([]defectbench.FormalPairEvaluation(nil), report.Pairs...),
		Results:           make([]defectbench.BundleTrialResult, 0, len(report.Results)),
		ModelWorkMeasured: true,
	}
	for _, trial := range report.Results {
		result.Results = append(result.Results, trial.Result)
		result.ModelWork.Calls += trial.ModelWork.Calls
		result.ModelWork.InputTokens += trial.ModelWork.InputTokens
		result.ModelWork.OutputTokens += trial.ModelWork.OutputTokens
		result.ModelWork.TotalTokens += trial.ModelWork.TotalTokens
		result.ScenarioDecisions += trial.ScenarioDecisionsUsed
	}
	return result, nil
}

// Build validates comparison compatibility, aggregates each method, and
// computes all pairwise exact comparisons in label order.
func Build(evidence []Evidence) (Report, error) {
	if len(evidence) == 0 {
		return Report{}, errors.New("EVALUATION_REPORT_INPUT_REQUIRED")
	}
	ordered := append([]Evidence(nil), evidence...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Label < ordered[j].Label })

	benchmarkID := ordered[0].BenchmarkID
	labels := make(map[string]bool, len(ordered))
	rootSets := make([]map[string]bool, len(ordered))
	outcomes := make([]map[string]bool, len(ordered))
	report := Report{BenchmarkID: benchmarkID}
	for index, current := range ordered {
		if strings.TrimSpace(current.Label) == "" || current.Label != strings.TrimSpace(current.Label) ||
			strings.ContainsAny(current.Label, "|\r\n") {
			return Report{}, errors.New("EVALUATION_REPORT_LABEL_INVALID")
		}
		if labels[current.Label] {
			return Report{}, errors.New("EVALUATION_REPORT_LABEL_DUPLICATE")
		}
		labels[current.Label] = true
		if current.Source != SourceFormalFresh && current.Source != SourceAgenticHoldout {
			return Report{}, errors.New("EVALUATION_REPORT_SOURCE_INVALID")
		}
		if strings.TrimSpace(current.BenchmarkID) == "" || current.BenchmarkID != benchmarkID {
			return Report{}, errors.New("EVALUATION_REPORT_BENCHMARK_MISMATCH")
		}
		roots, killed, err := rootOutcomes(current.Pairs, current.Results)
		if err != nil {
			return Report{}, err
		}
		rootSets[index], outcomes[index] = roots, killed
		if index > 0 && !sameStringSet(rootSets[0], roots) {
			return Report{}, errors.New("EVALUATION_REPORT_ROOT_SET_MISMATCH")
		}
		report.Methods = append(report.Methods, summarize(current, roots, killed))
	}

	for left := 0; left < len(ordered); left++ {
		for right := left + 1; right < len(ordered); right++ {
			report.Comparisons = append(report.Comparisons, compare(
				ordered[left].Label, ordered[right].Label, rootSets[left], outcomes[left], outcomes[right],
			))
		}
	}
	return report, nil
}

func rootOutcomes(
	pairs []defectbench.FormalPairEvaluation,
	results []defectbench.BundleTrialResult,
) (map[string]bool, map[string]bool, error) {
	if len(pairs) == 0 || len(results) != len(pairs)*2 {
		return nil, nil, errors.New("EVALUATION_REPORT_PAIR_SET_INVALID")
	}
	byTrial := make(map[string]defectbench.BundleTrialResult, len(results))
	for _, result := range results {
		if result.TrialID == "" || byTrial[result.TrialID].TrialID != "" {
			return nil, nil, errors.New("EVALUATION_REPORT_RESULT_SET_INVALID")
		}
		byTrial[result.TrialID] = result
	}
	roots, killed, used := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, pair := range pairs {
		control, controlOK := byTrial[pair.ControlTrialID]
		candidate, candidateOK := byTrial[pair.CandidateTrialID]
		if pair.RootCauseID == "" || !controlOK || !candidateOK ||
			used[pair.ControlTrialID] || used[pair.CandidateTrialID] ||
			control.Kind != defectbench.BundleKindControl ||
			candidate.Kind != defectbench.FormalBundleKindCandidate ||
			candidate.RootCauseID != pair.RootCauseID {
			return nil, nil, errors.New("EVALUATION_REPORT_PAIR_SET_INVALID")
		}
		used[pair.ControlTrialID], used[pair.CandidateTrialID] = true, true
		roots[pair.RootCauseID] = true
		if candidate.Status == defectbench.BundleStatusKilled {
			killed[pair.RootCauseID] = true
		}
	}
	if len(used) != len(results) {
		return nil, nil, errors.New("EVALUATION_REPORT_RESULT_SET_INVALID")
	}
	return roots, killed, nil
}

func summarize(current Evidence, roots, killed map[string]bool) MethodSummary {
	summary := MethodSummary{
		Label: current.Label, Source: current.Source, RootCauses: len(roots),
		KilledRootCauses: len(killed), ModelWorkMeasured: current.ModelWorkMeasured,
		ModelCalls: int64(current.ModelWork.Calls), ModelTokens: int64(current.ModelWork.TotalTokens),
		ScenarioDecisions: int64(current.ScenarioDecisions),
	}
	for _, result := range current.Results {
		summary.Decisions += int64(result.Decisions)
		summary.PrimaryWork += int64(result.PrimaryWork)
		summary.ReplayWork += int64(result.ReplayWork)
		if result.Status == defectbench.BundleStatusInvalid {
			summary.InvalidTrials++
		}
		switch result.Kind {
		case defectbench.BundleKindControl:
			summary.Controls++
			if result.Status == defectbench.BundleStatusFalsePositive {
				summary.FalsePositives++
			}
		case defectbench.FormalBundleKindCandidate:
			summary.Candidates++
			if result.Status == defectbench.BundleStatusKilled {
				summary.KilledCandidates++
			}
		}
	}
	summary.FalsePositiveInterval = wilson95(summary.FalsePositives, summary.Controls)
	summary.RootCauseInterval = wilson95(summary.KilledRootCauses, summary.RootCauses)
	return summary
}

func compare(left, right string, roots, leftKilled, rightKilled map[string]bool) PairedComparison {
	result := PairedComparison{Left: left, Right: right, RootCauses: len(roots)}
	for root := range roots {
		switch {
		case leftKilled[root] && rightKilled[root]:
			result.BothKilled++
		case leftKilled[root]:
			result.LeftOnly++
		case rightKilled[root]:
			result.RightOnly++
		default:
			result.Neither++
		}
	}
	result.Delta = float64(result.LeftOnly-result.RightOnly) / float64(result.RootCauses)
	result.ExactP = exactMcNemar(result.LeftOnly, result.RightOnly)
	return result
}

func sameStringSet(left, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if !right[value] {
			return false
		}
	}
	return true
}

func wilson95(successes, total int) Interval {
	if total == 0 {
		return Interval{}
	}
	const z = 1.959963984540054
	n := float64(total)
	p := float64(successes) / n
	z2 := z * z
	center := (p + z2/(2*n)) / (1 + z2/n)
	half := z * math.Sqrt(p*(1-p)/n+z2/(4*n*n)) / (1 + z2/n)
	return Interval{Low: math.Max(0, center-half), High: math.Min(1, center+half)}
}

func exactMcNemar(leftOnly, rightOnly int) float64 {
	discordant := leftOnly + rightOnly
	if discordant == 0 {
		return 1
	}
	limit := leftOnly
	if rightOnly < limit {
		limit = rightOnly
	}
	logTwo := math.Log(2)
	cumulative := 0.0
	for successes := 0; successes <= limit; successes++ {
		logCombination, _ := math.Lgamma(float64(discordant + 1))
		leftGamma, _ := math.Lgamma(float64(successes + 1))
		rightGamma, _ := math.Lgamma(float64(discordant - successes + 1))
		cumulative += math.Exp(logCombination - leftGamma - rightGamma - float64(discordant)*logTwo)
	}
	return math.Min(1, 2*cumulative)
}

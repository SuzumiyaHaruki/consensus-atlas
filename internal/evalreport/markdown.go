package evalreport

import (
	"fmt"
	"strings"
)

// Markdown renders a deterministic, aggregate-only paper table. It never
// prints private trial IDs, pair IDs, or root-cause labels.
func (report Report) Markdown() string {
	var output strings.Builder
	fmt.Fprintln(&output, "# ConsensusAtlas defect-effectiveness report")
	fmt.Fprintln(&output)
	fmt.Fprintf(&output, "Benchmark: `%s`\n\n", report.BenchmarkID)
	fmt.Fprintln(&output, "## Method summary")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "| Method | Evidence | Root causes killed (Wilson 95% CI) | Candidate kills | Control false positives (Wilson 95% CI) | Invalid trials | Decisions | Primary work | Replay work | Model calls | Model tokens | Scenario decisions |")
	fmt.Fprintln(&output, "|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, method := range report.Methods {
		modelCalls, modelTokens, scenarioDecisions := "n/a", "n/a", "n/a"
		if method.ModelWorkMeasured {
			modelCalls = fmt.Sprintf("%d", method.ModelCalls)
			modelTokens = fmt.Sprintf("%d", method.ModelTokens)
			scenarioDecisions = fmt.Sprintf("%d", method.ScenarioDecisions)
		}
		fmt.Fprintf(&output, "| %s | %s | %s | %d/%d | %s | %d/%d | %d | %d | %d | %s | %s | %s |\n",
			method.Label, method.Source,
			proportion(method.KilledRootCauses, method.RootCauses, method.RootCauseInterval),
			method.KilledCandidates, method.Candidates,
			proportion(method.FalsePositives, method.Controls, method.FalsePositiveInterval),
			method.InvalidTrials, method.Controls+method.Candidates,
			method.Decisions, method.PrimaryWork, method.ReplayWork, modelCalls, modelTokens, scenarioDecisions,
		)
	}
	if len(report.Comparisons) != 0 {
		fmt.Fprintln(&output)
		fmt.Fprintln(&output, "## Paired root-cause comparison")
		fmt.Fprintln(&output)
		fmt.Fprintln(&output, "| Methods (left vs right) | Both killed | Left only | Right only | Neither | Kill-rate delta | Exact McNemar p (unadjusted) |")
		fmt.Fprintln(&output, "|---|---:|---:|---:|---:|---:|---:|")
		for _, comparison := range report.Comparisons {
			fmt.Fprintf(&output, "| %s vs %s | %d | %d | %d | %d | %+.1f%% | %.4g |\n",
				comparison.Left, comparison.Right, comparison.BothKilled,
				comparison.LeftOnly, comparison.RightOnly, comparison.Neither,
				100*comparison.Delta, comparison.ExactP,
			)
		}
	}
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "Wilson intervals use root causes and control trials as their respective Bernoulli units. Exact McNemar tests pair methods by private root-cause ID; reported p-values are unadjusted. Invalid trials remain in the denominators and never receive kill credit. This report is a downstream statistical projection; trusted evaluator verdicts remain authoritative.")
	return output.String()
}

func proportion(successes, total int, interval Interval) string {
	if total == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d/%d (%.1f%%; %.1f–%.1f%%)", successes, total,
		100*float64(successes)/float64(total), 100*interval.Low, 100*interval.High)
}

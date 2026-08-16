package controlexperiment

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const scenarioProgressRecentActions = 8

type ScenarioRecentAction struct {
	Decision int                `json:"decision"`
	Kind     control.ActionKind `json:"kind"`
	Node     control.NodeID     `json:"node,omitempty"`
}

// ScenarioProgressDelta is compact execution feedback for the next planning
// call. Exact Actions and state digests remain in Trace; this view reports only
// progress, novelty, and repeated abstract scheduling patterns.
type ScenarioProgressDelta struct {
	Decisions                int                    `json:"decisions"`
	NewMilestones            []string               `json:"new_milestones,omitempty"`
	FirstMissingMilestone    string                 `json:"first_missing_milestone,omitempty"`
	UniqueStateTransitions   int                    `json:"unique_state_transitions"`
	RepeatedStateTransitions int                    `json:"repeated_state_transitions"`
	RepeatedPatternDepth     int                    `json:"repeated_pattern_depth"`
	RecentActions            []ScenarioRecentAction `json:"recent_actions,omitempty"`
}

func NewScenarioProgressDelta(
	spec semantic.RiskWitnessSpec,
	beforeRisk semantic.RiskWitnessResult,
	before controlruntime.Trace,
	afterRisk semantic.RiskWitnessResult,
	after controlruntime.Trace,
) (ScenarioProgressDelta, error) {
	if spec.Validate() != nil || before.Validate() != nil || after.Validate() != nil ||
		beforeRisk.Validate(spec) != nil || afterRisk.Validate(spec) != nil ||
		beforeRisk.ExecutionDigest != before.Digest || afterRisk.ExecutionDigest != after.Digest ||
		!scenarioTraceHasPrefix(after, before) {
		return ScenarioProgressDelta{}, errors.New("EXPERIMENT_SCENARIO_PROGRESS_DELTA_INPUT_INVALID")
	}
	records := after.Records[len(before.Records):]
	result := ScenarioProgressDelta{Decisions: len(records)}
	seenMilestones := make(map[string]bool, len(beforeRisk.SatisfiedMilestones))
	for _, milestone := range beforeRisk.SatisfiedMilestones {
		seenMilestones[milestone] = true
	}
	for _, milestone := range afterRisk.SatisfiedMilestones {
		if !seenMilestones[milestone] {
			result.NewMilestones = append(result.NewMilestones, milestone)
		}
	}
	if len(afterRisk.MissingMilestones) > 0 {
		result.FirstMissingMilestone = afterRisk.MissingMilestones[0]
	}
	transitions := make(map[string]bool, len(records))
	keys := make([]string, len(records))
	for index, record := range records {
		transition := record.BeforeStateDigest + "\x00" + record.AfterStateDigest
		transitions[transition] = true
		keys[index] = string(record.Action.Kind) + "\x00" + string(record.Action.Node.Node)
	}
	result.UniqueStateTransitions = len(transitions)
	result.RepeatedStateTransitions = len(records) - len(transitions)
	result.RepeatedPatternDepth = repeatedScenarioPatternDepth(keys)
	start := len(records) - scenarioProgressRecentActions
	if start < 0 {
		start = 0
	}
	for _, record := range records[start:] {
		result.RecentActions = append(result.RecentActions, ScenarioRecentAction{
			Decision: int(record.Step), Kind: record.Action.Kind, Node: record.Action.Node.Node,
		})
	}
	return result, nil
}

func repeatedScenarioPatternDepth(keys []string) int {
	best := 0
	for period := 1; period <= 8 && period*2 <= len(keys); period++ {
		blocks := 1
		end := len(keys)
		for end-period*2 >= 0 {
			equal := true
			for offset := 0; offset < period; offset++ {
				if keys[end-period+offset] != keys[end-period*2+offset] {
					equal = false
					break
				}
			}
			if !equal {
				break
			}
			blocks++
			end -= period
		}
		if blocks-1 > best {
			best = blocks - 1
		}
	}
	return best
}

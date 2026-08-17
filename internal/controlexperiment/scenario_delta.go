package controlexperiment

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const scenarioProgressRecentActions = 8

const (
	ScenarioMilestoneProgressUnchanged = "milestone-unchanged"
	ScenarioMilestoneProgressAdvanced  = "milestone-advanced"
	ScenarioMilestoneProgressRepeated  = "scheduling-pattern-repeated"
	ScenarioMilestoneProgressStalled   = "milestone-stalled"
	ScenarioMilestoneProgressReached   = "risk-reached"
)

type ScenarioRecentAction struct {
	Decision int                `json:"decision"`
	Kind     control.ActionKind `json:"kind"`
	Node     control.NodeID     `json:"node,omitempty"`
}

type ScenarioActionCount struct {
	Kind  control.ActionKind `json:"kind"`
	Count int                `json:"count"`
}

// ScenarioMilestoneEvidence is the bounded Agent-facing part of newly
// satisfied trusted evidence. Participant identities and evidence digests stay
// in the full Risk result; the planner only needs to know what appeared where.
type ScenarioMilestoneEvidence struct {
	MilestoneID string `json:"milestone_id"`
	Step        uint64 `json:"step"`
	Kind        string `json:"kind"`
}

// ScenarioProgressDelta is compact execution feedback for the next planning
// call. Exact Actions and state digests remain in Trace; this view reports only
// progress, novelty, and repeated abstract scheduling patterns.
type ScenarioProgressDelta struct {
	Decisions                int                         `json:"decisions"`
	NewMilestones            []string                    `json:"new_milestones,omitempty"`
	NewMilestoneEvidence     []ScenarioMilestoneEvidence `json:"new_milestone_evidence,omitempty"`
	FirstMissingMilestone    string                      `json:"first_missing_milestone,omitempty"`
	MilestoneProgress        string                      `json:"milestone_progress"`
	UniqueStateTransitions   int                         `json:"unique_state_transitions"`
	RepeatedStateTransitions int                         `json:"repeated_state_transitions"`
	RepeatedPatternDepth     int                         `json:"repeated_pattern_depth"`
	ActionCounts             []ScenarioActionCount       `json:"action_counts,omitempty"`
	TemporalCallbacks        int                         `json:"temporal_callbacks"`
	LogicalClockAdvances     int                         `json:"logical_clock_advances"`
	LogicalTimeElapsed       uint64                      `json:"logical_time_elapsed"`
	NaturalProgressStop      string                      `json:"natural_progress_stop,omitempty"`
	FaultAllowance           *FaultEnvelope              `json:"fault_allowance,omitempty"`
	FaultUsage               *FaultUsage                 `json:"fault_usage,omitempty"`
	FaultRemaining           *FaultUsage                 `json:"fault_remaining,omitempty"`
	AvailableInterventions   []control.ActionKind        `json:"available_interventions,omitempty"`
	RecentActions            []ScenarioRecentAction      `json:"recent_actions,omitempty"`
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
	newMilestones := make(map[string]bool, len(result.NewMilestones))
	for _, milestone := range result.NewMilestones {
		newMilestones[milestone] = true
	}
	for _, evidence := range afterRisk.Milestones {
		if newMilestones[evidence.MilestoneID] {
			result.NewMilestoneEvidence = append(result.NewMilestoneEvidence, ScenarioMilestoneEvidence{
				MilestoneID: evidence.MilestoneID, Step: evidence.Step, Kind: evidence.Kind,
			})
		}
	}
	if len(afterRisk.MissingMilestones) > 0 {
		result.FirstMissingMilestone = afterRisk.MissingMilestones[0]
	}
	transitions := make(map[string]bool, len(records))
	actionCounts := make(map[control.ActionKind]int)
	keys := make([]string, len(records))
	for index, record := range records {
		transition := record.BeforeStateDigest + "\x00" + record.AfterStateDigest
		transitions[transition] = true
		keys[index] = string(record.Action.Kind) + "\x00" + string(record.Action.Node.Node)
		actionCounts[record.Action.Kind]++
		if record.Action.Kind == control.ActionFireTemporal {
			result.TemporalCallbacks++
			if record.ClockAdvance != nil && record.ClockAdvance.To > record.ClockAdvance.From {
				result.LogicalClockAdvances++
			}
		}
	}
	kinds := make([]control.ActionKind, 0, len(actionCounts))
	for kind := range actionCounts {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	for _, kind := range kinds {
		result.ActionCounts = append(result.ActionCounts, ScenarioActionCount{Kind: kind, Count: actionCounts[kind]})
	}
	beforeTime, afterTime := scenarioTraceLogicalTime(before), scenarioTraceLogicalTime(after)
	if afterTime >= beforeTime {
		result.LogicalTimeElapsed = afterTime - beforeTime
	}
	result.UniqueStateTransitions = len(transitions)
	result.RepeatedStateTransitions = len(records) - len(transitions)
	result.RepeatedPatternDepth = repeatedScenarioPatternDepth(keys)
	switch {
	case afterRisk.Status == semantic.RiskWitnessReached:
		result.MilestoneProgress = ScenarioMilestoneProgressReached
	case len(result.NewMilestones) > 0:
		result.MilestoneProgress = ScenarioMilestoneProgressAdvanced
	case result.RepeatedPatternDepth > 0:
		result.MilestoneProgress = ScenarioMilestoneProgressRepeated
	case len(records) > 0:
		result.MilestoneProgress = ScenarioMilestoneProgressStalled
	default:
		result.MilestoneProgress = ScenarioMilestoneProgressUnchanged
	}
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

func enrichScenarioProgressDelta(
	delta *ScenarioProgressDelta,
	naturalProgressStop string,
	faultEnvelope *FaultEnvelope,
	trace controlruntime.Trace,
	actions []FrontierActionRef,
) {
	delta.NaturalProgressStop = naturalProgressStop
	if faultEnvelope != nil {
		allowance := *faultEnvelope
		usage := faultUsageFromRecords(trace.Records)
		remaining := FaultUsage{
			Crashes:           allowance.MaxCrashes - usage.Crashes,
			MessageDrops:      allowance.MaxMessageDrops - usage.MessageDrops,
			MessageDuplicates: allowance.MaxMessageDuplicates - usage.MessageDuplicates,
			Partitions:        allowance.MaxPartitions - usage.Partitions,
		}
		delta.FaultAllowance, delta.FaultUsage, delta.FaultRemaining = &allowance, &usage, &remaining
	}
	seen := make(map[control.ActionKind]bool)
	for _, action := range actions {
		if scenarioNaturalProgressKind(action.Kind) || seen[action.Kind] {
			continue
		}
		seen[action.Kind] = true
		delta.AvailableInterventions = append(delta.AvailableInterventions, action.Kind)
	}
	sort.Slice(delta.AvailableInterventions, func(i, j int) bool {
		return delta.AvailableInterventions[i] < delta.AvailableInterventions[j]
	})
}

func scenarioTraceLogicalTime(trace controlruntime.Trace) uint64 {
	if len(trace.Records) == 0 {
		return 0
	}
	return trace.Records[len(trace.Records)-1].LogicalTime
}

func scenarioNaturalProgressKind(kind control.ActionKind) bool {
	for _, candidate := range scenarioNaturalProgressPriority {
		if candidate == kind {
			return true
		}
	}
	return false
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

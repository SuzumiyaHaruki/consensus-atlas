package coverage

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

type matchResult struct {
	Reach         bool
	Observe       bool
	ReachSteps    []int
	ObserveSteps  []int
	OrderingSteps []StepPair
	CountResults  []int
}

func matchEvidence(version int, obligation Obligation, trace []core.TraceRecord, matcher SemanticMatcher) matchResult {
	if version == LegacyProfileVersion {
		predicate := TracePredicate{ObservationLabel: obligation.WitnessLabel}
		steps, matched := matchAll([]TracePredicate{predicate}, trace, matcher)
		return matchResult{Reach: matched, Observe: matched, ReachSteps: steps, ObserveSteps: append([]int(nil), steps...)}
	}
	reachSteps, reached := matchAll(obligation.Evidence.Reach, trace, matcher)
	observeSteps, observed := matchAll(obligation.Evidence.Observe, trace, matcher)
	orderingSteps, ordered := matchOrderings(obligation.Evidence.Orderings, trace, matcher)
	countResults, counted := matchCounts(obligation.Evidence.Counts, trace, matcher)
	return matchResult{
		Reach: reached, Observe: observed && ordered && counted, ReachSteps: reachSteps,
		ObserveSteps: observeSteps, OrderingSteps: orderingSteps, CountResults: countResults,
	}
}

func matchAll(predicates []TracePredicate, trace []core.TraceRecord, matcher SemanticMatcher) ([]int, bool) {
	steps := make([]int, 0, len(predicates))
	for _, predicate := range predicates {
		step, _, ok := firstMatch(predicate, trace, 0, matcher)
		if !ok {
			return steps, false
		}
		steps = append(steps, step)
	}
	return uniqueSorted(steps), true
}

func matchOrderings(orderings []OrderingConstraint, trace []core.TraceRecord, matcher SemanticMatcher) ([]StepPair, bool) {
	steps := make([]StepPair, 0, len(orderings))
	for _, ordering := range orderings {
		found := false
		for beforeIndex := range trace {
			if !predicateMatches(ordering.Before, trace, beforeIndex, matcher) {
				continue
			}
			for afterIndex := beforeIndex + 1; afterIndex < len(trace); afterIndex++ {
				if !predicateMatches(ordering.After, trace, afterIndex, matcher) {
					continue
				}
				if ordering.SameGroup && (trace[beforeIndex].Event.Group == "" ||
					trace[beforeIndex].Event.Group != trace[afterIndex].Event.Group) {
					continue
				}
				if ordering.SameTarget && (trace[beforeIndex].Event.Target == "" ||
					trace[beforeIndex].Event.Target != trace[afterIndex].Event.Target) {
					continue
				}
				if ordering.SameMessage && !sameMessage(trace[beforeIndex].Event, trace[afterIndex].Event) {
					continue
				}
				if anyMatchBetween(ordering.Without, trace, beforeIndex, afterIndex, matcher) {
					continue
				}
				steps = append(steps, StepPair{Before: trace[beforeIndex].Step, After: trace[afterIndex].Step})
				found = true
				break
			}
			if found {
				break
			}
		}
		if !found {
			return steps, false
		}
	}
	return steps, true
}

func sameMessage(before, after core.Event) bool {
	if before.Message == nil || after.Message == nil {
		return false
	}
	return before.Message.PayloadDigest != "" &&
		before.Message.PayloadDigest == after.Message.PayloadDigest &&
		before.Message.From == after.Message.From && before.Message.To == after.Message.To
}

func anyMatchBetween(predicates []TracePredicate, trace []core.TraceRecord, before, after int, matcher SemanticMatcher) bool {
	for index := before + 1; index < after; index++ {
		for _, predicate := range predicates {
			if predicateMatches(predicate, trace, index, matcher) {
				return true
			}
		}
	}
	return false
}

func matchCounts(constraints []CountConstraint, trace []core.TraceRecord, matcher SemanticMatcher) ([]int, bool) {
	results := make([]int, 0, len(constraints))
	for _, constraint := range constraints {
		count := 0
		distinct := make(map[string]bool)
		for index := range trace {
			if !predicateMatches(constraint.Predicate, trace, index, matcher) {
				continue
			}
			if constraint.DistinctBy == "" {
				count++
				continue
			}
			key := distinctValue(constraint, trace[index])
			if key != "" && !distinct[key] {
				distinct[key] = true
				count++
			}
		}
		results = append(results, count)
		if count < constraint.AtLeast || (constraint.AtMost > 0 && count > constraint.AtMost) {
			return results, false
		}
	}
	return results, true
}

func distinctValue(constraint CountConstraint, record core.TraceRecord) string {
	switch constraint.DistinctBy {
	case "event_source":
		return record.Event.Source
	case "event_target":
		return record.Event.Target
	case "observation_node":
		for _, observation := range record.Observations {
			if observationMatches(constraint.Predicate, observation) {
				return observation.Node
			}
		}
	default:
		const prefix = "observation_evidence:"
		if strings.HasPrefix(constraint.DistinctBy, prefix) {
			key := strings.TrimPrefix(constraint.DistinctBy, prefix)
			for _, observation := range record.Observations {
				if observationMatches(constraint.Predicate, observation) {
					return observation.Evidence[key]
				}
			}
		}
	}
	return ""
}

func firstMatch(predicate TracePredicate, trace []core.TraceRecord, start int, matcher SemanticMatcher) (int, int, bool) {
	for index := start; index < len(trace); index++ {
		if predicateMatches(predicate, trace, index, matcher) {
			return trace[index].Step, index, true
		}
	}
	return 0, 0, false
}

func predicateMatches(predicate TracePredicate, trace []core.TraceRecord, index int, matcher SemanticMatcher) bool {
	record := trace[index]
	if predicate.EventKind != "" && record.Event.Kind != predicate.EventKind {
		return false
	}
	if predicate.EventSource != "" && record.Event.Source != predicate.EventSource {
		return false
	}
	if predicate.EventTarget != "" && record.Event.Target != predicate.EventTarget {
		return false
	}
	if predicate.MessageType != "" && (record.Event.Message == nil || record.Event.Message.TypeHint != predicate.MessageType) {
		return false
	}
	if predicate.Outcome != "" && record.Outcome != predicate.Outcome {
		return false
	}
	if predicate.HostCutpoint != "" && !matchesHostCutpoint(predicate.HostCutpoint, trace, index) {
		return false
	}
	if predicate.Semantic != nil && (matcher == nil || matcher.ID() != predicate.Semantic.Domain || !matcher.Match(*predicate.Semantic, record)) {
		return false
	}
	if !hasObservationSelector(predicate) {
		return true
	}
	for _, observation := range record.Observations {
		if observationMatches(predicate, observation) {
			return true
		}
	}
	return false
}

func matchesHostCutpoint(cutpoint string, trace []core.TraceRecord, index int) bool {
	record := trace[index]
	if record.Event.Kind != core.EventCrash || record.Event.Target == "" {
		return false
	}
	batch, declared := batchBeforeCrash(record.Before, record.Event.Target)
	if batch == "" {
		return false
	}
	seen := make(map[core.EventKind]bool)
	for prior := 0; prior < index; prior++ {
		if trace[prior].Event.Group == batch && trace[prior].Outcome == string(core.StatusApplied) {
			seen[trace[prior].Event.Kind] = true
		}
	}
	switch cutpoint {
	case "before-persist":
		return declared[core.EventPersist] && !seen[core.EventPersist]
	case "persist-before-sync":
		return declared[core.EventPersist] && declared[core.EventSync] && seen[core.EventPersist] && !seen[core.EventSync]
	case "sync-before-release":
		return declared[core.EventSync] && declared[core.EventEmit] && seen[core.EventSync] && !seen[core.EventEmit]
	case "release-before-ack":
		return declared[core.EventEmit] && declared[core.EventAcknowledge] && seen[core.EventEmit] && !seen[core.EventAcknowledge]
	case "apply-before-ack":
		return declared[core.EventApply] && declared[core.EventAcknowledge] && seen[core.EventApply] && !seen[core.EventAcknowledge]
	default:
		return false
	}
}

func batchBeforeCrash(snapshot any, node string) (string, map[core.EventKind]bool) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", nil
	}
	var state struct {
		Runtime struct {
			Nodes map[string]struct {
				Batch      string           `json:"batch"`
				Operations []core.EventKind `json:"operations"`
			} `json:"nodes"`
		} `json:"runtime"`
	}
	if json.Unmarshal(encoded, &state) != nil {
		return "", nil
	}
	declared := make(map[core.EventKind]bool)
	for _, kind := range state.Runtime.Nodes[node].Operations {
		declared[kind] = true
	}
	return state.Runtime.Nodes[node].Batch, declared
}

func hasObservationSelector(predicate TracePredicate) bool {
	return predicate.ObservationKind != "" || predicate.ObservationLabel != "" || predicate.ObservationNode != "" ||
		predicate.ObservationValue != "" || len(predicate.ObservationEvidence) != 0
}

func observationMatches(predicate TracePredicate, observation core.Observation) bool {
	if predicate.ObservationKind != "" && observation.Kind != predicate.ObservationKind {
		return false
	}
	if predicate.ObservationLabel != "" && observation.Label != predicate.ObservationLabel {
		return false
	}
	if predicate.ObservationNode != "" && observation.Node != predicate.ObservationNode {
		return false
	}
	if predicate.ObservationValue != "" && observation.Value != predicate.ObservationValue {
		return false
	}
	for key, value := range predicate.ObservationEvidence {
		if observation.Evidence == nil || observation.Evidence[key] != value {
			return false
		}
	}
	return true
}

func uniqueSorted(values []int) []int {
	seen := make(map[int]bool, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Ints(result)
	return result
}

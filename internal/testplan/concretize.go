package testplan

import (
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
)

// Concretize is a pure deterministic translation. It adds the trusted common
// bootstrap and cannot alter targets, search budgets, or runtime actions.
func Concretize(profile coverage.Profile, plan Plan) (ConcretePlan, error) {
	synthetic := Suite{
		Version: Version, ID: "concretize-check", ProfileID: profile.ID,
		MaxTotalRuns:      plan.Search.Config.Runs,
		MaxTotalDecisions: plan.Search.Config.TargetDecisionBudget(), Plans: []Plan{plan},
	}
	digest, err := coverage.Digest(profile)
	if err != nil {
		return ConcretePlan{}, err
	}
	synthetic.ProfileDigest = digest
	if err := synthetic.Validate(profile); err != nil {
		return ConcretePlan{}, err
	}
	nodes := append([]string(nil), profile.Nodes...)
	sort.Strings(nodes)
	steps := make([]scenario.Step, 0, len(nodes)+1+len(plan.Prepare)+len(plan.Stimuli))
	for _, node := range nodes {
		steps = append(steps, scenario.Step{Op: OpInject, Kind: core.EventStart, Target: node})
	}
	steps = append(steps, scenario.Step{Op: "run", Count: BootstrapDrainLimit})
	for _, action := range plan.Prepare {
		step, err := concretizeAction(action)
		if err != nil {
			return ConcretePlan{}, err
		}
		steps = append(steps, step)
	}
	for _, input := range plan.Stimuli {
		steps = append(steps, scenario.Step{
			Op: OpInject, Kind: input.Kind, Target: input.Target,
			Payload: append([]byte(nil), input.Payload...),
		})
	}
	return ConcretePlan{
		ID: plan.ID, Targets: append([]string(nil), plan.Targets...),
		Setup:  scenario.Spec{Version: 1, Name: "test-plan/" + plan.ID, Steps: steps},
		Search: plan.Search,
	}, nil
}

func concretizeAction(action Action) (scenario.Step, error) {
	step := scenario.Step{Op: action.Op}
	switch action.Op {
	case OpInject:
		step.Kind, step.Target = action.Kind, action.Target
		step.Payload = append([]byte(nil), action.Payload...)
	case OpExecute:
		step.Op = "execute_match"
		step.Match = cloneSelector(action.Match)
	case OpDrop:
		step.Op = "drop_match"
		step.Match = cloneSelector(action.Match)
	case OpDuplicate:
		step.Op = "duplicate_match"
		step.Match = cloneSelector(action.Match)
	case OpPartition:
		step.Groups = cloneGroups(action.Groups)
	case OpHeal:
	case OpDrain:
		step.Op, step.Count = "run", action.Count
	case OpAdvance:
		step.Ticks = action.Ticks
	default:
		return scenario.Step{}, fmt.Errorf("unsupported concrete action %q", action.Op)
	}
	return step, nil
}

func cloneSelector(selector *scenario.Selector) *scenario.Selector {
	if selector == nil {
		return nil
	}
	copy := *selector
	return &copy
}

func cloneGroups(groups [][]string) [][]string {
	result := make([][]string, len(groups))
	for index := range groups {
		result[index] = append([]string(nil), groups[index]...)
	}
	return result
}

package explore_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/explore"
)

type fixtureAdapter struct {
	executed int
}

func (*fixtureAdapter) Protocol() string                  { return "explore-fixture" }
func (*fixtureAdapter) Nodes() []string                   { return []string{"n1", "n2"} }
func (a *fixtureAdapter) Snapshot() any                   { return map[string]int{"executed": a.executed} }
func (*fixtureAdapter) CheckConformance() error           { return nil }
func (*fixtureAdapter) Enabled(core.Event) (bool, string) { return true, "" }
func (a *fixtureAdapter) Apply(_ context.Context, _ core.Event) (core.ApplyResult, error) {
	a.executed++
	return core.ApplyResult{Status: core.StatusApplied}, nil
}

func twoEventFactory(_ context.Context) (*engine.Engine, error) {
	e := engine.New(&fixtureAdapter{})
	e.Schedule(core.Event{Kind: core.EventCampaign, Target: "n1"})
	e.Schedule(core.Event{Kind: core.EventCampaign, Target: "n2"})
	return e, nil
}

func messageFactory(_ context.Context) (*engine.Engine, error) {
	e := engine.New(&fixtureAdapter{})
	e.Schedule(core.Event{
		Kind: core.EventMessage, Source: "n1", Target: "n2", Payload: json.RawMessage(`"vote"`),
	})
	return e, nil
}

func TestDFSReplaysBothTwoEventOrders(t *testing.T) {
	explorer, err := explore.New(explore.StrategyDFS)
	if err != nil {
		t.Fatal(err)
	}
	result, err := explorer.Explore(context.Background(), twoEventFactory, explore.Config{
		Runs: 4, BudgetPerRun: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	runs := result.Runs
	if len(runs) != 2 || result.StopReason != "search_space_exhausted" {
		t.Fatalf("result = %#v, want both event orders and exhausted space", result)
	}
	orders := [][]string{
		{runs[0].Decisions[0].EventID, runs[0].Decisions[1].EventID},
		{runs[1].Decisions[0].EventID, runs[1].Decisions[1].EventID},
	}
	if !reflect.DeepEqual(orders, [][]string{{"e000001", "e000002"}, {"e000002", "e000001"}}) {
		t.Fatalf("DFS orders = %#v", orders)
	}
	for _, run := range runs {
		if run.Termination != "budget_exhausted" || !run.Conform || len(run.Trace) != 2 {
			t.Fatalf("invalid DFS run: %#v", run)
		}
	}
}

func TestRandomIsDeterministicForPublishedSeeds(t *testing.T) {
	explorer := explore.Random{}
	config := explore.Config{Runs: 5, BudgetPerRun: 2, Seed: 17}
	left, err := explorer.Explore(context.Background(), twoEventFactory, config)
	if err != nil {
		t.Fatal(err)
	}
	right, err := explorer.Explore(context.Background(), twoEventFactory, config)
	if err != nil {
		t.Fatal(err)
	}
	for index := range left.Runs {
		if !reflect.DeepEqual(left.Runs[index].Decisions, right.Runs[index].Decisions) ||
			left.Runs[index].ExecutionFingerprint != right.Runs[index].ExecutionFingerprint {
			t.Fatalf("run %d is not deterministic", index+1)
		}
	}
}

func TestRecordedDecisionLogReplaysWithoutExplorerChoice(t *testing.T) {
	result, err := (explore.Random{}).Explore(context.Background(), twoEventFactory, explore.Config{
		Runs: 1, BudgetPerRun: 2, DecisionBudget: 2, Seed: 21,
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := explore.ReplayRun(
		context.Background(), twoEventFactory, explore.ActionPolicy{}, result.Runs[0],
	)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ExecutionFingerprint != result.Runs[0].ExecutionFingerprint {
		t.Fatal("forced decision replay changed the execution fingerprint")
	}

	tampered := result.Runs[0]
	tampered.Decisions = append([]explore.Decision(nil), tampered.Decisions...)
	tampered.Decisions[0].CandidateCount++
	if _, err := explore.ReplayRun(context.Background(), twoEventFactory, explore.ActionPolicy{}, tampered); err == nil {
		t.Fatal("tampered candidate count was accepted by strict replay")
	}
}

func TestDFSIncludesBoundedDropAndDuplicateActions(t *testing.T) {
	result, err := (explore.DFS{}).Explore(context.Background(), messageFactory, explore.Config{
		Runs: 3, BudgetPerRun: 2,
		Actions: explore.ActionPolicy{
			DropMessages: true, DuplicateMessages: true, MaxDuplicates: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runs := result.Runs
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want execute/drop/duplicate roots", len(runs))
	}
	want := []explore.DecisionAction{explore.ActionExecute, explore.ActionDrop, explore.ActionDuplicate}
	for index, action := range want {
		if runs[index].Decisions[0].Action != action {
			t.Fatalf("run %d first action = %s, want %s", index+1, runs[index].Decisions[0].Action, action)
		}
	}
	if runs[2].Decisions[0].CreatedEventID == "" {
		t.Fatal("duplicate decision did not record the created event ID")
	}
}

func TestRandomChargesAnExplicitTotalDecisionBudget(t *testing.T) {
	result, err := (explore.Random{}).Explore(context.Background(), twoEventFactory, explore.Config{
		Runs: 4, BudgetPerRun: 2, DecisionBudget: 3, Seed: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.BudgetReached || result.ChargedDecisions != 3 || result.StopReason != "decision_budget_reached" {
		t.Fatalf("unexpected budget result: %#v", result)
	}
	if len(result.Runs) != 2 || len(result.Runs[1].Decisions) != 1 {
		t.Fatalf("last run was not truncated to remaining budget: %#v", result.Runs)
	}
}

func TestConfigRejectsUnboundedDuplication(t *testing.T) {
	err := (explore.Config{
		Runs: 1, BudgetPerRun: 1,
		Actions: explore.ActionPolicy{DuplicateMessages: true},
	}).Validate()
	if err == nil {
		t.Fatal("duplicate policy without a bound was accepted")
	}
}

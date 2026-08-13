package controlexperiment

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type semanticEpisodeTestFixture struct {
	ctx        context.Context
	runtime    RuntimeConfig
	root       controlruntime.Trace
	searchSpec StatelessDFSSpec
	search     StatelessDFSResult
	knowledge  ProtocolKnowledgePack
	riskSpec   semantic.RiskWitnessSpec
	riskResult semantic.RiskWitnessResult
	progress   semantic.RiskWitnessProgress
	hypothesis TestHypothesis
	view       SemanticEpisodeView
}

func TestEpisodeContractsAreCanonicalClonedAndTamperEvident(t *testing.T) {
	fixtureData := newSemanticEpisodeTestFixture(t)
	repeated, err := NewTestHypothesis(
		fixtureData.hypothesis.ID, fixtureData.knowledge, fixtureData.riskSpec,
		fixtureData.hypothesis.Rationale,
	)
	if err != nil || !reflect.DeepEqual(repeated, fixtureData.hypothesis) {
		t.Fatalf("hypothesis construction is not deterministic: repeated=%#v err=%v", repeated, err)
	}
	repeatedView, err := NewSemanticEpisodeView(
		fixtureData.view.ID, fixtureData.hypothesis, fixtureData.knowledge,
		fixtureData.riskSpec, fixtureData.riskResult, fixtureData.search, fixtureData.root,
		EpisodeExposureOpaqueSource, EpisodeFeedbackPublicMechanical, nil, nil, nil, ModelWork{},
	)
	if err != nil || !reflect.DeepEqual(repeatedView, fixtureData.view) {
		t.Fatalf("episode view construction is not deterministic: repeated=%#v err=%v", repeatedView, err)
	}
	ordered := episodeCandidateIDs(fixtureData.view)
	plan, err := NewEpisodePlan(
		"fixture-a1-plan-clone", fixtureData.hypothesis, fixtureData.view,
		fixtureData.riskSpec, ordered,
	)
	if err != nil {
		t.Fatal(err)
	}
	ordered[0] = strings.Repeat("f", 64)
	if plan.OrderedCandidateIDs[0] == ordered[0] ||
		plan.Validate(fixtureData.hypothesis, fixtureData.view, fixtureData.riskSpec) != nil {
		t.Fatal("episode plan retained caller-owned candidate storage")
	}

	tamperedHypothesis := fixtureData.hypothesis
	tamperedHypothesis.KnowledgeDigest = strings.Repeat("d", 64)
	tamperedHypothesis, err = tamperedHypothesis.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tamperedHypothesis.Validate(
		fixtureData.knowledge, fixtureData.riskSpec,
	); err == nil {
		t.Fatal("resealed hypothesis detached from its trusted source was accepted")
	}
	unsupportedKnowledge := fixtureData.knowledge
	unsupportedKnowledge.Risks = cloneProtocolKnowledgeRisks(fixtureData.knowledge.Risks)
	unsupportedKnowledge.Risks[0].AllowedBackendIDs = []string{"different-search-v1"}
	unsupportedKnowledge, err = unsupportedKnowledge.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixtureData.hypothesis.Validate(unsupportedKnowledge, fixtureData.riskSpec); err == nil {
		t.Fatal("hypothesis was accepted by a risk that disallows the selected search backend")
	}
	tamperedView := fixtureData.view
	tamperedView.Candidates = append([]SemanticCandidateRef(nil), fixtureData.view.Candidates...)
	tamperedView.Candidates[0].PathDepth++
	tamperedView, err = tamperedView.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tamperedView.ValidateInitialSources(
		fixtureData.hypothesis, fixtureData.knowledge, fixtureData.riskSpec,
		fixtureData.riskResult, fixtureData.search, fixtureData.root,
	); err == nil {
		t.Fatal("resealed candidate view detached from its DFS WorkItem was accepted")
	}
	tamperedPrior := fixtureData.view
	tamperedPrior.PriorFeedbackDigest = strings.Repeat("a", 64)
	tamperedPrior, err = tamperedPrior.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tamperedPrior.ValidateInitialSources(
		fixtureData.hypothesis, fixtureData.knowledge, fixtureData.riskSpec,
		fixtureData.riskResult, fixtureData.search, fixtureData.root,
	); err == nil {
		t.Fatal("resealed initial view with fabricated prior feedback was accepted")
	}
	for name, policies := range map[string][2]string{
		"exposure": {"unregistered-source-view", EpisodeFeedbackPublicMechanical},
		"feedback": {EpisodeExposureOpaqueSource, "verdict-bearing-feedback"},
	} {
		t.Run("unknown-"+name+"-policy", func(t *testing.T) {
			if _, err := NewSemanticEpisodeView(
				"fixture-a1-untrusted-policy", fixtureData.hypothesis, fixtureData.knowledge,
				fixtureData.riskSpec, fixtureData.riskResult, fixtureData.search, fixtureData.root,
				policies[0], policies[1], nil, nil, nil, ModelWork{},
			); err == nil {
				t.Fatalf("unknown %s policy was accepted", name)
			}
		})
	}
}

func TestEpisodePlanStrictBoundaryAndCompleteCandidatePermutation(t *testing.T) {
	fixtureData := newSemanticEpisodeTestFixture(t)
	base := episodePlanWire(
		fixtureData.hypothesis, fixtureData.view, episodeCandidateIDs(fixtureData.view),
	)
	validJSON, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEpisodePlan(
		validJSON, fixtureData.hypothesis, fixtureData.view, fixtureData.riskSpec,
	); err != nil {
		t.Fatalf("valid strict episode plan was rejected: %v", err)
	}
	for _, field := range []string{
		"action_ids", "decision", "operator_id", "algorithm_id", "fault_envelope",
		"budget", "stop_rule", "oracle_assertion", "expected_bug", "verdict",
	} {
		t.Run(field, func(t *testing.T) {
			withAuthority := append([]byte(nil), validJSON[:len(validJSON)-1]...)
			withAuthority = append(withAuthority, []byte(`,"`+field+`":"forbidden"}`)...)
			if _, err := ParseEpisodePlan(
				withAuthority, fixtureData.hypothesis, fixtureData.view, fixtureData.riskSpec,
			); err == nil {
				t.Fatalf("authority-bearing field %q was accepted", field)
			}
		})
	}
	if _, err := ParseEpisodePlan(
		append(validJSON, []byte(` {}`)...),
		fixtureData.hypothesis, fixtureData.view, fixtureData.riskSpec,
	); err == nil {
		t.Fatal("trailing episode plan JSON was accepted")
	}

	validIDs := episodeCandidateIDs(fixtureData.view)
	invalidOrders := [][]string{
		validIDs[:len(validIDs)-1],
		append(append([]string(nil), validIDs...), validIDs[0]),
		append([]string{validIDs[0]}, validIDs[:len(validIDs)-1]...),
		append([]string{strings.Repeat("e", 64)}, validIDs[1:]...),
	}
	for index, ids := range invalidOrders {
		if _, err := NewEpisodePlan(
			"fixture-a1-invalid-plan", fixtureData.hypothesis, fixtureData.view,
			fixtureData.riskSpec, ids,
		); err == nil {
			t.Fatalf("invalid candidate permutation %d was accepted: %v", index, ids)
		}
	}
	validPlan, err := NewEpisodePlan(
		"fixture-a1-bound-plan", fixtureData.hypothesis, fixtureData.view,
		fixtureData.riskSpec, validIDs,
	)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*EpisodePlan){
		"hypothesis": func(plan *EpisodePlan) { plan.HypothesisDigest = strings.Repeat("a", 64) },
		"view":       func(plan *EpisodePlan) { plan.ViewDigest = strings.Repeat("b", 64) },
		"set":        func(plan *EpisodePlan) { plan.PlanningCandidateSetDigest = strings.Repeat("c", 64) },
		"target":     func(plan *EpisodePlan) { plan.TargetRiskID = "different-risk" },
	} {
		t.Run("stale-"+name, func(t *testing.T) {
			tampered := validPlan
			mutate(&tampered)
			tampered, err = tampered.seal()
			if err != nil {
				t.Fatal(err)
			}
			if err := tampered.Validate(
				fixtureData.hypothesis, fixtureData.view, fixtureData.riskSpec,
			); err == nil {
				t.Fatalf("stale %s binding was accepted", name)
			}
		})
	}
}

func TestEpisodeCompilerUsesAbsoluteSelectedDecisionForNonEmptyRoot(t *testing.T) {
	fixtureData := newSemanticEpisodeTestFixtureWithRootDecisions(t, 1)
	ordered := episodeCandidateIDs(fixtureData.view)
	plan, err := NewEpisodePlan(
		"fixture-a1-nonempty-root-plan", fixtureData.hypothesis, fixtureData.view,
		fixtureData.riskSpec, ordered,
	)
	if err != nil {
		t.Fatal(err)
	}
	selected, policy, err := CompileSemanticEpisodePlan(
		"fixture-a1-nonempty-root-policy", plan, fixtureData.hypothesis,
		fixtureData.knowledge, fixtureData.view, fixtureData.riskSpec,
		fixtureData.riskResult, fixtureData.search, fixtureData.root,
	)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Path.Decision != len(fixtureData.root.Records)+selected.Path.Depth ||
		policy.Rules[len(policy.Rules)-1].Decision != selected.Path.Decision {
		t.Fatalf("episode compiler used a relative decision budget: item=%#v policy=%#v", selected, policy)
	}
	actual := executeEpisodePolicy(t, fixtureData, policy)
	if actual.Digest != selected.ChildPrefixDigest {
		t.Fatalf("non-empty root compiled to wrong exact child: got=%s want=%s", actual.Digest, selected.ChildPrefixDigest)
	}
}

func TestEpisodeRejectReasonTaxonomyIsMechanical(t *testing.T) {
	fixtureData := newSemanticEpisodeTestFixture(t)
	validIDs := episodeCandidateIDs(fixtureData.view)
	base := episodePlanWire(fixtureData.hypothesis, fixtureData.view, validIDs)
	binding := base
	binding.ViewDigest = strings.Repeat("a", 64)
	candidate := base
	candidate.OrderedCandidateIDs = append([]string(nil), validIDs...)
	candidate.OrderedCandidateIDs[len(candidate.OrderedCandidateIDs)-1] = validIDs[0]
	proposals := map[string][]byte{
		EpisodeReasonPlanJSONInvalid:      []byte(`{"schema_version":`),
		EpisodeReasonPlanBindingInvalid:   mustMarshalEpisodePlan(t, binding),
		EpisodeReasonPlanCandidateInvalid: mustMarshalEpisodePlan(t, candidate),
	}
	for wantReason, proposal := range proposals {
		t.Run(wantReason, func(t *testing.T) {
			report, err := NewRejectedEpisodeReport(
				"fixture-a1-reason-report", fixtureData.hypothesis, fixtureData.knowledge,
				fixtureData.view, fixtureData.riskSpec, fixtureData.riskResult,
				fixtureData.search, fixtureData.root, proposal, ModelWork{},
			)
			if err != nil {
				t.Fatal(err)
			}
			if report.PlanningFeedback.ReasonCode != wantReason ||
				report.PlanningFeedback.ProposalDigest != AgentInvocationDigest(proposal) {
				t.Fatalf("unexpected rejection classification: %#v", report.PlanningFeedback)
			}
			if _, err := NewSemanticEpisodeView(
				"fixture-a1-reason-repair", fixtureData.hypothesis, fixtureData.knowledge,
				fixtureData.riskSpec, fixtureData.riskResult, fixtureData.search, fixtureData.root,
				EpisodeExposureOpaqueSource, EpisodeFeedbackPublicMechanical,
				&report, &fixtureData.view, proposal, ModelWork{},
			); err != nil {
				t.Fatalf("mechanical %s rejection could not authorize repair: %v", wantReason, err)
			}
		})
	}
}

func TestEpisodePlannerProjectionUsesExactFieldAllowlists(t *testing.T) {
	fixtureData := newSemanticEpisodeTestFixture(t)
	invalid := []byte(`{"schema_version":`)
	rejected, err := NewRejectedEpisodeReport(
		"fixture-a1-allowlist-rejected", fixtureData.hypothesis, fixtureData.knowledge,
		fixtureData.view, fixtureData.riskSpec, fixtureData.riskResult,
		fixtureData.search, fixtureData.root, invalid, ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONObjectKeys(t, fixtureData.view, map[string]bool{
		"schema_version": true, "id": true, "hypothesis_digest": true,
		"search_algorithm_id": true, "risk_progress": true, "candidates": true,
		"planning_candidate_set_digest": true, "exposure_policy_id": true,
		"feedback_policy_id": true, "digest": true,
	})
	assertJSONObjectKeys(t, fixtureData.view.RiskProgress, map[string]bool{
		"schema_version": true, "spec_digest": true, "risk_id": true,
		"evidence_prefix_digest": true, "satisfied_milestones": true,
		"first_missing_milestone": true, "status": true,
		"source_result_digest": true, "digest": true,
	})
	assertJSONObjectKeys(t, fixtureData.view.Candidates[0], map[string]bool{
		"candidate_id": true, "parent_prefix_decisions": true,
		"path_depth": true, "action_kind": true,
	})
	assertJSONObjectKeys(t, rejected.PlanningFeedback, map[string]bool{
		"schema_version": true, "outcome": true, "reason_code": true,
		"hypothesis_digest": true, "view_digest": true,
		"planning_candidate_set_digest": true, "proposal_digest": true,
		"risk_progress": true, "search_work": true, "model_work": true,
		"digest": true,
	})
}

func TestEpisodeRejectFeedbackRepairCompileExecuteAndReplay(t *testing.T) {
	fixtureData := newSemanticEpisodeTestFixture(t)
	invalidJSON := []byte(`{"schema_version":"consensus-atlas/episode-plan/v1","oracle_assertion":"pass"}`)
	if _, err := ParseEpisodePlan(
		invalidJSON, fixtureData.hypothesis, fixtureData.view, fixtureData.riskSpec,
	); err == nil {
		t.Fatal("invalid plan unexpectedly crossed the strict parser")
	}
	rejected, err := NewRejectedEpisodeReport(
		"fixture-a1-rejected-report", fixtureData.hypothesis, fixtureData.knowledge,
		fixtureData.view, fixtureData.riskSpec, fixtureData.riskResult,
		fixtureData.search, fixtureData.root, invalidJSON, ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	repairedView, err := NewSemanticEpisodeView(
		"fixture-a1-repaired-view", fixtureData.hypothesis, fixtureData.knowledge,
		fixtureData.riskSpec, fixtureData.riskResult, fixtureData.search, fixtureData.root,
		EpisodeExposureOpaqueSource, EpisodeFeedbackPublicMechanical,
		&rejected, &fixtureData.view, invalidJSON, ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if repairedView.PriorFeedbackDigest != rejected.PlanningFeedback.Digest ||
		repairedView.PlanningCandidateSetDigest != fixtureData.view.PlanningCandidateSetDigest ||
		repairedView.Digest == fixtureData.view.Digest {
		t.Fatalf("repair view did not bind the mechanical feedback: %#v", repairedView)
	}
	if err := repairedView.ValidateRepairSources(
		fixtureData.hypothesis, fixtureData.knowledge, fixtureData.view, rejected,
		fixtureData.riskSpec, fixtureData.riskResult, fixtureData.search,
		fixtureData.root, invalidJSON, ModelWork{},
	); err != nil {
		t.Fatalf("repair view is not independently source-verifiable: %v", err)
	}

	ordered := nonCanonicalEpisodeOrder(t, repairedView, fixtureData.search)
	wire, err := json.Marshal(episodePlanWire(fixtureData.hypothesis, repairedView, ordered))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ParseEpisodePlan(wire, fixtureData.hypothesis, repairedView, fixtureData.riskSpec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRejectedEpisodeReport(
		"fixture-a1-false-rejection", fixtureData.hypothesis, fixtureData.knowledge,
		repairedView, fixtureData.riskSpec, fixtureData.riskResult,
		fixtureData.search, fixtureData.root, wire, ModelWork{},
	); err == nil {
		t.Fatal("valid proposal was relabelled as a rejection")
	}
	selected, policy, err := CompileSemanticEpisodePlan(
		"fixture-a1-compiled-plan", plan, fixtureData.hypothesis, fixtureData.knowledge,
		repairedView, fixtureData.riskSpec, fixtureData.riskResult,
		fixtureData.search, fixtureData.root,
	)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Ordinal == fixtureData.search.Items[0].Ordinal {
		t.Fatalf("episode plan did not select a non-canonical trusted WorkItem: %#v", selected)
	}
	actual := executeEpisodePolicy(t, fixtureData, policy)
	if actual.Digest != selected.ChildPrefixDigest {
		t.Fatalf("compiled policy produced the wrong exact trace: got=%s want=%s", actual.Digest, selected.ChildPrefixDigest)
	}
	runtimeConfig, _ := fixtureData.runtime.runtimeConfig()
	if _, err := controlruntime.Replay(fixtureData.ctx, fixture.New(), runtimeConfig, actual); err != nil {
		t.Fatalf("episode trace did not fresh Replay: %v", err)
	}
	recordDigest, err := control.CanonicalDigest(actual.Records[0])
	if err != nil {
		t.Fatal(err)
	}
	riskResult, err := semantic.NewRiskWitnessResult(
		"fixture-a1-executed-risk", fixtureData.riskSpec, actual.ManifestDigest,
		actual.Digest, "fixture-a1-projector", []semantic.RiskWitnessMilestoneEvidence{{
			MilestoneID: "phase-one", Step: 1, Kind: "trace-action", EvidenceDigest: recordDigest,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	executionWork := expectedEpisodeExecutionWork(fixtureData.root, actual, ModelWork{})
	executed, err := NewExecutedEpisodeReport(
		"fixture-a1-executed-report", fixtureData.hypothesis, fixtureData.knowledge,
		repairedView, plan, fixtureData.riskSpec, fixtureData.riskResult, riskResult,
		fixtureData.search, fixtureData.root, actual, wire, executionWork,
		ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	whitespaceWire := append(append([]byte(nil), wire...), '\n')
	whitespaceReport, err := NewExecutedEpisodeReport(
		"fixture-a1-whitespace-report", fixtureData.hypothesis, fixtureData.knowledge,
		repairedView, plan, fixtureData.riskSpec, fixtureData.riskResult, riskResult,
		fixtureData.search, fixtureData.root, actual, whitespaceWire, executionWork,
		ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if whitespaceReport.PlanningFeedback.PlanDigest != executed.PlanningFeedback.PlanDigest ||
		whitespaceReport.PlanningFeedback.ProposalDigest == executed.PlanningFeedback.ProposalDigest {
		t.Fatal("normalized plan identity and exact proposal identity were conflated")
	}
	undercharged := executionWork
	undercharged.Primary.SchedulerDecisions--
	undercharged.Primary.WorkUnits--
	if _, err := NewExecutedEpisodeReport(
		"fixture-a1-undercharged-report", fixtureData.hypothesis, fixtureData.knowledge,
		repairedView, plan, fixtureData.riskSpec, fixtureData.riskResult, riskResult,
		fixtureData.search, fixtureData.root, actual, wire, undercharged,
		ModelWork{},
	); err == nil {
		t.Fatal("undercharged execution work was accepted")
	}
	wrongOrdered := episodeCandidateIDs(repairedView)
	if wrongOrdered[0] == ordered[0] {
		wrongOrdered[0], wrongOrdered[1] = wrongOrdered[1], wrongOrdered[0]
	}
	wrongPlan, err := NewEpisodePlan(
		"fixture-a1-wrong-child-plan", fixtureData.hypothesis, repairedView,
		fixtureData.riskSpec, wrongOrdered,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewExecutedEpisodeReport(
		"fixture-a1-wrong-child-report", fixtureData.hypothesis, fixtureData.knowledge,
		repairedView, wrongPlan, fixtureData.riskSpec, fixtureData.riskResult, riskResult,
		fixtureData.search, fixtureData.root, actual, wire, executionWork,
		ModelWork{},
	); err == nil {
		t.Fatal("valid trace from a different plan candidate was accepted")
	}
	if executed.PlanningFeedback.Outcome != EpisodeOutcomeExecuted ||
		executed.PlanningFeedback.RiskProgress.FirstMissingMilestone != "phase-two" ||
		executed.ValidateExecutedSources(
			fixtureData.hypothesis, fixtureData.knowledge, repairedView, plan,
			fixtureData.riskSpec, fixtureData.riskResult, riskResult,
			fixtureData.search, fixtureData.root, actual, wire,
			ModelWork{},
		) != nil {
		t.Fatalf("executed feedback is not source-bound: %#v", executed)
	}
	assertPlanningFeedbackHasNoPrivateFields(t, executed.PlanningFeedback)
	assertJSONObjectKeys(t, executed.PlanningFeedback, map[string]bool{
		"schema_version": true, "outcome": true, "hypothesis_digest": true,
		"view_digest": true, "planning_candidate_set_digest": true,
		"proposal_digest": true, "plan_digest": true, "selected_candidate_id": true,
		"risk_progress": true, "search_work": true, "execution_work": true,
		"model_work": true, "digest": true,
	})
	var persisted EpisodeReport
	roundTripJSON(t, executed, &persisted)
	if err := persisted.ValidateExecutedSources(
		fixtureData.hypothesis, fixtureData.knowledge, repairedView, plan,
		fixtureData.riskSpec, fixtureData.riskResult, riskResult,
		fixtureData.search, fixtureData.root, actual, wire, ModelWork{},
	); err != nil {
		t.Fatalf("persisted episode report lost source verifiability: %v", err)
	}

	tampered := executed
	tampered.PlanningFeedback.SelectedCandidateID = fixtureData.view.Candidates[1].CandidateID
	tampered.PlanningFeedback, err = tampered.PlanningFeedback.seal()
	if err != nil {
		t.Fatal(err)
	}
	tampered, err = tampered.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tampered.ValidateExecutedSources(
		fixtureData.hypothesis, fixtureData.knowledge, repairedView, plan,
		fixtureData.riskSpec, fixtureData.riskResult, riskResult,
		fixtureData.search, fixtureData.root, actual, wire,
		ModelWork{},
	); err == nil {
		t.Fatal("resealed planning feedback detached from its trusted WorkItem was accepted")
	}
	tamperedRejection := rejected
	tamperedRejection.PlanningFeedback.ReasonCode = EpisodeReasonPlanBindingInvalid
	tamperedRejection.PlanningFeedback, err = tamperedRejection.PlanningFeedback.seal()
	if err != nil {
		t.Fatal(err)
	}
	tamperedRejection, err = tamperedRejection.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tamperedRejection.ValidateRejectedSources(
		fixtureData.hypothesis, fixtureData.knowledge, fixtureData.view,
		fixtureData.riskSpec, fixtureData.riskResult, fixtureData.search,
		fixtureData.root, invalidJSON, ModelWork{},
	); err == nil {
		t.Fatal("caller-authored rejection reason was accepted")
	}
	tamperedModelWork := rejected
	tamperedModelWork.PlanningFeedback.ModelWork = ModelWork{Calls: 1}
	tamperedModelWork.PlanningFeedback, err = tamperedModelWork.PlanningFeedback.seal()
	if err != nil {
		t.Fatal(err)
	}
	tamperedModelWork, err = tamperedModelWork.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tamperedModelWork.ValidateRejectedSources(
		fixtureData.hypothesis, fixtureData.knowledge, fixtureData.view,
		fixtureData.riskSpec, fixtureData.riskResult, fixtureData.search,
		fixtureData.root, invalidJSON, ModelWork{},
	); err == nil {
		t.Fatal("rejection feedback detached from trusted model work was accepted")
	}
	tamperedExecutedWork := executed
	tamperedExecutedWork.PlanningFeedback.ModelWork = ModelWork{Calls: 1}
	work := *tamperedExecutedWork.PlanningFeedback.ExecutionWork
	work.Model = ModelWork{Calls: 1}
	tamperedExecutedWork.PlanningFeedback.ExecutionWork = &work
	tamperedExecutedWork.PlanningFeedback, err = tamperedExecutedWork.PlanningFeedback.seal()
	if err != nil {
		t.Fatal(err)
	}
	tamperedExecutedWork, err = tamperedExecutedWork.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tamperedExecutedWork.ValidateExecutedSources(
		fixtureData.hypothesis, fixtureData.knowledge, repairedView, plan,
		fixtureData.riskSpec, fixtureData.riskResult, riskResult,
		fixtureData.search, fixtureData.root, actual, wire, ModelWork{},
	); err == nil {
		t.Fatal("execution feedback detached from trusted model work was accepted")
	}
	if _, err := NewSemanticEpisodeView(
		"fixture-a1-unbound-repair", fixtureData.hypothesis, fixtureData.knowledge,
		fixtureData.riskSpec, fixtureData.riskResult, fixtureData.search, fixtureData.root,
		EpisodeExposureOpaqueSource, EpisodeFeedbackPublicMechanical,
		&rejected, &fixtureData.view, nil, ModelWork{},
	); err == nil {
		t.Fatal("prior rejection without its exact proposal evidence was accepted")
	}
}

func newSemanticEpisodeTestFixture(t *testing.T) semanticEpisodeTestFixture {
	return newSemanticEpisodeTestFixtureWithRootDecisions(t, 0)
}

func newSemanticEpisodeTestFixtureWithRootDecisions(
	t *testing.T,
	rootDecisions int,
) semanticEpisodeTestFixture {
	t.Helper()
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "61312d73656d616e7469632d657069736f6465", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	if rootDecisions > 0 {
		config, err := runtimeConfig.runtimeConfig()
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := controlruntime.Replay(ctx, fixture.New(), config, root)
		if err != nil {
			t.Fatal(err)
		}
		for decision := 0; decision < rootDecisions; decision++ {
			actions, err := runtime.EnabledActions(ctx)
			if err != nil || len(actions) == 0 {
				t.Fatalf("cannot materialize fixture root decision %d: actions=%v err=%v", decision+1, actions, err)
			}
			if _, err := runtime.Select(ctx, actions[0].ID); err != nil {
				t.Fatal(err)
			}
		}
		root, err = runtime.Trace()
		if err != nil {
			t.Fatal(err)
		}
	}
	searchSpec, err := NewStatelessDFSSpec(
		"fixture-a1-episode-search", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}, 1, 8, 400,
	)
	if err != nil {
		t.Fatal(err)
	}
	search, err := ExploreBoundedStatelessDFS(
		ctx, searchSpec, root, func() (control.Adapter, error) { return fixture.New(), nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Items) < 2 {
		t.Fatalf("fixture needs at least two global candidates: %#v", search)
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-a1-episode-knowledge", Family: "fixture-cft", Protocol: "fixture",
		Knowledge: []KnowledgeStatement{{
			ID: "natural-progress", Text: "Natural temporal progress may expose different protocol phases.",
		}},
		Risks: []ProtocolRisk{{
			ID: "phase-transition", Summary: "Explore a two-phase transition.",
			RequiredCapabilities: []string{"fixture-control"},
			RequiredActions:      []control.ActionKind{control.ActionFireTemporal},
			AllowedBackendIDs:    []string{StatelessSearchBoundedDepthFirst},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	riskSpec, err := semantic.NewRiskWitnessSpec(
		"fixture-a1-risk-spec", "fixture-cft", "phase-transition",
		[]string{"phase-one", "phase-two"},
		[]semantic.RiskWitnessOrder{{Before: "phase-one", After: "phase-two"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	riskResult, err := semantic.NewRiskWitnessResult(
		"fixture-a1-root-risk", riskSpec, root.ManifestDigest, root.Digest,
		"fixture-a1-projector", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := semantic.NewRiskWitnessProgress(riskSpec, riskResult)
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"fixture-a1-hypothesis", knowledge, riskSpec,
		"Explore whether natural progress reaches the frozen two-phase relation.",
	)
	if err != nil {
		t.Fatal(err)
	}
	view, err := NewSemanticEpisodeView(
		"fixture-a1-episode-view", hypothesis, knowledge, riskSpec, riskResult, search, root,
		EpisodeExposureOpaqueSource, EpisodeFeedbackPublicMechanical, nil, nil, nil, ModelWork{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return semanticEpisodeTestFixture{
		ctx: ctx, runtime: runtimeConfig, root: root, searchSpec: searchSpec, search: search,
		knowledge: knowledge, riskSpec: riskSpec, riskResult: riskResult, progress: progress,
		hypothesis: hypothesis, view: view,
	}
}

func episodePlanWire(
	hypothesis TestHypothesis,
	view SemanticEpisodeView,
	ordered []string,
) EpisodePlan {
	return EpisodePlan{
		SchemaVersion: EpisodePlanSchemaVersion, ID: "fixture-a1-wire-plan",
		HypothesisID: hypothesis.ID, HypothesisDigest: hypothesis.Digest,
		ViewDigest: view.Digest, PlanningCandidateSetDigest: view.PlanningCandidateSetDigest,
		TargetRiskID: hypothesis.RiskID, OrderedCandidateIDs: append([]string(nil), ordered...),
	}
}

func mustMarshalEpisodePlan(t *testing.T, plan EpisodePlan) []byte {
	t.Helper()
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func episodeCandidateIDs(view SemanticEpisodeView) []string {
	result := make([]string, len(view.Candidates))
	for index, candidate := range view.Candidates {
		result[index] = candidate.CandidateID
	}
	return result
}

func nonCanonicalEpisodeOrder(
	t *testing.T,
	view SemanticEpisodeView,
	search StatelessDFSResult,
) []string {
	t.Helper()
	all := episodeCandidateIDs(view)
	selected := ""
	for _, candidateID := range all {
		item, err := resolveEpisodeCandidate(candidateID, search)
		if err != nil {
			t.Fatal(err)
		}
		if item.Ordinal != search.Items[0].Ordinal {
			selected = candidateID
			break
		}
	}
	if selected == "" {
		t.Fatal("fixture cannot select a non-canonical candidate")
	}
	ordered := []string{selected}
	for _, candidateID := range all {
		if candidateID != selected {
			ordered = append(ordered, candidateID)
		}
	}
	return ordered
}

func executeEpisodePolicy(
	t *testing.T,
	fixtureData semanticEpisodeTestFixture,
	policy Policy,
) controlruntime.Trace {
	t.Helper()
	runtimeConfig, err := fixtureData.runtime.runtimeConfig()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.Replay(
		fixtureData.ctx, fixture.New(), runtimeConfig, fixtureData.root,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range policy.Rules[len(fixtureData.root.Records):] {
		if _, err := runtime.Select(fixtureData.ctx, rule.ActionID); err != nil {
			t.Fatal(err)
		}
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	return trace
}

func assertPlanningFeedbackHasNoPrivateFields(t *testing.T, feedback PlanningFeedback) {
	t.Helper()
	encoded, err := json.Marshal(feedback)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"candidate_control_identity": true, "sut_variant_identity": true,
		"source_variant": true, "binary_identity": true, "root_cause": true,
		"private_monitor": true, "oracle": true, "verdict": true,
	}
	var inspect func(any)
	inspect = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if forbidden[key] {
					t.Fatalf("planner feedback leaked private field %q", key)
				}
				inspect(child)
			}
		case []any:
			for _, child := range typed {
				inspect(child)
			}
		}
	}
	inspect(value)
}

func assertJSONObjectKeys(t *testing.T, value any, allowed map[string]bool) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if len(object) != len(allowed) {
		t.Fatalf("planner projection keys changed: got=%v want=%v", reflect.ValueOf(object).MapKeys(), allowed)
	}
	for key := range object {
		if !allowed[key] {
			t.Fatalf("planner projection contains non-allowlisted key %q", key)
		}
	}
}

func roundTripJSON(t *testing.T, source any, target any) {
	t.Helper()
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatal(err)
	}
}

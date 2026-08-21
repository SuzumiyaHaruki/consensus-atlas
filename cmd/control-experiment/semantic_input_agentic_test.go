package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

const (
	etcdraftAgenticTestInputPath  = "../../plans/agent/etcdraft-agentic-calibration-v1.json"
	omnipaxosAgenticTestInputPath = "../../plans/agent/omnipaxos-agentic-calibration-v1.json"
)

func TestEtcdraftAgenticAuthoringHasNoSeededRiskOrHypothesis(t *testing.T) {
	knowledge, experiment, workload, err := loadEtcdraftAgenticAuthoringSource(
		etcdraftAgenticTestInputPath,
	)
	if err != nil || knowledge.ValidateAgentMaterials() != nil || len(knowledge.Risks) != 0 ||
		knowledge.Protocol != "etcdraft" || knowledge.Family != "raft" ||
		knowledge.TargetDossier == nil || len(knowledge.Properties) < 6 ||
		len(knowledge.IssuePatterns) < 6 || len(knowledge.TargetDossier.BlindSpots) < 3 ||
		experiment.validateAgentic() != nil || workload.Validate() != nil {
		t.Fatalf("etcd/raft A9e1 materials did not load: %#v/%#v/%#v/%v",
			knowledge, experiment, workload, err)
	}

	withHypothesis := agenticInputWithExtraField(
		t, etcdraftAgenticTestInputPath, "test_hypothesis", map[string]any{},
	)
	if _, _, _, err := loadEtcdraftAgenticAuthoringSource(withHypothesis); err == nil ||
		!strings.Contains(err.Error(), "INPUT_FILE_INVALID") {
		t.Fatalf("empty pre-seeded etcd/raft hypothesis was accepted: %v", err)
	}

	var source etcdraftAgenticAuthoringSource
	if err := readStrictJSONFile(etcdraftAgenticTestInputPath, etcdraftSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Knowledge.Risks = []controlexperiment.ProtocolRisk{seededAgenticRisk()}
	withRisk := writeAgenticInputFixture(t, source)
	if _, _, _, err := loadEtcdraftAgenticAuthoringSource(withRisk); err == nil ||
		!strings.Contains(err.Error(), "INPUT_MATERIALS_INVALID") {
		t.Fatalf("pre-seeded etcd/raft Risk was accepted: %v", err)
	}
}

func TestEtcdraftAgenticNodeCountFlowsIntoQualificationAndRoot(t *testing.T) {
	for _, nodeCount := range []int{3, 5} {
		t.Run(fmt.Sprintf("nodes-%d", nodeCount), func(t *testing.T) {
			var source etcdraftAgenticAuthoringSource
			if err := readStrictJSONFile(etcdraftAgenticTestInputPath, etcdraftSemanticInputLimit, &source); err != nil {
				t.Fatal(err)
			}
			source.Experiment.AdapterConfig = etcdraftv2.Config{
				NodeCount: nodeCount, ElectionTick: 7, HeartbeatTick: 2,
			}
			source.Experiment.RootMode = agenticRootBootstrap
			path := writeAgenticInputFixture(t, source)
			_, experiment, workload, err := loadEtcdraftAgenticAuthoringSource(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(30*time.Second))
			defer cancel()
			execution, root, err := prepareEtcdraftAgenticExecutionInputs(ctx, workload, experiment)
			if err != nil {
				t.Fatal(err)
			}
			if err := execution.preparation.Validate(); err != nil {
				t.Fatalf("etcd/raft bootstrap preparation ledger invalid: %v", err)
			}
			want := agenticTestNodeIDs(nodeCount)
			if !reflect.DeepEqual(execution.qualification.Manifest.Nodes, want) {
				t.Fatalf("qualified nodes = %v, want %v", execution.qualification.Manifest.Nodes, want)
			}
			if root.ManifestDigest != execution.admission.ManifestDigest ||
				execution.qualification.Qualification.ManifestDigest != execution.admission.ManifestDigest {
				t.Fatalf("%d-node identity drift: root=%s admission=%s qualification=%s",
					nodeCount, root.ManifestDigest, execution.admission.ManifestDigest,
					execution.qualification.Qualification.ManifestDigest)
			}
			for _, record := range root.Records {
				if record.Action.Kind != control.ActionCompleteEffect {
					t.Fatalf("etcd/raft bootstrap root contains protocol progress: %s", record.Action.Kind)
				}
			}
			assertBootstrapRootOracleClean(t, root, targetoracles.EtcdraftV2Registry())
			assertBootstrapRootReplay(t, ctx, root, experiment.Runtime, func() (control.Adapter, error) {
				return etcdraftv2.NewWithConfig(experiment.AdapterConfig)
			})
		})
	}
}

func TestEtcdraftAgenticMissingMembershipUsesThreeNodeDefault(t *testing.T) {
	var source etcdraftAgenticAuthoringSource
	if err := readStrictJSONFile(etcdraftAgenticTestInputPath, etcdraftSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Experiment.AdapterConfig = etcdraftv2.Config{}
	path := writeAgenticInputFixture(t, source)
	_, experiment, _, err := loadEtcdraftAgenticAuthoringSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(experiment.AdapterConfig, etcdraftv2.ThreeNodeConfig()) {
		t.Fatalf("missing etcd/raft membership resolved to %#v", experiment.AdapterConfig)
	}
}

func TestEtcdraftAgenticOverridesBindResolvedEffectiveInput(t *testing.T) {
	_, base, _, baseDigest, err := loadEtcdraftAgenticAuthoringSourceResolved(
		etcdraftAgenticTestInputPath, agenticInputOverrides{},
	)
	if err != nil {
		t.Fatal(err)
	}
	overrides := agenticInputOverrides{NodeCount: 5, RootMode: agenticRootBootstrap}
	_, resolved, _, resolvedDigest, err := loadEtcdraftAgenticAuthoringSourceResolved(
		etcdraftAgenticTestInputPath, overrides,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, repeated, _, repeatedDigest, err := loadEtcdraftAgenticAuthoringSourceResolved(
		etcdraftAgenticTestInputPath, overrides,
	)
	if err != nil || resolved.AdapterConfig.NodeCount != 5 || len(resolved.AdapterConfig.Nodes) != 0 ||
		resolved.RootMode != agenticRootBootstrap || baseDigest == resolvedDigest ||
		resolvedDigest != repeatedDigest || !reflect.DeepEqual(resolved, repeated) ||
		base.AdapterConfig.NodeCount != 3 || base.RootMode != agenticRootWorkloadReady {
		t.Fatalf("etcd/raft effective override identity drifted: base=%#v/%s resolved=%#v/%s repeated=%#v/%s err=%v",
			base, baseDigest, resolved, resolvedDigest, repeated, repeatedDigest, err)
	}
}

func TestOmnipaxosAgenticAuthoringHasNoSeededRiskOrHypothesis(t *testing.T) {
	knowledge, experiment, workload, err := loadOmnipaxosAgenticAuthoringSource(
		omnipaxosAgenticTestInputPath,
	)
	if err != nil || knowledge.ValidateAgentMaterials() != nil || len(knowledge.Risks) != 0 ||
		knowledge.Protocol != "omnipaxos" || knowledge.Family != "paxos" ||
		knowledge.TargetDossier == nil || len(knowledge.Properties) < 6 ||
		len(knowledge.IssuePatterns) < 6 || len(knowledge.TargetDossier.BlindSpots) < 3 ||
		experiment.validate() != nil || workload.Validate() != nil {
		t.Fatalf("OmniPaxos A9e1 materials did not load: %#v/%#v/%#v/%v",
			knowledge, experiment, workload, err)
	}

	withHypothesis := agenticInputWithExtraField(
		t, omnipaxosAgenticTestInputPath, "test_hypothesis", map[string]any{},
	)
	if _, _, _, err := loadOmnipaxosAgenticAuthoringSource(withHypothesis); err == nil ||
		!strings.Contains(err.Error(), "INPUT_FILE_INVALID") {
		t.Fatalf("empty pre-seeded OmniPaxos hypothesis was accepted: %v", err)
	}

	var source omnipaxosAgenticAuthoringSource
	if err := readStrictJSONFile(omnipaxosAgenticTestInputPath, omnipaxosSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Knowledge.Risks = []controlexperiment.ProtocolRisk{seededAgenticRisk()}
	withRisk := writeAgenticInputFixture(t, source)
	if _, _, _, err := loadOmnipaxosAgenticAuthoringSource(withRisk); err == nil ||
		!strings.Contains(err.Error(), "INPUT_MATERIALS_INVALID") {
		t.Fatalf("pre-seeded OmniPaxos Risk was accepted: %v", err)
	}
}

func TestOmnipaxosAgenticNodeCountFlowsIntoQualificationAndRoot(t *testing.T) {
	workerPath := buildOmnipaxosScenarioWorker(t)
	for _, nodeCount := range []int{3, 5} {
		t.Run(fmt.Sprintf("nodes-%d", nodeCount), func(t *testing.T) {
			var source omnipaxosAgenticAuthoringSource
			if err := readStrictJSONFile(omnipaxosAgenticTestInputPath, omnipaxosSemanticInputLimit, &source); err != nil {
				t.Fatal(err)
			}
			source.Experiment.AdapterConfig = omnipaxosv2.Config{NodeCount: nodeCount}
			source.Experiment.RootMode = agenticRootBootstrap
			path := writeAgenticInputFixture(t, source)
			ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(90*time.Second))
			defer cancel()
			inputs, err := prepareOmnipaxosAgenticEpisode(ctx, workerPath, path)
			if err != nil {
				t.Fatal(err)
			}
			if err := inputs.Preparation.Validate(); err != nil {
				t.Fatalf("OmniPaxos bootstrap preparation ledger invalid: %v", err)
			}
			want := agenticTestNodeIDs(nodeCount)
			if !reflect.DeepEqual(inputs.Qualification.Bundle.Manifest.Nodes, want) {
				t.Fatalf("qualified nodes = %v, want %v", inputs.Qualification.Bundle.Manifest.Nodes, want)
			}
			if inputs.Root.ManifestDigest != inputs.Qualification.Admission.ManifestDigest ||
				inputs.Qualification.Bundle.Qualification.ManifestDigest != inputs.Qualification.Admission.ManifestDigest {
				t.Fatalf("%d-node OmniPaxos identity drift: root=%s admission=%s qualification=%s",
					nodeCount, inputs.Root.ManifestDigest, inputs.Qualification.Admission.ManifestDigest,
					inputs.Qualification.Bundle.Qualification.ManifestDigest)
			}
			if len(inputs.Root.Records) != 0 {
				t.Fatalf("OmniPaxos bootstrap root advanced protocol state: %d decisions", len(inputs.Root.Records))
			}
			assertBootstrapRootOracleClean(t, inputs.Root, targetoracles.OmnipaxosV2Registry())
			assertBootstrapRootReplay(t, ctx, inputs.Root, source.Experiment.Runtime, func() (control.Adapter, error) {
				return omnipaxosv2.New(source.Experiment.adapterConfig(workerPath))
			})
		})
	}
}

func TestOmnipaxosAgenticOverridesBindResolvedEffectiveInput(t *testing.T) {
	_, base, _, baseDigest, err := loadOmnipaxosAgenticAuthoringSourceResolved(
		omnipaxosAgenticTestInputPath, agenticInputOverrides{},
	)
	if err != nil {
		t.Fatal(err)
	}
	overrides := agenticInputOverrides{NodeCount: 5, RootMode: agenticRootBootstrap}
	_, resolved, _, resolvedDigest, err := loadOmnipaxosAgenticAuthoringSourceResolved(
		omnipaxosAgenticTestInputPath, overrides,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, repeated, _, repeatedDigest, err := loadOmnipaxosAgenticAuthoringSourceResolved(
		omnipaxosAgenticTestInputPath, overrides,
	)
	if err != nil || resolved.AdapterConfig.NodeCount != 5 ||
		resolved.RootMode != agenticRootBootstrap || baseDigest == resolvedDigest ||
		resolvedDigest != repeatedDigest || !reflect.DeepEqual(resolved, repeated) ||
		base.AdapterConfig.ResolvedNodeCount() != 3 || base.RootMode != agenticRootWorkloadReady {
		t.Fatalf("OmniPaxos effective override identity drifted: base=%#v/%s resolved=%#v/%s repeated=%#v/%s err=%v",
			base, baseDigest, resolved, resolvedDigest, repeated, repeatedDigest, err)
	}
}

func TestEtcdraftBootstrapScenarioEstablishesCoordinationBeforeInvoke(t *testing.T) {
	for _, nodeCount := range []int{3, 5} {
		t.Run(fmt.Sprintf("nodes-%d", nodeCount), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(
				context.Background(), controlExperimentTestTimeout(120*time.Second),
			)
			defer cancel()
			inputs, err := prepareEtcdraftAgenticEpisodeWithOverrides(
				ctx, "", etcdraftAgenticTestInputPath, fixtureOpenRouterIntentClient(),
				agenticInputOverrides{NodeCount: nodeCount, RootMode: agenticRootBootstrap},
			)
			if err != nil {
				t.Fatal(err)
			}
			target, err := newEtcdraftAgenticEpisodeTarget(inputs)
			if err != nil {
				t.Fatal(err)
			}
			assertBootstrapScenarioEstablishesCoordination(
				t, ctx, target, etcdraftAlternateQuorumRiskCandidate(), "etcdraft", nodeCount,
			)
		})
	}
}

func TestOmnipaxosBootstrapScenarioEstablishesCoordinationBeforeInvoke(t *testing.T) {
	workerPath := buildOmnipaxosScenarioWorker(t)
	for _, nodeCount := range []int{3, 5} {
		t.Run(fmt.Sprintf("nodes-%d", nodeCount), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(
				context.Background(), controlExperimentTestTimeout(180*time.Second),
			)
			defer cancel()
			inputs, err := prepareOmnipaxosAgenticEpisodeWithOverrides(
				ctx, workerPath, omnipaxosAgenticTestInputPath,
				agenticInputOverrides{NodeCount: nodeCount, RootMode: agenticRootBootstrap},
			)
			if err != nil {
				t.Fatal(err)
			}
			target, err := newOmnipaxosAgenticEpisodeTarget(inputs)
			if err != nil {
				t.Fatal(err)
			}
			existing, _, err := loadExistingRiskInput(
				"../../plans/agent/omnipaxos-message-loss-risk-v2.json", target,
			)
			if err != nil || existing == nil {
				t.Fatalf("load executable OmniPaxos bootstrap Risk: %#v/%v", existing, err)
			}
			assertBootstrapScenarioEstablishesCoordination(
				t, ctx, target, existing.Candidate, "omnipaxos", nodeCount,
			)
		})
	}
}

func assertBootstrapScenarioEstablishesCoordination(
	t *testing.T,
	ctx context.Context,
	target agenticEpisodeTarget,
	candidate controlexperiment.RiskCandidate,
	idPrefix string,
	nodeCount int,
) {
	t.Helper()
	assessment, err := controlexperiment.AssessRiskCandidateForTarget(
		target.Knowledge, candidate, target.ObservationProjector.Capabilities(),
		target.Surface.Capabilities.ComposableActions, &target.Surface,
	)
	if err != nil || !assessment.Qualification.Qualified {
		t.Fatalf("bootstrap Risk was not executable: %#v/%v", assessment, err)
	}
	risk, err := controlexperiment.BuildScenarioRiskHypothesis(
		target.Knowledge, assessment, target.ObservationProjector.Capabilities(),
		target.Surface.Capabilities.ComposableActions, &target.Surface,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector, err := controlexperiment.NewLinearObservationRiskProjector(
		risk.Spec, risk.Predicates, target.ObservationProjector,
	)
	if err != nil {
		t.Fatal(err)
	}
	core, err := target.ScenarioInputs(risk, projector)
	if err != nil {
		t.Fatal(err)
	}
	core.RootID = fmt.Sprintf("%s-bootstrap-%d", idPrefix, nodeCount)
	core.TargetSurface = &target.Surface
	coordinationSeen := false
	invokePlanned := false
	preferredNode := control.NodeID("n1")
	var planningLog []string
	decisionBudget := 64
	if idPrefix == "omnipaxos" {
		decisionBudget = 128
	}
	result, err := runScenarioEpisodeCore(
		ctx, core, 8, 1, decisionBudget,
		func(_ context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			coordination := view.Semantics.Coordination
			if coordination == nil {
				t.Fatal("bootstrap feedback omitted trusted coordination status")
			}
			intent := view.AvailableIntents[0]
			step := controlexperiment.ScenarioStep{ID: "start-election", Selector: controlexperiment.FrontierActionSelector{}}
			switch coordination.Status {
			case controlexperiment.ConsensusCoordinatorAbsent,
				controlexperiment.ConsensusCoordinatorAmbiguous:
				if len(coordination.ElectionProgress.CandidateNodes) > 0 {
					preferredNode = coordination.ElectionProgress.CandidateNodes[0]
				}
				step.Selector.ActionID = bootstrapCoordinationAction(
					view.Frontier.Actions, preferredNode,
					coordination.ElectionProgress.TermOrBallotChanged,
				)
				if step.Selector.ActionID == "" {
					t.Fatalf("coordination absent without causal progress for %s: %#v",
						preferredNode, view.Frontier.Actions)
				}
			case controlexperiment.ConsensusCoordinatorPresent:
				if coordination.CoordinatorNode == "" {
					t.Fatalf("present coordinator had no identity: %#v", coordination)
				}
				coordinationSeen = true
				preferredNode = coordination.CoordinatorNode
				if coordination.InvokeReady {
					invokePlanned = true
					step = controlexperiment.ScenarioStep{
						ID:       "invoke-after-coordination",
						Selector: controlexperiment.FrontierActionSelector{Kind: control.ActionInvoke},
					}
				} else {
					step.Selector.ActionID = bootstrapCoordinationAction(
						view.Frontier.Actions, preferredNode, true,
					)
					if step.Selector.ActionID == "" {
						t.Fatalf("coordinator %s was not Invoke-ready and had no causal action: %#v",
							preferredNode, view.Frontier.Actions)
					}
				}
			default:
				t.Fatalf("unexpected coordination status: %#v", coordination)
			}
			planningLog = append(planningLog, fmt.Sprintf(
				"%s ready=%t candidates=%v action=%s",
				coordination.Status, coordination.InvokeReady,
				coordination.ElectionProgress.CandidateNodes,
				bootstrapActionSummary(view.Frontier.Actions, step.Selector.ActionID, step.Selector.Kind),
			))
			encoded, marshalErr := json.Marshal(controlexperiment.ScenarioInvestigationProposal{
				Intent: intent,
				Plan: controlexperiment.ScenarioPlan{
					ID: fmt.Sprintf("%s-bootstrap-plan", idPrefix), Steps: []controlexperiment.ScenarioStep{step},
				},
			})
			return encoded, controlexperiment.ModelWork{
				Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5,
			}, marshalErr
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !coordinationSeen || !invokePlanned || result.Agent.Execution == nil {
		t.Fatalf("bootstrap path did not establish coordination and Invoke: calls=%d decisions=%d log=%v",
			len(result.Agent.Attempts), result.Agent.DecisionsUsed, planningLog)
	}
	foundInvoke := false
	for _, record := range result.Agent.Execution.FinalTrace.Records {
		if record.Action.Kind == control.ActionInvoke {
			foundInvoke = true
			break
		}
	}
	if !foundInvoke {
		t.Fatalf("bootstrap execution did not materialize Invoke: %#v", result.Agent.Execution)
	}
}

func bootstrapActionSummary(
	actions []controlexperiment.FrontierActionRef,
	actionID control.ActionID,
	kind control.ActionKind,
) string {
	if actionID == "" {
		return fmt.Sprintf("kind:%s", kind)
	}
	for _, action := range actions {
		if action.ActionID == actionID {
			return fmt.Sprintf("%s:%s node=%s owner=%s message=%s->%s type=%s metadata=%v",
				action.ActionID, action.Kind, action.Node.Node, action.Owner.Node,
				action.MessageSource.Node, action.MessageTarget, action.MessageTypeHint,
				action.MessageMetadata)
		}
	}
	return string(actionID)
}

func bootstrapCoordinationAction(
	actions []controlexperiment.FrontierActionRef,
	preferredNode control.NodeID,
	progressStarted bool,
) control.ActionID {
	// The OmniPaxos semantic surface exposes BLE ballot metadata. A zero-model
	// planner should be able to complete one participant's request/reply round
	// without relying on protocol internals or a pre-elected coordinator.
	for _, messageType := range []string{"ble/heartbeat-reply", "ble/heartbeat-request"} {
		for _, action := range actions {
			if action.Kind != control.ActionDeliverMessage || action.MessageTypeHint != messageType {
				continue
			}
			if messageType == "ble/heartbeat-reply" && action.MessageTarget == preferredNode ||
				messageType == "ble/heartbeat-request" && action.MessageSource.Node == preferredNode {
				return action.ActionID
			}
		}
	}
	// If the selected participant has no remaining round traffic, disseminate
	// the highest ballot visible in the same trusted frontier.
	var highestBallot controlexperiment.FrontierActionRef
	var highestNumber, highestPriority, highestPID uint64
	for _, action := range actions {
		if action.Kind != control.ActionDeliverMessage ||
			action.MessageTypeHint != "ble/heartbeat-reply" {
			continue
		}
		number, numberErr := strconv.ParseUint(action.MessageMetadata["ballot_number"], 10, 32)
		priority, priorityErr := strconv.ParseUint(action.MessageMetadata["ballot_priority"], 10, 32)
		pid, pidErr := strconv.ParseUint(action.MessageMetadata["ballot_pid"], 10, 64)
		if numberErr != nil || priorityErr != nil || pidErr != nil {
			continue
		}
		if highestBallot.ActionID == "" || number > highestNumber ||
			number == highestNumber && priority > highestPriority ||
			number == highestNumber && priority == highestPriority && pid > highestPID {
			highestBallot, highestNumber, highestPriority, highestPID = action, number, priority, pid
		}
	}
	if highestBallot.ActionID != "" {
		return highestBallot.ActionID
	}
	if progressStarted {
		for _, kind := range []control.ActionKind{
			control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionFireTemporal,
		} {
			for _, action := range actions {
				if action.Kind == kind && bootstrapActionTouchesNode(action, preferredNode) {
					return action.ActionID
				}
			}
		}
	}
	for _, action := range actions {
		if action.Kind == control.ActionFireTemporal && action.Node.Node == preferredNode {
			return action.ActionID
		}
	}
	for _, kind := range []control.ActionKind{
		control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionFireTemporal,
	} {
		for _, action := range actions {
			if action.Kind == kind {
				return action.ActionID
			}
		}
	}
	return ""
}

func bootstrapActionTouchesNode(
	action controlexperiment.FrontierActionRef,
	node control.NodeID,
) bool {
	return action.Node.Node == node || action.Owner.Node == node ||
		action.MessageSource.Node == node || action.MessageTarget == node
}

func TestEtcdraftScenarioPreparerMaterializesInvokeAfterElection(t *testing.T) {
	_, experiment, workload, err := loadEtcdraftAgenticAuthoringSource(etcdraftAgenticTestInputPath)
	if err != nil || experiment.RootMode != agenticRootWorkloadReady {
		t.Fatalf("load workload-ready etcd/raft input: mode=%s err=%v", experiment.RootMode, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(30*time.Second))
	defer cancel()
	_, root, err := prepareEtcdraftAgenticExecutionInputs(ctx, workload, experiment)
	if err != nil {
		t.Fatal(err)
	}
	assertScenarioPreparerRecreatesInvoke(t, ctx, root, experiment.Runtime,
		func() (control.Adapter, error) {
			return etcdraftv2.NewWithConfig(experiment.AdapterConfig)
		}, newEtcdraftScenarioActionPreparer(workload))
}

func TestOmnipaxosScenarioPreparerMaterializesInvokeAfterCoordinator(t *testing.T) {
	workerPath := buildOmnipaxosScenarioWorker(t)
	knowledge, experiment, workload, err := loadOmnipaxosAgenticAuthoringSource(
		omnipaxosAgenticTestInputPath,
	)
	if err != nil || knowledge.ValidateAgentMaterials() != nil ||
		experiment.RootMode != agenticRootWorkloadReady {
		t.Fatalf("load workload-ready OmniPaxos input: mode=%s err=%v", experiment.RootMode, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(90*time.Second))
	defer cancel()
	root, err := buildOmnipaxosScenarioRoot(ctx, workerPath, experiment, workload)
	if err != nil {
		t.Fatal(err)
	}
	assertScenarioPreparerRecreatesInvoke(t, ctx, root, experiment.Runtime,
		func() (control.Adapter, error) {
			return omnipaxosv2.New(experiment.adapterConfig(workerPath))
		}, newOmnipaxosScenarioActionPreparer(workload))
}

func assertScenarioPreparerRecreatesInvoke(
	t *testing.T,
	ctx context.Context,
	root controlruntime.Trace,
	config controlexperiment.RuntimeConfig,
	adapterFactory func() (control.Adapter, error),
	preparer controlexperiment.ScenarioActionPreparer,
) {
	t.Helper()
	if len(root.Records) == 0 || root.Records[len(root.Records)-1].Action.Kind != control.ActionInvoke {
		t.Fatalf("workload-ready root does not end at Invoke: %d records", len(root.Records))
	}
	prefix, err := controlexperiment.ExecutionTracePrefix(root, len(root.Records)-1)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := hex.DecodeString(config.SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := adapterFactory()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.Replay(ctx, adapter, controlruntime.Config{
		Seed: seed, ClockError: config.ClockError, MaxClones: config.MaxClones,
	}, prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	actionID, prepared, err := preparer(
		ctx, controlexperiment.FrontierActionSelector{Kind: control.ActionInvoke}, prefix, runtime,
	)
	if err != nil || !prepared {
		t.Fatalf("trusted Invoke preparation failed: id=%s prepared=%t err=%v", actionID, prepared, err)
	}
	if _, err := runtime.Select(ctx, actionID); err != nil {
		t.Fatal(err)
	}
	actual, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if actual.Digest != root.Digest {
		t.Fatalf("prepared Invoke did not recreate root: got %s want %s", actual.Digest, root.Digest)
	}
}

func agenticTestNodeIDs(nodeCount int) []control.NodeID {
	nodes := make([]control.NodeID, nodeCount)
	for index := range nodes {
		nodes[index] = control.NodeID(fmt.Sprintf("n%d", index+1))
	}
	return nodes
}

func assertBootstrapRootReplay(
	t *testing.T,
	ctx context.Context,
	root controlruntime.Trace,
	config controlexperiment.RuntimeConfig,
	adapterFactory func() (control.Adapter, error),
) {
	t.Helper()
	seed, err := hex.DecodeString(config.SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := adapterFactory()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.Replay(ctx, adapter, controlruntime.Config{
		Seed: seed, ClockError: config.ClockError, MaxClones: config.MaxClones,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	replayed, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Digest != root.Digest {
		t.Fatalf("bootstrap root replay drift: got %s want %s", replayed.Digest, root.Digest)
	}
}

func assertBootstrapRootOracleClean(
	t *testing.T,
	root controlruntime.Trace,
	registry targetoracles.Registry,
) {
	t.Helper()
	// Bootstrap roots contain no workload result and are not standalone formal
	// Bundles. Run the registry's existing property monitors over exactly the
	// root Trace; trace-integrity remains covered by root.Validate and Replay.
	if err := root.Validate(); err != nil {
		t.Fatal(err)
	}
	result := oracle.CheckBundle(
		controlexperiment.ExecutionBundle{Trace: root}, registry.EvaluationMonitors()...,
	)
	if len(result.Violations) != 0 {
		t.Fatalf("bootstrap root is not Oracle-clean: %#v", result.Violations)
	}
}

func TestActiveAgenticDossiersExposeReadableDeclaredSources(t *testing.T) {
	etcdKnowledge, _, _, err := loadEtcdraftAgenticAuthoringSource(etcdraftAgenticTestInputPath)
	if err != nil {
		t.Fatal(err)
	}
	omniKnowledge, _, _, err := loadOmnipaxosAgenticAuthoringSource(omnipaxosAgenticTestInputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		knowledge controlexperiment.ProtocolKnowledgePack
		reference string
		contains  string
	}{
		{
			name: "etcdraft", knowledge: etcdKnowledge,
			reference: "adapters/etcdraftv2/adapter.go:Check", contains: "Check",
		},
		{
			name: "omnipaxos", knowledge: omniKnowledge,
			reference: "adapters/omnipaxosv2/adapter.go:Manifest", contains: "Manifest",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := controlexperiment.KnowledgeSourceCatalog(test.knowledge)
			if err != nil || len(catalog) < 10 {
				t.Fatalf("active Dossier did not produce a useful source catalog: %d/%v", len(catalog), err)
			}
			result, err := controlexperiment.ReadDeclaredKnowledgeSource(
				"../..", test.knowledge,
				controlexperiment.KnowledgeReadRequest{Reference: test.reference, MaxLines: 40},
			)
			if err != nil || result.Validate() != nil ||
				result.Status != controlexperiment.KnowledgeDiscoveryCompleted ||
				!strings.Contains(result.Text, test.contains) {
				t.Fatalf("active declared source was not readable: %#v/%v", result, err)
			}
		})
	}
}

func seededAgenticRisk() controlexperiment.ProtocolRisk {
	return controlexperiment.ProtocolRisk{
		ID:                   "preseeded-risk",
		Summary:              "A curated Risk must not enter the A9e1 Agentic input.",
		RequiredCapabilities: []string{"runtime-owned-message-control"},
		RequiredActions:      []control.ActionKind{control.ActionDropMessage},
		AllowedBackendIDs:    []string{controlexperiment.ScenarioPlanningBackendID},
	}
}

func agenticInputWithExtraField(t *testing.T, sourcePath string, name string, value any) string {
	t.Helper()
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]any
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	source[name] = value
	return writeAgenticInputFixture(t, source)
}

func writeAgenticInputFixture(t *testing.T, source any) string {
	t.Helper()
	encoded, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "agentic-input.json")
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

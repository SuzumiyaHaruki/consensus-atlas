package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

var portableM522eFrozenSeeds = []uint64{1, 2, 3}

var portableM522eFrozenExecutions = map[uint64]struct {
	EtcdActionClass  string
	EtcdUniform      string
	EtcdClassSteps   int
	EtcdUniformSteps int
	OmniActionClass  string
	OmniUniform      string
	OmniClassSteps   int
	OmniUniformSteps int
}{
	1: {
		EtcdActionClass: "17446c303a6e3039d49009b80cfb8c62995758e866fd2a81d8554878b3b00280",
		EtcdUniform:     "1b9cc0597310662fdbd10db75137f80945684ea03ff915f8ff1778d8b6ae299b",
		EtcdClassSteps:  42, EtcdUniformSteps: 57,
		OmniActionClass: "ba499fd5d78b2707b60efed9304797ecd9d7b4853fae7bba8140ed0d42f13bb4",
		OmniUniform:     "754dfd2f67bd6363221bb57823804c61374579ad531f592d0a9f40ba4f65fd3c",
		OmniClassSteps:  29, OmniUniformSteps: 53,
	},
	2: {
		EtcdActionClass: "34ccd2803cfc6a211160da8d97fce6ddd10384b52879a717ae7e06c56940dcce",
		EtcdUniform:     "564fc382925e1415a3544a3968f97dde48655474d21a80ae6c9426dfed71ff3a",
		EtcdClassSteps:  42, EtcdUniformSteps: 72,
		OmniActionClass: "f296b41d534ee65f5ece96654ce35cbfa22c9b8497d1fc1ebf01bba072e389b6",
		OmniUniform:     "56061f8f3efdcb8bccdc2c7cba499b52b6ad79d5f77b17f3775b2af6a10197a7",
		OmniClassSteps:  34, OmniUniformSteps: 42,
	},
	3: {
		EtcdActionClass: "e21732b22a5ac2515540eb336e5b27618638f4d5f3aa274aaaa476ca23d2b009",
		EtcdUniform:     "5b07d5e7a4297805f60f88cef154b9878e7dd71645f5133203da510f58a7e9b7",
		EtcdClassSteps:  43, EtcdUniformSteps: 56,
		OmniActionClass: "635e165768192961f7863753eb7ecf0dae2a89d2d5dbf1bb04886abc9d27e993",
		OmniUniform:     "61589944ee4996d5f499d8848b10fa170676cb67b206ead4e33839bd82acbe1c",
		OmniClassSteps:  34, OmniUniformSteps: 43,
	},
}

func TestM522eTwoCommonBackendsChangeBothTargetTraces(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	inputs, err := newPortableM522eInputs(ctx, buildPortableM522bWorker(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs.CommonView.EligibleBackends) != 2 ||
		len(inputs.CommonView.ValidatedCapabilities) != 6 ||
		len(inputs.CommonView.AvailableActions) != 3 {
		t.Fatalf("M5.22e common surface is not the frozen two-backend gate: %#v", inputs.CommonView)
	}
	for _, seed := range portableM522eFrozenSeeds {
		actionClass, err := planPortableM522eTargets(inputs, portableM522bBackend, seed)
		if err != nil {
			t.Fatal(err)
		}
		uniform, err := planPortableM522eTargets(inputs, portableM522eBackendUniform, seed)
		if err != nil {
			t.Fatal(err)
		}
		assertPortableM522ePlanningPair(t, seed, actionClass, uniform)
		classEtcd, classOmni, err := executePortableM522eTargets(ctx, inputs, actionClass)
		if err != nil {
			t.Fatal(err)
		}
		uniformEtcd, uniformOmni, err := executePortableM522eTargets(ctx, inputs, uniform)
		if err != nil {
			t.Fatal(err)
		}
		assertPortableM522eExecutionPair(t, "etcd", seed, classEtcd, uniformEtcd)
		assertPortableM522eExecutionPair(t, "omnipaxos", seed, classOmni, uniformOmni)
		expected := portableM522eFrozenExecutions[seed]
		if classEtcd.Bundle.Trace.Digest != expected.EtcdActionClass ||
			uniformEtcd.Bundle.Trace.Digest != expected.EtcdUniform ||
			classEtcd.Report.Runs[0].ChargedDecisions != expected.EtcdClassSteps ||
			uniformEtcd.Report.Runs[0].ChargedDecisions != expected.EtcdUniformSteps ||
			classOmni.Bundle.Trace.Digest != expected.OmniActionClass ||
			uniformOmni.Bundle.Trace.Digest != expected.OmniUniform ||
			classOmni.Report.Runs[0].ChargedDecisions != expected.OmniClassSteps ||
			uniformOmni.Report.Runs[0].ChargedDecisions != expected.OmniUniformSteps {
			t.Fatalf("M5.22e frozen execution drift for seed %d", seed)
		}
		t.Logf(
			"seed=%d etcd class=%s/%d uniform=%s/%d omni class=%s/%d uniform=%s/%d",
			seed,
			classEtcd.Bundle.Trace.Digest, classEtcd.Report.Runs[0].ChargedDecisions,
			uniformEtcd.Bundle.Trace.Digest, uniformEtcd.Report.Runs[0].ChargedDecisions,
			classOmni.Bundle.Trace.Digest, classOmni.Report.Runs[0].ChargedDecisions,
			uniformOmni.Bundle.Trace.Digest, uniformOmni.Report.Runs[0].ChargedDecisions,
		)
	}
	assertPortableM522eFrozenSummary(t)
}

func assertPortableM522eFrozenSummary(t *testing.T) {
	t.Helper()
	type execution struct {
		Seed                     uint64 `json:"seed"`
		EtcdActionClassTrace     string `json:"etcd_action_class_trace"`
		EtcdActionClassDecisions int    `json:"etcd_action_class_decisions"`
		EtcdUniformTrace         string `json:"etcd_uniform_trace"`
		EtcdUniformDecisions     int    `json:"etcd_uniform_decisions"`
		OmniActionClassTrace     string `json:"omnipaxos_action_class_trace"`
		OmniActionClassDecisions int    `json:"omnipaxos_action_class_decisions"`
		OmniUniformTrace         string `json:"omnipaxos_uniform_trace"`
		OmniUniformDecisions     int    `json:"omnipaxos_uniform_decisions"`
	}
	var summary struct {
		Executions []execution `json:"executions"`
		Result     struct {
			PrimaryExecutions  int  `json:"primary_executions"`
			StableReplays      int  `json:"stable_replays"`
			DistinctTracePairs int  `json:"distinct_trace_pairs"`
			ExecutionInfluence bool `json:"execution_influence"`
		} `json:"result"`
	}
	data, err := os.ReadFile("../../benchmarks/experiments/cross-target-backend-influence-m5.22e/summary.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Executions) != len(portableM522eFrozenExecutions) ||
		summary.Result.PrimaryExecutions != 12 || summary.Result.StableReplays != 12 ||
		summary.Result.DistinctTracePairs != 6 || !summary.Result.ExecutionInfluence {
		t.Fatalf("M5.22e summary boundary drifted: %#v", summary)
	}
	for _, got := range summary.Executions {
		want, ok := portableM522eFrozenExecutions[got.Seed]
		if !ok || got.EtcdActionClassTrace != want.EtcdActionClass ||
			got.EtcdActionClassDecisions != want.EtcdClassSteps ||
			got.EtcdUniformTrace != want.EtcdUniform || got.EtcdUniformDecisions != want.EtcdUniformSteps ||
			got.OmniActionClassTrace != want.OmniActionClass || got.OmniActionClassDecisions != want.OmniClassSteps ||
			got.OmniUniformTrace != want.OmniUniform || got.OmniUniformDecisions != want.OmniUniformSteps {
			t.Fatalf("M5.22e summary execution drifted: %#v", got)
		}
	}
}

func assertPortableM522ePlanningPair(
	t *testing.T,
	seed uint64,
	actionClass portableM522bPlanning,
	uniform portableM522bPlanning,
) {
	t.Helper()
	if actionClass.Intent.Digest == uniform.Intent.Digest ||
		!reflect.DeepEqual(actionClass.Intent.Must, uniform.Intent.Must) ||
		actionClass.EtcdPlan.BackendID != portableM522bBackend ||
		actionClass.OmniPlan.BackendID != portableM522bBackend ||
		uniform.EtcdPlan.BackendID != portableM522eBackendUniform ||
		uniform.OmniPlan.BackendID != portableM522eBackendUniform ||
		actionClass.EtcdPlan.FallbackUsed || actionClass.OmniPlan.FallbackUsed ||
		uniform.EtcdPlan.FallbackUsed || uniform.OmniPlan.FallbackUsed ||
		len(actionClass.EtcdPlan.PreferenceMisses) != 0 || len(actionClass.OmniPlan.PreferenceMisses) != 0 ||
		len(uniform.EtcdPlan.PreferenceMisses) != 0 || len(uniform.OmniPlan.PreferenceMisses) != 0 ||
		actionClass.EtcdPlan.CompilerWork.CandidatesEvaluated != 2 ||
		uniform.EtcdPlan.CompilerWork.CandidatesEvaluated != 2 ||
		actionClass.EtcdInstance.PolicySeed != seed || actionClass.OmniInstance.PolicySeed != seed ||
		uniform.EtcdInstance.PolicySeed != seed || uniform.OmniInstance.PolicySeed != seed ||
		actionClass.EtcdInstance.Budget != uniform.EtcdInstance.Budget ||
		actionClass.OmniInstance.Budget != uniform.OmniInstance.Budget {
		t.Fatalf("M5.22e preference did not select one exact common backend: class=%#v uniform=%#v",
			actionClass, uniform)
	}
}

func assertPortableM522eExecutionPair(
	t *testing.T,
	target string,
	seed uint64,
	actionClass portableM522bTargetExecution,
	uniform portableM522bTargetExecution,
) {
	t.Helper()
	for method, execution := range map[string]portableM522bTargetExecution{
		"action-class": actionClass, "uniform": uniform,
	} {
		if execution.Outcome.ExecutionStatus != controlexperiment.IntentExecutionValid ||
			execution.Outcome.IntentStatus != controlexperiment.IntentReachabilityReached ||
			len(execution.Report.Runs) != 1 || !execution.Report.Runs[0].Replay.Stable ||
			execution.Report.Runs[0].Workload == nil || execution.Report.Runs[0].Workload.Completed != 1 ||
			execution.Report.Runs[0].Termination != controlexperiment.RunTerminationConfigured {
			t.Fatalf("%s/%s seed %d did not complete with stable Replay: %#v",
				target, method, seed, execution)
		}
	}
	classPolicy := actionClass.Report.Config.Runs[0].Policy
	uniformPolicy := uniform.Report.Config.Runs[0].Policy
	if classPolicy.Version != controlexperiment.BoundedActionClassPolicyVersion ||
		uniformPolicy.Version != controlexperiment.BoundedUniformPolicyVersion ||
		classPolicy.SeedHex != uniformPolicy.SeedHex ||
		!reflect.DeepEqual(classPolicy.SelectableActions, uniformPolicy.SelectableActions) ||
		actionClass.Report.Config.DecisionsPerRun != uniform.Report.Config.DecisionsPerRun ||
		actionClass.Bundle.Trace.Digest == uniform.Bundle.Trace.Digest ||
		reflect.DeepEqual(traceActionIDs(actionClass), traceActionIDs(uniform)) {
		t.Fatalf("%s seed %d did not lower into behaviorally distinct equal-surface policies: class=%#v uniform=%#v",
			target, seed, classPolicy, uniformPolicy)
	}
	for _, execution := range []portableM522bTargetExecution{actionClass, uniform} {
		observed := selectedActionCounts(execution.Bundle)
		for _, kind := range portableM522bActions() {
			if observed[kind] == 0 {
				t.Fatalf("%s seed %d did not reach common Action %s: %v", target, seed, kind, observed)
			}
		}
		if target == "etcd" && observed[control.ActionCompleteEffect] == 0 {
			t.Fatalf("%s seed %d omitted required target-local plumbing: %v", target, seed, observed)
		}
	}
}

func traceActionIDs(execution portableM522bTargetExecution) []control.ActionID {
	ids := make([]control.ActionID, len(execution.Bundle.Trace.Records))
	for index, record := range execution.Bundle.Trace.Records {
		ids[index] = record.Action.ID
	}
	return ids
}

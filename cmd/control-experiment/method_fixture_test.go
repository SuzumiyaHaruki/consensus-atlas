package main

import (
	"context"
	"sync"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

var (
	etcdraftWorkloadFixtureOnce   sync.Once
	etcdraftWorkloadFixtureReport controlexperiment.Report
	etcdraftWorkloadFixtureBundle controlexperiment.ExecutionBundle
	etcdraftWorkloadFixtureErr    error

	etcdraftUniformFixtureOnce sync.Once
	etcdraftUniformFixture     etcdraftMethodExecution
	etcdraftUniformFixtureErr  error

	etcdraftActionFixtureOnce sync.Once
	etcdraftActionFixture     etcdraftMethodExecution
	etcdraftActionFixtureErr  error

	etcdraftActionV2FixtureOnce sync.Once
	etcdraftActionV2Fixture     etcdraftMethodExecution
	etcdraftActionV2FixtureErr  error
)

// sharedEtcdraftWorkloadBundleFixture avoids rebuilding the same immutable
// workload/96/seed-1 witness in two independent regression tests. Each test
// still validates the complete report and bundle; this physical reuse does
// not change any persisted method cost or identity.
func sharedEtcdraftWorkloadBundleFixture(t *testing.T) (
	controlexperiment.Report,
	controlexperiment.ExecutionBundle,
) {
	t.Helper()
	etcdraftWorkloadFixtureOnce.Do(func() {
		etcdraftWorkloadFixtureReport, etcdraftWorkloadFixtureBundle, etcdraftWorkloadFixtureErr =
			etcdraftBundle(context.Background(), "workload", 96, 1)
	})
	if etcdraftWorkloadFixtureErr != nil {
		t.Fatal(etcdraftWorkloadFixtureErr)
	}
	return etcdraftWorkloadFixtureReport, etcdraftWorkloadFixtureBundle
}

// These package-local fixtures execute each frozen seeds-1/2/3 source batch
// once. Every consumer still reconstructs and validates its own corpus,
// feedback and ledger identities from the complete bundles.
func sharedEtcdraftUniformMethodFixture(t *testing.T) etcdraftMethodExecution {
	t.Helper()
	etcdraftUniformFixtureOnce.Do(func() {
		etcdraftUniformFixture, etcdraftUniformFixtureErr = etcdraftAdmissibleUniformMethod(
			context.Background(), 96, 1,
		)
	})
	if etcdraftUniformFixtureErr != nil {
		t.Fatal(etcdraftUniformFixtureErr)
	}
	return etcdraftUniformFixture
}

func sharedEtcdraftActionMethodFixture(t *testing.T) etcdraftMethodExecution {
	t.Helper()
	etcdraftActionFixtureOnce.Do(func() {
		etcdraftActionFixture, etcdraftActionFixtureErr = etcdraftActionClassMethod(
			context.Background(), 96, 1,
		)
	})
	if etcdraftActionFixtureErr != nil {
		t.Fatal(etcdraftActionFixtureErr)
	}
	return etcdraftActionFixture
}

func sharedEtcdraftActionClassBundleFixture(t *testing.T) (
	controlexperiment.Report,
	controlexperiment.ExecutionBundle,
) {
	t.Helper()
	method := sharedEtcdraftActionMethodFixture(t)
	if len(method.Reports) == 0 || len(method.Bundles) == 0 {
		t.Fatal("shared action-class method has no completed source")
	}
	return method.Reports[0], method.Bundles[0]
}

func sharedEtcdraftActionV2MethodFixture(t *testing.T) etcdraftMethodExecution {
	t.Helper()
	etcdraftActionV2FixtureOnce.Do(func() {
		etcdraftActionV2Fixture, etcdraftActionV2FixtureErr = etcdraftActionClassMethodV2(
			context.Background(), 96, 1,
		)
	})
	if etcdraftActionV2FixtureErr != nil {
		t.Fatal(etcdraftActionV2FixtureErr)
	}
	return etcdraftActionV2Fixture
}

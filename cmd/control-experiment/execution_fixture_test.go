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
)

// sharedEtcdraftWorkloadBundleFixture avoids rebuilding the same immutable
// workload/96/seed-1 witness in independent regression tests.
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

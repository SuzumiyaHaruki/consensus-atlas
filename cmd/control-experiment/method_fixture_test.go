package main

import (
	"context"
	"sync"
	"testing"
)

var (
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

//go:build race

package main

import "time"

// Race instrumentation makes the real Adapter/Replay integration tests take
// substantially longer without changing their logical work bounds. The
// package-level race shard timeout remains the independent hard ceiling.
func controlExperimentTestTimeout(base time.Duration) time.Duration {
	return 3 * base
}

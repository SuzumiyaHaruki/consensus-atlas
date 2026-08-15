//go:build !race

package main

import "time"

func controlExperimentTestTimeout(base time.Duration) time.Duration {
	return base
}

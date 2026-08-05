package main

import "testing"

func TestExperimentIDIsOneSafePathComponent(t *testing.T) {
	for _, valid := range []string{"etcdraft-001", "run_2", "v1.0"} {
		if !validExperimentID(valid) {
			t.Fatalf("valid id rejected: %q", valid)
		}
	}
	for _, invalid := range []string{"", ".", "..", "../escape", "nested/run", "with space"} {
		if validExperimentID(invalid) {
			t.Fatalf("unsafe id accepted: %q", invalid)
		}
	}
}

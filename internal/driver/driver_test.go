package driver_test

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
)

func TestOutputBatchRejectsDependencyCycle(t *testing.T) {
	batch := driver.OutputBatch{
		Token: "b1", Node: "n1",
		Operations: []driver.Operation{
			{Token: "persist", Kind: core.EventPersist, After: []string{"ack"}},
			{Token: "ack", Kind: core.EventAcknowledge, After: []string{"persist"}},
		},
	}
	if err := batch.Validate(); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Validate() error = %v, want dependency cycle", err)
	}
}

func TestOutputBatchRequiresAckToJoinAllOperations(t *testing.T) {
	batch := driver.OutputBatch{
		Token: "b1", Node: "n1",
		Operations: []driver.Operation{
			{Token: "persist", Kind: core.EventPersist},
			{Token: "orphan", Kind: core.EventApply},
			{Token: "ack", Kind: core.EventAcknowledge, After: []string{"persist"}},
		},
	}
	if err := batch.Validate(); err == nil || !strings.Contains(err.Error(), "every host operation") {
		t.Fatalf("Validate() error = %v, want orphan operation rejection", err)
	}
}

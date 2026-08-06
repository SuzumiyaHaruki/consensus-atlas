package autoonboard_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

type recordingGenerator struct {
	binding  *autoonboard.Binding
	requests []autoonboard.GenerationRequest
}

func (g *recordingGenerator) Generate(_ context.Context, request autoonboard.GenerationRequest) (*autoonboard.Binding, error) {
	g.requests = append(g.requests, request)
	copy := *g.binding
	return &copy, nil
}

func TestCoordinatorFeedsMechanicalFindingsBack(t *testing.T) {
	root := filepath.Join("..", "..")
	contract, err := protocolcontract.Load(filepath.Join(root, "contracts", "etcdraft-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := autoonboard.Load(filepath.Join(root, "onboarding", "etcdraft-binding-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	generator := &recordingGenerator{binding: binding}
	validations := 0
	validator := func(_ context.Context, proposal *autoonboard.Binding) (autoonboard.Report, error) {
		validations++
		if validations == 1 {
			return autoonboard.Report{Status: "invalid", Findings: []autoonboard.Finding{{
				Code: "witness.failed", Message: "deterministic failure", Actionable: true,
			}}}, nil
		}
		return autoonboard.Report{Status: "validated", BindingID: proposal.ID}, nil
	}
	loop, err := autoonboard.Coordinate(context.Background(), contract, autoonboard.GenerationContext{}, 3, generator, validator)
	if err != nil {
		t.Fatal(err)
	}
	if loop.Status != "validated" || len(loop.Attempts) != 2 {
		t.Fatalf("unexpected loop: %+v", loop)
	}
	if len(generator.requests) != 2 || len(generator.requests[1].PreviousFindings) != 1 || generator.requests[1].PreviousFindings[0].Code != "witness.failed" {
		t.Fatalf("mechanical feedback was not returned to generator: %+v", generator.requests)
	}
	if generator.requests[1].PreviousBinding == nil || generator.requests[1].PreviousBinding.ID != binding.ID {
		t.Fatalf("previous proposal was not returned to generator: %+v", generator.requests[1])
	}
	if generator.requests[1].PreviousReport == nil || generator.requests[1].PreviousReport.Status != "invalid" {
		t.Fatalf("previous validation evidence was not returned to generator: %+v", generator.requests[1])
	}
}

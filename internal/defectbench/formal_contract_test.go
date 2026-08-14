package defectbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type fixtureProjector struct{ id string }

func (projector fixtureProjector) ID() string { return projector.id }
func (fixtureProjector) Project(control.EvidenceEnvelope) ([]semantic.DecisionObservation, error) {
	return nil, nil
}

func TestFormalBenchmarkContractProducesOpaqueViewAndResolvesComposition(t *testing.T) {
	contract := sealFixtureContract(t)
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	if err := view.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := contract.ValidateOpaqueView(view); err != nil {
		t.Fatal(err)
	}
	if len(view.Trials) != 6 || view.Trials[0].TrialID != "opaque-01" || view.Trials[5].TrialID != "opaque-06" {
		t.Fatalf("opaque trials = %#v", view.Trials)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range fixturePrivateAtoms(contract) {
		if bytes.Contains(encoded, []byte(private)) {
			t.Fatalf("opaque view contains private atom %q", private)
		}
	}

	selected, err := ResolveFormalComposition(
		contract,
		fixtureProjector{id: "fixture/projector-v1"},
		oracle.BundleAgreement{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Name() != "agreement" {
		t.Fatalf("selected monitors = %#v", selected)
	}
	if _, err := ResolveFormalComposition(contract, fixtureProjector{id: "wrong"}, oracle.BundleAgreement{}); err == nil {
		t.Fatal("mismatched projector was accepted")
	}
	if _, err := ResolveFormalComposition(contract, fixtureProjector{id: "fixture/projector-v1"}); err == nil {
		t.Fatal("missing monitor was accepted")
	}
}

func TestFormalBenchmarkContractRejectsInvalidPrivateStructure(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*FormalBenchmarkContract)
		code   string
	}{
		{
			name: "fewer than three pairs",
			mutate: func(contract *FormalBenchmarkContract) {
				contract.Pairs = contract.Pairs[:2]
			},
			code: "FORMAL_BENCHMARK_MINIMUM_PAIRS_REQUIRED",
		},
		{
			name: "fewer than three roots",
			mutate: func(contract *FormalBenchmarkContract) {
				contract.Pairs[1].RootCauseID = contract.Pairs[0].RootCauseID
				contract.Pairs[2].RootCauseID = contract.Pairs[0].RootCauseID
			},
			code: "FORMAL_BENCHMARK_MINIMUM_ROOT_CAUSES_REQUIRED",
		},
		{
			name: "duplicate opaque trial",
			mutate: func(contract *FormalBenchmarkContract) {
				contract.Pairs[1].Control.TrialID = contract.Pairs[0].Control.TrialID
			},
			code: "FORMAL_BENCHMARK_VARIANT_DUPLICATE",
		},
		{
			name: "opaque trial is private root",
			mutate: func(contract *FormalBenchmarkContract) {
				contract.Pairs[0].Control.TrialID = contract.Pairs[0].RootCauseID
			},
			code: "FORMAL_BENCHMARK_OPAQUE_TRIAL_COLLISION",
		},
		{
			name: "trace integrity used as kill monitor",
			mutate: func(contract *FormalBenchmarkContract) {
				contract.Composition.MonitorIDs = []string{"trace-integrity"}
			},
			code: "FORMAL_BENCHMARK_MONITOR_INVALID",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := fixtureContract()
			test.mutate(&contract)
			_, err := contract.Seal()
			if err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}
}

func TestFormalOpaqueViewRejectsResealedProjectionChange(t *testing.T) {
	contract := sealFixtureContract(t)
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	view.Budget.MaxDecisions++
	view, err = view.Seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.ValidateOpaqueView(view); err == nil ||
		!strings.Contains(err.Error(), "FORMAL_OPAQUE_VIEW_PROJECTION_MISMATCH") {
		t.Fatalf("projection error = %v", err)
	}
}

type formalContractStageSummary struct {
	SchemaVersion            string `json:"schema_version"`
	ID                       string `json:"id"`
	ContractDigest           string `json:"contract_digest"`
	OpaqueViewDigest         string `json:"opaque_view_digest"`
	Pairs                    int    `json:"pairs"`
	Trials                   int    `json:"trials"`
	DistinctRootCauses       int    `json:"distinct_root_causes"`
	DatasetClassification    string `json:"dataset_classification"`
	CompositionResolved      bool   `json:"composition_resolved"`
	FreshEvaluatorWired      bool   `json:"fresh_evaluator_wired"`
	ExposureAuditImplemented bool   `json:"exposure_audit_implemented"`
	FormalReady              bool   `json:"formal_ready"`
	NewModelCalls            int    `json:"new_model_calls"`
	NewSUTExecutions         int    `json:"new_sut_executions"`
	Digest                   string `json:"digest"`
}

func TestSyntheticContractSummaryIsRecomputed(t *testing.T) {
	contract := sealFixtureContract(t)
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveFormalComposition(contract, fixtureProjector{id: contract.Composition.ProjectorID}, oracle.BundleAgreement{}); err != nil {
		t.Fatal(err)
	}
	roots := map[string]bool{}
	for _, pair := range contract.Pairs {
		roots[pair.RootCauseID] = true
	}
	recomputed := formalContractStageSummary{
		SchemaVersion: "consensus-atlas/formal-contract-stage-summary/v1",
		ID:            "formal-benchmark-contract-m5-21l", ContractDigest: contract.Digest,
		OpaqueViewDigest: view.Digest, Pairs: len(contract.Pairs), Trials: len(view.Trials),
		DistinctRootCauses: len(roots), DatasetClassification: "public-synthetic-contract-fixture-only",
		CompositionResolved: true, FreshEvaluatorWired: false, ExposureAuditImplemented: false,
		FormalReady: false, NewModelCalls: 0, NewSUTExecutions: 0,
	}
	digest, err := control.CanonicalDigest(recomputed)
	if err != nil {
		t.Fatal(err)
	}
	recomputed.Digest = digest

	path := "../../benchmarks/experiments/formal-benchmark-contract-m5.21l/summary.json"
	data, err := os.ReadFile(path)
	if err != nil {
		encoded, _ := json.MarshalIndent(recomputed, "", "  ")
		t.Fatalf("read stage summary: %v\n%s", err, append(encoded, '\n'))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var archived formalContractStageSummary
	if err := decoder.Decode(&archived); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatal("stage summary contains trailing JSON")
	}
	if !reflect.DeepEqual(archived, recomputed) {
		encoded, _ := json.MarshalIndent(recomputed, "", "  ")
		t.Fatalf("stage summary is stale; recomputed:\n%s", append(encoded, '\n'))
	}
}

func fixtureContract() FormalBenchmarkContract {
	pair := func(index int, root string) FormalPair {
		controlTrial := "opaque-0" + string(rune('1'+(index-1)*2))
		candidateTrial := "opaque-0" + string(rune('2'+(index-1)*2))
		return FormalPair{
			PairID: "private-pair-" + string(rune('a'+index-1)), RootCauseID: root,
			Control:   fixtureVariant(controlTrial, "private-control-"+string(rune('a'+index-1))),
			Candidate: fixtureVariant(candidateTrial, "private-candidate-"+string(rune('a'+index-1))),
		}
	}
	return FormalBenchmarkContract{
		ID: "synthetic-formal-contract", FamilyID: "leader-cft",
		ProfileDigest: testFormalDigest("profile"), BlindingNonce: testFormalDigest("nonce"),
		MethodSpecDigest:     testFormalDigest("method"),
		RequiredBundleSchema: "consensus-atlas/execution-bundle/v3",
		Budget:               BundleBudget{MaxDecisions: 64, MaxPrimaryWorkUnits: 96},
		Composition:          FormalCompositionSpec{ProjectorID: "fixture/projector-v1", MonitorIDs: []string{"agreement"}},
		Pairs: []FormalPair{
			pair(1, "private-root-alpha"), pair(2, "private-root-beta"), pair(3, "private-root-gamma"),
		},
	}
}

func fixtureVariant(trial, variant string) FormalVariant {
	return FormalVariant{
		TrialID: trial, VariantID: variant, ExpectedBuildID: "build-" + variant,
		ExpectedConfigDigest:     testFormalDigest("config"),
		ExpectedBuildAuditDigest: testFormalDigest("audit-" + variant),
		ExpectedBinaryDigest:     testFormalDigest("binary-" + variant),
	}
}

func sealFixtureContract(t *testing.T) FormalBenchmarkContract {
	t.Helper()
	contract, err := fixtureContract().Seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	return contract
}

func fixturePrivateAtoms(contract FormalBenchmarkContract) []string {
	result := []string{contract.BlindingNonce, contract.Composition.ProjectorID}
	result = append(result, contract.Composition.MonitorIDs...)
	for _, pair := range contract.Pairs {
		result = append(result, pair.PairID, pair.RootCauseID)
		result = append(result, formalVariantPrivateAtoms(pair.Control)...)
		result = append(result, formalVariantPrivateAtoms(pair.Candidate)...)
	}
	return result
}

func testFormalDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

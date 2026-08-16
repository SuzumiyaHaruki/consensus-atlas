package etcdraftv2_test

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	hashqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/hashicorpraftv2"
)

const matrixPath = "../../benchmarks/qualifications/portable-cft-v2-m5.4e/capability-matrix.json"

type capabilityMatrix struct {
	SchemaVersion   string                 `json:"schema_version"`
	MatrixID        string                 `json:"matrix_id"`
	ProfileID       string                 `json:"profile_id"`
	ProfileDigest   string                 `json:"profile_digest"`
	Implementations []matrixImplementation `json:"implementations"`
	Capabilities    []matrixCapability     `json:"capabilities"`
	SharedValidated []string               `json:"shared_validated"`
	Evidence        []matrixEvidence       `json:"evidence"`
	Policy          matrixPolicy           `json:"policy"`
	Digest          string                 `json:"digest"`
}

type matrixImplementation struct {
	Key                 string                           `json:"key"`
	AdapterID           string                           `json:"adapter_id"`
	ImplementationID    string                           `json:"implementation_id"`
	BuildID             string                           `json:"build_id"`
	QualificationDigest string                           `json:"qualification_report_digest"`
	Qualified           bool                             `json:"qualified"`
	Summary             conformance.QualificationSummary `json:"summary"`
}

type matrixCapability struct {
	ID              string                   `json:"id"`
	Required        bool                     `json:"required"`
	Implementations []matrixCapabilityStatus `json:"implementations"`
}

type matrixCapabilityStatus struct {
	Implementation string                       `json:"implementation"`
	Status         conformance.CapabilityStatus `json:"status"`
}

type matrixEvidence struct {
	Kind           string `json:"kind"`
	Implementation string `json:"implementation"`
	Path           string `json:"path"`
	Digest         string `json:"digest"`
}

type matrixPolicy struct {
	EligibleStatus          string `json:"eligible_status"`
	PerImplementationRule   string `json:"per_implementation_rule"`
	CrossImplementationRule string `json:"cross_implementation_rule"`
	CoverageCreditRule      string `json:"coverage_credit_rule"`
}

func TestFrozenPortableCapabilityMatrixMatchesMechanicalQualification(t *testing.T) {
	etcd := portableV2Qualification(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hash, err := hashqualification.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if etcd.ProfileID != hash.Qualification.ProfileID || etcd.ProfileDigest != hash.Qualification.ProfileDigest {
		t.Fatal("capability matrix inputs use different profiles")
	}

	hashBundleDigest := checkedDigest(t,
		"../../benchmarks/qualifications/hashicorp-raft-v2-m5.4c/report.json", "digest")
	if hashBundleDigest != hash.Digest {
		t.Fatalf("checked HashiCorp qualification is stale: %s != %s", hashBundleDigest, hash.Digest)
	}
	auditDigest := checkedDigest(t,
		"../../benchmarks/probes/hashicorp-raft-v1.7.3-m5.4d/report.json", "canonical_digest")

	fresh := buildMatrix(t, etcd, hash.Qualification, hashBundleDigest, auditDigest)
	encoded, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	var frozen capabilityMatrix
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		t.Fatal(err)
	}
	if fresh.Digest == "" {
		t.Fatal("fresh capability matrix has no mechanical identity")
	}
	sealed, err := sealMatrix(frozen)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Digest != frozen.Digest {
		t.Fatalf("frozen matrix digest is invalid: %s != %s", sealed.Digest, frozen.Digest)
	}
}

func buildMatrix(t *testing.T, etcd, hash conformance.QualificationReport, hashBundleDigest, auditDigest string) capabilityMatrix {
	t.Helper()
	etcdByID := qualificationStatuses(etcd)
	hashByID := qualificationStatuses(hash)
	if len(etcdByID) != len(hashByID) {
		t.Fatal("qualification capability counts differ")
	}
	capabilities := make([]matrixCapability, 0, len(etcd.Capabilities))
	shared := make([]string, 0)
	for _, capability := range etcd.Capabilities {
		hashCapability, ok := hashByID[capability.ID]
		if !ok || hashCapability.Required != capability.Required {
			t.Fatalf("capability %s is not aligned across reports", capability.ID)
		}
		capabilities = append(capabilities, matrixCapability{
			ID: capability.ID, Required: capability.Required,
			Implementations: []matrixCapabilityStatus{
				{Implementation: "etcd-raft", Status: capability.Status},
				{Implementation: "hashicorp-raft", Status: hashCapability.Status},
			},
		})
		if capability.Status == conformance.CapabilityValidated && hashCapability.Status == conformance.CapabilityValidated {
			shared = append(shared, capability.ID)
		}
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].ID < capabilities[j].ID })
	sort.Strings(shared)
	matrix := capabilityMatrix{
		SchemaVersion: "consensus-atlas/portable-cft-capability-matrix/v1",
		MatrixID:      "portable-cft-control-v2-m5.4e", ProfileID: etcd.ProfileID, ProfileDigest: etcd.ProfileDigest,
		Implementations: []matrixImplementation{
			matrixImplementationFrom("etcd-raft", etcd),
			matrixImplementationFrom("hashicorp-raft", hash),
		},
		Capabilities: capabilities, SharedValidated: shared,
		Evidence: []matrixEvidence{
			{Kind: "qualification-bundle", Implementation: "hashicorp-raft", Path: "benchmarks/qualifications/hashicorp-raft-v2-m5.4c/report.json", Digest: hashBundleDigest},
			{Kind: "determinism-source-audit", Implementation: "hashicorp-raft", Path: "benchmarks/probes/hashicorp-raft-v1.7.3-m5.4d/report.json", Digest: auditDigest},
		},
		Policy: matrixPolicy{
			EligibleStatus: "validated", PerImplementationRule: "use-only-capabilities-validated-for-selected-implementation",
			CrossImplementationRule: "intersection-of-validated-capabilities", CoverageCreditRule: "unsupported-or-unvalidated-capabilities-receive-no-credit",
		},
	}
	sealed, err := sealMatrix(matrix)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func qualificationStatuses(report conformance.QualificationReport) map[string]conformance.CapabilityQualification {
	statuses := make(map[string]conformance.CapabilityQualification, len(report.Capabilities))
	for _, capability := range report.Capabilities {
		statuses[capability.ID] = capability
	}
	return statuses
}

func matrixImplementationFrom(key string, report conformance.QualificationReport) matrixImplementation {
	return matrixImplementation{
		Key: key, AdapterID: report.AdapterID, ImplementationID: report.ImplementationID, BuildID: report.BuildID,
		QualificationDigest: report.Digest, Qualified: report.Qualified, Summary: report.Summary,
	}
}

func sealMatrix(matrix capabilityMatrix) (capabilityMatrix, error) {
	matrix.Digest = ""
	digest, err := control.CanonicalDigest(matrix)
	if err != nil {
		return capabilityMatrix{}, err
	}
	matrix.Digest = digest
	return matrix, nil
}

func checkedDigest(t *testing.T, path, field string) string {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	var digest string
	if err := json.Unmarshal(value[field], &digest); err != nil || digest == "" {
		t.Fatalf("read digest field %s from %s: %v", field, path, err)
	}
	return digest
}

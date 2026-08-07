package etcdraftv2_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	etcdadapter "github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	hashqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/hashicorpraftv2"
)

const surfaceReportPath = "../../benchmarks/qualifications/control-surfaces-m5.5a/report.json"

type surfaceComparison struct {
	SchemaVersion              string                             `json:"schema_version"`
	ComparisonID               string                             `json:"comparison_id"`
	QualificationProfileID     string                             `json:"qualification_profile_id"`
	QualificationProfileDigest string                             `json:"qualification_profile_digest"`
	Implementations            []conformance.ControlSurfaceReport `json:"implementations"`
	Policy                     surfacePolicy                      `json:"policy"`
	Digest                     string                             `json:"digest"`
}

type surfacePolicy struct {
	PresenceCredit string `json:"presence_credit"`
	ControlCredit  string `json:"control_credit"`
	GuaranteeRule  string `json:"guarantee_rule"`
}

func TestFrozenControlSurfaceComparisonMatchesFreshQualifications(t *testing.T) {
	etcdQualification := portableV2Qualification(t)
	etcdAdapter, err := etcdadapter.NewWithConfig(etcdadapter.ThreeNodeConfig())
	if err != nil {
		t.Fatal(err)
	}
	etcdManifest, err := etcdAdapter.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	etcd, err := conformance.EvaluateControlSurfaces(etcdManifest, etcdQualification, raftSurfaceDeclarations("ETCD_RAFT"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hashBundle, err := hashqualification.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := conformance.EvaluateControlSurfaces(hashBundle.Manifest, hashBundle.Qualification, raftSurfaceDeclarations("HASHICORP_RAFT"))
	if err != nil {
		t.Fatal(err)
	}

	if etcd.Summary != (conformance.ControlSurfaceSummary{
		Surfaces: 5, Present: 5, DeclaredSchedulerOwned: 5, ValidatedSchedulerOwned: 4,
		Guarantees: 5, RequiredGuarantees: 4, ValidatedGuarantees: 4,
	}) {
		t.Fatalf("unexpected etcd surface summary: %+v", etcd.Summary)
	}
	if hash.Summary != (conformance.ControlSurfaceSummary{
		Surfaces: 5, Present: 5, DeclaredSchedulerOwned: 3, ValidatedSchedulerOwned: 3,
		Guarantees: 5, RequiredGuarantees: 4, ValidatedGuarantees: 0,
	}) {
		t.Fatalf("unexpected HashiCorp surface summary: %+v", hash.Summary)
	}

	fresh, err := sealSurfaceComparison(surfaceComparison{
		SchemaVersion:              "consensus-atlas/control-surface-comparison/v1",
		ComparisonID:               "raft-control-surfaces-m5.5a",
		QualificationProfileID:     etcdQualification.ProfileID,
		QualificationProfileDigest: etcdQualification.ProfileDigest,
		Implementations:            []conformance.ControlSurfaceReport{etcd, hash},
		Policy: surfacePolicy{
			PresenceCredit: "presence-does-not-imply-control",
			ControlCredit:  "validated-control-only",
			GuaranteeRule:  "determinism-guarantees-are-not-protocol-features",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(surfaceReportPath)
	if err != nil {
		t.Fatal(err)
	}
	var frozen surfaceComparison
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh, frozen) {
		want, _ := json.MarshalIndent(fresh, "", "  ")
		t.Fatalf("frozen control surface comparison is stale; fresh report:\n%s", want)
	}
}

func TestSurfaceDeclarationCannotSelfAwardSchedulerControl(t *testing.T) {
	qualification := portableV2Qualification(t)
	adapter, err := etcdadapter.NewWithConfig(etcdadapter.ThreeNodeConfig())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := adapter.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	declarations := raftSurfaceDeclarations("INVALID")
	declarations[0].NativeControl = conformance.ControlSchedulerOwned
	if _, err := conformance.EvaluateControlSurfaces(manifest, qualification, declarations); err == nil || err.Error() != "CONTROL_SURFACE_NATIVE_SELF_CREDIT_FORBIDDEN" {
		t.Fatalf("self-awarded control error = %v", err)
	}
}

func raftSurfaceDeclarations(prefix string) []conformance.SurfaceDeclaration {
	return []conformance.SurfaceDeclaration{
		{ID: conformance.SurfaceDurability, Presence: conformance.PresencePresent, NativeControl: conformance.ControlOpaque, EvidenceCode: prefix + "_STABLE_STORAGE"},
		{ID: conformance.SurfaceExternalInput, Presence: conformance.PresencePresent, NativeControl: conformance.ControlOpaque, EvidenceCode: prefix + "_CLIENT_PROPOSAL"},
		{ID: conformance.SurfaceLifecycle, Presence: conformance.PresencePresent, NativeControl: conformance.ControlOpaque, EvidenceCode: prefix + "_NODE_LIFECYCLE"},
		{ID: conformance.SurfaceMessage, Presence: conformance.PresencePresent, NativeControl: conformance.ControlOpaque, EvidenceCode: prefix + "_PEER_RPC"},
		{ID: conformance.SurfaceTemporal, Presence: conformance.PresencePresent, NativeControl: conformance.ControlOpaque, EvidenceCode: prefix + "_TIMEOUT"},
	}
}

func sealSurfaceComparison(report surfaceComparison) (surfaceComparison, error) {
	sort.Slice(report.Implementations, func(i, j int) bool {
		return report.Implementations[i].AdapterID < report.Implementations[j].AdapterID
	})
	report.Digest = ""
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		return surfaceComparison{}, err
	}
	report.Digest = digest
	return report, nil
}

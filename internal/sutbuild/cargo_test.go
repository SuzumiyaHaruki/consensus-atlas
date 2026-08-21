package sutbuild_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

func TestBuildCargoProducesOmnipaxosWorkerAudit(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	protocolDigest, err := sutbuild.SourceTreeDigest(filepath.Join(repositoryRoot, "suts", "omnipaxos"))
	if err != nil {
		t.Fatal(err)
	}
	workerRoot := filepath.Join(repositoryRoot, "adapters", "omnipaxosv2", "worker")
	workerDigest, err := sutbuild.SourceTreeDigest(workerRoot)
	if err != nil {
		t.Fatal(err)
	}
	testOutputRoot := filepath.Join(repositoryRoot, "artifacts", "test-sutbuild-cargo", t.Name())
	t.Cleanup(func() { _ = os.RemoveAll(testOutputRoot) })
	spec := sutbuild.CargoSpec{
		Version: sutbuild.CargoSpecVersion, ID: "omnipaxos-worker-test", TrialID: "cargo-trial",
		Module:             sutbuild.ModuleIdentity{Path: "crates.io/omnipaxos", Version: "0.2.2"},
		ProtocolSourceRoot: "suts/omnipaxos", ProtocolSourceDigest: protocolDigest,
		WorkerSourceRoot: "adapters/omnipaxosv2/worker", WorkerSourceDigest: workerDigest,
		ManifestPath: "adapters/omnipaxosv2/worker/Cargo.toml",
		Package:      "consensus-atlas-omnipaxos-worker", BinaryName: "consensus-atlas-omnipaxos-worker",
		CommandAllowlist: []string{
			sutbuild.CommandCargoBuild, sutbuild.CommandCargoMetadata,
			sutbuild.CommandCargoVersion, sutbuild.CommandRustcVersion,
		},
		OutputPath: filepath.ToSlash(filepath.Join(
			"artifacts", "test-sutbuild-cargo", t.Name(), "worker",
		)),
	}
	audit, err := sutbuild.BuildCargo(repositoryRoot, spec)
	if err != nil {
		t.Fatal(err)
	}
	if audit.Validate() != nil || audit.Version != sutbuild.AuditVersion5 ||
		audit.SUTBuildIdentity != "sha256:"+audit.BinaryDigest || len(audit.Commands) != 4 {
		t.Fatalf("Cargo audit is incomplete: %#v", audit)
	}
	if _, err := os.Stat(filepath.Join(repositoryRoot, filepath.FromSlash(spec.OutputPath))); err != nil {
		t.Fatalf("audited worker was not written: %v", err)
	}
	adapter, err := omnipaxosv2.New(omnipaxosv2.Config{
		WorkerPath: filepath.Join(repositoryRoot, filepath.FromSlash(spec.OutputPath)),
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := adapter.Manifest(context.Background())
	_ = adapter.Close()
	if err != nil || manifest.BuildID != audit.SUTBuildIdentity {
		t.Fatalf("audited worker identity did not reach Adapter manifest: %q/%q err=%v",
			manifest.BuildID, audit.SUTBuildIdentity, err)
	}
	tampered := audit
	tampered.SUTBuildIdentity = "sha256:" + protocolDigest
	if tampered.Validate() == nil {
		t.Fatal("Cargo audit accepted an identity unrelated to the worker binary")
	}
}

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

func TestEtcdraftLocalSourceIsReadableAndBoundThroughExplicitMount(t *testing.T) {
	output, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "go.etcd.io/raft/v3").Output()
	if err != nil {
		t.Skipf("local etcd/raft module source is unavailable: %v", err)
	}
	moduleDirectory := strings.TrimSpace(string(output))
	wantModuleDirectory, err := filepath.Abs("../../suts/etcdraft")
	if err != nil {
		t.Fatal(err)
	}
	wantModuleDirectory, err = filepath.EvalSymlinks(wantModuleDirectory)
	if err != nil || moduleDirectory != wantModuleDirectory {
		t.Fatalf("Go did not resolve etcd/raft to the repository checkout: got=%q want=%q err=%v",
			moduleDirectory, wantModuleDirectory, err)
	}
	mountValues := []string{
		"repo=../..",
		"go.etcd.io/raft/v3@v3.6.0/=" + moduleDirectory,
	}
	mounts, err := prepareKnowledgeSourceMounts(mountValues)
	if err != nil || len(mounts) != 2 || mounts[0].ReferencePrefix != "" ||
		mounts[1].ReferencePrefix != "go.etcd.io/raft/v3@v3.6.0/" {
		t.Fatalf("explicit source mounts were not prepared: %#v/%v", mounts, err)
	}
	knowledge, _, _, err := loadEtcdraftAgenticAuthoringSource(etcdraftAgenticTestInputPath)
	if err != nil {
		t.Fatal(err)
	}
	reference := "go.etcd.io/raft/v3@v3.6.0/doc.go:First, you must read from the Node.Ready()"
	result, err := controlexperiment.ReadDeclaredKnowledgeSourceFromMounts(
		mounts, knowledge,
		controlexperiment.KnowledgeReadRequest{Reference: reference, MaxLines: 40},
	)
	if err != nil || result.Validate() != nil ||
		result.Status != controlexperiment.KnowledgeDiscoveryCompleted ||
		!strings.Contains(result.Text, "no messages be sent until the latest HardState has been persisted") {
		t.Fatalf("official etcd/raft Ready contract was not readable: %#v/%v", result, err)
	}
	composition, err := prepareAgenticEpisodeComposition(t.Context(), controlExperimentOptions{
		Target: "etcdraft-v2", InvestigationEpisodes: 1, SemanticInput: etcdraftAgenticTestInputPath,
		AgentKeyFile: "fixture-key.txt", AgentModel: openRouterFixtureModel,
		KnowledgeSourceMounts: mountValues,
	})
	if err != nil || len(composition.KnowledgeSourceMounts) != 2 ||
		composition.MethodSpec.SourceExposure.Mode != controlexperiment.AgenticSourceExposureRepositorySearchV3 ||
		len(composition.MethodSpec.SourceExposure.ReferencePrefixes) != 2 ||
		len(composition.MethodSpec.SourceExposure.SUTBindings) != 1 ||
		composition.MethodSpec.SourceExposure.SUTBindings[0].ModulePath != etcdraftLocalModulePath ||
		composition.MethodSpec.SourceExposure.SUTBindings[0].ContentDigest == "" ||
		composition.KnowledgeSourceMounts[1].SUTSource == nil ||
		composition.KnowledgeSourceMounts[1].Directory != moduleDirectory {
		t.Fatalf("Agentic composition lost explicit source mounts: %#v/%v",
			composition.KnowledgeSourceMounts, err)
	}
}

func TestEtcdraftLocalSourceBindingRejectsASeparateTree(t *testing.T) {
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "doc.go"), []byte("package raft\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareAgenticEpisodeComposition(t.Context(), controlExperimentOptions{
		Target: "etcdraft-v2", InvestigationEpisodes: 1, SemanticInput: etcdraftAgenticTestInputPath,
		AgentKeyFile: "fixture-key.txt", AgentModel: openRouterFixtureModel,
		KnowledgeSourceMounts: []string{etcdraftLocalReferencePrefix + "=" + other},
	})
	if err == nil || !strings.Contains(err.Error(), "AGENTIC_ETCDRAFT_SOURCE_MOUNT_MISMATCH") {
		t.Fatalf("a separate Agent-visible etcd/raft tree was accepted: %v", err)
	}
}

func TestOmnipaxosLocalSourceIsReadableAndBoundThroughExplicitMount(t *testing.T) {
	worker := buildOmnipaxosScenarioWorker(t)
	sourceDirectory, err := filepath.Abs("../../suts/omnipaxos")
	if err != nil {
		t.Fatal(err)
	}
	sourceDirectory, err = filepath.EvalSymlinks(sourceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	mountValues := []string{
		"repo=../..",
		omnipaxosLocalReferencePrefix + "=" + sourceDirectory,
	}
	mounts, err := prepareKnowledgeSourceMounts(mountValues)
	if err != nil || len(mounts) != 2 || mounts[0].ReferencePrefix != "" ||
		mounts[1].ReferencePrefix != omnipaxosLocalReferencePrefix {
		t.Fatalf("explicit OmniPaxos source mounts were not prepared: %#v/%v", mounts, err)
	}
	knowledge, _, _, err := loadOmnipaxosAgenticAuthoringSource(omnipaxosAgenticTestInputPath)
	if err != nil {
		t.Fatal(err)
	}
	reference := omnipaxosLocalReferencePrefix + "omnipaxos/src/omni_paxos.rs:pub fn tick"
	result, err := controlexperiment.ReadDeclaredKnowledgeSourceFromMounts(
		mounts, knowledge,
		controlexperiment.KnowledgeReadRequest{Reference: reference, MaxLines: 40},
	)
	if err != nil || result.Validate() != nil ||
		result.Status != controlexperiment.KnowledgeDiscoveryCompleted ||
		!strings.Contains(result.Text, "pub fn tick") {
		t.Fatalf("local OmniPaxos protocol source was not readable: %#v/%v", result, err)
	}
	composition, err := prepareAgenticEpisodeComposition(t.Context(), controlExperimentOptions{
		Target: "omnipaxos-v2", InvestigationEpisodes: 1,
		SemanticInput: omnipaxosAgenticTestInputPath, WorkerPath: worker,
		AgentKeyFile: "fixture-key.txt", AgentModel: openRouterFixtureModel,
		KnowledgeSourceMounts: mountValues,
	})
	if err != nil || len(composition.KnowledgeSourceMounts) != 2 ||
		composition.MethodSpec.SourceExposure.Mode != controlexperiment.AgenticSourceExposureRepositorySearchV3 ||
		len(composition.MethodSpec.SourceExposure.ReferencePrefixes) != 2 ||
		len(composition.MethodSpec.SourceExposure.SUTBindings) != 1 ||
		composition.MethodSpec.SourceExposure.SUTBindings[0].ModulePath != omnipaxosLocalModulePath ||
		composition.MethodSpec.SourceExposure.SUTBindings[0].ModuleVersion != omnipaxosLocalModuleVersion ||
		composition.MethodSpec.SourceExposure.SUTBindings[0].ContentDigest == "" ||
		composition.KnowledgeSourceMounts[1].SUTSource == nil ||
		composition.KnowledgeSourceMounts[1].Directory != sourceDirectory {
		t.Fatalf("Agentic composition lost the local OmniPaxos source binding: %#v/%v",
			composition.KnowledgeSourceMounts, err)
	}
}

func TestOmnipaxosLocalSourceBindingRejectsASeparateTree(t *testing.T) {
	worker := buildOmnipaxosScenarioWorker(t)
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "Cargo.toml"), []byte("[workspace]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareAgenticEpisodeComposition(t.Context(), controlExperimentOptions{
		Target: "omnipaxos-v2", InvestigationEpisodes: 1,
		SemanticInput: omnipaxosAgenticTestInputPath, WorkerPath: worker,
		AgentKeyFile: "fixture-key.txt", AgentModel: openRouterFixtureModel,
		KnowledgeSourceMounts: []string{omnipaxosLocalReferencePrefix + "=" + other},
	})
	if err == nil || !strings.Contains(err.Error(), "AGENTIC_OMNIPAXOS_SOURCE_MOUNT_MISMATCH") {
		t.Fatalf("a separate Agent-visible OmniPaxos tree was accepted: %v", err)
	}
}

func TestBoundKnowledgeSourceChangeIsDetected(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "source.go")
	if err := os.WriteFile(path, []byte("package source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := sutbuild.SourceTreeDigest(directory)
	if err != nil {
		t.Fatal(err)
	}
	mounts := []controlexperiment.KnowledgeSourceMount{{
		ReferencePrefix: "example.test/sut@v1.0.0/", Directory: directory,
		SUTSource: &controlexperiment.AgenticSUTSourceBinding{
			ReferencePrefix: "example.test/sut@v1.0.0/", ModulePath: "example.test/sut",
			ModuleVersion: "v1.0.0", ContentDigest: digest,
		},
	}}
	if err := verifyKnowledgeSourceMountIdentities(mounts); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyKnowledgeSourceMountIdentities(mounts); err == nil {
		t.Fatal("a changed Agent-visible SUT source tree retained its method identity")
	}
}

func TestEtcdraftSealedBinaryDoesNotGuessAgentSourceIdentity(t *testing.T) {
	original := etcdraftv2.SUTBuildIdentity
	etcdraftv2.SUTBuildIdentity = "sut-0123456789abcdef"
	t.Cleanup(func() { etcdraftv2.SUTBuildIdentity = original })
	withoutSource, err := bindEtcdraftLocalSUTSource(context.Background(), "", etcdraftAgenticTestInputPath, nil)
	if err != nil || len(withoutSource) != 0 {
		t.Fatalf("sealed execution without source exposure was rejected: %#v/%v", withoutSource, err)
	}
	_, err = bindEtcdraftLocalSUTSource(context.Background(), "", etcdraftAgenticTestInputPath, []controlexperiment.KnowledgeSourceMount{{
		ReferencePrefix: etcdraftLocalReferencePrefix, Directory: t.TempDir(),
	}})
	if err == nil || !strings.Contains(err.Error(), "AGENTIC_ETCDRAFT_AUDITED_SOURCE_BINDING_REQUIRED") {
		t.Fatalf("sealed binary guessed an Agent-visible SUT source identity: %v", err)
	}
}

func TestKnowledgeSourceMountPreparationRejectsAmbiguousOrMissingRoots(t *testing.T) {
	if _, err := prepareKnowledgeSourceMounts([]string{"repo=/definitely/missing/consensus-atlas"}); err == nil {
		t.Fatal("missing source root was accepted")
	}
	if _, err := prepareKnowledgeSourceMounts([]string{"repo=../..", "repo=../.."}); err == nil {
		t.Fatal("duplicate repository mounts were accepted")
	}
}

func TestExplicitRepositoryRootDecouplesLocalSUTFromSemanticInputLocation(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mounts, err := bindEtcdraftLocalSUTSource(
		context.Background(), repositoryRoot, filepath.Join(t.TempDir(), "plan.json"), nil,
	)
	if err != nil || len(mounts) != 0 {
		t.Fatalf("explicit repository root did not bind local etcd/raft: mounts=%v err=%v", mounts, err)
	}
	if _, err := consensusAtlasRepositoryRoot(t.TempDir(), "ignored.json"); err == nil || err.Error() != "AGENTIC_REPOSITORY_ROOT_INVALID" {
		t.Fatalf("non-repository explicit root was accepted: %v", err)
	}
}

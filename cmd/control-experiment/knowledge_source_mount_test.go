package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftOfficialSourceIsReadableThroughExplicitMount(t *testing.T) {
	output, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "go.etcd.io/raft/v3").Output()
	if err != nil {
		t.Skipf("official etcd/raft module source is unavailable: %v", err)
	}
	moduleDirectory := strings.TrimSpace(string(output))
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
		Target: "etcdraft-v2", SemanticInput: etcdraftAgenticTestInputPath,
		AgentKeyFile: "fixture-key.txt", AgentModel: openRouterFixtureModel,
		KnowledgeSourceMounts: mountValues,
	})
	if err != nil || len(composition.KnowledgeSourceMounts) != 2 ||
		composition.KnowledgeSourceMounts[1].Directory != moduleDirectory {
		t.Fatalf("Agentic composition lost explicit source mounts: %#v/%v",
			composition.KnowledgeSourceMounts, err)
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

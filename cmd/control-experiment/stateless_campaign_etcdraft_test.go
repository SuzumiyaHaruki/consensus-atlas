package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestM523R3EtcdraftStatelessMethodSequencesArePredeclared(t *testing.T) {
	canonical, err := newEtcdraftStatelessCampaignMethods(
		etcdraftStatelessCanonicalCampaignStrategy, 2, 1,
	)
	if err != nil || len(canonical) != 2 || canonical[0] != canonical[1] ||
		canonical[0].Strategy != controlexperiment.StatelessTraversalCanonical {
		t.Fatalf("canonical repeat sequence drifted: %#v/%v", canonical, err)
	}
	uniform, err := newEtcdraftStatelessCampaignMethods(
		etcdraftStatelessUniformCampaignStrategy, 3, 1,
	)
	if err != nil || len(uniform) != 3 || uniform[0].SeedHex != "01" ||
		uniform[1].SeedHex != "02" || uniform[2].SeedHex != "03" ||
		uniform[0].Digest == uniform[1].Digest {
		t.Fatalf("uniform sequence drifted: %#v/%v", uniform, err)
	}
	if _, err := newEtcdraftStatelessCampaignMethods("unknown", 1, 1); err == nil {
		t.Fatal("unknown stateless Campaign strategy was accepted")
	}
	if _, err := newEtcdraftStatelessCampaignMethods(
		etcdraftStatelessUniformCampaignStrategy, 2, ^uint64(0),
	); err == nil {
		t.Fatal("overflowing stateless Campaign seed sequence was accepted")
	}
}

func TestM523R3EtcdraftUniformAttemptUsesFrozenCorpusAndQualifiedExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()
	inputs, err := loadEtcdraftStatelessCampaignInputs(ctx, "../../"+m523gCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	methods, err := newEtcdraftStatelessCampaignMethods(
		etcdraftStatelessUniformCampaignStrategy, 1, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := newEtcdraftStatelessCampaignSpec(inputs, methods)
	if err != nil {
		t.Fatal(err)
	}
	config, err := controlexperiment.NewStatelessCampaignConfig(
		"etcdraft-stateless-uniform-test", spec, 360_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	head, err := controlexperiment.NewCampaignCheckpoint(config)
	if err != nil {
		t.Fatal(err)
	}
	request, err := controlexperiment.NewCampaignAttemptRequest(config, head)
	if err != nil {
		t.Fatal(err)
	}
	execution := executeEtcdraftStatelessCampaignAttempt(ctx, request, spec, inputs)
	if execution.Failure != nil || execution.Discovery == nil {
		t.Fatalf("real stateless attempt failed: %#v", execution.Failure)
	}
	discovery := execution.Discovery
	if discovery.ValidateStructure() != nil || discovery.CorpusDigest != inputs.corpus.Digest ||
		discovery.MethodDigest != methods[0].Digest ||
		discovery.QualifiedExecutionAttempts != len(inputs.corpus.Roots)*etcdraftStatelessCampaignItemsPerRoot ||
		discovery.SearchWork.TotalWorkUnits == 0 ||
		discovery.QualifiedExecutionWork.Primary.WorkUnits == 0 ||
		discovery.QualifiedExecutionWork.Replay.WorkUnits == 0 {
		t.Fatalf("real stateless discovery lost trusted evidence: %#v", discovery)
	}
	artifact, err := controlexperiment.NewStatelessCampaignAttemptArtifact(request, spec, execution)
	if err != nil || artifact.ValidateInputs(request) != nil ||
		artifact.Outcome != controlexperiment.CampaignAttemptCompleted {
		t.Fatalf("real stateless attempt did not fit durable artifact: %#v/%v", artifact, err)
	}
}

func TestM523R3EtcdraftStatelessCLIResumesAndStrictlyReadsArtifacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()
	corpusPath := "../../" + m523gCorpusPath
	inputs, err := loadEtcdraftStatelessCampaignInputs(ctx, corpusPath)
	if err != nil {
		t.Fatal(err)
	}
	methods, err := newEtcdraftStatelessCampaignMethods(
		etcdraftStatelessCanonicalCampaignStrategy, 1, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := newEtcdraftStatelessCampaignSpec(inputs, methods)
	if err != nil {
		t.Fatal(err)
	}
	config, err := controlexperiment.NewStatelessCampaignConfig(
		"etcdraft-stateless-canonical-campaign", spec, 360_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	directory := filepath.Join(root, "campaign")
	if _, err := controlexperiment.CreateCampaignDirectory(directory, config); err != nil {
		t.Fatal(err)
	}
	summaryPath := filepath.Join(root, "summary.json")
	observationPath := filepath.Join(root, "observation.json")
	args := []string{
		"-strategy", etcdraftStatelessCanonicalCampaignStrategy,
		"-stateless-corpus", corpusPath,
		"-campaign-dir", directory,
		"-campaign-resume",
		"-campaign-attempts", "1",
		"-campaign-wall-clock-ms", "360000",
		"-campaign-observation-out", observationPath,
		"-out", summaryPath,
	}
	var stdout bytes.Buffer
	if err := run(ctx, args, &stdout); err != nil {
		t.Fatal(err)
	}
	summary := readEtcdraftCampaignSummary(t, summaryPath)
	encoded, err := os.ReadFile(observationPath)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := controlexperiment.DecodeStatelessCampaignObservation(encoded)
	if err != nil || observation.SummaryDigest != summary.Digest || observation.AttemptCount != 1 ||
		observation.Completed != 1 || observation.Failed != 0 ||
		observation.Attempts[0].QualifiedExecutionAttempts != 18 ||
		observation.Attempts[0].Method.Digest != methods[0].Digest ||
		!strings.Contains(stdout.String(), "status=stopped attempts=1") ||
		bytes.Contains(bytes.ToLower(encoded), []byte("coverage")) {
		t.Fatalf("Stateless CLI observation drifted: %#v stdout=%q err=%v", observation, stdout.String(), err)
	}
	recovered, err := controlexperiment.RecoverCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	request, err := controlexperiment.NewCampaignAttemptRequest(config, recovered.Checkpoints[0])
	if err != nil {
		t.Fatal(err)
	}
	artifactBytes, err := recovered.ReadAttemptArtifact(1)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := controlexperiment.DecodeStatelessCampaignAttemptArtifact(artifactBytes, request)
	if err != nil || artifact.Discovery == nil ||
		artifact.Discovery.Digest != observation.Attempts[0].DiscoveryDigest {
		t.Fatalf("durable Stateless artifact failed strict reread: %#v/%v", artifact, err)
	}
	tampered := append([]byte(nil), encoded...)
	tampered = append(tampered[:len(tampered)-2], []byte(`,"coverage_percent":100}`)...)
	if _, err := controlexperiment.DecodeStatelessCampaignObservation(tampered); err == nil {
		t.Fatal("Stateless observation accepted an invented coverage percentage")
	}
}

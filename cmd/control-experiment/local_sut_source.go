package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
	raft "go.etcd.io/raft/v3"
)

const (
	etcdraftLocalModulePath        = "go.etcd.io/raft/v3"
	etcdraftLocalModuleVersion     = "v3.6.0"
	etcdraftLocalReferencePrefix   = "go.etcd.io/raft/v3@v3.6.0/"
	etcdraftLocalReplacePath       = "./suts/etcdraft"
	etcdraftAdapterReferencePrefix = "repo/adapters/etcdraftv2/"

	omnipaxosLocalModulePath        = "crates.io/omnipaxos"
	omnipaxosLocalModuleVersion     = "0.2.2"
	omnipaxosLocalReferencePrefix   = "crates.io/omnipaxos@0.2.2/"
	omnipaxosAdapterReferencePrefix = "repo/adapters/omnipaxosv2/"
)

type listedGoModule struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
	Dir     string `json:"Dir"`
	Replace *struct {
		Path string `json:"Path"`
		Dir  string `json:"Dir"`
	} `json:"Replace"`
}

type cargoMetadata struct {
	Packages []struct {
		Name         string `json:"name"`
		Version      string `json:"version"`
		ManifestPath string `json:"manifest_path"`
		Dependencies []struct {
			Name   string  `json:"name"`
			Source *string `json:"source"`
			Req    string  `json:"req"`
			Path   *string `json:"path"`
		} `json:"dependencies"`
	} `json:"packages"`
	TargetDirectory string `json:"target_directory"`
	WorkspaceRoot   string `json:"workspace_root"`
}

// bindEtcdraftLocalSUTSource proves two distinct facts before an Agentic run:
// this executable was linked through the repository-local module replacement,
// and any Agent-visible etcd/raft mount is that same resolved source directory.
// The tree digest, rather than the machine-local directory, enters MethodSpec.
func bindEtcdraftLocalSUTSource(
	ctx context.Context,
	explicitRepositoryRoot string,
	semanticInput string,
	mounts []controlexperiment.KnowledgeSourceMount,
) ([]controlexperiment.KnowledgeSourceMount, error) {
	if etcdraftv2.SUTBuildIdentity != "local-source-unsealed:etcdraft" {
		for _, mount := range mounts {
			if mount.ReferencePrefix == etcdraftLocalReferencePrefix {
				return nil, errors.New("AGENTIC_ETCDRAFT_AUDITED_SOURCE_BINDING_REQUIRED")
			}
		}
		// A sealed binary may run without SUT source exposure. Its source and
		// binary identity remain governed by the formal build audit.
		return append([]controlexperiment.KnowledgeSourceMount(nil), mounts...), nil
	}
	repositoryRoot, err := consensusAtlasRepositoryRoot(explicitRepositoryRoot, semanticInput)
	if err != nil || !executableUsesLocalEtcdraftReplacement() {
		return nil, errors.New("AGENTIC_ETCDRAFT_LOCAL_SUT_EXECUTION_UNBOUND")
	}
	module, err := resolveEtcdraftModule(ctx, repositoryRoot)
	if err != nil {
		return nil, err
	}
	compiledModule, err := executableEtcdraftModuleDirectory()
	if err != nil || compiledModule != module.Dir {
		return nil, errors.New("AGENTIC_ETCDRAFT_COMPILED_SOURCE_MISMATCH")
	}
	digest, err := sutbuild.SourceTreeDigest(module.Dir)
	if err != nil {
		return nil, errors.New("AGENTIC_ETCDRAFT_LOCAL_SUT_DIGEST_FAILED")
	}
	binding := controlexperiment.AgenticSUTSourceBinding{
		ReferencePrefix: etcdraftLocalReferencePrefix,
		ModulePath:      etcdraftLocalModulePath,
		ModuleVersion:   etcdraftLocalModuleVersion,
		ContentDigest:   digest,
	}
	result := append([]controlexperiment.KnowledgeSourceMount(nil), mounts...)
	for index := range result {
		if result[index].ReferencePrefix != etcdraftLocalReferencePrefix {
			continue
		}
		mounted, mountErr := canonicalDirectory(result[index].Directory)
		resolved, resolvedErr := canonicalDirectory(module.Dir)
		if mountErr != nil || resolvedErr != nil || mounted != resolved {
			return nil, errors.New("AGENTIC_ETCDRAFT_SOURCE_MOUNT_MISMATCH")
		}
		copyBinding := binding
		result[index].SUTSource = &copyBinding
		result[index].SearchRole = controlexperiment.KnowledgeSourceSearchSUT
	}
	result, err = appendTargetAdapterKnowledgeMount(
		result, repositoryRoot, "adapters/etcdraftv2", etcdraftAdapterReferencePrefix,
	)
	if err != nil {
		return nil, err
	}
	if len(result) > 0 && controlexperiment.ValidateKnowledgeSourceMounts(result) != nil {
		return nil, errors.New("AGENTIC_ETCDRAFT_SOURCE_BINDING_INVALID")
	}
	return result, nil
}

func resolveEtcdraftModule(ctx context.Context, repositoryRoot string) (listedGoModule, error) {
	command := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-m", "-json", etcdraftLocalModulePath)
	command.Dir = repositoryRoot
	command.Env = withoutEnvironmentKey(os.Environ(), "GOWORK")
	command.Env = append(command.Env, "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		return listedGoModule{}, errors.New("AGENTIC_ETCDRAFT_LOCAL_MODULE_UNRESOLVED")
	}
	var module listedGoModule
	if json.Unmarshal(output, &module) != nil || module.Path != etcdraftLocalModulePath ||
		module.Version != etcdraftLocalModuleVersion || module.Replace == nil ||
		module.Replace.Path != etcdraftLocalReplacePath || module.Replace.Dir == "" ||
		module.Dir == "" {
		return listedGoModule{}, errors.New("AGENTIC_ETCDRAFT_LOCAL_MODULE_IDENTITY_MISMATCH")
	}
	want, err := canonicalDirectory(filepath.Join(repositoryRoot, "suts", "etcdraft"))
	if err != nil {
		return listedGoModule{}, errors.New("AGENTIC_ETCDRAFT_LOCAL_MODULE_IDENTITY_MISMATCH")
	}
	directory, err := canonicalDirectory(module.Dir)
	if err != nil || directory != want {
		return listedGoModule{}, errors.New("AGENTIC_ETCDRAFT_LOCAL_MODULE_IDENTITY_MISMATCH")
	}
	module.Dir = directory
	return module, nil
}

func executableUsesLocalEtcdraftReplacement() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, dependency := range info.Deps {
		if dependency.Path == etcdraftLocalModulePath && dependency.Version == etcdraftLocalModuleVersion &&
			dependency.Replace != nil && dependency.Replace.Path == etcdraftLocalReplacePath {
			return true
		}
	}
	return false
}

// executableEtcdraftModuleDirectory reads the source location recorded for a
// concrete etcd/raft function in this executable. BuildInfo alone preserves
// only the relative replace directive, which cannot distinguish two checkouts
// that both use ./suts/etcdraft.
func executableEtcdraftModuleDirectory() (string, error) {
	function := runtime.FuncForPC(reflect.ValueOf(raft.NewRawNode).Pointer())
	if function == nil {
		return "", errors.New("AGENTIC_ETCDRAFT_COMPILED_SOURCE_UNAVAILABLE")
	}
	file, _ := function.FileLine(function.Entry())
	if file == "" || !filepath.IsAbs(file) {
		return "", errors.New("AGENTIC_ETCDRAFT_COMPILED_SOURCE_UNAVAILABLE")
	}
	directory, err := canonicalDirectory(filepath.Dir(file))
	if err != nil {
		return "", errors.New("AGENTIC_ETCDRAFT_COMPILED_SOURCE_UNAVAILABLE")
	}
	// NewRawNode currently lives at the module root. Keep the check explicit so
	// a future upstream move fails closed instead of silently deriving a wrong
	// checkout identity.
	if filepath.Base(file) != "rawnode.go" {
		return "", errors.New("AGENTIC_ETCDRAFT_COMPILED_SOURCE_LAYOUT_CHANGED")
	}
	return directory, nil
}

// bindOmnipaxosLocalSUTSource is the Rust-worker counterpart of the Go module
// binding above. The worker remains an external process, so the trusted
// preparation first proves Cargo resolves both protocol crates from the pinned
// repository checkout and rebuilds the canonical worker before accepting it.
// An optional Agent-visible source mount must name that same checkout; its
// machine-independent tree digest then enters MethodSpec.
func bindOmnipaxosLocalSUTSource(
	ctx context.Context,
	explicitRepositoryRoot string,
	semanticInput string,
	workerPath string,
	mounts []controlexperiment.KnowledgeSourceMount,
) ([]controlexperiment.KnowledgeSourceMount, error) {
	repositoryRoot, err := consensusAtlasRepositoryRoot(explicitRepositoryRoot, semanticInput)
	if err != nil || workerPath == "" {
		return nil, errors.New("AGENTIC_OMNIPAXOS_LOCAL_SUT_EXECUTION_UNBOUND")
	}
	metadata, err := resolveOmnipaxosCargoMetadata(ctx, repositoryRoot)
	if err != nil {
		return nil, err
	}
	manifest := filepath.Join(repositoryRoot, "adapters", "omnipaxosv2", "worker", "Cargo.toml")
	command := exec.CommandContext(
		ctx, "cargo", "build", "--locked", "--offline", "--quiet", "--manifest-path", manifest,
	)
	command.Dir = repositoryRoot
	if output, buildErr := command.CombinedOutput(); buildErr != nil {
		return nil, fmt.Errorf("AGENTIC_OMNIPAXOS_LOCAL_WORKER_BUILD_FAILED: %w: %s", buildErr, output)
	}
	wantWorker, err := canonicalDirectory(filepath.Join(
		metadata.TargetDirectory, "debug", "consensus-atlas-omnipaxos-worker",
	))
	if err != nil {
		return nil, errors.New("AGENTIC_OMNIPAXOS_LOCAL_WORKER_IDENTITY_MISMATCH")
	}
	gotWorker, err := canonicalDirectory(workerPath)
	if err != nil || gotWorker != wantWorker {
		return nil, errors.New("AGENTIC_OMNIPAXOS_LOCAL_WORKER_IDENTITY_MISMATCH")
	}
	sourceRoot, err := canonicalDirectory(filepath.Join(repositoryRoot, "suts", "omnipaxos"))
	if err != nil {
		return nil, errors.New("AGENTIC_OMNIPAXOS_LOCAL_SUT_IDENTITY_MISMATCH")
	}
	digest, err := sutbuild.SourceTreeDigest(sourceRoot)
	if err != nil {
		return nil, errors.New("AGENTIC_OMNIPAXOS_LOCAL_SUT_DIGEST_FAILED")
	}
	binding := controlexperiment.AgenticSUTSourceBinding{
		ReferencePrefix: omnipaxosLocalReferencePrefix,
		ModulePath:      omnipaxosLocalModulePath,
		ModuleVersion:   omnipaxosLocalModuleVersion,
		ContentDigest:   digest,
	}
	result := append([]controlexperiment.KnowledgeSourceMount(nil), mounts...)
	for index := range result {
		if result[index].ReferencePrefix != omnipaxosLocalReferencePrefix {
			continue
		}
		mounted, mountErr := canonicalDirectory(result[index].Directory)
		if mountErr != nil || mounted != sourceRoot {
			return nil, errors.New("AGENTIC_OMNIPAXOS_SOURCE_MOUNT_MISMATCH")
		}
		copyBinding := binding
		result[index].SUTSource = &copyBinding
		result[index].SearchRole = controlexperiment.KnowledgeSourceSearchSUT
	}
	result, err = appendTargetAdapterKnowledgeMount(
		result, repositoryRoot, "adapters/omnipaxosv2", omnipaxosAdapterReferencePrefix,
	)
	if err != nil {
		return nil, err
	}
	if len(result) > 0 && controlexperiment.ValidateKnowledgeSourceMounts(result) != nil {
		return nil, errors.New("AGENTIC_OMNIPAXOS_SOURCE_BINDING_INVALID")
	}
	return result, nil
}

func appendTargetAdapterKnowledgeMount(
	mounts []controlexperiment.KnowledgeSourceMount,
	repositoryRoot string,
	relativeDirectory string,
	referencePrefix string,
) ([]controlexperiment.KnowledgeSourceMount, error) {
	root, err := canonicalDirectory(repositoryRoot)
	if err != nil {
		return nil, errors.New("AGENTIC_TARGET_ADAPTER_SOURCE_INVALID")
	}
	authorized := false
	for _, mount := range mounts {
		if mount.ReferencePrefix != "" {
			continue
		}
		directory, directoryErr := canonicalDirectory(mount.Directory)
		if directoryErr == nil && directory == root {
			authorized = true
			break
		}
	}
	if !authorized {
		return append([]controlexperiment.KnowledgeSourceMount(nil), mounts...), nil
	}
	adapterDirectory, err := canonicalDirectory(filepath.Join(root, relativeDirectory))
	if err != nil {
		return nil, errors.New("AGENTIC_TARGET_ADAPTER_SOURCE_INVALID")
	}
	for index := range mounts {
		if mounts[index].ReferencePrefix != referencePrefix {
			continue
		}
		directory, directoryErr := canonicalDirectory(mounts[index].Directory)
		if directoryErr != nil || directory != adapterDirectory {
			return nil, errors.New("AGENTIC_TARGET_ADAPTER_SOURCE_MISMATCH")
		}
		result := append([]controlexperiment.KnowledgeSourceMount(nil), mounts...)
		result[index].SearchRole = controlexperiment.KnowledgeSourceSearchAdapter
		return result, nil
	}
	result := append([]controlexperiment.KnowledgeSourceMount(nil), mounts...)
	result = append(result, controlexperiment.KnowledgeSourceMount{
		ReferencePrefix: referencePrefix,
		Directory:       adapterDirectory,
		SearchRole:      controlexperiment.KnowledgeSourceSearchAdapter,
	})
	return result, nil
}

func resolveOmnipaxosCargoMetadata(
	ctx context.Context,
	repositoryRoot string,
) (cargoMetadata, error) {
	manifest, err := canonicalDirectory(filepath.Join(
		repositoryRoot, "adapters", "omnipaxosv2", "worker", "Cargo.toml",
	))
	if err != nil {
		return cargoMetadata{}, errors.New("AGENTIC_OMNIPAXOS_LOCAL_CARGO_METADATA_INVALID")
	}
	command := exec.CommandContext(
		ctx, "cargo", "metadata", "--locked", "--offline", "--format-version", "1", "--no-deps",
		"--manifest-path", manifest,
	)
	command.Dir = repositoryRoot
	output, err := command.Output()
	if err != nil {
		return cargoMetadata{}, errors.New("AGENTIC_OMNIPAXOS_LOCAL_CARGO_METADATA_UNRESOLVED")
	}
	var metadata cargoMetadata
	if json.Unmarshal(output, &metadata) != nil || len(metadata.Packages) != 1 ||
		metadata.Packages[0].Name != "consensus-atlas-omnipaxos-worker" ||
		metadata.Packages[0].ManifestPath == "" || metadata.TargetDirectory == "" {
		return cargoMetadata{}, errors.New("AGENTIC_OMNIPAXOS_LOCAL_CARGO_METADATA_INVALID")
	}
	gotManifest, err := canonicalDirectory(metadata.Packages[0].ManifestPath)
	if err != nil || gotManifest != manifest {
		return cargoMetadata{}, errors.New("AGENTIC_OMNIPAXOS_LOCAL_CARGO_METADATA_INVALID")
	}
	wantDependencies := map[string]string{
		"omnipaxos":         filepath.Join(repositoryRoot, "suts", "omnipaxos", "omnipaxos"),
		"omnipaxos_storage": filepath.Join(repositoryRoot, "suts", "omnipaxos", "omnipaxos_storage"),
	}
	found := make(map[string]bool, len(wantDependencies))
	for _, dependency := range metadata.Packages[0].Dependencies {
		want, ok := wantDependencies[dependency.Name]
		if !ok {
			continue
		}
		if dependency.Source != nil || dependency.Path == nil || dependency.Req != "=0.2.2" {
			return cargoMetadata{}, errors.New("AGENTIC_OMNIPAXOS_LOCAL_CARGO_DEPENDENCY_MISMATCH")
		}
		got, pathErr := canonicalDirectory(*dependency.Path)
		want, wantErr := canonicalDirectory(want)
		if pathErr != nil || wantErr != nil || got != want {
			return cargoMetadata{}, errors.New("AGENTIC_OMNIPAXOS_LOCAL_CARGO_DEPENDENCY_MISMATCH")
		}
		found[dependency.Name] = true
	}
	if !found["omnipaxos"] || !found["omnipaxos_storage"] {
		return cargoMetadata{}, errors.New("AGENTIC_OMNIPAXOS_LOCAL_CARGO_DEPENDENCY_MISSING")
	}
	metadata.TargetDirectory, err = canonicalDirectory(metadata.TargetDirectory)
	if err != nil {
		return cargoMetadata{}, errors.New("AGENTIC_OMNIPAXOS_LOCAL_CARGO_METADATA_INVALID")
	}
	return metadata, nil
}

func consensusAtlasRepositoryRoot(explicit, semanticInput string) (string, error) {
	if explicit != "" {
		root, err := canonicalDirectory(explicit)
		if err != nil || !isConsensusAtlasRepositoryRoot(root) {
			return "", errors.New("AGENTIC_REPOSITORY_ROOT_INVALID")
		}
		return root, nil
	}
	path, err := filepath.Abs(semanticInput)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(path)
	for {
		if isConsensusAtlasRepositoryRoot(directory) {
			return canonicalDirectory(directory)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("AGENTIC_REPOSITORY_ROOT_NOT_FOUND")
		}
		directory = parent
	}
}

func isConsensusAtlasRepositoryRoot(directory string) bool {
	data, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	return err == nil && strings.HasPrefix(
		string(data), "module github.com/SuzumiyaHaruki/consensus-atlas\n",
	)
}

func canonicalDirectory(directory string) (string, error) {
	abs, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func withoutEnvironmentKey(environment []string, key string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return result
}

func verifyKnowledgeSourceMountIdentities(
	mounts []controlexperiment.KnowledgeSourceMount,
) error {
	for _, mount := range mounts {
		if mount.SUTSource == nil {
			continue
		}
		digest, err := sutbuild.SourceTreeDigest(mount.Directory)
		if err != nil || digest != mount.SUTSource.ContentDigest {
			return errors.New("AGENTIC_SUT_SOURCE_CHANGED_DURING_RUN")
		}
	}
	return nil
}

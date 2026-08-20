package sutbuild

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const CargoSpecVersion = 1

// CargoSpec is the target-owned build pack for an external worker binary. It
// feeds the existing Audit consumed by formal evaluation; it is not a second
// evidence ledger. ProtocolSourceDigest binds the editable OmniPaxos checkout,
// while WorkerSourceDigest binds the adapter-side executable source and lockfile.
type CargoSpec struct {
	Version              int            `json:"version"`
	ID                   string         `json:"id"`
	TrialID              string         `json:"trial_id"`
	Module               ModuleIdentity `json:"module"`
	ProtocolSourceRoot   string         `json:"protocol_source_root"`
	ProtocolSourceDigest string         `json:"protocol_source_digest"`
	WorkerSourceRoot     string         `json:"worker_source_root"`
	WorkerSourceDigest   string         `json:"worker_source_digest"`
	ManifestPath         string         `json:"manifest_path"`
	Package              string         `json:"package"`
	BinaryName           string         `json:"binary_name"`
	CommandAllowlist     []string       `json:"command_allowlist"`
	OutputPath           string         `json:"output_path"`
}

func (spec CargoSpec) Validate() error {
	if spec.Version != CargoSpecVersion || spec.ID == "" || spec.TrialID == "" ||
		spec.Module.Path == "" || spec.Module.Version == "" || spec.Package == "" ||
		spec.BinaryName == "" || !isDigest(spec.ProtocolSourceDigest) ||
		!isDigest(spec.WorkerSourceDigest) || spec.OutputPath == "" {
		return errors.New("Cargo build spec requires complete identities and source digests")
	}
	for _, path := range []string{
		spec.ProtocolSourceRoot, spec.WorkerSourceRoot, spec.ManifestPath, spec.OutputPath,
	} {
		if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path ||
			strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			return errors.New("Cargo build spec paths must be clean repository-relative paths")
		}
	}
	want := []string{
		CommandCargoBuild, CommandCargoMetadata, CommandCargoVersion, CommandRustcVersion,
	}
	got := append([]string(nil), spec.CommandAllowlist...)
	sort.Strings(got)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		return fmt.Errorf("Cargo command allowlist must be exactly %v", want)
	}
	return nil
}

type cargoBuildMetadata struct {
	Packages []struct {
		Name         string `json:"name"`
		Version      string `json:"version"`
		ManifestPath string `json:"manifest_path"`
		Dependencies []struct {
			Name   string  `json:"name"`
			Source *string `json:"source"`
			Path   *string `json:"path"`
		} `json:"dependencies"`
	} `json:"packages"`
}

// BuildCargo builds a locked, offline worker from repository-local protocol
// sources and returns the same Audit type used by the Go build path.
func BuildCargo(repoRoot string, spec CargoSpec) (Audit, error) {
	if err := spec.Validate(); err != nil {
		return Audit{}, err
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return Audit{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Audit{}, err
	}
	protocolRoot, err := resolveCargoBuildPath(root, spec.ProtocolSourceRoot, true)
	if err != nil {
		return Audit{}, err
	}
	workerRoot, err := resolveCargoBuildPath(root, spec.WorkerSourceRoot, true)
	if err != nil {
		return Audit{}, err
	}
	manifest, err := resolveCargoBuildPath(root, spec.ManifestPath, false)
	if err != nil || filepath.Dir(manifest) != workerRoot {
		return Audit{}, errors.New("Cargo manifest is not the declared worker source root")
	}
	protocolDigest, err := digestTree(protocolRoot)
	if err != nil || protocolDigest != spec.ProtocolSourceDigest {
		return Audit{}, errors.New("Cargo protocol source digest does not match build spec")
	}
	workerDigest, err := digestTree(workerRoot)
	if err != nil || workerDigest != spec.WorkerSourceDigest {
		return Audit{}, errors.New("Cargo worker source digest does not match build spec")
	}
	outputPath, err := resolveOutput(root, spec.OutputPath)
	if err != nil {
		return Audit{}, err
	}
	if _, statErr := os.Lstat(outputPath); statErr == nil {
		return Audit{}, fmt.Errorf("refusing to overwrite existing binary %s", outputPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Audit{}, statErr
	}

	specDigest, err := digestJSON(spec)
	if err != nil {
		return Audit{}, err
	}
	buildWorkRoot := filepath.Join(root, "artifacts", "build-work")
	if err := os.MkdirAll(buildWorkRoot, 0o700); err != nil {
		return Audit{}, err
	}
	temporary := filepath.Join(buildWorkRoot, "cargo-"+specDigest[:16])
	if _, statErr := os.Lstat(temporary); statErr == nil {
		return Audit{}, fmt.Errorf("refusing to reuse existing Cargo build directory %s", temporary)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Audit{}, statErr
	}
	if err := os.Mkdir(temporary, 0o700); err != nil {
		return Audit{}, err
	}
	defer os.RemoveAll(temporary)
	targetDirectory := filepath.Join(temporary, "target")

	logicalEnvironment := []string{"CARGO_NET_OFFLINE=true", "CARGO_TARGET_DIR=<generated>"}
	commands := []CommandAudit{
		{Identity: CommandCargoVersion, Executable: "cargo", LogicalArgs: []string{"--version"},
			Environment: logicalEnvironment, CommandPolicy: "exact-allowlist-v1"},
		{Identity: CommandRustcVersion, Executable: "rustc", LogicalArgs: []string{"--version"},
			Environment: logicalEnvironment, CommandPolicy: "exact-allowlist-v1"},
		{Identity: CommandCargoMetadata, Executable: "cargo", LogicalArgs: []string{
			"metadata", "--locked", "--offline", "--format-version", "1", "--no-deps",
			"--manifest-path", spec.ManifestPath,
		}, Environment: logicalEnvironment, CommandPolicy: "exact-allowlist-v1"},
		{Identity: CommandCargoBuild, Executable: "cargo", LogicalArgs: []string{
			"build", "--locked", "--offline", "--quiet", "--manifest-path", spec.ManifestPath,
			"--target-dir", "<generated>", "--package", spec.Package,
		}, Environment: logicalEnvironment, CommandPolicy: "exact-allowlist-v1"},
	}
	for index := range commands {
		commands[index].Digest, err = commandDigest(commands[index])
		if err != nil {
			return Audit{}, err
		}
	}
	actualEnvironment := cargoBuildEnvironment(os.Environ(), targetDirectory)
	cargoVersion, err := runAuditedCommand(root, actualEnvironment, "cargo", "--version")
	if err != nil {
		return Audit{}, err
	}
	rustcVersion, err := runAuditedCommand(root, actualEnvironment, "rustc", "--version")
	if err != nil {
		return Audit{}, err
	}
	metadataBytes, err := runAuditedCommand(root, actualEnvironment, "cargo",
		"metadata", "--locked", "--offline", "--format-version", "1", "--no-deps",
		"--manifest-path", manifest,
	)
	if err != nil {
		return Audit{}, err
	}
	if err := validateCargoBuildMetadata(metadataBytes, spec, manifest, protocolRoot); err != nil {
		return Audit{}, err
	}
	if _, err := runAuditedCommand(root, actualEnvironment, "cargo",
		"build", "--locked", "--offline", "--quiet", "--manifest-path", manifest,
		"--target-dir", targetDirectory, "--package", spec.Package,
	); err != nil {
		return Audit{}, err
	}
	binary, err := os.ReadFile(filepath.Join(targetDirectory, "debug", spec.BinaryName))
	if err != nil {
		return Audit{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return Audit{}, err
	}
	if err := os.WriteFile(outputPath, binary, 0o700); err != nil {
		return Audit{}, err
	}
	protocolAfter, protocolErr := digestTree(protocolRoot)
	workerAfter, workerErr := digestTree(workerRoot)
	if protocolErr != nil || workerErr != nil || protocolAfter != protocolDigest || workerAfter != workerDigest {
		return Audit{}, errors.New("Cargo source tree changed during offline build")
	}
	commandIdentity, err := digestJSON(commands)
	if err != nil {
		return Audit{}, err
	}
	binaryDigest := digestBytes(binary)
	audit := Audit{
		Version: AuditVersion5, TrialID: spec.TrialID, BuildSpecDigest: specDigest,
		Module: spec.Module, SourcePath: "<unmodified-cargo>",
		InputSourceDigest: workerDigest, OutputSourceDigest: workerDigest,
		InputModuleDigest: protocolDigest, OutputModuleDigest: protocolDigest,
		SUTBuildIdentity: "sha256:" + binaryDigest, BinaryPath: spec.OutputPath,
		BinaryDigest: binaryDigest, CommandIdentity: commandIdentity, Commands: commands,
		ToolchainIdentity: strings.TrimSpace(string(cargoVersion)) + "/" +
			strings.TrimSpace(string(rustcVersion)),
		ModuleCacheUnmodified: true, OfflineReadonlyBuild: true,
	}
	return audit, audit.Validate()
}

func resolveCargoBuildPath(root, relative string, directory bool) (string, error) {
	path, err := filepath.EvalSymlinks(filepath.Join(root, relative))
	if err != nil || !within(root, path) {
		return "", errors.New("Cargo build input resolves outside repository")
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() != directory {
		return "", errors.New("Cargo build input has unexpected file type")
	}
	return path, nil
}

func cargoBuildEnvironment(parent []string, targetDirectory string) []string {
	result := make([]string, 0, len(parent)+2)
	for _, entry := range parent {
		key, _, found := strings.Cut(entry, "=")
		if found && (key == "CARGO_NET_OFFLINE" || key == "CARGO_TARGET_DIR") {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "CARGO_NET_OFFLINE=true", "CARGO_TARGET_DIR="+targetDirectory)
}

func runAuditedCommand(directory string, environment []string, executable string, args ...string) ([]byte, error) {
	command := exec.Command(executable, args...)
	command.Dir = directory
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", executable, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func validateCargoBuildMetadata(
	encoded []byte,
	spec CargoSpec,
	manifest string,
	protocolRoot string,
) error {
	var metadata cargoBuildMetadata
	if json.Unmarshal(encoded, &metadata) != nil || len(metadata.Packages) != 1 ||
		metadata.Packages[0].Name != spec.Package {
		return errors.New("Cargo metadata does not identify the declared worker package")
	}
	resolvedManifest, err := filepath.EvalSymlinks(metadata.Packages[0].ManifestPath)
	if err != nil || resolvedManifest != manifest {
		return errors.New("Cargo metadata manifest does not match build spec")
	}
	want := map[string]string{
		"omnipaxos":         filepath.Join(protocolRoot, "omnipaxos"),
		"omnipaxos_storage": filepath.Join(protocolRoot, "omnipaxos_storage"),
	}
	seen := make(map[string]bool, len(want))
	for _, dependency := range metadata.Packages[0].Dependencies {
		expected, ok := want[dependency.Name]
		if !ok {
			continue
		}
		if dependency.Source != nil || dependency.Path == nil {
			return errors.New("Cargo protocol dependency is not repository-local")
		}
		resolved, err := filepath.EvalSymlinks(*dependency.Path)
		if err != nil || resolved != expected {
			return errors.New("Cargo protocol dependency path does not match build spec")
		}
		seen[dependency.Name] = true
	}
	if !seen["omnipaxos"] || !seen["omnipaxos_storage"] {
		return errors.New("Cargo metadata is missing protocol dependencies")
	}
	return nil
}

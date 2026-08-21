// Package sutbuild creates auditable SUT binaries using a read-only Go build
// overlay. It never edits the module cache or ConsensusAtlas sources.
package sutbuild

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	Version      = 1
	SpecVersion2 = 2
	SpecVersion3 = 3
	// SpecVersion4 adds the explicit Driver linker variable required for new
	// builds. Versions 1–3 remain structurally readable so archived public
	// inputs stay auditable, but cannot be rebuilt without this trusted field.
	SpecVersion4  = 4
	AuditVersion  = 2
	AuditVersion3 = 3
	AuditVersion4 = 4
	// AuditVersion5 reuses the same source/binary/command evidence for an
	// unmodified Cargo worker build. It does not introduce another identity
	// scheme: the worker's existing sha256:<binary> BuildID is the audit SUT ID.
	AuditVersion5 = 5

	CommandGoListModule  = "go-list-module"
	CommandGoBuild       = "go-build"
	CommandCargoVersion  = "cargo-version"
	CommandRustcVersion  = "rustc-version"
	CommandCargoMetadata = "cargo-metadata"
	CommandCargoBuild    = "cargo-build"
)

type Spec struct {
	Version      int                    `json:"version"`
	ID           string                 `json:"id"`
	TrialID      string                 `json:"trial_id"`
	Module       ModuleIdentity         `json:"module"`
	ModuleDigest string                 `json:"module_digest,omitempty"`
	Source       SourceTransformation   `json:"source"`
	Sources      []SourceTransformation `json:"sources,omitempty"`
	Package      string                 `json:"package"`
	// IdentityVariable is a trusted build-pack supplied Go linker variable.
	// It is deliberately explicit: generic build code must not know any
	// concrete Driver package path.
	IdentityVariable string   `json:"identity_variable"`
	SUTBuildIdentity string   `json:"sut_build_identity"`
	CommandAllowlist []string `json:"command_allowlist"`
	OutputPath       string   `json:"output_path"`
}

type ModuleIdentity struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

type SourceTransformation struct {
	RelativePath      string             `json:"relative_path"`
	OriginalDigest    string             `json:"original_digest"`
	Match             string             `json:"match"`
	Replacement       string             `json:"replacement"`
	TransformedDigest string             `json:"transformed_digest"`
	Replacements      []ExactReplacement `json:"replacements,omitempty"`
}

type ExactReplacement struct {
	Match       string `json:"match"`
	Replacement string `json:"replacement"`
}

// SourceAudit records one exact source conversion. Version-3 audits use the
// complete ordered set; version-2 audits retain their historical single-file
// fields unchanged.
type SourceAudit struct {
	RelativePath    string `json:"relative_path"`
	InputDigest     string `json:"input_digest"`
	OutputDigest    string `json:"output_digest"`
	ExactMatchCount int    `json:"exact_match_count"`
}

type CommandAudit struct {
	Identity      string   `json:"identity"`
	Digest        string   `json:"digest"`
	Executable    string   `json:"executable"`
	LogicalArgs   []string `json:"logical_args"`
	Environment   []string `json:"environment"`
	CommandPolicy string   `json:"command_policy"`
}

type Audit struct {
	Version               int            `json:"version"`
	TrialID               string         `json:"trial_id"`
	BuildSpecDigest       string         `json:"build_spec_digest"`
	Module                ModuleIdentity `json:"module"`
	SourcePath            string         `json:"source_path"`
	InputSourceDigest     string         `json:"input_source_digest"`
	OutputSourceDigest    string         `json:"output_source_digest"`
	InputModuleDigest     string         `json:"input_module_digest"`
	OutputModuleDigest    string         `json:"output_module_digest"`
	SUTBuildIdentity      string         `json:"sut_build_identity"`
	BinaryPath            string         `json:"binary_path"`
	BinaryDigest          string         `json:"binary_digest"`
	CommandIdentity       string         `json:"command_identity"`
	Commands              []CommandAudit `json:"commands"`
	ToolchainIdentity     string         `json:"toolchain_identity"`
	ModuleCacheUnmodified bool           `json:"module_cache_unmodified"`
	OfflineReadonlyBuild  bool           `json:"offline_readonly_build"`
	ExactMatchCount       int            `json:"exact_match_count"`
	Sources               []SourceAudit  `json:"sources,omitempty"`
}

// Validate checks a persisted build audit before it can be used as trusted
// trial evidence. The expected digests still come from the private benchmark
// manifest; this method only proves internal structure and self-consistency.
func (audit Audit) Validate() error {
	if (audit.Version != AuditVersion && audit.Version != AuditVersion3 &&
		audit.Version != AuditVersion4 && audit.Version != AuditVersion5) ||
		audit.TrialID == "" || audit.Module.Path == "" ||
		audit.Module.Version == "" || audit.SourcePath == "" || audit.SUTBuildIdentity == "" ||
		audit.BinaryPath == "" || audit.ToolchainIdentity == "" {
		return errors.New("build audit requires version 2 and complete identities")
	}
	for name, value := range map[string]string{
		"build spec": audit.BuildSpecDigest, "input source": audit.InputSourceDigest,
		"output source": audit.OutputSourceDigest, "input module": audit.InputModuleDigest,
		"output module": audit.OutputModuleDigest, "binary": audit.BinaryDigest,
		"command": audit.CommandIdentity,
	} {
		if !isDigest(value) {
			return fmt.Errorf("build audit %s digest is invalid", name)
		}
	}
	if !audit.ModuleCacheUnmodified || !audit.OfflineReadonlyBuild {
		return errors.New("build audit does not prove a read-only unique transformation")
	}
	if audit.Version == AuditVersion && (audit.ExactMatchCount != 1 || len(audit.Sources) != 0) {
		return errors.New("version-2 build audit must prove one source transformation")
	}
	if audit.Version == AuditVersion3 {
		if len(audit.Sources) == 0 {
			return errors.New("version-3 build audit must prove every source transformation exactly once")
		}
		if err := validateSourceAudits(audit.Sources, audit.InputSourceDigest, audit.OutputSourceDigest); err != nil {
			return err
		}
		matchCount := 0
		for _, source := range audit.Sources {
			matchCount += source.ExactMatchCount
		}
		if audit.ExactMatchCount != matchCount {
			return errors.New("version-3 exact match total does not match source audits")
		}
	}
	if audit.Version == AuditVersion4 {
		if audit.SourcePath != "<unmodified>" || audit.ExactMatchCount != 0 || len(audit.Sources) != 0 ||
			audit.InputSourceDigest != audit.InputModuleDigest || audit.OutputSourceDigest != audit.OutputModuleDigest ||
			audit.InputModuleDigest != audit.OutputModuleDigest {
			return errors.New("version-4 build audit does not prove an unmodified module")
		}
	}
	if audit.Version == AuditVersion5 {
		if audit.SourcePath != "<unmodified-cargo>" || audit.ExactMatchCount != 0 ||
			len(audit.Sources) != 0 || audit.InputSourceDigest != audit.OutputSourceDigest ||
			audit.InputModuleDigest != audit.OutputModuleDigest ||
			audit.SUTBuildIdentity != "sha256:"+audit.BinaryDigest {
			return errors.New("version-5 build audit does not prove an unmodified Cargo worker")
		}
	}
	expectedCommands := []struct {
		identity   string
		executable string
	}{{CommandGoListModule, "go"}, {CommandGoBuild, "go"}}
	if audit.Version == AuditVersion5 {
		expectedCommands = []struct {
			identity   string
			executable string
		}{
			{CommandCargoVersion, "cargo"}, {CommandRustcVersion, "rustc"},
			{CommandCargoMetadata, "cargo"}, {CommandCargoBuild, "cargo"},
		}
	}
	if len(audit.Commands) != len(expectedCommands) {
		return errors.New("build audit must contain the exact ordered command allowlist")
	}
	for index, command := range audit.Commands {
		if command.Identity != expectedCommands[index].identity ||
			command.Executable != expectedCommands[index].executable ||
			command.CommandPolicy != "exact-allowlist-v1" ||
			len(command.LogicalArgs) == 0 || !isDigest(command.Digest) {
			return fmt.Errorf("build command %q is incomplete", command.Identity)
		}
		want, err := commandDigest(command)
		if err != nil || want != command.Digest {
			return fmt.Errorf("build command %q digest is inconsistent", command.Identity)
		}
	}
	wantCommands, err := digestJSON(audit.Commands)
	if err != nil || wantCommands != audit.CommandIdentity {
		return errors.New("build audit command identity is inconsistent")
	}
	return nil
}

type moduleInfo struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
	Dir     string `json:"Dir"`
}

type preparedSource struct {
	transformation SourceTransformation
	sourcePath     string
	original       []byte
	transformed    []byte
	matchCount     int
}

func (spec Spec) Validate() error {
	if (spec.Version != Version && spec.Version != SpecVersion2 && spec.Version != SpecVersion3 && spec.Version != SpecVersion4) || spec.ID == "" || spec.TrialID == "" ||
		spec.Module.Path == "" || spec.Module.Version == "" || spec.Package == "" ||
		spec.SUTBuildIdentity == "" || spec.OutputPath == "" {
		return errors.New("build spec requires a supported version and complete identities, package, and output path")
	}
	if spec.IdentityVariable != "" && !validLinkVariable(spec.IdentityVariable) {
		return errors.New("build spec identity variable is invalid")
	}
	if spec.Version == SpecVersion4 && spec.IdentityVariable == "" {
		return errors.New("version-4 build spec requires an explicit identity variable")
	}
	sources, transformedIdentity, err := spec.sourceSet()
	if err != nil {
		return err
	}
	if len(sources) == 0 && spec.Version != SpecVersion3 && spec.Version != SpecVersion4 {
		return errors.New("build spec has no source transformation")
	}
	expectedIdentity := OpaqueBuildIdentity(spec.Module, transformedIdentity, spec.Package)
	if spec.SUTBuildIdentity != expectedIdentity {
		return fmt.Errorf("SUT build identity %q is not bound to module, transformed source digest, and package; want %q", spec.SUTBuildIdentity, expectedIdentity)
	}
	want := []string{CommandGoBuild, CommandGoListModule}
	got := append([]string(nil), spec.CommandAllowlist...)
	sort.Strings(got)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		return fmt.Errorf("command allowlist must be exactly %v", want)
	}
	return nil
}

func (spec Spec) sourceSet() ([]SourceTransformation, string, error) {
	switch spec.Version {
	case Version:
		if len(spec.Sources) != 0 {
			return nil, "", errors.New("version-1 build spec cannot declare sources")
		}
		if len(spec.Source.Replacements) != 0 {
			return nil, "", errors.New("version-1 build spec cannot declare multiple replacements")
		}
		if err := validateTransformation(spec.Source); err != nil {
			return nil, "", err
		}
		return []SourceTransformation{spec.Source}, spec.Source.TransformedDigest, nil
	case SpecVersion2:
		if !emptyTransformation(spec.Source) || len(spec.Sources) == 0 {
			return nil, "", errors.New("version-2 build spec requires sources and cannot mix source")
		}
		sources := append([]SourceTransformation(nil), spec.Sources...)
		for index, source := range sources {
			if err := validateTransformation(source); err != nil {
				return nil, "", fmt.Errorf("sources[%d]: %w", index, err)
			}
		}
		sort.Slice(sources, func(i, j int) bool { return sources[i].RelativePath < sources[j].RelativePath })
		for index := 1; index < len(sources); index++ {
			if sources[index-1].RelativePath == sources[index].RelativePath {
				return nil, "", fmt.Errorf("duplicate source transformation path %q", sources[index].RelativePath)
			}
		}
		identity, err := sourceSetDigest(sources, true)
		return sources, identity, err
	case SpecVersion3, SpecVersion4:
		if !emptyTransformation(spec.Source) || len(spec.Sources) != 0 || !isDigest(spec.ModuleDigest) {
			return nil, "", fmt.Errorf("version-%d build spec requires only a declared module digest", spec.Version)
		}
		return nil, spec.ModuleDigest, nil
	default:
		return nil, "", fmt.Errorf("unsupported build spec version %d", spec.Version)
	}
}

func emptyTransformation(source SourceTransformation) bool {
	return source.RelativePath == "" && source.OriginalDigest == "" && source.Match == "" &&
		source.Replacement == "" && source.TransformedDigest == "" && len(source.Replacements) == 0
}

func validateTransformation(source SourceTransformation) error {
	if source.RelativePath == "" || filepath.IsAbs(source.RelativePath) ||
		filepath.Clean(source.RelativePath) != source.RelativePath ||
		strings.HasPrefix(source.RelativePath, ".."+string(filepath.Separator)) {
		return errors.New("source transformation requires a clean relative path")
	}
	if !isDigest(source.OriginalDigest) || !isDigest(source.TransformedDigest) {
		return errors.New("source transformation digests must be lowercase SHA-256")
	}
	if len(source.Replacements) == 0 {
		if source.Match == "" {
			return errors.New("source transformation requires a non-empty exact match")
		}
		return nil
	}
	if source.Match != "" || source.Replacement != "" {
		return errors.New("multi-replacement source transformation cannot mix match or replacement")
	}
	for index, replacement := range source.Replacements {
		if replacement.Match == "" {
			return fmt.Errorf("replacement[%d] has an empty exact match", index)
		}
	}
	return nil
}

func ApplyTransformation(original []byte, transformation SourceTransformation) ([]byte, int, error) {
	if len(transformation.Replacements) != 0 {
		return ApplySourceTransformation(original, transformation)
	}
	match := []byte(transformation.Match)
	count := bytes.Count(original, match)
	if count != 1 {
		return nil, count, fmt.Errorf("source match count is %d, want exactly 1", count)
	}
	transformed := bytes.Replace(original, match, []byte(transformation.Replacement), 1)
	return transformed, count, nil
}

// ApplySourceTransformation applies either the historical single replacement
// or the ordered version-2 replacement list. Every individual replacement is
// required to match the evolving source exactly once.
func ApplySourceTransformation(original []byte, transformation SourceTransformation) ([]byte, int, error) {
	if len(transformation.Replacements) == 0 {
		match := []byte(transformation.Match)
		count := bytes.Count(original, match)
		if count != 1 {
			return nil, count, fmt.Errorf("source match count is %d, want exactly 1", count)
		}
		return bytes.Replace(original, match, []byte(transformation.Replacement), 1), count, nil
	}
	transformed := append([]byte(nil), original...)
	total := 0
	for index, replacement := range transformation.Replacements {
		match := []byte(replacement.Match)
		count := bytes.Count(transformed, match)
		if count != 1 {
			return nil, total + count, fmt.Errorf("source replacement %d match count is %d, want exactly 1", index, count)
		}
		transformed = bytes.Replace(transformed, match, []byte(replacement.Replacement), 1)
		total += count
	}
	return transformed, total, nil
}

type sourceDigestEntry struct {
	RelativePath string `json:"relative_path"`
	Digest       string `json:"digest"`
}

func sourceSetDigest(sources []SourceTransformation, transformed bool) (string, error) {
	entries := make([]sourceDigestEntry, 0, len(sources))
	for _, source := range sources {
		digest := source.OriginalDigest
		if transformed {
			digest = source.TransformedDigest
		}
		entries = append(entries, sourceDigestEntry{RelativePath: source.RelativePath, Digest: digest})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].RelativePath < entries[j].RelativePath })
	return digestJSON(entries)
}

// TransformedSourceSetDigest returns the canonical final-source identity for a
// version-2 source set. Build specs use it to bind their opaque SUT identity
// before any build directory exists.
func TransformedSourceSetDigest(sources []SourceTransformation) (string, error) {
	copy := append([]SourceTransformation(nil), sources...)
	for index, source := range copy {
		if err := validateTransformation(source); err != nil {
			return "", fmt.Errorf("sources[%d]: %w", index, err)
		}
	}
	sort.Slice(copy, func(i, j int) bool { return copy[i].RelativePath < copy[j].RelativePath })
	for index := 1; index < len(copy); index++ {
		if copy[index-1].RelativePath == copy[index].RelativePath {
			return "", fmt.Errorf("duplicate source transformation path %q", copy[index].RelativePath)
		}
	}
	return sourceSetDigest(copy, true)
}

func validateSourceAudits(sources []SourceAudit, inputDigest, outputDigest string) error {
	entriesIn := make([]sourceDigestEntry, 0, len(sources))
	entriesOut := make([]sourceDigestEntry, 0, len(sources))
	seen := make(map[string]bool, len(sources))
	for index, source := range sources {
		if source.RelativePath == "" || filepath.IsAbs(source.RelativePath) ||
			filepath.Clean(source.RelativePath) != source.RelativePath ||
			strings.HasPrefix(source.RelativePath, ".."+string(filepath.Separator)) ||
			!isDigest(source.InputDigest) || !isDigest(source.OutputDigest) || source.ExactMatchCount < 1 || seen[source.RelativePath] {
			return fmt.Errorf("version-3 source audit[%d] is invalid", index)
		}
		seen[source.RelativePath] = true
		entriesIn = append(entriesIn, sourceDigestEntry{RelativePath: source.RelativePath, Digest: source.InputDigest})
		entriesOut = append(entriesOut, sourceDigestEntry{RelativePath: source.RelativePath, Digest: source.OutputDigest})
	}
	matchCount := 0
	for _, source := range sources {
		matchCount += source.ExactMatchCount
	}
	if matchCount == 0 {
		return errors.New("version-3 source audits have no exact matches")
	}
	sort.Slice(entriesIn, func(i, j int) bool { return entriesIn[i].RelativePath < entriesIn[j].RelativePath })
	sort.Slice(entriesOut, func(i, j int) bool { return entriesOut[i].RelativePath < entriesOut[j].RelativePath })
	actualIn, err := digestJSON(entriesIn)
	if err != nil || actualIn != inputDigest {
		return errors.New("version-3 input source digest does not match source audits")
	}
	actualOut, err := digestJSON(entriesOut)
	if err != nil || actualOut != outputDigest {
		return errors.New("version-3 output source digest does not match source audits")
	}
	return nil
}

func preparedSourceDigests(version int, sources []preparedSource) (string, string, error) {
	if len(sources) == 0 {
		return "", "", errors.New("no prepared source transformations")
	}
	if version == Version {
		return digestBytes(sources[0].original), digestBytes(sources[0].transformed), nil
	}
	input := make([]sourceDigestEntry, 0, len(sources))
	output := make([]sourceDigestEntry, 0, len(sources))
	for _, source := range sources {
		input = append(input, sourceDigestEntry{RelativePath: source.transformation.RelativePath, Digest: digestBytes(source.original)})
		output = append(output, sourceDigestEntry{RelativePath: source.transformation.RelativePath, Digest: digestBytes(source.transformed)})
	}
	sort.Slice(input, func(i, j int) bool { return input[i].RelativePath < input[j].RelativePath })
	sort.Slice(output, func(i, j int) bool { return output[i].RelativePath < output[j].RelativePath })
	inputDigest, err := digestJSON(input)
	if err != nil {
		return "", "", err
	}
	outputDigest, err := digestJSON(output)
	return inputDigest, outputDigest, err
}

func sourceAudits(sources []preparedSource) ([]SourceAudit, int) {
	result := make([]SourceAudit, 0, len(sources))
	total := 0
	for _, source := range sources {
		result = append(result, SourceAudit{
			RelativePath: source.transformation.RelativePath,
			InputDigest:  digestBytes(source.original), OutputDigest: digestBytes(source.transformed),
			ExactMatchCount: source.matchCount,
		})
		total += source.matchCount
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RelativePath < result[j].RelativePath })
	return result, total
}

func Build(repoRoot string, spec Spec) (Audit, error) {
	if err := spec.Validate(); err != nil {
		return Audit{}, err
	}
	if spec.IdentityVariable == "" {
		return Audit{}, errors.New("archived build spec has no identity variable; migrate it to version 4 with a trusted concrete build pack before rebuilding")
	}
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return Audit{}, err
	}
	outputPath, err := resolveOutput(repoRoot, spec.OutputPath)
	if err != nil {
		return Audit{}, err
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return Audit{}, fmt.Errorf("refusing to overwrite existing binary %s", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Audit{}, err
	}

	listCommand := CommandAudit{
		Identity: CommandGoListModule, Executable: "go",
		LogicalArgs: []string{"list", "-mod=readonly", "-m", "-json", spec.Module.Path},
		Environment: lockedEnvironment(), CommandPolicy: "exact-allowlist-v1",
	}
	listCommand.Digest, err = commandDigest(listCommand)
	if err != nil {
		return Audit{}, err
	}
	listOutput, err := runGo(repoRoot, listCommand.LogicalArgs...)
	if err != nil {
		return Audit{}, fmt.Errorf("resolve module: %w", err)
	}
	var resolved moduleInfo
	if err := json.Unmarshal(listOutput, &resolved); err != nil {
		return Audit{}, fmt.Errorf("decode go list module: %w", err)
	}
	if resolved.Path != spec.Module.Path || resolved.Version != spec.Module.Version || resolved.Dir == "" {
		return Audit{}, fmt.Errorf("resolved module %s@%s, want %s@%s",
			resolved.Path, resolved.Version, spec.Module.Path, spec.Module.Version)
	}

	moduleDir, err := filepath.EvalSymlinks(resolved.Dir)
	if err != nil {
		return Audit{}, err
	}
	sources, _, err := spec.sourceSet()
	if err != nil {
		return Audit{}, err
	}
	inputModuleDigest, err := digestTree(moduleDir)
	if err != nil {
		return Audit{}, fmt.Errorf("digest input module: %w", err)
	}
	if (spec.Version == SpecVersion3 || spec.Version == SpecVersion4) && inputModuleDigest != spec.ModuleDigest {
		return Audit{}, fmt.Errorf("resolved module digest %s does not match spec %s", inputModuleDigest, spec.ModuleDigest)
	}
	prepared := make([]preparedSource, 0, len(sources))
	for _, source := range sources {
		sourcePath, resolveErr := filepath.EvalSymlinks(filepath.Join(moduleDir, source.RelativePath))
		if resolveErr != nil {
			return Audit{}, resolveErr
		}
		if !within(moduleDir, sourcePath) {
			return Audit{}, errors.New("source transformation escapes the resolved module directory")
		}
		original, readErr := os.ReadFile(sourcePath)
		if readErr != nil {
			return Audit{}, readErr
		}
		inputDigest := digestBytes(original)
		if inputDigest != source.OriginalDigest {
			return Audit{}, fmt.Errorf("original source digest %s does not match spec %s", inputDigest, source.OriginalDigest)
		}
		transformed, matchCount, transformErr := ApplyTransformation(original, source)
		if transformErr != nil {
			return Audit{}, transformErr
		}
		outputDigest := digestBytes(transformed)
		if outputDigest != source.TransformedDigest {
			return Audit{}, fmt.Errorf("transformed source digest %s does not match spec %s", outputDigest, source.TransformedDigest)
		}
		prepared = append(prepared, preparedSource{
			transformation: source, sourcePath: sourcePath, original: original,
			transformed: transformed, matchCount: matchCount,
		})
	}
	inputSourceDigest, outputSourceDigest := inputModuleDigest, inputModuleDigest
	if len(prepared) != 0 {
		inputSourceDigest, outputSourceDigest, err = preparedSourceDigests(spec.Version, prepared)
		if err != nil {
			return Audit{}, err
		}
	}

	buildWorkRoot := filepath.Join(repoRoot, "artifacts", "build-work")
	if err := os.MkdirAll(buildWorkRoot, 0o700); err != nil {
		return Audit{}, err
	}
	resolvedBuildWorkRoot, err := filepath.EvalSymlinks(buildWorkRoot)
	if err != nil || !within(repoRoot, resolvedBuildWorkRoot) {
		return Audit{}, errors.New("build work root resolves outside the ConsensusAtlas repository")
	}
	temporary := filepath.Join(resolvedBuildWorkRoot, spec.SUTBuildIdentity)
	if _, err := os.Lstat(temporary); err == nil {
		return Audit{}, fmt.Errorf("refusing to reuse existing build work directory %s", temporary)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Audit{}, err
	}
	if err := os.Mkdir(temporary, 0o700); err != nil {
		return Audit{}, err
	}
	defer os.RemoveAll(temporary)
	stagedModule := filepath.Join(temporary, "module")
	if err := copyModule(moduleDir, stagedModule); err != nil {
		return Audit{}, err
	}
	for _, source := range prepared {
		transformedPath := filepath.Join(stagedModule, source.transformation.RelativePath)
		if err := os.WriteFile(transformedPath, source.transformed, 0o600); err != nil {
			return Audit{}, err
		}
	}
	outputModuleDigest, err := digestTree(stagedModule)
	if err != nil {
		return Audit{}, fmt.Errorf("digest transformed module: %w", err)
	}
	if (spec.Version == SpecVersion3 || spec.Version == SpecVersion4) && outputModuleDigest != inputModuleDigest {
		return Audit{}, errors.New("unmodified build staging changed the module tree")
	}
	projectMod, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return Audit{}, err
	}
	modFilePath := filepath.Join(temporary, "build.mod")
	// The relative replacement is interpreted from the generated modfile and
	// recorded verbatim in Go build info. Unlike an absolute temporary path it
	// gives repeated builds the same binary identity.
	replacementPath, err := filepath.Rel(repoRoot, stagedModule)
	if err != nil || strings.HasPrefix(replacementPath, "..") {
		return Audit{}, errors.New("staged module is outside the ConsensusAtlas repository")
	}
	modFile, err := replaceModuleSource(projectMod, spec.Module.Path, "./"+filepath.ToSlash(replacementPath))
	if err != nil {
		return Audit{}, err
	}
	if err := os.WriteFile(modFilePath, modFile, 0o600); err != nil {
		return Audit{}, err
	}
	projectSum, err := os.ReadFile(filepath.Join(repoRoot, "go.sum"))
	if err != nil {
		return Audit{}, err
	}
	if err := os.WriteFile(filepath.Join(temporary, "build.sum"), projectSum, 0o600); err != nil {
		return Audit{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return Audit{}, err
	}
	resolvedOutputDir, err := filepath.EvalSymlinks(filepath.Dir(outputPath))
	if err != nil {
		return Audit{}, err
	}
	if !within(repoRoot, resolvedOutputDir) {
		return Audit{}, errors.New("binary output directory resolves outside the ConsensusAtlas repository")
	}
	outputPath = filepath.Join(resolvedOutputDir, filepath.Base(outputPath))

	logicalBuildArgs := []string{
		"build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-modfile=<generated>",
		"-ldflags=-buildid= -X=" + spec.IdentityVariable + "=" + spec.SUTBuildIdentity,
		"-o", spec.OutputPath, spec.Package,
	}
	buildCommand := CommandAudit{
		Identity: CommandGoBuild, Executable: "go", LogicalArgs: logicalBuildArgs,
		Environment: lockedEnvironment(), CommandPolicy: "exact-allowlist-v1",
	}
	buildCommand.Digest, err = commandDigest(buildCommand)
	if err != nil {
		return Audit{}, err
	}
	actualBuildArgs := append([]string(nil), logicalBuildArgs...)
	actualBuildArgs[4] = "-modfile=" + modFilePath
	actualBuildArgs[7] = outputPath
	if _, err := runGo(repoRoot, actualBuildArgs...); err != nil {
		return Audit{}, fmt.Errorf("build controlled SUT: %w", err)
	}
	for _, source := range prepared {
		moduleSourceAfterBuild, readErr := os.ReadFile(source.sourcePath)
		if readErr != nil {
			return Audit{}, readErr
		}
		if digestBytes(moduleSourceAfterBuild) != digestBytes(source.original) {
			return Audit{}, errors.New("module cache source changed during overlay build")
		}
	}
	inputModuleAfterBuild, err := digestTree(moduleDir)
	if err != nil {
		return Audit{}, err
	}
	if inputModuleAfterBuild != inputModuleDigest {
		return Audit{}, errors.New("module cache tree changed during overlay build")
	}
	binary, err := os.ReadFile(outputPath)
	if err != nil {
		return Audit{}, err
	}
	specDigest, err := digestJSON(spec)
	if err != nil {
		return Audit{}, err
	}
	commandIdentity, err := digestJSON([]CommandAudit{listCommand, buildCommand})
	if err != nil {
		return Audit{}, err
	}
	audit := Audit{
		Version: AuditVersion, TrialID: spec.TrialID, BuildSpecDigest: specDigest,
		Module: spec.Module, SourcePath: spec.Source.RelativePath,
		InputSourceDigest: inputSourceDigest, OutputSourceDigest: outputSourceDigest,
		InputModuleDigest: inputModuleDigest, OutputModuleDigest: outputModuleDigest,
		SUTBuildIdentity: spec.SUTBuildIdentity, BinaryPath: spec.OutputPath,
		BinaryDigest: digestBytes(binary), CommandIdentity: commandIdentity,
		Commands:              []CommandAudit{listCommand, buildCommand},
		ToolchainIdentity:     runtime.Version() + "/" + runtime.GOOS + "/" + runtime.GOARCH,
		ModuleCacheUnmodified: true,
		OfflineReadonlyBuild:  true,
	}
	if spec.Version == SpecVersion2 {
		audit.Version = AuditVersion3
		audit.SourcePath = "<multiple>"
		audit.Sources, audit.ExactMatchCount = sourceAudits(prepared)
	} else if spec.Version == SpecVersion3 || spec.Version == SpecVersion4 {
		audit.Version = AuditVersion4
		audit.SourcePath = "<unmodified>"
		audit.ExactMatchCount = 0
	} else {
		audit.ExactMatchCount = prepared[0].matchCount
	}
	return audit, nil
}

// replaceModuleSource swaps the repository's editable local checkout for the
// builder's audited staging copy.  ConsensusAtlas intentionally keeps its two
// SUT replacements as single-line directives; rejecting a replace block is
// safer than silently retaining two competing sources for the same module.
func replaceModuleSource(projectMod []byte, modulePath, replacement string) ([]byte, error) {
	lines := strings.Split(string(projectMod), "\n")
	kept := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 1 && fields[0] == "replace" && (len(fields) == 1 || fields[1] == "(") {
			return nil, errors.New("audited SUT build does not accept replace blocks in the project go.mod")
		}
		if len(fields) >= 4 && fields[0] == "replace" && fields[2] == "=>" {
			// The generated modfile lives below artifacts/build-work, so the
			// project's relative suts/ paths would resolve in the wrong place.
			// Drop every ordinary local SUT replacement; the selected module is
			// re-added below as the one audited staging source. Other required
			// modules resolve from the locked module cache.
			if fields[1] == modulePath || strings.HasPrefix(filepath.ToSlash(fields[3]), "./suts/") {
				continue
			}
		}
		kept = append(kept, line)
	}
	result := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	result += "\n\nreplace " + modulePath + " => " + replacement + "\n"
	return []byte(result), nil
}

func resolveOutput(repoRoot, requested string) (string, error) {
	resolved := requested
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(repoRoot, resolved)
	}
	resolved = filepath.Clean(resolved)
	if !within(repoRoot, resolved) {
		return "", errors.New("binary output must remain inside the ConsensusAtlas repository")
	}
	return resolved, nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func runGo(directory string, args ...string) ([]byte, error) {
	command := exec.Command("go", args...)
	command.Dir = directory
	command.Env = controlledEnvironment(os.Environ())
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func lockedEnvironment() []string {
	return []string{"GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOFLAGS=", "GOTOOLCHAIN=local"}
}

func controlledEnvironment(parent []string) []string {
	lockedKeys := map[string]bool{
		"GOPROXY": true, "GOSUMDB": true, "GOWORK": true, "GOFLAGS": true, "GOTOOLCHAIN": true,
	}
	result := make([]string, 0, len(parent)+len(lockedKeys))
	for _, entry := range parent {
		key, _, found := strings.Cut(entry, "=")
		if found && lockedKeys[key] {
			continue
		}
		result = append(result, entry)
	}
	return append(result, lockedEnvironment()...)
}

func OpaqueBuildIdentity(module ModuleIdentity, transformedDigest, packagePath string) string {
	material := strings.Join([]string{module.Path, module.Version, transformedDigest, packagePath}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return "sut-" + hex.EncodeToString(sum[:8])
}

func copyModule(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if moduleVCSMetadata(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("module source contains unsupported symlink %s", relative)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("module source contains non-regular file %s", relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
}

type treeEntry struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

func digestTree(root string) (string, error) {
	var entries []treeEntry
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if moduleVCSMetadata(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("module source contains unsupported non-regular file %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, treeEntry{Path: filepath.ToSlash(relative), Digest: digestBytes(data)})
		return nil
	})
	if err != nil {
		return "", err
	}
	return digestJSON(entries)
}

// SourceTreeDigest returns the same content identity used by audited SUT
// builds. Callers can bind an Agent-visible local checkout to a MethodSpec
// without duplicating tree hashing rules. Git administrative metadata is
// excluded by digestTree; all ordinary module files remain covered.
func SourceTreeDigest(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	return digestTree(root)
}

// moduleVCSMetadata excludes only the checkout's own Git administrative
// entry.  Submodules use either a .git directory or a .git text file; neither
// participates in a Go build and both contain machine-local paths/state.  We
// deliberately keep .gitignore, .github and nested source files in the module
// digest.
func moduleVCSMetadata(relative string) bool {
	relative = filepath.ToSlash(relative)
	return relative == ".git" || strings.HasPrefix(relative, ".git/")
}

func commandDigest(command CommandAudit) (string, error) {
	command.Digest = ""
	return digestJSON(command)
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return digestBytes(encoded), nil
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func isDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func validLinkVariable(value string) bool {
	separator := strings.LastIndex(value, ".")
	if separator <= 0 || separator == len(value)-1 || strings.Contains(value, " ") || strings.Contains(value, "=") {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '/' || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

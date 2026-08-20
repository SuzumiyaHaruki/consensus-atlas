package controlexperiment

import (
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	KnowledgeDiscoveryCompleted = "completed"
	KnowledgeDiscoveryStopped   = "stopped"

	KnowledgeDiscoveryReferenceUnknown = "reference-not-declared"
	KnowledgeDiscoveryPathUnsafe       = "source-path-unsafe"
	KnowledgeDiscoverySourceNotFound   = "source-not-found"
	KnowledgeDiscoverySourceTooLarge   = "source-too-large"
	KnowledgeDiscoverySourceNotText    = "source-not-text"
	KnowledgeDiscoveryMountUnavailable = "source-mount-unavailable"
	KnowledgeDiscoveryLocatorNotFound  = "locator-not-found"
	KnowledgeDiscoveryLocatorAmbiguous = "locator-ambiguous"
	KnowledgeDiscoveryRangeInvalid     = "line-range-invalid"
	KnowledgeDiscoverySearchNoMatch    = "search-no-match"

	KnowledgeDiscoveryMaxLines         = 160
	KnowledgeDiscoveryMaxSearchResults = 20
	KnowledgeDiscoveryMaxQueryBytes    = 128
	knowledgeDiscoveryMaxSourceBytes   = 2 << 20
	knowledgeDiscoveryMaxExcerptBytes  = 24 << 10
	knowledgeDiscoveryContextLines     = 12
	knowledgeDiscoverySearchFiles      = 4096
	knowledgeDiscoverySearchBytes      = 8 << 20
)

// KnowledgeSource is derived from Target Dossier evidence_refs. MaterialIDs
// explain why the source is offered; they do not certify its contents.
type KnowledgeSource struct {
	Reference   string   `json:"reference"`
	Path        string   `json:"path"`
	Locator     string   `json:"locator,omitempty"`
	MaterialIDs []string `json:"material_ids"`
}

type KnowledgeReadRequest struct {
	Reference  string `json:"reference"`
	Query      string `json:"query,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	MaxLines   int    `json:"max_lines,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}

type KnowledgeSearchMatch struct {
	Reference string `json:"reference"`
	Line      int    `json:"line"`
	Preview   string `json:"preview"`
}

// KnowledgeSourceMount maps a virtual prefix used by evidence_refs to an
// explicitly supplied read-only source directory. The empty prefix denotes
// the main repository.
type KnowledgeSourceMount struct {
	ReferencePrefix string `json:"reference_prefix"`
	Directory       string `json:"directory"`
	// SUTSource is trusted runtime metadata. Directory remains machine-local,
	// while this content identity is projected into MethodSpec.
	SUTSource *AgenticSUTSourceBinding `json:"sut_source,omitempty"`
}

type KnowledgeReadResult struct {
	Status     string                 `json:"status"`
	ReasonCode string                 `json:"reason_code,omitempty"`
	Source     KnowledgeSource        `json:"source"`
	StartLine  int                    `json:"start_line,omitempty"`
	EndLine    int                    `json:"end_line,omitempty"`
	TotalLines int                    `json:"total_lines,omitempty"`
	Text       string                 `json:"text,omitempty"`
	Truncated  bool                   `json:"truncated,omitempty"`
	Query      string                 `json:"query,omitempty"`
	Matches    []KnowledgeSearchMatch `json:"matches,omitempty"`
}

// KnowledgeSourceCatalog exposes only references already present in the
// editable Target Dossier. It adds no repository-wide file enumeration.
func KnowledgeSourceCatalog(pack ProtocolKnowledgePack) ([]KnowledgeSource, error) {
	if pack.ValidateAgentMaterials() != nil || pack.TargetDossier == nil {
		return nil, errors.New("EXPERIMENT_KNOWLEDGE_CATALOG_INPUT_INVALID")
	}
	materials := targetDossierMaterials(*pack.TargetDossier)
	byReference := make(map[string]*KnowledgeSource)
	for _, material := range materials {
		for _, reference := range material.EvidenceRefs {
			source := byReference[reference]
			if source == nil {
				path, locator := splitKnowledgeReference(reference)
				source = &KnowledgeSource{Reference: reference, Path: path, Locator: locator}
				byReference[reference] = source
			}
			source.MaterialIDs = append(source.MaterialIDs, material.ID)
		}
	}
	result := make([]KnowledgeSource, 0, len(byReference))
	for _, source := range byReference {
		sort.Strings(source.MaterialIDs)
		source.MaterialIDs = compactSortedStrings(source.MaterialIDs)
		result = append(result, *source)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Reference < result[j].Reference })
	return result, nil
}

// ReadDeclaredKnowledgeSource returns a bounded text excerpt from one declared
// repository source. It never invokes a shell, follows an undeclared path, or
// interprets the excerpt as execution evidence.
func ReadDeclaredKnowledgeSource(
	repositoryRoot string,
	pack ProtocolKnowledgePack,
	request KnowledgeReadRequest,
) (KnowledgeReadResult, error) {
	return ReadDeclaredKnowledgeSourceFromMounts(
		[]KnowledgeSourceMount{{Directory: repositoryRoot}}, pack, request,
	)
}

func ReadDeclaredKnowledgeSourceFromMounts(
	mounts []KnowledgeSourceMount,
	pack ProtocolKnowledgePack,
	request KnowledgeReadRequest,
) (KnowledgeReadResult, error) {
	if ValidateKnowledgeSourceMounts(mounts) != nil || !validKnowledgeReadRequest(request) {
		return KnowledgeReadResult{}, errors.New("EXPERIMENT_KNOWLEDGE_READ_INPUT_INVALID")
	}
	catalog, err := KnowledgeSourceCatalog(pack)
	if err != nil {
		return KnowledgeReadResult{}, err
	}
	var source *KnowledgeSource
	for index := range catalog {
		if catalog[index].Reference == request.Reference {
			matched := catalog[index]
			source = &matched
			break
		}
	}
	if source == nil {
		return stoppedKnowledgeRead(KnowledgeSource{Reference: request.Reference}, KnowledgeDiscoveryReferenceUnknown), nil
	}
	return readKnowledgeSourceFromMounts(mounts, *source, request)
}

// ReadMountedKnowledgeSourceFromMounts reads one path that trusted discovery
// has already admitted. Callers must still keep the discovered source catalog
// authoritative; this function only enforces mount and path safety plus the
// ordinary excerpt bounds.
func ReadMountedKnowledgeSourceFromMounts(
	mounts []KnowledgeSourceMount,
	source KnowledgeSource,
	request KnowledgeReadRequest,
) (KnowledgeReadResult, error) {
	if ValidateKnowledgeSourceMounts(mounts) != nil || !validKnowledgeReadRequest(request) ||
		source.Reference != request.Reference || source.Path == "" {
		return KnowledgeReadResult{}, errors.New("EXPERIMENT_KNOWLEDGE_READ_INPUT_INVALID")
	}
	return readKnowledgeSourceFromMounts(mounts, source, request)
}

func readKnowledgeSourceFromMounts(
	mounts []KnowledgeSourceMount,
	source KnowledgeSource,
	request KnowledgeReadRequest,
) (KnowledgeReadResult, error) {
	result := KnowledgeReadResult{Status: KnowledgeDiscoveryStopped, Source: source}
	mount, relativeSource, mounted := selectKnowledgeSourceMount(mounts, source.Path)
	if !mounted {
		return stoppedKnowledgeRead(source, KnowledgeDiscoveryMountUnavailable), nil
	}
	root, err := filepath.Abs(mount.Directory)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return stoppedKnowledgeRead(source, KnowledgeDiscoveryMountUnavailable), nil
		}
		return result, err
	}
	path, safe := safeKnowledgeSourcePath(root, relativeSource)
	if !safe {
		return stoppedKnowledgeRead(source, KnowledgeDiscoveryPathUnsafe), nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return stoppedKnowledgeRead(source, KnowledgeDiscoverySourceNotFound), nil
		}
		return result, err
	}
	if _, safe = safeResolvedKnowledgeSourcePath(root, resolved); !safe {
		return stoppedKnowledgeRead(source, KnowledgeDiscoveryPathUnsafe), nil
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return stoppedKnowledgeRead(source, KnowledgeDiscoverySourceNotFound), nil
	}
	file, err := os.Open(resolved)
	if err != nil {
		return result, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, knowledgeDiscoveryMaxSourceBytes+1))
	if err != nil {
		return result, err
	}
	if len(data) > knowledgeDiscoveryMaxSourceBytes {
		return stoppedKnowledgeRead(source, KnowledgeDiscoverySourceTooLarge), nil
	}
	if len(data) == 0 || !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return stoppedKnowledgeRead(source, KnowledgeDiscoverySourceNotText), nil
	}
	lines := splitKnowledgeLines(string(data))
	start := request.StartLine
	if start == 0 {
		start = 1
		if source.Locator != "" {
			located, ambiguous := locateKnowledgeLine(lines, source.Locator)
			if ambiguous {
				return stoppedKnowledgeRead(source, KnowledgeDiscoveryLocatorAmbiguous), nil
			}
			if located == 0 {
				return stoppedKnowledgeRead(source, KnowledgeDiscoveryLocatorNotFound), nil
			}
			start = located - knowledgeDiscoveryContextLines
			minimumStart := located - request.MaxLines + 1
			if start < minimumStart {
				start = minimumStart
			}
			if start < 1 {
				start = 1
			}
		}
	}
	if start > len(lines) {
		return stoppedKnowledgeRead(source, KnowledgeDiscoveryRangeInvalid), nil
	}
	end := start + request.MaxLines - 1
	if end > len(lines) {
		end = len(lines)
	}
	text := strings.Join(lines[start-1:end], "\n")
	excerptTruncated := false
	if len(text) > knowledgeDiscoveryMaxExcerptBytes {
		excerptTruncated = true
		encoded := []byte(text)[:knowledgeDiscoveryMaxExcerptBytes]
		for len(encoded) > 0 && !utf8.Valid(encoded) {
			encoded = encoded[:len(encoded)-1]
		}
		text = string(encoded)
		end = start + strings.Count(text, "\n")
	}
	result.Status = KnowledgeDiscoveryCompleted
	result.StartLine = start
	result.EndLine = end
	result.TotalLines = len(lines)
	result.Text = text
	result.Truncated = excerptTruncated || start > 1 || end < len(lines)
	return result, nil
}

func validKnowledgeReadRequest(request KnowledgeReadRequest) bool {
	return request.Query == "" && request.Reference != "" && request.StartLine >= 0 &&
		request.MaxLines > 0 && request.MaxLines <= KnowledgeDiscoveryMaxLines && request.MaxResults == 0
}

func validKnowledgeSearchRequest(request KnowledgeReadRequest) bool {
	return request.Reference == "" && request.StartLine == 0 && request.MaxLines == 0 &&
		request.Query != "" && strings.TrimSpace(request.Query) == request.Query &&
		len(request.Query) <= KnowledgeDiscoveryMaxQueryBytes && request.MaxResults > 0 &&
		request.MaxResults <= KnowledgeDiscoveryMaxSearchResults
}

// SearchMountedKnowledgeSources performs a deterministic, bounded, read-only
// substring search over explicitly mounted source trees. It does not use the
// Target Dossier to choose files and therefore cannot encode an experiment-
// specific source hint. Matches only authorize later bounded reads through the
// trusted per-call catalog maintained by the Risk coordinator.
func SearchMountedKnowledgeSources(
	mounts []KnowledgeSourceMount,
	request KnowledgeReadRequest,
) (KnowledgeReadResult, error) {
	if ValidateKnowledgeSourceMounts(mounts) != nil || !validKnowledgeSearchRequest(request) {
		return KnowledgeReadResult{}, errors.New("EXPERIMENT_KNOWLEDGE_SEARCH_INPUT_INVALID")
	}
	result := KnowledgeReadResult{Status: KnowledgeDiscoveryCompleted, Query: request.Query}
	query := strings.ToLower(request.Query)
	ordered := append([]KnowledgeSourceMount(nil), mounts...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ReferencePrefix < ordered[j].ReferencePrefix
	})
	files, consumed := 0, int64(0)
	for _, mount := range ordered {
		root, err := filepath.Abs(mount.Directory)
		if err != nil {
			return KnowledgeReadResult{}, err
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return KnowledgeReadResult{}, err
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == root {
				return nil
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if entry.IsDir() {
				if entry.Type()&os.ModeSymlink != 0 || ignoredKnowledgeSearchDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() ||
				!searchableKnowledgeSource(path) {
				return nil
			}
			files++
			if files > knowledgeDiscoverySearchFiles {
				result.Truncated = true
				return filepath.SkipAll
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if info.Size() <= 0 || info.Size() > knowledgeDiscoveryMaxSourceBytes ||
				consumed+info.Size() > knowledgeDiscoverySearchBytes {
				result.Truncated = true
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			consumed += int64(len(data))
			if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
				return nil
			}
			reference := mount.ReferencePrefix + filepath.ToSlash(relative)
			for lineIndex, line := range splitKnowledgeLines(string(data)) {
				if !strings.Contains(strings.ToLower(line), query) {
					continue
				}
				preview := strings.TrimSpace(line)
				if len(preview) > 240 {
					preview = preview[:240]
				}
				result.Matches = append(result.Matches, KnowledgeSearchMatch{
					Reference: reference, Line: lineIndex + 1, Preview: preview,
				})
				if len(result.Matches) >= request.MaxResults {
					result.Truncated = true
					return filepath.SkipAll
				}
			}
			return nil
		})
		if err != nil {
			return KnowledgeReadResult{}, err
		}
		if len(result.Matches) >= request.MaxResults || files > knowledgeDiscoverySearchFiles {
			break
		}
	}
	if len(result.Matches) == 0 {
		result.Status = KnowledgeDiscoveryStopped
		result.ReasonCode = KnowledgeDiscoverySearchNoMatch
	}
	return result, nil
}

func ignoredKnowledgeSearchDirectory(name string) bool {
	switch name {
	case ".git", "target", "vendor", "node_modules", "artifacts", "benchmarks":
		return true
	default:
		return false
	}
}

func searchableKnowledgeSource(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".rs", ".proto", ".c", ".cc", ".cpp", ".h", ".hpp", ".java", ".kt", ".py", ".toml":
		return true
	default:
		return false
	}
}

func ValidateKnowledgeSourceMounts(mounts []KnowledgeSourceMount) error {
	if len(mounts) == 0 {
		return errors.New("EXPERIMENT_KNOWLEDGE_SOURCE_MOUNTS_INVALID")
	}
	seen := make(map[string]bool, len(mounts))
	for _, mount := range mounts {
		prefix := mount.ReferencePrefix
		if strings.TrimSpace(mount.Directory) == "" || seen[prefix] ||
			mount.SUTSource != nil && (mount.SUTSource.Validate() != nil ||
				mount.SUTSource.ReferencePrefix != prefix) ||
			prefix != "" && (!strings.HasSuffix(prefix, "/") || strings.HasPrefix(prefix, "/") ||
				strings.Contains(prefix, "\\") ||
				pathpkg.Clean(strings.TrimSuffix(prefix, "/"))+"/" != prefix) {
			return errors.New("EXPERIMENT_KNOWLEDGE_SOURCE_MOUNTS_INVALID")
		}
		seen[prefix] = true
	}
	return nil
}

func selectKnowledgeSourceMount(
	mounts []KnowledgeSourceMount,
	sourcePath string,
) (KnowledgeSourceMount, string, bool) {
	selected := -1
	for index, mount := range mounts {
		if strings.HasPrefix(sourcePath, mount.ReferencePrefix) &&
			(selected < 0 || len(mount.ReferencePrefix) > len(mounts[selected].ReferencePrefix)) {
			selected = index
		}
	}
	if selected < 0 {
		return KnowledgeSourceMount{}, "", false
	}
	relative := strings.TrimPrefix(sourcePath, mounts[selected].ReferencePrefix)
	if relative == "" {
		return KnowledgeSourceMount{}, "", false
	}
	return mounts[selected], relative, true
}

func targetDossierMaterials(dossier TargetDossier) []TargetMaterial {
	var result []TargetMaterial
	for _, section := range [][]TargetMaterial{
		dossier.Assumptions, dossier.Components, dossier.Contracts,
		dossier.ControlSemantics, dossier.ActiveExperiment, dossier.BlindSpots,
	} {
		result = append(result, section...)
	}
	return result
}

func splitKnowledgeReference(reference string) (string, string) {
	path, locator, found := strings.Cut(reference, ":")
	if !found {
		return reference, ""
	}
	return path, locator
}

func compactSortedStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	write := 1
	for read := 1; read < len(values); read++ {
		if values[read] != values[write-1] {
			values[write] = values[read]
			write++
		}
	}
	return values[:write]
}

func safeKnowledgeSourcePath(root string, source string) (string, bool) {
	if source == "" || filepath.IsAbs(source) || filepath.Clean(source) == "." {
		return "", false
	}
	path := filepath.Join(root, filepath.FromSlash(source))
	_, safe := safeResolvedKnowledgeSourcePath(root, path)
	return path, safe
}

func safeResolvedKnowledgeSourcePath(root string, path string) (string, bool) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}

func splitKnowledgeLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

func locateKnowledgeLine(lines []string, locator string) (int, bool) {
	candidates := []string{locator}
	if index := strings.LastIndex(locator, "::"); index >= 0 && index+2 < len(locator) {
		candidates = append(candidates, locator[index+2:])
	}
	if index := strings.LastIndex(locator, "."); index >= 0 && index+1 < len(locator) {
		candidates = append(candidates, locator[index+1:])
	}
	compact := compactKnowledgeLocators(candidates)
	for _, declarationsOnly := range []bool{true, false} {
		for _, candidate := range compact {
			matches := knowledgeLocatorMatches(lines, candidate, declarationsOnly)
			if len(matches) > 1 {
				return 0, true
			}
			if len(matches) == 1 {
				return matches[0], false
			}
		}
	}
	return 0, false
}

func knowledgeLocatorMatches(lines []string, candidate string, declarationsOnly bool) []int {
	result := make([]int, 0, 1)
	for index, line := range lines {
		position := knowledgeLocatorPosition(line, candidate)
		if position < 0 || declarationsOnly && !knowledgeDeclarationPrefix(line[:position]) {
			continue
		}
		result = append(result, index+1)
	}
	return result
}

func knowledgeLocatorPosition(line, candidate string) int {
	for offset := 0; offset <= len(line)-len(candidate); {
		index := strings.Index(line[offset:], candidate)
		if index < 0 {
			return -1
		}
		index += offset
		end := index + len(candidate)
		if (index == 0 || !knowledgeIdentifierByte(line[index-1])) &&
			(end == len(line) || !knowledgeIdentifierByte(line[end])) {
			return index
		}
		offset = index + 1
	}
	return -1
}

func knowledgeIdentifierByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' || value == '_'
}

func knowledgeDeclarationPrefix(prefix string) bool {
	prefix = strings.TrimLeft(prefix, " \t")
	if strings.Contains(prefix, "//") || strings.ContainsAny(prefix, "{}") {
		return false
	}
	for _, marker := range []string{"func ", "type ", "fn ", "pub fn ", "struct ", "pub struct ", "enum ", "pub enum ", "const ", "var "} {
		if strings.HasPrefix(prefix, marker) {
			return true
		}
	}
	return strings.Contains(prefix, " fn ")
}

func compactKnowledgeLocators(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func stoppedKnowledgeRead(source KnowledgeSource, reason string) KnowledgeReadResult {
	return KnowledgeReadResult{Status: KnowledgeDiscoveryStopped, ReasonCode: reason, Source: source}
}

func (result KnowledgeReadResult) Validate() error {
	if result.Status == KnowledgeDiscoveryStopped {
		if result.ReasonCode == "" || result.Text != "" || result.StartLine != 0 || result.EndLine != 0 ||
			len(result.Matches) != 0 || result.Query != "" && result.Source.Reference != "" {
			return errors.New("EXPERIMENT_KNOWLEDGE_READ_RESULT_INVALID")
		}
		return nil
	}
	if result.Status != KnowledgeDiscoveryCompleted || result.ReasonCode != "" {
		return fmt.Errorf("EXPERIMENT_KNOWLEDGE_READ_RESULT_INVALID: %s", result.Status)
	}
	if result.Query != "" {
		if result.Source.Reference != "" || result.Text != "" || result.StartLine != 0 ||
			result.EndLine != 0 || len(result.Matches) == 0 ||
			len(result.Matches) > KnowledgeDiscoveryMaxSearchResults {
			return errors.New("EXPERIMENT_KNOWLEDGE_SEARCH_RESULT_INVALID")
		}
		seen := make(map[string]bool, len(result.Matches))
		for _, match := range result.Matches {
			key := fmt.Sprintf("%s:%d", match.Reference, match.Line)
			if match.Reference == "" || match.Line <= 0 || match.Preview == "" || seen[key] {
				return errors.New("EXPERIMENT_KNOWLEDGE_SEARCH_RESULT_INVALID")
			}
			seen[key] = true
		}
		return nil
	}
	if result.Source.Reference == "" || result.StartLine <= 0 || result.EndLine < result.StartLine ||
		result.TotalLines < result.EndLine || result.Text == "" || len(result.Matches) != 0 {
		return fmt.Errorf("EXPERIMENT_KNOWLEDGE_READ_RESULT_INVALID: %s", result.Status)
	}
	return nil
}

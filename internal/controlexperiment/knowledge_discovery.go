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
	KnowledgeDiscoveryRangeInvalid     = "line-range-invalid"

	KnowledgeDiscoveryMaxLines        = 160
	knowledgeDiscoveryMaxSourceBytes  = 2 << 20
	knowledgeDiscoveryMaxExcerptBytes = 24 << 10
	knowledgeDiscoveryContextLines    = 12
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
	Reference string `json:"reference"`
	StartLine int    `json:"start_line,omitempty"`
	MaxLines  int    `json:"max_lines"`
}

// KnowledgeSourceMount maps a virtual prefix used by evidence_refs to an
// explicitly supplied read-only source directory. The empty prefix denotes
// the main repository.
type KnowledgeSourceMount struct {
	ReferencePrefix string `json:"reference_prefix"`
	Directory       string `json:"directory"`
}

type KnowledgeReadResult struct {
	Status     string          `json:"status"`
	ReasonCode string          `json:"reason_code,omitempty"`
	Source     KnowledgeSource `json:"source"`
	StartLine  int             `json:"start_line,omitempty"`
	EndLine    int             `json:"end_line,omitempty"`
	TotalLines int             `json:"total_lines,omitempty"`
	Text       string          `json:"text,omitempty"`
	Truncated  bool            `json:"truncated,omitempty"`
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
	if ValidateKnowledgeSourceMounts(mounts) != nil || request.Reference == "" || request.StartLine < 0 ||
		request.MaxLines <= 0 || request.MaxLines > KnowledgeDiscoveryMaxLines {
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
	result := KnowledgeReadResult{Status: KnowledgeDiscoveryStopped, Source: *source}
	mount, relativeSource, mounted := selectKnowledgeSourceMount(mounts, source.Path)
	if !mounted {
		return stoppedKnowledgeRead(*source, KnowledgeDiscoveryMountUnavailable), nil
	}
	root, err := filepath.Abs(mount.Directory)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return stoppedKnowledgeRead(*source, KnowledgeDiscoveryMountUnavailable), nil
		}
		return result, err
	}
	path, safe := safeKnowledgeSourcePath(root, relativeSource)
	if !safe {
		return stoppedKnowledgeRead(*source, KnowledgeDiscoveryPathUnsafe), nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return stoppedKnowledgeRead(*source, KnowledgeDiscoverySourceNotFound), nil
		}
		return result, err
	}
	if _, safe = safeResolvedKnowledgeSourcePath(root, resolved); !safe {
		return stoppedKnowledgeRead(*source, KnowledgeDiscoveryPathUnsafe), nil
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return stoppedKnowledgeRead(*source, KnowledgeDiscoverySourceNotFound), nil
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
		return stoppedKnowledgeRead(*source, KnowledgeDiscoverySourceTooLarge), nil
	}
	if len(data) == 0 || !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return stoppedKnowledgeRead(*source, KnowledgeDiscoverySourceNotText), nil
	}
	lines := splitKnowledgeLines(string(data))
	start := request.StartLine
	if start == 0 {
		start = 1
		if source.Locator != "" {
			located := locateKnowledgeLine(lines, source.Locator)
			if located == 0 {
				return stoppedKnowledgeRead(*source, KnowledgeDiscoveryLocatorNotFound), nil
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
		return stoppedKnowledgeRead(*source, KnowledgeDiscoveryRangeInvalid), nil
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

func ValidateKnowledgeSourceMounts(mounts []KnowledgeSourceMount) error {
	if len(mounts) == 0 {
		return errors.New("EXPERIMENT_KNOWLEDGE_SOURCE_MOUNTS_INVALID")
	}
	seen := make(map[string]bool, len(mounts))
	for _, mount := range mounts {
		prefix := mount.ReferencePrefix
		if strings.TrimSpace(mount.Directory) == "" || seen[prefix] ||
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

func locateKnowledgeLine(lines []string, locator string) int {
	candidates := []string{locator}
	if index := strings.LastIndex(locator, "::"); index >= 0 && index+2 < len(locator) {
		candidates = append(candidates, locator[index+2:])
	}
	if index := strings.LastIndex(locator, "."); index >= 0 && index+1 < len(locator) {
		candidates = append(candidates, locator[index+1:])
	}
	for _, candidate := range compactKnowledgeLocators(candidates) {
		for index, line := range lines {
			if strings.Contains(line, candidate) {
				return index + 1
			}
		}
	}
	return 0
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
		if result.ReasonCode == "" || result.Text != "" || result.StartLine != 0 || result.EndLine != 0 {
			return errors.New("EXPERIMENT_KNOWLEDGE_READ_RESULT_INVALID")
		}
		return nil
	}
	if result.Status != KnowledgeDiscoveryCompleted || result.ReasonCode != "" || result.Source.Reference == "" ||
		result.StartLine <= 0 || result.EndLine < result.StartLine || result.TotalLines < result.EndLine || result.Text == "" {
		return fmt.Errorf("EXPERIMENT_KNOWLEDGE_READ_RESULT_INVALID: %s", result.Status)
	}
	return nil
}

package contractonly

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
)

const (
	maxGeneratedFiles = 20
	maxGeneratedBytes = 512 << 10
)

var allowedImports = map[string]bool{
	"bytes": true, "context": true, "crypto/sha256": true,
	"encoding/base64": true, "encoding/hex": true, "encoding/json": true,
	"errors": true, "fmt": true, "io": true, "math": true,
	"sort": true, "strconv": true, "strings": true,
	"sync": true,
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter":  true,
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core":     true,
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage": true,
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver":   true,
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/host":     true,
	"go.etcd.io/raft/v3":        true,
	"go.etcd.io/raft/v3/raftpb": true,
}

func ValidateProposal(proposal *Proposal) ([]Finding, string) {
	var findings []Finding
	add := func(code, message string) {
		findings = append(findings, Finding{Stage: "proposal", Code: code, Message: message, Actionable: true})
	}
	if proposal == nil {
		return []Finding{{Stage: "proposal", Code: "proposal.nil", Message: "proposal is nil", Actionable: true}}, ""
	}
	if proposal.Version != Version || proposal.ID == "" {
		add("proposal.identity", "proposal version must be 1 and id must be non-empty")
	}
	if len(proposal.Files) == 0 || len(proposal.Files) > maxGeneratedFiles {
		add("proposal.files", fmt.Sprintf("proposal needs 1-%d generated files", maxGeneratedFiles))
	}
	seen := make(map[string]bool)
	scenarios := make(map[string]bool)
	goFiles := 0
	total := 0
	files := append([]GeneratedFile(nil), proposal.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	hash := sha256.New()
	for _, file := range files {
		total += len(file.Content)
		if file.Path == "" || file.Path != path.Clean(file.Path) || path.IsAbs(file.Path) || strings.HasPrefix(file.Path, "../") || seen[file.Path] {
			add("file.path", fmt.Sprintf("generated path %q is empty, duplicated, or unsafe", file.Path))
			continue
		}
		seen[file.Path] = true
		if strings.ContainsRune(file.Content, '\x00') {
			add("file.content", fmt.Sprintf("generated file %q contains NUL", file.Path))
		}
		switch {
		case strings.HasPrefix(file.Path, "integration/") && strings.HasSuffix(file.Path, ".go") && !strings.HasSuffix(file.Path, "_test.go"):
			goFiles++
			for _, finding := range validateGoFile(file) {
				findings = append(findings, finding)
			}
		case strings.HasPrefix(file.Path, "scenarios/") && strings.HasSuffix(file.Path, ".json"):
			scenarios[file.Path] = true
			if err := validateScenario(file.Content); err != nil {
				add("scenario.invalid", fmt.Sprintf("%s: %v", file.Path, err))
			}
		default:
			add("file.path", fmt.Sprintf("generated path %q is outside integration/*.go and scenarios/*.json", file.Path))
		}
		_, _ = io.WriteString(hash, file.Path)
		_, _ = hash.Write([]byte{0})
		_, _ = io.WriteString(hash, file.Content)
		_, _ = hash.Write([]byte{0})
	}
	if total > maxGeneratedBytes {
		add("proposal.size", fmt.Sprintf("generated content is %d bytes; maximum is %d", total, maxGeneratedBytes))
	}
	if goFiles == 0 {
		add("proposal.driver", "proposal must contain at least one integration/*.go file")
	}
	if len(scenarios) == 0 {
		add("proposal.scenarios", "proposal must contain at least one scenarios/*.json file")
	}
	for _, witness := range proposal.Binding.Witnesses {
		if !scenarios[witness.Scenario] {
			add("binding.witness-file", fmt.Sprintf("witness %q references non-generated scenario %q", witness.ID, witness.Scenario))
		}
	}
	bindingJSON, err := json.Marshal(proposal.Binding)
	if err != nil {
		add("binding.json", err.Error())
	} else {
		_, _ = io.WriteString(hash, "binding.json")
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(bindingJSON)
	}
	return findings, hex.EncodeToString(hash.Sum(nil))
}

func ComponentDigests(proposal *Proposal) (string, string) {
	if proposal == nil {
		return "", ""
	}
	files := append([]GeneratedFile(nil), proposal.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	fileHash := sha256.New()
	for _, file := range files {
		_, _ = io.WriteString(fileHash, file.Path)
		_, _ = fileHash.Write([]byte{0})
		_, _ = io.WriteString(fileHash, file.Content)
		_, _ = fileHash.Write([]byte{0})
	}
	binding, err := json.Marshal(proposal.Binding)
	if err != nil {
		return hex.EncodeToString(fileHash.Sum(nil)), ""
	}
	bindingSum := sha256.Sum256(binding)
	return hex.EncodeToString(fileHash.Sum(nil)), hex.EncodeToString(bindingSum[:])
}

func validateGoFile(file GeneratedFile) []Finding {
	add := func(code, message string) Finding {
		return Finding{Stage: "proposal", Code: code, Message: fmt.Sprintf("%s: %s", file.Path, message), Actionable: true}
	}
	var findings []Finding
	for _, directive := range []string{"//go:build", "// +build", "//go:generate", "//go:embed", "//go:linkname"} {
		if strings.Contains(file.Content, directive) {
			findings = append(findings, add("go.directive", fmt.Sprintf("directive %q is forbidden", directive)))
		}
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), file.Path, file.Content, parser.AllErrors)
	if err != nil {
		return append(findings, add("go.parse", err.Error()))
	}
	if parsed.Name.Name != "integration" {
		findings = append(findings, add("go.package", "package must be integration"))
	}
	for _, imported := range parsed.Imports {
		name, err := strconv.Unquote(imported.Path.Value)
		if err != nil || !allowedImports[name] {
			findings = append(findings, add("go.import", fmt.Sprintf("import %s is not allowlisted", imported.Path.Value)))
		}
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.Name == "init" {
			findings = append(findings, add("go.init", "init functions are forbidden"))
		}
	}
	return findings
}

func validateScenario(content string) error {
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	decoder.DisallowUnknownFields()
	var spec scenario.Spec
	if err := decoder.Decode(&spec); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return spec.Validate()
}

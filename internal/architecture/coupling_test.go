package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// This test makes the architectural claim executable: protocol-specific
// packages may depend on the core, but the core must not depend on a SUT.
func TestInternalPackagesDoNotImportConcreteConsensus(t *testing.T) {
	repositoryRoot := locateRepository(t)
	internalRoot := filepath.Join(repositoryRoot, "internal")
	forbidden := []string{"go.etcd.io/" + "raft", "github.com/hashicorp/" + "raft"}
	checkImports(t, internalRoot, func(importPath string) bool {
		for _, prefix := range forbidden {
			if strings.HasPrefix(importPath, prefix) {
				return true
			}
		}
		return false
	})
}

// The v2 control plane must remain a standalone replacement boundary. This
// prevents a convenient fixture or conformance helper from silently pulling
// old Runtime packages or a concrete protocol back into the new core.
func TestControlV2PackagesDoNotImportLegacyRuntimeOrProtocol(t *testing.T) {
	repositoryRoot := locateRepository(t)
	roots := []string{
		"internal/blackbox",
		"internal/control",
		"internal/controlentropy",
		"internal/controlruntime",
		"internal/controlexperiment",
		"internal/conformance",
		"internal/psscore",
		"adapters/fixture",
	}
	forbiddenPrefixes := []string{
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/core",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/host",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine",
		"github.com/SuzumiyaHaruki/consensus-atlas/families/",
		"github.com/SuzumiyaHaruki/consensus-atlas/adapters/",
		"go.etcd.io/raft",
		"github.com/hashicorp/raft",
	}
	for _, root := range roots {
		checkImports(t, filepath.Join(repositoryRoot, root), func(importPath string) bool {
			for _, forbidden := range forbiddenPrefixes {
				if strings.HasPrefix(importPath, forbidden) {
					return true
				}
			}
			return false
		})
	}
}

func TestV2AdaptersDoNotImportLegacyRuntime(t *testing.T) {
	repositoryRoot := locateRepository(t)
	forbiddenPrefixes := []string{
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/core",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/host",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine",
		"github.com/SuzumiyaHaruki/consensus-atlas/families/",
	}
	for _, root := range []string{"adapters/etcdraftv2", "adapters/hashicorpraftv2"} {
		checkImports(t, filepath.Join(repositoryRoot, root), func(importPath string) bool {
			for _, forbidden := range forbiddenPrefixes {
				if strings.HasPrefix(importPath, forbidden) {
					return true
				}
			}
			return false
		})
	}
}

func TestMigrationModelDoesNotImportEitherExecutionPath(t *testing.T) {
	repositoryRoot := locateRepository(t)
	forbiddenPrefixes := []string{
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/core",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/host",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine",
		"github.com/SuzumiyaHaruki/consensus-atlas/internal/control",
		"github.com/SuzumiyaHaruki/consensus-atlas/adapters/",
		"github.com/SuzumiyaHaruki/consensus-atlas/drivers/",
		"go.etcd.io/raft",
		"github.com/hashicorp/raft",
	}
	checkImports(t, filepath.Join(repositoryRoot, "internal/migration"), func(importPath string) bool {
		for _, forbidden := range forbiddenPrefixes {
			if strings.HasPrefix(importPath, forbidden) {
				return true
			}
		}
		return false
	})
}

func locateRepository(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate coupling test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
}

func checkImports(t *testing.T, root string, forbidden func(string) bool) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// External-package tests intentionally compose fixtures and concrete
		// Adapters. The production dependency graph is the architecture claim.
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.IMPORT {
				continue
			}
			for _, spec := range general.Specs {
				importSpec := spec.(*ast.ImportSpec)
				importPath, err := strconv.Unquote(importSpec.Path.Value)
				if err != nil {
					return err
				}
				if forbidden(importPath) {
					t.Errorf("protocol coupling: %s imports %s", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

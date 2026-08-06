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
// packages may depend on the core, but the core must not depend on etcd/raft.
func TestInternalPackagesDoNotImportEtcdRaft(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate coupling test")
	}
	internalRoot := filepath.Clean(filepath.Join(filepath.Dir(current), ".."))
	forbidden := "go.etcd.io/" + "raft"
	err := filepath.WalkDir(internalRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
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
				if strings.HasPrefix(importPath, forbidden) {
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

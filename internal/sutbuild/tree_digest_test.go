package sutbuild

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDigestTreeDetectsNonTargetSourceChanges(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "raft.go")
	nonTarget := filepath.Join(root, "log.go")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nonTarget, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := digestTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nonTarget, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := digestTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("changing a non-target module source did not change the module tree digest")
	}
}

func TestReplaceModuleSourceUsesAuditedStagingCopy(t *testing.T) {
	input := []byte(`module example.test/project

go 1.24

replace example.test/other => ./suts/other
replace example.test/sut => ./suts/sut
`)
	output, err := replaceModuleSource(input, "example.test/sut", "./artifacts/build-work/module")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(output, []byte("replace example.test/sut")) != 1 ||
		!bytes.Contains(output, []byte("replace example.test/sut => ./artifacts/build-work/module")) ||
		bytes.Contains(output, []byte("replace example.test/other => ./suts/other")) {
		t.Fatalf("unexpected generated modfile:\n%s", output)
	}
	if _, err := replaceModuleSource([]byte("replace (\n)\n"), "example.test/sut", "./stage"); err == nil {
		t.Fatal("replace block was silently accepted")
	}
}

func TestBuildStagesAnEditableLocalModule(t *testing.T) {
	repo := t.TempDir()
	moduleDir := filepath.Join(repo, "suts", "sut")
	commandDir := filepath.Join(repo, "cmd", "app")
	for _, directory := range []string{moduleDir, commandDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"go.mod":                               "module example.test/project\n\ngo 1.24\n\nrequire example.test/sut v1.0.0\n\nreplace example.test/sut => ./suts/sut\n",
		"go.sum":                               "",
		filepath.Join("suts", "sut", "go.mod"): "module example.test/sut\n\ngo 1.24\n",
		filepath.Join("suts", "sut", "sut.go"): "package sut\n\nconst Value = \"local\"\n",
		filepath.Join("suts", "sut", ".git"):   "gitdir: /machine/local/metadata\n",
		filepath.Join("cmd", "app", "main.go"): "package main\n\nimport (\"fmt\"; \"example.test/sut\")\n\nvar BuildIdentity = \"unsealed\"\nfunc main() { fmt.Print(sut.Value, BuildIdentity) }\n",
	}
	for relative, content := range files {
		if err := os.WriteFile(filepath.Join(repo, relative), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	moduleDigest, err := digestTree(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Version: SpecVersion4, ID: "local-source-build", TrialID: "trial",
		Module:       ModuleIdentity{Path: "example.test/sut", Version: "v1.0.0"},
		ModuleDigest: moduleDigest, Package: "./cmd/app",
		IdentityVariable: "example.test/project/cmd/app.BuildIdentity",
		CommandAllowlist: []string{CommandGoBuild, CommandGoListModule},
		OutputPath:       "artifacts/bin/app",
	}
	spec.SUTBuildIdentity = OpaqueBuildIdentity(spec.Module, moduleDigest, spec.Package)
	audit, err := Build(repo, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Validate(); err != nil {
		t.Fatal(err)
	}
	if audit.InputModuleDigest != moduleDigest || audit.OutputModuleDigest != moduleDigest ||
		audit.SUTBuildIdentity != spec.SUTBuildIdentity {
		t.Fatalf("unexpected local source audit: %+v", audit)
	}
}

func TestModuleTreeIgnoresCheckoutMetadataButCopiesSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/sut\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /machine/one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := digestTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /machine/two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := digestTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("Git administrative metadata changed the source tree digest")
	}

	destination := filepath.Join(t.TempDir(), "copy")
	if err := copyModule(root, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "go.mod")); err != nil {
		t.Fatalf("copied source is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, ".git")); !os.IsNotExist(err) {
		t.Fatalf("copied module retained Git metadata: %v", err)
	}
}

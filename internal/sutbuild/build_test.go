package sutbuild_test

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

func TestApplyTransformationRequiresExactlyOneMatch(t *testing.T) {
	transformation := sutbuild.SourceTransformation{Match: "needle", Replacement: "replacement"}
	for _, test := range []struct {
		name  string
		input string
		count int
		ok    bool
	}{
		{name: "absent", input: "", count: 0},
		{name: "unique", input: "before needle after", count: 1, ok: true},
		{name: "duplicate", input: "needle needle", count: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, count, err := sutbuild.ApplyTransformation([]byte(test.input), transformation)
			if count != test.count || (err == nil) != test.ok {
				t.Fatalf("count=%d err=%v", count, err)
			}
			if test.ok && string(output) != "before replacement after" {
				t.Fatalf("output = %q", output)
			}
		})
	}
}

func TestApplySourceTransformationAppliesOrderedUniqueReplacements(t *testing.T) {
	transformation := sutbuild.SourceTransformation{Replacements: []sutbuild.ExactReplacement{
		{Match: "one", Replacement: "two"},
		{Match: "two", Replacement: "three"},
	}}
	output, count, err := sutbuild.ApplySourceTransformation([]byte("one"), transformation)
	if err != nil || count != 2 || string(output) != "three" {
		t.Fatalf("output=%q count=%d err=%v", output, count, err)
	}
	transformation.Replacements[1].Match = "missing"
	if _, count, err := sutbuild.ApplySourceTransformation([]byte("one"), transformation); err == nil || count != 1 {
		t.Fatalf("missing second replacement count=%d err=%v", count, err)
	}
}

func TestSpecRequiresExactCommandAllowlist(t *testing.T) {
	spec := sutbuild.Spec{
		Version: 1, ID: "build", TrialID: "trial", Package: "./cmd/campaign",
		Module: sutbuild.ModuleIdentity{Path: "example.invalid/module", Version: "v1.0.0"},
		Source: sutbuild.SourceTransformation{
			RelativePath: "source.go", OriginalDigest: strings.Repeat("a", 64), Match: "x",
			Replacement: "y", TransformedDigest: strings.Repeat("b", 64),
		}, OutputPath: "artifacts/bin/campaign", IdentityVariable: "example.identity.Variable",
	}
	spec.SUTBuildIdentity = sutbuild.OpaqueBuildIdentity(spec.Module, spec.Source.TransformedDigest, spec.Package)
	if err := spec.Validate(); err == nil {
		t.Fatal("spec without command allowlist was accepted")
	}
	spec.CommandAllowlist = []string{sutbuild.CommandGoBuild, sutbuild.CommandGoListModule}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.CommandAllowlist = append(spec.CommandAllowlist, "shell")
	if err := spec.Validate(); err == nil {
		t.Fatal("expanded command allowlist was accepted")
	}
}

func TestVersion2SpecBindsACompleteSourceSet(t *testing.T) {
	sources := []sutbuild.SourceTransformation{
		{RelativePath: "a.go", OriginalDigest: strings.Repeat("a", 64), TransformedDigest: strings.Repeat("b", 64), Replacements: []sutbuild.ExactReplacement{{Match: "a", Replacement: "b"}}},
		{RelativePath: "b.go", OriginalDigest: strings.Repeat("c", 64), TransformedDigest: strings.Repeat("d", 64), Replacements: []sutbuild.ExactReplacement{{Match: "c", Replacement: "d"}}},
	}
	identity, err := sutbuild.TransformedSourceSetDigest(sources)
	if err != nil {
		t.Fatal(err)
	}
	spec := sutbuild.Spec{
		Version: sutbuild.SpecVersion2, ID: "build-v2", TrialID: "trial-v2", Package: "./cmd/campaign",
		Module: sutbuild.ModuleIdentity{Path: "example.invalid/module", Version: "v1.0.0"}, Sources: sources,
		OutputPath: "artifacts/bin/campaign", IdentityVariable: "example.identity.Variable", CommandAllowlist: []string{sutbuild.CommandGoBuild, sutbuild.CommandGoListModule},
	}
	spec.SUTBuildIdentity = sutbuild.OpaqueBuildIdentity(spec.Module, identity, spec.Package)
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.Sources[1].RelativePath = "a.go"
	if err := spec.Validate(); err == nil {
		t.Fatal("version-2 spec accepted duplicate source paths")
	}
}

func TestVersion3SpecBindsAnUnmodifiedModuleTree(t *testing.T) {
	moduleDigest := strings.Repeat("e", 64)
	spec := sutbuild.Spec{
		Version: sutbuild.SpecVersion3, ID: "build-v3", TrialID: "trial-v3", Package: "./cmd/campaign",
		Module: sutbuild.ModuleIdentity{Path: "example.invalid/module", Version: "v1.0.0"}, ModuleDigest: moduleDigest,
		OutputPath: "artifacts/bin/campaign", CommandAllowlist: []string{sutbuild.CommandGoBuild, sutbuild.CommandGoListModule},
	}
	spec.SUTBuildIdentity = sutbuild.OpaqueBuildIdentity(spec.Module, moduleDigest, spec.Package)
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.Sources = []sutbuild.SourceTransformation{{RelativePath: "a.go"}}
	if err := spec.Validate(); err == nil {
		t.Fatal("version-3 spec accepted a source conversion")
	}
}

func TestVersion4RequiresExplicitIdentityVariable(t *testing.T) {
	moduleDigest := strings.Repeat("e", 64)
	spec := sutbuild.Spec{
		Version: sutbuild.SpecVersion4, ID: "build-v4", TrialID: "trial-v4", Package: "./cmd/campaign",
		Module: sutbuild.ModuleIdentity{Path: "example.invalid/module", Version: "v1.0.0"}, ModuleDigest: moduleDigest,
		OutputPath: "artifacts/bin/campaign", CommandAllowlist: []string{sutbuild.CommandGoBuild, sutbuild.CommandGoListModule},
	}
	spec.SUTBuildIdentity = sutbuild.OpaqueBuildIdentity(spec.Module, moduleDigest, spec.Package)
	if err := spec.Validate(); err == nil {
		t.Fatal("version-4 spec without an identity variable was accepted")
	}
	spec.IdentityVariable = "example.identity.Variable"
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
}

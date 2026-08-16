package controlexperiment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeDiscoveryReadsOnlyDeclaredBoundedRepositorySources(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "source"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		"package source", "", "type Node struct{}", "", "func (Node) Tick() {", "}", "", "func helper() {}",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "source", "node.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	pack := knowledgeDiscoveryFixture(t, []string{
		"source/node.go:Tick", "../outside.txt:secret",
	})
	catalog, err := KnowledgeSourceCatalog(pack)
	if err != nil || len(catalog) != 2 || catalog[1].Reference != "source/node.go:Tick" ||
		len(catalog[1].MaterialIDs) != 1 || catalog[1].MaterialIDs[0] != "runtime-loop" {
		t.Fatalf("unexpected source catalog: %#v/%v", catalog, err)
	}
	result, err := ReadDeclaredKnowledgeSource(root, pack, KnowledgeReadRequest{
		Reference: "source/node.go:Tick", MaxLines: 3,
	})
	if err != nil || result.Validate() != nil || result.Status != KnowledgeDiscoveryCompleted ||
		!strings.Contains(result.Text, "Tick") || result.EndLine-result.StartLine+1 != 3 || !result.Truncated {
		t.Fatalf("declared source was not read as a bounded locator excerpt: %#v/%v", result, err)
	}
	unknown, err := ReadDeclaredKnowledgeSource(root, pack, KnowledgeReadRequest{
		Reference: "go.mod", MaxLines: 10,
	})
	if err != nil || unknown.Validate() != nil || unknown.ReasonCode != KnowledgeDiscoveryReferenceUnknown {
		t.Fatalf("undeclared source did not stop mechanically: %#v/%v", unknown, err)
	}
	unsafe, err := ReadDeclaredKnowledgeSource(root, pack, KnowledgeReadRequest{
		Reference: "../outside.txt:secret", MaxLines: 10,
	})
	if err != nil || unsafe.Validate() != nil || unsafe.ReasonCode != KnowledgeDiscoveryPathUnsafe {
		t.Fatalf("declared traversal escaped the repository boundary: %#v/%v", unsafe, err)
	}
}

func TestKnowledgeDiscoveryRejectsSymlinkEscapeAndMissingLocator(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\nconst Secret = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ordinary.go"), []byte("package ordinary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pack := knowledgeDiscoveryFixture(t, []string{"linked.go:Secret", "ordinary.go:Missing"})
	escape, err := ReadDeclaredKnowledgeSource(root, pack, KnowledgeReadRequest{
		Reference: "linked.go:Secret", MaxLines: 10,
	})
	if err != nil || escape.Validate() != nil || escape.ReasonCode != KnowledgeDiscoveryPathUnsafe {
		t.Fatalf("symlink escape was not rejected: %#v/%v", escape, err)
	}
	missing, err := ReadDeclaredKnowledgeSource(root, pack, KnowledgeReadRequest{
		Reference: "ordinary.go:Missing", MaxLines: 10,
	})
	if err != nil || missing.Validate() != nil || missing.ReasonCode != KnowledgeDiscoveryLocatorNotFound {
		t.Fatalf("missing locator did not produce mechanical feedback: %#v/%v", missing, err)
	}
}

func TestKnowledgeDiscoveryUsesLongestExplicitSourceMount(t *testing.T) {
	repository := t.TempDir()
	dependency := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dependency, "doc.go"),
		[]byte("package protocol\n// Ready documents host ordering.\ntype Ready struct{}\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	pack := knowledgeDiscoveryFixture(t, []string{"example.org/protocol@v1/doc.go:Ready"})
	result, err := ReadDeclaredKnowledgeSourceFromMounts([]KnowledgeSourceMount{
		{Directory: repository},
		{ReferencePrefix: "example.org/protocol@v1/", Directory: dependency},
	}, pack, KnowledgeReadRequest{Reference: "example.org/protocol@v1/doc.go:Ready", MaxLines: 20})
	if err != nil || result.Validate() != nil || result.Status != KnowledgeDiscoveryCompleted ||
		!strings.Contains(result.Text, "type Ready") {
		t.Fatalf("explicit dependency mount was not selected: %#v/%v", result, err)
	}
	unmounted, err := ReadDeclaredKnowledgeSourceFromMounts(
		[]KnowledgeSourceMount{{ReferencePrefix: "another.example/", Directory: repository}},
		pack, KnowledgeReadRequest{Reference: "example.org/protocol@v1/doc.go:Ready", MaxLines: 20},
	)
	if err != nil || unmounted.Validate() != nil ||
		unmounted.ReasonCode != KnowledgeDiscoveryMountUnavailable {
		t.Fatalf("missing dependency mount did not stop mechanically: %#v/%v", unmounted, err)
	}
}

func knowledgeDiscoveryFixture(t *testing.T, references []string) ProtocolKnowledgePack {
	t.Helper()
	pack, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "knowledge-discovery-fixture", Family: "leader-based-cft", Protocol: "fixture-consensus",
		Knowledge:  []KnowledgeStatement{{ID: "primer", Text: "A participant advances through host-driven ticks."}},
		Properties: []ProtocolProperty{{ID: "progress", Summary: "A submitted operation can make progress."}},
		IssuePatterns: []HistoricalIssuePattern{{
			ID: "tick-boundary", Summary: "Host tick ordering can affect progress.", Mechanism: "Tick handling is delayed.",
		}},
		TargetDossier: &TargetDossier{
			Scope: "Repository fixture.",
			Components: []TargetMaterial{{
				ID: "runtime-loop", Summary: "The host advances a participant.", EvidenceRefs: references,
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

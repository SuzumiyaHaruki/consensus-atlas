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

func TestKnowledgeDiscoveryContinuesFromExplicitDeclaredLine(t *testing.T) {
	root := t.TempDir()
	content := strings.Join([]string{
		"package source", "", "func Round() {", "  prepare()", "  retry()", "}",
		"", "func retry() {", "  sendAgain()", "}",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "node.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	pack := knowledgeDiscoveryFixture(t, []string{"node.go:Round"})
	first, err := ReadDeclaredKnowledgeSource(root, pack, KnowledgeReadRequest{
		Reference: "node.go:Round", MaxLines: 4,
	})
	if err != nil || first.Validate() != nil || first.Status != KnowledgeDiscoveryCompleted ||
		first.StartLine != 1 || first.EndLine != 4 {
		t.Fatalf("initial locator window failed: %#v/%v", first, err)
	}
	continued, err := ReadDeclaredKnowledgeSource(root, pack, KnowledgeReadRequest{
		Reference: "node.go:Round", StartLine: first.EndLine + 1, MaxLines: 4,
	})
	if err != nil || continued.Validate() != nil || continued.Status != KnowledgeDiscoveryCompleted ||
		continued.StartLine != 5 || continued.EndLine != 8 || !strings.Contains(continued.Text, "func retry") {
		t.Fatalf("explicit continuation window failed: %#v/%v", continued, err)
	}
	outOfRange, err := ReadDeclaredKnowledgeSource(root, pack, KnowledgeReadRequest{
		Reference: "node.go:Round", StartLine: 100, MaxLines: 4,
	})
	if err != nil || outOfRange.Validate() != nil ||
		outOfRange.ReasonCode != KnowledgeDiscoveryRangeInvalid {
		t.Fatalf("out-of-range continuation did not stop mechanically: %#v/%v", outOfRange, err)
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

func TestKnowledgeDiscoveryStopsWhenLocatorIsAmbiguous(t *testing.T) {
	root := t.TempDir()
	content := "package ordinary\nfunc Check() {}\nfunc Check() {}\n"
	if err := os.WriteFile(filepath.Join(root, "ordinary.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	pack := knowledgeDiscoveryFixture(t, []string{"ordinary.go:Check"})
	result, err := ReadDeclaredKnowledgeSource(root, pack, KnowledgeReadRequest{
		Reference: "ordinary.go:Check", MaxLines: 10,
	})
	if err != nil || result.Validate() != nil || result.ReasonCode != KnowledgeDiscoveryLocatorAmbiguous {
		t.Fatalf("ambiguous locator did not stop mechanically: %#v/%v", result, err)
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

func TestKnowledgeDiscoverySearchesNeutralMountedSourcesBeforeBoundedRead(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "protocol"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "protocol", "election.go"),
		[]byte("package protocol\n\nfunc tallyVotes() {\n  // quorum transition\n}\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "protocol", "election_test.go"),
		[]byte("package protocol\n// quorum transition in a directed regression\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".git", "hidden.go"), []byte("package hidden\n// quorum transition\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	mounts := []KnowledgeSourceMount{{ReferencePrefix: "sut/", Directory: root}}
	search, err := SearchMountedKnowledgeSources(mounts, KnowledgeReadRequest{
		Query: "quorum transition", MaxResults: 5,
	})
	if err != nil || search.Validate() != nil || search.Status != KnowledgeDiscoveryCompleted ||
		len(search.Matches) != 1 || search.Matches[0].Reference != "sut/protocol/election.go" ||
		search.Matches[0].Line != 4 {
		t.Fatalf("neutral mounted search drifted: %#v/%v", search, err)
	}
	read, err := ReadMountedKnowledgeSourceFromMounts(
		mounts,
		KnowledgeSource{Reference: search.Matches[0].Reference, Path: search.Matches[0].Reference},
		KnowledgeReadRequest{Reference: search.Matches[0].Reference, StartLine: 2, MaxLines: 3},
	)
	if err != nil || read.Validate() != nil || read.Status != KnowledgeDiscoveryCompleted ||
		!strings.Contains(read.Text, "tallyVotes") || read.StartLine != 2 || read.EndLine != 4 {
		t.Fatalf("bounded read of discovered source failed: %#v/%v", read, err)
	}
}

func knowledgeDiscoveryFixture(t *testing.T, references []string) ProtocolKnowledgePack {
	t.Helper()
	pack, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "knowledge-discovery-fixture", Family: "leader-based-cft", Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{ID: "primer", Text: "A participant advances through host-driven ticks."}},
		Properties: []ProtocolProperty{{
			ID: "progress", Summary: "A submitted operation can make progress.",
			EvidenceLevel: PropertyEvidenceHypothesis,
		}},
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

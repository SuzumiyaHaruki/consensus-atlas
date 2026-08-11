package sutbuild

import (
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

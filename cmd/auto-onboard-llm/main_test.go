package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSecretFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "key.txt")
	if err := os.WriteFile(path, []byte("not-a-real-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSecretFile(path); err != nil {
		t.Fatalf("private regular file rejected: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateSecretFile(path); err == nil {
		t.Fatal("group-readable key file accepted")
	}
	link := filepath.Join(directory, "key-link.txt")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := validateSecretFile(link); err == nil {
		t.Fatal("symlink key file accepted")
	}
}

func TestLoadScenariosUsesRestrictedPrefix(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := loadScenarios(root, "etcdraft-")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) < 5 {
		t.Fatalf("got %d etcdraft scenarios, want at least 5", len(candidates))
	}
	for _, candidate := range candidates {
		if filepath.Dir(candidate.Path) != "scenarios" {
			t.Fatalf("scenario escaped allowed directory: %q", candidate.Path)
		}
		if !strings.HasPrefix(filepath.Base(candidate.Path), "etcdraft-") {
			t.Fatalf("scenario bypassed prefix: %q", candidate.Path)
		}
	}
}

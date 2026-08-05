package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSecretFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "key.txt")
	if err := os.WriteFile(path, []byte("not-a-real-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSecretFile(path); err != nil {
		t.Fatalf("private key file rejected: %v", err)
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

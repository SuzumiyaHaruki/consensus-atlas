package modelcommand_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/modelcommand"
)

func TestRunPassesOnlyExplicitEnvironment(t *testing.T) {
	t.Setenv("MODEL_COMMAND_HELPER", "1")
	t.Setenv("MODEL_COMMAND_UNRELATED", "must-not-pass")
	result, err := modelcommand.Run(context.Background(), modelcommand.Config{
		Path: os.Args[0], Args: []string{"-test.run=TestModelCommandHelper"}, Dir: t.TempDir(),
		Env:         []string{"MODEL_COMMAND_HELPER=1", "MODEL_COMMAND_EXPLICIT=visible"},
		StdoutLimit: 1024, StderrLimit: 1024,
	}, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "visible||payload" {
		t.Fatalf("isolated command output = %q", result)
	}
}

func TestValidateSecretFileRejectsOpenModeAndSymlink(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "key.txt")
	if err := os.WriteFile(path, []byte("test-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := modelcommand.ValidateSecretFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := modelcommand.ValidateSecretFile(path); err == nil {
		t.Fatal("group-readable key was accepted")
	}
	link := filepath.Join(directory, "key-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := modelcommand.ValidateSecretFile(link); err == nil {
		t.Fatal("key symlink was accepted")
	}
}

func TestModelCommandHelper(t *testing.T) {
	if os.Getenv("MODEL_COMMAND_HELPER") != "1" {
		return
	}
	payload, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	fmt.Printf("%s|%s|%s", os.Getenv("MODEL_COMMAND_EXPLICIT"),
		os.Getenv("MODEL_COMMAND_UNRELATED"), payload)
	os.Exit(0)
}

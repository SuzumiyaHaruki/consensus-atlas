package contractonly

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func LoadContexts(repoRoot, moduleCache string) ([]SourceFile, Target, error) {
	apiPaths := []string{
		"families/raft/state.go",
		"internal/adapter/adapter.go",
		"internal/core/model.go",
		"internal/coverage/coverage.go",
		"internal/driver/driver.go",
		"internal/host/adapter.go",
		"internal/scenario/scenario.go",
	}
	api, err := loadFiles(repoRoot, apiPaths)
	if err != nil {
		return nil, Target{}, err
	}
	moduleRoot := filepath.Join(moduleCache, "go.etcd.io", "raft", "v3@v3.6.0")
	targetPaths := []string{"rawnode.go", "node.go", "storage.go", "status.go", "raftpb/raft.proto"}
	sources, err := loadFiles(moduleRoot, targetPaths)
	if err != nil {
		return nil, Target{}, err
	}
	configExcerpt, err := loadLineRange(filepath.Join(moduleRoot, "raft.go"), 110, 340)
	if err != nil {
		return nil, Target{}, err
	}
	sources = append(sources, SourceFile{Path: "raft.go#L110-L340", Content: configExcerpt})
	return api, Target{Module: "go.etcd.io/raft/v3", Version: "v3.6.0", Sources: sources}, nil
}

func loadFiles(root string, paths []string) ([]SourceFile, error) {
	result := make([]SourceFile, 0, len(paths))
	for _, relative := range paths {
		full := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(full)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("source context is not a regular file: %s", full)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		result = append(result, SourceFile{Path: filepath.ToSlash(relative), Content: string(data)})
	}
	return result, nil
}

func loadLineRange(path string, first, last int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		if line >= first && line <= last {
			lines = append(lines, scanner.Text())
		}
		if line > last {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if len(lines) != last-first+1 {
		return "", fmt.Errorf("source %s ended before line %d", path, last)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func sourceDigest(sources []SourceFile) string {
	copy := append([]SourceFile(nil), sources...)
	sort.Slice(copy, func(i, j int) bool { return copy[i].Path < copy[j].Path })
	hash := sha256.New()
	for _, source := range copy {
		_, _ = io.WriteString(hash, source.Path)
		_, _ = hash.Write([]byte{0})
		_, _ = io.WriteString(hash, source.Content)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

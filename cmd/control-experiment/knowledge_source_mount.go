package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type repeatableStringFlag []string

func (values *repeatableStringFlag) Set(value string) error {
	if values == nil || strings.TrimSpace(value) == "" {
		return errors.New("empty repeatable flag value")
	}
	*values = append(*values, value)
	return nil
}

func (values repeatableStringFlag) String() string {
	return strings.Join(values, ",")
}

func prepareKnowledgeSourceMounts(values []string) ([]controlexperiment.KnowledgeSourceMount, error) {
	if len(values) == 0 {
		return nil, nil
	}
	mounts := make([]controlexperiment.KnowledgeSourceMount, 0, len(values))
	for _, value := range values {
		prefix, directory, found := strings.Cut(value, "=")
		if !found || strings.TrimSpace(directory) == "" {
			return nil, errors.New("AGENTIC_KNOWLEDGE_SOURCE_MOUNT_INVALID")
		}
		if prefix == "repo" {
			prefix = ""
		} else if prefix == "" {
			return nil, errors.New("AGENTIC_KNOWLEDGE_SOURCE_MOUNT_INVALID")
		}
		resolved, err := filepath.Abs(directory)
		if err != nil {
			return nil, errors.New("AGENTIC_KNOWLEDGE_SOURCE_MOUNT_INVALID")
		}
		resolved, err = filepath.EvalSymlinks(resolved)
		if err != nil {
			return nil, errors.New("AGENTIC_KNOWLEDGE_SOURCE_MOUNT_INVALID")
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return nil, errors.New("AGENTIC_KNOWLEDGE_SOURCE_MOUNT_INVALID")
		}
		mounts = append(mounts, controlexperiment.KnowledgeSourceMount{
			ReferencePrefix: prefix, Directory: resolved,
		})
	}
	if controlexperiment.ValidateKnowledgeSourceMounts(mounts) != nil {
		return nil, errors.New("AGENTIC_KNOWLEDGE_SOURCE_MOUNT_INVALID")
	}
	return mounts, nil
}

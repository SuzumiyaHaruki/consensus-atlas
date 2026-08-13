package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
)

func readM521hJSONFile[T any](t *testing.T, path string, limit int64) T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) > limit {
		t.Fatalf("read %s: size=%d err=%v", path, len(data), err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("%s has trailing JSON", path)
	}
	return value
}

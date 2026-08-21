package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

func readStrictJSONFile(path string, limit int64, target any) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > limit {
		return errors.New("CONTROL_EXPERIMENT_JSON_FILE_INVALID")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, limit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("CONTROL_EXPERIMENT_JSON_TRAILING_DATA")
	}
	return nil
}

// Package history implements JSON file persistence independently of task execution.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// JSON stores one collection in a JSON file. The caller controls its record type.
type JSON[T any] struct{ path string }

func NewJSON[T any](path string) (*JSON[T], error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	return &JSON[T]{path: abs}, nil
}

func (s *JSON[T]) Load() ([]T, error) {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []T
	if err = json.Unmarshal(b, &records); err != nil {
		return nil, fmt.Errorf("invalid task history: %w", err)
	}
	return records, nil
}

func (s *JSON[T]) Save(records []T) error {
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	temp := s.path + ".tmp"
	if err = os.WriteFile(temp, b, 0600); err != nil {
		return err
	}
	return os.Rename(temp, s.path)
}

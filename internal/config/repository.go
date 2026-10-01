package config

import (
	"os"
)

// ShouldPull defaults to true for omitted settings and existing history records.
func (r Repository) ShouldPull() bool { return r.PullAfterPush == nil || *r.PullAfterPush }

// ValidatePath checks the configured directory without invoking Git or creating it.
func (r Repository) ValidatePath() error {
	info, err := os.Stat(r.Path)
	if os.IsNotExist(err) {
		return &PathError{Path: r.Path, Problem: "does not exist", Cause: err}
	}
	if err != nil {
		problem := "cannot be accessed"
		if os.IsPermission(err) {
			problem = "access denied"
		}
		return &PathError{Path: r.Path, Problem: problem, Cause: err}
	}
	if !info.IsDir() {
		return &PathError{Path: r.Path, Problem: "is not a directory"}
	}
	return nil
}

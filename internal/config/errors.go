package config

import "fmt"

// FieldError identifies the configuration setting responsible for a failure.
type FieldError struct {
	Field string
	Err   error
	Hint  string
}

func (e *FieldError) Error() string { return fmt.Sprintf("%s: %v", e.Field, e.Err) }
func (e *FieldError) Unwrap() error { return e.Err }

// At attaches a configuration field and an actionable hint to an error.
func At(field string, err error, hint string) error {
	return &FieldError{Field: field, Err: err, Hint: hint}
}

// PathError preserves the OS cause without repeating platform-specific details.
type PathError struct {
	Path, Problem string
	Cause         error
}

func (e *PathError) Error() string { return fmt.Sprintf("path \"%s\" %s", e.Path, e.Problem) }
func (e *PathError) Unwrap() error { return e.Cause }

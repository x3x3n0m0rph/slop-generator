// Package form describes configuration fields without depending on a UI.
package form

import (
	"fmt"
	"strconv"
	"strings"
)

type Kind string

const (
	Text      Kind = "text"
	Multiline Kind = "multiline"
	Number    Kind = "number"
	Boolean   Kind = "boolean"
	Choice    Kind = "choice"
)

type Field struct {
	ID, Label, Value string
	Kind             Kind
	Options          []string
	Required         bool
	Check            func(string) error
}
type Answers map[string]string

func (f Field) Validate(value string) error {
	if f.Required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", f.Label)
	}
	if f.Kind == Choice {
		found := false
		for _, option := range f.Options {
			if option == value {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("invalid choice for %s", f.Label)
		}
	}
	if value == "" && !f.Required {
		return nil
	}
	switch f.Kind {
	case Number:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("%s must be an integer", f.Label)
		}
	case Boolean:
		if value != "true" && value != "false" {
			return fmt.Errorf("%s must be true or false", f.Label)
		}
	}
	if f.Check != nil {
		return f.Check(value)
	}
	return nil
}

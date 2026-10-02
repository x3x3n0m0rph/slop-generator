package stage

import (
	"context"
	"fmt"
	"slop-generator/internal/config"
	"slop-generator/internal/form"
	"slop-generator/internal/inference"
	"strings"
)

type Generator interface {
	Generate(context.Context, inference.Request) (inference.Result, error)
}
type Artifact struct{ Path string }
type Artifacts []Artifact
type Candidate struct {
	Previous      Artifacts
	Path, Content string
}
type Diagnostic struct{ Message string }
type GenerationConfig struct{ Filename, Instruction, SystemInstruction string }

func GenerationFields(id string, values map[string]string) ([]form.Field, error) {
	for key := range values {
		if key != "filename" && key != "instruction" && key != "system_instruction" {
			return nil, fmt.Errorf("stage %s: unknown setting %s", id, key)
		}
	}
	var fields []form.Field
	for _, key := range []string{"filename", "instruction"} {
		if strings.TrimSpace(values[key]) == "" {
			kind := form.Text
			if key == "instruction" {
				kind = form.Multiline
			}
			field := form.Field{ID: id + "." + key, Label: id + ": " + key, Kind: kind, Required: true}
			if key == "filename" {
				field.Check = func(value string) error {
					if !config.SafeRelative(value) {
						return fmt.Errorf("unsafe filename")
					}
					return nil
				}
			}
			fields = append(fields, field)
		}
	}
	return fields, nil
}
func ConfigureGeneration(values map[string]string) GenerationConfig {
	return GenerationConfig{Filename: values["filename"], Instruction: values["instruction"], SystemInstruction: values["system_instruction"]}
}

type Generate struct {
	Config      GenerationConfig
	Generator   Generator
	Python      bool
	RecordUsage func(*inference.Usage)
	messages    []inference.Message
}

func (s *Generate) Run(ctx context.Context, _ *RunContext, in Input[Artifacts, Diagnostic]) (Output[Candidate, Unit], error) {
	if in.Feedback == nil {
		instruction := "Return only the file contents, without commentary or Markdown fences."
		if s.Python {
			instruction += " Return valid Python source."
		}
		s.messages = []inference.Message{{Role: "system", Content: s.Config.SystemInstruction + "\n" + instruction}, {Role: "user", Content: s.Config.Instruction}}
	} else {
		s.messages = append(s.messages, inference.Message{Role: "user", Content: "Fix the Python compilation error. Return only corrected Python source.\n" + in.Feedback.Message})
	}
	result, err := s.Generator.Generate(ctx, inference.Request{Messages: append([]inference.Message(nil), s.messages...)})
	if s.RecordUsage != nil {
		s.RecordUsage(result.Usage)
	}
	if err != nil {
		return Output[Candidate, Unit]{}, err
	}
	content := result.Content
	if s.Python {
		content = stripFence(content)
	}
	if strings.TrimSpace(content) == "" {
		return Output[Candidate, Unit]{}, fmt.Errorf("empty generated content")
	}
	s.messages = append(s.messages, inference.Message{Role: "assistant", Content: content})
	return Output[Candidate, Unit]{Status: Success, Result: Candidate{Previous: append(Artifacts(nil), in.Result...), Path: s.Config.Filename, Content: content}}, nil
}
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) >= 3 && (strings.TrimSpace(lines[0]) == "```python" || strings.TrimSpace(lines[0]) == "```") && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		return strings.Join(lines[1:len(lines)-1], "\n")
	}
	return s
}

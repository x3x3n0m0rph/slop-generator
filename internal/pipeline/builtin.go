package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"slop-generator/internal/inference"
)

// Builtin implements the Python and text generation pipelines.
type Builtin struct{}

func (Builtin) Run(ctx context.Context, r Runtime) error {
	if r.Config.Type != "python" && r.Config.Type != "text" {
		return fmt.Errorf("unknown pipeline type")
	}
	for _, j := range r.Config.Jobs {
		instruction := "Return only the file contents, without commentary or Markdown fences."
		if r.Config.Type == "python" {
			instruction += " Return valid Python source."
		}
		msgs := []inference.Message{{Role: "system", Content: r.Config.Instruction + "\n" + instruction}, {Role: "user", Content: j.Instruction}}
		path := filepath.Join(r.OutputDir, j.ID+j.Extension)
		for attempt := 0; ; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			r.Event(fmt.Sprintf("Generating %s (attempt %d)", j.ID, attempt+1))
			result, err := r.Generator.Generate(ctx, inference.Request{Messages: msgs})
			r.RecordUsage(result.Usage)
			if err != nil {
				return err
			}
			content := result.Content
			if r.Config.Type == "python" {
				content = stripFence(content)
			}
			if strings.TrimSpace(content) == "" {
				return fmt.Errorf("job %s returned empty content", j.ID)
			}
			if err = os.WriteFile(path, []byte(content), 0600); err != nil {
				return err
			}
			if r.Config.Type == "text" {
				break
			}
			r.Event("Checking " + j.ID)
			diagnostic, invalid, err := CheckPython(ctx, r.Python, path, filepath.Join(r.CacheDir, j.ID+".pyc"))
			if err != nil {
				return err
			}
			if !invalid {
				break
			}
			if attempt == 3 {
				return fmt.Errorf("job %s: py_compile failed after three repairs", j.ID)
			}
			msgs = append(msgs, inference.Message{Role: "assistant", Content: content}, inference.Message{Role: "user", Content: "Fix the Python compilation error. Return only corrected Python source.\n" + diagnostic})
		}
		r.JobComplete(j.ID)
	}
	return nil
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) >= 3 && (strings.TrimSpace(lines[0]) == "```python" || strings.TrimSpace(lines[0]) == "```") && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		return strings.Join(lines[1:len(lines)-1], "\n")
	}
	return s
}

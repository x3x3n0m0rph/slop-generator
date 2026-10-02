package tui

import (
	"os"
	"path/filepath"
	"slop-generator/internal/config"
	"slop-generator/internal/form"
	"slop-generator/internal/task"
	"testing"
	"time"
)

// Explicitly opt in to capture actual View output for documentation previews.
func TestCaptureUIPreviews(t *testing.T) {
	dir := os.Getenv("SLOP_UI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview capture not requested")
	}
	created := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	tasks := []task.Task{{ID: "20261002T120000-example", PipelineName: "python-functions", RepoName: "playground", ProviderName: "openrouter", Provider: config.Provider{Model: "openai/gpt-4.1-mini"}, Repo: config.Repository{Branch: "main"}, Status: "running", StageTotal: 6, Completed: 3, Step: "Running read-lines.validate", Created: created, Updated: created.Add(20 * time.Second), Events: []string{"Running insertion-sort.generate", "Running insertion-sort.validate", "Running read-lines.generate", "Running read-lines.validate", "Retrying read-lines.generate after read-lines.validate (1/3)"}}}
	// Fixed terminal elapsed time: an interrupted sample avoids wall-clock drift.
	tasks[0].Status = "interrupted"
	progress := ui{engine: &stubController{}, tasks: tasks, width: 110, height: 24}
	wizard := progress
	wizard.creating = true
	wizard.stage = 3
	wizard.fields = []form.Field{{ID: "quadratic.generate.instruction", Label: "quadratic.generate: instruction", Kind: form.Multiline, Required: true}}
	wizard.value = "Implement real_roots(a, b, c).\nReturn sorted unique real roots and handle linear cases."
	for name, m := range map[string]ui{"stage-progress": progress, "stage-form": wizard} {
		assertScreenBounds(t, m)
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

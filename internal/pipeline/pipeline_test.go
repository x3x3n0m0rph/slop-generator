package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"slop-generator/internal/config"
	"slop-generator/internal/inference"
)

type generatorFunc func(context.Context, inference.Request) (inference.Result, error)

func (f generatorFunc) Generate(ctx context.Context, r inference.Request) (inference.Result, error) {
	return f(ctx, r)
}

func TestBuiltinAcceptsGeneratorInterface(t *testing.T) {
	dir := t.TempDir()
	completed := 0
	usage := 0
	generator := generatorFunc(func(ctx context.Context, r inference.Request) (inference.Result, error) {
		if len(r.Messages) != 2 || r.Messages[1].Content != "write a note" {
			t.Fatal("incorrect request")
		}
		return inference.Result{Content: "a note", Usage: &inference.Usage{Total: 3}}, nil
	})
	err := (Builtin{}).Run(context.Background(), Runtime{Generator: generator, Config: config.Pipeline{Type: "text", Jobs: []config.Job{{ID: "note", Instruction: "write a note", Extension: ".md"}}}, OutputDir: dir, Event: func(string) {}, RecordUsage: func(u *inference.Usage) { usage += u.Total }, JobComplete: func(string) { completed++ }})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "note.md"))
	if err != nil || string(data) != "a note" || completed != 1 || usage != 3 {
		t.Fatal("pipeline did not save generator output")
	}
}

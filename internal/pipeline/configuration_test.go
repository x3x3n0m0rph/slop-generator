package pipeline

import (
	"os"
	"path/filepath"
	"slop-generator/internal/config"
	"strings"
	"testing"
)

func TestExampleConfigurationMatchesDefinitions(t *testing.T) {
	b, err := os.ReadFile("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	text := strings.Replace(string(b), "path: ../playground", "path: "+filepath.ToSlash(dir), 1)
	file := filepath.Join(dir, "config.yaml")
	if err = os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{Definitions: Builtins()}
	for name, p := range cfg.Pipelines {
		fields, err := engine.Fields(p)
		if err != nil || len(fields) != 0 {
			t.Fatalf("example profile %s incomplete: %v %v", name, fields, err)
		}
	}
}

func TestStageFieldsAndConfigurationValidation(t *testing.T) {
	engine := &Engine{Definitions: Builtins()}
	p := config.Pipeline{Definition: "python-functions", Stages: map[string]map[string]string{
		"insertion-sort.generate": {"filename": "insertion-sort.py", "instruction": "sort"},
		"read-lines.generate":     {"filename": "read-lines.py"},
		"quadratic.generate":      {"filename": "quadratic.py", "instruction": "roots"},
	}}
	fields, err := engine.Fields(p)
	if err != nil || len(fields) != 1 || fields[0].ID != "read-lines.generate.instruction" {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	for _, tc := range []struct{ id, key, value string }{
		{"unknown", "instruction", "write"}, {"insertion-sort.generate", "unknown", "value"},
		{"insertion-sort.generate", "filename", "../outside.py"}, {"read-lines.generate", "filename", "insertion-sort.py"},
		{"insertion-sort.generate", "filename", "one.txt"}, {"insertion-sort.validate", "unknown", "value"},
	} {
		copy := config.Pipeline{Definition: p.Definition, Stages: map[string]map[string]string{}}
		for id, v := range p.Stages {
			copy.Stages[id] = map[string]string{}
			for k, value := range v {
				copy.Stages[id][k] = value
			}
		}
		if copy.Stages[tc.id] == nil {
			copy.Stages[tc.id] = map[string]string{}
		}
		copy.Stages[tc.id][tc.key] = tc.value
		if _, err := engine.Fields(copy); err == nil {
			t.Fatalf("invalid setting accepted: %+v", tc)
		}
	}
}

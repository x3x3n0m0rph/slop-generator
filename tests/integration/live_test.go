package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"slop-generator/internal/config"
	"slop-generator/internal/inference"
	"slop-generator/internal/stage"
)

// Explicit opt-in: normal tests never use credentials or consume API tokens.
func TestLiveInference(t *testing.T) {
	if os.Getenv("SLOP_LIVE") != "1" {
		t.Skip("set SLOP_LIVE=1 to exercise OpenRouter")
	}
	settings, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := settings.Providers["openrouter"]
	if !ok {
		t.Fatal("config.yaml requires an openrouter provider for this test")
	}
	cfg.MaxTokens = 256
	cfg.Timeout = 30
	p, err := inference.NewHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	request := inference.Request{Messages: []inference.Message{{Role: "system", Content: "Return only Python source without Markdown fences or commentary."}, {Role: "user", Content: "Implement add(a, b), returning a + b."}}}
	result, err := p.Generate(ctx, request)
	p.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Content) == "" {
		t.Fatal("empty live response")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "result.py")
	if err = os.WriteFile(source, []byte(strings.TrimSpace(result.Content)), 0600); err != nil {
		t.Fatal(err)
	}
	_, invalid, err := stage.CheckPython(ctx, "python", source, filepath.Join(dir, "result.pyc"))
	if err != nil || invalid {
		t.Fatalf("live response failed Python validation: %v", err)
	}
	t.Log("OpenRouter generation and Python validation succeeded")
}

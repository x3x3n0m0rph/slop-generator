package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDefaultsAndValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "repo"), 0700); err != nil {
		t.Fatal(err)
	}
	base := `providers:
  test:
    base_url: https://example.com/v1
    model: test
    key_file: token.txt
repositories:
  test:
    path: repo
    branch: main
pipelines:
  test:
    definition: python-functions
    stages:
      insertion-sort.generate:
        filename: one.py
        instruction: write
`
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(base), 0600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Parallelism != 2 || c.Providers["test"].MaxTokens != 4096 || c.Repositories["test"].Remote != "origin" || c.Pipelines["test"].Stages["insertion-sort.generate"]["filename"] != "one.py" || c.Providers["test"].KeyFile != filepath.Join(dir, "token.txt") {
		t.Fatalf("%+v", c)
	}
	if !c.Repositories["test"].ShouldPull() {
		t.Fatal("pull_after_push must default to true")
	}
	for _, bad := range []string{base + "unknown: true\n", strings.Replace(base, "definition: python-functions", "definition: ", 1), strings.Replace(base, "branch: main", "branch: main\n    output_dir: ../outside", 1), strings.Replace(base, "key_file: token.txt", "key_file: token.txt\n    key_env: TOKEN", 1)} {
		os.WriteFile(p, []byte(bad), 0600)
		if _, err := Load(p); err == nil {
			t.Fatal("invalid config accepted")
		}
	}

	inline := strings.Replace(base, "key_file: token.txt", "api_key: inline-test-key", 1)
	os.WriteFile(p, []byte(inline), 0600)
	c, err = Load(p)
	if err != nil || c.Providers["test"].APIKey != "inline-test-key" || len(c.Pipelines["test"].Stages) != 1 {
		t.Fatal("single-file configuration failed", err)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(inline, "branch: main", "branch: main\n    pull_after_push: false", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(p)
	if err != nil || c.Repositories["test"].ShouldPull() {
		t.Fatalf("explicit pull disable ignored: %v", err)
	}
	for _, bad := range []string{
		strings.Replace(inline, "definition: python-functions", "type: python", 1),
		strings.Replace(inline, "definition: python-functions", "definition: python-functions\n    config:\n      max_retries: -1", 1),
		strings.Replace(inline, "api_key: inline-test-key", "api_key: inline-test-key\n    key_env: TOKEN", 1),
	} {
		os.WriteFile(p, []byte(bad), 0600)
		if _, err := Load(p); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "not-a-directory"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want string }{{"missing", "does not exist"}, {"not-a-directory", "not a directory"}} {
		t.Run(tc.path, func(t *testing.T) {
			if err := os.WriteFile(p, []byte(strings.Replace(base, "path: repo", "path: "+tc.path, 1)), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(p)
			if err == nil || !strings.Contains(err.Error(), `repository "test"`) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("unexpected path error: %v", err)
			}
		})
	}
}

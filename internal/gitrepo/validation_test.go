package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"slop-generator/internal/config"
)

func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	s, err := run(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return s
}
func validRepository(t *testing.T) config.Repository {
	t.Helper()
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	testGit(t, dir, "config", "user.name", "Test")
	testGit(t, dir, "config", "user.email", "test@example.com")
	testGit(t, dir, "commit", "--allow-empty", "-m", "initial")
	// Intentionally unreachable: validation must not attempt network access.
	testGit(t, dir, "remote", "add", "origin", "https://example.invalid/repo.git")
	return config.Repository{Path: dir, Branch: "main", Remote: "origin"}
}
func TestRepositoryValidation(t *testing.T) {
	r := validRepository(t)
	client := Client{}
	if err := client.Validate(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()
	file := filepath.Join(plain, "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(r.Path, "sub")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, path, branch, remote, want string }{
		{"missing", filepath.Join(plain, "missing"), "main", "origin", "does not exist"},
		{"file", file, "main", "origin", "not a directory"},
		{"plain directory", plain, "main", "origin", "not a readable Git repository"},
		{"subdirectory", sub, "main", "origin", "select its root"},
		{"branch missing", r.Path, "missing", "origin", "local branch"},
		{"invalid branch", r.Path, "bad..name", "origin", "invalid branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := client.ValidateAll(context.Background(), map[string]config.Repository{"target": {Path: tc.path, Branch: tc.branch, Remote: tc.remote}})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), `repository "target"`) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWorktreeAndBareValidation(t *testing.T) {
	r := validRepository(t)
	root := t.TempDir()
	work := filepath.Join(root, "worktree")
	testGit(t, r.Path, "worktree", "add", "-b", "work", work)
	if err := (Client{}).Validate(context.Background(), config.Repository{Path: work, Branch: "work", Remote: "origin"}); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(root, "bare.git")
	testGit(t, root, "clone", "--bare", r.Path, bare)
	if err := (Client{}).Validate(context.Background(), config.Repository{Path: bare, Branch: "main", Remote: "origin"}); err != nil {
		t.Fatal(err)
	}
}

func TestValidationWithoutRemote(t *testing.T) {
	r := validRepository(t)
	testGit(t, r.Path, "remote", "remove", "origin")
	if err := (Client{}).ValidateAll(context.Background(), map[string]config.Repository{"target": r}); err != nil {
		t.Fatal("repository without origin must pass startup validation:", err)
	}
	if _, err := (Client{}).Identity(context.Background(), r); err != nil {
		t.Fatal("repository without origin must allow task creation:", err)
	}
}

func TestRepositoryRemovedBeforeEnqueue(t *testing.T) {
	r := validRepository(t)
	r.Path = filepath.Join(r.Path, "missing")
	if _, err := (Client{}).Identity(context.Background(), r); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnbornBranchHasAccurateError(t *testing.T) {
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "master")
	r := config.Repository{Path: dir, Branch: "master", Remote: "origin"}
	err := (Client{}).Validate(context.Background(), r)
	if err == nil || !strings.Contains(err.Error(), "нет первого коммита") || strings.Contains(err.Error(), "local branch") {
		t.Fatalf("unborn branch misclassified: %v", err)
	}
	if testGit(t, dir, "symbolic-ref", "HEAD") != "refs/heads/master" {
		t.Fatal("validation changed HEAD")
	}
	if _, err = run(context.Background(), dir, "rev-parse", "--verify", "HEAD"); err == nil {
		t.Fatal("validation created a commit")
	}
	r.Branch = "missing"
	err = (Client{}).Validate(context.Background(), r)
	if err == nil || !strings.Contains(err.Error(), "local branch") || strings.Contains(err.Error(), "нет первого коммита") {
		t.Fatalf("missing branch misclassified: %v", err)
	}
}

func TestBranchLookupCancellationIsNotMissingBranch(t *testing.T) {
	r := validRepository(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := branchCommit(ctx, r)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation misclassified: %v", err)
	}
}

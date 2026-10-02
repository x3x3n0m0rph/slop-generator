package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitExplicitPathsAndRepeat(t *testing.T) {
	r := validRepository(t)
	ctx := context.Background()
	client := Client{}
	for _, name := range []string{"result.txt", "unrelated.txt"} {
		if err := os.WriteFile(filepath.Join(r.Path, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sha, err := client.Commit(ctx, r.Path, []string{"result.txt"}, "Save result", filepath.Join(t.TempDir(), "no-hooks"))
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := client.Commit(ctx, r.Path, []string{"result.txt"}, "Save result", filepath.Join(t.TempDir(), "no-hooks"))
	if err != nil || repeated != sha {
		t.Fatalf("repeat created another commit: %s %v", repeated, err)
	}
	files := testGit(t, r.Path, "ls-tree", "-r", "--name-only", "HEAD")
	if strings.Contains(files, "unrelated.txt") || !strings.Contains(files, "result.txt") {
		t.Fatal(files)
	}
	for _, paths := range [][]string{nil, {"../outside"}, {".git/config"}} {
		if _, err := client.Commit(ctx, r.Path, paths, "Bad", ""); err == nil {
			t.Fatal("unsafe commit accepted")
		}
	}
}

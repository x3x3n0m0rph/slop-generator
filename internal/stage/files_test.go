package stage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspacePathBoundaries(t *testing.T) {
	dir := Directory(t.TempDir())
	for _, name := range []string{"../outside", ".git/config", "/absolute", "C:/outside", "."} {
		if _, err := dir.Path(name); err == nil {
			t.Fatalf("unsafe path %q accepted", name)
		}
	}
	if _, err := dir.Path("nested/file.py"); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	link := filepath.Join(string(dir), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	if _, err := dir.Path("link/file.py"); err == nil {
		t.Fatal("symlink redirection accepted")
	}
}
func TestCommitRequiresCapability(t *testing.T) {
	if _, err := (&Commit{}).Run(context.Background(), &RunContext{}, Input[Artifacts, Unit]{}); err == nil {
		t.Fatal("ungranted commit capability accepted")
	}
}

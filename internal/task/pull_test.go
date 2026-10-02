package task

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDefaultPullUpdatesSourceAndPreservesUnrelatedChanges(t *testing.T) {
	r, _ := gitFixture(t)
	if err := os.WriteFile(filepath.Join(r.Path, "initial.txt"), []byte("local edits"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "untracked.txt"), []byte("untracked"), 0600); err != nil {
		t.Fatal(err)
	}
	before := mustGit(t, r.Path, "status", "--porcelain")
	e := setupEngine(t, r, testPipeline("text", "one", "write", ".txt"), func(w http.ResponseWriter, r *http.Request) { answer(w, "generated text") })
	for i := 0; i < 2; i++ {
		task := waitTask(t, e, enqueue(t, e))
		if task.Status != "succeeded" || !task.Published || mustGit(t, r.Path, "rev-parse", "HEAD") != task.SHA {
			t.Fatalf("source did not receive published commit: %+v", task)
		}
		content, err := os.ReadFile(filepath.Join(r.Path, "generated", task.ID, "one.txt"))
		if err != nil || string(content) != "generated text" {
			t.Fatalf("source output missing: %v", err)
		}
	}
	if mustGit(t, r.Path, "status", "--porcelain") != before {
		t.Fatal("pull changed unrelated local modifications")
	}
	content, _ := os.ReadFile(filepath.Join(r.Path, "initial.txt"))
	if string(content) != "local edits" {
		t.Fatal("local edits overwritten")
	}
}

func TestPullFailureRetainsPublicationAndRetryOnlyPulls(t *testing.T) {
	r, remote := gitFixture(t)
	var calls atomic.Int32
	e := setupEngine(t, r, testPipeline("text", "one", "write", ".txt"), func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		commitFile(t, r.Path, "local-change.txt")
		answer(w, "generated text")
	})
	id := enqueue(t, e)
	task := waitTask(t, e, id)
	if task.Status != "failed" || !task.Published || !strings.Contains(task.Error, "Коммит уже опубликован") {
		t.Fatalf("publication not distinguished from pull failure: %+v", task)
	}
	if mustGit(t, remote, "rev-parse", "main") != task.SHA {
		t.Fatal("commit not published")
	}
	local := mustGit(t, r.Path, "rev-parse", "HEAD")
	if local == task.SHA {
		t.Fatal("divergent source branch forcibly changed")
	}
	if _, err := os.Stat(task.Work); err != nil {
		t.Fatal("working copy lost after pull failure")
	}
	// Resolve divergence explicitly; application must never merge automatically.
	mustGit(t, r.Path, "fetch", "origin", "main")
	mustGit(t, r.Path, "merge", "--no-edit", "FETCH_HEAD")
	if err := e.RetryPublish(id); err != nil {
		t.Fatal(err)
	}
	done := waitTask(t, e, id)
	if done.Status != "succeeded" || calls.Load() != 1 || mustGit(t, remote, "rev-parse", "main") != task.SHA {
		t.Fatalf("retry generated or republished: %+v calls=%d", done, calls.Load())
	}
}

package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"slop-generator/internal/config"
	"slop-generator/internal/form"
	"slop-generator/internal/gitrepo"
	"slop-generator/internal/history"
	"slop-generator/internal/inference"
	"slop-generator/internal/pipeline"
	"slop-generator/internal/stage"
)

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	b, e := cmd.CombinedOutput()
	s := strings.TrimSpace(string(b))
	if e != nil {
		t.Fatalf("%v (%v)", e, args)
	}
	return s
}
func gitFixture(t *testing.T) (config.Repository, string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	local := filepath.Join(root, "source")
	mustGit(t, root, "init", "--bare", remote)
	mustGit(t, root, "init", "-b", "main", local)
	mustGit(t, local, "config", "user.name", "Test")
	mustGit(t, local, "config", "user.email", "test@example.com")
	os.WriteFile(filepath.Join(local, "initial.txt"), []byte("initial"), 0600)
	mustGit(t, local, "add", ".")
	mustGit(t, local, "commit", "-m", "initial")
	mustGit(t, local, "remote", "add", "origin", remote)
	mustGit(t, local, "push", "origin", "main")
	return config.Repository{Path: local, Remote: "origin", Branch: "main", OutputDir: "generated"}, remote
}
func commitFile(t *testing.T, dir, name string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); e != nil {
		t.Fatal(e)
	}
	mustGit(t, dir, "add", "--", name)
	mustGit(t, dir, "commit", "-m", name)
}
func waitTask(t *testing.T, e *Service, id string) Task {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ts, _ := e.Snapshot()
		for _, task := range ts {
			if task.ID == id && task.Status != "queued" && task.Status != "running" {
				return task
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task timed out")
	return Task{}
}
func setupEngine(t *testing.T, r config.Repository, pipeline config.Pipeline, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv("SLOP_TEST_KEY", "super-secret")
	c := config.Config{Parallelism: 2, Providers: map[string]config.Provider{"test": {BaseURL: server.URL, Model: "test", KeyEnv: "SLOP_TEST_KEY", MaxTokens: 100, Timeout: 2}}, Repositories: map[string]config.Repository{"test": r}, Pipelines: map[string]config.Pipeline{"test": pipeline}}
	e, err := newTestService(c, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}
func answer(w http.ResponseWriter, text string) {
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}}}, "usage": map[string]int{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5}})
}
func enqueue(t *testing.T, e *Service) string {
	t.Helper()
	if err := e.Enqueue("test", "test", "test"); err != nil {
		t.Fatal(err)
	}
	ts, _ := e.Snapshot()
	return ts[len(ts)-1].ID
}
func TestTextTaskPublishesAndPreservesSource(t *testing.T) {
	r, remote := gitFixture(t)
	disablePull := false
	r.PullAfterPush = &disablePull
	commitFile(t, r.Path, "local-only.txt")
	sha := mustGit(t, r.Path, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(r.Path, "dirty.txt"), []byte("leave me"), 0600)
	before := mustGit(t, r.Path, "status", "--porcelain")
	e := setupEngine(t, r, testPipeline("text", "one", "write", ".md", "two", "write", ".txt"), func(w http.ResponseWriter, r *http.Request) { answer(w, "generated text") })
	provider := e.cfg.Providers["test"]
	provider.KeyEnv = ""
	provider.APIKey = "super-secret"
	e.cfg.Providers["test"] = provider
	task := waitTask(t, e, enqueue(t, e))
	if task.Status != "succeeded" || task.Completed != 4 || task.Usage.Total != 10 {
		t.Fatalf("%+v", task)
	}
	if mustGit(t, r.Path, "rev-parse", "HEAD") != sha || mustGit(t, r.Path, "status", "--porcelain") != before {
		t.Fatal("source changed")
	}
	if _, err := os.Stat(task.Work); !os.IsNotExist(err) {
		t.Fatal("successful copy not cleaned")
	}
	files := mustGit(t, remote, "ls-tree", "-r", "--name-only", "main")
	if !strings.Contains(files, "local-only.txt") || !strings.Contains(files, task.ID+"/one.md") {
		t.Fatal(files)
	}
	b, _ := os.ReadFile(filepath.Join(e.dir, "history.json"))
	if strings.Contains(string(b), "super-secret") || strings.Contains(string(b), "SLOP_TEST_KEY") {
		t.Fatal("credential information persisted")
	}
	restored, err := newTestService(e.cfg, e.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	ts, _ := restored.Snapshot()
	if len(ts) != 1 || ts[0].SHA != task.SHA {
		t.Fatal("history lost")
	}
}
func TestPythonRepairAndFailure(t *testing.T) {
	if _, e := exec.LookPath("python"); e != nil {
		t.Skip("Python unavailable")
	}
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "repair", false: "exhausted"}[success], func(t *testing.T) {
			r, remote := gitFixture(t)
			initial := mustGit(t, remote, "rev-parse", "main")
			var count atomic.Int32
			e := setupEngine(t, r, testPipeline("python", "function", "write", ".py"), func(w http.ResponseWriter, r *http.Request) {
				n := count.Add(1)
				if success && n > 1 {
					var b struct {
						Messages []inference.Message `json:"messages"`
					}
					json.NewDecoder(r.Body).Decode(&b)
					if len(b.Messages) != 4 || !strings.Contains(b.Messages[3].Content, "SyntaxError") {
						t.Errorf("repair missing diagnostics: %+v", b.Messages)
					}
					answer(w, "```python\ndef f():\n    return 42\n```")
				} else {
					answer(w, "def broken(:")
				}
			})
			task := waitTask(t, e, enqueue(t, e))
			if success {
				if task.Status != "succeeded" || count.Load() != 2 {
					t.Fatalf("%+v calls=%d", task, count.Load())
				}
			} else {
				if task.Status != "failed" || count.Load() != 4 || mustGit(t, remote, "rev-parse", "main") != initial {
					t.Fatalf("%+v calls=%d", task, count.Load())
				}
				if _, err := os.Stat(task.Work); err != nil {
					t.Fatal("failed work lost")
				}
			}
		})
	}
}
func TestGitPreflightAndRemoteRace(t *testing.T) {
	t.Run("behind", func(t *testing.T) {
		r, remote := gitFixture(t)
		old := mustGit(t, r.Path, "rev-parse", "HEAD")
		commitFile(t, r.Path, "ahead.txt")
		mustGit(t, r.Path, "push", "origin", "main")
		mustGit(t, r.Path, "checkout", "--detach", old)
		mustGit(t, r.Path, "branch", "-f", "main", old)
		var calls atomic.Int32
		e := setupEngine(t, r, testPipeline("text", "one", "write", ".txt"), func(w http.ResponseWriter, r *http.Request) { calls.Add(1); answer(w, "text") })
		task := waitTask(t, e, enqueue(t, e))
		if task.Status != "failed" || calls.Load() != 0 || !strings.Contains(task.Error, "behind") {
			t.Fatalf("%+v", task)
		}
		_ = remote
	})
	t.Run("remote changed", func(t *testing.T) {
		r, remote := gitFixture(t)
		e := setupEngine(t, r, testPipeline("text", "one", "write", ".txt"), func(w http.ResponseWriter, req *http.Request) {
			commitFile(t, r.Path, "external.txt")
			mustGit(t, r.Path, "push", "origin", "main")
			answer(w, "text")
		})
		task := waitTask(t, e, enqueue(t, e))
		if task.Status != "failed" || task.SHA == "" {
			t.Fatalf("%+v", task)
		}
		if mustGit(t, remote, "rev-parse", "main") == task.SHA {
			t.Fatal("remote overwritten")
		}
		if _, err := os.Stat(task.Work); err != nil {
			t.Fatal(err)
		}
	})
}
func TestEmptyResponseAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{true: "cancel", false: "empty"}[cancel], func(t *testing.T) {
			r, remote := gitFixture(t)
			initial := mustGit(t, remote, "rev-parse", "main")
			started := make(chan struct{})
			e := setupEngine(t, r, testPipeline("text", "one", "write", ".txt"), func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				close(started)
				if cancel {
					<-r.Context().Done()
				} else {
					answer(w, "")
				}
			})
			id := enqueue(t, e)
			if cancel {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("request not started")
				}
				e.Cancel(id)
			}
			task := waitTask(t, e, id)
			want := "failed"
			if cancel {
				want = "cancelled"
			}
			if task.Status != want || mustGit(t, remote, "rev-parse", "main") != initial {
				t.Fatalf("%+v", task)
			}
		})
	}
}
func TestRecoveryMarksActiveTasksInterrupted(t *testing.T) {
	dir := t.TempDir()
	tasks := []Task{{ID: "one", Status: "running"}, {ID: "two", Status: "queued"}}
	b, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Tasks   []Task `json:"tasks"`
	}{2, tasks})
	os.WriteFile(filepath.Join(dir, "history.json"), b, 0600)
	e, err := newTestService(config.Config{Parallelism: 2}, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ts, _ := e.Snapshot()
	for _, task := range ts {
		if task.Status != "interrupted" {
			t.Fatal(task.Status)
		}
	}
}

func TestSchedulerParallelismAndSameRepoQueue(t *testing.T) {
	r, _ := gitFixture(t)
	other, _ := gitFixture(t)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	e := setupEngine(t, r, testPipeline("text", "one", "write", ".txt"), func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		select {
		case <-release:
			answer(w, "text")
		case <-r.Context().Done():
		}
	})
	e.cfg.Repositories["other"] = other
	first := enqueue(t, e)
	second := enqueue(t, e)
	if err := e.Enqueue("test", "other", "test"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("separate repositories did not run concurrently")
		}
	}
	tasks, _ := e.Snapshot()
	if tasks[1].Status != "queued" {
		t.Fatal("same repository ran concurrently")
	}
	close(release)
	if task := waitTask(t, e, first); task.Status != "succeeded" {
		t.Fatalf("%+v", task)
	}
	if task := waitTask(t, e, second); task.Status != "succeeded" {
		t.Fatalf("second task must use the local branch updated by pull: %+v", task)
	}
	if task := waitTask(t, e, tasks[2].ID); task.Status != "succeeded" {
		t.Fatalf("%+v", task)
	}
}

func TestRetryPublicationWithoutRegeneration(t *testing.T) {
	r, remote := gitFixture(t)
	// A remote hook rejects the first publication, then is removed.
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	e := setupEngine(t, r, testPipeline("text", "one", "write", ".txt"), func(w http.ResponseWriter, r *http.Request) { calls.Add(1); answer(w, "text") })
	id := enqueue(t, e)
	task := waitTask(t, e, id)
	if task.Status != "failed" || task.SHA == "" {
		t.Fatalf("%+v", task)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if err := e.RetryPublish(id); err != nil {
		t.Fatal(err)
	}
	task = waitTask(t, e, id)
	if task.Status != "succeeded" || calls.Load() != 1 || mustGit(t, remote, "rev-parse", "main") != task.SHA {
		t.Fatalf("%+v calls=%d", task, calls.Load())
	}
}

func newTestService(c config.Config, dir string) (*Service, error) {
	store, err := history.NewJSON[Task](filepath.Join(dir, "history.json"))
	if err != nil {
		return nil, err
	}
	definitions := map[string]pipeline.Definition{}
	for _, p := range c.Pipelines {
		var ids []string
		for id := range p.Stages {
			if strings.HasSuffix(id, ".generate") {
				ids = append(ids, strings.TrimSuffix(id, ".generate"))
			}
		}
		sort.Strings(ids)
		definitions[p.Definition] = testDefinition{ids: ids, python: p.Definition == "test-python"}
	}
	return New(c, dir, Dependencies{Pipeline: &pipeline.Engine{Git: gitrepo.Client{}, Definitions: definitions, NewProvider: func(c config.Provider) (pipeline.Provider, error) { return inference.NewHTTP(c) }}, History: store})
}

type testDefinition struct {
	ids    []string
	python bool
}

func (d testDefinition) StageCount(p config.Pipeline) int {
	n := 2 * len(d.ids)
	if p.Config.CommitMode == "stages" {
		n++
	}
	return n
}
func (d testDefinition) Fields(p config.Pipeline) ([]form.Field, error) {
	allowed := map[string]bool{}
	var fields []form.Field
	for _, id := range d.ids {
		gen := id + ".generate"
		write := id + ".write"
		if d.python {
			write = id + ".validate"
		}
		allowed[gen] = true
		allowed[write] = true
		fs, err := stage.GenerationFields(gen, p.Stages[gen])
		if err != nil {
			return nil, err
		}
		fields = append(fields, fs...)
		if d.python {
			if _, err := stage.ValidationFields(write, p.Stages[write]); err != nil {
				return nil, err
			}
		} else if len(p.Stages[write]) != 0 {
			return nil, fmt.Errorf("unknown fixture stage settings")
		}
	}
	for id := range p.Stages {
		if !allowed[id] {
			return nil, fmt.Errorf("unknown fixture stage %s", id)
		}
	}
	return fields, nil
}
func (d testDefinition) Build(p config.Pipeline, r pipeline.Resources) (pipeline.Chain[stage.Artifacts, stage.Artifacts, stage.Unit], error) {
	fields, err := d.Fields(p)
	if err != nil {
		return pipeline.Chain[stage.Artifacts, stage.Artifacts, stage.Unit]{}, err
	}
	if len(fields) != 0 {
		return pipeline.Chain[stage.Artifacts, stage.Artifacts, stage.Unit]{}, fmt.Errorf("stage configuration incomplete")
	}
	var chain pipeline.Chain[stage.Artifacts, stage.Artifacts, stage.Unit]
	for i, id := range d.ids {
		genID := id + ".generate"
		fileID := id + ".write"
		if d.python {
			fileID = id + ".validate"
		}
		gen := &stage.Generate{Config: stage.ConfigureGeneration(p.Stages[genID]), Generator: r.Generator, Python: d.python, RecordUsage: r.RecordUsage}
		var generated pipeline.Chain[stage.Artifacts, stage.Candidate, stage.Diagnostic]
		if i == 0 {
			generated = pipeline.Start[stage.Artifacts, stage.Candidate, stage.Diagnostic](genID, gen)
		} else {
			generated = pipeline.Then(chain, genID, gen)
		}
		chain = pipeline.Then(generated, fileID, &stage.WriteFile{Workspace: r.Workspace, Config: stage.ConfigureValidation(p.Stages[fileID]), Check: d.python, Cache: filepath.Join(r.CacheDir, id+".pyc")})
	}
	if p.Config.CommitMode == "stages" {
		chain = pipeline.Then(chain, "commit", &stage.Commit{Committer: r.Committer, Message: "Save fixture artifacts"})
	}
	return chain, nil
}

func testPipeline(kind string, fields ...string) config.Pipeline {
	p := config.Pipeline{Definition: "test-" + kind, Stages: map[string]map[string]string{}}
	for i := 0; i < len(fields); i += 3 {
		p.Stages[fields[i]+".generate"] = map[string]string{"filename": fields[i] + fields[i+2], "instruction": fields[i+1]}
	}
	return p
}

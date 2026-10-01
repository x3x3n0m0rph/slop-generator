package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"slop-generator/internal/config"
)

// Service owns the task queue and coordinates injected execution dependencies.
type Service struct {
	deps Dependencies

	mu           sync.Mutex
	cfg          config.Config
	dir          string
	tasks        []*Task
	running      map[string]context.CancelFunc
	busy         map[string]bool
	wg           sync.WaitGroup
	closing      bool
	storageError error
}

// New restores history and takes an owned copy of the selected configuration.
func New(c config.Config, dir string, deps Dependencies) (*Service, error) {
	if deps.Git == nil || deps.Pipeline == nil || deps.History == nil || deps.NewProvider == nil {
		return nil, fmt.Errorf("all task dependencies are required")
	}
	abs, e := filepath.Abs(dir)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(abs, 0700); e != nil {
		return nil, e
	}
	en := &Service{deps: deps, cfg: cloneConfig(c), dir: abs, running: map[string]context.CancelFunc{}, busy: map[string]bool{}}
	saved, e := deps.History.Load()
	if e != nil {
		return nil, e
	}
	for _, t := range saved {
		if t.Status == "running" || t.Status == "queued" {
			t.Status = "interrupted"
			t.Error = "application stopped before completion"
		}
		en.tasks = append(en.tasks, &t)
	}
	en.saveLocked()
	if en.storageError != nil {
		return nil, en.storageError
	}
	return en, nil
}
func (e *Service) saveLocked() {
	// Never serialize credential sources or proxy URLs into history.
	clean := make([]Task, len(e.tasks))
	for i, t := range e.tasks {
		clean[i] = publicTask(*t)
	}
	if err := e.deps.History.Save(clean); err != nil {
		e.storageError = fmt.Errorf("cannot save history: %w", err)
	} else {
		e.storageError = nil
	}
}

// Snapshot returns detached task records without credentials, plus any storage error.
func (e *Service) Snapshot() ([]Task, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Task, len(e.tasks))
	for i, t := range e.tasks {
		out[i] = publicTask(*t)
	}
	return out, e.storageError
}

// Enqueue selects profiles and schedules a new task subject to concurrency limits.
func (e *Service) Enqueue(p, r, v string) error {
	pc, ok := e.cfg.Pipelines[p]
	if !ok {
		return fmt.Errorf("unknown pipeline")
	}
	rc, ok := e.cfg.Repositories[r]
	if !ok {
		return fmt.Errorf("unknown repository")
	}
	vc, ok := e.cfg.Providers[v]
	if !ok {
		return fmt.Errorf("unknown provider")
	}
	key, err := e.deps.Git.Identity(context.Background(), rc)
	if err != nil {
		return fmt.Errorf("cannot inspect repository: %w", err)
	}
	raw := make([]byte, 8)
	if _, err = rand.Read(raw); err != nil {
		return err
	}
	id := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(raw)
	t := &Task{ID: id, PipelineName: p, RepoName: r, ProviderName: v, Pipeline: pc, Repo: rc, Provider: vc, Python: e.cfg.Python, RepoKey: key, Status: "queued", Created: time.Now(), Updated: time.Now()}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return fmt.Errorf("application is closing")
	}
	e.tasks = append(e.tasks, t)
	e.saveLocked()
	e.scheduleLocked()
	return nil
}
func (e *Service) scheduleLocked() {
	if e.closing {
		return
	}
	for _, t := range e.tasks {
		if len(e.running) >= e.cfg.Parallelism {
			return
		}
		if t.Status != "queued" || e.busy[t.RepoKey] {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		e.running[t.ID] = cancel
		e.busy[t.RepoKey] = true
		t.Status = "running"
		e.wg.Add(1)
		e.saveLocked()
		go e.execute(ctx, t)
	}
}
func (e *Service) update(t *Task, step string, fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if fn != nil {
		fn()
	}
	t.Step = step
	t.Updated = time.Now()
	t.Events = append(t.Events, time.Now().Format(time.RFC3339)+" "+step)
	e.saveLocked()
}
func (e *Service) execute(ctx context.Context, t *Task) {
	defer e.wg.Done()
	err := e.run(ctx, t)
	e.mu.Lock()
	t.Updated = time.Now()
	if err == nil {
		t.Status = "succeeded"
		t.Error = ""
	} else {
		t.Status = "failed"
		if ctx.Err() != nil {
			t.Status = "cancelled"
		}
		t.Error = err.Error()
	}
	delete(e.running, t.ID)
	delete(e.busy, t.RepoKey)
	e.saveLocked()
	e.scheduleLocked()
	e.mu.Unlock()
}

// Cancel stops a queued or active task; already published commits are not reverted.
func (e *Service) Cancel(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range e.tasks {
		if t.ID == id {
			if c := e.running[id]; c != nil {
				c()
			} else if t.Status == "queued" {
				t.Status = "cancelled"
			}
			e.saveLocked()
			return
		}
	}
}

// RetryPublish resumes publication or source pull without generating another commit.
func (e *Service) RetryPublish(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return fmt.Errorf("application is closing")
	}
	for _, t := range e.tasks {
		if t.ID == id {
			if t.SHA == "" || t.Status == "succeeded" || t.Status == "running" || t.Status == "queued" {
				return fmt.Errorf("no publication or source pull to retry")
			}
			if !t.Published {
				if _, err := os.Stat(t.Work); err != nil {
					return fmt.Errorf("saved working copy is unavailable")
				}
			}
			t.Status = "queued"
			t.Error = ""
			e.saveLocked()
			e.scheduleLocked()
			return nil
		}
	}
	return fmt.Errorf("task not found")
}

// Close cancels active work, saves history, and waits for workers to finish.
func (e *Service) Close() {
	e.mu.Lock()
	e.closing = true
	for _, c := range e.running {
		c()
	}
	for _, t := range e.tasks {
		if t.Status == "queued" {
			t.Status = "interrupted"
		}
	}
	e.saveLocked()
	e.mu.Unlock()
	e.wg.Wait()
}

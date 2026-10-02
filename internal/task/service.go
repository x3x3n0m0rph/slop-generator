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
	"slop-generator/internal/form"
	"slop-generator/internal/inference"
	"slop-generator/internal/pipeline"
	"strings"
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
	if deps.Pipeline == nil || deps.History == nil {
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
	return e.EnqueueConfigured(p, r, v, nil)
}
func (e *Service) EnqueueConfigured(p, r, v string, answers form.Answers) error {
	pc, ok := e.cfg.Pipelines[p]
	if !ok {
		return fmt.Errorf("unknown pipeline")
	}
	pc = clonePipeline(pc)
	requested, err := e.deps.Pipeline.Fields(pc)
	if err != nil {
		return err
	}
	allowed := map[string]form.Field{}
	for _, field := range requested {
		allowed[field.ID] = field
	}
	for key, value := range answers {
		field, ok := allowed[key]
		if !ok {
			return fmt.Errorf("field %s is not requested by this pipeline", key)
		}
		if err := field.Validate(value); err != nil {
			return err
		}
		i := strings.LastIndex(key, ".")
		if i < 0 {
			return fmt.Errorf("invalid field ID")
		}
		id, fieldName := key[:i], key[i+1:]
		if pc.Stages[id] == nil {
			pc.Stages[id] = map[string]string{}
		}
		pc.Stages[id][fieldName] = value
	}
	rc, ok := e.cfg.Repositories[r]
	if !ok {
		return fmt.Errorf("unknown repository")
	}
	vc, ok := e.cfg.Providers[v]
	if !ok {
		return fmt.Errorf("unknown provider")
	}
	fields, err := e.deps.Pipeline.Fields(pc)
	if err != nil {
		return err
	}
	if len(fields) != 0 {
		return fmt.Errorf("stage configuration incomplete")
	}
	key, err := e.deps.Pipeline.Identity(context.Background(), pipeline.Runtime{Repo: rc})
	if err != nil {
		return fmt.Errorf("cannot inspect repository: %w", err)
	}
	raw := make([]byte, 8)
	if _, err = rand.Read(raw); err != nil {
		return err
	}
	id := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(raw)
	t := &Task{ID: id, PipelineName: p, RepoName: r, ProviderName: v, Pipeline: pc, Repo: rc, Provider: vc, StageTotal: e.deps.Pipeline.StageCount(pc), RepoKey: key, Status: "queued", Created: time.Now(), Updated: time.Now()}
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

// record persists progress without replacing the active stage or adding log noise.
func (e *Service) record(t *Task, fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn()
	t.Updated = time.Now()
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
			if err := e.deps.Pipeline.ValidateResume(pipeline.Runtime{ID: t.ID, DataDir: e.dir, State: pipeline.State{Work: t.Work, SHA: t.SHA, Published: t.Published}}); err != nil {
				return err
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

func (e *Service) Fields(name string) ([]form.Field, error) {
	p, ok := e.cfg.Pipelines[name]
	if !ok {
		return nil, fmt.Errorf("unknown pipeline")
	}
	return e.deps.Pipeline.Fields(p)
}
func (e *Service) Rerun(id string) error {
	e.mu.Lock()
	var old Task
	found := false
	for _, t := range e.tasks {
		if t.ID == id {
			old = publicTask(*t)
			found = true
			break
		}
	}
	e.mu.Unlock()
	if !found {
		return fmt.Errorf("task not found")
	}
	provider, ok := e.cfg.Providers[old.ProviderName]
	if !ok {
		return fmt.Errorf("provider profile unavailable")
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	old.ID = time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(raw)
	// Reuse the saved public settings, resolving only credentials from the profile.
	old.Provider.APIKey = provider.APIKey
	old.Provider.KeyEnv = provider.KeyEnv
	old.Provider.KeyFile = provider.KeyFile
	old.Provider.SOCKS5 = provider.SOCKS5
	old.Provider.SOCKS5File = provider.SOCKS5File
	old.Status = "queued"
	old.Step = ""
	old.Error = ""
	old.Work = ""
	old.SHA = ""
	old.Published = false
	old.Completed = 0
	old.Commits = nil
	old.Events = nil
	old.Usage = inference.Usage{}
	old.UsageKnown = false
	old.UsageMissing = false
	old.Created = time.Now()
	old.Updated = old.Created
	fields, err := e.deps.Pipeline.Fields(old.Pipeline)
	if err != nil {
		return err
	}
	if len(fields) != 0 {
		return fmt.Errorf("saved configuration incomplete")
	}
	key, err := e.deps.Pipeline.Identity(context.Background(), pipeline.Runtime{Repo: old.Repo})
	if err != nil {
		return err
	}
	old.RepoKey = key
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return fmt.Errorf("application is closing")
	}
	e.tasks = append(e.tasks, &old)
	e.saveLocked()
	e.scheduleLocked()
	return nil
}

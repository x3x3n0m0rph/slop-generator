package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slop-generator/internal/stage"
)

type session struct {
	engine  *Engine
	runtime Runtime
	output  string
	state   State
}

func (s *session) event(message string) {
	if s.runtime.Event != nil {
		s.runtime.Event("[pipeline] " + message)
	}
}
func (s *session) save() {
	if s.runtime.SaveState != nil {
		state := s.state
		state.Commits = append([]string(nil), state.Commits...)
		s.runtime.SaveState(state)
	}
}
func (s *session) Commit(ctx context.Context, paths []string, message string) (string, error) {
	var relative []string
	for _, path := range paths {
		full, err := stage.Directory(s.output).Path(path)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(s.state.Work, full)
		if err != nil {
			return "", err
		}
		relative = append(relative, rel)
	}
	sha, err := s.engine.Git.Commit(ctx, s.state.Work, relative, message, filepath.Join(s.runtime.DataDir, "no-hooks"))
	if err == nil {
		if len(s.state.Commits) == 0 || s.state.Commits[len(s.state.Commits)-1] != sha {
			s.state.Commits = append(s.state.Commits, sha)
		}
		s.save()
	}
	return sha, err
}
func (e *Engine) Identity(ctx context.Context, r Runtime) (string, error) {
	return e.Git.Identity(ctx, r.Repo)
}
func (e *Engine) ValidateResume(r Runtime) error {
	if r.State.SHA == "" {
		return fmt.Errorf("no final commit to publish")
	}
	if r.ID == "" || filepath.Base(r.ID) != r.ID || r.ID == "." || r.ID == ".." || r.State.Work != filepath.Join(r.DataDir, "work", r.ID) {
		return fmt.Errorf("saved work directory does not belong to task")
	}
	if !r.State.Published {
		if _, err := os.Stat(r.State.Work); err != nil {
			return fmt.Errorf("saved working copy is unavailable")
		}
	}
	return nil
}
func (e *Engine) Run(ctx context.Context, r Runtime) error {
	if e.Git == nil || e.NewProvider == nil {
		return fmt.Errorf("pipeline dependencies required")
	}
	s := &session{engine: e, runtime: r, state: r.State}
	expected := filepath.Join(r.DataDir, "work", r.ID)
	if r.ID == "" || filepath.Base(r.ID) != r.ID || r.ID == "." || r.ID == ".." {
		return fmt.Errorf("invalid task ID")
	}
	if s.state.SHA != "" {
		if err := e.ValidateResume(r); err != nil {
			return err
		}
		return s.publish(ctx)
	}
	fields, err := e.Fields(r.Config)
	if err != nil {
		return err
	}
	if len(fields) != 0 {
		return fmt.Errorf("stage configuration incomplete")
	}
	s.state.Work = expected
	s.event("Preparing Git copy")
	s.save()
	if err = os.MkdirAll(filepath.Dir(expected), 0700); err != nil {
		return err
	}
	base, err := e.Git.Prepare(ctx, r.Repo, expected)
	if err != nil {
		return err
	}
	s.output, err = outputPath(expected, r.Repo.OutputDir, r.ID)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(s.output, 0700); err != nil {
		return err
	}
	p, err := e.NewProvider(r.Provider)
	if err != nil {
		return err
	}
	defer p.Close()
	// Commit capability is granted only in the explicit stage-commit mode.
	var committer stage.Committer
	if r.Config.Config.CommitMode == "stages" {
		committer = s
	}
	chain, err := e.Definitions[r.Config.Definition].Build(r.Config, Resources{Generator: p, Worktree: stage.Directory(expected), Workspace: stage.Directory(s.output), Committer: committer, CacheDir: filepath.Join(r.DataDir, "pycache", r.ID), RecordUsage: r.RecordUsage})
	if err != nil {
		return err
	}
	artifacts, err := chain.Run(ctx, nil, r.Config.Retries(), r.Event, r.Progress)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if r.Config.Config.CommitMode == "stages" {
		head, err := e.Git.Head(ctx, expected)
		if err != nil {
			return err
		}
		clean, err := e.Git.Clean(ctx, expected)
		if err != nil {
			return err
		}
		if head == base || len(s.state.Commits) == 0 || !clean {
			return fmt.Errorf("stage-commit pipeline must commit all changes before publication")
		}
		s.state.SHA = head
	} else {
		s.event("Creating commit")
		paths := make([]string, len(artifacts))
		for i, a := range artifacts {
			paths[i] = a.Path
		}
		sha, err := s.Commit(ctx, paths, "slop: "+r.Name+" "+r.ID)
		if err != nil {
			return err
		}
		s.state.SHA = sha
	}
	s.save()
	return s.publish(ctx)
}
func (s *session) publish(ctx context.Context) error {
	if !s.state.Published {
		s.event("Publishing saved commit")
		if err := s.engine.Git.Publish(ctx, s.state.Work, s.runtime.Repo.Branch, s.state.SHA); err != nil {
			return err
		}
		s.state.Published = true
		s.save()
		s.event("Commit published")
	}
	if s.runtime.Repo.ShouldPull() {
		s.event("Pulling into source repository")
		if err := s.engine.Git.Pull(ctx, s.runtime.Repo); err != nil {
			return fmt.Errorf("Коммит уже опубликован, но pull в основной репозиторий не выполнен: %w", err)
		}
		s.event("Source repository updated")
	}
	expected := filepath.Join(s.runtime.DataDir, "work", s.runtime.ID)
	if s.state.Work == expected {
		if err := os.RemoveAll(expected); err != nil {
			s.event("Published; working copy cleanup failed")
		}
	}
	s.event("Pipeline complete")
	return nil
}

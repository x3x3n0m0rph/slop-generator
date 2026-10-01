package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"slop-generator/internal/inference"
	"slop-generator/internal/pipeline"
)

func (e *Service) run(ctx context.Context, t *Task) error {
	if t.SHA != "" {
		if !t.Published {
			e.update(t, "Publishing saved commit", nil)
			if err := e.deps.Git.Publish(ctx, t.Work, t.Repo.Branch, t.SHA); err != nil {
				return err
			}
			e.update(t, "Commit published", func() { t.Published = true })
		}
		return e.finishPublication(ctx, t)
	}
	p, err := e.deps.NewProvider(t.Provider)
	if err != nil {
		return err
	}
	defer p.Close()
	e.update(t, "Preparing Git copy", func() { t.Work = filepath.Join(e.dir, "work", t.ID) })
	if err = os.MkdirAll(filepath.Dir(t.Work), 0700); err != nil {
		return err
	}
	if _, err = e.deps.Git.Prepare(ctx, t.Repo, t.Work); err != nil {
		return err
	}
	dest, err := outputPath(t.Work, t.Repo.OutputDir, t.ID)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dest, 0700); err != nil {
		return err
	}
	runtime := pipeline.Runtime{Generator: p, Config: t.Pipeline, Python: t.Python, OutputDir: dest,
		CacheDir: filepath.Join(e.dir, "pycache", t.ID),
		Event:    func(step string) { e.update(t, step, nil) },
		RecordUsage: func(u *inference.Usage) {
			e.update(t, "Recorded inference usage", func() {
				if u == nil {
					t.UsageMissing = true
					return
				}
				t.UsageKnown = true
				t.Usage.Prompt += u.Prompt
				t.Usage.Completion += u.Completion
				t.Usage.Total += u.Total
			})
		},
		JobComplete: func(id string) { e.update(t, "Saved "+id, func() { t.Completed++ }) },
	}
	if err = e.deps.Pipeline.Run(ctx, runtime); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	e.update(t, "Creating commit", nil)
	sha, err := e.deps.Git.Commit(ctx, t.Work, dest, "slop: "+t.PipelineName+" "+t.ID, filepath.Join(e.dir, "no-hooks"))
	if err != nil {
		return err
	}
	e.update(t, "Publishing commit", func() { t.SHA = sha })
	if err = e.deps.Git.Publish(ctx, t.Work, t.Repo.Branch, t.SHA); err != nil {
		return err
	}
	e.update(t, "Commit published", func() { t.Published = true })
	return e.finishPublication(ctx, t)
}

func (e *Service) finishPublication(ctx context.Context, t *Task) error {
	if t.Repo.ShouldPull() {
		e.update(t, "Pulling into source repository", nil)
		if err := e.deps.Git.Pull(ctx, t.Repo); err != nil {
			return fmt.Errorf("Коммит уже опубликован, но pull в основной репозиторий не выполнен: %w", err)
		}
		e.update(t, "Source repository updated", nil)
	}
	return e.cleanup(t)
}

func (e *Service) cleanup(t *Task) error {
	// Only remove the explicitly verified task-owned directory.
	expected := filepath.Join(e.dir, "work", t.ID)
	if t.Work == expected {
		if err := os.RemoveAll(t.Work); err != nil {
			e.update(t, "Published; working copy cleanup failed", nil)
		}
	}
	return nil
}

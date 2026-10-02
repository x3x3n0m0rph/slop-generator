package task

import (
	"context"
	"slop-generator/internal/inference"
	"slop-generator/internal/pipeline"
)

func (e *Service) run(ctx context.Context, t *Task) error {
	return e.deps.Pipeline.Run(ctx, pipeline.Runtime{ID: t.ID, Name: t.PipelineName, DataDir: e.dir, Config: t.Pipeline, Repo: t.Repo, Provider: t.Provider, State: pipeline.State{Work: t.Work, SHA: t.SHA, Published: t.Published, Commits: t.Commits},
		Event:    func(s string) { e.update(t, s, nil) },
		Progress: func(n int) { e.record(t, func() { t.Completed = n }) },
		SaveState: func(s pipeline.State) {
			e.record(t, func() {
				t.Work = s.Work
				t.SHA = s.SHA
				t.Published = s.Published
				t.Commits = append([]string(nil), s.Commits...)
			})
		},
		RecordUsage: func(u *inference.Usage) {
			e.record(t, func() {
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
	})
}

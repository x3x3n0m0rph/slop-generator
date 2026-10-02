// Package pipeline composes typed stages and owns repository execution.
package pipeline

import (
	"context"
	"slop-generator/internal/config"
	"slop-generator/internal/form"
	"slop-generator/internal/inference"
	"slop-generator/internal/stage"
)

type Generator = stage.Generator
type Provider interface {
	Generator
	Close()
}
type Repository interface {
	Identity(context.Context, config.Repository) (string, error)
	Prepare(context.Context, config.Repository, string) (string, error)
	Commit(context.Context, string, []string, string, string) (string, error)
	Head(context.Context, string) (string, error)
	Clean(context.Context, string) (bool, error)
	Publish(context.Context, string, string, string) error
	Pull(context.Context, config.Repository) error
}
type State struct {
	Work, SHA string
	Published bool
	Commits   []string
}
type Runtime struct {
	ID, Name, DataDir string
	Config            config.Pipeline
	Repo              config.Repository
	Provider          config.Provider
	State             State
	Event             func(string)
	RecordUsage       func(*inference.Usage)
	Progress          func(int)
	SaveState         func(State)
}
type Definition interface {
	StageCount(config.Pipeline) int
	Fields(config.Pipeline) ([]form.Field, error)
	Build(config.Pipeline, Resources) (Chain[stage.Artifacts, stage.Artifacts, stage.Unit], error)
}
type Resources struct {
	Generator   Generator
	Worktree    stage.Worktree
	Workspace   stage.Workspace
	Committer   stage.Committer
	CacheDir    string
	RecordUsage func(*inference.Usage)
}
type Engine struct {
	Git         Repository
	NewProvider func(config.Provider) (Provider, error)
	Definitions map[string]Definition
}

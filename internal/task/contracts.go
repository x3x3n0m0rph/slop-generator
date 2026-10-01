package task

import (
	"context"

	"slop-generator/internal/config"
	"slop-generator/internal/pipeline"
)

// Provider is the inference client lifecycle needed by task execution.
// Pipeline execution itself consumes only pipeline.Generator.
type Provider interface {
	pipeline.Generator
	Close()
}

// Pipeline defines the generation and validation sequence consumed by a task.
// Commit and push belong to task orchestration and always follow a successful run.
type Pipeline interface {
	Run(context.Context, pipeline.Runtime) error
}

// Repository defines repository operations without exposing Git commands.
type Repository interface {
	Identity(context.Context, config.Repository) (string, error)
	Prepare(context.Context, config.Repository, string) (string, error)
	Commit(ctx context.Context, work, output, message, hooksDir string) (string, error)
	Publish(ctx context.Context, work, branch, sha string) error
	Pull(context.Context, config.Repository) error
}

// Store persists task history; credentials are removed by Service before saving.
type Store interface {
	Load() ([]Task, error)
	Save([]Task) error
}

// Dependencies are supplied by the command, not constructed by Service.
type Dependencies struct {
	NewProvider func(config.Provider) (Provider, error)
	Pipeline    Pipeline
	Git         Repository
	History     Store
}

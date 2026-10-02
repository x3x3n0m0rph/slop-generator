package task

import (
	"context"
	"slop-generator/internal/config"
	"slop-generator/internal/form"
	"slop-generator/internal/pipeline"
)

type Pipeline interface {
	ValidateResume(pipeline.Runtime) error
	Run(context.Context, pipeline.Runtime) error
	Identity(context.Context, pipeline.Runtime) (string, error)
	Fields(config.Pipeline) ([]form.Field, error)
	StageCount(config.Pipeline) int
}
type Store interface {
	Load() ([]Task, error)
	Save([]Task) error
}
type Dependencies struct {
	Pipeline Pipeline
	History  Store
}

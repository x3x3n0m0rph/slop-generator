// Package pipeline executes fixed generation and validation sequences.
package pipeline

import (
	"context"

	"slop-generator/internal/config"
	"slop-generator/internal/inference"
)

// Generator is the inference capability consumed by a pipeline.
type Generator interface {
	Generate(context.Context, inference.Request) (inference.Result, error)
}

// Runtime supplies the selected definition, inference capability and progress hooks.
// Event, RecordUsage and JobComplete are provided by the task orchestrator.
type Runtime struct {
	Generator                   Generator
	Config                      config.Pipeline
	Python, OutputDir, CacheDir string
	Event                       func(string)
	RecordUsage                 func(*inference.Usage)
	JobComplete                 func(string)
}

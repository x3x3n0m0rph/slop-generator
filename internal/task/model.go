// Package task schedules tasks and orchestrates pipelines and repository publication.
package task

import (
	"time"

	"slop-generator/internal/config"
	"slop-generator/internal/inference"
)

// Task is a task record. Service snapshots and history omit provider credentials.
type Task struct {
	ID                                   string
	PipelineName, RepoName, ProviderName string
	Pipeline                             config.Pipeline
	Repo                                 config.Repository
	Provider                             config.Provider
	Python                               string
	RepoKey                              string
	Status, Step, Error, Work, SHA       string
	Completed                            int
	Published                            bool
	Usage                                inference.Usage
	UsageKnown                           bool
	UsageMissing                         bool
	Created, Updated                     time.Time
	Events                               []string
}

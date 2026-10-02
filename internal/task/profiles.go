package task

import (
	"maps"
	"slices"

	"slop-generator/internal/config"
)

// Profiles exposes only names available for creating tasks, never credentials.
type Profiles struct {
	Pipelines, Repositories, Providers []string
}

func (e *Service) Profiles() Profiles {
	return Profiles{
		Pipelines:    slices.Sorted(maps.Keys(e.cfg.Pipelines)),
		Repositories: slices.Sorted(maps.Keys(e.cfg.Repositories)),
		Providers:    slices.Sorted(maps.Keys(e.cfg.Providers)),
	}
}

func cloneConfig(c config.Config) config.Config {
	c.Providers = maps.Clone(c.Providers)
	c.Repositories = maps.Clone(c.Repositories)
	for name, r := range c.Repositories {
		c.Repositories[name] = cloneRepository(r)
	}
	c.Pipelines = maps.Clone(c.Pipelines)
	for name, p := range c.Pipelines {
		p = clonePipeline(p)
		c.Pipelines[name] = p
	}
	return c
}

func publicTask(t Task) Task {
	t.Repo = cloneRepository(t.Repo)
	t.Provider = config.Provider{BaseURL: t.Provider.BaseURL, Model: t.Provider.Model, MaxTokens: t.Provider.MaxTokens, Timeout: t.Provider.Timeout}
	t.Pipeline = clonePipeline(t.Pipeline)
	t.Commits = slices.Clone(t.Commits)
	t.Events = slices.Clone(t.Events)
	return t
}

func cloneRepository(r config.Repository) config.Repository {
	if r.PullAfterPush != nil {
		value := *r.PullAfterPush
		r.PullAfterPush = &value
	}
	return r
}

func clonePipeline(p config.Pipeline) config.Pipeline {
	p.Stages = maps.Clone(p.Stages)
	if p.Stages == nil {
		p.Stages = map[string]map[string]string{}
	}
	for id, v := range p.Stages {
		p.Stages[id] = maps.Clone(v)
	}
	if p.Config.MaxRetries != nil {
		n := *p.Config.MaxRetries
		p.Config.MaxRetries = &n
	}
	return p
}

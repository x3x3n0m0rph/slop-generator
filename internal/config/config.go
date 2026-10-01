// Package config loads and validates the single-file application configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Provider struct {
	BaseURL    string `yaml:"base_url"`
	Model      string `yaml:"model"`
	APIKey     string `yaml:"api_key"`
	KeyEnv     string `yaml:"key_env"`
	KeyFile    string `yaml:"key_file"`
	SOCKS5     string `yaml:"socks5"`
	SOCKS5File string `yaml:"socks5_file"`
	MaxTokens  int    `yaml:"max_tokens"`
	Timeout    int    `yaml:"timeout_seconds"`
}
type Repository struct {
	Path          string `yaml:"path"`
	Remote        string `yaml:"remote"`
	Branch        string `yaml:"branch"`
	OutputDir     string `yaml:"output_dir"`
	AuthorName    string `yaml:"author_name"`
	AuthorEmail   string `yaml:"author_email"`
	PullAfterPush *bool  `yaml:"pull_after_push"`
}
type Job struct {
	ID          string `yaml:"id"`
	Instruction string `yaml:"instruction"`
	Extension   string `yaml:"extension"`
}
type Pipeline struct {
	Type        string `yaml:"type"`
	Instruction string `yaml:"instruction"`
	Jobs        []Job  `yaml:"jobs"`
}
type Config struct {
	Parallelism  int                   `yaml:"parallelism"`
	Python       string                `yaml:"python"`
	Providers    map[string]Provider   `yaml:"providers"`
	Repositories map[string]Repository `yaml:"repositories"`
	Pipelines    map[string]Pipeline   `yaml:"pipelines"`
}

func readYAML(path string, dst any) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	d := yaml.NewDecoder(f)
	d.KnownFields(true)
	return d.Decode(dst)
}
func resolve(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func SafeRelative(p string) bool {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" || filepath.IsAbs(p) || strings.Contains(p, ":") {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == ".." || strings.EqualFold(s, ".git") {
			return false
		}
	}
	return p != "."
}
func Load(path string) (Config, error) {
	var c Config
	if e := readYAML(path, &c); e != nil {
		return c, e
	}
	abs, e := filepath.Abs(path)
	if e != nil {
		return c, e
	}
	base := filepath.Dir(abs)
	if c.Parallelism == 0 {
		c.Parallelism = 2
	}
	if c.Parallelism < 1 {
		return c, At("parallelism", fmt.Errorf("parallelism must be positive"), "Укажите положительное число параллельных тасок.")
	}
	if c.Python == "" {
		c.Python = "python"
	}
	if len(c.Providers) == 0 || len(c.Repositories) == 0 || len(c.Pipelines) == 0 {
		return c, fmt.Errorf("providers, repositories and pipelines are required")
	}
	for name, p := range c.Providers {
		u, e := url.Parse(p.BaseURL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return c, At("providers."+name+".base_url", fmt.Errorf("provider %s: invalid base_url", name), "Укажите полный HTTP или HTTPS URL API.")
		}
		keySources := 0
		for _, source := range []string{p.APIKey, p.KeyEnv, p.KeyFile} {
			if strings.TrimSpace(source) != "" {
				keySources++
			}
		}
		if p.Model == "" || keySources != 1 {
			return c, At("providers."+name, fmt.Errorf("provider %s: model and exactly one key source required", name), "Укажите model и один источник ключа: api_key, key_env или key_file.")
		}
		if p.SOCKS5 != "" && p.SOCKS5File != "" {
			return c, At("providers."+name+".socks5", fmt.Errorf("provider %s: choose one proxy source", name), "Оставьте только socks5 или socks5_file.")
		}
		if p.MaxTokens == 0 {
			p.MaxTokens = 4096
		}
		if p.Timeout == 0 {
			p.Timeout = 120
		}
		if p.MaxTokens < 1 || p.Timeout < 1 {
			return c, At("providers."+name, fmt.Errorf("provider %s: invalid limits", name), "max_tokens и timeout_seconds должны быть положительными.")
		}
		p.KeyFile = resolve(base, p.KeyFile)
		p.SOCKS5File = resolve(base, p.SOCKS5File)
		c.Providers[name] = p
	}
	for name, r := range c.Repositories {
		if r.Path == "" || r.Branch == "" {
			return c, At("repositories."+name, fmt.Errorf("repository %s: path and branch required", name), "Укажите path и branch для репозитория.")
		}
		r.Path = resolve(base, r.Path)
		if err := r.ValidatePath(); err != nil {
			return c, At("repositories."+name+".path", fmt.Errorf("repository %q: %w", name, err), "Укажите путь к существующему Git-репозиторию. Относительный путь считается от файла конфигурации.")
		}
		if r.Remote == "" {
			r.Remote = "origin"
		}
		if r.OutputDir == "" {
			r.OutputDir = "generated"
		}
		if !SafeRelative(r.OutputDir) {
			return c, At("repositories."+name+".output_dir", fmt.Errorf("repository %s: unsafe output_dir", name), "Укажите относительный каталог внутри репозитория без .. и .git.")
		}
		c.Repositories[name] = r
	}
	for name, p := range c.Pipelines {
		if p.Type != "python" && p.Type != "text" {
			return c, At("pipelines."+name+".type", fmt.Errorf("pipeline %s: unknown type", name), "Поддерживаются python и text.")
		}
		if len(p.Jobs) == 0 {
			return c, At("pipelines."+name+".jobs", fmt.Errorf("pipeline %s: no jobs", name), "Добавьте хотя бы одно задание.")
		}
		seen := map[string]bool{}
		for i, j := range p.Jobs {
			if !validID.MatchString(j.ID) || seen[j.ID] || strings.TrimSpace(j.Instruction) == "" {
				return c, At(fmt.Sprintf("pipelines.%s.jobs[%d]", name, i), fmt.Errorf("pipeline %s: invalid or duplicate job", name), "Задание должно иметь непустую инструкцию и уникальный id из букв, цифр, _ и -.")
			}
			seen[j.ID] = true
			if p.Type == "python" {
				j.Extension = ".py"
			} else {
				if j.Extension == "" {
					j.Extension = ".txt"
				}
				if j.Extension != ".txt" && j.Extension != ".md" {
					return c, At(fmt.Sprintf("pipelines.%s.jobs[%d].extension", name, i), fmt.Errorf("pipeline %s: unsupported extension", name), "Для текста используйте .txt или .md.")
				}
			}
			p.Jobs[i] = j
		}
		c.Pipelines[name] = p
	}
	return c, nil
}

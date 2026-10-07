// Package config loads and validates the single-file application configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Provider struct {
	BaseURL    string `yaml:"base_url"`
	Model      string `yaml:"model"`
	AuthMode   string `yaml:"auth_mode"`
	APIKey     string `yaml:"api_key"`
	KeyEnv     string `yaml:"key_env"`
	KeyFile    string `yaml:"key_file"`
	SOCKS5     string `yaml:"socks5"`
	SOCKS5File string `yaml:"socks5_file"`
	MaxTokens  int    `yaml:"max_tokens"`
	Timeout    int    `yaml:"timeout_seconds"`
	OAuth2     OAuth2 `yaml:"oauth2"`
}

type OAuth2 struct {
	WellKnownURL          string `yaml:"well_known_url"`
	ClientID              string `yaml:"client_id"`
	RedirectURI           string `yaml:"redirect_uri"`
	Scope                 string `yaml:"scope"`
	TLSInsecureSkipVerify bool   `yaml:"tls_insecure_skip_verify"`
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
type PipelineSettings struct {
	MaxRetries *int   `yaml:"max_retries"`
	CommitMode string `yaml:"commit_mode"`
}
type Pipeline struct {
	Definition string                       `yaml:"definition"`
	Config     PipelineSettings             `yaml:"config"`
	Stages     map[string]map[string]string `yaml:"stages"`
}

func (p Pipeline) Retries() int {
	if p.Config.MaxRetries == nil {
		return 3
	}
	return *p.Config.MaxRetries
}

type Config struct {
	Parallelism  int                   `yaml:"parallelism"`
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

func SafeRelative(p string) bool {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(p) || strings.Contains(p, ":") {
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
	if len(c.Providers) == 0 || len(c.Repositories) == 0 || len(c.Pipelines) == 0 {
		return c, fmt.Errorf("providers, repositories and pipelines are required")
	}
	for name, p := range c.Providers {
		u, e := url.Parse(p.BaseURL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return c, At("providers."+name+".base_url", fmt.Errorf("provider %s: invalid base_url", name), "Укажите полный HTTP или HTTPS URL API.")
		}
		if p.AuthMode == "" {
			p.AuthMode = "token"
		}
		keySources := 0
		for _, source := range []string{p.APIKey, p.KeyEnv, p.KeyFile} {
			if strings.TrimSpace(source) != "" {
				keySources++
			}
		}
		if p.Model == "" {
			return c, At("providers."+name+".model", fmt.Errorf("provider %s: model is required", name), "Укажите model провайдера.")
		}
		switch p.AuthMode {
		case "token":
			if keySources != 1 || p.OAuth2 != (OAuth2{}) {
				return c, At("providers."+name, fmt.Errorf("provider %s: token auth requires exactly one key source and no OAuth2 settings", name), "Для auth_mode: token укажите один источник ключа: api_key, key_env или key_file.")
			}
		case "oauth2":
			if keySources != 0 {
				return c, At("providers."+name, fmt.Errorf("provider %s: OAuth2 cannot be combined with an API key source", name), "Для auth_mode: oauth2 удалите api_key, key_env и key_file.")
			}
			if err := validateOAuth2(p.OAuth2); err != nil {
				return c, At("providers."+name+".oauth2", fmt.Errorf("provider %s: %w", name, err), "Проверьте well_known_url, client_id и loopback redirect_uri для OAuth2.")
			}
			if p.OAuth2.Scope == "" {
				p.OAuth2.Scope = "openid profile"
			}
		default:
			return c, At("providers."+name+".auth_mode", fmt.Errorf("provider %s: unsupported auth_mode %q", name, p.AuthMode), "Выберите auth_mode: token или oauth2.")
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
		if p.Definition == "" {
			return c, At("pipelines."+name+".definition", fmt.Errorf("pipeline definition is required"), "Choose a code-defined pipeline.")
		}
		if p.Retries() < 0 {
			return c, fmt.Errorf("pipeline %s: max_retries must not be negative", name)
		}
		if p.Config.CommitMode == "" {
			p.Config.CommitMode = "pipeline"
		}
		if p.Config.CommitMode != "pipeline" && p.Config.CommitMode != "stages" {
			return c, fmt.Errorf("pipeline %s: invalid commit_mode", name)
		}
		c.Pipelines[name] = p
	}
	return c, nil
}

func validateOAuth2(c OAuth2) error {
	wellKnown, err := url.Parse(c.WellKnownURL)
	if err != nil || wellKnown.Host == "" || (wellKnown.Scheme != "https" && wellKnown.Scheme != "http") || wellKnown.User != nil || wellKnown.Fragment != "" {
		return fmt.Errorf("invalid well_known_url")
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("client_id is required")
	}
	redirect, err := url.Parse(c.RedirectURI)
	if err != nil || redirect.Scheme != "http" || redirect.User != nil || redirect.Fragment != "" || redirect.RawQuery != "" || redirect.Path == "" {
		return fmt.Errorf("redirect_uri must be an http loopback URL with a path and explicit port")
	}
	host := strings.ToLower(redirect.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("redirect_uri host must be localhost or a loopback address")
	}
	port := redirect.Port()
	if port == "" {
		return fmt.Errorf("redirect_uri must include an explicit port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("redirect_uri has an invalid port")
	}
	return nil
}

package stage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slop-generator/internal/config"
	"slop-generator/internal/form"
)

type Workspace interface{ Path(string) (string, error) }
type Worktree interface{ Root() string }
type Committer interface {
	Commit(context.Context, []string, string) (string, error)
}
type ValidationConfig struct{ Python string }

func ValidationFields(id string, v map[string]string) ([]form.Field, error) {
	for k := range v {
		if k != "python" {
			return nil, fmt.Errorf("stage %s: unknown setting %s", id, k)
		}
	}
	return nil, nil
}
func ConfigureValidation(v map[string]string) ValidationConfig {
	p := v["python"]
	if p == "" {
		p = "python"
	}
	return ValidationConfig{Python: p}
}

type WriteFile struct {
	Workspace Workspace
	Config    ValidationConfig
	Check     bool
	Cache     string
}

func (s *WriteFile) Run(ctx context.Context, _ *RunContext, in Input[Candidate, Unit]) (Output[Artifacts, Diagnostic], error) {
	path, err := s.Workspace.Path(in.Result.Path)
	if err != nil {
		return Output[Artifacts, Diagnostic]{}, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return Output[Artifacts, Diagnostic]{}, err
	}
	if err = os.WriteFile(path, []byte(in.Result.Content), 0600); err != nil {
		return Output[Artifacts, Diagnostic]{}, err
	}
	if s.Check {
		d, invalid, err := CheckPython(ctx, s.Config.Python, path, s.Cache)
		if err != nil {
			return Output[Artifacts, Diagnostic]{}, err
		}
		if invalid {
			return Output[Artifacts, Diagnostic]{Status: Failure, Feedback: &Diagnostic{Message: d}}, nil
		}
	}
	return Output[Artifacts, Diagnostic]{Status: Success, Result: append(append(Artifacts(nil), in.Result.Previous...), Artifact{Path: in.Result.Path})}, nil
}

// Directory prevents traversal and symlink redirection outside the task output.
type Directory string

func (d Directory) Root() string { return string(d) }

func (d Directory) Path(name string) (string, error) {
	if !config.SafeRelative(name) {
		return "", fmt.Errorf("unsafe artifact path %q", name)
	}
	root := filepath.Clean(string(d))
	path := filepath.Join(root, name)
	rel, err := filepath.Rel(root, path)
	if err != nil || !config.SafeRelative(rel) {
		return "", fmt.Errorf("unsafe artifact path")
	}
	for cur := path; ; cur = filepath.Dir(cur) {
		info, err := os.Lstat(cur)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("artifact path contains a symlink")
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if cur == root {
			break
		}
	}
	return path, nil
}

type Commit struct {
	Committer Committer
	Message   string
}

func (s *Commit) Run(ctx context.Context, _ *RunContext, in Input[Artifacts, Unit]) (Output[Artifacts, Unit], error) {
	if s.Committer == nil {
		return Output[Artifacts, Unit]{}, fmt.Errorf("commit capability was not granted by pipeline")
	}
	paths := make([]string, len(in.Result))
	for i, a := range in.Result {
		paths[i] = a.Path
	}
	_, err := s.Committer.Commit(ctx, paths, s.Message)
	return Output[Artifacts, Unit]{Status: Success, Result: append(Artifacts(nil), in.Result...)}, err
}

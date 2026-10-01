package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"slop-generator/internal/config"
)

// ValidateAll checks configured repositories locally in deterministic name order.
func (c Client) ValidateAll(ctx context.Context, repositories map[string]config.Repository) error {
	for _, name := range slices.Sorted(maps.Keys(repositories)) {
		if err := c.Validate(ctx, repositories[name]); err != nil {
			fieldName := "repositories." + name
			hint := "Проверьте локальный репозиторий и настройки Git."
			var field *config.FieldError
			var path *config.PathError
			if errors.As(err, &field) {
				fieldName += "." + field.Field
				hint = field.Hint
			} else if errors.As(err, &path) {
				fieldName += ".path"
				hint = "Укажите путь к существующему Git-репозиторию."
			}
			return config.At(fieldName, fmt.Errorf("repository %q: %w", name, err), hint)
		}
	}
	return nil
}

// Validate checks repository root and local branch without contacting a remote.
// Git itself recognizes normal checkouts, worktrees and bare repositories.
func (Client) Validate(ctx context.Context, r config.Repository) error {
	if err := r.ValidatePath(); err != nil {
		return err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("Git executable not found")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	bare, err := run(ctx, r.Path, "rev-parse", "--is-bare-repository")
	if err != nil {
		return config.At("path", fmt.Errorf("path \"%s\" is not a readable Git repository", r.Path), "Укажите корень Git-репозитория; проверьте права доступа и доверие Git к владельцу каталога.")
	}
	rootCommand := "--show-toplevel"
	if bare == "true" {
		rootCommand = "--absolute-git-dir"
	}
	root, err := run(ctx, r.Path, "rev-parse", rootCommand)
	if err != nil {
		return fmt.Errorf("cannot determine Git repository root for %q", r.Path)
	}
	actual, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("cannot resolve Git repository root: %w", err)
	}
	selected, err := filepath.Abs(r.Path)
	if err != nil {
		return err
	}
	selected, err = filepath.EvalSymlinks(selected)
	if err != nil {
		return err
	}
	same := filepath.Clean(actual) == filepath.Clean(selected)
	if runtime.GOOS == "windows" {
		same = strings.EqualFold(filepath.Clean(actual), filepath.Clean(selected))
	}
	if !same {
		return config.At("path", fmt.Errorf("path \"%s\" is inside a Git repository; select its root \"%s\"", r.Path, root), "Используйте корневой каталог репозитория, а не вложенную папку.")
	}
	if _, err = run(ctx, r.Path, "check-ref-format", "refs/heads/"+r.Branch); err != nil {
		return config.At("branch", fmt.Errorf("invalid branch name %q", r.Branch), "Укажите корректное имя существующей локальной ветки.")
	}
	if _, err = branchCommit(ctx, r); err != nil {
		return err
	}
	return nil
}

// branchCommit distinguishes an unborn HEAD from a missing branch and command
// failures. A selected branch has no refs/heads entry until its first commit.
func branchCommit(ctx context.Context, r config.Repository) (string, error) {
	ref := "refs/heads/" + r.Branch
	sha, err := run(ctx, r.Path, "rev-parse", "--verify", ref+"^{commit}")
	if err == nil {
		return sha, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	_, refErr := run(ctx, r.Path, "show-ref", "--verify", "--quiet", ref)
	var exit *exec.ExitError
	if refErr != nil && errors.As(refErr, &exit) && exit.ExitCode() == 1 {
		head, headErr := run(ctx, r.Path, "symbolic-ref", "--quiet", "HEAD")
		if headErr == nil && head == ref {
			return "", config.At("branch", fmt.Errorf("Ветка %q выбрана, но в репозитории ещё нет первого коммита.", r.Branch), "Создайте первый коммит в этой ветке. Пайплайн использует его как основу изолированной рабочей копии.")
		}
		if headErr != nil {
			var headExit *exec.ExitError
			if !errors.As(headErr, &headExit) || headExit.ExitCode() != 1 {
				return "", config.At("branch", fmt.Errorf("Не удалось проверить состояние HEAD: %w", headErr), "Проверьте доступ к Git-репозиторию.")
			}
		}
		return "", config.At("branch", fmt.Errorf("local branch %q does not exist in \"%s\"", r.Branch, r.Path), "Укажите существующую локальную ветку в настройке branch.")
	}
	return "", config.At("branch", fmt.Errorf("Не удалось прочитать коммит ветки %q: %w", r.Branch, err), "Проверьте доступ к репозиторию и целостность Git-ссылок.")
}

package gitrepo

import (
	"context"
	"fmt"

	"slop-generator/internal/config"
)

// Pull updates the source checkout from the same destination used for publication.
// It never switches branches, resets files, rebases, or stashes local changes.
func (Client) Pull(ctx context.Context, r config.Repository) error {
	if err := r.ValidatePath(); err != nil {
		return err
	}
	branch, err := run(ctx, r.Path, "symbolic-ref", "--quiet", "HEAD")
	if err != nil || branch != "refs/heads/"+r.Branch {
		return fmt.Errorf("основной checkout должен находиться на ветке %q; переключите ветку и повторите pull", r.Branch)
	}
	bare, err := run(ctx, r.Path, "rev-parse", "--is-bare-repository")
	if err != nil {
		return err
	}
	if bare == "true" {
		return fmt.Errorf("pull требует рабочий checkout; для bare-репозитория установите pull_after_push: false")
	}
	remote, err := run(ctx, r.Path, "remote", "get-url", "--push", r.Remote)
	if err != nil {
		return fmt.Errorf("remote %q недоступен для pull", r.Remote)
	}
	_, err = run(ctx, r.Path, "-c", "merge.autostash=false", "-c", "rebase.autostash=false", "pull", "--ff-only", "--no-rebase", "--no-autostash", remote, "refs/heads/"+r.Branch)
	if err != nil {
		return fmt.Errorf("pull --ff-only не выполнен; проверьте доступ к remote, локальные изменения и расхождение веток: %w", err)
	}
	return nil
}

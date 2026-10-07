// Package gitrepo implements isolated repository preparation and publication using Git.
package gitrepo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"slop-generator/internal/config"
)

// Client performs isolated repository operations through the installed Git binary.
type Client struct{}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	b, e := cmd.CombinedOutput()
	if e != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		return "", fmt.Errorf("git %s failed: %w", args[0], e)
	}
	return strings.TrimSpace(string(b)), nil
}
func (Client) Identity(ctx context.Context, r config.Repository) (string, error) {
	if err := (Client{}).Validate(ctx, r); err != nil {
		return "", err
	}
	s, e := run(ctx, r.Path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if e != nil {
		return "", e
	}
	p, e := filepath.EvalSymlinks(s)
	if e != nil {
		return "", e
	}
	return strings.ToLower(filepath.Clean(p)), nil
}
func (Client) Prepare(ctx context.Context, r config.Repository, work string) (string, error) {
	if err := (Client{}).Validate(ctx, r); err != nil {
		return "", err
	}
	if _, e := run(ctx, r.Path, "check-ref-format", "refs/heads/"+r.Branch); e != nil {
		return "", fmt.Errorf("invalid target branch")
	}
	sha, e := branchCommit(ctx, r)
	if e != nil {
		return "", e
	}
	remote, e := run(ctx, r.Path, "remote", "get-url", "--push", r.Remote)
	if e != nil {
		return "", fmt.Errorf("configured remote does not exist")
	}
	// A full independent copy keeps user checkout and index untouched.
	if _, e = run(ctx, "", "clone", "--no-hardlinks", "--no-checkout", "--", r.Path, work); e != nil {
		return "", e
	}
	if _, e = run(ctx, work, "remote", "set-url", "origin", remote); e != nil {
		return "", e
	}
	if _, e = run(ctx, work, "fetch", "origin", "+refs/heads/"+r.Branch+":refs/remotes/origin/target"); e != nil {
		return "", fmt.Errorf("cannot fetch target remote branch")
	}
	if _, e = run(ctx, work, "merge-base", "--is-ancestor", "refs/remotes/origin/target", sha); e != nil {
		return "", fmt.Errorf("local branch is behind or diverges from remote; synchronize it before running")
	}
	if _, e = run(ctx, work, "checkout", "--detach", sha); e != nil {
		return "", e
	}
	for _, v := range []struct{ key, val string }{{"user.name", r.AuthorName}, {"user.email", r.AuthorEmail}} {
		s := v.val
		if s == "" {
			s, _ = run(ctx, r.Path, "config", "--get", v.key)
		}
		if s == "" {
			return "", fmt.Errorf("configure Git %s", v.key)
		}
		if _, e = run(ctx, work, "config", v.key, s); e != nil {
			return "", e
		}
	}
	return sha, nil
}
func published(ctx context.Context, work, branch, sha string) bool {
	if _, e := run(ctx, work, "fetch", "origin", "+refs/heads/"+branch+":refs/remotes/origin/target"); e != nil {
		return false
	}
	_, e := run(ctx, work, "merge-base", "--is-ancestor", sha, "refs/remotes/origin/target")
	return e == nil
}
func (Client) Publish(ctx context.Context, work, branch, sha string) error {
	if published(ctx, work, branch, sha) {
		return nil
	}
	_, e := run(ctx, work, "push", "origin", sha+":refs/heads/"+branch)
	if e == nil {
		return nil
	}
	// Cancellation or a broken connection may happen after the server accepted it.
	check, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if published(check, work, branch, sha) {
		return nil
	}
	return fmt.Errorf("push failed or could not be confirmed; working copy retained")
}

// Commit stages only the task output and records one commit in the private copy.
func (Client) Commit(ctx context.Context, work string, paths []string, message, hooksDir string) (string, error) {
	for _, path := range paths {
		if !config.SafeRelative(path) {
			return "", fmt.Errorf("unsafe commit path")
		}
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("commit paths required")
	}
	args := append([]string{"add", "--"}, paths...)
	if _, err := run(ctx, work, args...); err != nil {
		return "", err
	}
	diff, err := run(ctx, work, "diff", "--cached", "--name-only")
	if err != nil {
		return "", err
	}
	if diff == "" {
		return run(ctx, work, "rev-parse", "HEAD")
	}
	if _, err = run(ctx, work, "-c", "core.hooksPath="+hooksDir, "commit", "-m", message); err != nil {
		return "", err
	}
	return run(ctx, work, "rev-parse", "HEAD")
}

// CommitContext stages the generated paths, returns their first 20 diff lines,
// and reads up to 20 most recent commit subjects from the source repository reflog.
func (Client) CommitContext(ctx context.Context, source, work string, paths []string) (string, []string, error) {
	if source == "" || len(paths) == 0 {
		return "", nil, fmt.Errorf("source repository and commit paths are required")
	}
	for _, path := range paths {
		if !config.SafeRelative(path) {
			return "", nil, fmt.Errorf("unsafe commit path")
		}
	}
	args := append([]string{"add", "--"}, paths...)
	if _, err := run(ctx, work, args...); err != nil {
		return "", nil, err
	}
	diffArgs := append([]string{"diff", "--cached", "--no-ext-diff", "--"}, paths...)
	diff, err := run(ctx, work, diffArgs...)
	if err != nil {
		return "", nil, err
	}
	lines := strings.Split(diff, "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	reflog, err := run(ctx, source, "reflog", "--format=%gs")
	if err != nil {
		return "", nil, err
	}
	var messages []string
	for _, line := range strings.Split(reflog, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "commit") {
			continue
		}
		_, message, ok := strings.Cut(line, ": ")
		if ok && strings.TrimSpace(message) != "" {
			messages = append(messages, strings.TrimSpace(message))
			if len(messages) == 20 {
				break
			}
		}
	}
	return strings.Join(lines, "\n"), messages, nil
}

func (Client) Head(ctx context.Context, work string) (string, error) {
	return run(ctx, work, "rev-parse", "HEAD")
}
func (Client) Clean(ctx context.Context, work string) (bool, error) {
	s, err := run(ctx, work, "status", "--porcelain")
	return s == "", err
}

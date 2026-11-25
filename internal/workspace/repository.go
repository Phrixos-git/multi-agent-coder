package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ResolveRepositoryRoot finds the worktree containing an explicit directory.
func ResolveRepositoryRoot(ctx context.Context, directory string) (string, error) {
	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("an absolute directory path is required")
	}
	start, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("resolve directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", start, "rev-parse", "--show-toplevel")
	output, err := runGitCommand(cmd)
	if ctx.Err() != nil {
		return "", fmt.Errorf("resolve Git worktree: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("resolve Git worktree: %w", err)
	}
	root, err := filepath.EvalSymlinks(strings.TrimSuffix(output, "\n"))
	if err != nil {
		return "", fmt.Errorf("resolve Git root: %w", err)
	}
	if !filepath.IsAbs(root) || !withinRoot(root, start) {
		return "", fmt.Errorf("Git returned a worktree outside the supplied directory's hierarchy")
	}
	return root, nil
}

// GitWorktreeEnv returns the child environment without repository-scoped Git overrides.
func GitWorktreeEnv(cmd *exec.Cmd) []string {
	return slices.DeleteFunc(cmd.Environ(), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS",
			"GIT_CONFIG_COUNT", "GIT_OBJECT_DIRECTORY", "GIT_DIR", "GIT_WORK_TREE",
			"GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE", "GIT_INDEX_FILE",
			"GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE", "GIT_PREFIX",
			"GIT_SHALLOW_FILE", "GIT_COMMON_DIR", "GIT_NAMESPACE":
			return true
		}
		return false
	})
}

func runGitCommand(cmd *exec.Cmd) (string, error) {
	if cmd.Env == nil {
		cmd.Env = GitWorktreeEnv(cmd)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

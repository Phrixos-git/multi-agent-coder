package worktree

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Manager struct{ Repository string }

type Error struct {
	Kind string
	Err  error
}

func (e *Error) Error() string     { return e.Kind + ": " + e.Err.Error() }
func (e *Error) Unwrap() error     { return e.Err }
func (e *Error) ErrorKind() string { return e.Kind }
func failure(kind, format string, args ...any) error {
	return &Error{Kind: kind, Err: fmt.Errorf(format, args...)}
}

type Tree struct {
	Path, Branch string
	diffCaptured bool
}

type Changes struct {
	Patch string
	Files []string
}

func (m Manager) Dirty(ctx context.Context) (bool, error) {
	out, err := m.git(ctx, "status", "--porcelain")
	if err != nil {
		return false, failure("GitRepositoryError", "inspect working tree: %w: %s", err, out)
	}
	return len(out) != 0, err
}

func (m Manager) Create(ctx context.Context) (*Tree, error) {
	if _, err := m.git(ctx, "rev-parse", "--show-toplevel"); err != nil {
		return nil, failure("GitRepositoryError", "detect repository: %w", err)
	}
	base, err := os.MkdirTemp("", "multi-agent-coder-")
	if err != nil {
		return nil, failure("WorktreeCreateError", "create temp directory: %w", err)
	}
	path := filepath.Join(base, "worktree")
	id, err := randomID()
	if err != nil {
		_ = os.RemoveAll(base)
		return nil, failure("WorktreeCreateError", "generate task ID: %w", err)
	}
	branch := "mac/task-" + id
	if _, err := m.git(ctx, "worktree", "add", "-b", branch, path, "HEAD"); err != nil {
		_ = os.RemoveAll(base)
		return nil, failure("WorktreeCreateError", "git worktree add: %w", err)
	}
	return &Tree{Path: path, Branch: branch}, nil
}

func (m Manager) Diff(ctx context.Context, tree *Tree) (Changes, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", tree.Path, "add", "-N", "--all")
	if out, err := cmd.CombinedOutput(); err != nil {
		return Changes{}, failure("DiffCollectionError", "expose untracked files: %w: %s", err, out)
	}
	patch, err := command(ctx, "git", "-C", tree.Path, "diff", "--binary", "HEAD", "--")
	if err != nil {
		return Changes{}, failure("DiffCollectionError", "collect patch: %w", err)
	}
	names, err := command(ctx, "git", "-C", tree.Path, "diff", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return Changes{}, failure("DiffCollectionError", "collect changed files: %w", err)
	}
	files := parseNames(names)
	tree.diffCaptured = true
	return Changes{Patch: string(patch), Files: files}, nil
}

func (m Manager) Cleanup(ctx context.Context, tree *Tree) error {
	return m.cleanup(ctx, tree, false)
}

// ForceCleanup removes resources after diff collection has irrecoverably failed.
// Callers must preserve and report the collection error before using it.
func (m Manager) ForceCleanup(ctx context.Context, tree *Tree) error {
	return m.cleanup(ctx, tree, true)
}

func (m Manager) cleanup(ctx context.Context, tree *Tree, allowUncaptured bool) error {
	if tree == nil {
		return nil
	}
	if !tree.diffCaptured && !allowUncaptured {
		return failure("CleanupError", "refusing cleanup before diff collection")
	}
	var errs []error
	if out, err := m.git(ctx, "worktree", "remove", "--force", tree.Path); err != nil {
		errs = append(errs, fmt.Errorf("remove worktree: %w: %s", err, out))
	}
	if out, err := m.git(ctx, "branch", "-D", tree.Branch); err != nil {
		errs = append(errs, fmt.Errorf("delete temporary branch: %w: %s", err, out))
	}
	if err := os.RemoveAll(filepath.Dir(tree.Path)); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return failure("CleanupError", "%v", errs)
	}
	return nil
}

func (m Manager) git(ctx context.Context, args ...string) ([]byte, error) {
	return command(ctx, "git", append([]string{"-C", m.Repository}, args...)...)
}
func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	var b bytes.Buffer
	c := exec.CommandContext(ctx, name, args...)
	c.Stdout = &b
	c.Stderr = &b
	err := c.Run()
	return b.Bytes(), err
}
func randomID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func parseNames(data []byte) []string {
	parts := bytes.Split(data, []byte{0})
	set := map[string]bool{}
	for _, p := range parts {
		name := strings.TrimSpace(string(p))
		if name != "" {
			set[name] = true
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

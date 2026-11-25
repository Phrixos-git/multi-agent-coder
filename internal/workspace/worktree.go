package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree is an isolated checkout created from HEAD for one Pi task.
type Worktree struct{ root, path, branch, base, baseline, tempParent string }

// CreateWorktree creates a temporary branch and checkout from the repository HEAD.
func CreateWorktree(repositoryRoot, requestID string) (*Worktree, bool, error) {
	if repositoryRoot == "" {
		return nil, false, fmt.Errorf("WorktreeCreateError: repository root is required")
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, false, err
	}
	if _, err = gitOutput(root, "rev-parse", "--show-toplevel"); err != nil {
		return nil, false, fmt.Errorf("WorktreeCreateError: repository is unavailable: %w", err)
	}
	status, err := gitOutput(root, "status", "--porcelain")
	if err != nil {
		return nil, false, fmt.Errorf("WorktreeCreateError: inspect source status: %w", err)
	}
	base, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return nil, false, fmt.Errorf("WorktreeCreateError: resolve HEAD: %w", err)
	}
	base = strings.TrimSpace(base)
	tempParent, err := os.MkdirTemp("", "mac-pi-worktree-")
	if err != nil {
		return nil, false, fmt.Errorf("WorktreeCreateError: create temporary directory: %w", err)
	}
	dir := filepath.Join(tempParent, "checkout")
	branch := "mac/pi/" + safeBranchID(requestID)
	if _, err := gitOutput(root, "worktree", "add", "--detach", dir, "HEAD"); err != nil {
		_ = os.RemoveAll(tempParent)
		return nil, len(strings.TrimSpace(status)) != 0, fmt.Errorf("WorktreeCreateError: add worktree: %w", err)
	}
	if _, err := gitOutput(dir, "switch", "-c", branch); err != nil {
		_, _ = gitOutput(root, "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(tempParent)
		return nil, false, fmt.Errorf("WorktreeCreateError: create task branch: %w", err)
	}
	return &Worktree{root: root, path: dir, branch: branch, base: base, baseline: base, tempParent: tempParent}, len(strings.TrimSpace(status)) != 0, nil
}

// OverlayCurrent copies only requested main-worktree file states into this
// checkout, then records that exact state as the patch baseline.
func (w *Worktree) OverlayCurrent(paths []string) error {
	validator, err := NewValidator(w.root)
	if err != nil {
		return err
	}
	for _, path := range paths {
		source, err := validator.ValidateOverlay(path)
		if err != nil {
			return err
		}
		rel := filepath.Clean(path)
		destination := filepath.Join(w.path, rel)
		if err := rejectSymlinkParents(w.path, filepath.Dir(destination)); err != nil {
			return fmt.Errorf("WorktreeOverlayError: unsafe destination for %q: %w", path, err)
		}
		info, statErr := os.Stat(source)
		if os.IsNotExist(statErr) {
			if err := os.RemoveAll(destination); err != nil {
				return fmt.Errorf("WorktreeOverlayError: delete %q: %w", path, err)
			}
			continue
		}
		if statErr != nil {
			return fmt.Errorf("WorktreeOverlayError: inspect %q: %w", path, statErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("WorktreeOverlayError: %q is not a regular file", path)
		}
		content, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("WorktreeOverlayError: read %q: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return fmt.Errorf("WorktreeOverlayError: create parent for %q: %w", path, err)
		}
		if linkInfo, linkErr := os.Lstat(destination); linkErr == nil && linkInfo.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(destination); err != nil {
				return fmt.Errorf("WorktreeOverlayError: replace symlink %q: %w", path, err)
			}
		}
		if err := os.WriteFile(destination, content, info.Mode().Perm()); err != nil {
			return fmt.Errorf("WorktreeOverlayError: write %q: %w", path, err)
		}
		if err := os.Chmod(destination, info.Mode().Perm()); err != nil {
			return fmt.Errorf("WorktreeOverlayError: set mode for %q: %w", path, err)
		}
	}
	index := filepath.Join(w.tempParent, "baseline.index")
	if err := w.gitWithIndex(index, "read-tree", w.base); err != nil {
		return fmt.Errorf("WorktreeBaselineError: initialize index: %w", err)
	}
	if err := w.gitWithIndex(index, "add", "-A", "--force"); err != nil {
		return fmt.Errorf("WorktreeBaselineError: stage overlay: %w", err)
	}
	tree, err := w.gitWithIndexOutput(index, "write-tree")
	if err != nil {
		return fmt.Errorf("WorktreeBaselineError: snapshot overlay: %w", err)
	}
	w.baseline = strings.TrimSpace(tree)
	return nil
}

func rejectSymlinkParents(root, parent string) error {
	rel, err := filepath.Rel(root, parent)
	if err != nil || escapesRoot(rel) {
		return fmt.Errorf("destination escapes worktree")
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink parent is denied")
		}
	}
	return nil
}

func safeBranchID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "task"
	}
	return b.String()
}

func (w *Worktree) Path() string   { return w.path }
func (w *Worktree) Branch() string { return w.branch }

// Collect returns HEAD-relative binary patch and changed paths, including new files.
func (w *Worktree) Collect() (string, []string, error) {
	if _, err := gitOutput(w.path, "add", "-A", "--force"); err != nil {
		return "", nil, fmt.Errorf("WorktreePatchError: stage temporary worktree changes: %w", err)
	}
	pathsRaw, err := gitOutput(w.path, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACDMRT", w.baseline)
	if err != nil {
		return "", nil, fmt.Errorf("WorktreePatchError: collect changed paths: %w", err)
	}
	paths := make([]string, 0)
	var patch strings.Builder
	for _, p := range strings.Split(pathsRaw, "\x00") {
		if p == "" || secretComponent(p) {
			continue
		}
		paths = append(paths, filepath.ToSlash(p))
		filePatch, err := gitOutput(w.path, "diff", "--cached", "--binary", "--no-ext-diff", w.baseline, "--", p)
		if err != nil {
			return "", nil, fmt.Errorf("WorktreePatchError: collect patch for %q: %w", p, err)
		}
		patch.WriteString(filePatch)
	}
	return strings.TrimSuffix(patch.String(), "\n"), paths, nil
}

func (w *Worktree) gitWithIndex(index string, args ...string) error {
	_, err := w.gitWithIndexOutput(index, args...)
	return err
}
func (w *Worktree) gitWithIndexOutput(index string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = w.path
	cmd.Env = append(GitWorktreeEnv(cmd), "GIT_INDEX_FILE="+index)
	return runGitCommand(cmd)
}

// Cleanup unregisters the worktree and removes its temporary branch.
func (w *Worktree) Cleanup() error {
	_, removeErr := gitOutput(w.root, "worktree", "remove", "--force", w.path)
	_, branchErr := gitOutput(w.root, "branch", "-D", w.branch)
	parentErr := os.RemoveAll(w.tempParent)
	if removeErr != nil || branchErr != nil || parentErr != nil {
		return fmt.Errorf("WorktreeCleanupError: remove worktree: %v; remove branch: %v; remove temporary parent: %v", removeErr, branchErr, parentErr)
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return runGitCommand(cmd)
}

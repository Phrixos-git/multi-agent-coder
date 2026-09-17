package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeUsesHEADCollectsAllChangesAndCleansUp(t *testing.T) {
	repo := newRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("dirty source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := Manager{Repository: repo}
	dirty, err := m.Dirty(context.Background())
	if err != nil || !dirty {
		t.Fatalf("Dirty() = %v, %v", dirty, err)
	}
	tree, err := m.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(tree.Path, "tracked.txt")); got != "head\n" {
		t.Fatalf("worktree content = %q", got)
	}
	if err := os.WriteFile(filepath.Join(tree.Path, "tracked.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree.Path, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tree.Path, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	changes, err := m.Diff(context.Background(), tree)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tracked.txt", "new.txt", "delete.txt"} {
		if !containsFile(changes.Files, name) {
			t.Errorf("missing %s: %v", name, changes.Files)
		}
	}
	if !strings.Contains(changes.Patch, "new.txt") || !strings.Contains(changes.Patch, "deleted file mode") {
		t.Fatalf("patch missing changes:\n%s", changes.Patch)
	}
	path, branch := tree.Path, tree.Branch
	if err := m.Cleanup(context.Background(), tree); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree remains: %v", err)
	}
	cmd := exec.Command("git", "-C", repo, "branch", "--list", branch)
	out, err := cmd.Output()
	if err != nil || len(out) != 0 {
		t.Fatalf("branch remains: %q %v", out, err)
	}
}

func TestCleanupRefusesBeforeDiff(t *testing.T) {
	repo := newRepo(t)
	m := Manager{Repository: repo}
	tree, err := m.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cleanup(context.Background(), tree); err == nil {
		t.Fatal("Cleanup() error=nil")
	}
	if _, err := m.Diff(context.Background(), tree); err != nil {
		t.Fatal(err)
	}
	if err := m.Cleanup(context.Background(), tree); err != nil {
		t.Fatal(err)
	}
}
func newRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	run(t, d, "init")
	run(t, d, "config", "user.email", "test@example.invalid")
	run(t, d, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(d, "tracked.txt"), []byte("head\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "delete.txt"), []byte("delete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, d, "add", ".")
	run(t, d, "commit", "-m", "initial")
	return d
}
func run(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func containsFile(files []string, want string) bool {
	for _, f := range files {
		if f == want {
			return true
		}
	}
	return false
}

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRepositoryRootUsesLinkedWorktreeAndIgnoresGitOverrides(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	linked := filepath.Join(t.TempDir(), "linked checkout")
	runGit(t, root, "worktree", "add", "--detach", linked, "HEAD")
	nested := filepath.Join(linked, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "foreign-index"))
	got, err := ResolveRepositoryRoot(context.Background(), nested)
	want, pathErr := filepath.EvalSymlinks(linked)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if err != nil || got != want {
		t.Fatalf("ResolveRepositoryRoot() = %q, %v, want %q", got, err, want)
	}
	worktree, _, err := CreateWorktree(got, "repository-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.Cleanup(); err != nil {
		t.Fatal(err)
	}
}

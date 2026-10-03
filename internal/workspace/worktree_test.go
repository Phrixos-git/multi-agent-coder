package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeStartsAtHEADAndCollectsChanges(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("dirty main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	wt, dirty, err := CreateWorktree(root, "request-id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.Cleanup(); err != nil {
			t.Errorf("Cleanup(): %v", err)
		}
	}()
	if !dirty {
		t.Fatal("dirty source warning missing")
	}
	data, err := os.ReadFile(filepath.Join(wt.Path(), "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "base\n" {
		t.Fatalf("worktree content = %q, want HEAD content", data)
	}
	if !strings.HasPrefix(wt.Branch(), "mac/pi/") {
		t.Fatalf("branch = %q", wt.Branch())
	}
	if err := os.WriteFile(filepath.Join(wt.Path(), "tracked.txt"), []byte("updated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path(), "new.txt"), []byte("new file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path(), ".env.secret"), []byte("SECRET SHOULD NOT LEAK"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path(), ".env.example"), []byte("EXAMPLE=value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path(), ".env.example.local"), []byte("LOCAL_EXAMPLE=value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	patch, files, err := wt.Collect()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "updated") || !strings.Contains(patch, "new file") || !strings.Contains(patch, "+EXAMPLE=value") || !strings.Contains(patch, "+LOCAL_EXAMPLE=value") || strings.Contains(patch, "SECRET SHOULD NOT LEAK") {
		t.Fatalf("unexpected patch: %q", patch)
	}
	if strings.Join(files, ",") != ".env.example,.env.example.local,new.txt,tracked.txt" {
		t.Fatalf("changed files = %#v", files)
	}
}

func TestWorktreeCollectsDeletionAndRename(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "Test")
	for name, content := range map[string]string{"remove.txt": "remove\n", "old.txt": "rename\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "initial")
	wt, _, err := CreateWorktree(root, "delete-rename")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.Cleanup(); err != nil {
			t.Errorf("Cleanup(): %v", err)
		}
	}()
	if err := os.Remove(filepath.Join(wt.Path(), "remove.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(wt.Path(), "old.txt"), filepath.Join(wt.Path(), "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	patch, files, err := wt.Collect()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "deleted file mode") || !strings.Contains(patch, "renamed.txt") {
		t.Fatalf("patch missing delete/rename: %s", patch)
	}
	if len(files) == 0 {
		t.Fatal("changed files empty")
	}
}

func TestFixerOverlayAndPatchUseOverlayBaseline(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "Test")
	for name, content := range map[string]string{"foo.go": "A\n", "old.go": "old\n", "unrelated.go": "base\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(root, "foo.go"), []byte("B\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("B new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "old.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated.go"), []byte("dirty unrelated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	wt, _, err := CreateWorktree(root, "fixer-overlay")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.Cleanup(); err != nil {
			t.Errorf("Cleanup(): %v", err)
		}
	}()
	if err := wt.OverlayCurrent([]string{"foo.go", "new.go", "old.go"}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"foo.go": "B\n", "new.go": "B new\n", "unrelated.go": "base\n"} {
		got, err := os.ReadFile(filepath.Join(wt.Path(), path))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(wt.Path(), "old.go")); !os.IsNotExist(err) {
		t.Fatalf("deleted target exists: err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path(), "foo.go"), []byte("C\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path(), "new.go"), []byte("C new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	patch, files, err := wt.Collect()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(patch, "A\n") || !strings.Contains(patch, "-B") || !strings.Contains(patch, "+C") || strings.Contains(patch, "dirty unrelated") {
		t.Fatalf("patch is not baseline-relative: %s", patch)
	}
	if strings.Join(files, ",") != "foo.go,new.go" {
		t.Fatalf("changed files = %#v", files)
	}
}

func TestFixerOverlayAcceptsDeletedTargetsAndRejectsSecretsAndEscapes(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "base"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "initial")
	wt, _, err := CreateWorktree(root, "fixer-paths")
	if err != nil {
		t.Fatal(err)
	}
	defer wt.Cleanup()
	if err := wt.OverlayCurrent([]string{"deleted-new.txt"}); err != nil {
		t.Fatalf("missing path should represent deletion/new absence: %v", err)
	}
	for _, path := range []string{".env", "../outside", filepath.Join("..", filepath.Base(root)+"-escape", "id")} {
		if err := wt.OverlayCurrent([]string{path}); err == nil {
			t.Errorf("OverlayCurrent(%q) accepted", path)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

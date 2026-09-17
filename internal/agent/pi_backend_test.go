package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arietis/multi-agent-coder/internal/pirpc"
)

func TestPiBackendEditsOnlyTemporaryWorktree(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.invalid")
	gitRun(t, repo, "config", "user.name", "Test")
	source := filepath.Join(repo, "main.go")
	if err := os.WriteFile(source, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "initial")
	script := filepath.Join(t.TempDir(), "fake-pi")
	body := "#!/bin/sh\nset -eu\nread first\nprintf 'package main\\n// changed\\n' > main.go\nprintf 'new\\n' > added.txt\necho '{\"id\":\"task\",\"type\":\"response\",\"command\":\"prompt\",\"success\":true}'\necho '{\"type\":\"agent_settled\"}'\nread second\necho '{\"id\":\"summary\",\"type\":\"response\",\"command\":\"get_last_assistant_text\",\"success\":true,\"data\":{\"text\":\"implemented\"}}'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	b := NewPiBackend(repo, pirpc.Config{Executable: script}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	raw, err := b.Run(context.Background(), PromptInput{Role: RoleCoder, Task: "change it"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "added.txt") || !strings.Contains(raw, "backend\":\"pi") {
		t.Fatalf("result=%s", raw)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package main\n" {
		t.Fatalf("source tree changed: %q", data)
	}
	out, err := exec.Command("git", "-C", repo, "branch", "--list", "mac/task-*").Output()
	if err != nil || len(out) != 0 {
		t.Fatalf("temporary branch remains: %q %v", out, err)
	}
}

func TestPiBackendIntegration(t *testing.T) {
	if os.Getenv("MAC_PI_E2E") != "1" {
		t.Skip("set MAC_PI_E2E=1 to run the external Pi/LLM integration test")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.invalid")
	gitRun(t, repo, "config", "user.name", "Test")
	files := map[string]string{"go.mod": "module fixture\n\ngo 1.24.0\n", "math.go": "package fixture\n\nfunc Add(a, b int) int { return a - b }\n", "math_test.go": "package fixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"wrong sum\") } }\n"}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "fixture")
	provider := os.Getenv("MAC_PI_PROVIDER")
	model := os.Getenv("MAC_PI_MODEL")
	b := NewPiBackend(repo, pirpc.Config{Executable: "pi", Provider: provider, Model: model}, 5*time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	raw, err := b.Run(context.Background(), PromptInput{Role: RoleCoder, Task: "Fix the Add function so go test ./... passes. Run the test and report the result."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "math.go") || !strings.Contains(raw, "return a + b") {
		t.Fatalf("Pi result did not contain expected patch: %s", raw)
	}
	if got, err := os.ReadFile(filepath.Join(repo, "math.go")); err != nil || strings.Contains(string(got), "a + b") {
		t.Fatalf("source working tree was modified: %q %v", got, err)
	}
}

func TestPiBackendReturnsPartialDiffOnProcessFailure(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.invalid")
	gitRun(t, repo, "config", "user.name", "Test")
	source := filepath.Join(repo, "main.go")
	if err := os.WriteFile(source, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "initial")
	script := filepath.Join(t.TempDir(), "failing-pi")
	body := "#!/bin/sh\nset -eu\nread first\nprintf 'package main\\n// partial\\n' > main.go\nexit 7\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	b := NewPiBackend(repo, pirpc.Config{Executable: script}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	raw, err := b.Run(context.Background(), PromptInput{Role: RoleCoder, Task: "change"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"status":"error"`) || !strings.Contains(raw, "// partial") || !strings.Contains(raw, "PiAbnormalExit") {
		t.Fatalf("result=%s", raw)
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != "package main\n" {
		t.Fatalf("source changed: %q %v", got, err)
	}
}

func gitRun(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

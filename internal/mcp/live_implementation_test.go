package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arietis/multi-agent-coder/internal/config"
)

// The seeded fixer implementation validates and copies input, but only merges
// overlaps. Adjacent intervals remain separate, violating the contract.
const seededIntervals = `package intervals

import (
    "errors"
    "sort"
)

type Interval struct { Start, End int }
var ErrInvalidInterval = errors.New("invalid interval")

func Normalize(input []Interval) ([]Interval, error) {
    for _, v := range input {
        if v.Start > v.End { return nil, ErrInvalidInterval }
    }
    values := append([]Interval(nil), input...)
    sort.Slice(values, func(i, j int) bool { return values[i].Start < values[j].Start })
    var result []Interval
    for _, v := range values {
        if len(result) == 0 || v.Start > result[len(result)-1].End {
            result = append(result, v)
        } else if v.End > result[len(result)-1].End {
            result[len(result)-1].End = v.End
        }
    }
    return result, nil
}
`

// TestImplementationChallengeFixtures ensures the negative fixtures fail on
// behavior rather than compilation or infrastructure errors. No model is used.
func TestImplementationChallengeFixtures(t *testing.T) {
	for _, role := range []string{"coder", "fixer"} {
		t.Run(role, func(t *testing.T) {
			repo := implementationFixture(t)
			if role == "fixer" {
				writeImplementationSource(t, repo, seededIntervals)
			}
			output, err := implementationGoTest(repo)
			if err == nil || !strings.Contains(output, "--- FAIL: TestNormalizeContract") || strings.Contains(output, "[build failed]") {
				t.Fatalf("fixture must fail its behavioral tests: %v\n%s", err, output)
			}
			if role == "fixer" && !strings.Contains(output, "TestNormalizeContract/adjacent") {
				t.Fatalf("seeded defect did not fail adjacency contract:\n%s", output)
			}
		})
	}
}

// TestLiveImplementationE2E uses current source, isolated repositories and real
// Pi agents. Unlike role-routing smoke tests, success requires applying and
// executing the returned implementation against immutable acceptance tests.
func TestLiveImplementationE2E(t *testing.T) {
	if os.Getenv("MAC_LIVE_IMPLEMENTATION_E2E") != "1" {
		t.Skip("set MAC_LIVE_IMPLEMENTATION_E2E=1 to use live Pi implementation tasks")
	}
	base, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if base.CoderBackend != "pi" || base.FixerBackend != "pi" {
		t.Fatal("implementation challenge requires MAC_CODER_BACKEND=pi and MAC_FIXER_BACKEND=pi")
	}
	for _, role := range []string{"coder", "fixer"} {
		t.Run(role, func(t *testing.T) {
			repo := implementationFixture(t)
			for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-m", "interval challenge contract and tests"}} {
				liveGit(t, repo, args...)
			}
			if role == "fixer" {
				// Only this dirty file is overlaid into the fixer's worktree.
				writeImplementationSource(t, repo, seededIntervals)
			}
			before, err := os.ReadFile(filepath.Join(repo, "intervals.go"))
			if err != nil {
				t.Fatal(err)
			}
			output, testErr := implementationGoTest(repo)
			if testErr == nil || !strings.Contains(output, "--- FAIL: TestNormalizeContract") {
				t.Fatalf("challenge must start with failing contract tests: %v\n%s", testErr, output)
			}
			cfg := base
			cfg.RepositoryRoot = repo
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			task := "Implement Normalize in intervals.go according to CONTRACT.md. Acceptance tests already exist; do not modify them. Run go test ./... before and after the change. Change only intervals.go."
			if role == "fixer" {
				task = "Repair Normalize in intervals.go: adjacent intervals are not merged. Follow CONTRACT.md, preserve correct validation and input immutability, and handle extreme machine integers. Run the failing tests before repair and go test ./... afterward. Change only intervals.go."
			}
			result := liveCallTool(t, ctx, cfg, "local_agent_run", map[string]any{
				"role": role, "task": task,
				"files": []string{"intervals.go", "CONTRACT.md", "intervals_test.go", "go.mod"},
			})
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var got RunOutput
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != "success" || got.Role != role || got.Backend != "pi" || got.PiExitStatus != "0" || got.Error != nil {
				t.Fatalf("live %s failed: %s", role, encoded)
			}
			if len(got.ChangedFiles) != 1 || got.ChangedFiles[0] != "intervals.go" || strings.TrimSpace(got.Patch) == "" {
				t.Fatalf("expected only intervals.go patch; files=%v patch=%q", got.ChangedFiles, got.Patch)
			}
			after, err := os.ReadFile(filepath.Join(repo, "intervals.go"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("Pi changed the source repository: %v", err)
			}
			patch := got.Patch
			if !strings.HasSuffix(patch, "\n") {
				t.Log("raw patch lacks final LF; appending LF for implementation verification (raw patch applicability is not proven)")
				patch += "\n"
			}
			for _, args := range [][]string{{"apply", "--check", "-"}, {"apply", "-"}} {
				cmd := exec.CommandContext(ctx, "git", args...)
				cmd.Dir, cmd.Stdin = repo, strings.NewReader(patch)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1", "--untracked-files=all")
			statusCmd.Dir = repo
			status, err := statusCmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(status)) != "M intervals.go" {
				t.Fatalf("unexpected applied changes: %v\n%s", err, status)
			}
			// A patch can edit paths not declared by ChangedFiles. Re-read the
			// immutable inputs before trusting the test run.
			for _, name := range []string{"CONTRACT.md", "intervals_test.go", "go.mod"} {
				want, err := os.ReadFile(filepath.Join("testdata", "intervals", name))
				if err != nil {
					t.Fatal(err)
				}
				actual, err := os.ReadFile(filepath.Join(repo, name))
				if err != nil || !bytes.Equal(actual, want) {
					t.Fatalf("acceptance input %s was modified: %v", name, err)
				}
			}
			output, testErr = implementationGoTest(repo)
			if testErr != nil {
				t.Fatalf("%s implementation failed acceptance tests: %v\n%s", role, testErr, output)
			}
			t.Logf("%s: Pi model=%s, tools=%d, test commands=%d; acceptance passed\n%s", role, got.PiModel, got.ToolCallCount, got.TestCommandCount, output)
		})
	}
}

func implementationFixture(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, name := range []string{"go.mod", "CONTRACT.md", "intervals.go", "intervals_test.go"} {
		data, err := os.ReadFile(filepath.Join("testdata", "intervals", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func writeImplementationSource(t *testing.T, repo, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "intervals.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func implementationGoTest(repo string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-v", "-count=1", "-timeout=30s", "./...")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

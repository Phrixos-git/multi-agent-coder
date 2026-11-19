package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arietis/multi-agent-coder/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestLiveRoleRouting exercises the MCP tools against running local models.
// Run explicitly with MAC_LIVE_ROLE_E2E=1 and both local LLM servers available.
func TestLiveRoleRouting(t *testing.T) {
	if os.Getenv("MAC_LIVE_ROLE_E2E") != "1" {
		t.Skip("set MAC_LIVE_ROLE_E2E=1 to use live local models")
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Test"}} {
		liveGit(t, repo, args...)
	}
	if err := os.WriteFile(filepath.Join(repo, "source.go"), []byte("package sample\n\nfunc Add(a, b int) int { return a + b }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "broken.go"), []byte("package sample\n\nfunc Subtract(a, b int) int { return a + b }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	liveGit(t, repo, "add", "source.go")
	liveGit(t, repo, "add", "broken.go")
	liveGit(t, repo, "commit", "-m", "fixture")
	base, err := config.Load()
	if err != nil {
		t.Fatalf("load live E2E configuration: %v", err)
	}
	base.RepositoryRoot = repo
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	base.MainLLM.Model = liveModelID(t, ctx, base.MainLLM)
	base.CodingLLM.Model = liveModelID(t, ctx, base.CodingLLM)
	health := liveCallTool(t, ctx, base, "local_agent_health", nil)
	if health["status"] != "ok" || health["main_llm"].(map[string]any)["status"] != "available" || health["coding_llm"].(map[string]any)["status"] != "available" {
		t.Fatalf("live health = %#v", health)
	}
	t.Logf("health: main=%v coding=%v", health["main_llm"].(map[string]any)["status"], health["coding_llm"].(map[string]any)["status"])

	for _, tc := range []struct{ role, task string }{
		{"coder", "Correct broken.go so Subtract returns a - b. Return one JSON object with status success, role coder, a short summary, and an artifact containing the complete corrected broken.go. Do not use Markdown outside JSON."},
		{"reviewer", "Review source.go. It correctly adds two integers. Return a concise JSON review with status success, role reviewer, and no findings."},
		{"tester", "Suggest one concrete unit test for Add in source.go. Put the suggestion in exactly one notes entry. Return JSON with status success and role tester; do not edit files."},
		{"fixer", "Fix broken.go: Subtract currently adds, but it must return a - b. Return one JSON object with status success, role fixer, a short summary, and an artifact containing the complete corrected broken.go. Do not use Markdown outside JSON."},
	} {
		t.Run(tc.role, func(t *testing.T) {
			cfg := base
			if tc.role == "coder" || tc.role == "fixer" {
				cfg.MainLLM.BaseURL = "http://127.0.0.1:1"
			} else {
				cfg.CodingLLM.BaseURL = "http://127.0.0.1:1"
			}
			file := "source.go"
			if tc.role == "coder" || tc.role == "fixer" {
				file = "broken.go"
			}
			out := liveCallTool(t, ctx, cfg, "local_agent_run", map[string]any{"role": tc.role, "task": tc.task, "files": []string{file}})
			if out["status"] != "success" || out["role"] != tc.role || out["backend"] != "direct" {
				t.Fatalf("live %s output = %#v", tc.role, out)
			}
			if tc.role == "coder" || tc.role == "fixer" {
				artifacts := out["artifacts"].([]any)
				if len(artifacts) != 1 || artifacts[0].(map[string]any)["path"] != "broken.go" || !contains(artifacts[0].(map[string]any)["content"].(string), "return a - b") {
					t.Fatalf("%s artifact = %#v", tc.role, artifacts)
				}
			} else if tc.role == "reviewer" {
				if findings := out["findings"].([]any); len(findings) != 0 {
					t.Fatalf("reviewer findings = %#v", findings)
				}
			} else if tc.role == "tester" {
				notes := out["notes"].([]any)
				if len(notes) != 1 || strings.TrimSpace(notes[0].(string)) == "" {
					t.Fatalf("tester notes = %#v", notes)
				}
			}
			t.Logf("%s: success, backend=%v, summary=%v", tc.role, out["backend"], out["summary"])
		})
	}
	contents, err := os.ReadFile(filepath.Join(repo, "broken.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(contents), "return a + b") {
		t.Fatalf("Direct agents modified the source repository: %s", contents)
	}
}

func liveModelID(t *testing.T, ctx context.Context, cfg config.LLMConfig) string {
	t.Helper()
	if model := strings.TrimSpace(cfg.Model); model != "" {
		return model
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.BaseURL, "/")+"/v1/models", nil)
	if err != nil {
		t.Fatalf("create model discovery request: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("discover Main LLM model from %s/v1/models: %v", strings.TrimRight(cfg.BaseURL, "/"), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("discover Main LLM model: /v1/models returned %s", resp.Status)
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		t.Fatalf("decode Main LLM /v1/models response: %v", err)
	}
	if len(models.Data) == 0 || strings.TrimSpace(models.Data[0].ID) == "" {
		t.Fatalf("Main LLM /v1/models at %s returned no model IDs", strings.TrimRight(cfg.BaseURL, "/"))
	}
	return models.Data[0].ID
}

func TestLiveModelIDUsesConfiguredModel(t *testing.T) {
	got := liveModelID(t, context.Background(), config.LLMConfig{Model: "configured-model"})
	if got != "configured-model" {
		t.Fatalf("liveModelID() = %q, want configured model", got)
	}
}

func TestLiveModelIDDiscoversModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"discovered-model"}]}`))
	}))
	defer server.Close()

	got := liveModelID(t, context.Background(), config.LLMConfig{BaseURL: server.URL})
	if got != "discovered-model" {
		t.Fatalf("liveModelID() = %q, want discovered model", got)
	}
}

func liveCallTool(t *testing.T, ctx context.Context, cfg config.Config, name string, args map[string]any) map[string]any {
	t.Helper()
	server, err := NewServer(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "live-routing-e2e", Version: "1"}, nil)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = server.SDKServer().Run(serverCtx, serverTransport) }()
	session, err := client.Connect(serverCtx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(serverCtx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	out, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content = %T", result.StructuredContent)
	}
	return out
}

func liveGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

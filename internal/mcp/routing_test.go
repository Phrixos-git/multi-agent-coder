package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arietis/multi-agent-coder/internal/config"
	"github.com/arietis/multi-agent-coder/internal/llm"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLocalAgentRunRoutesDirectRoles(t *testing.T) {
	for _, tc := range []struct {
		role, target, model string
		maxTokens           int
	}{
		{"fixer", "coding", "coding-test", 4096},
		{"reviewer", "main", "main-test", 4096},
		{"tester", "main", "main-test", 3072},
	} {
		t.Run(tc.role, func(t *testing.T) {
			var mainCalls, codingCalls atomic.Int32
			mock := func(name string, calls *atomic.Int32) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var request llm.ChatCompletionRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if name != tc.target || request.Model != tc.model || request.MaxTokens != tc.maxTokens {
						t.Errorf("%s received role %s request: %+v", name, tc.role, request)
					}
					if tc.role == "fixer" {
						if request.ResponseFormat.Type != "json_schema" || request.ResponseFormat.JSONSchema == nil {
							t.Errorf("fixer response format = %+v", request.ResponseFormat)
						}
					} else if request.ResponseFormat.Type != "json_object" {
						t.Errorf("%s response format = %+v", tc.role, request.ResponseFormat)
					}
					response := llm.ChatCompletionResponse{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: `{"status":"success","role":"` + tc.role + `","summary":"ok","artifacts":[],"findings":[],"risks":[],"notes":[]}`}, FinishReason: "stop"}}}
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Error(err)
					}
				}))
			}
			main := mock("main", &mainCalls)
			defer main.Close()
			coding := mock("coding", &codingCalls)
			defer coding.Close()
			cfg := config.Config{
				MainLLM:      config.LLMConfig{BaseURL: main.URL, Model: "main-test", Timeout: time.Second},
				CodingLLM:    config.LLMConfig{BaseURL: coding.URL, Model: "coding-test", Timeout: 2 * time.Second},
				LLMMaxTokens: 1024, ReviewerMaxTokens: 4096, TesterMaxTokens: 3072, FixerMaxTokens: 4096,
				RepositoryRoot: t.TempDir(),
			}
			out := runMCPRole(t, cfg, tc.role)
			if out["status"] != "success" {
				t.Fatalf("output = %#v", out)
			}
			wantMain, wantCoding := int32(1), int32(0)
			if tc.target == "coding" {
				wantMain, wantCoding = 0, 1
			}
			if mainCalls.Load() != wantMain || codingCalls.Load() != wantCoding {
				t.Fatalf("main calls=%d coding calls=%d", mainCalls.Load(), codingCalls.Load())
			}
		})
	}
}

func TestLocalAgentRunCoderUsesPiWithoutDirectRequests(t *testing.T) {
	var mainCalls, codingCalls atomic.Int32
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { mainCalls.Add(1) }))
	defer main.Close()
	coding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { codingCalls.Add(1) }))
	defer coding.Close()
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Test"}} {
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "source.go"), []byte("package sample\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "source.go"}, {"commit", "-m", "fixture"}} {
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	cfg := config.Config{
		MainLLM:        config.LLMConfig{BaseURL: main.URL, Timeout: time.Second},
		CodingLLM:      config.LLMConfig{BaseURL: coding.URL, Timeout: time.Second},
		RepositoryRoot: repo, CoderBackend: "pi", PiExecutable: filepath.Join(repo, "missing-pi"), PiTimeout: time.Second,
	}
	out := runMCPRole(t, cfg, "coder")
	if out["backend"] != "pi" || out["status"] != "error" || mainCalls.Load() != 0 || codingCalls.Load() != 0 {
		t.Fatalf("output=%#v main=%d coding=%d", out, mainCalls.Load(), codingCalls.Load())
	}
}

func runMCPRole(t *testing.T, cfg config.Config, role string) map[string]any {
	t.Helper()
	server, err := NewServer(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "routing-test", Version: "1"}, nil)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.SDKServer().Run(ctx, serverTransport) }()
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "local_agent_run", Arguments: map[string]any{"role": role, "task": "test routing"}})
	if err != nil {
		t.Fatal(err)
	}
	return result.StructuredContent.(map[string]any)
}

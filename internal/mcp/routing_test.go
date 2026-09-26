package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arietis/multi-agent-coder/internal/agent"
	"github.com/arietis/multi-agent-coder/internal/config"
	"github.com/arietis/multi-agent-coder/internal/llm"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLocalAgentRunRoutesAllRolesDirectly(t *testing.T) {
	for _, tc := range []struct {
		role, target, model string
		maxTokens           int
		timeout             time.Duration
	}{
		{"coder", "coding", "coding-test", 4096, 2 * time.Second},
		{"fixer", "coding", "coding-test", 4096, 2 * time.Second},
		{"reviewer", "main", "main-test", 4096, time.Second},
		{"tester", "main", "main-test", 3072, time.Second},
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
					if name != tc.target || request.Model != tc.model || request.MaxTokens != tc.maxTokens || request.ReasoningEffort != "none" || request.Temperature != 0.2 {
						t.Errorf("%s received role %s request: %+v", name, tc.role, request)
					}
					if tc.role == "coder" || tc.role == "fixer" {
						if request.ResponseFormat.Type != "json_schema" || request.ResponseFormat.JSONSchema == nil {
							t.Errorf("%s response format = %+v", tc.role, request.ResponseFormat)
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
				LLMMaxTokens: 1024, CoderMaxTokens: 4096, ReviewerMaxTokens: 4096, TesterMaxTokens: 3072, FixerMaxTokens: 4096,
				LLMReasoningEffort: "none", LLMTemperature: 0.2, RepositoryRoot: t.TempDir(),
			}
			resolved, resolvedMaxTokens := directConfigForRole(cfg, agent.Role(tc.role))
			if resolved.Model != tc.model || resolved.Timeout != tc.timeout || resolvedMaxTokens != tc.maxTokens {
				t.Fatalf("resolved config = %+v max_tokens=%d", resolved, resolvedMaxTokens)
			}
			out := runMCPRole(t, cfg, tc.role)
			if out["status"] != "success" || out["backend"] != "direct" {
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

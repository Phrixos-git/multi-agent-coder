package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arietis/multi-agent-coder/internal/agent"
	"github.com/arietis/multi-agent-coder/internal/config"
	"github.com/arietis/multi-agent-coder/internal/llm"
	"github.com/arietis/multi-agent-coder/internal/workspace"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLocalAgentRunConnectsMCPToLLM(t *testing.T) {
	root, _ := makeWorkspace(t, "run")
	if err := os.WriteFile(filepath.Join(root, "input.go"), []byte("package sample\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	completer := &recordingCompleter{}
	server, err := newServer(context.Background(), config.Config{}, discardLogger(), completer, root)
	if err != nil {
		t.Fatal(err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "1"}, nil)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.SDKServer().Run(ctx, serverTransport) }()
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "local_agent_run", Arguments: map[string]any{
		"role": "coder", "task": "make a candidate", "files": []string{"input.go"},
	}})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	output := result.StructuredContent.(map[string]any)
	if output["status"] != "success" || output["role"] != "coder" || output["summary"] != "candidate prepared" {
		t.Fatalf("output = %#v", output)
	}
	if len(completer.messages) != 2 || !contains(completer.messages[1].Content, "package sample") {
		t.Fatalf("file content was not supplied to LLM: %#v", completer.messages)
	}
	if string(contentMust(t, filepath.Join(root, "input.go"))) != "package sample\n" {
		t.Fatal("local_agent_run modified the repository file")
	}
}

func TestExecuteRunSupportsAllRoles(t *testing.T) {
	for _, role := range []string{"coder", "reviewer", "tester", "fixer"} {
		t.Run(role, func(t *testing.T) {
			runner := staticRunner{response: `{"status":"success","role":"` + role + `","summary":"ok","artifacts":[],"findings":[],"risks":[],"notes":[]}`}
			output := executeRun(context.Background(), runner, noFilesReader{}, RunInput{Role: role, Task: "task"})
			if output.Status != "success" || output.Role != role {
				t.Fatalf("output = %#v", output)
			}
		})
	}
}

func TestExecuteRunReturnsStructuredErrorForBrokenLLMResponse(t *testing.T) {
	output := executeRun(context.Background(), staticRunner{response: "not JSON"}, noFilesReader{}, RunInput{Role: "reviewer", Task: "review"})
	if output.Status != "error" || output.Error == nil || output.Error.Kind != "LLMInvalidJSONError" {
		t.Fatalf("output = %#v", output)
	}
}

func TestExecuteRunReturnsStructuredErrorsBeforeCallingRunner(t *testing.T) {
	t.Run("invalid role", func(t *testing.T) {
		runner := &countingRunner{}
		output := executeRun(context.Background(), runner, noFilesReader{}, RunInput{Role: "architect", Task: "task"})
		if output.Status != "error" || output.Error == nil || output.Error.Kind != "InvalidRoleError" || runner.calls != 0 {
			t.Fatalf("output=%#v runner calls=%d", output, runner.calls)
		}
	})

	t.Run("file read failure", func(t *testing.T) {
		wantErr := &workspace.WorkspaceError{Operation: "read file", Path: "missing.go", Err: errors.New("not found")}
		runner := &countingRunner{}
		output := executeRun(context.Background(), runner, errorFileReader{err: wantErr}, RunInput{Role: "coder", Task: "task", Files: []string{"missing.go"}})
		if output.Status != "error" || output.Error == nil || output.Error.Kind != "WorkspaceError" || runner.calls != 0 {
			t.Fatalf("output=%#v runner calls=%d", output, runner.calls)
		}
	})
}

func TestParseRunOutputRejectsMalformedEnvelopes(t *testing.T) {
	valid := `{"status":"success","role":"coder","summary":"ok","artifacts":[],"findings":[],"risks":[],"notes":[]}`
	for _, test := range []struct {
		name      string
		raw       string
		wantError string
	}{
		{name: "unknown field", raw: `{"status":"success","role":"coder","summary":"ok","unexpected":true}`, wantError: "LLMInvalidJSONError"},
		{name: "multiple values", raw: valid + ` {}`, wantError: "LLMInvalidJSONError"},
		{name: "invalid status", raw: `{"status":"partial","role":"coder","summary":"ok"}`, wantError: "LLMResponseError"},
		{name: "wrong role", raw: `{"status":"success","role":"reviewer","summary":"ok"}`, wantError: "LLMResponseError"},
		{name: "empty summary", raw: `{"status":"success","role":"coder","summary":"  "}`, wantError: "LLMResponseError"},
		{name: "error without details", raw: `{"status":"error","role":"coder","summary":"failed"}`, wantError: "LLMResponseError"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseRunOutput(test.raw, agent.RoleCoder)
			var llmErr *llm.Error
			if !errors.As(err, &llmErr) || string(llmErr.Kind) != test.wantError {
				t.Fatalf("parseRunOutput() error = %v, want %s", err, test.wantError)
			}
		})
	}
}

func TestParseRunOutputNormalizesNullCollections(t *testing.T) {
	raw := `{"status":"success","role":"coder","summary":"ok","artifacts":null,"findings":null,"risks":null,"notes":null,"changed_files":null,"execution_results":null,"warnings":null}`
	output, err := parseRunOutput(raw, agent.RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if output.Artifacts == nil || output.Findings == nil || output.Risks == nil || output.Notes == nil || output.ChangedFiles == nil || output.ExecutionResults == nil || output.Warnings == nil {
		t.Fatalf("nil collections were not normalized: %#v", output)
	}
}

func TestErrorKindPreservesWrappedAgentAndBackendCategories(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "wrapped invalid role", err: fmt.Errorf("wrapped: %w", &agent.InvalidRoleError{Role: "architect"}), want: "InvalidRoleError"},
		{name: "wrapped prompt error", err: fmt.Errorf("wrapped: %w", &agent.PromptError{Err: errors.New("empty task")}), want: "PromptError"},
		{name: "wrapped workspace error", err: fmt.Errorf("wrapped: %w", &workspace.WorkspaceError{Operation: "read", Path: "source.go", Err: errors.New("not found")}), want: "WorkspaceError"},
		{name: "LLM", err: fmt.Errorf("wrapped: %w", &llm.Error{Kind: llm.ErrorTimeout, Err: errors.New("timeout")}), want: "LLMTimeoutError"},
		{name: "unknown", err: errors.New("unexpected"), want: "InternalError"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := errorKind(test.err); got != test.want {
				t.Fatalf("errorKind() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExecuteRunClassifiesHTTPAgentResponses(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		finishReason string
		wantStatus   string
		wantError    string
	}{
		{
			name:         "stop with valid JSON",
			content:      `{"status":"success","role":"coder","summary":"ok","artifacts":[],"findings":[],"risks":[],"notes":[]}`,
			finishReason: "stop",
			wantStatus:   "success",
		},
		{name: "length with truncated JSON", content: `{"status":`, finishReason: "length", wantStatus: "error", wantError: "LLMTruncatedError"},
		{name: "stop with invalid JSON", content: `{"status":`, finishReason: "stop", wantStatus: "error", wantError: "LLMInvalidJSONError"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				response := llm.ChatCompletionResponse{Choices: []llm.Choice{{
					Message: llm.Message{Role: "assistant", Content: test.content}, FinishReason: test.finishReason,
				}}}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()

			client := llm.NewClient(config.Config{MainLLM: config.LLMConfig{BaseURL: server.URL, Model: "test", Timeout: time.Second}, LLMMaxTokens: 1, CoderMaxTokens: 1})
			runner, err := agent.NewRunner(client)
			if err != nil {
				t.Fatal(err)
			}
			output := executeRun(context.Background(), runner, noFilesReader{}, RunInput{Role: "coder", Task: "task"})
			if output.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q; output = %#v", output.Status, test.wantStatus, output)
			}
			if test.wantError != "" && (output.Error == nil || output.Error.Kind != test.wantError) {
				t.Fatalf("output = %#v, want error %s", output, test.wantError)
			}
		})
	}
}

func TestExecuteRunRejectsRoleDescriptionsForAllRoles(t *testing.T) {
	tests := []struct {
		role        string
		description string
	}{
		{role: "coder", description: "software implementation agent"},
		{role: "reviewer", description: "strict software code reviewer"},
		{role: "tester", description: "software test design agent"},
		{role: "fixer", description: "software repair agent"},
	}
	for _, test := range tests {
		t.Run(test.role, func(t *testing.T) {
			response := `{"status":"success","role":"` + test.description + `","summary":"ok","artifacts":[],"findings":[],"risks":[],"notes":[]}`
			output := executeRun(context.Background(), staticRunner{response: response}, noFilesReader{}, RunInput{Role: test.role, Task: "task"})
			if output.Status != "error" || output.Error == nil || output.Error.Kind != "LLMResponseError" {
				t.Fatalf("output = %#v, want LLMResponseError", output)
			}
		})
	}
}

type staticRunner struct{ response string }

func (r staticRunner) Run(context.Context, agent.PromptInput) (string, error) { return r.response, nil }

type countingRunner struct{ calls int }

func (r *countingRunner) Run(context.Context, agent.PromptInput) (string, error) {
	r.calls++
	return "", nil
}

type errorFileReader struct{ err error }

func (r errorFileReader) ReadFile(string) ([]byte, error) { return nil, r.err }

type noFilesReader struct{}

func (noFilesReader) ReadFile(string) ([]byte, error) { panic("unexpected file read") }

type recordingCompleter struct{ messages []llm.Message }

func (c *recordingCompleter) ChatCompletion(_ context.Context, messages []llm.Message, _ ...string) (llm.ChatCompletionResponse, error) {
	c.messages = append([]llm.Message(nil), messages...)
	return llm.ChatCompletionResponse{Choices: []llm.Choice{{Message: llm.Message{
		Role: "assistant", Content: `{"status":"success","role":"coder","summary":"candidate prepared","artifacts":[],"findings":[],"risks":[],"notes":[]}`,
	}}}}, nil
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func contains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func contentMust(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

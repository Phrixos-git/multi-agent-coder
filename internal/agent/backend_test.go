package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arietis/multi-agent-coder/internal/llm"
)

func TestNewDirectBackendRejectsNilClient(t *testing.T) {
	if backend, err := NewDirectBackend(nil); err == nil || backend != nil {
		t.Fatalf("NewDirectBackend(nil) = (%v, %v), want nil backend and error", backend, err)
	}
}

func TestDirectBackendRun(t *testing.T) {
	t.Run("sends role-specific prompt and returns first choice", func(t *testing.T) {
		client := &scriptedCompleter{response: llm.ChatCompletionResponse{Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: "candidate"}},
			{Message: llm.Message{Role: "assistant", Content: "ignored"}},
		}}}
		backend, err := NewDirectBackend(client)
		if err != nil {
			t.Fatal(err)
		}
		got, err := backend.Run(context.Background(), PromptInput{
			Role: RoleFixer, Task: "repair the candidate", Files: []File{{Path: "main.go", Content: "package sample"}},
		})
		if err != nil || got != "candidate" {
			t.Fatalf("Run() = %q, %v; want candidate", got, err)
		}
		if client.calls != 1 || len(client.roles) != 1 || client.roles[0] != string(RoleFixer) {
			t.Fatalf("calls=%d roles=%v", client.calls, client.roles)
		}
		if len(client.messages) != 2 || !strings.Contains(client.messages[1].Content, "repair the candidate") || !strings.Contains(client.messages[1].Content, "package sample") {
			t.Fatalf("messages = %#v", client.messages)
		}
	})

	t.Run("does not call client for invalid prompt", func(t *testing.T) {
		client := &scriptedCompleter{}
		backend, err := NewDirectBackend(client)
		if err != nil {
			t.Fatal(err)
		}
		_, err = backend.Run(context.Background(), PromptInput{Role: RoleCoder})
		var promptErr *PromptError
		if !errors.As(err, &promptErr) || client.calls != 0 {
			t.Fatalf("Run() error=%v calls=%d, want PromptError and no request", err, client.calls)
		}
	})

	t.Run("wraps client error", func(t *testing.T) {
		cause := errors.New("connection refused")
		backend, err := NewDirectBackend(&scriptedCompleter{err: cause})
		if err != nil {
			t.Fatal(err)
		}
		_, err = backend.Run(context.Background(), PromptInput{Role: RoleCoder, Task: "implement"})
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "run coder agent") {
			t.Fatalf("Run() error = %v, want wrapped client error", err)
		}
	})

	t.Run("classifies empty choices", func(t *testing.T) {
		backend, err := NewDirectBackend(&scriptedCompleter{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = backend.Run(context.Background(), PromptInput{Role: RoleCoder, Task: "implement"})
		var llmErr *llm.Error
		if !errors.As(err, &llmErr) || llmErr.Kind != llm.ErrorResponse {
			t.Fatalf("Run() error = %v, want LLMResponseError", err)
		}
	})
}

type scriptedCompleter struct {
	response llm.ChatCompletionResponse
	err      error
	calls    int
	messages []llm.Message
	roles    []string
}

func (c *scriptedCompleter) ChatCompletion(_ context.Context, messages []llm.Message, roles ...string) (llm.ChatCompletionResponse, error) {
	c.calls++
	c.messages = append([]llm.Message(nil), messages...)
	c.roles = append([]string(nil), roles...)
	return c.response, c.err
}

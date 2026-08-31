package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/arietis/multi-agent-coder/internal/llm"
)

func TestRunnerUsesSharedClientWithIndependentRequests(t *testing.T) {
	client := &recordingClient{}
	runner, err := NewRunner(client)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	for _, input := range []PromptInput{
		{Role: RoleCoder, Task: "coder-only-task"},
		{Role: RoleReviewer, Task: "reviewer-only-task"},
	} {
		if _, err := runner.Run(context.Background(), input); err != nil {
			t.Fatalf("Run(%s) error = %v", input.Role, err)
		}
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 2 {
		t.Fatalf("request count = %d", len(client.requests))
	}
	if len(client.requests[0]) != 2 || len(client.requests[1]) != 2 {
		t.Fatalf("message counts = %d, %d", len(client.requests[0]), len(client.requests[1]))
	}
	if containsMessage(client.requests[1], "coder-only-task") || containsMessage(client.requests[0], "reviewer-only-task") {
		t.Fatalf("agent request contexts leaked: %#v", client.requests)
	}
	if client.requests[0][0].Content == client.requests[1][0].Content {
		t.Fatal("coder and reviewer received the same system prompt")
	}
}

func TestRunnerRejectsResponseWithoutChoices(t *testing.T) {
	runner, err := NewRunner(emptyResponseClient{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), PromptInput{Role: RoleCoder, Task: "task"})
	var llmErr *llm.Error
	if !errors.As(err, &llmErr) || llmErr.Kind != llm.ErrorResponse {
		t.Fatalf("Run() error = %v, want LLMResponseError", err)
	}
}

type recordingClient struct {
	mu       sync.Mutex
	requests [][]llm.Message
}

type emptyResponseClient struct{}

func (emptyResponseClient) ChatCompletion(context.Context, []llm.Message) (llm.ChatCompletionResponse, error) {
	return llm.ChatCompletionResponse{}, nil
}

func (c *recordingClient) ChatCompletion(_ context.Context, messages []llm.Message) (llm.ChatCompletionResponse, error) {
	copied := append([]llm.Message(nil), messages...)
	c.mu.Lock()
	c.requests = append(c.requests, copied)
	c.mu.Unlock()
	return llm.ChatCompletionResponse{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: `{}`}}}}, nil
}

func containsMessage(messages []llm.Message, value string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, value) {
			return true
		}
	}
	return false
}

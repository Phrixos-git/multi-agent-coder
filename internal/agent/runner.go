package agent

import (
	"context"
	"fmt"

	"github.com/arietis/multi-agent-coder/internal/llm"
)

// ChatCompleter is the shared LLM capability needed by all agents.
type ChatCompleter interface {
	ChatCompletion(context.Context, []llm.Message, ...string) (llm.ChatCompletionResponse, error)
}

// Runner executes independent logical agents against one shared LLM client.
type Runner struct {
	client  ChatCompleter
	prompts *PromptBuilder
}

// NewRunner constructs a runner using embedded role prompts.
func NewRunner(client ChatCompleter) (*Runner, error) {
	if client == nil {
		return nil, fmt.Errorf("create agent runner: LLM client is nil")
	}
	promptBuilder, err := NewPromptBuilder()
	if err != nil {
		return nil, err
	}
	return &Runner{client: client, prompts: promptBuilder}, nil
}

// Run creates a fresh request for one role and returns its generated content.
// The runner stores no conversation messages between calls.
func (r *Runner) Run(ctx context.Context, input PromptInput) (string, error) {
	messages, err := r.prompts.Build(input)
	if err != nil {
		return "", err
	}
	response, err := r.client.ChatCompletion(ctx, messages, string(input.Role))
	if err != nil {
		return "", fmt.Errorf("run %s agent: %w", input.Role, err)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("run %s agent: %w", input.Role, &llm.Error{
			Kind: llm.ErrorResponse,
			Err:  fmt.Errorf("response contains no choices"),
		})
	}
	return response.Choices[0].Message.Content, nil
}

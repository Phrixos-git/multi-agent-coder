package agent

import (
	"context"
	"fmt"

	"github.com/arietis/multi-agent-coder/internal/llm"
)

// Backend executes one isolated role invocation.
type Backend interface {
	Run(context.Context, PromptInput) (string, error)
}

// DirectBackend preserves the original direct llama.cpp request path.
type DirectBackend struct {
	client  ChatCompleter
	prompts *PromptBuilder
}

func NewDirectBackend(client ChatCompleter) (*DirectBackend, error) {
	if client == nil {
		return nil, fmt.Errorf("create direct backend: LLM client is nil")
	}
	p, err := NewPromptBuilder()
	if err != nil {
		return nil, err
	}
	return &DirectBackend{client: client, prompts: p}, nil
}

// NewDirectBackendForOrchestrator is NewDirectBackend with role prompts
// selected for the given MCP client (see NewPromptBuilderForOrchestrator).
func NewDirectBackendForOrchestrator(client ChatCompleter, orchestrator string) (*DirectBackend, error) {
	if client == nil {
		return nil, fmt.Errorf("create direct backend: LLM client is nil")
	}
	p, err := NewPromptBuilderForOrchestrator(orchestrator)
	if err != nil {
		return nil, err
	}
	return &DirectBackend{client: client, prompts: p}, nil
}

func (b *DirectBackend) Run(ctx context.Context, input PromptInput) (string, error) {
	messages, err := b.prompts.Build(input)
	if err != nil {
		return "", err
	}
	response, err := b.client.ChatCompletion(ctx, messages, string(input.Role))
	if err != nil {
		return "", fmt.Errorf("run %s agent: %w", input.Role, err)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("run %s agent: %w", input.Role, &llm.Error{Kind: llm.ErrorResponse, Err: fmt.Errorf("response contains no choices")})
	}
	return response.Choices[0].Message.Content, nil
}

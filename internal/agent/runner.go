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
	backends map[Role]Backend
}

// NewRunner constructs a runner using embedded role prompts.
func NewRunner(client ChatCompleter) (*Runner, error) {
	if client == nil {
		return nil, fmt.Errorf("create agent runner: LLM client is nil")
	}
	direct, err := NewDirectBackend(client)
	if err != nil {
		return nil, err
	}
	return NewRunnerWithBackends(direct, nil), nil
}

func NewRunnerWithBackends(direct Backend, coder Backend) *Runner {
	if coder == nil {
		coder = direct
	}
	return &Runner{backends: map[Role]Backend{RoleCoder: coder, RoleReviewer: direct, RoleTester: direct, RoleFixer: direct}}
}

// Run creates a fresh request for one role and returns its generated content.
// The runner stores no conversation messages between calls.
func (r *Runner) Run(ctx context.Context, input PromptInput) (string, error) {
	b, ok := r.backends[input.Role]
	if !ok || b == nil {
		return "", fmt.Errorf("run %s agent: backend is not configured", input.Role)
	}
	return b.Run(ctx, input)
}

func (r *Runner) NeedsInputFiles(role Role) bool {
	b := r.backends[role]
	if consumer, ok := b.(suppliedFileConsumer); ok {
		return consumer.UsesSuppliedFiles()
	}
	return true
}

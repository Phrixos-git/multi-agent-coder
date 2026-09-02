package agent

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/arietis/multi-agent-coder/internal/llm"
	roleprompts "github.com/arietis/multi-agent-coder/prompts"
)

const outputFormat = `The "role" field is a machine-readable role identifier, not a role description.
It must be one of "coder", "reviewer", "tester", or "fixer". For this request, it must be %q.

Return one JSON object with this shape:
{
  "status": "success or needs_input",
  "role": %q,
  "summary": "short result summary",
  "artifacts": [{"path": "relative path", "content": "complete candidate file content"}],
  "findings": [{"severity": "low, medium, or high", "description": "finding"}],
  "risks": ["risk"],
  "notes": ["note"]
}
Return no text outside the JSON object.`

// PromptInput is all context made available to one independent agent request.
type PromptInput struct {
	Role              Role
	Task              string
	Requirements      []string
	Files             []File
	AdditionalContext string
}

// File is caller-supplied file content. PromptBuilder never reads it itself.
type File struct {
	Path    string
	Content string
}

// PromptBuilder creates fresh role-specific chat messages.
type PromptBuilder struct {
	systemPrompts map[Role]string
}

// NewPromptBuilder loads all embedded role prompts.
func NewPromptBuilder() (*PromptBuilder, error) {
	return newPromptBuilder(roleprompts.FS)
}

func newPromptBuilder(promptFS fs.FS) (*PromptBuilder, error) {
	prompts := make(map[Role]string, len(validRoles))
	for role := range validRoles {
		content, err := fs.ReadFile(promptFS, string(role)+".md")
		if err != nil {
			return nil, &PromptError{Err: fmt.Errorf("load %s system prompt: %w", role, err)}
		}
		prompt := strings.TrimSpace(string(content))
		if prompt == "" {
			return nil, &PromptError{Err: fmt.Errorf("%s system prompt is empty", role)}
		}
		prompts[role] = prompt
	}
	return &PromptBuilder{systemPrompts: prompts}, nil
}

// Build returns a new two-message context containing only this invocation.
func (b *PromptBuilder) Build(input PromptInput) ([]llm.Message, error) {
	if !input.Role.Valid() {
		return nil, &InvalidRoleError{Role: string(input.Role)}
	}
	if strings.TrimSpace(input.Task) == "" {
		return nil, &PromptError{Err: fmt.Errorf("task is empty")}
	}

	var user strings.Builder
	writeSection(&user, "TASK", input.Task)
	writeListSection(&user, "REQUIREMENTS", input.Requirements)
	writeFilesSection(&user, input.Files)
	writeSection(&user, "ADDITIONAL CONTEXT", input.AdditionalContext)
	writeSection(&user, "OUTPUT FORMAT", fmt.Sprintf(outputFormat, input.Role, input.Role))

	return []llm.Message{
		{Role: "system", Content: b.systemPrompts[input.Role]},
		{Role: "user", Content: strings.TrimSpace(user.String())},
	}, nil
}

// PromptError reports that a prompt could not be loaded or built.
type PromptError struct {
	Err error
}

func (e *PromptError) Error() string { return "PromptError: " + e.Err.Error() }
func (e *PromptError) Unwrap() error { return e.Err }

func writeSection(builder *strings.Builder, heading, value string) {
	fmt.Fprintf(builder, "# %s\n\n%s\n\n", heading, valueOrNone(value))
}

func writeListSection(builder *strings.Builder, heading string, values []string) {
	fmt.Fprintf(builder, "# %s\n\n", heading)
	if len(values) == 0 {
		builder.WriteString("(none)\n\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(builder, "- %s\n", value)
	}
	builder.WriteString("\n")
}

func writeFilesSection(builder *strings.Builder, files []File) {
	builder.WriteString("# FILES\n\n")
	if len(files) == 0 {
		builder.WriteString("(none)\n\n")
		return
	}
	for _, file := range files {
		fmt.Fprintf(builder, "## %s\n\n```\n%s\n```\n\n", file.Path, file.Content)
	}
}

func valueOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(none)"
	}
	return value
}

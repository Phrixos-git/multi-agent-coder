package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestPromptBuilderBuildsEveryRole(t *testing.T) {
	builder, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() error = %v", err)
	}
	for _, role := range []Role{RoleCoder, RoleReviewer, RoleTester, RoleFixer} {
		t.Run(string(role), func(t *testing.T) {
			messages, err := builder.Build(PromptInput{
				Role:              role,
				Task:              "implement feature",
				Requirements:      []string{"keep API stable"},
				Files:             []File{{Path: "main.go", Content: "package main"}},
				AdditionalContext: "context",
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
				t.Fatalf("messages = %#v", messages)
			}
			for _, section := range []string{"# TASK", "# REQUIREMENTS", "# FILES", "# ADDITIONAL CONTEXT", "# OUTPUT FORMAT"} {
				if !strings.Contains(messages[1].Content, section) {
					t.Errorf("user prompt does not contain %q", section)
				}
			}
			if !strings.Contains(messages[0].Content, "cannot read or write files") || !strings.Contains(messages[0].Content, "cannot execute shell commands") {
				t.Errorf("system prompt does not enforce capability boundaries: %q", messages[0].Content)
			}
			expectedRole := `"role": "` + string(role) + `"`
			if !strings.Contains(messages[1].Content, expectedRole) {
				t.Errorf("user prompt does not contain exact role identifier %q: %q", expectedRole, messages[1].Content)
			}
			if !strings.Contains(messages[1].Content, `must be one of "coder", "reviewer", "tester", or "fixer"`) ||
				!strings.Contains(messages[1].Content, `not a role description`) {
				t.Errorf("user prompt does not explain role identifier semantics: %q", messages[1].Content)
			}
		})
	}
}

func TestPromptBuilderRejectsInvalidInput(t *testing.T) {
	builder, err := NewPromptBuilder()
	if err != nil {
		t.Fatal(err)
	}

	_, err = builder.Build(PromptInput{Role: "unknown", Task: "task"})
	var roleErr *InvalidRoleError
	if !errors.As(err, &roleErr) {
		t.Fatalf("invalid role error = %v", err)
	}

	_, err = builder.Build(PromptInput{Role: RoleCoder})
	var promptErr *PromptError
	if !errors.As(err, &promptErr) {
		t.Fatalf("empty task error = %v", err)
	}
}

func TestRoleSystemPromptsAreIndependent(t *testing.T) {
	builder, err := NewPromptBuilder()
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]Role)
	for _, role := range []Role{RoleCoder, RoleReviewer, RoleTester, RoleFixer} {
		messages, err := builder.Build(PromptInput{Role: role, Task: "same task"})
		if err != nil {
			t.Fatal(err)
		}
		if previous, exists := seen[messages[0].Content]; exists {
			t.Fatalf("roles %s and %s share a system prompt", previous, role)
		}
		seen[messages[0].Content] = role
	}
}

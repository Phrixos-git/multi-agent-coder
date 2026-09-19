package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/arietis/multi-agent-coder/internal/agent"
	"github.com/arietis/multi-agent-coder/internal/llm"
	"github.com/arietis/multi-agent-coder/internal/workspace"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RunInput is the complete, repository-read-only input to local_agent_run.
type RunInput struct {
	Role              string   `json:"role" jsonschema:"agent role: coder, reviewer, tester, or fixer"`
	Task              string   `json:"task" jsonschema:"task specification"`
	Requirements      []string `json:"requirements,omitempty" jsonschema:"requirements and constraints"`
	Files             []string `json:"files,omitempty" jsonschema:"repository-relative files to supply to the agent"`
	AdditionalContext string   `json:"additional_context,omitempty" jsonschema:"additional context for this invocation only"`
}

type Artifact struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Finding struct {
	Severity    string `json:"severity"`
	Description string `json:"description"`
}

// RunError is a safe, structured failure returned instead of panicking.
type RunError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// RunOutput is the validated local agent result.
type RunOutput struct {
	Status           string     `json:"status"`
	Role             string     `json:"role"`
	Summary          string     `json:"summary"`
	Artifacts        []Artifact `json:"artifacts"`
	Findings         []Finding  `json:"findings"`
	Risks            []string   `json:"risks"`
	Notes            []string   `json:"notes"`
	Error            *RunError  `json:"error,omitempty"`
	Backend          string     `json:"backend,omitempty"`
	Patch            string     `json:"patch,omitempty"`
	ChangedFiles     []string   `json:"changed_files,omitempty"`
	ExecutionResults []string   `json:"execution_results,omitempty"`
	Warnings         []string   `json:"warnings,omitempty"`
}

type agentRunner interface {
	Run(context.Context, agent.PromptInput) (string, error)
}

type fileReader interface {
	ReadFile(string) ([]byte, error)
}

func registerRunTool(server *sdkmcp.Server, runner agentRunner, reader fileReader, logger *slog.Logger) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "local_agent_run",
		Description: "Run an independent coder, reviewer, tester, or fixer. Direct mode is read-only; optional Pi coder mode edits only an isolated temporary Git worktree.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, input RunInput) (*sdkmcp.CallToolResult, RunOutput, error) {
		started, requestID := time.Now(), newRequestID()
		output := executeRun(ctx, runner, reader, input)
		errorType := ""
		if output.Error != nil {
			errorType = output.Error.Kind
		}
		backend := output.Backend
		if backend == "" {
			backend = "direct"
		}
		logger.InfoContext(ctx, "MCP tool request completed",
			"role", input.Role, "agent", input.Role, "request_id", requestID,
			"backend", backend, "duration", time.Since(started),
			"status", output.Status, "result", output.Status, "error_type", errorType)
		return nil, output, nil
	})
}

func executeRun(ctx context.Context, runner agentRunner, reader fileReader, input RunInput) RunOutput {
	role, err := agent.ParseRole(input.Role)
	if err != nil {
		return failedOutput(input.Role, err)
	}
	files := make([]agent.File, 0, len(input.Files))
	needsFiles := true
	if policy, ok := runner.(interface{ NeedsInputFiles(agent.Role) bool }); ok {
		needsFiles = policy.NeedsInputFiles(role)
	}
	if !needsFiles {
		for _, path := range input.Files {
			if validator, ok := reader.(interface{ ValidatePath(string) error }); ok {
				if err := validator.ValidatePath(path); err != nil {
					return failedOutput(input.Role, err)
				}
			}
			files = append(files, agent.File{Path: path})
		}
	}
	for _, path := range input.Files {
		if !needsFiles {
			break
		}
		content, err := reader.ReadFile(path)
		if err != nil {
			return failedOutput(input.Role, err)
		}
		files = append(files, agent.File{Path: path, Content: string(content)})
	}

	raw, err := runner.Run(ctx, agent.PromptInput{Role: role, Task: input.Task,
		Requirements: input.Requirements, Files: files, AdditionalContext: input.AdditionalContext})
	if err != nil {
		return failedOutput(input.Role, err)
	}
	output, err := parseRunOutput(raw, role)
	if err != nil {
		return failedOutput(input.Role, err)
	}
	if output.Backend == "" {
		output.Backend = "direct"
	}
	return output
}

func parseRunOutput(raw string, expectedRole agent.Role) (RunOutput, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var output RunOutput
	if err := decoder.Decode(&output); err != nil {
		return RunOutput{}, &llm.Error{Kind: llm.ErrorInvalidJSON, Err: fmt.Errorf("decode agent JSON: %w", err)}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return RunOutput{}, &llm.Error{Kind: llm.ErrorInvalidJSON, Err: fmt.Errorf("validate agent JSON: %w", err)}
	}
	if output.Status != "success" && output.Status != "needs_input" && output.Status != "error" {
		return RunOutput{}, responseValidationError("invalid status %q", output.Status)
	}
	if output.Role != string(expectedRole) {
		return RunOutput{}, responseValidationError("role %q does not match requested role %q", output.Role, expectedRole)
	}
	if strings.TrimSpace(output.Summary) == "" {
		return RunOutput{}, responseValidationError("summary is empty")
	}
	if output.Status == "error" && output.Error == nil {
		return RunOutput{}, responseValidationError("error status has no error details")
	}
	output.Artifacts = nonNil(output.Artifacts)
	output.Findings = nonNil(output.Findings)
	output.Risks = nonNil(output.Risks)
	output.Notes = nonNil(output.Notes)
	output.ChangedFiles = nonNil(output.ChangedFiles)
	output.ExecutionResults = nonNil(output.ExecutionResults)
	output.Warnings = nonNil(output.Warnings)
	return output, nil
}

func responseValidationError(format string, args ...any) error {
	return &llm.Error{Kind: llm.ErrorResponse, Err: fmt.Errorf(format, args...)}
}

func failedOutput(role string, err error) RunOutput {
	return RunOutput{Status: "error", Role: role, Artifacts: []Artifact{}, Findings: []Finding{},
		Risks: []string{}, Notes: []string{}, Error: &RunError{Kind: errorKind(err), Message: err.Error()}}
}

func errorKind(err error) string {
	var invalidRole *agent.InvalidRoleError
	var prompt *agent.PromptError
	var workspaceErr *workspace.WorkspaceError
	var llmErr *llm.Error
	var categorized interface{ ErrorKind() string }
	switch {
	case errors.As(err, &invalidRole):
		return "InvalidRoleError"
	case errors.As(err, &prompt):
		return "PromptError"
	case errors.As(err, &workspaceErr):
		return "WorkspaceError"
	case errors.As(err, &llmErr):
		return string(llmErr.Kind)
	case errors.As(err, &categorized):
		return categorized.ErrorKind()
	default:
		message := err.Error()
		for _, kind := range []string{"PiExecutableNotFound", "PiStartupError", "PiRPCProtocolError", "PiTimeoutError", "PiCanceledError", "PiAbnormalExit", "GitRepositoryError", "WorktreeCreateError", "DiffCollectionError", "CleanupError"} {
			if strings.Contains(message, kind) {
				return kind
			}
		}
		return "InternalError"
	}
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

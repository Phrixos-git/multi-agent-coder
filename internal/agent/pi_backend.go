package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/arietis/multi-agent-coder/internal/pirpc"
	"github.com/arietis/multi-agent-coder/internal/worktree"
)

type PiBackend struct {
	manager worktree.Manager
	client  pirpc.Client
	timeout time.Duration
	logger  *slog.Logger
}

func NewPiBackend(repository string, cfg pirpc.Config, timeout time.Duration, logger *slog.Logger) *PiBackend {
	return &PiBackend{manager: worktree.Manager{Repository: repository}, client: pirpc.Client{Config: cfg, Logger: logger}, timeout: timeout, logger: logger}
}

func (*PiBackend) UsesSuppliedFiles() bool { return false }

func (b *PiBackend) Run(ctx context.Context, input PromptInput) (raw string, retErr error) {
	started := time.Now()
	dirty, err := b.manager.Dirty(ctx)
	if err != nil {
		return "", fmt.Errorf("inspect source working tree: %w", err)
	}
	tree, err := b.manager.Create(ctx)
	if err != nil {
		return "", err
	}
	taskID := strings.TrimPrefix(tree.Branch, "mac/task-")
	b.logger.InfoContext(ctx, "Pi worktree created", "task_id", taskID, "role", input.Role, "backend", "pi", "source_repository", b.manager.Repository, "worktree_path", tree.Path, "temporary_branch", tree.Branch)
	diffCaptured := false
	changes := worktree.Changes{}
	result := pirpc.Result{}
	var operationErr error
	warnings := []string{}
	if dirty {
		warnings = append(warnings, "The source working tree contains uncommitted changes. Pi executed against HEAD and did not receive those changes.")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if !diffCaptured {
			if c, e := b.manager.Diff(cleanupCtx, tree); e == nil {
				changes = c
				diffCaptured = true
			} else {
				warnings = append(warnings, "Diff collection failed: "+e.Error())
				if operationErr == nil {
					operationErr = e
				}
			}
		}
		var cleanupErr error
		if diffCaptured {
			cleanupErr = b.manager.Cleanup(cleanupCtx, tree)
		} else {
			cleanupErr = b.manager.ForceCleanup(cleanupCtx, tree)
		}
		if e := cleanupErr; e != nil {
			b.logger.Error("Pi cleanup failed", "task_id", taskID, "error", e)
			warnings = append(warnings, "Cleanup failed: "+e.Error())
			if operationErr == nil {
				operationErr = e
			}
		} else {
			b.logger.Info("Pi cleanup completed", "task_id", taskID, "temporary_branch", tree.Branch)
		}
		summary := strings.TrimSpace(result.Summary)
		if summary != "" {
			summary = "Pi reported: " + summary
		}
		if operationErr != nil && summary == "" {
			summary = "Pi coder execution failed."
		}
		if operationErr == nil && summary == "" {
			summary = "Pi completed; no final summary was provided."
		}
		status := "success"
		var errorValue any
		if operationErr != nil {
			status = "error"
			errorValue = map[string]string{"kind": piErrorKind(operationErr), "message": operationErr.Error()}
		}
		out := map[string]any{"status": status, "role": "coder", "summary": summary, "artifacts": []any{}, "findings": []any{}, "risks": []any{}, "notes": []string{"backend: pi", "Pi summary is model-reported; patch and tool events are independently collected."}, "backend": "pi", "patch": changes.Patch, "changed_files": changes.Files, "execution_results": result.ToolResults, "warnings": warnings}
		if errorValue != nil {
			out["error"] = errorValue
		}
		data, e := json.Marshal(out)
		if e != nil {
			retErr = fmt.Errorf("encode Pi result: %w", e)
		} else {
			raw = string(data)
			retErr = nil
		}
		errorKind := ""
		if operationErr != nil {
			errorKind = piErrorKind(operationErr)
		}
		b.logger.Info("Pi process exited", "task_id", taskID, "role", input.Role, "backend", "pi", "duration", time.Since(started), "success", operationErr == nil, "error_kind", errorKind)
	}()
	runCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	b.logger.InfoContext(ctx, "Pi process starting", "task_id", taskID, "role", input.Role, "backend", "pi", "worktree_path", tree.Path)
	result, operationErr = b.client.Run(runCtx, tree.Path, piPrompt(input))
	collectCtx, collectCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer collectCancel()
	changes, diffErr := b.manager.Diff(collectCtx, tree)
	if diffErr != nil {
		if operationErr == nil {
			operationErr = diffErr
		}
		return "", nil
	}
	diffCaptured = true
	if result.AgentError != "" && operationErr == nil {
		operationErr = &piAgentError{message: result.AgentError}
	}
	b.logger.InfoContext(ctx, "Pi process completed", "task_id", taskID, "role", input.Role, "backend", "pi", "duration", time.Since(started), "changed_files_count", len(changes.Files), "tool_execution_count", len(result.ToolResults))
	return "", nil
}

func piErrorKind(err error) string {
	if categorized, ok := err.(interface{ ErrorKind() string }); ok {
		return categorized.ErrorKind()
	}
	message := err.Error()
	for _, kind := range []string{"PiExecutableNotFound", "PiStartupError", "PiRPCProtocolError", "PiTimeoutError", "PiCanceledError", "PiAbnormalExit", "PiAgentError", "DiffCollectionError", "CleanupError"} {
		if strings.Contains(message, kind) {
			return kind
		}
	}
	return "InternalError"
}

type piAgentError struct{ message string }

func (e *piAgentError) Error() string   { return "PiAgentError: " + e.message }
func (*piAgentError) ErrorKind() string { return "PiAgentError" }

func piPrompt(input PromptInput) string {
	var s strings.Builder
	s.WriteString("You are the coder agent operating only inside a temporary Git worktree. First inspect the repository and all applicable AGENTS.md instructions. Implement only the requested scope, preserve existing design and public APIs, then run appropriate build, tests, and lint. If a check fails, investigate, fix, and rerun it. Do not use multi-agent-coder MCP. Do not commit or push. Do not rewrite Git history, manage branches/worktrees, run git reset --hard, git clean -fdx, git rebase, git checkout, or git switch. Do not modify unrelated files. Finish with a concise summary and test results.\n\nTask:\n")
	s.WriteString(input.Task)
	if len(input.Requirements) > 0 {
		s.WriteString("\n\nRequirements:\n- ")
		s.WriteString(strings.Join(input.Requirements, "\n- "))
	}
	if len(input.Files) > 0 {
		paths := make([]string, 0, len(input.Files))
		for _, file := range input.Files {
			paths = append(paths, file.Path)
		}
		s.WriteString("\n\nRequested file hints (inspect these paths in HEAD; no caller working-tree content is supplied):\n- ")
		s.WriteString(strings.Join(paths, "\n- "))
	}
	if input.AdditionalContext != "" {
		s.WriteString("\n\nAdditional context:\n")
		s.WriteString(input.AdditionalContext)
	}
	return s.String()
}

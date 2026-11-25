package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/arietis/multi-agent-coder/internal/config"
	"github.com/arietis/multi-agent-coder/internal/workspace"
)

const (
	piStartupTimeout = 15 * time.Second
	piIdleTimeout    = 5 * time.Minute
	piShutdownGrace  = 3 * time.Second
	maxRPCLine       = 4 << 20
	maxToolOutput    = 12 << 10
)

// PiBackend runs a coder or fixer in a fresh Pi RPC process and temporary worktree.
type PiBackend struct {
	cfg            config.Config
	repositoryRoot string
}

func NewPiBackend(cfg config.Config, repositoryRoot string) *PiBackend {
	return &PiBackend{cfg: cfg, repositoryRoot: repositoryRoot}
}

func (b *PiBackend) Run(ctx context.Context, input PromptInput) (string, error) {
	result, err := b.runPi(ctx, input)
	if err != nil {
		kind := "PiBackendError"
		if i := strings.Index(err.Error(), ":"); i > 0 {
			kind = err.Error()[:i]
		}
		result.Status = "error"
		result.Error = &piRunError{Kind: kind, Message: err.Error()}
		if strings.TrimSpace(result.Summary) == "" {
			result.Summary = err.Error()
		}
	}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil && err == nil {
		err = marshalErr
	}
	return string(encoded), nil
}

type piRunError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type piResult struct {
	Status           string      `json:"status"`
	Role             string      `json:"role"`
	Summary          string      `json:"summary"`
	Artifacts        []any       `json:"artifacts"`
	Findings         []any       `json:"findings"`
	Risks            []string    `json:"risks"`
	Notes            []string    `json:"notes"`
	Backend          string      `json:"backend"`
	Patch            string      `json:"patch"`
	ChangedFiles     []string    `json:"changed_files"`
	ExecutionResults []string    `json:"execution_results"`
	Warnings         []string    `json:"warnings"`
	Error            *piRunError `json:"error,omitempty"`
	PiModel          string      `json:"pi_model"`
	PiExitStatus     string      `json:"pi_exit_status"`
	RepairCycleCount int         `json:"repair_cycle_count"`
	TestCommandCount int         `json:"test_command_count"`
	ToolCallCount    int         `json:"tool_call_count"`
}

func (b *PiBackend) runPi(parent context.Context, input PromptInput) (result piResult, retErr error) {
	result = piResult{Status: "success", Role: string(input.Role), Artifacts: []any{}, Findings: []any{}, Risks: []string{}, Notes: []string{}, Backend: "pi", ChangedFiles: []string{}, ExecutionResults: []string{}, Warnings: []string{}, PiModel: b.cfg.PiModel, PiExitStatus: "not_started"}
	requestID := fmt.Sprintf("%d", time.Now().UnixNano())
	wt, dirty, err := workspace.CreateWorktree(b.repositoryRoot, requestID)
	if err != nil {
		return result, err
	}
	collected := false
	defer func() {
		if !collected {
			patch, files, collectErr := wt.Collect()
			result.Patch, result.ChangedFiles = patch, files
			if collectErr != nil {
				result.Warnings = append(result.Warnings, collectErr.Error())
				if retErr == nil {
					retErr = collectErr
				}
			}
		}
		if e := wt.Cleanup(); e != nil {
			result.Warnings = append(result.Warnings, e.Error())
		}
	}()
	if input.Role == RoleFixer {
		paths := make([]string, 0, len(input.Files))
		for _, file := range input.Files {
			paths = append(paths, file.Path)
		}
		if err := wt.OverlayCurrent(paths); err != nil {
			collected = true
			return result, err
		}
	}
	if dirty {
		if input.Role == RoleFixer {
			result.Warnings = append(result.Warnings, "Main Working Tree contains dirty files. Only files explicitly supplied to fixer were overlaid.")
		} else {
			result.Warnings = append(result.Warnings, "Source working tree contains uncommitted changes. Pi worked from HEAD and did not receive those changes.")
		}
	}
	if _, err := exec.LookPath(b.cfg.PiExecutable); err != nil {
		return result, fmt.Errorf("PiExecutableNotFound: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	if b.cfg.PiTimeout > 0 {
		cancel()
		ctx, cancel = context.WithTimeout(parent, b.cfg.PiTimeout)
	}
	defer cancel()
	args := []string{"--mode", "rpc", "--no-session", "--no-extensions", "--tools", "read,bash,edit,write", "--provider", b.cfg.PiProvider, "--model", b.cfg.PiModel, "--thinking", "medium"}
	if b.cfg.PiRPCArgs != "" {
		args = append(args, strings.Fields(b.cfg.PiRPCArgs)...)
	}
	cmd := exec.Command(b.cfg.PiExecutable, args...)
	cmd.Dir = wt.Path()
	cmd.Env = workspace.GitWorktreeEnv(cmd)
	if b.cfg.PiAgentDir != "" {
		cmd.Env = replaceEnv(cmd.Env, "PI_CODING_AGENT_DIR", b.cfg.PiAgentDir)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return result, fmt.Errorf("PiStartupError: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("PiStartupError: stdout: %w", err)
	}
	var stderr limitedBuffer
	stderr.limit = 32 << 10
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		return result, fmt.Errorf("PiStartupError: %w", err)
	}
	result.PiExitStatus = "running"
	lines := make(chan rpcLine, 32)
	go readRPC(stdout, lines, ctx)
	prompt := buildPiPrompt(input)
	request := map[string]any{"id": "mac-prompt", "type": "prompt", "message": prompt}
	writeDone := make(chan error, 1)
	go func() { writeDone <- writeRPC(stdin, request) }()
	settled, accepted := false, false
	commands := map[string]string{}
	failedTestInTurn := false
	var testFailures int
	var finalText strings.Builder
	var waitErr error
	startup := time.NewTimer(piStartupTimeout)
	defer startup.Stop()
	for !settled {
		var idle *time.Timer
		idle = time.NewTimer(piIdleTimeout)
		select {
		case <-ctx.Done():
			_ = writeRPC(stdin, map[string]any{"type": "abort"})
			_ = stdin.Close()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil {
				waitErr = fmt.Errorf("PiTimeoutError: %w", ctx.Err())
			} else {
				waitErr = fmt.Errorf("PiCancelledError: %w", ctx.Err())
			}
			settled = true
		case <-startup.C:
			startup.Reset(time.Hour)
			if !accepted {
				_ = cmd.Process.Kill()
				waitErr = fmt.Errorf("PiStartupError: no RPC prompt response within %s", piStartupTimeout)
				settled = true
			}
		case <-idle.C:
			_ = writeRPC(stdin, map[string]any{"type": "abort"})
			_ = stdin.Close()
			waitErr = fmt.Errorf("PiRPCError: no RPC activity within %s", piIdleTimeout)
			settled = true
		case writeErr := <-writeDone:
			if writeErr != nil {
				waitErr = fmt.Errorf("PiRPCError: send prompt: %w", writeErr)
				settled = true
			}
		case line, ok := <-lines:
			idle.Stop()
			if !ok {
				waitErr = fmt.Errorf("PiProcessError: RPC stdout closed before agent_settled")
				settled = true
				break
			}
			if line.err != nil {
				waitErr = fmt.Errorf("PiRPCError: read JSONL: %w", line.err)
				settled = true
				break
			}
			var ev map[string]any
			if err := json.Unmarshal(line.data, &ev); err != nil {
				waitErr = fmt.Errorf("PiRPCError: malformed JSONL: %w", err)
				settled = true
				break
			}
			switch ev["type"] {
			case "turn_start":
				failedTestInTurn = false
			case "tool_execution_start":
				if ev["toolName"] == "bash" {
					id, _ := ev["toolCallId"].(string)
					args, _ := ev["args"].(map[string]any)
					command, _ := args["command"].(string)
					if id != "" {
						commands[id] = command
					}
					if looksLikeTest(command) {
						result.TestCommandCount++
					}
				}
			case "tool_execution_end":
				appendToolResult(&result.ExecutionResults, ev)
				result.ToolCallCount++
				if ev["toolName"] == "bash" {
					id, _ := ev["toolCallId"].(string)
					if ev["isError"] == true && looksLikeTest(commands[id]) {
						failedTestInTurn = true
					}
					delete(commands, id)
				}
			case "turn_end":
				if failedTestInTurn {
					testFailures++
					if testFailures >= 4 {
						waitErr = fmt.Errorf("PiRepairLimitError: tests still fail after 3 implementation/test repair cycles")
						_ = writeRPC(stdin, map[string]any{"type": "abort"})
						failedTestInTurn = false
					}
				}
			case "response":
				if ev["id"] == "mac-prompt" {
					if ev["success"] != true {
						waitErr = fmt.Errorf("PiRPCError: Pi rejected prompt")
						settled = true
					} else {
						accepted = true
						startup.Stop()
					}
				}
			case "agent_settled":
				settled = true
			case "agent_end":
				finalText.Reset()
				collectFinalText(&finalText, ev)
			}
		}
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
	}
	if !accepted && waitErr == nil {
		waitErr = fmt.Errorf("PiRPCError: agent settled before accepting prompt")
	}
	_ = stdin.Close()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case processErr := <-wait:
		if processErr != nil {
			result.PiExitStatus = processErr.Error()
			if waitErr == nil {
				waitErr = fmt.Errorf("PiProcessError: %w: %s", processErr, stderr.String())
			}
		} else if processErr == nil {
			result.PiExitStatus = "0"
		}
	case <-time.After(piShutdownGrace):
		_ = cmd.Process.Kill()
		<-wait
		if waitErr == nil {
			waitErr = fmt.Errorf("PiProcessError: shutdown exceeded grace period")
		}
	}
	patch, files, collectErr := wt.Collect()
	collected = true
	result.Patch = patch
	result.ChangedFiles = files
	if testFailures > 0 {
		result.RepairCycleCount = testFailures
		if result.RepairCycleCount > 3 {
			result.RepairCycleCount = 3
		}
	}
	if result.TestCommandCount == 0 {
		result.Warnings = append(result.Warnings, "Pi did not execute a recognized test command.")
	}
	if collectErr != nil && waitErr == nil {
		waitErr = collectErr
	}
	result.Summary = shortSummary(finalText.String(), waitErr)
	if waitErr != nil {
		result.Status = "error"
		return result, waitErr
	}
	return result, nil
}

func replaceEnv(environment []string, key, value string) []string {
	out := make([]string, 0, len(environment)+1)
	prefix := key + "="
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}

func buildPiPrompt(in PromptInput) string {
	var b strings.Builder
	if in.Role == RoleFixer {
		b.WriteString("You are a software repair agent.\nWork only inside the current temporary worktree.\nThe worktree already contains the current implementation that needs repair. Start by inspecting the files named in the task.\nUse review findings, test failures, requirements, and the current implementation to identify the smallest correct fix. Do not reimplement or redesign the feature unless required to fix the defect. Preserve unrelated existing changes and existing public APIs unless explicitly required.\nRun the smallest relevant test first. If it fails, inspect the failure, repair the implementation, and rerun the relevant test. Perform at most 3 repair/test cycles. After targeted tests pass, run the appropriate broader test once when practical.\nDo not commit, push, rebase, reset --hard, clean, switch branches, change branches, delete branches, create/remove worktrees, or modify Git history.\nDo not return full source files or the Direct Backend JSON artifact format. The worktree files are authoritative. Keep the final response short: defect fixed, files changed, tests executed, unresolved problems.\n\nTask:\n")
	} else {
		b.WriteString("You are the implementation worker for multi-agent-coder.\nWork only inside the current temporary worktree.\nStart by inspecting the files named in the task and their direct dependencies. Do not scan the entire repository unless necessary.\nImplement the smallest change satisfying the requirements. Preserve existing public APIs unless explicitly requested. Do not redesign unrelated components.\nAfter editing, run targeted tests first; if they fail, diagnose and repair. Perform at most 3 implementation/test repair cycles. After targeted tests pass, run the appropriate broader project test once.\nDo not commit, push, rebase, reset --hard, clean, switch branches, change branches, delete branches, create/remove worktrees, or modify Git history.\nDo not output full replacement source files or the old Agent JSON artifact format. The actual worktree files are authoritative. Keep the final response short: changes, tests executed, unresolved problems.\n\nTask:\n")
	}
	b.WriteString(in.Task)
	if len(in.Requirements) > 0 {
		b.WriteString("\nRequirements:\n")
		for _, r := range in.Requirements {
			b.WriteString("- " + r + "\n")
		}
	}
	if len(in.Files) > 0 {
		b.WriteString("\nStarting file paths (read only what is needed):\n")
		for _, f := range in.Files {
			b.WriteString("- " + f.Path + "\n")
		}
	}
	if strings.TrimSpace(in.AdditionalContext) != "" {
		b.WriteString("\nAdditional context:\n" + in.AdditionalContext)
	}
	return b.String()
}

type rpcLine struct {
	data []byte
	err  error
}

func readRPC(r io.Reader, out chan<- rpcLine, ctx context.Context) {
	defer close(out)
	br := bufio.NewReader(r)
	for {
		line := make([]byte, 0, 4096)
		var readErr error
		for {
			fragment, err := br.ReadSlice('\n')
			if len(line)+len(fragment) > maxRPCLine {
				select {
				case out <- rpcLine{err: fmt.Errorf("RPC line exceeds limit")}:
				case <-ctx.Done():
				}
				return
			}
			line = append(line, fragment...)
			if err == bufio.ErrBufferFull {
				continue
			}
			readErr = err
			break
		}
		if readErr == io.EOF && len(line) > 0 {
			select {
			case out <- rpcLine{err: fmt.Errorf("unterminated JSONL record")}:
			case <-ctx.Done():
			}
			return
		}
		if len(line) > 0 {
			select {
			case out <- rpcLine{data: bytes.TrimSuffix(bytes.TrimSuffix(line, []byte{'\n'}), []byte{'\r'})}:
			case <-ctx.Done():
				return
			}
		}
		if readErr == io.EOF {
			return
		}
		if readErr != nil {
			select {
			case out <- rpcLine{err: readErr}:
			case <-ctx.Done():
			}
			return
		}
	}
}
func writeRPC(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
func appendToolResult(dst *[]string, ev map[string]any) {
	if len(*dst) >= 64 {
		return
	}
	name, _ := ev["toolName"].(string)
	result, _ := ev["result"].(map[string]any)
	content, _ := result["content"].([]any)
	var texts []string
	for _, item := range content {
		m, _ := item.(map[string]any)
		if t, _ := m["type"].(string); t == "text" {
			s, _ := m["text"].(string)
			if s != "" {
				texts = append(texts, s)
			}
		}
	}
	text := strings.Join(texts, "\n")
	if len(text) > maxToolOutput {
		text = "[truncated]\n" + text[len(text)-maxToolOutput:]
	}
	if name != "" {
		*dst = append(*dst, name+": "+text)
	}
}

func looksLikeTest(command string) bool {
	command = strings.ToLower(command)
	for _, marker := range []string{"go test", "pytest", "python -m unittest", "npm test", "pnpm test", "yarn test", "bun test", "cargo test", "make test", "ctest", "dotnet test", "mvn test", "gradle test"} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return strings.Contains(command, "test")
}
func collectFinalText(dst *strings.Builder, ev map[string]any) {
	msgs, _ := ev["messages"].([]any)
	for _, raw := range msgs {
		m, _ := raw.(map[string]any)
		if m["role"] != "assistant" {
			continue
		}
		content, _ := m["content"].([]any)
		for _, c := range content {
			part, _ := c.(map[string]any)
			if part["type"] == "text" {
				s, _ := part["text"].(string)
				if strings.TrimSpace(s) != "" {
					const maxFinalText = 8 << 10
					remaining := maxFinalText - dst.Len()
					if remaining > 0 {
						if len(s) > remaining {
							s = s[:remaining]
						}
						dst.WriteString(s)
					}
				}
			}
		}
	}
}
func shortSummary(text string, err error) string {
	v := strings.TrimSpace(text)
	if err != nil {
		if v != "" {
			v += "\nUnresolved: "
		}
		v += err.Error()
	}
	if v == "" {
		return "Pi completed the task."
	}
	if len(v) > 1200 {
		return v[:1200]
	}
	return v
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < b.limit {
		remaining := b.limit - b.Len()
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}

package pirpc

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
	"sync"
	"syscall"
	"time"
)

type Config struct {
	Executable, Provider, Model string
	Args                        []string
}
type Result struct {
	Summary     string
	ToolResults []string
	AgentError  string
}

type Client struct{ Config Config }

type Error struct {
	Kind string
	Err  error
}

func (e *Error) Error() string     { return e.Kind + ": " + e.Err.Error() }
func (e *Error) Unwrap() error     { return e.Err }
func (e *Error) ErrorKind() string { return e.Kind }
func failure(kind, format string, args ...any) error {
	return &Error{Kind: kind, Err: fmt.Errorf(format, args...)}
}

type event struct {
	Type     string          `json:"type"`
	ID       string          `json:"id,omitempty"`
	Command  string          `json:"command,omitempty"`
	Success  bool            `json:"success,omitempty"`
	Error    string          `json:"error,omitempty"`
	ToolName string          `json:"toolName,omitempty"`
	IsError  bool            `json:"isError,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
	Message  json.RawMessage `json:"message,omitempty"`
	Data     struct {
		Text *string `json:"text"`
	} `json:"data,omitempty"`
}

func (c Client) Run(ctx context.Context, dir, prompt string) (Result, error) {
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	path, err := exec.LookPath(c.Config.Executable)
	if err != nil {
		return Result{}, failure("PiExecutableNotFound", "%q: %w", c.Config.Executable, err)
	}
	args := []string{"--mode", "rpc", "--no-session"}
	if c.Config.Provider != "" {
		args = append(args, "--provider", c.Config.Provider)
	}
	if c.Config.Model != "" {
		args = append(args, "--model", c.Config.Model)
	}
	args = append(args, c.Config.Args...)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, failure("PiStartupError", "stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, failure("PiStartupError", "stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, failure("PiStartupError", "stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		if contextErr := contextError(ctx); contextErr != nil {
			return Result{}, contextErr
		}
		return Result{}, failure("PiStartupError", "start: %w", err)
	}
	stderrBuf := &limitedBuffer{limit: 64 << 10}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = io.Copy(stderrBuf, stderr) }()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	waited := false
	defer func() {
		_ = stdin.Close()
		if !waited {
			_ = stdout.Close()
			_ = stderr.Close()
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-waitCh
		}
		wg.Wait()
	}()
	enc := json.NewEncoder(stdin)
	result := Result{}
	if err := enc.Encode(map[string]any{"id": "task", "type": "prompt", "message": prompt}); err != nil {
		return result, failure("PiRPCProtocolError", "send prompt: %w", err)
	}

	reader := bufio.NewReader(stdout)
	accepted := false
	settled := false
	for !settled {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSuffix(line, []byte{'\n'})
			line = bytes.TrimSuffix(line, []byte{'\r'})
			var ev event
			if err := json.Unmarshal(line, &ev); err != nil {
				return result, failure("PiRPCProtocolError", "decode JSONL: %w", err)
			}
			switch ev.Type {
			case "response":
				if ev.ID == "task" {
					if !ev.Success {
						return result, failure("PiRPCProtocolError", "prompt rejected: %s", ev.Error)
					}
					accepted = true
				}
			case "tool_execution_end":
				result.ToolResults = append(result.ToolResults, toolResult(ev))
			case "message_end":
				var message struct{ Role, StopReason, ErrorMessage string }
				if json.Unmarshal(ev.Message, &message) == nil && message.Role == "assistant" && (message.StopReason == "error" || message.ErrorMessage != "") {
					result.AgentError = strings.TrimSpace(message.ErrorMessage)
					if result.AgentError == "" {
						result.AgentError = "assistant stopped with reason: " + message.StopReason
					}
				}
			case "agent_settled":
				settled = true
			}
		}
		if readErr != nil && !settled {
			waitErr := <-waitCh
			waited = true
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return result, failure("PiTimeoutError", "%w", ctx.Err())
			}
			if ctx.Err() != nil {
				return result, failure("PiCanceledError", "%w", ctx.Err())
			}
			return result, failure("PiAbnormalExit", "unexpected EOF: %v: %s", waitErr, strings.TrimSpace(stderrBuf.String()))
		}
	}
	if !accepted {
		return result, failure("PiRPCProtocolError", "agent settled before prompt acceptance")
	}
	if err := enc.Encode(map[string]any{"id": "summary", "type": "get_last_assistant_text"}); err != nil {
		return result, failure("PiRPCProtocolError", "request final text: %w", err)
	}
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return result, failure("PiRPCProtocolError", "read final text: %w", err)
		}
		var ev event
		if err := json.Unmarshal(bytes.TrimSpace(line), &ev); err != nil {
			return result, failure("PiRPCProtocolError", "decode final text: %w", err)
		}
		if ev.Type == "response" && ev.ID == "summary" {
			if !ev.Success {
				return result, failure("PiRPCProtocolError", "final text rejected: %s", ev.Error)
			}
			if ev.Data.Text != nil {
				result.Summary = *ev.Data.Text
			}
			break
		}
	}
	_ = stdin.Close()
	waitErr := <-waitCh
	waited = true
	if waitErr != nil {
		return result, failure("PiAbnormalExit", "%w: %s", waitErr, strings.TrimSpace(stderrBuf.String()))
	}
	return result, nil
}

func toolResult(ev event) string {
	entry := map[string]any{"tool": ev.ToolName, "is_error": ev.IsError}
	if len(ev.Args) > 0 {
		var v any
		if json.Unmarshal(ev.Args, &v) == nil {
			entry["args"] = v
		}
	}
	if len(ev.Result) > 0 {
		const resultLimit = 12 << 10
		if len(ev.Result) > resultLimit {
			entry["result_preview"] = string(ev.Result[:resultLimit])
			entry["truncated"] = true
		} else {
			var v any
			if json.Unmarshal(ev.Result, &v) == nil {
				entry["result"] = v
			}
		}
	}
	b, _ := json.Marshal(entry)
	return string(b)
}

type limitedBuffer struct {
	data  []byte
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n >= b.limit {
		b.data = append(b.data[:0], p[n-b.limit:]...)
		return n, nil
	}
	overflow := len(b.data) + n - b.limit
	if overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *limitedBuffer) String() string { return string(b.data) }

func contextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failure("PiTimeoutError", "%w", ctx.Err())
	}
	if ctx.Err() != nil {
		return failure("PiCanceledError", "%w", ctx.Err())
	}
	return nil
}

package pirpc

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientNormalCompletion(t *testing.T) {
	p := script(t, "read first\necho '{\"id\":\"task\",\"type\":\"response\",\"command\":\"prompt\",\"success\":true}'\necho '{\"type\":\"tool_execution_end\",\"toolName\":\"bash\",\"isError\":false,\"result\":{}}'\necho '{\"type\":\"agent_settled\"}'\nread second\necho '{\"id\":\"summary\",\"type\":\"response\",\"command\":\"get_last_assistant_text\",\"success\":true,\"data\":{\"text\":\"done\"}}'")
	got, err := (Client{Config: Config{Executable: p}}).Run(context.Background(), t.TempDir(), "task")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "done" || len(got.ToolResults) != 1 {
		t.Fatalf("result=%+v", got)
	}
}

func TestClientProcessesToolCallsUntilAgentSettled(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := script(t, `
read first
echo '{"id":"task","type":"response","command":"prompt","success":true}'
echo '{"type":"agent_start"}'
echo '{"type":"turn_start"}'
echo '{"type":"message_start"}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"text_start"}}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"safe text"}}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"text_end"}}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"toolcall_start","toolName":"read"}}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","delta":"SECRET_TOOL_ARGUMENTS"}}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","toolCall":{"name":"read","arguments":{"path":"secret.go"}}}}'
echo '{"type":"tool_execution_start","toolName":"read"}'
echo '{"type":"tool_execution_update","toolName":"read"}'
echo '{"type":"tool_execution_end","toolName":"read","isError":false,"args":{"path":"secret.go"},"result":{"text":"SECRET_SOURCE"}}'
echo '{"type":"message_end","message":{"role":"assistant"}}'
echo '{"type":"turn_end"}'
echo '{"type":"turn_start"}'
echo '{"type":"message_start"}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"toolcall_start","toolName":"edit"}}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","delta":"SECRET_EDIT_ARGUMENTS"}}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","toolCall":{"name":"edit","arguments":{"path":"secret.go"}}}}'
echo '{"type":"tool_execution_start","toolName":"edit"}'
echo '{"type":"tool_execution_update","toolName":"edit"}'
echo '{"type":"tool_execution_end","toolName":"edit","isError":false,"args":{"path":"secret.go"},"result":{"text":"updated"}}'
echo '{"type":"message_end","message":{"role":"assistant"}}'
echo '{"type":"turn_end"}'
echo '{"type":"agent_end"}'
echo '{"type":"agent_settled"}'
read second
echo '{"id":"summary","type":"response","command":"get_last_assistant_text","success":true,"data":{"text":"edited file"}}'
`)
	got, err := (Client{Config: Config{Executable: p}, Logger: logger}).Run(context.Background(), t.TempDir(), "SECRET_PROMPT")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "edited file" || len(got.ToolResults) != 2 {
		t.Fatalf("result=%+v", got)
	}
	if !strings.Contains(got.ToolResults[0], `"tool":"read"`) || !strings.Contains(got.ToolResults[1], `"tool":"edit"`) {
		t.Fatalf("tool results do not preserve read then edit order: %v", got.ToolResults)
	}
	logText := logs.String()
	for _, expected := range []string{
		"command=prompt", "success=true", "event_type=agent_start", "event_type=turn_start",
		"event_type=message_start", "event_type=message_update", "assistant_message_event_type=text_start",
		"assistant_message_event_type=text_delta", "assistant_message_event_type=text_end",
		"assistant_message_event_type=toolcall_start", "assistant_message_event_type=toolcall_delta",
		"assistant_message_event_type=toolcall_end", "event_type=tool_execution_start",
		"event_type=tool_execution_update", "event_type=tool_execution_end", "tool_name=read",
		"tool_name=edit", "success=true", "event_type=agent_end", "agent_settled", "cwd=",
	} {
		if !strings.Contains(logText, expected) {
			t.Errorf("debug logs missing %q:\n%s", expected, logText)
		}
	}
	for _, forbidden := range []string{"SECRET_PROMPT", "SECRET_TOOL_ARGUMENTS", "SECRET_EDIT_ARGUMENTS", "SECRET_SOURCE", "secret.go"} {
		if strings.Contains(logText, forbidden) {
			t.Errorf("debug logs contain sensitive payload %q", forbidden)
		}
	}
}

func TestClientOmitsToolCallJSONFromSummary(t *testing.T) {
	p := script(t, "read first\necho '{\"id\":\"task\",\"type\":\"response\",\"command\":\"prompt\",\"success\":true}'\necho '{\"type\":\"agent_settled\"}'\nread second\necho '{\"id\":\"summary\",\"type\":\"response\",\"command\":\"get_last_assistant_text\",\"success\":true,\"data\":{\"text\":\"{{\\\"name\\\": \\\"read\\\", \\\"arguments\\\": {\\\"path\\\": \\\"source.go\\\"}}}\"}}'")
	got, err := (Client{Config: Config{Executable: p}}).Run(context.Background(), t.TempDir(), "task")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "" {
		t.Fatalf("tool-call JSON leaked into summary: %q", got.Summary)
	}
}

func TestIsToolCallJSON(t *testing.T) {
	for _, input := range []string{
		`{"name":"read","arguments":{"path":"source.go"}}`,
		`{{"name":"read","arguments":{"path":"source.go"}}}`,
		"```json\n{\"name\":\"read\",\"arguments\":{\"path\":\"source.go\"}}\n```",
	} {
		if !isToolCallJSON(input) {
			t.Errorf("isToolCallJSON(%q) = false", input)
		}
	}
	if isToolCallJSON(`{"status":"done","summary":"read file"}`) {
		t.Fatal("ordinary JSON summary classified as a tool call")
	}
}

func TestSanitizeSummaryRemovesToolCallBlockAndKeepsText(t *testing.T) {
	input := "```json\n{\"name\": \"read\", \"arguments\": {\"path\": \"source.go\"}}\n```\n\nReads source.go to locate the requested function."
	got := sanitizeSummary(input)
	if got != "Reads source.go to locate the requested function." {
		t.Fatalf("sanitizeSummary() = %q", got)
	}
}

func TestClientRunsInSuppliedDirectory(t *testing.T) {
	dir := t.TempDir()
	cwdFile := filepath.Join(t.TempDir(), "cwd")
	t.Setenv("PI_TEST_CWD_FILE", cwdFile)
	p := script(t, "pwd > \"$PI_TEST_CWD_FILE\"\nread first\necho '{\"id\":\"task\",\"type\":\"response\",\"command\":\"prompt\",\"success\":true}'\necho '{\"type\":\"agent_settled\"}'\nread second\necho '{\"id\":\"summary\",\"type\":\"response\",\"command\":\"get_last_assistant_text\",\"success\":true,\"data\":{\"text\":\"done\"}}'")
	if _, err := (Client{Config: Config{Executable: p}}).Run(context.Background(), dir, "task"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cwdFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != dir {
		t.Fatalf("Pi cwd=%q, want %q", strings.TrimSpace(string(got)), dir)
	}
}

func TestClientMalformedResponse(t *testing.T) {
	p := script(t, "read first\necho '{broken'")
	_, err := (Client{Config: Config{Executable: p}}).Run(context.Background(), t.TempDir(), "task")
	if err == nil || !strings.Contains(err.Error(), "PiRPCProtocolError") {
		t.Fatalf("error=%v", err)
	}
}
func TestClientUnexpectedEOF(t *testing.T) {
	p := script(t, "read first\nexit 7")
	_, err := (Client{Config: Config{Executable: p}}).Run(context.Background(), t.TempDir(), "task")
	if err == nil || !strings.Contains(err.Error(), "PiAbnormalExit") {
		t.Fatalf("error=%v", err)
	}
}
func TestClientTimeout(t *testing.T) {
	p := script(t, "read first\nsleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := (Client{Config: Config{Executable: p}}).Run(ctx, t.TempDir(), "task")
	if err == nil || !strings.Contains(err.Error(), "PiTimeoutError") {
		t.Fatalf("error=%v", err)
	}
}
func TestClientCancel(t *testing.T) {
	p := script(t, "read first\nsleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (Client{Config: Config{Executable: p}}).Run(ctx, t.TempDir(), "task")
	if err == nil || !strings.Contains(err.Error(), "PiCanceledError") {
		t.Fatalf("error=%v", err)
	}
}
func TestClientExecutableNotFound(t *testing.T) {
	_, err := (Client{Config: Config{Executable: filepath.Join(t.TempDir(), "missing")}}).Run(context.Background(), t.TempDir(), "task")
	if err == nil || !strings.Contains(err.Error(), "PiExecutableNotFound") {
		t.Fatalf("error=%v", err)
	}
}

func TestClientReportsAssistantFailure(t *testing.T) {
	p := script(t, "read first\necho '{\"id\":\"task\",\"type\":\"response\",\"command\":\"prompt\",\"success\":true}'\necho '{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"stopReason\":\"error\",\"errorMessage\":\"provider failed\"}}'\necho '{\"type\":\"agent_settled\"}'\nread second\necho '{\"id\":\"summary\",\"type\":\"response\",\"command\":\"get_last_assistant_text\",\"success\":true,\"data\":{\"text\":null}}'")
	got, err := (Client{Config: Config{Executable: p}}).Run(context.Background(), t.TempDir(), "task")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentError != "provider failed" {
		t.Fatalf("AgentError=%q", got.AgentError)
	}
}

func TestLimitedBufferKeepsBoundedTail(t *testing.T) {
	b := &limitedBuffer{limit: 5}
	n, err := b.Write([]byte("abcdefgh"))
	if err != nil || n != 8 || b.String() != "defgh" {
		t.Fatalf("Write=%d,%v buffer=%q", n, err, b.String())
	}
}
func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-pi")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

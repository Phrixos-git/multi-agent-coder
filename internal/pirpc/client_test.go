package pirpc

import (
	"context"
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

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/arietis/multi-agent-coder/internal/config"
	roleprompts "github.com/arietis/multi-agent-coder/prompts"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectInMemory serves server over an in-memory transport for one test.
func connectInMemory(t *testing.T, server *sdkmcp.Server) *sdkmcp.ClientSession {
	t.Helper()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "1"}, nil)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestLocalAgentRunSystemPromptPerOrchestrator(t *testing.T) {
	for _, test := range []struct {
		orchestrator string
		promptDir    string
	}{
		{orchestrator: "", promptDir: ""},
		{orchestrator: config.OrchestratorCodex, promptDir: ""},
		{orchestrator: config.OrchestratorClaudeCode, promptDir: "claude-code/"},
	} {
		t.Run("orchestrator="+test.orchestrator, func(t *testing.T) {
			completer := &recordingCompleter{}
			server, err := newServer(config.Config{RepositoryRoot: t.TempDir(), Orchestrator: test.orchestrator}, discardLogger(), completer)
			if err != nil {
				t.Fatal(err)
			}
			session := connectInMemory(t, server.SDKServer())
			if _, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "local_agent_run", Arguments: map[string]any{
				"role": "coder", "task": "make a candidate",
			}}); err != nil {
				t.Fatal(err)
			}
			want, err := fs.ReadFile(roleprompts.FS, test.promptDir+"coder.md")
			if err != nil {
				t.Fatal(err)
			}
			if len(completer.messages) != 2 || completer.messages[0].Role != "system" || completer.messages[0].Content != strings.TrimSpace(string(want)) {
				t.Fatalf("system message = %#v, want prompts/%scoder.md", completer.messages, test.promptDir)
			}
		})
	}
}

// runToolOnce registers local_agent_run with a fixed agent response and calls it.
func runToolOnce(t *testing.T, cfg config.Config, response string) *sdkmcp.CallToolResult {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	registerRunTool(server, staticRunner{response: response}, noFilesReader{}, cfg, discardLogger())
	session := connectInMemory(t, server)
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "local_agent_run", Arguments: map[string]any{
		"role": "coder", "task": "task",
	}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// legacyEncoding is the text Content that the unmodified handler returns:
// the SDK encodes the RunOutput, decodes it into a map for schema validation,
// and encodes that map again.
func legacyEncoding(t *testing.T, response string) string {
	t.Helper()
	encoded, err := json.Marshal(executeRun(context.Background(), staticRunner{response: response}, noFilesReader{}, RunInput{Role: "coder", Task: "task"}))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	if encoded, err = json.Marshal(value); err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func resultText(t *testing.T, result *sdkmcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v", result.Content)
	}
	text, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %#v", result.Content[0])
	}
	return text.Text
}

// assertTextMatchesStructured checks that the text Content and the
// structured content carry the same JSON value.
func assertTextMatchesStructured(t *testing.T, result *sdkmcp.CallToolResult) map[string]any {
	t.Helper()
	var fromText map[string]any
	if err := json.Unmarshal([]byte(resultText(t, result)), &fromText); err != nil {
		t.Fatal(err)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok || !reflect.DeepEqual(fromText, structured) {
		t.Fatalf("text content %v differs from structured content %#v", fromText, result.StructuredContent)
	}
	return structured
}

func largeResponse(t *testing.T, status string) string {
	t.Helper()
	value := map[string]any{
		"status": status, "role": "coder", "summary": "large candidate",
		"artifacts": []map[string]string{
			{"path": "a.go", "content": strings.Repeat("a\n", 30000)},
			{"path": "b.go", "content": strings.Repeat("日本語", 20000)},
		},
		"findings":      []map[string]string{{"severity": "low", "description": "kept finding"}},
		"risks":         []string{"kept risk"},
		"notes":         []string{"kept note"},
		"patch":         strings.Repeat("+<line>\n", 10000),
		"changed_files": []string{"a.go", "b.go"},
		"warnings":      []string{"existing warning"},
	}
	if status == "error" {
		value["error"] = map[string]string{"kind": "LLMResponseError", "message": "kept error"}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

const smallResponse = `{"status":"success","role":"coder","summary":"ok","artifacts":[{"path":"a.go","content":"package a\n"}],"findings":[],"risks":[],"notes":[],"patch":"diff"}`

func TestLocalAgentRunCodexOutputIsUnchanged(t *testing.T) {
	// Claude Code settings must not affect Codex, even when set to values
	// that would truncate output or set isError.
	for _, cfg := range []config.Config{
		{},
		{Orchestrator: config.OrchestratorCodex},
		{Orchestrator: config.OrchestratorCodex, ClaudeMaxOutputChars: 100, ClaudeToolErrorFlag: true},
	} {
		for _, response := range []string{smallResponse, largeResponse(t, "success"), largeResponse(t, "error"), "not JSON"} {
			result := runToolOnce(t, cfg, response)
			if result.IsError {
				t.Fatalf("cfg=%+v: Codex result has isError", cfg)
			}
			if got, want := resultText(t, result), legacyEncoding(t, response); got != want {
				t.Fatalf("cfg=%+v: Codex output changed:\n got=%.200s\nwant=%.200s", cfg, got, want)
			}
			assertTextMatchesStructured(t, result)
		}
	}
}

func TestLocalAgentRunClaudeCodeWithinLimitIsUnchanged(t *testing.T) {
	cfg := config.Config{Orchestrator: config.OrchestratorClaudeCode, ClaudeMaxOutputChars: 80000, ClaudeToolErrorFlag: true}
	result := runToolOnce(t, cfg, smallResponse)
	if result.IsError || resultText(t, result) != legacyEncoding(t, smallResponse) {
		t.Fatalf("result = %+v", result)
	}
	// 0 means unlimited.
	cfg.ClaudeMaxOutputChars = 0
	large := largeResponse(t, "success")
	if got := resultText(t, runToolOnce(t, cfg, large)); got != legacyEncoding(t, large) {
		t.Fatal("unlimited Claude Code output was modified")
	}
}

func TestLocalAgentRunClaudeCodeTruncatesLargeFields(t *testing.T) {
	const limit = 40000
	cfg := config.Config{Orchestrator: config.OrchestratorClaudeCode, ClaudeMaxOutputChars: limit, ClaudeToolErrorFlag: true}
	result := runToolOnce(t, cfg, largeResponse(t, "success"))
	if result.IsError {
		t.Fatal("success result has isError")
	}
	if size := utf8.RuneCountInString(resultText(t, result)); size > limit {
		t.Fatalf("response has %d chars, limit %d", size, limit)
	}
	output := assertTextMatchesStructured(t, result)

	var original RunOutput
	if err := json.Unmarshal([]byte(largeResponse(t, "success")), &original); err != nil {
		t.Fatal(err)
	}
	patch := output["patch"].(string)
	if !strings.HasPrefix(original.Patch, patch) || len(patch) >= len(original.Patch) {
		t.Fatal("patch was not truncated from the end")
	}
	artifacts := output["artifacts"].([]any)
	for i, raw := range artifacts {
		artifact := raw.(map[string]any)
		content := artifact["content"].(string)
		if artifact["path"] != original.Artifacts[i].Path || !strings.HasPrefix(original.Artifacts[i].Content, content) || !utf8.ValidString(content) {
			t.Fatalf("artifact %d was not truncated from the end", i)
		}
	}

	warnings := output["warnings"].([]any)
	if warnings[0] != "existing warning" {
		t.Fatalf("existing warning was not kept: %v", warnings)
	}
	wantPrefixes := map[string]int{"patch": utf8.RuneCountInString(original.Patch), "artifacts[0].content": utf8.RuneCountInString(original.Artifacts[0].Content), "artifacts[1].content": utf8.RuneCountInString(original.Artifacts[1].Content)}
	for field, from := range wantPrefixes {
		var current int
		switch field {
		case "patch":
			current = utf8.RuneCountInString(patch)
		default:
			index := int(field[len("artifacts[")] - '0')
			current = utf8.RuneCountInString(artifacts[index].(map[string]any)["content"].(string))
		}
		want := fmt.Sprintf("OutputTruncated: %s truncated from %d to %d chars", field, from, current)
		if !containsAny(warnings, want) {
			t.Fatalf("warnings %v do not contain %q", warnings, want)
		}
	}
	if !containsAny(warnings, outputTruncatedSplitHint) {
		t.Fatalf("warnings %v do not ask to split the task", warnings)
	}

	if output["summary"] != "large candidate" || output["status"] != "success" {
		t.Fatalf("summary/status changed: %v", output)
	}
	var originalValues map[string]any
	if err := json.Unmarshal([]byte(largeResponse(t, "success")), &originalValues); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"findings", "risks", "notes", "changed_files"} {
		if !reflect.DeepEqual(output[key], originalValues[key]) {
			t.Fatalf("%s changed: %v, want %v", key, output[key], originalValues[key])
		}
	}
}

func TestLocalAgentRunClaudeCodeKeepsErrorAndSetsIsError(t *testing.T) {
	cfg := config.Config{Orchestrator: config.OrchestratorClaudeCode, ClaudeMaxOutputChars: 40000, ClaudeToolErrorFlag: true}
	result := runToolOnce(t, cfg, largeResponse(t, "error"))
	if !result.IsError {
		t.Fatal("error result has no isError")
	}
	output := assertTextMatchesStructured(t, result)
	if output["status"] != "error" || output["error"].(map[string]any)["message"] != "kept error" {
		t.Fatalf("error details changed: %v", output["error"])
	}
	if size := utf8.RuneCountInString(resultText(t, result)); size > 40000 {
		t.Fatalf("response has %d chars", size)
	}
}

func TestLocalAgentRunClaudeCodeIsErrorFlag(t *testing.T) {
	for _, test := range []struct {
		response    string
		flag        bool
		wantIsError bool
	}{
		{response: "not JSON", flag: true, wantIsError: true},
		{response: "not JSON", flag: false, wantIsError: false},
		{response: smallResponse, flag: true, wantIsError: false},
	} {
		cfg := config.Config{Orchestrator: config.OrchestratorClaudeCode, ClaudeMaxOutputChars: 80000, ClaudeToolErrorFlag: test.flag}
		result := runToolOnce(t, cfg, test.response)
		if result.IsError != test.wantIsError {
			t.Fatalf("response=%q flag=%t: isError=%t", test.response, test.flag, result.IsError)
		}
		// The payload is identical to the Codex payload; only isError differs.
		if got, want := resultText(t, result), legacyEncoding(t, test.response); got != want {
			t.Fatalf("payload changed:\n got=%s\nwant=%s", got, want)
		}
		assertTextMatchesStructured(t, result)
	}
}

func TestLimitOutputCharsReportsUntruncatableOverflow(t *testing.T) {
	output := RunOutput{Status: "success", Role: "coder", Summary: strings.Repeat("s", 500), Artifacts: []Artifact{}, Findings: []Finding{}, Risks: []string{}, Notes: []string{}, Patch: strings.Repeat("p", 500)}
	limited := limitOutputChars(output, 300)
	if limited.Patch != "" || limited.Summary != output.Summary {
		t.Fatalf("limited = %+v", limited)
	}
	if !containsAny(toAny(limited.Warnings), "OutputTruncated: patch truncated from 500 to 0 chars") ||
		!strings.Contains(limited.Warnings[len(limited.Warnings)-1], "still exceeds MAC_CLAUDE_MAX_OUTPUT_CHARS=300") {
		t.Fatalf("warnings = %v", limited.Warnings)
	}
	if output.Patch == "" || output.Warnings != nil {
		t.Fatal("limitOutputChars modified its input")
	}
	if encoded, _ := json.Marshal(limited); !strings.Contains(string(encoded), `"artifacts":[]`) {
		t.Fatalf("empty artifacts were not kept as []: %s", encoded)
	}
}

func TestLocalAgentHealthReportsOrchestrator(t *testing.T) {
	for _, test := range []struct{ orchestrator, want string }{
		{"", "codex"}, {config.OrchestratorCodex, "codex"}, {config.OrchestratorClaudeCode, "claude-code"},
	} {
		server, err := NewServer(config.Config{RepositoryRoot: t.TempDir(), Orchestrator: test.orchestrator}, discardLogger())
		if err != nil {
			t.Fatal(err)
		}
		session := connectInMemory(t, server.SDKServer())
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "local_agent_health", Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		if got := result.StructuredContent.(map[string]any)["orchestrator"]; got != test.want {
			t.Fatalf("orchestrator=%q: health orchestrator = %v, want %s", test.orchestrator, got, test.want)
		}
	}
}

func containsAny(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func toAny(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

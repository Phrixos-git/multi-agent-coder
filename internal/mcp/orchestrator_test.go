package mcp

import (
	"context"
	"io/fs"
	"strings"
	"testing"

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

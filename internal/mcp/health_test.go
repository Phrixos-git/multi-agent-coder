package mcp

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/arietis/multi-agent-coder/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLocalAgentHealth(t *testing.T) {
	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()

	ctx := context.Background()
	go func() { _ = server.SDKServer().Run(ctx, serverTransport) }()
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "local_agent_health"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	output, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent type = %T", result.StructuredContent)
	}
	if output["status"] != "ok" || output["llm"] != "not_checked" {
		t.Fatalf("StructuredContent = %#v", output)
	}
}

package mcp

import (
	"context"
	"log/slog"

	"github.com/arietis/multi-agent-coder/internal/agent"
	"github.com/arietis/multi-agent-coder/internal/config"
	"github.com/arietis/multi-agent-coder/internal/llm"
	"github.com/arietis/multi-agent-coder/internal/workspace"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "multi-agent-coder"
	serverVersion = "0.1.0"
)

// Server is the STDIO MCP server.
type Server struct {
	server *sdkmcp.Server
}

// NewServer constructs the server and registers its tools.
func NewServer(cfg config.Config, logger *slog.Logger) (*Server, error) {
	return newServer(cfg, logger, llm.NewClientWithLogger(cfg, nil, logger))
}

func newServer(cfg config.Config, logger *slog.Logger, completer agent.ChatCompleter) (*Server, error) {
	reader, err := workspace.NewReader(cfg.RepositoryRoot)
	if err != nil {
		return nil, err
	}
	runner, err := agent.NewRunner(completer)
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	registerHealthTool(server, cfg, logger)
	registerRunTool(server, runner, reader, logger)
	return &Server{server: server}, nil
}

// Run serves MCP messages on stdin and stdout until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &sdkmcp.StdioTransport{})
}

// SDKServer exposes the underlying official SDK server for in-process clients.
func (s *Server) SDKServer() *sdkmcp.Server { return s.server }

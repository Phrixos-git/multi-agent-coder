package mcp

import (
	"context"
	"log/slog"

	"github.com/arietis/multi-agent-coder/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "multi-agent-coder"
	serverVersion = "0.1.0"
)

// Server is the Phase 1 STDIO MCP server.
type Server struct {
	server *sdkmcp.Server
}

// NewServer constructs the server and registers the Phase 1 tools.
func NewServer(cfg config.Config, logger *slog.Logger) *Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	registerHealthTool(server, cfg, logger)
	return &Server{server: server}
}

// Run serves MCP messages on stdin and stdout until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &sdkmcp.StdioTransport{})
}

// SDKServer exposes the underlying official SDK server for in-process clients.
func (s *Server) SDKServer() *sdkmcp.Server { return s.server }

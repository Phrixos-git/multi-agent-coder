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
	backends := make(map[agent.Role]agent.Backend, 4)
	for _, role := range []agent.Role{agent.RoleCoder, agent.RoleFixer, agent.RoleReviewer, agent.RoleTester} {
		endpoint, maxTokens := directConfigForRole(cfg, role)
		format := llm.ResponseFormat{Type: "json_object"}
		if role == agent.RoleCoder || role == agent.RoleFixer {
			format = agentJSONSchema(role)
		}
		direct, err := agent.NewDirectBackend(llm.NewClientForBackendWithFormat(endpoint, maxTokens, cfg.LLMReasoningEffort, cfg.LLMTemperature, format, nil, logger))
		if err != nil {
			return nil, err
		}
		backends[role] = direct
	}
	piBackend := agent.NewPiBackend(cfg)
	if cfg.CoderBackend == "pi" {
		backends[agent.RoleCoder] = piBackend
	}
	if cfg.FixerBackend == "pi" {
		backends[agent.RoleFixer] = piBackend
	}
	return newServerWithBackends(cfg, logger, backends)
}

// directConfigForRole resolves routing before the HTTP client is constructed.
func directConfigForRole(cfg config.Config, role agent.Role) (config.LLMConfig, int) {
	switch role {
	case agent.RoleCoder:
		return cfg.CodingLLM, cfg.CoderMaxTokens
	case agent.RoleFixer:
		return cfg.CodingLLM, cfg.FixerMaxTokens
	case agent.RoleReviewer:
		return cfg.MainLLM, cfg.ReviewerMaxTokens
	case agent.RoleTester:
		return cfg.MainLLM, cfg.TesterMaxTokens
	default:
		return cfg.MainLLM, cfg.CoderMaxTokens
	}
}

func newServer(cfg config.Config, logger *slog.Logger, completer agent.ChatCompleter) (*Server, error) {
	direct, err := agent.NewDirectBackend(completer)
	if err != nil {
		return nil, err
	}
	return newServerWithBackends(cfg, logger, map[agent.Role]agent.Backend{
		agent.RoleCoder: direct, agent.RoleFixer: direct, agent.RoleReviewer: direct, agent.RoleTester: direct,
	})
}

func newServerWithBackends(cfg config.Config, logger *slog.Logger, backends map[agent.Role]agent.Backend) (*Server, error) {
	reader, err := workspace.NewReader(cfg.RepositoryRoot)
	if err != nil {
		return nil, err
	}
	runner := agent.NewRunnerWithRoleBackends(backends)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	registerHealthTool(server, cfg, logger)
	registerRunTool(server, runner, reader, cfg, logger)
	return &Server{server: server}, nil
}

// Run serves MCP messages on stdin and stdout until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &sdkmcp.StdioTransport{})
}

// SDKServer exposes the underlying official SDK server for in-process clients.
func (s *Server) SDKServer() *sdkmcp.Server { return s.server }
